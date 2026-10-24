package dbmanagement

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"tubes1_wbd/backend/access"

	"github.com/jackc/pgx/v5"
)

type Schema struct {
	Name  string
	Owner string
}
type Table struct {
	Schema        string
	Name          string
	Kind          string
	EstimatedRows int64
}
type Column struct {
	Name      string
	Position  int
	DataType  string
	Nullable  bool
	Default   *string
	Identity  string
	Generated string
}
type Constraint struct {
	Name             string
	Type             string
	Definition       string
	Columns          []string
	ReferencedSchema *string
	ReferencedTable  *string
}
type Index struct {
	Name       string
	Definition string
	Method     string
	Unique     bool
	Primary    bool
	Valid      bool
}
type TableDetail struct {
	Table       Table
	Columns     []Column
	Constraints []Constraint
	Indexes     []Index
}
type SortColumn struct {
	Name       string
	Descending bool
}
type PageOptions struct {
	Limit   int
	Offset  int
	OrderBy []SortColumn
}
type DataColumn struct {
	Name     string
	DataType string
}
type DataPage struct {
	Columns []DataColumn
	Rows    [][]any
	Limit   int
	Offset  int
	HasMore bool
}

func validIdentifier(value string) bool {
	return value != "" && len(value) <= 63 && !strings.ContainsRune(value, 0)
}

func validSchema(value string) bool {
	return validIdentifier(value) && value != "_schakker" && value != "information_schema" && !strings.HasPrefix(value, "pg_")
}

func (s *Service) read(ctx context.Context, actor, database int64, action func(context.Context, *sql.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()
	db, err := s.open(ctx, actor, database, access.ViewOnly)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ErrConnection
	}
	defer tx.Rollback()
	if err := action(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return ErrQuery
	}
	return nil
}

func (s *Service) Schemas(ctx context.Context, actor, database int64) ([]Schema, error) {
	result := make([]Schema, 0)
	err := s.read(ctx, actor, database, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT n.nspname,pg_get_userbyid(n.nspowner) FROM pg_catalog.pg_namespace n WHERE n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%' AND has_schema_privilege(n.oid,'USAGE') ORDER BY n.nspname`)
		if err != nil {
			return ErrQuery
		}
		defer rows.Close()
		for rows.Next() {
			var schema Schema
			if err := rows.Scan(&schema.Name, &schema.Owner); err != nil {
				return ErrQuery
			}
			result = append(result, schema)
		}
		if rows.Err() != nil {
			return ErrQuery
		}
		return nil
	})
	return result, err
}

