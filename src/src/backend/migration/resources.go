package migration

// Resource projections keep persistence models separate from the SPA contract.
// Capability flags are hints; policy is checked again on every mutation/run.
import (
	"context"
	"errors"
	"strconv"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/migration/engines"
	"tubes1_wbd/database/dao"
)

type Resources struct {
	w         *Workflow
	ctx       context.Context
	actor     int64
	edit      bool
	state     dao.MigrationState
	blocked   bool
	revisions map[int64]dao.MigrationRevision
	names     map[int64]string
}

func (w *Workflow) Resources(ctx context.Context, actor, database int64) (*Resources, error) {
	p, err := w.auth.EffectivePermission(ctx, actor, database)
	if err != nil {
		return nil, err
	}
	if !p.Allows(access.ViewOnly) {
		return nil, ErrForbidden
	}
	state, err := w.store.MigrationState(ctx, database, false)
	if err != nil {
		return nil, err
	}
	blocked, err := w.store.BlockingWorkflowJob(ctx, database, nil)
	if err != nil {
		return nil, err
	}
	return &Resources{w: w, ctx: ctx, actor: actor, edit: p.Allows(access.ViewEdit), state: state, blocked: blocked || state.BlockedReason != "", revisions: map[int64]dao.MigrationRevision{}, names: map[int64]string{}}, nil
}

func (v *Resources) name(id int64) string {
	if name, ok := v.names[id]; ok {
		return name
	}
	name := "User #" + strconv.FormatInt(id, 10)
	if user, err := v.w.store.User(v.ctx, id); err == nil {
		name = user.Username
	}
	v.names[id] = name
	return name
}
func (v *Resources) revision(id int64) (dao.MigrationRevision, error) {
	if r, ok := v.revisions[id]; ok {
		return r, nil
	}
	r, err := v.w.store.Revision(v.ctx, id)
	if err == nil {
		v.revisions[id] = r
	}
	return r, err
}
func batchResource(r dao.MigrationRevision) any {
	s, err := engines.ParseBatch(r.UpSQL, r.DownSQL)
	if err != nil || s == nil {
		return nil
	}
	return map[string]any{"template": "update_by_key", "schema": s.Schema, "table": s.Table, "key_column": s.Key, "value_column": s.Column, "delta": s.Delta, "batch_size": s.Size, "expected_rows": s.ExpectedRows}
}
func resourceAction(action string, r dao.MigrationRevision) string {
	if action == "batch_cancel" {
		return "cancel_batch"
	}
	if action == "up" && batchResource(r) != nil {
		return "up_batch"
	}
	return action
}
func (v *Resources) Definition(r dao.MigrationRevision) map[string]any {
	status := "draft"
	if r.FrozenAt != nil {
		status = "submitted"
	}
	if r.LogicalVersion != nil {
		status = "executed"
	}
	return map[string]any{
		"id":                     r.DefinitionID,
		"revision_id":            r.ID,
		"revision":               r.RevisionNo,
		"database_id":            r.DatabaseID,
		"title":                  r.Title,
		"up_sql":                 r.UpSQL,
		"down_sql":               r.DownSQL,
		"irreversible":           r.Irreversible,
		"irreversibility_reason": r.IrreversibleReason,
		"batch":                  batchResource(r),
		"status":                 status,
		"editable":               v.edit && r.FrozenAt == nil && r.LogicalVersion == nil,
		"definition_hash":        r.SQLHash,
		"created_by":             v.name(r.CreatedBy),
		"updated_at":             r.CreatedAt,
		"expected_generation":    v.state.Generation,
	}
}
func (v *Resources) Review(r dao.MigrationReview) (map[string]any, error) {
	revision, err := v.revision(r.RevisionID)
	if err != nil {
		return nil, err
	}
	groups := make([]map[string]any, 0, len(r.AuthoritySnapshot.Groups))
	for _, g := range r.AuthoritySnapshot.Groups {
		group := map[string]any{
			"group_id":       g.ID,
			"group_name":     g.Name,
			"permission":     g.Permission,
			"leader_user_id": g.LeaderID,
			"leader_name":    g.LeaderName,
			"decision":       "pending",
			"can_decide":     g.Valid && g.LeaderID == v.actor && r.State == "pending_review" && r.QueuedJobID == nil,
		}
		for _, d := range r.Decisions {
			if d.GroupID != g.ID {
				continue
			}
			decision := "approved"
			if d.Decision == "reject" {
				decision = "rejected"
			}
			group["decision"], group["reason"], group["decided_by"], group["decided_at"], group["can_decide"] = decision, d.Note, v.name(d.ApproverID), d.DecidedAt, false
		}
		groups = append(groups, group)
	}
	return map[string]any{
		"id":                     r.ID,
		"database_id":            r.DatabaseID,
		"definition_id":          revision.DefinitionID,
		"revision_id":            r.RevisionID,
		"revision":               revision.RevisionNo,
		"title":                  revision.Title,
		"action":                 resourceAction(r.Action, revision),
		"target_execution_id":    r.TargetExecutionID,
		"target_job_id":          r.TargetJobID,
		"status":                 r.State,
		"requested_by":           v.name(r.SubmittedBy),
		"requested_at":           r.SubmittedAt,
		"definition_hash":        revision.SQLHash,
		"authority_revision":     strconv.FormatInt(r.AuthorityRevision, 10),
		"up_sql":                 revision.UpSQL,
		"down_sql":               revision.DownSQL,
		"irreversible":           revision.Irreversible,
		"irreversibility_reason": revision.IrreversibleReason,
		"batch":                  batchResource(revision),
		"required_groups":        groups,
		"can_enqueue":            v.edit && (!v.blocked || r.Action == "batch_cancel") && r.State == "approved" && r.QueuedJobID == nil && allApproved(r),
		"can_resubmit":           v.edit && (r.State == "rejected" || r.State == "invalidated"),
		"job_id":                 r.QueuedJobID,
		"expected_generation":    v.state.Generation,
	}, nil
}

