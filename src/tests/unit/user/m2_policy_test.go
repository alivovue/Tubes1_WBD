package unit_test

import (
	"errors"
	"testing"
	"tubes1_wbd/backend/migration"
)

func TestM2DefinitionValidationRequiresAnExplicitSafeDirection(t *testing.T) {
	valid := migration.DefinitionInput{Title: "Add notes", UpSQL: "ALTER TABLE employees ADD note text;", DownSQL: "ALTER TABLE employees DROP note;"}
	if err := migration.ValidateDefinition(valid); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*migration.DefinitionInput)
	}{
		{"missing down", func(i *migration.DefinitionInput) { i.DownSQL = "" }},
		{"missing reason", func(i *migration.DefinitionInput) { i.DownSQL = ""; i.Irreversible = true }},
		{"ambiguous directions", func(i *migration.DefinitionInput) {
			i.Irreversible = true
			i.IrreversibleReason = "Cannot restore the original values"
		}},
		{"transaction control", func(i *migration.DefinitionInput) { i.DownSQL = "COMMIT" }},
		{"protected metadata", func(i *migration.DefinitionInput) { i.DownSQL = "DROP SCHEMA _schakker CASCADE" }},
		{"procedural SQL", func(i *migration.DefinitionInput) { i.UpSQL = "DO $$ BEGIN RAISE NOTICE 'unsafe'; END $$" }},
		{"blank title", func(i *migration.DefinitionInput) { i.Title = " " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			tc.change(&input)
			if err := migration.ValidateDefinition(input); !errors.Is(err, migration.ErrDefinitionInvalid) {
				t.Fatalf("invalid content accepted: %v", err)
			}
		})
	}
	valid.DownSQL = ""
	valid.Irreversible = true
	valid.IrreversibleReason = "The deleted values cannot be reconstructed"
	if err := migration.ValidateDefinition(valid); err != nil {
		t.Fatal(err)
	}
}
func TestM2RevisionHashBindsBothSQLDirectionsAndReason(t *testing.T) {
	base := migration.ContentHash("SELECT 1", "SELECT 2", false, "")
	for _, different := range []string{
		migration.ContentHash("SELECT 1 ", "SELECT 2", false, ""),
		migration.ContentHash("SELECT 1", "SELECT 3", false, ""),
		migration.ContentHash("SELECT 1", "", true, "Cannot recover source data"),
		migration.ContentHash("a", "bc", false, ""), migration.ContentHash("ab", "c", false, ""),
	} {
		if different == base {
			t.Fatal("content change reused the revision hash")
		}
	}
	if migration.ContentHash("a", "bc", false, "") == migration.ContentHash("ab", "c", false, "") {
		t.Fatal("ambiguous hash framing")
	}
}
