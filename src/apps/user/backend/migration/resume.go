package migration

import (
	"context"
	"tubes1_wbd/database/dao"
)

func (w *Workflow) ResumeReview(ctx context.Context, actor int64, id string, generation int64) (dao.MigrationReview, error) {
	if !uuidPattern.MatchString(id) {
		return dao.MigrationReview{}, ErrInvalid
	}
	original, _, err := w.store.ReviewForJob(ctx, id)
	if err != nil {
		return dao.MigrationReview{}, err
	}
	if original.Action != "up" && original.Action != "forward" {
		return dao.MigrationReview{}, ErrWorkflowConflict
	}
	return w.SubmitReview(ctx, actor, ReviewInput{RevisionID: original.RevisionID, Action: original.Action, TargetExecutionID: original.TargetExecutionID, TargetJobID: &id, ExpectedGeneration: generation})
}

func (w *Workflow) resumption(ctx context.Context, tx *dao.Store, r dao.MigrationReview, revision dao.MigrationRevision) (RecoverySource, error) {
	if w.options.Recovery == nil {
		return RecoverySource{}, ErrWorkflowUnavailable
	}
	if r.TargetJobID == nil {
		return RecoverySource{}, ErrWorkflowConflict
	}
	original, status, err := tx.ReviewForJob(ctx, *r.TargetJobID)
	if err != nil {
		return RecoverySource{}, ErrWorkflowConflict
	}
	if status != "partial" || original.RevisionID != revision.ID || original.DatabaseID != r.DatabaseID || original.Action != r.Action ||
		!allApproved(original) || original.ExpectedGeneration != r.ExpectedGeneration || original.ExpectedActiveVersion != r.ExpectedActiveVersion ||
		original.QueuedJobID == nil || *original.QueuedJobID != *r.TargetJobID {
		return RecoverySource{}, ErrWorkflowConflict
	}
	if (original.TargetExecutionID == nil) != (r.TargetExecutionID == nil) || original.TargetExecutionID != nil && *original.TargetExecutionID != *r.TargetExecutionID {
		return RecoverySource{}, ErrWorkflowConflict
	}
	var source RecoverySource
	if provider, ok := w.options.Recovery.(AuthorizedRecoveryProvider); ok {
		source, err = provider.CancellationSourceFor(ctx, r.SubmittedBy, *r.TargetJobID)
	} else {
		source, err = w.options.Recovery.CancellationSource(ctx, *r.TargetJobID)
	}
	if err != nil {
		return source, ErrWorkflowUnavailable
	}
	if source.JobID != *r.TargetJobID || source.DatabaseID != r.DatabaseID || source.RevisionID != revision.ID || source.Fingerprint == "" || !source.Paused || !source.PartiallyCommitted || !source.CanResume {
		return source, ErrWorkflowConflict
	}
	return source, nil
}
