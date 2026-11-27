package postgres

import (
	"context"
	"database/sql"

	"schakker_shared/observability"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/engines"
)

type batchRelation struct{ schema, table, key string }

func (r batchRelation) available(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var available bool
	err := observability.WrapSQL(tx).QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid
 WHERE a.attrelid=to_regclass($1) AND c.relkind='r' AND a.attname=$2
 AND a.atttypid='pg_catalog.int8'::regtype AND NOT a.attisdropped)`, table, r.key).Scan(&available)
	return available, err
}

// Keep expectations in the database rather than loading tracked row IDs into Go.
// Only tuples whose expected version still matches are locked and captured. An
// external edit, including a no-op update, therefore cannot be legitimized by a
// later platform migration. The temporary snapshot is protected like receipts.
func captureBatchVersions(ctx context.Context, tx *sql.Tx, a adapters.Adapter, namespace string, scope *engines.BatchSpec, ids []int64) ([]batchRelation, error) {
	q := observability.WrapSQL(tx)
	if _, err := q.ExecContext(ctx, `CREATE TEMP TABLE _schakker (
 namespace text, job_id uuid, row_id bigint, before_xmin text,
 PRIMARY KEY(namespace,job_id,row_id)) ON COMMIT DROP`); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT spec->>'schema',spec->>'table',spec->>'key'
 FROM _schakker.batch_jobs WHERE namespace=$1 AND state<>'reverted'`, namespace)
	if err != nil {
		return nil, err
	}
	var relations []batchRelation
	for rows.Next() {
		var relation batchRelation
		if err = rows.Scan(&relation.schema, &relation.table, &relation.key); err != nil {
			rows.Close()
			return nil, err
		}
		if scope == nil || relation.schema == scope.Schema && relation.table == scope.Table && relation.key == scope.Key {
			relations = append(relations, relation)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, relation := range relations {
		table, err := a.QuoteIdentifier(relation.schema, relation.table)
		if err != nil {
			return nil, err
		}
		key, err := a.QuoteIdentifier(relation.key)
		if err != nil {
			return nil, err
		}
		available, err := relation.available(ctx, tx, table)
		if err != nil {
			return nil, err
		}
		if !available {
			continue
		}
		query := `INSERT INTO pg_temp._schakker
 SELECT u.namespace,u.job_id,u.row_id,t.xmin::text
 FROM _schakker.batch_undo u JOIN _schakker.batch_jobs b USING(namespace,job_id)
 JOIN ` + table + ` t ON t.` + key + `=u.row_id
 WHERE u.namespace=$1 AND b.spec->>'schema'=$2 AND b.spec->>'table'=$3 AND b.spec->>'key'=$4
 AND NOT u.reverted AND u.after_xmin=t.xmin::text`
		args := []any{namespace, relation.schema, relation.table, relation.key}
		if ids != nil {
			query += ` AND u.row_id=ANY($5::bigint[])`
			args = append(args, ids)
		}
		if _, err := q.ExecContext(ctx, query+` FOR UPDATE OF t`, args...); err != nil {
			return nil, err
		}
	}
	return relations, nil
}

// Advance only versions captured before this approved transaction. Expected
// values stay unchanged: an older migration can roll back only once the later
// migration's down SQL has restored its values, in the normal LIFO order.
func refreshBatchVersions(ctx context.Context, tx *sql.Tx, a adapters.Adapter, relations []batchRelation) error {
	for _, relation := range relations {
		table, err := a.QuoteIdentifier(relation.schema, relation.table)
		if err != nil {
			return err
		}
		key, err := a.QuoteIdentifier(relation.key)
		if err != nil {
			return err
		}
		available, err := relation.available(ctx, tx, table)
		if err != nil {
			return err
		}
		if !available {
			continue
		}
		_, err = observability.WrapSQL(tx).ExecContext(ctx, `UPDATE _schakker.batch_undo u
 SET after_xmin=t.xmin::text
 FROM pg_temp._schakker v JOIN `+table+` t ON t.`+key+`=v.row_id, _schakker.batch_jobs b
 WHERE u.namespace=v.namespace AND u.job_id=v.job_id AND u.row_id=v.row_id
 AND b.namespace=u.namespace AND b.job_id=u.job_id
 AND b.spec->>'schema'=$1 AND b.spec->>'table'=$2 AND b.spec->>'key'=$3
 AND NOT u.reverted AND u.after_xmin=v.before_xmin`, relation.schema, relation.table, relation.key)
		if err != nil {
			return err
		}
	}
	return nil
}
