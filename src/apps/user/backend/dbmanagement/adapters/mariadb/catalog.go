package mariadb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

func databaseName(ctx context.Context, q adapters.Querier) (string, error) {
	var name string
	if q.QueryRowContext(ctx, `SELECT DATABASE()`).Scan(&name) != nil {
		return "", adapters.ErrQuery
	}
	return name, nil
}
func validSchema(ctx context.Context, q adapters.Querier, name string) error {
	current, err := databaseName(ctx, q)
	if err != nil {
		return err
	}
	if !adapters.ValidIdentifier(name, 64) || name != current {
		return adapters.ErrInvalid
	}
	return nil
}
func validTable(name string) bool {
	return adapters.ValidIdentifier(name, 64) && !strings.HasPrefix(strings.ToLower(name), "_schakker_")
}
func (a Adapter) Schemas(ctx context.Context, q adapters.Querier) ([]adapters.Schema, error) {
	var name, owner string
	if q.QueryRowContext(ctx, `SELECT DATABASE(),CURRENT_USER()`).Scan(&name, &owner) != nil {
		return nil, adapters.ErrQuery
	}
	return []adapters.Schema{{Name: name, Owner: owner}}, nil
}
func (a Adapter) Tables(ctx context.Context, q adapters.Querier, schema string) ([]adapters.Table, error) {
	if err := validSchema(ctx, q, schema); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT TABLE_SCHEMA,TABLE_NAME,CASE WHEN TABLE_TYPE='VIEW' THEN 'v' ELSE 'r' END,COALESCE(TABLE_ROWS,0),COALESCE(ENGINE,''),COALESCE(TABLE_COLLATION,'') FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND LEFT(TABLE_NAME,10)<>'_schakker_' ORDER BY TABLE_NAME`, schema)
	if err != nil {
		return nil, adapters.ErrQuery
	}
	defer rows.Close()
	result := make([]adapters.Table, 0)
	for rows.Next() {
		var t adapters.Table
		if rows.Scan(&t.Schema, &t.Name, &t.Kind, &t.EstimatedRows, &t.Engine, &t.Collation) != nil {
			return nil, adapters.ErrQuery
		}
		result = append(result, t)
	}
	if rows.Err() != nil {
		return nil, adapters.ErrQuery
	}
	return result, nil
}

func (a Adapter) DescribeTable(ctx context.Context, q adapters.Querier, schema, table string) (adapters.TableDetail, error) {
	result := adapters.TableDetail{Columns: []adapters.Column{}, Constraints: []adapters.Constraint{}, Indexes: []adapters.Index{}}
	if err := validSchema(ctx, q, schema); err != nil {
		return result, err
	}
	if !validTable(table) {
		return result, adapters.ErrInvalid
	}
	tables, err := a.Tables(ctx, q, schema)
	if err != nil {
		return result, err
	}
	found := false
	for _, t := range tables {
		if t.Name == table {
			result.Table = t
			found = true
			break
		}
	}
	if !found {
		return result, adapters.ErrNotFound
	}
	if result.Table.Kind == "v" {
		if q.QueryRowContext(ctx, `SELECT VIEW_DEFINITION FROM information_schema.VIEWS WHERE TABLE_SCHEMA=? AND TABLE_NAME=?`, schema, table).Scan(&result.Table.Definition) != nil {
			return result, adapters.ErrQuery
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT COLUMN_NAME,ORDINAL_POSITION,COLUMN_TYPE,IS_NULLABLE='YES',COLUMN_DEFAULT,EXTRA,GENERATION_EXPRESSION FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return result, adapters.ErrQuery
	}
	for rows.Next() {
		var c adapters.Column
		var extra string
		var generated *string
		if rows.Scan(&c.Name, &c.Position, &c.DataType, &c.Nullable, &c.Default, &extra, &generated) != nil {
			rows.Close()
			return result, adapters.ErrQuery
		}
		if c.Default != nil && strings.EqualFold(*c.Default, "NULL") && c.Nullable {
			c.Default = nil
		}
		if strings.Contains(extra, "auto_increment") {
			c.Identity = "auto_increment"
		}
		if generated != nil && *generated != "" {
			c.GeneratedExpression = generated
			c.Default = nil
			if strings.Contains(strings.ToUpper(extra), "STORED") {
				c.Generated = "stored"
			} else {
				c.Generated = "virtual"
			}
		}
		result.Columns = append(result.Columns, c)
	}
	if rows.Err() != nil {
		rows.Close()
		return result, adapters.ErrQuery
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT CONSTRAINT_NAME,CONSTRAINT_TYPE FROM information_schema.TABLE_CONSTRAINTS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? ORDER BY CONSTRAINT_NAME`, schema, table)
	if err != nil {
		return result, adapters.ErrQuery
	}
	for rows.Next() {
		var c adapters.Constraint
		var kind string
		if rows.Scan(&c.Name, &kind) != nil {
			rows.Close()
			return result, adapters.ErrQuery
		}
		switch kind {
		case "PRIMARY KEY":
			c.Type = "p"
		case "UNIQUE":
			c.Type = "u"
		case "FOREIGN KEY":
			c.Type = "f"
		case "CHECK":
			c.Type = "c"
		default:
			rows.Close()
			return result, adapters.ErrUnsupported
		}
		result.Constraints = append(result.Constraints, c)
	}
	if rows.Err() != nil {
		rows.Close()
		return result, adapters.ErrQuery
	}
	rows.Close()
	for i := range result.Constraints {
		c := &result.Constraints[i]
		c.Columns = []string{}
		c.ReferencedColumns = []string{}
		if c.Type == "c" {
			if q.QueryRowContext(ctx, `SELECT CHECK_CLAUSE FROM information_schema.CHECK_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=? AND TABLE_NAME=? AND CONSTRAINT_NAME=?`, schema, table, c.Name).Scan(&c.Expression) != nil {
				return result, adapters.ErrQuery
			}
			c.Definition = "CHECK (" + c.Expression + ")"
			continue
		}
		keys, err := q.QueryContext(ctx, `SELECT COLUMN_NAME,REFERENCED_TABLE_SCHEMA,REFERENCED_TABLE_NAME,REFERENCED_COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=? AND TABLE_NAME=? AND CONSTRAINT_NAME=? ORDER BY ORDINAL_POSITION`, schema, table, c.Name)
		if err != nil {
			return result, adapters.ErrQuery
		}
		for keys.Next() {
			var col string
			var rs, rt, rc *string
			if keys.Scan(&col, &rs, &rt, &rc) != nil {
				keys.Close()
				return result, adapters.ErrQuery
			}
			c.Columns = append(c.Columns, col)
			if rc != nil {
				c.ReferencedColumns = append(c.ReferencedColumns, *rc)
				c.ReferencedSchema = rs
				c.ReferencedTable = rt
			}
		}
		if keys.Err() != nil {
			keys.Close()
			return result, adapters.ErrQuery
		}
		keys.Close()
		quoted := make([]string, 0, len(c.Columns))
		for _, col := range c.Columns {
			x, _ := a.QuoteIdentifier(col)
			quoted = append(quoted, x)
		}
		switch c.Type {
		case "p":
			c.Definition = "PRIMARY KEY (" + strings.Join(quoted, ",") + ")"
		case "u":
			c.Definition = "UNIQUE (" + strings.Join(quoted, ",") + ")"
		case "f":
			if q.QueryRowContext(ctx, `SELECT UPDATE_RULE,DELETE_RULE FROM information_schema.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=? AND TABLE_NAME=? AND CONSTRAINT_NAME=?`, schema, table, c.Name).Scan(&c.UpdateRule, &c.DeleteRule) != nil {
				return result, adapters.ErrQuery
			}
			c.Definition = "FOREIGN KEY (" + strings.Join(quoted, ",") + ")"
		}
	}
	rows, err = q.QueryContext(ctx, `SELECT INDEX_NAME,NON_UNIQUE=0,INDEX_TYPE,COLUMN_NAME,COLLATION,COALESCE(SUB_PART,0) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? ORDER BY INDEX_NAME,SEQ_IN_INDEX`, schema, table)
	if err != nil {
		return result, adapters.ErrQuery
	}
	defer rows.Close()
	for rows.Next() {
		var name, method string
		var column, direction *string
		var unique bool
		var prefix int
		if rows.Scan(&name, &unique, &method, &column, &direction, &prefix) != nil || column == nil {
			return result, adapters.ErrQuery
		}
		position := -1
		for i := range result.Indexes {
			if result.Indexes[i].Name == name {
				position = i
				break
			}
		}
		if position < 0 {
			owned := false
			constraintName := ""
			for _, c := range result.Constraints {
				if c.Name == name && (c.Type == "p" || c.Type == "u" || c.Type == "f") {
					owned = true
					constraintName = c.Name
				}
			}
			result.Indexes = append(result.Indexes, adapters.Index{Name: name, Method: strings.ToLower(method), Unique: unique, Primary: name == "PRIMARY", Valid: true, ConstraintOwned: owned, ConstraintName: constraintName, Columns: []adapters.IndexColumn{}})
			position = len(result.Indexes) - 1
		}
		key := adapters.IndexColumn{Name: *column, PrefixLength: prefix, Descending: direction != nil && *direction == "D"}
		result.Indexes[position].Columns = append(result.Indexes[position].Columns, key)
	}
	if rows.Err() != nil {
		return result, adapters.ErrQuery
	}
	return result, nil
}

