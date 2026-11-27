package sqltext_test

import (
	"reflect"
	"schakker_shared/sqltext"
	"testing"
)

func TestPostgresRelationsPreserveNamesAndIgnoreCommentsAndLiterals(t *testing.T) {
	refs, all, err := sqltext.PostgresRelations(`SELECT 'FROM hidden' FROM "Odd Schema"."Odd Table"; -- UPDATE hidden
 SELECT note FROM public.employees`)
	if err != nil || all {
		t.Fatalf("simple references: %v %v", all, err)
	}
	want := []sqltext.Relation{{Schema: "Odd Schema", Name: "Odd Table"}, {Schema: "public", Name: "employees"}}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("references=%#v", refs)
	}
	refs, all, err = sqltext.PostgresRelations(`UPDATE public.counters SET value=42`)
	if err != nil || all || len(refs) != 1 || refs[0].Name != "counters" {
		t.Fatalf("typed UPDATE relation missing: %#v %v %v", refs, all, err)
	}
	refs, all, err = sqltext.PostgresRelations(`CREATE TABLE unrelated(id integer)`)
	if err != nil || all || len(refs) != 1 || refs[0].Name != "unrelated" {
		t.Fatalf("plain DDL scope: %#v %v %v", refs, all, err)
	}
}
func TestPostgresRelationsConservativelyHandleRuntimeEffects(t *testing.T) {
	for _, query := range []string{
		`SELECT hidden_update()`, `UPDATE counters SET value=value+1`,
		`INSERT INTO counters SELECT '1'::custom_type`, `CREATE TABLE example(value public.custom_domain)`,
		`DROP TABLE unrelated`, `ALTER TABLE counters RENAME TO renamed`,
	} {
		_, all, err := sqltext.PostgresRelations(query)
		if err != nil || !all {
			t.Fatalf("runtime effects were assumed isolated for %q: %v %v", query, all, err)
		}
	}
	if _, _, err := sqltext.PostgresRelations(`CREATE TABLE bad(id integer,,)`); err == nil {
		t.Fatal("malformed SQL accepted")
	}
}
