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

const draftColumns = `id,database_id,created_by,title,input_mode,COALESCE(sql_text,''),created_at,updated_at`

func scanDraft(row scanner) (MigrationDraft, error) {
	var d MigrationDraft
	err := row.Scan(&d.ID, &d.DatabaseID, &d.CreatedBy, &d.Title, &d.InputMode, &d.SQL, &d.CreatedAt, &d.UpdatedAt)
	return d, classify(err)
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
