package backend

import (
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/platform"
)

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
	return Services{Access: runtime.Access, Databases: databases, Migrations: migrations, Production: runtime.Production}, closeDB, nil
}
