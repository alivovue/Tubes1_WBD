package migration

import (
	"context"
	"errors"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"
)

// VerifiedCommit is P3's receipt-to-history handoff, never an HTTP request DTO.
// P3 verifies a committed target receipt before calling this method, including on
// crash recovery. A failed/ambiguous SQL attempt must never call it.
type VerifiedCommit struct {
	ReviewID                                     int64
	ExecutionID, JobID, ActualSQL, TargetReceipt string
	CommittedAt                                  time.Time
}

func (w *Workflow) RecordCommittedExecution(ctx context.Context, proof VerifiedCommit) (out dao.CommittedExecution, err error) {
	ctx, end := w.observe(ctx, "migration.history.commit", 0, 0)
	defer func() { end(err) }()
	if proof.ReviewID < 1 || !uuidPattern.MatchString(proof.ExecutionID) || !uuidPattern.MatchString(proof.JobID) || proof.TargetReceipt == "" || proof.CommittedAt.IsZero() {
		return out, ErrInvalid
	}
	proof.CommittedAt = proof.CommittedAt.UTC().Truncate(time.Microsecond)
	original, err := w.store.Review(ctx, proof.ReviewID, false)
	if err != nil {
		return out, err
	}
	err = w.store.Transaction(ctx, func(tx *dao.Store) error {
		state, err := tx.MigrationState(ctx, original.DatabaseID, true)
		if err != nil {
			return err
		}
		previous, lookup := tx.CommittedExecution(ctx, proof.ExecutionID)
		if lookup == nil {
			if previous.JobID != proof.JobID || previous.ReviewID == nil || *previous.ReviewID != proof.ReviewID || previous.SQL != proof.ActualSQL || previous.TargetReceipt != proof.TargetReceipt || !previous.CommittedAt.Equal(proof.CommittedAt) {
				return ErrWorkflowConflict
			}
			out = previous
			return nil
		}
		if !errors.Is(lookup, dao.ErrNotFound) {
			return lookup
		}
		r, err := tx.Review(ctx, proof.ReviewID, true)
		if err != nil {
			return err
		}
		revision, err := tx.Revision(ctx, r.RevisionID)
		if err != nil {
			return err
		}
		if r.QueuedJobID == nil || *r.QueuedJobID != proof.JobID || !allApproved(r) || r.ActionHash != actionHash(r, revision) {
			return ErrWorkflowConflict
		}
		plan := planFor(r, revision)
		if proof.ActualSQL != plan.SQL {
			return ErrWorkflowConflict
		}
		// Persist committed facts even if authority changed AFTER target commit.
		// No network lookups or target SQL occur during this metadata transaction.
		if err = w.checkPosition(ctx, tx, r, revision, state); err != nil {
			return err
		}
		version := state.ActiveVersion + 1
		switch r.Action {
		case "up":
			state.ActiveStack = append(state.ActiveStack, proof.ExecutionID)
			state.ForwardStack = []string{}
			state.ActiveVersion++
			if err = tx.SetDefinitionVersion(ctx, revision.DefinitionID, version); err != nil {
				return err
			}
		case "rollback":
			version = state.ActiveVersion
			state.ActiveStack = state.ActiveStack[:len(state.ActiveStack)-1]
			state.ForwardStack = append(state.ForwardStack, *r.TargetExecutionID)
			state.ActiveVersion--
		case "forward":
			state.ForwardStack = state.ForwardStack[:len(state.ForwardStack)-1]
			state.ActiveStack = append(state.ActiveStack, proof.ExecutionID)
			state.ActiveVersion++
		case "batch_cancel":
			state.BlockedReason = ""
		}
		state.Generation++
		out = dao.CommittedExecution{ExecutionID: proof.ExecutionID, JobID: proof.JobID, ReviewID: &r.ID, DefinitionID: revision.DefinitionID, RevisionID: revision.ID, DatabaseID: r.DatabaseID, LogicalVersion: version, ActorID: r.SubmittedBy, Operation: r.Action, SQL: proof.ActualSQL, CommittedAt: proof.CommittedAt, TargetExecutionID: r.TargetExecutionID, TargetReceipt: proof.TargetReceipt, Status: "succeeded"}
		if r.Action == "rollback" {
			out.ReversesExecutionID = r.TargetExecutionID
		}
		if err = tx.AppendCommittedExecution(ctx, out); err != nil {
			return err
		}
		if err = tx.SaveMigrationState(ctx, state); err != nil {
			return err
		}
		out, err = tx.CommittedExecution(ctx, proof.ExecutionID)
		return err
	})
	return out, err
}
func (w *Workflow) History(ctx context.Context, actor, database int64, hide bool, limit, offset int) ([]dao.CommittedExecution, error) {
	if err := w.auth.RequirePermission(ctx, actor, database, access.ViewOnly); err != nil {
		return nil, err
	}
	limit, err := validPage(limit, offset)
	if err != nil {
		return nil, err
	}
	return w.store.CommittedHistory(ctx, database, hide, limit, offset)
}
func (w *Workflow) Activities(ctx context.Context, actor, database int64, limit, offset int) ([]Activity, error) {
	if err := w.auth.RequirePermission(ctx, actor, database, access.ViewOnly); err != nil {
		return nil, err
	}
	limit, err := validPage(limit, offset)
	if err != nil {
		return nil, err
	}
	if w.options.Activities == nil {
		return w.store.MigrationActivities(ctx, database, limit, offset)
	}
	return w.options.Activities.MigrationActivities(ctx, database, limit, offset)
}
