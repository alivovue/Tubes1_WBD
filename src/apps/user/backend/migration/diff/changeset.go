package diff

import (
	"sort"
	"strings"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

func Build(current Snapshot, operations []Operation) (Result, error) {
	if len(operations) == 0 || len(operations) > 500 {
		return Result{}, ErrInvalid
	}
	desired, err := clone(current)
	if err != nil {
		return Result{}, err
	}
	data := make([]Operation, 0)
	drops := make([]Operation, 0)
	for _, op := range operations {
		position := -1
		for i, table := range desired.Tables {
			if table.Table.Schema == op.Schema && table.Table.Name == op.Table {
				position = i
				break
			}
		}
		switch op.Kind {
		case "insert_row", "update_row":
			data = append(data, op)
		case "create_table":
			if position >= 0 || op.Definition == nil {
				return Result{}, ErrInvalid
			}
			table := *op.Definition
			table.Table.Schema = op.Schema
			table.Table.Name = op.Table
			desired.Tables = append(desired.Tables, table)
			exists := false
			for _, schema := range desired.Schemas {
				if schema.Name == op.Schema {
					exists = true
				}
			}
			if !exists {
				desired.Schemas = append(desired.Schemas, adapters.Schema{Name: op.Schema})
			}
		case "drop_table":
			if position < 0 {
				return Result{}, ErrInvalid
			}
			desired.Tables = append(desired.Tables[:position], desired.Tables[position+1:]...)
			drops = append(drops, op)
		case "add_column":
			if position < 0 {
				return Result{}, ErrInvalid
			}
			table := &desired.Tables[position]
			for _, column := range table.Columns {
				if column.Name == op.Column.Name {
					return Result{}, ErrInvalid
				}
			}
			column := op.Column
			column.Position = len(table.Columns) + 1
			table.Columns = append(table.Columns, column)
		case "alter_column":
			if position < 0 {
				return Result{}, ErrInvalid
			}
			found := false
			for i, column := range desired.Tables[position].Columns {
				if column.Name == op.Column.Name {
					next := op.Column
					next.Position = column.Position
					desired.Tables[position].Columns[i] = next
					found = true
				}
			}
			if !found {
				return Result{}, ErrInvalid
			}
		case "drop_column":
			if position < 0 {
				return Result{}, ErrInvalid
			}
			table := &desired.Tables[position]
			found := false
			for i, column := range table.Columns {
				if column.Name == op.Column.Name {
					table.Columns = append(table.Columns[:i], table.Columns[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				return Result{}, ErrInvalid
			}
			drops = append(drops, op)
		case "add_index":
			if position < 0 {
				return Result{}, ErrInvalid
			}
			for _, index := range desired.Tables[position].Indexes {
				if index.Name == op.Index.Name {
					return Result{}, ErrInvalid
				}
			}
			desired.Tables[position].Indexes = append(desired.Tables[position].Indexes, op.Index)
		case "drop_index":
			if position < 0 {
				return Result{}, ErrInvalid
			}
			found := false
			for i, index := range desired.Tables[position].Indexes {
				if index.Name == op.Index.Name {
					desired.Tables[position].Indexes = append(desired.Tables[position].Indexes[:i], desired.Tables[position].Indexes[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				return Result{}, ErrInvalid
			}
		default:
			return Result{}, ErrUnsupported
		}
	}
	if err := resolveDropDependencies(&desired, drops); err != nil {
		return Result{}, err
	}
	result, err := Compare(current, desired)
	if err != nil {
		return result, err
	}
	all, _ := tables(desired)
	for _, op := range data {
		table, exists := all[tableKey(op.Schema, op.Table)]
		if !exists || table.Table.Kind == "v" {
			return result, ErrInvalid
		}
		known := columns(table)
		for name := range op.Values {
			if _, exists := known[name]; !exists {
				return result, ErrInvalid
			}
		}
		for name := range op.Where {
			if _, exists := known[name]; !exists {
				return result, ErrInvalid
			}
		}
		target, err := quote(result.DBMS, op.Schema, op.Table)
		if err != nil {
			return result, err
		}
		names := make([]string, 0, len(op.Values))
		for name := range op.Values {
			names = append(names, name)
		}
		sort.Strings(names)
		quoted := make([]string, 0, len(names))
		values := make([]string, 0, len(names))
		assignments := make([]string, 0, len(names))
		for _, name := range names {
			column, err := quote(result.DBMS, name)
			if err != nil {
				return result, err
			}
			value, err := literal(result.DBMS, op.Values[name])
			if err != nil {
				return result, err
			}
			quoted = append(quoted, column)
			values = append(values, value)
			assignments = append(assignments, column+"="+value)
		}
		var sql string
		if op.Kind == "insert_row" {
			if len(names) == 0 {
				if result.DBMS == "mariadb" {
					sql = "INSERT INTO " + target + " () VALUES ()"
				} else {
					sql = "INSERT INTO " + target + " DEFAULT VALUES"
				}
			} else {
				sql = "INSERT INTO " + target + " (" + strings.Join(quoted, ",") + ") VALUES (" + strings.Join(values, ",") + ")"
			}
		} else {
			if len(op.Where) == 0 || len(op.Values) == 0 {
				return result, ErrInvalid
			}
			filters := make([]string, 0, len(op.Where))
			for _, name := range keys(op.Where) {
				column, _ := quote(result.DBMS, name)
				if op.Where[name] == nil {
					filters = append(filters, column+" IS NULL")
				} else {
					value, err := literal(result.DBMS, op.Where[name])
					if err != nil {
						return result, err
					}
					filters = append(filters, column+"="+value)
				}
			}
			sql = "UPDATE " + target + " SET " + strings.Join(assignments, ",") + " WHERE " + strings.Join(filters, " AND ")
		}
		result.Statements = append(result.Statements, sql)
		result.DataChanges = append(result.DataChanges, DataChange{Kind: op.Kind, Schema: op.Schema, Table: op.Table, Values: copyValues(op.Values), Where: copyValues(op.Where)})
	}
	if len(result.Statements) > 0 {
		result.SQL = strings.Join(result.Statements, ";\n") + ";"
	}
	return result, nil
}

func copyValues(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		if data, ok := value.([]byte); ok {
			result[key] = append([]byte(nil), data...)
		} else {
			result[key] = value
		}
	}
	return result
}