func (a Adapter) Snapshot(ctx context.Context, q adapters.Querier) (adapters.Snapshot, error) {
	return adapters.Capture(ctx, q, a)
}
func (a Adapter) Fingerprint(ctx context.Context, q adapters.Querier) (string, error) {
	snapshot, err := a.Snapshot(ctx, q)
	if err != nil {
		return "", err
	}
	for i := range snapshot.Tables {
		snapshot.Tables[i].Table.EstimatedRows = 0
	}
	snapshot.Fingerprint = ""
	extra := make([][]string, 0)
	for _, query := range []string{`SELECT ROUTINE_NAME,ROUTINE_TYPE,COALESCE(ROUTINE_DEFINITION,''),SQL_DATA_ACCESS,SECURITY_TYPE FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=DATABASE() ORDER BY ROUTINE_NAME,ROUTINE_TYPE`, `SELECT TRIGGER_NAME,ACTION_TIMING,EVENT_MANIPULATION,EVENT_OBJECT_TABLE,ACTION_STATEMENT FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() ORDER BY TRIGGER_NAME`} {
		rows, err := q.QueryContext(ctx, query)
		if err != nil {
			return "", adapters.ErrQuery
		}
		for rows.Next() {
			values := make([]string, 5)
			if rows.Scan(&values[0], &values[1], &values[2], &values[3], &values[4]) != nil {
				rows.Close()
				return "", adapters.ErrQuery
			}
			extra = append(extra, values)
		}
		if rows.Err() != nil {
			rows.Close()
			return "", adapters.ErrQuery
		}
		rows.Close()
	}
	sort.Slice(snapshot.Tables, func(i, j int) bool { return snapshot.Tables[i].Table.Name < snapshot.Tables[j].Table.Name })
	data, err := json.Marshal(struct {
		Schema adapters.Snapshot
		Extra  [][]string
	}{snapshot, extra})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
