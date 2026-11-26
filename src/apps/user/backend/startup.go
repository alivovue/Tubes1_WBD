package backend

import (
	"crypto/rand"
	"os"
	"strconv"
	"time"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/queue"
	"tubes1_wbd/backend/worker"
	"tubes1_wbd/database/dao"
	"tubes1_wbd/database/platform"
)

type Services struct {
	Access     *access.Service
	Databases  *dbmanagement.Service
	Migrations *migration.Service
	Production bool
	Jobs       *queue.Service
}

func servicesFromEnvironment(root string) (Services, func(), error) {
	runtime, err := platform.Open(root)
	if err != nil {
		return Services{}, nil, err
	}
	closeDB := func() { runtime.Close() }
	databases, err := dbmanagement.NewService(runtime.Store, runtime.Access, dbmanagement.DefaultConfig(runtime.Key, runtime.Production))
	if err != nil {
		closeDB()
		return Services{}, nil, err
	}
	migrations, err := migration.NewService(runtime.Store, runtime.Access, databases)
	if err != nil {
		closeDB()
		return Services{}, nil, err
	}
	jobs := ConfigureExecution(runtime.Store, runtime.Access, databases, migrations)
	return Services{Access: runtime.Access, Databases: databases, Migrations: migrations, Jobs: jobs, Production: runtime.Production}, closeDB, nil
}

func ConfigureExecution(store *dao.Store, auth *access.Service, databases *dbmanagement.Service, migrations *migration.Service) *queue.Service {
	jobs := &queue.Service{Store: store, Access: auth}
	migrations.ConfigureWorkflow(jobs.Options(worker.Recovery{Store: store, Databases: databases}))
	jobs.Workflow = migrations.Workflow()
	return jobs
}
func OpenWorker(root string) (*worker.Runner, func(), error) {
	runtime, err := platform.Open(root)
	if err != nil {
		return nil, nil, err
	}
	closeDB := func() { runtime.Close() }
	databases, err := dbmanagement.NewService(runtime.Store, runtime.Access, dbmanagement.DefaultConfig(runtime.Key, runtime.Production))
	if err != nil {
		closeDB()
		return nil, nil, err
	}
	migrations, err := migration.NewService(runtime.Store, runtime.Access, databases)
	if err != nil {
		closeDB()
		return nil, nil, err
	}
	ConfigureExecution(runtime.Store, runtime.Access, databases, migrations)
	runner := worker.New(runtime.Store, migrations.Workflow(), databases, rand.Text())
	for _, option := range []struct {
		name     string
		min, max int
		set      func(int)
	}{
		{"SCHAKKER_WORKER_CONCURRENCY", 1, 32, func(v int) { runner.Concurrency = v }},
		{"SCHAKKER_WORKER_LEASE_SECONDS", 3, 300, func(v int) { runner.Lease = time.Duration(v) * time.Second }},
		{"SCHAKKER_WORKER_TIMEOUT_SECONDS", 10, 86400, func(v int) { runner.JobTimeout = time.Duration(v) * time.Second }},
	} {
		if raw := os.Getenv(option.name); raw != "" {
			v, e := strconv.Atoi(raw)
			if e != nil || v < option.min || v > option.max {
				closeDB()
				return nil, nil, dao.ErrInvalid
			}
			option.set(v)
		}
	}
	return runner, closeDB, nil
}
