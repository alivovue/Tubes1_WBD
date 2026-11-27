package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
)

func TestWorkerDeadlineIsIndependentOfViewerTimeout(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	config := dbmanagement.DefaultConfig(make([]byte, 32), false)
	config.QueryTimeout = 500 * time.Millisecond
	databases, err := dbmanagement.NewService(f.runtimeStore, f.userAccess, config)
	must(t, err)
	runner.Databases = databases
	runner.JobTimeout = 10 * time.Second
	viewer, err := databases.OpenMigrationTarget(f.ctx, f.editor, f.database)
	must(t, err)
	var timeout string
	must(t, viewer.DB.QueryRowContext(f.ctx, `SHOW statement_timeout`).Scan(&timeout))
	if timeout != "500ms" {
		t.Fatalf("viewer statement timeout changed: %s", timeout)
	}
	_, err = viewer.DB.ExecContext(f.ctx, `SELECT pg_sleep(1)`)
	viewer.DB.Close()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "57014" {
		t.Fatalf("viewer deadline changed: %v", err)
	}
	id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Slow worker SQL", UpSQL: `CREATE TABLE worker_deadline(id integer); SELECT pg_sleep(1)`, DownSQL: `DROP TABLE worker_deadline`})
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	expired, cancel := context.WithCancel(f.ctx)
	cancel()
	if _, err = databases.OpenWorkerTarget(expired, f.editor, f.database); err == nil {
		t.Fatal("expired worker context ignored")
	}
}
