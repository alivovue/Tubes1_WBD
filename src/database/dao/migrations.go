package dao

import (
	"context"
	"errors"
	"time"
)

type MigrationDraft struct {
	ID         int64
	DatabaseID int64
	CreatedBy  int64
	Title      string
	InputMode  string
	SQL        string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type MigrationExecution struct {
	ID               int64
	DatabaseID       int64
	DraftID          *int64
	ExecutedBy       int64
	Username         string
	Description      string
	SQL              string
	AttemptedVersion int64
	AppliedVersion   *int64
	Status           string
	ErrorMessage     string
	StartedAt        time.Time
	FinishedAt       *time.Time
}

const draftColumns = `id,database_id,created_by,title,input_mode,COALESCE(sql_text,''),created_at,updated_at`
const executionColumns = `e.id,e.database_id,e.draft_id,e.executed_by,u.username,COALESCE(e.description,''),e.sql_text,e.attempted_version,e.applied_version,e.status,COALESCE(e.error_message,''),e.started_at,e.finished_at`

func scanDraft(row scanner) (MigrationDraft, error) {
	var d MigrationDraft
	err := row.Scan(&d.ID, &d.DatabaseID, &d.CreatedBy, &d.Title, &d.InputMode, &d.SQL, &d.CreatedAt, &d.UpdatedAt)
	return d, classify(err)
}

func scanExecution(row scanner) (MigrationExecution, error) {
	var e MigrationExecution
	err := row.Scan(&e.ID, &e.DatabaseID, &e.DraftID, &e.ExecutedBy, &e.Username, &e.Description, &e.SQL, &e.AttemptedVersion, &e.AppliedVersion, &e.Status, &e.ErrorMessage, &e.StartedAt, &e.FinishedAt)
	return e, classify(err)
}

func (d *Store) Draft(ctx context.Context, database, id int64) (MigrationDraft, error) {
	return scanDraft(d.q.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM migration_drafts WHERE database_id=$1 AND id=$2`, database, id))
}

func (d *Store) Drafts(ctx context.Context, database int64, limit, offset int) ([]MigrationDraft, error) {
	rows, err := d.q.QueryContext(ctx, `SELECT `+draftColumns+` FROM migration_drafts WHERE database_id=$1 ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, database, limit, offset)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	result := make([]MigrationDraft, 0)
	for rows.Next() {
		draft, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, draft)
	}
	return result, classify(rows.Err())
}

func (d *Store) CreateDraft(ctx context.Context, database, actor int64, title, query string) (MigrationDraft, error) {
	return scanDraft(d.q.QueryRowContext(ctx, `INSERT INTO migration_drafts(database_id,created_by,title,input_mode,sql_text) VALUES($1,$2,$3,'sql',$4) RETURNING `+draftColumns, database, actor, title, query))
}

func (d *Store) UpdateDraft(ctx context.Context, database, id int64, title, query string, expected time.Time) (MigrationDraft, error) {
	draft, err := scanDraft(d.q.QueryRowContext(ctx, `UPDATE migration_drafts SET title=$3,sql_text=$4,input_mode='sql',operations=NULL,updated_at=clock_timestamp() WHERE database_id=$1 AND id=$2 AND updated_at=$5 RETURNING `+draftColumns, database, id, title, query, expected))
	if errors.Is(err, ErrNotFound) {
		if _, lookup := d.Draft(ctx, database, id); lookup == nil {
			return draft, ErrConflict
		}
	}
	return draft, err
}

func (d *Store) DeleteDraft(ctx context.Context, database, id int64) error {
	return affected(d.q.ExecContext(ctx, `DELETE FROM migration_drafts WHERE database_id=$1 AND id=$2`, database, id))
}

func (d *Store) HasMigrationHistory(ctx context.Context, database int64) (bool, error) {
	var exists bool
	err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM migration_executions WHERE database_id=$1)`, database).Scan(&exists)
	return exists, classify(err)
}

func (d *Store) MigrationVersion(ctx context.Context, database int64) (int64, error) {
	var version int64
	err := d.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(applied_version),0) FROM migration_executions WHERE database_id=$1 AND status='succeeded'`, database).Scan(&version)
	return version, classify(err)
}

func (d *Store) StartExecution(ctx context.Context, e MigrationExecution) (MigrationExecution, error) {
	var id int64
	err := d.q.QueryRowContext(ctx, `INSERT INTO migration_executions(database_id,draft_id,executed_by,description,sql_text,attempted_version,status) VALUES($1,$2,$3,$4,$5,$6,'running') RETURNING id`, e.DatabaseID, e.DraftID, e.ExecutedBy, e.Description, e.SQL, e.AttemptedVersion).Scan(&id)
	if err != nil {
		return MigrationExecution{}, classify(err)
	}
	return d.Execution(ctx, e.DatabaseID, id)
}

func (d *Store) FinishExecution(ctx context.Context, database, id int64, status string, version *int64, message string, finished time.Time) error {
	if (status != "succeeded" && status != "failed") || (status == "succeeded" && (version == nil || *version < 1)) || (status == "failed" && version != nil) {
		return ErrInvalid
	}
	return affected(d.q.ExecContext(ctx, `UPDATE migration_executions SET status=$3,applied_version=$4,error_message=NULLIF($5,''),finished_at=$6 WHERE database_id=$1 AND id=$2 AND status='running'`, database, id, status, version, message, finished))
}

func (d *Store) Execution(ctx context.Context, database, id int64) (MigrationExecution, error) {
	return scanExecution(d.q.QueryRowContext(ctx, `SELECT `+executionColumns+` FROM migration_executions e JOIN users u ON u.id=e.executed_by WHERE e.database_id=$1 AND e.id=$2`, database, id))
}

func (d *Store) MigrationHistory(ctx context.Context, database int64, limit, offset int) ([]MigrationExecution, error) {
	return d.executions(ctx, `SELECT `+executionColumns+` FROM migration_executions e JOIN users u ON u.id=e.executed_by WHERE e.database_id=$1 ORDER BY e.id DESC LIMIT $2 OFFSET $3`, database, limit, offset)
}

func (d *Store) UnfinishedExecutions(ctx context.Context, database int64) ([]MigrationExecution, error) {
	return d.executions(ctx, `SELECT `+executionColumns+` FROM migration_executions e JOIN users u ON u.id=e.executed_by WHERE e.database_id=$1 AND e.status='running' ORDER BY e.id`, database)
}

func (d *Store) executions(ctx context.Context, query string, args ...any) ([]MigrationExecution, error) {
	rows, err := d.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	result := make([]MigrationExecution, 0)
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, classify(rows.Err())
}
