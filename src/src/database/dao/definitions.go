package dao

import (
	"context"
	"encoding/json"
)

func (d *Store) workflowOne(ctx context.Context, out any, query string, args ...any) error {
	var raw []byte
	if err := d.q.QueryRowContext(ctx, query, args...).Scan(&raw); err != nil {
		return classify(err)
	}
	return json.Unmarshal(raw, out)
}
func workflowList[T any](ctx context.Context, d *Store, query string, args ...any) ([]T, error) {
	rows, err := d.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := make([]T, 0)
	for rows.Next() {
		var raw []byte
		var item T
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, classify(rows.Err())
}

const revisionSelect = `SELECT r.*,d.database_id,d.title,d.logical_version,d.created_by FROM migration_revisions r JOIN migration_definitions d ON d.id=r.definition_id`

func (d *Store) Revision(ctx context.Context, id int64) (MigrationRevision, error) {
	var r MigrationRevision
	err := d.workflowOne(ctx, &r, `SELECT row_to_json(x) FROM (`+revisionSelect+` WHERE r.id=$1) x`, id)
	return r, err
}
func (d *Store) LatestRevision(ctx context.Context, definition int64) (MigrationRevision, error) {
	var r MigrationRevision
	err := d.workflowOne(ctx, &r, `SELECT row_to_json(x) FROM (`+revisionSelect+` WHERE d.id=$1 ORDER BY r.revision_no DESC LIMIT 1) x`, definition)
	return r, err
}
func (d *Store) LockDefinition(ctx context.Context, id int64) error {
	if !d.inTx {
		return ErrInvalid
	}
	var found int64
	return classify(d.q.QueryRowContext(ctx, `SELECT id FROM migration_definitions WHERE id=$1 FOR UPDATE`, id).Scan(&found))
}
func (d *Store) CreateDefinition(ctx context.Context, database, actor int64, title string) (int64, error) {
	var id int64
	err := d.q.QueryRowContext(ctx, `INSERT INTO migration_definitions(database_id,created_by,title) VALUES($1,$2,$3) RETURNING id`, database, actor, title).Scan(&id)
	return id, classify(err)
}
func (d *Store) CreateRevision(ctx context.Context, r MigrationRevision) (MigrationRevision, error) {
	var id int64
	err := d.q.QueryRowContext(ctx, `INSERT INTO migration_revisions(definition_id,revision_no,up_sql,down_sql,irreversible,irreversible_reason,sql_hash) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, r.DefinitionID, r.RevisionNo, r.UpSQL, r.DownSQL, r.Irreversible, r.IrreversibleReason, r.SQLHash).Scan(&id)
	if err != nil {
		return MigrationRevision{}, classify(err)
	}
	return d.Revision(ctx, id)
}
func (d *Store) Definitions(ctx context.Context, database int64, limit, offset int) ([]MigrationRevision, error) {
	return workflowList[MigrationRevision](ctx, d, `SELECT row_to_json(x) FROM (`+revisionSelect+` WHERE d.database_id=$1 AND r.revision_no=(SELECT max(rr.revision_no) FROM migration_revisions rr WHERE rr.definition_id=d.id) ORDER BY d.id DESC LIMIT $2 OFFSET $3) x`, database, limit, offset)
}
func (d *Store) DefinitionExecuted(ctx context.Context, id int64) (bool, error) {
	var out bool
	err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM migration_execution_history WHERE definition_id=$1) OR EXISTS(SELECT 1 FROM migration_reviews v JOIN migration_revisions r ON r.id=v.revision_id WHERE r.definition_id=$1 AND v.queued_job_id IS NOT NULL)`, id).Scan(&out)
	return out, classify(err)
}
func (d *Store) FreezeRevision(ctx context.Context, id int64) error {
	_, err := d.q.ExecContext(ctx, `UPDATE migration_revisions SET frozen_at=COALESCE(frozen_at,now()) WHERE id=$1`, id)
	return classify(err)
}
func (d *Store) InvalidateDefinitionReviews(ctx context.Context, definition int64) error {
	_, err := d.q.ExecContext(ctx, `UPDATE migration_reviews SET review_state='invalidated' WHERE revision_id IN(SELECT id FROM migration_revisions WHERE definition_id=$1) AND review_state IN('pending_review','approved')`, definition)
	return classify(err)
}
func (d *Store) RenameDefinition(ctx context.Context, id int64, title string) error {
	return affected(d.q.ExecContext(ctx, `UPDATE migration_definitions SET title=$2 WHERE id=$1`, id, title))
}
