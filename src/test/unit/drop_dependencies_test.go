package unit_test

import (
	"reflect"
	"strings"
	"testing"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/diff"
)

func TestDropColumnRemovesItsLocalIndexesAndConstraints(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			current := peopleSnapshot(dbms)
			table := &current.Tables[0]
			table.Constraints = append(table.Constraints,
				adapters.Constraint{Name: "people_name_key", Type: "u", Columns: []string{"name"}},
				adapters.Constraint{Name: "people_name_check", Type: "c", Expression: "length(name) > 0"})
			table.Indexes = []adapters.Index{
				{Name: "people_id_idx", Columns: []adapters.IndexColumn{{Name: "id"}}},
				{Name: "people_name_idx", Columns: []adapters.IndexColumn{{Name: "name"}}},
			}
			if dbms == "postgresql" {
				table.Indexes = append(table.Indexes,
					adapters.Index{Name: "people_lower_idx", Columns: []adapters.IndexColumn{{Expression: "lower(name)"}}},
					adapters.Index{Name: "people_partial_idx", Columns: []adapters.IndexColumn{{Name: "id"}}, Predicate: "name IS NOT NULL"},
					adapters.Index{Name: "people_include_idx", Columns: []adapters.IndexColumn{{Name: "id"}}, Include: []string{"name"}})
			}
			before := copySnapshot(t, current)
			result, err := diff.Build(current, []diff.Operation{{Kind: "drop_column", Schema: current.Schemas[0].Name, Table: "people", Column: adapters.Column{Name: "name"}}})
			if err != nil {
				t.Fatal(err)
			}
			desired := result.Desired.Tables[0]
			if len(desired.Columns) != 1 || desired.Columns[0].Name != "id" || len(desired.Constraints) != 1 || desired.Constraints[0].Type != "p" || len(desired.Indexes) != 1 || desired.Indexes[0].Name != "people_id_idx" {
				t.Fatalf("dependent objects were not removed correctly: %#v", desired)
			}
			if !hasChange(result, "drop_constraint") || !hasChange(result, "drop_index") || !hasChange(result, "drop_column") || strings.Contains(result.SQL, "CASCADE") || !result.Destructive {
				t.Fatalf("drop candidate is incomplete: %s", result.SQL)
			}
			if !reflect.DeepEqual(current, before) {
				t.Fatal("dependency cleanup changed the caller's snapshot")
			}
		})
	}
}

func TestDropPrimaryKeyColumnAndMariaDBAutoIncrement(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			current := peopleSnapshot(dbms)
			if dbms == "mariadb" {
				current.Tables[0].Columns[0].Identity = "auto_increment"
			}
			current.Tables[0].Indexes = []adapters.Index{{Name: current.Tables[0].Constraints[0].Name, Primary: true, ConstraintOwned: true, Columns: []adapters.IndexColumn{{Name: "id"}}}}
			result, err := diff.Build(current, []diff.Operation{{Kind: "drop_column", Schema: current.Schemas[0].Name, Table: "people", Column: adapters.Column{Name: "id"}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Desired.Tables[0].Constraints) != 0 || len(result.Desired.Tables[0].Indexes) != 0 {
				t.Fatal("primary key or its backing index survived the column drop")
			}
			if dbms == "mariadb" && !strings.Contains(result.SQL, "DROP PRIMARY KEY, DROP COLUMN `id`") {
				t.Fatalf("AUTO_INCREMENT column and key must be dropped in the same ALTER: %s", result.SQL)
			}
		})
	}
}

func TestDropForeignKeyDependencyRequiresExplicitChanges(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			current := peopleSnapshot(dbms)
			schema := current.Schemas[0].Name
			current.Tables = append(current.Tables, adapters.TableDetail{
				Table:       adapters.Table{Schema: schema, Name: "children", Kind: "r"},
				Columns:     []adapters.Column{{Name: "parent_id", DataType: "integer"}, {Name: "note", DataType: "text", Nullable: true}},
				Constraints: []adapters.Constraint{{Name: "children_parent_fk", Type: "f", Columns: []string{"parent_id"}, ReferencedSchema: &schema, ReferencedTable: stringPointer("people"), ReferencedColumns: []string{"id"}}},
			})
			parentDrop := diff.Operation{Kind: "drop_column", Schema: schema, Table: "people", Column: adapters.Column{Name: "id"}}
			for _, operation := range []diff.Operation{parentDrop, {Kind: "drop_table", Schema: schema, Table: "people"}} {
				_, err := diff.Build(current, []diff.Operation{operation})
				wantError(t, err, diff.ErrDependency)
				if !strings.Contains(err.Error(), "children_parent_fk") || !strings.Contains(err.Error(), schema+".children") || !strings.Contains(err.Error(), "SQL editor") {
					t.Fatalf("dependency error does not identify the blocker and remedy: %v", err)
				}
				if strings.Contains(err.Error(), "CASCADE") != (dbms == "postgresql") {
					t.Fatalf("CASCADE guidance does not match the database: %v", err)
				}
			}
			for _, childDrop := range []diff.Operation{
				{Kind: "drop_column", Schema: schema, Table: "children", Column: adapters.Column{Name: "parent_id"}},
				{Kind: "drop_table", Schema: schema, Table: "children"},
			} {
				if _, err := diff.Build(current, []diff.Operation{parentDrop, childDrop}); err != nil {
					t.Fatalf("explicitly removed dependency still blocks the batch: %v", err)
				}
			}
		})
	}
}

func TestDropColumnDetectsGeneratedDependenciesAndIgnoresStringLiterals(t *testing.T) {
	current := peopleSnapshot("postgresql")
	current.Tables[0].Indexes = []adapters.Index{{Name: "literal_name_idx", Columns: []adapters.IndexColumn{{Expression: "CASE WHEN id > 0 THEN 'name' ELSE '' END"}}}}
	drop := diff.Operation{Kind: "drop_column", Schema: "public", Table: "people", Column: adapters.Column{Name: "name"}}
	result, err := diff.Build(current, []diff.Operation{drop})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Desired.Tables[0].Indexes) != 1 {
		t.Fatal("a string literal was mistaken for a column reference")
	}
	current.Tables[0].Columns = append(current.Tables[0].Columns, adapters.Column{Name: "normalized_name", DataType: "text", Generated: "s", GeneratedExpression: stringPointer("lower(name)")})
	_, err = diff.Build(current, []diff.Operation{drop})
	wantError(t, err, diff.ErrDependency)
	if !strings.Contains(err.Error(), "generated column normalized_name") {
		t.Fatalf("generated dependency was not identified: %v", err)
	}
}
