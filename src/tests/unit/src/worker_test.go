package unit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"tubes1_wbd/backend/migration/engines"
	"tubes1_wbd/backend/queue"
	"tubes1_wbd/backend/worker"
	"tubes1_wbd/database/dao"
)

func TestP3BatchTemplateBindsConfigurationAndBothDirections(t *testing.T) {
	spec := engines.BatchSpec{Schema: "public", Table: "counters", Key: "id", Column: "value", Delta: 7, Size: 50, ExpectedRows: 100}
	up, down, err := engines.BatchSQL(spec)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := engines.ParseBatch(up, down)
	if err != nil || *parsed != spec {
		t.Fatalf("template round trip: %v", err)
	}
	for _, pair := range [][2]string{{up + " SELECT 1;", down}, {up, down + " "}, {up[:len(up)-1], down}} {
		if _, err = engines.ParseBatch(pair[0], pair[1]); err == nil {
			t.Fatal("changed approved template accepted")
		}
	}
	spec.Table = "x; DROP SCHEMA public"
	if _, _, err = engines.BatchSQL(spec); err == nil {
		t.Fatal("identifier injection accepted")
	}
	spec.Table = "counters"
	spec.Size = 10001
	if _, _, err = engines.BatchSQL(spec); err == nil {
		t.Fatal("unbounded batch accepted")
	}
}
func TestP3RetryClassificationAndPartialOutcomes(t *testing.T) {
	j := dao.MigrationJob{Attempt: 1, MaxAttempts: 5}
	for _, err := range []error{context.Canceled, &pgconn.PgError{Code: "40001"}, &pgconn.PgError{Code: "40P01"}} {
		if !worker.Retryable(err) {
			t.Fatalf("temporary error not retryable: %v", err)
		}
	}
	if worker.Retryable(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("constraint failure automatically retried")
	}
	status, _ := worker.Outcome(context.Canceled, j, engines.JobProgress{Batches: 2, Rows: 100})
	if status != "partial" {
		t.Fatal("committed batches were hidden")
	}
	status, _ = worker.Outcome(engines.ErrJobUncertain, j, engines.JobProgress{})
	if status != "uncertain" {
		t.Fatal("ambiguous commit was retried blindly")
	}
	j.Operation = "batch_cancel"
	status, _ = worker.Outcome(errors.New("failure"), j, engines.JobProgress{RevertedBatches: 1})
	if status != "partially_reverted" {
		t.Fatal("partial compensation lost")
	}
	j.Attempt = 5
	status, _ = worker.Outcome(context.DeadlineExceeded, j, engines.JobProgress{})
	if status != "failed" {
		t.Fatal("retry limit ignored")
	}
	if queue.RetryDelay(1) != time.Second || queue.RetryDelay(100) != 64*time.Second {
		t.Fatal("backoff is not bounded")
	}
}
