package diff

import (
	"reflect"
	"regexp"
	"sort"
	"strings"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

func tableKey(schema, table string) string { return schema + "\x00" + table }
func keys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
func tables(snapshot Snapshot) (map[string]adapters.TableDetail, error) {
	result := make(map[string]adapters.TableDetail)
	for _, table := range snapshot.Tables {
		if table.Table.Kind == "" {
			table.Table.Kind = "r"
		}
		if _, err := quote(snapshot.DBMS, table.Table.Schema, table.Table.Name); err != nil {
			return nil, err
		}
		key := tableKey(table.Table.Schema, table.Table.Name)
		if _, exists := result[key]; exists {
			return nil, ErrInvalid
		}
		result[key] = table
	}
	return result, nil
}
func columns(table adapters.TableDetail) map[string]adapters.Column {
	result := make(map[string]adapters.Column)
	for _, c := range table.Columns {
		result[c.Name] = c
	}
	return result
}
func constraints(table adapters.TableDetail) map[string]adapters.Constraint {
	result := make(map[string]adapters.Constraint)
	for _, c := range table.Constraints {
		result[c.Name] = c
	}
	return result
}
func indexes(table adapters.TableDetail) map[string]adapters.Index {
	result := make(map[string]adapters.Index)
	for _, index := range table.Indexes {
		if !index.ConstraintOwned && !index.Primary {
			result[index.Name] = index
		}
	}
	return result
}
func normalizedType(dbms, value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "\"") || strings.Contains(value, "`") {
		return value
	}
	value = strings.ToLower(value)
	if dbms == "postgresql" {
		for _, pair := range [][2]string{{"varchar", "character varying"}, {"int4", "integer"}, {"int8", "bigint"}, {"int2", "smallint"}, {"int", "integer"}, {"bool", "boolean"}} {
			if value == pair[0] || strings.HasPrefix(value, pair[0]+"(") {
				return pair[1] + strings.TrimPrefix(value, pair[0])
			}
		}
	} else {
		if !strings.Contains(value, "zerofill") {
			value = regexp.MustCompile(`^(tinyint|smallint|mediumint|int|integer|bigint)\([0-9]+\)`).ReplaceAllString(value, "$1")
		}
		if value == "integer" {
			value = "int"
		}
	}
	return value
}
func normalizedDefault(dbms string, value *string) *string {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(*value)
	upper := strings.ToUpper(text)
	if upper == "NULL" || strings.HasPrefix(upper, "NULL::") {
		return nil
	}
	if dbms == "postgresql" {
		pattern := regexp.MustCompile(`^('(?:''|[^'])*')::(?:text|character varying(?:\([0-9]+\))?|character(?:\([0-9]+\))?)$`)
		text = pattern.ReplaceAllString(text, "$1")
	}
	return &text
}
func sameColumn(dbms string, a, b adapters.Column) bool {
	a.Default = normalizedDefault(dbms, a.Default)
	b.Default = normalizedDefault(dbms, b.Default)
	a.Position = 0
	b.Position = 0
	a.DataType = normalizedType(dbms, a.DataType)
	b.DataType = normalizedType(dbms, b.DataType)
	return reflect.DeepEqual(a, b)
}
func sameConstraint(a, b adapters.Constraint) bool {
	if a.Type == "f" {
		if a.UpdateRule == "" {
			a.UpdateRule = "NO ACTION"
		}
		if a.DeleteRule == "" {
			a.DeleteRule = "NO ACTION"
		}
	}
	if b.Type == "f" {
		if b.UpdateRule == "" {
			b.UpdateRule = "NO ACTION"
		}
		if b.DeleteRule == "" {
			b.DeleteRule = "NO ACTION"
		}
	}

	a.Definition = ""
	b.Definition = ""
	if len(a.Columns) == 0 {
		a.Columns = nil
	}
	if len(b.Columns) == 0 {
		b.Columns = nil
	}
	if len(a.ReferencedColumns) == 0 {
		a.ReferencedColumns = nil
	}
	if len(b.ReferencedColumns) == 0 {
		b.ReferencedColumns = nil
	}
	return reflect.DeepEqual(a, b)
}
func sameIndex(a, b adapters.Index) bool {
	a.Definition = ""
	b.Definition = ""
	a.ConstraintName = ""
	b.ConstraintName = ""
	a.Valid = false
	b.Valid = false
	if len(a.Include) == 0 {
		a.Include = nil
	}
	if len(b.Include) == 0 {
		b.Include = nil
	}
	if a.Method == "" {
		a.Method = "btree"
	}
	if b.Method == "" {
		b.Method = "btree"
	}
	return reflect.DeepEqual(a, b)
}

