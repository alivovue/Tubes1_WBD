package diff

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

var ErrDependency = errors.New("schema change has dependent objects")

type DependencyError struct {
	DBMS, Kind, Schema, Table, Column string
	Objects                           []string
}

func (e *DependencyError) Error() string {
	target := e.Schema + "." + e.Table
	if e.Column != "" {
		target += "." + e.Column
	}
	message := fmt.Sprintf("Cannot drop %s %s because these objects depend on it: %s. Remove or change these dependencies explicitly in the SQL editor, then retry.", e.Kind, target, strings.Join(e.Objects, "; "))
	if adapters.Normalize(e.DBMS) == "postgresql" {
		message += " PostgreSQL DROP ... CASCADE can remove dependent objects; review every affected object before using it."
	}
	return message
}

func (e *DependencyError) Unwrap() error { return ErrDependency }

func referencesColumn(dbms, expression, column string) bool {
	if expression == "" {
		return false
	}
	statements, err := adapters.ParseSQL("SELECT "+expression, dbms)
	if err != nil {
		return true
	}
	for _, statement := range statements {
		for i, token := range statement.Tokens {
			if token.Literal || i == 0 || i+1 < len(statement.Tokens) && (adapters.IsWord(statement.Tokens[i+1], "(") || adapters.IsWord(statement.Tokens[i+1], ".")) {
				continue
			}
			name := token.Value
			if adapters.Normalize(dbms) == "mariadb" {
				if strings.EqualFold(name, column) {
					return true
				}
			} else {
				if !token.Quoted {
					name = strings.ToLower(name)
				}
				if name == column {
					return true
				}
			}
		}
	}
	return false
}

func resolveDropDependencies(desired *Snapshot, drops []Operation) error {
	for _, drop := range drops {
		position := -1
		for i, table := range desired.Tables {
			if table.Table.Schema == drop.Schema && table.Table.Name == drop.Table {
				position = i
				break
			}
		}
		column := drop.Column.Name
		if drop.Kind == "drop_column" {
			if position < 0 {
				continue
			}
			if _, kept := columns(desired.Tables[position])[column]; kept {
				continue
			}
		} else if position >= 0 {
			continue
		}
		objects := make([]string, 0)
		for i := range desired.Tables {
			table := &desired.Tables[i]
			remainingColumns := columns(*table)
			for _, constraint := range table.Constraints {
				if constraint.Type != "f" || constraint.ReferencedSchema == nil || constraint.ReferencedTable == nil || *constraint.ReferencedSchema != drop.Schema || *constraint.ReferencedTable != drop.Table {
					continue
				}
				removed := false
				for _, ownColumn := range constraint.Columns {
					if _, kept := remainingColumns[ownColumn]; !kept {
						removed = true
					}
				}
				if removed {
					continue
				}
				if drop.Kind == "drop_column" && (!slices.Contains(constraint.ReferencedColumns, column) || i == position && slices.Contains(constraint.Columns, column)) {
					continue
				}
				objects = append(objects, fmt.Sprintf("foreign key %s on %s.%s", constraint.Name, table.Table.Schema, table.Table.Name))
			}
			if i == position && drop.Kind == "drop_column" {
				for _, dependent := range table.Columns {
					if dependent.GeneratedExpression != nil && referencesColumn(desired.DBMS, *dependent.GeneratedExpression, column) {
						objects = append(objects, "generated column "+dependent.Name)
					}
				}
			}
		}
		if len(objects) > 0 {
			sort.Strings(objects)
			kind := "column"
			if drop.Kind == "drop_table" {
				kind, column = "table", ""
			}
			return &DependencyError{DBMS: desired.DBMS, Kind: kind, Schema: drop.Schema, Table: drop.Table, Column: column, Objects: objects}
		}
		if position < 0 {
			continue
		}
		table := &desired.Tables[position]
		removedConstraints := map[string]bool{}
		table.Constraints = slices.DeleteFunc(table.Constraints, func(constraint adapters.Constraint) bool {
			remove := slices.Contains(constraint.Columns, column) || constraint.Type == "c" && referencesColumn(desired.DBMS, constraint.Expression, column)
			if remove {
				removedConstraints[constraint.Name] = true
			}
			return remove
		})
		table.Indexes = slices.DeleteFunc(table.Indexes, func(index adapters.Index) bool {
			if removedConstraints[index.ConstraintName] || slices.Contains(index.Include, column) || referencesColumn(desired.DBMS, index.Predicate, column) {
				return true
			}
			for _, key := range index.Columns {
				if key.Name == column || referencesColumn(desired.DBMS, key.Expression, column) {
					return true
				}
			}
			return false
		})
	}
	return nil
}