func (s *Service) Tables(ctx context.Context, actor, database int64, schema string) ([]Table, error) {
	if !validSchema(schema) {
		return nil, ErrInvalid
	}
	result := make([]Table, 0)
	err := s.read(ctx, actor, database, func(ctx context.Context, tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1 AND has_schema_privilege(oid,'USAGE'))`, schema).Scan(&exists); err != nil {
			return ErrQuery
		}
		if !exists {
			return ErrNotFound
		}
		rows, err := tx.QueryContext(ctx, `SELECT n.nspname,c.relname,c.relkind::text,GREATEST(c.reltuples,0)::bigint FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind IN ('r','p','v','m','f') ORDER BY c.relname`, schema)
		if err != nil {
			return ErrQuery
		}
		defer rows.Close()
		for rows.Next() {
			var table Table
			if err := rows.Scan(&table.Schema, &table.Name, &table.Kind, &table.EstimatedRows); err != nil {
				return ErrQuery
			}
			result = append(result, table)
		}
		if rows.Err() != nil {
			return ErrQuery
		}
		return nil
	})
	return result, err
}

func describe(ctx context.Context, tx *sql.Tx, schema, table string) (TableDetail, error) {
	result := TableDetail{Columns: []Column{}, Constraints: []Constraint{}, Indexes: []Index{}}
	var oid int64
	err := tx.QueryRowContext(ctx, `SELECT c.oid::bigint,n.nspname,c.relname,c.relkind::text,GREATEST(c.reltuples,0)::bigint FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2 AND c.relkind IN ('r','p','v','m','f') AND has_schema_privilege(n.oid,'USAGE')`, schema, table).Scan(&oid, &result.Table.Schema, &result.Table.Name, &result.Table.Kind, &result.Table.EstimatedRows)
	if err == sql.ErrNoRows {
		return result, ErrNotFound
	}
	if err != nil {
		return result, ErrQuery
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.attname,a.attnum,pg_catalog.format_type(a.atttypid,a.atttypmod),NOT (a.attnotnull OR t.typnotnull),pg_catalog.pg_get_expr(d.adbin,d.adrelid),a.attidentity::text,a.attgenerated::text FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_type t ON t.oid=a.atttypid LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=$1 AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`, oid)
	if err != nil {
		return result, ErrQuery
	}
	for rows.Next() {
		var col Column
		if err := rows.Scan(&col.Name, &col.Position, &col.DataType, &col.Nullable, &col.Default, &col.Identity, &col.Generated); err != nil {
			rows.Close()
			return result, ErrQuery
		}
		result.Columns = append(result.Columns, col)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, ErrQuery
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT c.conname,c.contype::text,pg_catalog.pg_get_constraintdef(c.oid,true),COALESCE((SELECT json_agg(a.attname ORDER BY k.position)::text FROM unnest(c.conkey) WITH ORDINALITY k(number,position) JOIN pg_catalog.pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.number),'[]'),rn.nspname,rc.relname FROM pg_catalog.pg_constraint c LEFT JOIN pg_catalog.pg_class rc ON rc.oid=c.confrelid LEFT JOIN pg_catalog.pg_namespace rn ON rn.oid=rc.relnamespace WHERE c.conrelid=$1 ORDER BY c.conname`, oid)
	if err != nil {
		return result, ErrQuery
	}
	for rows.Next() {
		var constraint Constraint
		var columns string
		if err := rows.Scan(&constraint.Name, &constraint.Type, &constraint.Definition, &columns, &constraint.ReferencedSchema, &constraint.ReferencedTable); err != nil {
			rows.Close()
			return result, ErrQuery
		}
		if json.Unmarshal([]byte(columns), &constraint.Columns) != nil {
			rows.Close()
			return result, ErrQuery
		}
		result.Constraints = append(result.Constraints, constraint)
	}
	if rows.Err() != nil {
		rows.Close()
		return result, ErrQuery
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT c.relname,pg_catalog.pg_get_indexdef(i.indexrelid),am.amname,i.indisunique,i.indisprimary,i.indisvalid FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class c ON c.oid=i.indexrelid JOIN pg_catalog.pg_am am ON am.oid=c.relam WHERE i.indrelid=$1 ORDER BY c.relname`, oid)
	if err != nil {
		return result, ErrQuery
	}
	defer rows.Close()
	for rows.Next() {
		var index Index
		if err := rows.Scan(&index.Name, &index.Definition, &index.Method, &index.Unique, &index.Primary, &index.Valid); err != nil {
			return result, ErrQuery
		}
		result.Indexes = append(result.Indexes, index)
	}
	if rows.Err() != nil {
		return result, ErrQuery
	}
	return result, nil
}

func (s *Service) DescribeTable(ctx context.Context, actor, database int64, schema, table string) (TableDetail, error) {
	if !validSchema(schema) || !validIdentifier(table) {
		return TableDetail{}, ErrInvalid
	}
	var detail TableDetail
	err := s.read(ctx, actor, database, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		detail, err = describe(ctx, tx, schema, table)
		return err
	})
	return detail, err
}

func (s *Service) TableData(ctx context.Context, actor, database int64, schema, table string, options PageOptions) (DataPage, error) {
	if !validSchema(schema) || !validIdentifier(table) || options.Offset < 0 || options.Limit < 0 || options.Limit > 500 || len(options.OrderBy) > 16 {
		return DataPage{}, ErrInvalid
	}
	if options.Limit == 0 {
		options.Limit = 100
	}
	page := DataPage{Columns: []DataColumn{}, Rows: [][]any{}, Limit: options.Limit, Offset: options.Offset}
	err := s.read(ctx, actor, database, func(ctx context.Context, tx *sql.Tx) error {
		detail, err := describe(ctx, tx, schema, table)
		if err != nil {
			return err
		}
		known := make(map[string]bool)
		for _, column := range detail.Columns {
			known[column.Name] = true
		}
		order := make([]string, 0, len(options.OrderBy))
		for _, sort := range options.OrderBy {
			if !known[sort.Name] {
				return ErrInvalid
			}
			part := pgx.Identifier{sort.Name}.Sanitize()
			if sort.Descending {
				part += " DESC"
			}
			order = append(order, part)
		}
		if len(order) == 0 {
			for _, constraint := range detail.Constraints {
				if constraint.Type == "p" {
					for _, column := range constraint.Columns {
						order = append(order, pgx.Identifier{column}.Sanitize())
					}
					break
				}
			}
		}
		query := `SELECT * FROM ` + pgx.Identifier{schema, table}.Sanitize()
		if len(order) > 0 {
			query += " ORDER BY " + strings.Join(order, ",")
		}
		rows, err := tx.QueryContext(ctx, query+` LIMIT $1 OFFSET $2`, options.Limit+1, options.Offset)
		if err != nil {
			return ErrQuery
		}
		defer rows.Close()
		types, err := rows.ColumnTypes()
		if err != nil {
			return ErrQuery
		}
		for _, column := range types {
			page.Columns = append(page.Columns, DataColumn{Name: column.Name(), DataType: column.DatabaseTypeName()})
		}
		for rows.Next() {
			values := make([]any, len(types))
			dest := make([]any, len(types))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				return ErrQuery
			}
			if len(page.Rows) == options.Limit {
				page.HasMore = true
				break
			}
			for i, value := range values {
				if b, ok := value.([]byte); ok {
					values[i] = bytes.Clone(b)
				}
			}
			page.Rows = append(page.Rows, values)
		}
		if rows.Err() != nil {
			return ErrQuery
		}
		return nil
	})
	return page, err
}