func validate(snapshot Snapshot, all map[string]adapters.TableDetail) error {
	for _, table := range all {
		if table.Table.Kind != "" && table.Table.Kind != "r" && table.Table.Kind != "v" && table.Table.Kind != "p" && table.Table.Kind != "m" && table.Table.Kind != "f" {
			return ErrUnsupported
		}
		known := columns(table)
		if len(known) != len(table.Columns) {
			return ErrInvalid
		}
		if table.Table.Kind != "v" && len(table.Columns) == 0 {
			return ErrInvalid
		}
		for _, column := range table.Columns {
			if _, err := columnSQL(snapshot.DBMS, column); err != nil {
				return err
			}
		}
		if len(constraints(table)) != len(table.Constraints) {
			return ErrInvalid
		}
		for _, c := range table.Constraints {
			for _, column := range c.Columns {
				if _, exists := known[column]; !exists {
					return ErrInvalid
				}
			}
			if c.Type == "f" {
				if c.ReferencedSchema == nil || c.ReferencedTable == nil {
					return ErrInvalid
				}
				reference, exists := all[tableKey(*c.ReferencedSchema, *c.ReferencedTable)]
				if !exists {
					return ErrInvalid
				}
				refCols := columns(reference)
				for _, column := range c.ReferencedColumns {
					if _, exists := refCols[column]; !exists {
						return ErrInvalid
					}
				}
			}
			if _, err := constraintSQL(snapshot.DBMS, c); err != nil {
				return err
			}
		}
		for _, index := range table.Indexes {
			if snapshot.DBMS == "mariadb" && strings.EqualFold(index.Method, "hash") && (table.Table.Engine == "" || strings.EqualFold(table.Table.Engine, "InnoDB")) {
				return ErrUnsupported
			}
			for _, include := range index.Include {
				if _, exists := known[include]; !exists {
					return ErrInvalid
				}
			}
			if index.ConstraintOwned || index.Primary {
				if index.ConstraintName != "" {
					if _, exists := constraints(table)[index.ConstraintName]; !exists {
						return ErrInvalid
					}
				}
				continue
			}
			for _, column := range index.Columns {
				if column.Name != "" {
					if _, exists := known[column.Name]; !exists {
						return ErrInvalid
					}
				}
			}
			if _, err := indexSQL(snapshot.DBMS, table.Table.Schema, table.Table.Name, index); err != nil {
				return err
			}
		}
	}
	return nil
}

