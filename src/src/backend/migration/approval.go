package migration

import (
	"context"
	"errors"
	"strings"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"
)

type ReviewInput struct {
	RevisionID         int64   `json:"revision_id"`
	Action             string  `json:"action"`
	TargetExecutionID  *string `json:"target_execution_id"`
	TargetJobID        *string `json:"target_job_id"`
	ExpectedGeneration int64   `json:"expected_generation"`
}

func (w *Workflow) SubmitReview(ctx context.Context, actor int64, in ReviewInput) (out dao.MigrationReview, err error) {
	ctx, end := w.observe(ctx, "migration.review.submit", actor, 0)
	defer func() { end(err) }()
	if in.RevisionID < 1 || in.ExpectedGeneration < 0 {
		return out, ErrInvalid
	}
	revision, err := w.store.Revision(ctx, in.RevisionID)
	if err != nil {
		return out, err
	}
	if err = w.auth.RequirePermission(ctx, actor, revision.DatabaseID, access.ViewEdit); err != nil {
		return out, err
	}
	if in.Action != "up" && in.Action != "rollback" && in.Action != "forward" && in.Action != "batch_cancel" {
		return out, ErrInvalid
	}
	if ((in.Action == "rollback" || in.Action == "forward") != (in.TargetExecutionID != nil)) || ((in.Action == "batch_cancel") != (in.TargetJobID != nil)) {
		return out, ErrInvalid
	}
	if (in.TargetExecutionID != nil && !uuidPattern.MatchString(*in.TargetExecutionID)) || (in.TargetJobID != nil && !uuidPattern.MatchString(*in.TargetJobID)) {
		return out, ErrInvalid
	}
	snapshot, err := w.snapshot(ctx, actor, revision.DatabaseID)
	if err != nil {
		return out, err
	}
	err = w.store.Transaction(ctx, func(tx *dao.Store) error {
		state, err := tx.MigrationState(ctx, revision.DatabaseID, true)
		if err != nil {
			return err
		}
		if state.Generation != in.ExpectedGeneration {
			return ErrWorkflowConflict
		}
		if err := tx.LockDefinition(ctx, revision.DefinitionID); err != nil {
			return err
		}
		latest, err := tx.LatestRevision(ctx, revision.DefinitionID)
		if err != nil {
			return err
		}
		if latest.ID != revision.ID {
			return ErrWorkflowConflict
		}
		r := dao.MigrationReview{RevisionID: revision.ID, DatabaseID: revision.DatabaseID, Action: in.Action, TargetExecutionID: in.TargetExecutionID, TargetJobID: in.TargetJobID, ExpectedGeneration: state.Generation, ExpectedActiveVersion: state.ActiveVersion, AuthorityRevision: snapshot.Revision, AuthoritySnapshot: snapshot, ConnectionFingerprint: snapshot.ConnectionFingerprint, SubmittedBy: actor}
		if in.Action == "batch_cancel" {
			source, err := w.cancellation(ctx, tx, r, revision)
			if err != nil {
				return err
			}
			r.RecoveryFingerprint = source.Fingerprint
		}
		if err = w.checkAction(ctx, tx, r, revision, state); err != nil {
			return err
		}
		pending, err := tx.UnfinishedExecutions(ctx, revision.DatabaseID)
		if err != nil {
			return err
		}
		if len(pending) > 0 {
			return ErrWorkflowConflict
		}
		if err := tx.FreezeRevision(ctx, revision.ID); err != nil {
			return err
		}
		r.ActionHash = actionHash(r, revision)
		out, err = tx.CreateReview(ctx, r)
		return err
	})
	return out, err
}
func (w *Workflow) Reviews(ctx context.Context, actor, database int64, limit, offset int) ([]dao.MigrationReview, error) {
	if err := w.auth.RequirePermission(ctx, actor, database, access.ViewOnly); err != nil {
		return nil, err
	}
	limit, err := validPage(limit, offset)
	if err != nil {
		return nil, err
	}
	return w.store.Reviews(ctx, database, limit, offset)
}
func (w *Workflow) Review(ctx context.Context, actor, id int64) (dao.MigrationReview, error) {
	r, err := w.store.Review(ctx, id, false)
	if err != nil {
		return r, err
	}
	if err = w.auth.RequirePermission(ctx, actor, r.DatabaseID, access.ViewOnly); err != nil {
		return dao.MigrationReview{}, err
	}
	return r, nil
}
func (w *Workflow) Decide(ctx context.Context, actor, id, group int64, decision, note string) (out dao.MigrationReview, err error) {
	ctx, end := w.observe(ctx, "migration.review.decide", actor, 0)
	defer func() { end(err) }()
	if group < 1 || (decision != "approve" && decision != "reject") || len(note) > 2000 || (decision == "reject" && strings.TrimSpace(note) == "") {
		return out, ErrInvalid
	}
	original, err := w.store.Review(ctx, id, false)
	if err != nil {
		return out, err
	}
	if err = w.auth.RequirePermission(ctx, actor, original.DatabaseID, access.ViewOnly); err != nil {
		return out, err
	}
	err = w.withReview(ctx, id, func(tx *dao.Store, r dao.MigrationReview, _ dao.MigrationRevision, _ dao.MigrationState) error {
		if r.State != "pending_review" || r.QueuedJobID != nil {
			return ErrWorkflowConflict
		}
		leader := false
		for _, g := range r.AuthoritySnapshot.Groups {
			if g.ID == group && g.LeaderID == actor {
				leader = true
			}
		}
		if !leader {
			return ErrForbidden
		}
		if err := tx.AddDecision(ctx, id, dao.MigrationDecision{GroupID: group, ApproverID: actor, Decision: decision, Note: note}); err != nil {
			return err
		}
		r, err := tx.Review(ctx, id, false)
		if err != nil {
			return err
		}
		if decision == "reject" {
			if err = tx.SetReviewState(ctx, id, "rejected"); err != nil {
				return err
			}
		} else if allApproved(r) {
			if err = tx.SetReviewState(ctx, id, "approved"); err != nil {
				return err
			}
		}
		out, err = tx.Review(ctx, id, false)
		return err
	})
	return out, err
}
func (w *Workflow) PrepareApprovedAction(ctx context.Context, actor, id int64) (out ApprovedPlan, err error) {
	ctx, end := w.observe(ctx, "migration.approval.revalidate", actor, 0)
	defer func() { end(err) }()
	r, err := w.store.Review(ctx, id, false)
	if err != nil {
		return out, err
	}
	if err = w.auth.RequirePermission(ctx, actor, r.DatabaseID, access.ViewEdit); err != nil {
		return out, err
	}
	err = w.withReview(ctx, id, func(_ *dao.Store, r dao.MigrationReview, revision dao.MigrationRevision, _ dao.MigrationState) error {
		if r.State != "approved" || !allApproved(r) {
			return ErrWorkflowConflict
		}
		out = planFor(r, revision)
		return nil
	})
	return out, err
}

