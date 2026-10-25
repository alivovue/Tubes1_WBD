package unit_test

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/diff"
)

func emptySnapshot(dbms string) adapters.Snapshot {
	schema := "public"
	if dbms == "mariadb" {
		schema = "target"
	}
	return adapters.Snapshot{DBMS: dbms, Schemas: []adapters.Schema{{Name: schema}}, Tables: []adapters.TableDetail{}}
}
func peopleSnapshot(dbms string) adapters.Snapshot {
	snapshot := emptySnapshot(dbms)
	primary := "people_pkey"
	if dbms == "mariadb" {
		primary = "PRIMARY"
	}
	snapshot.Tables = []adapters.TableDetail{{Table: adapters.Table{Schema: snapshot.Schemas[0].Name, Name: "people", Kind: "r"}, Columns: []adapters.Column{{Name: "id", Position: 1, DataType: "integer"}, {Name: "name", Position: 2, DataType: "varchar(80)", Nullable: true}}, Constraints: []adapters.Constraint{{Name: primary, Type: "p", Columns: []string{"id"}}}}}
	return snapshot
}
func copySnapshot(t *testing.T, snapshot adapters.Snapshot) adapters.Snapshot {
	t.Helper()
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var copy adapters.Snapshot
	if err := json.Unmarshal(data, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}
func hasChange(result diff.Result, kind string) bool {
	for _, change := range result.Changes {
		if change.Kind == kind {
			return true
		}
	}
	return false
}
func stringPointer(value string) *string { return &value }

func TestSchemaDiffComparesActualStatesWithoutMutatingInputs(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			current := peopleSnapshot(dbms)
			before := copySnapshot(t, current)
			same, err := diff.Compare(current, current)
			if err != nil {
				t.Fatal(err)
			}
			if same.SQL != "" || len(same.Changes) != 0 {
				t.Fatal("identical schemas generated a migration")
			}
			desired := copySnapshot(t, current)
			desired.Tables[0].Columns = append(desired.Tables[0].Columns, adapters.Column{Name: "age", DataType: "integer", Nullable: true, Position: 3})
			desiredBefore := copySnapshot(t, desired)
			result, err := diff.Compare(current, desired)
			if err != nil {
				t.Fatal(err)
			}
			if !hasChange(result, "add_column") || !strings.Contains(result.SQL, "ADD COLUMN") || len(result.DataChanges) != 0 {
				t.Fatalf("real column difference missing: %#v", result)
			}
			if !reflect.DeepEqual(result.Current, before) || len(result.Desired.Tables) != len(desiredBefore.Tables) {
				t.Fatal("comparison result does not retain the complete before and after schema states")
			}
			if !reflect.DeepEqual(current, before) || !reflect.DeepEqual(desired, desiredBefore) {
				t.Fatal("comparison mutated caller schema states")
			}
		})
	}
}
func TestSchemaDiffGeneratesStructuralChanges(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			current := peopleSnapshot(dbms)
			for _, tc := range []struct {
				name, kind, sql string
				change          func(*adapters.Snapshot)
				destructive     bool
			}{
				{"drop column", "drop_column", "DROP COLUMN", func(s *adapters.Snapshot) { s.Tables[0].Columns = s.Tables[0].Columns[:1] }, true},
				{"alter type", "alter_column", "bigint", func(s *adapters.Snapshot) { s.Tables[0].Columns[0].DataType = "bigint" }, true},
				{"add unique constraint", "add_constraint", "UNIQUE", func(s *adapters.Snapshot) {
					s.Tables[0].Constraints = append(s.Tables[0].Constraints, adapters.Constraint{Name: "people_name_key", Type: "u", Columns: []string{"name"}})
				}, false},
				{"add index", "add_index", "CREATE INDEX", func(s *adapters.Snapshot) {
					s.Tables[0].Indexes = append(s.Tables[0].Indexes, adapters.Index{Name: "people_name_idx", Method: "btree", Columns: []adapters.IndexColumn{{Name: "name"}}})
				}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					desired := copySnapshot(t, current)
					tc.change(&desired)
					result, err := diff.Compare(current, desired)
					if err != nil {
						t.Fatal(err)
					}
					if !hasChange(result, tc.kind) || !strings.Contains(result.SQL, tc.sql) || result.Destructive != tc.destructive {
						t.Fatalf("incorrect structural diff: %#v", result)
					}
				})
			}
		})
	}
}
func TestChangeSetCreatesAndDropsTables(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			empty := emptySnapshot(dbms)
			schema := empty.Schemas[0].Name
			definition := peopleSnapshot(dbms).Tables[0]
			created, err := diff.Build(empty, []diff.Operation{{Kind: "create_table", Schema: schema, Table: "people", Definition: &definition}})
			if err != nil {
				t.Fatal(err)
			}
			if len(created.Changes) != 1 || !hasChange(created, "add_table") || !strings.Contains(created.SQL, "CREATE TABLE") {
				t.Fatal("create-table candidate incomplete")
			}
			removed, err := diff.Compare(created.Desired, empty)
			if err != nil {
				t.Fatal(err)
			}
			if !hasChange(removed, "drop_table") || !removed.Destructive || !strings.HasPrefix(removed.SQL, "DROP TABLE") {
				t.Fatalf("drop-table candidate incomplete: %s", removed.SQL)
			}
		})
	}
}
func TestChangeSetOrdersSchemaBeforeRowsAndSeparatesSummaries(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			current := peopleSnapshot(dbms)
			schema := current.Schemas[0].Name
			result, err := diff.Build(current, []diff.Operation{
				{Kind: "insert_row", Schema: schema, Table: "people", Values: map[string]any{"id": 1, "age": 23}},
				{Kind: "add_column", Schema: schema, Table: "people", Column: adapters.Column{Name: "age", DataType: "integer", Nullable: true}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Changes) != 1 || len(result.DataChanges) != 1 || strings.Index(result.SQL, "ADD COLUMN") > strings.Index(result.SQL, "INSERT INTO") {
				t.Fatalf("batch dependencies or summaries incorrect: %s", result.SQL)
			}
			if result.Changes[0].Kind == "insert_row" {
				t.Fatal("row insertion appeared as a schema diff")
			}
		})
	}
}
func TestForeignKeysFollowAllNewTableDefinitions(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			current := emptySnapshot(dbms)
			schema := current.Schemas[0].Name
			parent := adapters.TableDetail{Columns: []adapters.Column{{Name: "id", DataType: "integer"}}, Constraints: []adapters.Constraint{{Name: "parent_pkey", Type: "p", Columns: []string{"id"}}}}
			child := adapters.TableDetail{Columns: []adapters.Column{{Name: "parent_id", DataType: "integer"}}, Constraints: []adapters.Constraint{{Name: "child_parent_fk", Type: "f", Columns: []string{"parent_id"}, ReferencedSchema: &schema, ReferencedTable: stringPointer("parents"), ReferencedColumns: []string{"id"}}}}
			result, err := diff.Build(current, []diff.Operation{{Kind: "create_table", Schema: schema, Table: "children", Definition: &child}, {Kind: "create_table", Schema: schema, Table: "parents", Definition: &parent}})
			if err != nil {
				t.Fatal(err)
			}
			if strings.LastIndex(result.SQL, "CREATE TABLE") > strings.Index(result.SQL, "FOREIGN KEY") {
				t.Fatal("foreign key was generated before referenced tables")
			}
		})
	}
}
func TestGeneratedRowsEscapeValuesAndRestrictUpdates(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			current := peopleSnapshot(dbms)
			schema := current.Schemas[0].Name
			dangerous := "O'Brien; COMMIT; --"
			result, err := diff.Build(current, []diff.Operation{{Kind: "insert_row", Schema: schema, Table: "people", Values: map[string]any{"id": 1, "name": dangerous}}})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.SQL, "'O''Brien; COMMIT; --'") || len(result.Changes) != 0 || len(result.DataChanges) != 1 {
				t.Fatal("row literal was not safely encoded or was classified as schema")
			}
			statements, err := adapters.ParseSQL(result.SQL, dbms)
			if err != nil || len(statements) != 1 || statements[0].Command != "INSERT" {
				t.Fatal("row value escaped its generated SQL statement")
			}
			_, err = diff.Build(current, []diff.Operation{{Kind: "update_row", Schema: schema, Table: "people", Values: map[string]any{"name": "everyone"}}})
			wantError(t, err, diff.ErrInvalid)
			filtered, err := diff.Build(current, []diff.Operation{{Kind: "update_row", Schema: schema, Table: "people", Values: map[string]any{"name": "fixed"}, Where: map[string]any{"id": 1, "name": nil}}})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(filtered.SQL, "IS NULL") || !strings.Contains(filtered.SQL, " WHERE ") {
				t.Fatal("update filter not retained")
			}
		})
	}
}
func TestGeneratedValueEncodingAndValidation(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			current := peopleSnapshot(dbms)
			schema := current.Schemas[0].Name
			for _, value := range []any{nil, true, false, int8(-2), int16(3), int32(4), int64(5), uint(6), uint8(7), uint16(8), uint32(9), uint64(10), float32(1.25), float64(2.5), json.Number("42"), []byte{0, 1, 255}, time.Date(2026, 10, 25, 12, 30, 0, 0, time.FixedZone("offset", 7*3600))} {
				result, err := diff.Build(current, []diff.Operation{{Kind: "insert_row", Schema: schema, Table: "people", Values: map[string]any{"name": value}}})
				if err != nil || result.SQL == "" {
					t.Fatalf("supported literal %T rejected: %v", value, err)
				}
			}
			for _, value := range []any{math.NaN(), math.Inf(1), "null\x00byte", string([]byte{0xff}), json.Number("1; DROP TABLE people"), struct{}{}} {
				_, err := diff.Build(current, []diff.Operation{{Kind: "insert_row", Schema: schema, Table: "people", Values: map[string]any{"name": value}}})
				wantError(t, err, diff.ErrInvalid)
			}
		})
	}
}
func TestChangeSetRejectsInvalidIdentifiersTypesAndExpressions(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			current := peopleSnapshot(dbms)
			schema := current.Schemas[0].Name
			for _, column := range []adapters.Column{{Name: "bad\x00name", DataType: "integer"}, {Name: "unsafe", DataType: "integer); DROP TABLE people; --"}, {Name: "unsafe", DataType: "integer", Default: stringPointer("0; DROP TABLE people")}, {Name: "unsafe", DataType: "integer", Default: stringPointer("0 /* hidden */")}} {
				_, err := diff.Build(current, []diff.Operation{{Kind: "add_column", Schema: schema, Table: "people", Column: column}})
				wantError(t, err, diff.ErrInvalid)
			}
			_, err := diff.Build(current, nil)
			wantError(t, err, diff.ErrInvalid)
			_, err = diff.Build(current, []diff.Operation{{Kind: "unknown", Schema: schema, Table: "people"}})
			wantError(t, err, diff.ErrUnsupported)
			_, err = diff.Build(current, []diff.Operation{{Kind: "insert_row", Schema: schema, Table: "people", Values: map[string]any{"nonexistent": 1}}})
			wantError(t, err, diff.ErrInvalid)
		})
	}
	_, err := diff.Compare(emptySnapshot("postgresql"), emptySnapshot("mariadb"))
	wantError(t, err, diff.ErrInvalid)
}
func TestChangeSetCopiesCallerOwnedRowValues(t *testing.T) {
	current := peopleSnapshot("postgresql")
	payload := []byte("original")
	values := map[string]any{"name": payload}
	result, err := diff.Build(current, []diff.Operation{{Kind: "insert_row", Schema: "public", Table: "people", Values: values}})
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 'x'
	values["id"] = 2
	if string(result.DataChanges[0].Values["name"].([]byte)) != "original" || len(result.DataChanges[0].Values) != 1 {
		t.Fatal("preview retained mutable caller row values")
	}
}
