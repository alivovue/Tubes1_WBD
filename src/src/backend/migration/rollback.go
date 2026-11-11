package migration

import (
	"context"
	"fmt"

	"tubes1_wbd/database/dao"
)

var errRecoveryScopeChanged = fmt.Errorf("%w: recovery scope changed", ErrWorkflowConflict)

func (w *Workflow) cancellation(ctx context.Context, tx *dao.Store, r dao.MigrationReview, revision dao.MigrationRevision) (RecoverySource, error) {
	if w.options.Recovery == nil {
		return RecoverySource{}, ErrWorkflowUnavailable
	}
	if revision.Irreversible || revision.DownSQL == "" || r.TargetJobID == nil {
		return RecoverySource{}, ErrWorkflowConflict
	}
	original, status, err := tx.ReviewForJob(ctx, *r.TargetJobID)
	if err != nil {
		return RecoverySource{}, ErrWorkflowConflict
	}
	if original.RevisionID != revision.ID || original.DatabaseID != revision.DatabaseID || original.QueuedJobID == nil || *original.QueuedJobID != *r.TargetJobID || !allApproved(original) || (original.Action != "up" && original.Action != "forward") || (status != "partial" && status != "uncertain" && status != "partially_reverted") {
		return RecoverySource{}, ErrWorkflowConflict
	}
	source, err := w.options.Recovery.CancellationSource(ctx, *r.TargetJobID)
	if err != nil {
		return source, ErrWorkflowUnavailable
	}
	if source.JobID != *r.TargetJobID || source.RevisionID != revision.ID || source.DatabaseID != revision.DatabaseID || source.Fingerprint == "" || !source.Paused || !source.PartiallyCommitted || !source.SafeCompensation {
		return source, ErrWorkflowConflict
	}
	return source, nil
}
func (w *Workflow) checkAction(ctx context.Context, tx *dao.Store, r dao.MigrationReview, revision dao.MigrationRevision, state dao.MigrationState) error {
	if state.BlockedReason != "" && r.Action != "batch_cancel" {
		return ErrWorkflowConflict
	}
	if r.Action != "batch_cancel" {
		blocked, err := tx.BlockingWorkflowJob(ctx, r.DatabaseID, r.QueuedJobID)
		if err != nil {
			return err
		}
		if blocked {
			return ErrWorkflowConflict
		}
	}
	if r.Action == "batch_cancel" {
		source, err := w.cancellation(ctx, tx, r, revision)
		if err != nil {
			return err
		}
		if source.Fingerprint != r.RecoveryFingerprint {
			return errRecoveryScopeChanged
		}
	}
	return w.checkPosition(ctx, tx, r, revision, state)
}
func (w *Workflow) checkPosition(ctx context.Context, tx *dao.Store, r dao.MigrationReview, revision dao.MigrationRevision, state dao.MigrationState) error {
	if r.ExpectedGeneration != state.Generation || r.ExpectedActiveVersion != state.ActiveVersion || revision.DatabaseID != r.DatabaseID {
		return ErrWorkflowConflict
	}
	switch r.Action {
	case "up":
		if revision.LogicalVersion != nil {
			return ErrWorkflowConflict
		}
	case "rollback":
		if revision.Irreversible || revision.DownSQL == "" || r.TargetExecutionID == nil || len(state.ActiveStack) == 0 || state.ActiveStack[len(state.ActiveStack)-1] != *r.TargetExecutionID {
			return ErrWorkflowConflict
		}
		target, err := tx.CommittedExecution(ctx, *r.TargetExecutionID)
		if err != nil {
			return err
		}
		if target.RevisionID != revision.ID || target.DatabaseID != r.DatabaseID || (target.Operation != "up" && target.Operation != "forward") {
			return ErrWorkflowConflict
		}
	case "forward":
		if r.TargetExecutionID == nil || len(state.ForwardStack) == 0 || state.ForwardStack[len(state.ForwardStack)-1] != *r.TargetExecutionID {
			return ErrWorkflowConflict
		}
		target, err := tx.CommittedExecution(ctx, *r.TargetExecutionID)
		if err != nil {
			return err
		}
		if target.RevisionID != revision.ID || target.DatabaseID != r.DatabaseID || target.LogicalVersion != state.ActiveVersion+1 {
			return ErrWorkflowConflict
		}
	case "batch_cancel":
		if revision.Irreversible || revision.DownSQL == "" || r.TargetJobID == nil {
			return ErrWorkflowConflict
		}
	default:
		return ErrInvalid
	}
	return nil
}
func (w *Workflow) RollbackReview(ctx context.Context, actor int64, execution string, generation int64) (dao.MigrationReview, error) {
	if !uuidPattern.MatchString(execution) {
		return dao.MigrationReview{}, ErrInvalid
	}
	e, err := w.store.CommittedExecution(ctx, execution)
	if err != nil {
		return dao.MigrationReview{}, err
	}
	return w.SubmitReview(ctx, actor, ReviewInput{RevisionID: e.RevisionID, Action: "rollback", TargetExecutionID: &execution, ExpectedGeneration: generation})
}
