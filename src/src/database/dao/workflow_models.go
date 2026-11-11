package dao

import (
	"schakker_shared/authority"
	"time"
)

type MigrationRevision struct {
	ID                 int64      `json:"id"`
	DefinitionID       int64      `json:"definition_id"`
	RevisionNo         int64      `json:"revision_no"`
	DatabaseID         int64      `json:"database_id"`
	Title              string     `json:"title"`
	LogicalVersion     *int64     `json:"logical_version"`
	CreatedBy          int64      `json:"created_by"`
	UpSQL              string     `json:"up_sql"`
	DownSQL            string     `json:"down_sql"`
	Irreversible       bool       `json:"irreversible"`
	IrreversibleReason string     `json:"irreversible_reason"`
	SQLHash            string     `json:"sql_hash"`
	CreatedAt          time.Time  `json:"created_at"`
	FrozenAt           *time.Time `json:"frozen_at"`
}

type MigrationReview struct {
	ID                    int64               `json:"id"`
	RevisionID            int64               `json:"revision_id"`
	DatabaseID            int64               `json:"database_id"`
	Action                string              `json:"action"`
	TargetExecutionID     *string             `json:"target_execution_id"`
	TargetJobID           *string             `json:"target_job_id"`
	State                 string              `json:"review_state"`
	AuthorityRevision     int64               `json:"authority_revision"`
	AuthoritySnapshot     authority.Snapshot  `json:"authority_snapshot"`
	ConnectionFingerprint string              `json:"connection_fingerprint"`
	ExpectedGeneration    int64               `json:"expected_generation"`
	ExpectedActiveVersion int64               `json:"expected_active_version"`
	RecoveryFingerprint   string              `json:"recovery_fingerprint"`
	ActionHash            string              `json:"action_hash"`
	SubmittedBy           int64               `json:"submitted_by"`
	SubmittedAt           time.Time           `json:"submitted_at"`
	QueuedJobID           *string             `json:"queued_job_id"`
	IdempotencyKey        *string             `json:"idempotency_key"`
	Decisions             []MigrationDecision `json:"decisions"`
}
type MigrationDecision struct {
	GroupID    int64     `json:"group_id"`
	ApproverID int64     `json:"approver_user_id"`
	Decision   string    `json:"decision"`
	Note       string    `json:"note"`
	DecidedAt  time.Time `json:"decided_at"`
}
type MigrationState struct {
	DatabaseID    int64    `json:"database_id"`
	ActiveVersion int64    `json:"active_version"`
	Generation    int64    `json:"generation"`
	ActiveStack   []string `json:"active_stack"`
	ForwardStack  []string `json:"forward_stack"`
	BlockedReason string   `json:"blocked_reason"`
}
type CommittedExecution struct {
	Sequence            int64     `json:"history_seq"`
	ExecutionID         string    `json:"execution_id"`
	JobID               string    `json:"job_id"`
	ReviewID            *int64    `json:"review_id"`
	DefinitionID        int64     `json:"definition_id"`
	RevisionID          int64     `json:"revision_id"`
	DatabaseID          int64     `json:"database_id"`
	LogicalVersion      int64     `json:"logical_version"`
	ActorID             int64     `json:"actor_id"`
	Operation           string    `json:"operation"`
	SQL                 string    `json:"sql_snapshot"`
	CommittedAt         time.Time `json:"committed_at"`
	ReversesExecutionID *string   `json:"reverses_execution_id"`
	TargetExecutionID   *string   `json:"target_execution_id"`
	TargetReceipt       string    `json:"target_receipt"`
	Status              string    `json:"status"`
}