// ValidateApprovedExecution must be called by P3 while holding WithDatabaseLock,
// immediately before target SQL. The job ID binds the exact reserved review.
func (w *Workflow) ValidateApprovedExecution(ctx context.Context, reviewID int64, jobID string) (out ApprovedPlan, err error) {
	r, err := w.store.Review(ctx, reviewID, false)
	if err != nil {
		return out, err
	}
	if r.QueuedJobID == nil || *r.QueuedJobID != jobID {
		return out, ErrWorkflowConflict
	}
	return w.PrepareApprovedAction(ctx, r.SubmittedBy, reviewID)
}
func (w *Workflow) Enqueue(ctx context.Context, actor, id int64, key string) (job string, err error) {
	ctx, end := w.observe(ctx, "migration.review.enqueue", actor, 0)
	defer func() { end(err) }()
	if len(strings.TrimSpace(key)) < 1 || len(key) > 200 {
		return "", ErrInvalid
	}
	r, err := w.store.Review(ctx, id, false)
	if err != nil {
		return "", err
	}
	if err = w.auth.RequirePermission(ctx, actor, r.DatabaseID, access.ViewEdit); err != nil {
		return "", err
	}
	// Replays return the original job even after a successful commit has moved the head.
	if r.QueuedJobID != nil {
		if r.IdempotencyKey == nil || *r.IdempotencyKey != key {
			return "", ErrWorkflowConflict
		}
		return *r.QueuedJobID, nil
	}
	if w.options.Dispatcher == nil {
		return "", ErrWorkflowUnavailable
	}
	err = w.withReview(ctx, id, func(tx *dao.Store, r dao.MigrationReview, revision dao.MigrationRevision, _ dao.MigrationState) error {
		if r.State != "approved" || !allApproved(r) {
			return ErrWorkflowConflict
		}
		if r.QueuedJobID != nil {
			if r.IdempotencyKey == nil || *r.IdempotencyKey != key {
				return ErrWorkflowConflict
			}
			job = *r.QueuedJobID
			return nil
		}
		var err error
		job, err = w.options.Dispatcher.Enqueue(ctx, tx, planFor(r, revision), key)
		if err != nil {
			if errors.Is(err, dao.ErrConflict) {
				return ErrWorkflowConflict
			}
			return ErrWorkflowUnavailable
		}
		if !uuidPattern.MatchString(job) {
			return ErrWorkflowUnavailable
		}
		return tx.MarkReviewQueued(ctx, id, job, key)
	})
	return job, err
}
