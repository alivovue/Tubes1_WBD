package integration_test

import (
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
)

func TestPostgresUserSchemasStartingWithPGRemainVisibleAndFingerprintChanges(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewOnly)
	_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE SCHEMA pgtest; CREATE TABLE pgtest.visible(id bigint PRIMARY KEY)`)
	must(t, err)
	schemas, err := f.databases.Schemas(f.ctx, f.editor, target.ID)
	must(t, err)
	found := false
	for _, schema := range schemas {
		if strings.HasPrefix(schema.Name, "pg_") || schema.Name == "_schakker" || schema.Name == "information_schema" {
			t.Fatalf("internal schema leaked: %s", schema.Name)
		}
		found = found || schema.Name == "pgtest"
	}
	if !found {
		t.Fatal("pgtest schema hidden by a wildcard prefix filter")
	}
	tables, err := f.databases.Tables(f.ctx, f.editor, target.ID, "pgtest")
	must(t, err)
	if len(tables) != 1 || tables[0].Name != "visible" {
		t.Fatal("user schema cannot be explored")
	}
	before, err := f.databases.Snapshot(f.ctx, f.editor, target.ID)
	must(t, err)
	for _, statement := range []string{
		`ALTER TABLE pgtest.visible ADD COLUMN note text`,
		`CREATE INDEX note_idx ON pgtest.visible(note)`,
		`CREATE VIEW pgtest.visible_view AS SELECT id FROM pgtest.visible`,
		`CREATE TYPE pgtest.status AS ENUM ('ready','done')`,
	} {
		_, err = f.dbs.Managed.ExecContext(f.ctx, statement)
		must(t, err)
		after, err := f.databases.Snapshot(f.ctx, f.editor, target.ID)
		must(t, err)
		if before.Fingerprint == after.Fingerprint {
			t.Fatalf("schema change ignored by fingerprint: %s", statement)
		}
		before = after
	}
}
