package integration_test

import (
	"bytes"
	"fmt"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
)

func TestIT09ValidateAndRegisterPostgreSQLConnection(t *testing.T) {
	f := newFixture(t, true)
	input := f.dbs.Connection()
	must(t, f.databases.TestConnection(f.ctx, f.admin, 0, input))
	var count int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM managed_databases`).Scan(&count))
	if count != 0 {
		t.Fatal("connection testing saved metadata")
	}
	saved, err := f.databases.SaveConnection(f.ctx, f.admin, 0, input)
	must(t, err)
	metadata, err := f.store.DatabaseMetadata(f.ctx, saved.ID)
	must(t, err)
	if metadata.DBMS != "postgresql" || metadata.DatabaseName != f.dbs.ManagedName || metadata.Username != input.Username || !metadata.Active {
		t.Fatal("registered connection metadata differs from validated settings")
	}
	if len(metadata.EncryptedPassword) == 0 || bytes.Equal(metadata.EncryptedPassword, []byte(input.Password)) {
		t.Fatal("database credentials were not encrypted")
	}
	var superuser bool
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT rolsuper FROM pg_roles WHERE rolname=current_user`).Scan(&superuser))
	if superuser {
		t.Fatal("managed fixture connection unexpectedly uses a superuser")
	}
}
func TestIT10ExploreRealSchemasTablesColumnsIndexesAndRows(t *testing.T) {
	f := newFixture(t, true)
	user := f.user(t, "it.reader")
	target := f.register(t)
	f.grant(t, user.ID, target.ID, access.ViewOnly)
	schemas, err := f.databases.Schemas(f.ctx, user.ID, target.ID)
	must(t, err)
	found := false
	for _, schema := range schemas {
		if schema.Name == "public" {
			found = true
		}
	}
	if !found {
		t.Fatal("public schema missing")
	}
	tables, err := f.databases.Tables(f.ctx, user.ID, target.ID, "public")
	must(t, err)
	found = false
	for _, table := range tables {
		if table.Name == "employees" {
			found = true
		}
	}
	if !found {
		t.Fatal("fixture employees table missing")
	}
	detail, err := f.databases.DescribeTable(f.ctx, user.ID, target.ID, "public", "employees")
	must(t, err)
	if len(detail.Columns) != 3 || detail.Columns[0].Name != "id" || detail.Columns[0].DataType != "integer" || detail.Columns[1].Name != "name" || detail.Columns[1].Nullable || detail.Columns[2].Name != "salary" {
		t.Fatalf("real column metadata not returned: %#v", detail.Columns)
	}
	primary := false
	for _, constraint := range detail.Constraints {
		if constraint.Type == "p" && len(constraint.Columns) == 1 && constraint.Columns[0] == "id" {
			primary = true
		}
	}
	if !primary {
		t.Fatal("real primary key missing")
	}
	indexed := false
	for _, index := range detail.Indexes {
		if index.Name == "employees_salary_idx" {
			indexed = true
		}
	}
	if !indexed {
		t.Fatal("real index missing")
	}
	page, err := f.databases.TableData(f.ctx, user.ID, target.ID, "public", "employees", dbmanagement.PageOptions{Limit: 10})
	must(t, err)
	if len(page.Rows) != 2 || fmt.Sprint(page.Rows[0][1]) != "Alice" || fmt.Sprint(page.Rows[0][2]) != "5000" || fmt.Sprint(page.Rows[1][1]) != "Bob" || fmt.Sprint(page.Rows[1][2]) != "6000" {
		t.Fatalf("unexpected employees fixture rows: %#v", page.Rows)
	}
	page, err = f.databases.TableData(f.ctx, user.ID, target.ID, "public", "employees", dbmanagement.PageOptions{Limit: 1, Offset: 1})
	must(t, err)
	if len(page.Rows) != 1 || fmt.Sprint(page.Rows[0][1]) != "Bob" || page.HasMore {
		t.Fatal("real table pagination is incorrect")
	}
}
func TestIT11DisableConnectionRemovesAccessAndRevokesSessions(t *testing.T) {
	f := newFixture(t, true)
	user := f.user(t, "it.reader")
	target := f.register(t)
	f.grant(t, user.ID, target.ID, access.ViewOnly)
	_, raw := f.login(t, user.Username)
	_, err := f.databases.Schemas(f.ctx, user.ID, target.ID)
	must(t, err)
	must(t, f.databases.DisableConnection(f.ctx, f.admin, target.ID))
	metadata, err := f.store.DatabaseMetadata(f.ctx, target.ID)
	must(t, err)
	if metadata.Active {
		t.Fatal("disabled connection remains active in metadata")
	}
	_, err = f.databases.Schemas(f.ctx, user.ID, target.ID)
	wantError(t, err, access.ErrForbidden)
	accessible, err := f.databases.Databases(f.ctx, user.ID)
	must(t, err)
	if len(accessible) != 0 {
		t.Fatal("disabled connection remains accessible")
	}
	_, _, err = f.access.Authenticate(f.ctx, raw)
	wantError(t, err, access.ErrUnauthenticated)
	if !f.tableExists(t, "employees") {
		t.Fatal("disabling metadata modified the managed database")
	}
}
