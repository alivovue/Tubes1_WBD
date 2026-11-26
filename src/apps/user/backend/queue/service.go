package queue

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"time"

	"schakker_shared/observability"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	"tubes1_wbd/database/dao"
)

var validID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Service struct {
	Store    *dao.Store
	Access   *access.Service
	Workflow *migration.Workflow
}

func (s *Service) Options(recovery migration.RecoveryProvider) migration.WorkflowOptions {
	return migration.WorkflowOptions{Dispatcher: s, Recovery: recovery, Observer: observability.Observer{}, Logger: slog.Default()}
}
func (s *Service) Enqueue(ctx context.Context, tx *dao.Store, plan migration.ApprovedPlan, key string) (string, error) {
	ctx, span := observability.Start(ctx, "queue.publish")
	defer span.End()
	if plan.SQLHash != migration.SQLHash(plan.SQL) || plan.ActionHash == "" {
		return "", dao.ErrInvalid
	}
	revision, err := tx.Revision(ctx, plan.RevisionID)
	if err != nil {
		return "", err
	}
	if _, err = engines.ParseBatch(revision.UpSQL, revision.DownSQL); err != nil {
		return "", err
	}
	id, err := tx.InsertJob(ctx, plan.ReviewID, plan.DatabaseID, plan.ActorID, plan.Operation, key, plan.ActionHash, plan.SQLHash, observability.Inject(ctx))
	if err == nil && plan.Operation == "batch_cancel" {
		if plan.TargetJobID == nil {
			return "", dao.ErrInvalid
		}
		err = tx.ReserveCancellation(ctx, *plan.TargetJobID, id)
	}
	observability.Event(ctx, "queue.published", err, "job_id", id, "review_id", plan.ReviewID, "actor_id", plan.ActorID, "database_id", plan.DatabaseID)
	return id, err
}
func (s *Service) Status(ctx context.Context, actor int64, id string) (dao.MigrationJob, dao.BatchProgress, error) {
	if !validID.MatchString(id) {
		return dao.MigrationJob{}, dao.BatchProgress{}, dao.ErrInvalid
	}
	j, err := s.Store.Job(ctx, id)
	if err != nil {
		return j, dao.BatchProgress{}, err
	}
	if err = s.Access.RequirePermission(ctx, actor, j.DatabaseID, access.ViewOnly); err != nil {
		return dao.MigrationJob{}, dao.BatchProgress{}, err
	}
	p, err := s.Store.BatchProgress(ctx, id)
	return j, p, err
}
func (s *Service) Retry(ctx context.Context, actor int64, id string) (dao.MigrationJob, error) {
	j, _, err := s.Status(ctx, actor, id)
	if err != nil {
		return j, err
	}
	if err = s.Access.RequirePermission(ctx, actor, j.DatabaseID, access.ViewEdit); err != nil {
		return j, err
	}
	if actor != j.ActorID && j.Status != "uncertain" {
		return j, access.ErrForbidden
	}
	err = s.Store.WithDatabaseLock(ctx, j.DatabaseID, func(locked *dao.Store) error {
		if j.Status == "uncertain" {
			return locked.RetryJob(ctx, j.ID, actor)
		}
		if _, err := s.Workflow.UsingStore(locked).ValidateApprovedExecution(ctx, j.ReviewID, j.ID); err != nil {
			return err
		}
		return locked.RetryJob(ctx, j.ID)
	})
	observability.Event(ctx, "queue.retry_requested", err, "actor_id", actor, "job_id", j.ID, "database_id", j.DatabaseID)
	if err != nil {
		return j, err
	}
	return s.Store.Job(ctx, id)
}
func RetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 7 {
		attempt = 7
	}
	return time.Second * time.Duration(1<<uint(attempt-1))
}
func Conflict(err error) bool {
	return errors.Is(err, dao.ErrConflict) || errors.Is(err, dao.ErrJobLeaseLost)
}
