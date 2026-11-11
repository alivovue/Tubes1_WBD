package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"schakker_shared/observability"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	pgengine "tubes1_wbd/backend/migration/engines/postgres"
	"tubes1_wbd/backend/queue"
	"tubes1_wbd/database/dao"
)

var errRetryCommit = errors.New("commit receipt is absent after reconciliation")
var errAttemptsExhausted = errors.New("migration retry limit reached")
var errReconcileOnly = errors.New("receipt absent; reconciliation does not execute SQL")

type ExecuteFunc func(context.Context, *dbmanagement.Target, engines.JobRequest, engines.JobHooks) (engines.JobReceipt, error)
type Runner struct {
	Store                           *dao.Store
	Workflow                        *migration.Workflow
	Databases                       *dbmanagement.Service
	Owner                           string
	Concurrency                     int
	Lease, PollInterval, JobTimeout time.Duration
	Execute                         ExecuteFunc
}

func New(store *dao.Store, workflow *migration.Workflow, databases *dbmanagement.Service, owner string) *Runner {
	return &Runner{Store: store, Workflow: workflow, Databases: databases, Owner: owner, Concurrency: 2, Lease: 30 * time.Second, PollInterval: 250 * time.Millisecond, JobTimeout: 30 * time.Minute}
}
func (r *Runner) validate() error {
	if r.Store == nil || r.Workflow == nil || r.Databases == nil || r.Owner == "" || r.Concurrency < 1 || r.Concurrency > 32 || r.Lease < 3*time.Second || r.PollInterval <= 0 || r.JobTimeout <= 0 {
		return dao.ErrInvalid
	}
	return nil
}
func (r *Runner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	var group sync.WaitGroup
	for i := 0; i < r.Concurrency; i++ {
		group.Go(func() {
			for ctx.Err() == nil {
				_, err := r.RunOnce(ctx)
				if err != nil && !errors.Is(err, dao.ErrNotFound) && ctx.Err() == nil {
					observability.Event(ctx, "worker.poll.failed", err)
				}
				timer := time.NewTimer(r.PollInterval)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		})
	}
	group.Wait()
	return nil
}
func (r *Runner) RunOnce(ctx context.Context) (claimed bool, err error) {
	if err = r.validate(); err != nil {
		return false, err
	}
	j, err := r.Store.ClaimJob(ctx, r.Owner, r.Lease)
	if err != nil {
		return false, err
	}
	claimed = true
	ctx = observability.Extract(ctx, j.TraceContext)
	ctx = observability.WithActor(ctx, j.ActorID, j.DatabaseID)
	ctx, span := observability.Start(ctx, "worker.execute")
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, r.JobTimeout)
	defer cancel()
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(r.Lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				hbCtx, stop := context.WithTimeout(ctx, r.Lease/3)
				err := r.Store.HeartbeatJob(hbCtx, j, r.Lease)
				stop()
				if err != nil {
					observability.Event(ctx, "worker.lease.lost", err, "job_id", j.ID)
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(stopHeartbeat); <-heartbeatDone }()
	observability.Event(ctx, "worker.claimed", nil, "job_id", j.ID, "database_id", j.DatabaseID, "actor_id", j.ActorID, "attempt", j.Attempt)
	var progress engines.JobProgress
	targetReconciled := false
	lockCtx, lockSpan := observability.Start(ctx, "worker.database_lock")
	err = r.Store.WithDatabaseLock(lockCtx, j.DatabaseID, func(locked *dao.Store) error {
		policy := r.Workflow.UsingStore(locked)
		request, err := loadRequest(ctx, locked, j)
		if err != nil {
			return err
		}
		previous, err := locked.BatchProgress(ctx, j.ID)
		if err != nil {
			return err
		}
		progress = engines.JobProgress{Batches: previous.Batches, Rows: previous.Rows, Cursor: previous.Cursor, State: previous.State, RevertedBatches: previous.RevertedBatches, RevertedRows: previous.RevertedRows}
		guard := func(check context.Context) error {
			if err := locked.OwnJob(check, j); err != nil {
				return err
			}
			if j.ErrorClass == "reconcile_only" {
				return errReconcileOnly
			}
			if j.Attempt > j.MaxAttempts {
				return errAttemptsExhausted
			}
			plan, err := policy.ValidateApprovedExecution(check, j.ReviewID, j.ID)
			if err != nil {
				return err
			}
			if plan.ActionHash != j.ActionHash || plan.SQLHash != j.SQLHash || plan.SQL != request.SQL || plan.Operation != j.Operation {
				return migration.ErrWorkflowConflict
			}
			return nil
		}
		connectionActor := j.ActorID
		if j.ErrorClass == "reconcile_only" {
			value, err := strconv.ParseInt(j.TraceContext["recovery_actor_id"], 10, 64)
			if err != nil || value < 1 {
				return dao.ErrInvalid
			}
			connectionActor = value
		}
		target, err := r.Databases.OpenMigrationTarget(ctx, connectionActor, j.DatabaseID)
		if err != nil {
			return err
		}
		defer target.DB.Close()
		if target.Adapter.Name() != "postgresql" {
			return adapters.ErrUnsupported
		}
		hooks := engines.JobHooks{Guard: guard, Reconciled: func() { targetReconciled = true }, Progress: func(check context.Context, p engines.JobProgress) error {
			progress = p
			total := p.Total
			return locked.SaveBatchProgress(check, j, dao.BatchProgress{Batches: p.Batches, Rows: p.Rows, Cursor: p.Cursor, Total: &total, State: p.State, RevertedBatches: p.RevertedBatches, RevertedRows: p.RevertedRows})
		}}
		var receipt engines.JobReceipt
		if r.Execute != nil {
			receipt, err = r.Execute(ctx, &target, request, hooks)
		} else {
			receipt, err = pgengine.RunJob(ctx, target.DB, target.Adapter, request, hooks)
		}
		if receipt.Progress.Batches > 0 || progress.Batches == 0 {
			progress = receipt.Progress
		}
		if errors.Is(err, engines.ErrJobUncertain) {
			probe, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer stop()
			found, reconciled, readErr := reconcile(probe, &target, request)
			if readErr != nil {
				return engines.ErrJobUncertain
			}
			if !found {
				return errRetryCommit
			}
			receipt = reconciled
			progress = receipt.Progress
			err = nil
		}
		if err != nil {
			return err
		}
		if receipt.JobID != j.ID || receipt.ExecutionID != j.ExecutionID || receipt.SQLHash != j.SQLHash || receipt.ActionHash != j.ActionHash || receipt.CommittedAt.IsZero() {
			return engines.ErrJobUncertain
		}
		if progress.Batches > 0 {
			if err = hooks.Progress(ctx, progress); err != nil {
				return engines.ErrJobUncertain
			}
		}
		_, err = policy.RecordCommittedExecution(ctx, migration.VerifiedCommit{ReviewID: j.ReviewID, ExecutionID: j.ExecutionID, JobID: j.ID, ActualSQL: request.SQL, TargetReceipt: receipt.Proof(), CommittedAt: receipt.CommittedAt})
		if err != nil {
			return engines.ErrJobUncertain
		}
		return nil
	})
	lockSpan.End()
	if err != nil && !targetReconciled && (j.ErrorClass == "lease_recovery" || j.ErrorClass == "reconcile_only") {
		err = engines.ErrJobUncertain
	}
	status, class := Outcome(err, j, progress)
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if progress.Batches > 0 {
		total := progress.Total
		if projectionErr := r.Store.SaveBatchProgress(cleanup, j, dao.BatchProgress{Batches: progress.Batches, Rows: progress.Rows, Cursor: progress.Cursor, Total: &total, State: progress.State, RevertedBatches: progress.RevertedBatches, RevertedRows: progress.RevertedRows}); projectionErr != nil {
			return true, projectionErr
		}
	}
	finishErr := r.Store.FinishJob(cleanup, j, status, class, queue.RetryDelay(j.Attempt))
	observability.Event(ctx, "worker.finished", err, "job_id", j.ID, "execution_id", j.ExecutionID, "database_id", j.DatabaseID, "attempt", j.Attempt, "job_status", status, "error_class", class, "committed_batches", progress.Batches, "committed_rows", progress.Rows)
	if finishErr != nil {
		return true, finishErr
	}
	return true, err
}
func loadRequest(ctx context.Context, store *dao.Store, j dao.MigrationJob) (engines.JobRequest, error) {
	review, err := store.Review(ctx, j.ReviewID, false)
	if err != nil {
		return engines.JobRequest{}, err
	}
	revision, err := store.Revision(ctx, review.RevisionID)
	if err != nil {
		return engines.JobRequest{}, err
	}
	sql := revision.UpSQL
	if j.Operation == "rollback" || j.Operation == "batch_cancel" {
		sql = revision.DownSQL
	}
	if review.QueuedJobID == nil || *review.QueuedJobID != j.ID || review.DatabaseID != j.DatabaseID || review.SubmittedBy != j.ActorID || review.Action != j.Operation || review.ActionHash != j.ActionHash || engines.SQLDigest(sql) != j.SQLHash {
		return engines.JobRequest{}, migration.ErrWorkflowConflict
	}
	batch, err := engines.ParseBatch(revision.UpSQL, revision.DownSQL)
	if err != nil {
		return engines.JobRequest{}, err
	}
	var candidate [24]byte
	if _, err = rand.Read(candidate[:]); err != nil {
		return engines.JobRequest{}, err
	}
	namespace, err := store.DatabaseNamespace(ctx, j.DatabaseID, hex.EncodeToString(candidate[:]))
	if err != nil {
		return engines.JobRequest{}, err
	}
	out := engines.JobRequest{Namespace: namespace, JobID: j.ID, ExecutionID: j.ExecutionID, Operation: j.Operation, SQL: sql, DownSQL: revision.DownSQL, SQLHash: j.SQLHash, ActionHash: j.ActionHash, Batch: batch}
	if review.TargetJobID != nil {
		out.OriginalJobID = *review.TargetJobID
	}
	if batch != nil && j.Operation == "rollback" {
		if review.TargetExecutionID == nil {
			return out, migration.ErrWorkflowConflict
		}
		original, err := store.CommittedExecution(ctx, *review.TargetExecutionID)
		if err != nil {
			return out, err
		}
		out.OriginalJobID = original.JobID
	}
	return out, nil
}
func reconcile(ctx context.Context, target *dbmanagement.Target, request engines.JobRequest) (bool, engines.JobReceipt, error) {
	var receipt engines.JobReceipt
	tx, err := target.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, receipt, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(73421108)`); err != nil {
		return false, receipt, err
	}
	found, err := pgengine.ReadJobReceipt(ctx, tx, request, &receipt)
	return found, receipt, err
}
func Retryable(err error) bool {
	if errors.Is(err, errRetryCommit) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, migration.ErrWorkflowUnavailable) || errors.Is(err, adapters.ErrConnection) {
		return true
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "40001", "40P01", "55P03", "53300", "57P01", "57P02", "57P03":
			return true
		}
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}
func Outcome(err error, j dao.MigrationJob, p engines.JobProgress) (status, class string) {
	if err == nil {
		if j.Operation == "batch_cancel" {
			return "reverted", ""
		}
		return "succeeded", ""
	}
	if errors.Is(err, engines.ErrJobUncertain) {
		return "uncertain", "commit_unconfirmed"
	}
	if j.Operation == "batch_cancel" && p.RevertedBatches > 0 {
		if Retryable(err) && j.Attempt < j.MaxAttempts {
			return "partially_reverted", "temporary_failure"
		}
		return "partially_reverted", "compensation_incomplete"
	}
	if p.Batches > 0 {
		if Retryable(err) && j.Attempt < j.MaxAttempts {
			return "partial", "temporary_failure"
		}
		return "partial", "batch_incomplete"
	}
	if Retryable(err) && j.Attempt < j.MaxAttempts {
		return "pending", "temporary_failure"
	}
	return "failed", "execution_rejected"
}