type ExecutionSummary struct {
	ID          string    `json:"id"`
	DatabaseID  int64     `json:"database_id"`
	Title       string    `json:"title"`
	Actor       string    `json:"actor"`
	Operation   string    `json:"operation"`
	CommittedAt time.Time `json:"committed_at"`
	Version     int64     `json:"version"`
}

func (v *Resources) ExecutionSummary(e dao.CommittedExecution) (ExecutionSummary, error) {
	r, err := v.revision(e.RevisionID)
	if err != nil {
		return ExecutionSummary{}, err
	}
	return ExecutionSummary{ID: e.ExecutionID, DatabaseID: e.DatabaseID, Title: r.Title, Actor: v.name(e.ActorID), Operation: resourceAction(e.Operation, r), CommittedAt: e.CommittedAt, Version: e.LogicalVersion}, nil
}

func (v *Resources) Execution(e dao.CommittedExecution) (map[string]any, error) {
	summary, err := v.ExecutionSummary(e)
	if err != nil {
		return nil, err
	}
	r, err := v.revision(e.RevisionID)
	if err != nil {
		return nil, err
	}
	rollback := v.edit && !v.blocked && !r.Irreversible && len(v.state.ActiveStack) > 0 && v.state.ActiveStack[len(v.state.ActiveStack)-1] == e.ExecutionID
	forward := v.edit && !v.blocked && len(v.state.ForwardStack) > 0 && v.state.ForwardStack[len(v.state.ForwardStack)-1] == e.ExecutionID
	var duration *int64
	if j, err := v.w.store.Job(v.ctx, e.JobID); err == nil {
		start, _, err := v.w.store.JobTimes(v.ctx, j.ID)
		if err != nil {
			return nil, err
		}
		if start != nil {
			ms := e.CommittedAt.Sub(*start).Milliseconds()
			if ms < 0 {
				ms = 0
			}
			duration = &ms
		}
	} else if !errors.Is(err, dao.ErrNotFound) {
		return nil, err
	}
	return map[string]any{
		"id":                     summary.ID,
		"database_id":            summary.DatabaseID,
		"definition_id":          e.DefinitionID,
		"revision_id":            e.RevisionID,
		"revision":               r.RevisionNo,
		"version":                summary.Version,
		"title":                  summary.Title,
		"operation":              summary.Operation,
		"sql":                    e.SQL,
		"actor":                  summary.Actor,
		"committed_at":           summary.CommittedAt,
		"duration_ms":            duration,
		"job_id":                 e.JobID,
		"review_id":              e.ReviewID,
		"target_execution_id":    e.TargetExecutionID,
		"irreversible":           r.Irreversible,
		"irreversibility_reason": r.IrreversibleReason,
		"can_rollback":           rollback,
		"can_forward":            forward,
		"action_blocked_reason":  v.state.BlockedReason,
		"expected_generation":    v.state.Generation,
	}, nil
}
func (v *Resources) Job(j dao.MigrationJob, p dao.BatchProgress) (map[string]any, error) {
	review, err := v.w.store.Review(v.ctx, j.ReviewID, false)
	if err != nil {
		return nil, err
	}
	r, err := v.revision(review.RevisionID)
	if err != nil {
		return nil, err
	}
	start, finish, err := v.w.store.JobTimes(v.ctx, j.ID)
	if err != nil {
		return nil, err
	}
	var next any
	if j.Status == "pending" || ((j.Status == "partial" || j.Status == "partially_reverted") && j.ErrorClass == "temporary_failure" && j.Attempt < j.MaxAttempts) {
		next = j.NextAttemptAt
	}
	retry := v.edit && j.CancellationJobID == nil && (j.Status == "uncertain" || (j.ActorID == v.actor && j.Attempt < j.MaxAttempts && (j.Status == "failed" || j.Status == "partial" || j.Status == "partially_reverted")))
	cancel := v.edit && j.CancellationJobID == nil && !r.Irreversible && batchResource(r) != nil && (j.Operation == "up" || j.Operation == "forward") && (j.Status == "partial" || j.Status == "uncertain" || j.Status == "partially_reverted") && p.Batches > p.RevertedBatches
	return map[string]any{
		"job_id":              j.ID,
		"database_id":         j.DatabaseID,
		"definition_id":       r.DefinitionID,
		"revision_id":         r.ID,
		"review_id":           j.ReviewID,
		"title":               r.Title,
		"operation":           resourceAction(j.Operation, r),
		"status":              j.Status,
		"requested_by":        v.name(j.ActorID),
		"created_at":          j.CreatedAt,
		"started_at":          start,
		"completed_at":        finish,
		"attempt":             j.Attempt,
		"max_attempts":        j.MaxAttempts,
		"next_retry_at":       next,
		"committed_batches":   p.Batches,
		"committed_rows":      p.Rows,
		"reverted_batches":    p.RevertedBatches,
		"reverted_rows":       p.RevertedRows,
		"total_rows":          p.Total,
		"last_cursor":         p.Cursor,
		"retryable":           retry,
		"can_cancel":          cancel,
		"blocked_reason":      v.state.BlockedReason,
		"error":               j.ErrorClass,
		"expected_generation": v.state.Generation,
	}, nil
}
