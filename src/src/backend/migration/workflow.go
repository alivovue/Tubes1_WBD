package migration

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"schakker_shared/authority"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"
)

var (
	ErrWorkflowConflict    = errors.New("migration workflow changed or is blocked")
	ErrDefinitionInvalid   = errors.New("supply validated up/down SQL or an irreversible reason")
	ErrWorkflowUnavailable = errors.New("migration workflow infrastructure is unavailable")
	ErrWorkflowRequired    = errors.New("submit a migration revision for leader review and asynchronous execution")
)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Dispatcher must insert a durable job USING tx in this metadata transaction.
// It must not execute target SQL. P3 supplies its queue implementation at startup.
type Dispatcher interface {
	Enqueue(context.Context, *dao.Store, ApprovedPlan, string) (string, error)
}
type RecoverySource struct {
	JobID                                        string
	RevisionID, DatabaseID                       int64
	Fingerprint                                  string
	Paused, PartiallyCommitted, SafeCompensation bool
}
type RecoveryProvider interface {
	CancellationSource(context.Context, string) (RecoverySource, error)
}

type AuthorizedRecoveryProvider interface {
	CancellationSourceFor(context.Context, int64, string) (RecoverySource, error)
}
type Activity = dao.MigrationActivity
type ActivityReader interface {
	MigrationActivities(context.Context, int64, int, int) ([]Activity, error)
}

// Observer lets P3 supply shared OTel spans without coupling policy to a tracer SDK.
// End receives only a status, never SQL, credentials or raw driver errors.
type Observer interface {
	Start(context.Context, string, int64, int64) (context.Context, func(string))
}
type WorkflowOptions struct {
	Dispatcher Dispatcher
	Recovery   RecoveryProvider
	Activities ActivityReader
	Observer   Observer
	Logger     *slog.Logger
}
type Workflow struct {
	store   *dao.Store
	auth    *access.Service
	options WorkflowOptions
}

func NewWorkflow(store *dao.Store, auth *access.Service, options WorkflowOptions) *Workflow {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &Workflow{store: store, auth: auth, options: options}
}

// UsingStore binds policy/history operations to the connection already holding
// the worker's database advisory lock, avoiding a second pool checkout while locked.
func (w *Workflow) UsingStore(store *dao.Store) *Workflow {
	return &Workflow{store: store, auth: w.auth, options: w.options}
}
func (w *Workflow) observe(ctx context.Context, event string, actor, database int64) (context.Context, func(error)) {
	end := func(string) {}
	if w.options.Observer != nil {
		ctx, end = w.options.Observer.Start(ctx, event, actor, database)
		if end == nil {
			end = func(string) {}
		}
	}
	started := time.Now()
	return ctx, func(err error) {
		status := "succeeded"
		if err != nil {
			status = "rejected"
		}
		end(status)
		w.options.Logger.InfoContext(ctx, event, "event", event, "actor_id", actor, "database_id", database, "status", status, "duration_ms", time.Since(started).Milliseconds())
	}
}
func validPage(limit, offset int) (int, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 || offset < 0 || offset > 100000000 {
		return 0, ErrInvalid
	}
	return limit, nil
}
func (w *Workflow) snapshot(ctx context.Context, actor, database int64) (authority.Snapshot, error) {
	s, err := w.auth.MigrationAuthority(ctx, actor, database)
	if errors.Is(err, access.ErrForbidden) || errors.Is(err, access.ErrNotFound) {
		return s, ErrForbidden
	}
	if err != nil {
		return s, ErrWorkflowUnavailable
	}
	if len(s.Groups) == 0 {
		return s, ErrWorkflowConflict
	}
	for _, g := range s.Groups {
		if g.LeaderID < 1 || !g.Valid {
			return s, ErrWorkflowConflict
		}
	}
	return s, nil
}
func ContentHash(up, down string, irreversible bool, reason string) string {
	return SQLHash(strconv.Itoa(len(up)) + ":" + up + strconv.Itoa(len(down)) + ":" + down + strconv.FormatBool(irreversible) + ":" + reason)
}
func actionHash(r dao.MigrationReview, revision dao.MigrationRevision) string {
	target, job := "", ""
	if r.TargetExecutionID != nil {
		target = *r.TargetExecutionID
	}
	if r.TargetJobID != nil {
		job = *r.TargetJobID
	}
	return SQLHash(strings.Join([]string{strconv.FormatInt(r.RevisionID, 10), revision.SQLHash, r.Action, target, job, strconv.FormatInt(r.AuthorityRevision, 10), r.ConnectionFingerprint, strconv.FormatInt(r.ExpectedGeneration, 10), strconv.FormatInt(r.ExpectedActiveVersion, 10), r.RecoveryFingerprint}, "\x00"))
}
func (w *Workflow) State(ctx context.Context, actor, database int64) (dao.MigrationState, error) {
	if err := w.auth.RequirePermission(ctx, actor, database, access.ViewOnly); err != nil {
		return dao.MigrationState{}, err
	}
	return w.store.MigrationState(ctx, database, false)
}
func (w *Workflow) Definitions(ctx context.Context, actor, database int64, limit, offset int) ([]dao.MigrationRevision, error) {
	if err := w.auth.RequirePermission(ctx, actor, database, access.ViewOnly); err != nil {
		return nil, err
	}
	limit, err := validPage(limit, offset)
	if err != nil {
		return nil, err
	}
	return w.store.Definitions(ctx, database, limit, offset)
}
func (w *Workflow) Definition(ctx context.Context, actor, definition int64) (dao.MigrationRevision, error) {
	r, err := w.store.LatestRevision(ctx, definition)
	if err != nil {
		return r, err
	}
	if err = w.auth.RequirePermission(ctx, actor, r.DatabaseID, access.ViewOnly); err != nil {
		return dao.MigrationRevision{}, err
	}
	return r, nil
}

