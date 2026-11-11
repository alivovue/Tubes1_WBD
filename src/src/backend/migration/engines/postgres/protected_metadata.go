package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"schakker_shared/observability"
	"time"

	"tubes1_wbd/backend/migration/engines"
)

type receiptKey struct {
	namespace string
	attemptID int64
}

type protectedReceipt struct {
	version   int64
	sqlHash   string
	appliedAt time.Time
}

type protectedMetadata struct {
	catalog  string
	receipts map[receiptKey]protectedReceipt
	jobs     string
}

const protectedCatalogSQL = `SELECT COALESCE((SELECT c.relkind='r' AND NOT c.relrowsecurity AND NOT c.relforcerowsecurity FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='_schakker' AND c.relname='migration_receipts'),false),
COALESCE(pg_catalog.string_agg(item,E'\n' ORDER BY item),'') FROM (
SELECT pg_catalog.json_build_array('schema',n.oid,n.nspowner,n.nspacl)::text AS item FROM pg_catalog.pg_namespace n WHERE n.nspname='_schakker'
UNION ALL SELECT pg_catalog.json_build_array('relation',c.oid,c.relname,c.relkind,c.relowner,c.relpersistence,c.relrowsecurity,c.relforcerowsecurity,c.relacl,c.reloptions)::text FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='_schakker'
UNION ALL SELECT pg_catalog.json_build_array('column',pg_catalog.to_jsonb(a))::text FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid=a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='_schakker'
UNION ALL SELECT pg_catalog.json_build_array('default',d.adrelid,d.adnum,pg_catalog.pg_get_expr(d.adbin,d.adrelid))::text FROM pg_catalog.pg_attrdef d JOIN pg_catalog.pg_class c ON c.oid=d.adrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='_schakker'
UNION ALL SELECT pg_catalog.json_build_array('constraint',pg_catalog.to_jsonb(k))::text FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_namespace n ON n.oid=k.connamespace WHERE n.nspname='_schakker'
UNION ALL SELECT pg_catalog.json_build_array('index',pg_catalog.to_jsonb(i))::text FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class c ON c.oid=i.indexrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='_schakker'
UNION ALL SELECT pg_catalog.json_build_array('trigger',pg_catalog.to_jsonb(t),pg_catalog.pg_get_functiondef(t.tgfoid))::text FROM pg_catalog.pg_trigger t JOIN pg_catalog.pg_class c ON c.oid=t.tgrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='_schakker'
UNION ALL SELECT pg_catalog.json_build_array('rule',r.oid,pg_catalog.pg_get_ruledef(r.oid))::text FROM pg_catalog.pg_rewrite r JOIN pg_catalog.pg_class c ON c.oid=r.ev_class JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='_schakker'
UNION ALL SELECT pg_catalog.json_build_array('routine',p.oid,pg_catalog.pg_get_functiondef(p.oid))::text FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='_schakker'
UNION ALL SELECT pg_catalog.json_build_array('type',pg_catalog.to_jsonb(t))::text FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='_schakker'
UNION ALL SELECT pg_catalog.json_build_array('setting',name,setting)::text FROM pg_catalog.pg_settings
) protected`

func (s *session) captureProtectedMetadata(ctx context.Context) (*protectedMetadata, error) {
	state := &protectedMetadata{receipts: make(map[receiptKey]protectedReceipt)}
	var valid bool
	if err := observability.WrapSQL(s.tx).QueryRowContext(ctx, protectedCatalogSQL).Scan(&valid, &state.catalog); err != nil || !valid {
		return nil, engines.ErrProtectedMetadata
	}
	rows, err := observability.WrapSQL(s.tx).QueryContext(ctx, `SELECT namespace,attempt_id,version,sql_hash,applied_at FROM _schakker.migration_receipts`)
	if err != nil {
		return nil, engines.ErrProtectedMetadata
	}
	defer rows.Close()
	for rows.Next() {
		var key receiptKey
		var receipt protectedReceipt
		if err := rows.Scan(&key.namespace, &key.attemptID, &receipt.version, &receipt.sqlHash, &receipt.appliedAt); err != nil {
			return nil, engines.ErrProtectedMetadata
		}
		receipt.appliedAt = receipt.appliedAt.UTC()
		state.receipts[key] = receipt
	}
	if rows.Err() != nil {
		return nil, engines.ErrProtectedMetadata
	}
	rows.Close()
	state.jobs, err = s.hashJobMetadata(ctx)
	if err != nil {
		return nil, engines.ErrProtectedMetadata
	}
	return state, nil
}

func (s *session) hashJobMetadata(ctx context.Context) (string, error) {
	hash := sha256.New()
	for _, table := range []string{"job_receipts", "batch_jobs", "batch_undo", "batch_receipts"} {
		var exists bool
		if err := observability.WrapSQL(s.tx).QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, "_schakker."+table).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			continue
		}
		hash.Write([]byte(table + "\x00"))
		query := `SELECT row_to_json(x)::text FROM _schakker.` + table + ` x`
		var args []any
		if table == "job_receipts" {
			query += ` WHERE execution_id::text<>$1`
			args = []any{s.jobReceiptException}
		}
		query += ` ORDER BY namespace,job_id`
		if table == "batch_undo" {
			query += `,row_id`
		}
		if table == "batch_receipts" {
			query += `,batch_no,direction`
		}
		rows, err := observability.WrapSQL(s.tx).QueryContext(ctx, query, args...)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return "", err
			}
			hash.Write([]byte(raw))
			hash.Write([]byte{0})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *session) verifyProtectedMetadata(ctx context.Context) error {
	if s.protected == nil {
		return engines.ErrProtectedMetadata
	}
	actual, err := s.captureProtectedMetadata(ctx)
	if err != nil || actual.catalog != s.protected.catalog || actual.jobs != s.protected.jobs || !maps.Equal(actual.receipts, s.protected.receipts) {
		return engines.ErrProtectedMetadata
	}
	return nil
}