func (s *Service) Fingerprint(ctx context.Context, actor, database int64) (string, error) {
	var value string
	err := s.read(ctx, actor, database, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		value, err = SchemaFingerprint(ctx, tx)
		return err
	})
	return value, err
}

func SchemaFingerprint(ctx context.Context, tx *sql.Tx) (string, error) {
	var value string
	err := tx.QueryRowContext(ctx, `SELECT md5(COALESCE(string_agg(item,E'\n' ORDER BY item),'')) FROM (
	SELECT json_build_array('schema',n.nspname)::text AS item FROM pg_catalog.pg_namespace n WHERE n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%'
	UNION ALL SELECT json_build_array('table',n.nspname,c.relname,c.relkind)::text FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','p','v','m','f') AND n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%'
	UNION ALL SELECT json_build_array('column',n.nspname,c.relname,a.attnum,a.attname,pg_catalog.format_type(a.atttypid,a.atttypmod),a.attnotnull,a.attidentity,a.attgenerated,pg_catalog.pg_get_expr(d.adbin,d.adrelid))::text FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid=a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum WHERE a.attnum>0 AND NOT a.attisdropped AND c.relkind IN ('r','p','v','m','f') AND n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%'
	UNION ALL SELECT json_build_array('constraint',n.nspname,c.relname,k.conname,pg_catalog.pg_get_constraintdef(k.oid,true))::text FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_class c ON c.oid=k.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%'
	UNION ALL SELECT json_build_array('index',n.nspname,c.relname,ic.relname,pg_catalog.pg_get_indexdef(i.indexrelid))::text FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class c ON c.oid=i.indrelid JOIN pg_catalog.pg_class ic ON ic.oid=i.indexrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%'
    UNION ALL SELECT json_build_array('view',n.nspname,c.relname,pg_catalog.pg_get_viewdef(c.oid,true))::text FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('v','m') AND n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%'
    UNION ALL SELECT json_build_array('type',n.nspname,t.typname,t.typtype,t.typnotnull,t.typbasetype::regtype::text,t.typdefault,(SELECT json_agg(e.enumlabel ORDER BY e.enumsortorder) FROM pg_catalog.pg_enum e WHERE e.enumtypid=t.oid))::text FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace WHERE t.typtype IN ('d','e') AND n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%'
    UNION ALL SELECT json_build_array('domain_constraint',n.nspname,t.typname,k.conname,pg_catalog.pg_get_constraintdef(k.oid,true))::text FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_type t ON t.oid=k.contypid JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%'
    UNION ALL SELECT json_build_array('routine',n.nspname,p.proname,pg_catalog.pg_get_function_identity_arguments(p.oid),pg_catalog.pg_get_functiondef(p.oid))::text FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE p.prokind IN ('f','p','w') AND n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%'
    UNION ALL SELECT json_build_array('sequence',n.nspname,c.relname,s.seqstart,s.seqincrement,s.seqmax,s.seqmin,s.seqcache,s.seqcycle)::text FROM pg_catalog.pg_sequence s JOIN pg_catalog.pg_class c ON c.oid=s.seqrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%'
    UNION ALL SELECT json_build_array('policy',n.nspname,c.relname,p.polname,p.polcmd,p.polpermissive,p.polroles,pg_catalog.pg_get_expr(p.polqual,p.polrelid),pg_catalog.pg_get_expr(p.polwithcheck,p.polrelid))::text FROM pg_catalog.pg_policy p JOIN pg_catalog.pg_class c ON c.oid=p.polrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('information_schema','_schakker') AND n.nspname NOT LIKE 'pg_%') catalog`).Scan(&value)
	if err != nil {
		return "", ErrQuery
	}
	return value, nil
}