// ApprovedPlan is reconstructed on the server. Workers never trust SQL in a job payload.
type ApprovedPlan struct {
	ReviewID              int64   `json:"review_id"`
	RevisionID            int64   `json:"revision_id"`
	DefinitionID          int64   `json:"definition_id"`
	DatabaseID            int64   `json:"database_id"`
	ActorID               int64   `json:"actor_id"`
	Operation             string  `json:"operation"`
	SQL                   string  `json:"sql"`
	SQLHash               string  `json:"sql_hash"`
	RevisionHash          string  `json:"revision_hash"`
	ActionHash            string  `json:"action_hash"`
	ExpectedGeneration    int64   `json:"expected_generation"`
	ExpectedActiveVersion int64   `json:"expected_active_version"`
	AuthorityRevision     int64   `json:"authority_revision"`
	ConnectionFingerprint string  `json:"connection_fingerprint"`
	TargetExecutionID     *string `json:"target_execution_id"`
	TargetJobID           *string `json:"target_job_id"`
	RecoveryFingerprint   string  `json:"recovery_fingerprint,omitempty"`
}

func planFor(r dao.MigrationReview, revision dao.MigrationRevision) ApprovedPlan {
	sql := revision.UpSQL
	if r.Action == "rollback" || r.Action == "batch_cancel" {
		sql = revision.DownSQL
	}
	return ApprovedPlan{ReviewID: r.ID, RevisionID: revision.ID, DefinitionID: revision.DefinitionID, DatabaseID: r.DatabaseID, ActorID: r.SubmittedBy, Operation: r.Action, SQL: sql, SQLHash: SQLHash(sql), RevisionHash: revision.SQLHash, ActionHash: r.ActionHash, ExpectedGeneration: r.ExpectedGeneration, ExpectedActiveVersion: r.ExpectedActiveVersion, AuthorityRevision: r.AuthorityRevision, ConnectionFingerprint: r.ConnectionFingerprint, TargetExecutionID: r.TargetExecutionID, TargetJobID: r.TargetJobID, RecoveryFingerprint: r.RecoveryFingerprint}
}
func allApproved(r dao.MigrationReview) bool {
	if len(r.AuthoritySnapshot.Groups) == 0 || len(r.Decisions) != len(r.AuthoritySnapshot.Groups) {
		return false
	}
	for _, g := range r.AuthoritySnapshot.Groups {
		found := false
		for _, a := range r.Decisions {
			if a.GroupID == g.ID && a.ApproverID == g.LeaderID && a.Decision == "approve" {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// withReview commits invalidation even when the caller receives a conflict.
func (w *Workflow) withReview(ctx context.Context, id int64, fn func(*dao.Store, dao.MigrationReview, dao.MigrationRevision, dao.MigrationState) error) error {
	original, err := w.store.Review(ctx, id, false)
	if err != nil {
		return err
	}
	var stale bool
	err = w.store.Transaction(ctx, func(tx *dao.Store) error {
		state, err := tx.MigrationState(ctx, original.DatabaseID, true)
		if err != nil {
			return err
		}
		r, err := tx.Review(ctx, id, true)
		if err != nil {
			return err
		}
		revision, err := tx.Revision(ctx, r.RevisionID)
		if err != nil {
			return err
		}
		if r.State != "pending_review" && r.State != "approved" {
			return ErrWorkflowConflict
		}
		current, err := w.snapshot(ctx, r.SubmittedBy, r.DatabaseID)
		if err != nil && !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrWorkflowConflict) {
			return err
		}
		if err != nil || !reflect.DeepEqual(current, r.AuthoritySnapshot) || r.ActionHash != actionHash(r, revision) || revision.SQLHash != ContentHash(revision.UpSQL, revision.DownSQL, revision.Irreversible, revision.IrreversibleReason) || state.Generation != r.ExpectedGeneration || state.ActiveVersion != r.ExpectedActiveVersion {
			stale = true
			return tx.SetReviewState(ctx, id, "invalidated")
		}
		if err = w.checkAction(ctx, tx, r, revision, state); err != nil {
			if errors.Is(err, errRecoveryScopeChanged) {
				stale = true
				return tx.SetReviewState(ctx, id, "invalidated")
			}
			return err
		}
		return fn(tx, r, revision, state)
	})
	if err == nil && stale {
		return ErrWorkflowConflict
	}
	return err
}
