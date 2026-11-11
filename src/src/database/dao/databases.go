package dao

import "context"

func (d *Store) ExistingDatabaseNamespace(ctx context.Context, id int64) (string, error) {
	var namespace string
	err := d.q.QueryRowContext(ctx, `SELECT connection_options->>'migration_namespace' FROM managed_databases WHERE id=$1 AND COALESCE(connection_options->>'migration_namespace','')<>''`, id).Scan(&namespace)
	return namespace, classify(err)
}

// Registered IDs remain local solely for runtime foreign keys and receipt
// namespaces. Credentials and editable registration metadata live remotely.
func (d *Store) RememberDatabase(ctx context.Context, id int64) error {
	_, err := d.q.ExecContext(ctx, `INSERT INTO managed_databases(id) VALUES($1) ON CONFLICT(id) DO NOTHING`, id)
	return classify(err)
}
func (d *Store) DatabaseNamespace(ctx context.Context, id int64, candidate string) (string, error) {
	var namespace string
	err := d.q.QueryRowContext(ctx, `UPDATE managed_databases SET connection_options=jsonb_set(COALESCE(connection_options,'{}'::jsonb),'{migration_namespace}',to_jsonb(COALESCE(NULLIF(connection_options->>'migration_namespace',''),$2)::text)) WHERE id=$1 RETURNING connection_options->>'migration_namespace'`, id, candidate).Scan(&namespace)
	return namespace, classify(err)
}
