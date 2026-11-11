package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"testing"
	"time"

	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
)

func TestP3BatchPerformanceEvidence(t *testing.T) {
	if os.Getenv("SCHAKKER_RUN_BATCH_BENCHMARK") != "1" {
		t.Skip("set SCHAKKER_RUN_BATCH_BENCHMARK=1 for the real >1s baseline")
	}
	f, jobs, r := p3Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	f.ctx = ctx
	_, err := f.dbs.Managed.ExecContext(ctx, `CREATE TABLE public.batch_counters(id bigint PRIMARY KEY,value bigint NOT NULL)`)
	must(t, err)
	rows := int64(100000)
	var baseline time.Duration
	reset := func() {
		_, err = f.dbs.Managed.ExecContext(ctx, `TRUNCATE batch_counters`)
		must(t, err)
		_, err = f.dbs.Managed.ExecContext(ctx, `INSERT INTO batch_counters SELECT i,0 FROM generate_series(1,$1::bigint) i`, rows)
		must(t, err)
	}
	for {
		reset()
		started := time.Now()
		_, err = f.dbs.Managed.ExecContext(ctx, `UPDATE batch_counters SET value=value+3`)
		must(t, err)
		baseline = time.Since(started)
		if baseline > time.Second {
			break
		}
		if rows >= 12800000 {
			t.Fatal("increase fixture row limit to obtain an empirical >1s baseline")
		}
		rows *= 2
	}
	reset()
	up, down, err := engines.BatchSQL(engines.BatchSpec{Schema: "public", Table: "batch_counters", Key: "id", Column: "value", Delta: 3, Size: 10000, ExpectedRows: rows})
	must(t, err)
	revision, err := f.workflow.CreateDefinition(ctx, f.editor, f.database, migration.DefinitionInput{Title: "large batch benchmark", UpSQL: up, DownSQL: down})
	must(t, err)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(ctx, f.editor, review.ID, "performance-fixture")
	must(t, err)
	started := time.Now()
	_, err = r.RunOnce(ctx)
	must(t, err)
	duration := time.Since(started)
	j, p, err := jobs.Status(ctx, f.reader.ID, id)
	must(t, err)
	if j.Status != "succeeded" || p.Rows != rows {
		t.Fatalf("benchmark incomplete: %#v %#v", j, p)
	}
	var sum int64
	must(t, f.dbs.Managed.QueryRowContext(ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != rows*3 {
		t.Fatal("benchmark data differs from expected update")
	}
	var version string
	must(t, f.dbs.Managed.QueryRowContext(ctx, `SHOW server_version`).Scan(&version))
	report := map[string]any{"os": runtime.GOOS, "architecture": runtime.GOARCH, "logical_cpus": runtime.NumCPU(), "go_version": runtime.Version(), "postgresql_version": version, "rows": rows, "affected_rows": rows, "unbatched_seconds": baseline.Seconds(), "batch_size": 10000, "committed_batches": p.Batches, "committed_rows": p.Rows, "batched_seconds": duration.Seconds(), "batched_rows_per_second": float64(rows) / duration.Seconds(), "status": j.Status, "same_initial_dataset": true}
	raw, err := json.MarshalIndent(report, "", "  ")
	must(t, err)
	t.Log(string(raw))
	if output := os.Getenv("SCHAKKER_BATCH_REPORT"); output != "" {
		must(t, os.WriteFile(output, append(raw, '\n'), 0600))
	}
}