func Compare(current, desired Snapshot) (Result, error) {
	dbms := adapters.Normalize(current.DBMS)
	if dbms != "postgresql" && dbms != "mariadb" || adapters.Normalize(desired.DBMS) != dbms {
		return Result{}, ErrInvalid
	}
	current.DBMS = dbms
	desired.DBMS = dbms
	var copyErr error
	current, copyErr = clone(current)
	if copyErr != nil {
		return Result{}, copyErr
	}
	desired, copyErr = clone(desired)
	if copyErr != nil {
		return Result{}, copyErr
	}
	currentState, copyErr := clone(current)
	if copyErr != nil {
		return Result{}, copyErr
	}
	if dbms == "mariadb" {
		for i := range desired.Tables {
			for j := range desired.Tables[i].Constraints {
				c := &desired.Tables[i].Constraints[j]
				if c.Type == "p" {
					c.Name = "PRIMARY"
				}
				if c.Type == "f" {
					if c.UpdateRule == "" || c.UpdateRule == "NO ACTION" {
						c.UpdateRule = "RESTRICT"
					}
					if c.DeleteRule == "" || c.DeleteRule == "NO ACTION" {
						c.DeleteRule = "RESTRICT"
					}
				}
			}
		}
		for i := range current.Tables {
			for j := range current.Tables[i].Constraints {
				c := &current.Tables[i].Constraints[j]
				if c.Type == "f" {
					if c.UpdateRule == "NO ACTION" {
						c.UpdateRule = "RESTRICT"
					}
					if c.DeleteRule == "NO ACTION" {
						c.DeleteRule = "RESTRICT"
					}
				}
			}
		}
	}

	before, err := tables(current)
	if err != nil {
		return Result{}, err
	}
	after, err := tables(desired)
	if err != nil {
		return Result{}, err
	}
	if err := validate(desired, after); err != nil {
		return Result{}, err
	}
	result := Result{DBMS: dbms, Changes: []Change{}, Statements: []string{}, DataChanges: []DataChange{}, Current: currentState, Desired: desired}
	phases := make([][]string, 11)
	add := func(phase int, sql string) { phases[phase] = append(phases[phase], sql) }
	change := func(kind string, t adapters.Table, name string, old, new any, destructive bool) {
		result.Changes = append(result.Changes, Change{Kind: kind, Schema: t.Schema, Table: t.Name, Name: name, Before: old, After: new, Destructive: destructive})
		result.Destructive = result.Destructive || destructive
	}
	oldSchemas := make(map[string]bool)
	newSchemas := make(map[string]bool)
	for _, schema := range current.Schemas {
		oldSchemas[schema.Name] = true
	}
	for _, schema := range desired.Schemas {
		newSchemas[schema.Name] = true
	}
	for _, schema := range keys(newSchemas) {
		if oldSchemas[schema] {
			continue
		}
		if dbms == "mariadb" {
			return result, ErrUnsupported
		}
		name, err := quote(dbms, schema)
		if err != nil {
			return result, err
		}
		add(3, "CREATE SCHEMA "+name)
		result.Changes = append(result.Changes, Change{Kind: "add_schema", Schema: schema})
	}
	for _, schema := range keys(oldSchemas) {
		if newSchemas[schema] {
			continue
		}
		if dbms == "mariadb" {
			return result, ErrUnsupported
		}
		name, err := quote(dbms, schema)
		if err != nil {
			return result, err
		}
		add(10, "DROP SCHEMA "+name)
		result.Changes = append(result.Changes, Change{Kind: "drop_schema", Schema: schema, Destructive: true})
		result.Destructive = true
	}
	typeChanged := make(map[string]bool)
	for key, old := range before {
		new, exists := after[key]
		if !exists {
			typeChanged[key] = true
			continue
		}
		a, b := columns(old), columns(new)
		for name, column := range a {
			other, exists := b[name]
			if !exists || normalizedType(dbms, column.DataType) != normalizedType(dbms, other.DataType) {
				typeChanged[key] = true
			}
		}
	}
	for _, key := range keys(before) {
		old := before[key]
		new, kept := after[key]
		if kept {
			if old.Table.Kind != new.Table.Kind && !(old.Table.Kind == "r" && new.Table.Kind == "") {
				return result, ErrUnsupported
			}
			if old.Table.Engine != "" && new.Table.Engine != "" && !strings.EqualFold(old.Table.Engine, new.Table.Engine) {
				return result, ErrUnsupported
			}
			if new.Table.Collation != "" && old.Table.Collation != new.Table.Collation {
				return result, ErrUnsupported
			}
			if old.Table.Kind != "r" && old.Table.Kind != "v" {
				oldCopy, newCopy := old, new
				oldCopy.Table.EstimatedRows = 0
				newCopy.Table.EstimatedRows = 0
				if !reflect.DeepEqual(oldCopy, newCopy) {
					return result, ErrUnsupported
				}
				continue
			}
		}

		target, _ := quote(dbms, old.Table.Schema, old.Table.Name)
		if !kept && old.Table.Kind != "r" && old.Table.Kind != "v" {
			return result, ErrUnsupported
		}
		if old.Table.Kind == "v" {
			if !kept {
				add(0, "DROP VIEW "+target)
				change("drop_view", old.Table, old.Table.Name, old, nil, true)
			} else if new.Table.Kind != "v" {
				return result, ErrUnsupported
			} else if strings.TrimSpace(old.Table.Definition) != strings.TrimSpace(new.Table.Definition) {
				sql, err := viewSQL(dbms, new)
				if err != nil {
					return result, err
				}
				add(8, sql)
				change("alter_view", new.Table, new.Table.Name, old, new, false)
			}
			continue
		}
		wanted := constraints(new)
		for _, name := range keys(constraints(old)) {
			c := constraints(old)[name]
			if !kept && c.Type != "f" {
				continue
			}
			other, exists := wanted[name]
			rebuild := c.Type == "f" && (typeChanged[key] || c.ReferencedSchema != nil && c.ReferencedTable != nil && typeChanged[tableKey(*c.ReferencedSchema, *c.ReferencedTable)])
			if !kept || !exists || !sameConstraint(c, other) || rebuild {
				sql, err := dropConstraint(dbms, target, c)
				if err != nil {
					return result, err
				}
				phase := 2
				if c.Type == "f" {
					phase = 0
				}
				add(phase, sql)
				if kept && (!exists || !sameConstraint(c, other)) {
					change("drop_constraint", old.Table, name, c, nil, false)
				}
			}
		}
		if !kept {
			add(9, "DROP TABLE "+target)
			change("drop_table", old.Table, old.Table.Name, old, nil, true)
			continue
		}
		if new.Table.Kind == "v" {
			return result, ErrUnsupported
		}
		if dbms == "mariadb" {
			for _, index := range old.Indexes {
				owner, owned := constraints(old)[index.ConstraintName]
				if !owned || owner.Type != "f" {
					continue
				}
				if _, kept := constraints(new)[index.ConstraintName]; !kept {
					indexName, _ := quote(dbms, index.Name)
					add(1, "DROP INDEX "+indexName+" ON "+target)
				}
			}
		}
		oldIndexes, newIndexes := indexes(old), indexes(new)
		for _, name := range keys(oldIndexes) {
			index := oldIndexes[name]
			other, exists := newIndexes[name]
			if exists && sameIndex(index, other) {
				continue
			}
			quoted, _ := quote(dbms, name)
			sql := "DROP INDEX " + quoted
			if dbms == "mariadb" {
				sql += " ON " + target
			} else {
				qualified, _ := quote(dbms, old.Table.Schema, name)
				sql = "DROP INDEX " + qualified
			}
			add(1, sql)
			change("drop_index", old.Table, name, index, nil, false)
		}
		a, b := columns(old), columns(new)
		for _, column := range old.Columns {
			if _, exists := b[column.Name]; !exists {
				name, _ := quote(dbms, column.Name)
				add(5, "ALTER TABLE "+target+" DROP COLUMN "+name)
				change("drop_column", old.Table, column.Name, column, nil, true)
			}
		}
		for _, column := range new.Columns {
			previous, exists := a[column.Name]
			definition, err := columnSQL(dbms, column)
			if err != nil {
				return result, err
			}
			if !exists {
				add(5, "ALTER TABLE "+target+" ADD COLUMN "+definition)
				change("add_column", new.Table, column.Name, nil, column, false)
				continue
			}
			if sameColumn(dbms, previous, column) {
				continue
			}
			if previous.Generated != column.Generated || !reflect.DeepEqual(previous.GeneratedExpression, column.GeneratedExpression) || previous.Identity != column.Identity {
				return result, ErrUnsupported
			}
			name, _ := quote(dbms, column.Name)
			if dbms == "mariadb" {
				add(5, "ALTER TABLE "+target+" MODIFY COLUMN "+definition)
			} else {
				changedType := normalizedType(dbms, previous.DataType) != normalizedType(dbms, column.DataType)
				if changedType && previous.Default != nil {
					add(5, "ALTER TABLE "+target+" ALTER COLUMN "+name+" DROP DEFAULT")
				}
				if changedType {
					add(5, "ALTER TABLE "+target+" ALTER COLUMN "+name+" TYPE "+column.DataType+" USING "+name+"::"+column.DataType)
				}
				if changedType || !reflect.DeepEqual(previous.Default, column.Default) {
					if column.Default == nil {
						if !changedType {
							add(5, "ALTER TABLE "+target+" ALTER COLUMN "+name+" DROP DEFAULT")
						}
					} else {
						add(5, "ALTER TABLE "+target+" ALTER COLUMN "+name+" SET DEFAULT "+*column.Default)
					}
				}
				if previous.Nullable != column.Nullable {
					command := " SET NOT NULL"
					if column.Nullable {
						command = " DROP NOT NULL"
					}
					add(5, "ALTER TABLE "+target+" ALTER COLUMN "+name+command)
				}
			}
			change("alter_column", new.Table, column.Name, previous, column, true)
		}
	}
	for _, key := range keys(after) {
		new := after[key]
		old, existed := before[key]
		target, _ := quote(dbms, new.Table.Schema, new.Table.Name)
		if !newSchemas[new.Table.Schema] {
			return result, ErrInvalid
		}
		if new.Table.Kind == "v" {
			if !existed {
				sql, err := viewSQL(dbms, new)
				if err != nil {
					return result, err
				}
				add(8, sql)
				change("add_view", new.Table, new.Table.Name, nil, new, false)
			}
			continue
		}
		if !existed {
			if new.Table.Kind != "" && new.Table.Kind != "r" {
				return result, ErrUnsupported
			}
			parts := make([]string, 0, len(new.Columns)+len(new.Constraints))
			for _, column := range new.Columns {
				value, err := columnSQL(dbms, column)
				if err != nil {
					return result, err
				}
				parts = append(parts, value)
			}
			for _, c := range new.Constraints {
				if c.Type == "f" {
					continue
				}
				value, err := constraintSQL(dbms, c)
				if err != nil {
					return result, err
				}
				parts = append(parts, value)
			}
			sql := "CREATE TABLE " + target + " (" + strings.Join(parts, ",") + ")"
			if dbms == "mariadb" {
				engine := new.Table.Engine
				if engine == "" {
					engine = "InnoDB"
				}
				if !strings.EqualFold(engine, "InnoDB") {
					return result, ErrUnsupported
				}
				sql += " ENGINE=InnoDB"
			}
			add(4, sql)
			change("add_table", new.Table, new.Table.Name, nil, new, false)
		}
		oldConstraints := constraints(old)
		for _, c := range new.Constraints {
			previous, exists := oldConstraints[c.Name]
			rebuild := c.Type == "f" && (typeChanged[key] || c.ReferencedSchema != nil && c.ReferencedTable != nil && typeChanged[tableKey(*c.ReferencedSchema, *c.ReferencedTable)])
			if exists && sameConstraint(previous, c) && !rebuild {
				continue
			}
			if !existed && c.Type != "f" {
				continue
			}
			value, err := constraintSQL(dbms, c)
			if err != nil {
				return result, err
			}
			phase := 6
			if c.Type == "f" {
				phase = 8
			}
			add(phase, "ALTER TABLE "+target+" ADD "+value)
			change("add_constraint", new.Table, c.Name, nil, c, false)
		}
		oldIndexes := indexes(old)
		for _, name := range keys(indexes(new)) {
			index := indexes(new)[name]
			previous, exists := oldIndexes[name]
			if exists && sameIndex(previous, index) {
				continue
			}
			sql, err := indexSQL(dbms, new.Table.Schema, new.Table.Name, index)
			if err != nil {
				return result, err
			}
			add(7, sql)
			change("add_index", new.Table, name, nil, index, false)
		}
	}
	for _, phase := range phases {
		result.Statements = append(result.Statements, phase...)
	}
	if len(result.Statements) > 0 {
		result.SQL = strings.Join(result.Statements, ";\n") + ";"
	}
	return result, nil
}

func viewSQL(dbms string, table adapters.TableDetail) (string, error) {
	name, err := quote(dbms, table.Table.Schema, table.Table.Name)
	if err != nil {
		return "", err
	}
	statements, err := adapters.ParseSQL(table.Table.Definition, dbms)
	if err != nil || len(statements) != 1 || statements[0].Command != "SELECT" && statements[0].Command != "WITH" {
		return "", ErrInvalid
	}
	return "CREATE OR REPLACE VIEW " + name + " AS " + strings.TrimSuffix(strings.TrimSpace(table.Table.Definition), ";"), nil
}
