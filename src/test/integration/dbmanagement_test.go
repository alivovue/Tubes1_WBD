package integration_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
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

func TestDuplicateDatabaseRegistrationPreservesExistingHistory(t *testing.T) {
	f := newFixture(t, true)
	input := f.dbs.Connection()
	target := f.register(t)
	f.grant(t, f.admin, target.ID, access.ViewEdit)
	review, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "SELECT 1")
	must(t, err)
	_, err = f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: review})
	must(t, err)
	duplicate := input
	duplicate.Name = "Duplicate database under another name"
	_, err = f.databases.SaveConnection(f.ctx, f.admin, 0, duplicate)
	wantError(t, err, dbmanagement.ErrDuplicateConnection)
	duplicate.Host = "localhost"
	_, err = f.databases.SaveConnection(f.ctx, f.admin, 0, duplicate)
	wantError(t, err, dbmanagement.ErrDuplicateConnection)
	must(t, f.databases.DisableConnection(f.ctx, f.admin, target.ID))
	_, err = f.databases.SaveConnection(f.ctx, f.admin, 0, duplicate)
	wantError(t, err, dbmanagement.ErrDuplicateConnection)
	input.Name = "Renamed and reactivated database"
	saved, err := f.databases.SaveConnection(f.ctx, f.admin, target.ID, input)
	must(t, err)
	if saved.ID != target.ID || !saved.Active || saved.Name != input.Name {
		t.Fatal("editing or reactivating the original connection did not preserve its identity")
	}
	history, err := f.migrations.History(f.ctx, f.admin, target.ID, 50, 0)
	must(t, err)
	if len(history) != 1 || history[0].AppliedVersion == nil || *history[0].AppliedVersion != 1 {
		t.Fatal("rejecting duplicates or reactivating the connection changed migration history")
	}
	connections, err := f.databases.Connections(f.ctx, f.admin)
	must(t, err)
	if len(connections) != 1 {
		t.Fatal("duplicate database registrations were persisted")
	}
}

func TestEditingConnectionCannotDuplicateAnotherDatabase(t *testing.T) {
	f := newFixture(t, true)
	other := newFixture(t, false)
	first := f.register(t)
	input := other.dbs.Connection()
	input.Name = "Different database on the same server"
	second, err := f.databases.SaveConnection(f.ctx, f.admin, 0, input)
	must(t, err)
	input.DatabaseName = first.DatabaseName
	_, err = f.databases.SaveConnection(f.ctx, f.admin, second.ID, input)
	wantError(t, err, dbmanagement.ErrDuplicateConnection)
	metadata, err := f.store.DatabaseMetadata(f.ctx, second.ID)
	must(t, err)
	if metadata.DatabaseName != other.dbs.ManagedName {
		t.Fatal("rejected edit changed the saved target database")
	}
}

func TestConcurrentDuplicateDatabaseRegistrationsSaveOnlyOne(t *testing.T) {
	f := newFixture(t, true)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := range 2 {
		input := f.dbs.Connection()
		input.Name = fmt.Sprintf("Concurrent connection %d", i)
		go func(input dbmanagement.ConnectionInput) {
			<-start
			_, err := f.databases.SaveConnection(f.ctx, f.admin, 0, input)
			results <- err
		}(input)
	}
	close(start)
	saved, rejected := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			saved++
		case errors.Is(err, dbmanagement.ErrDuplicateConnection):
			rejected++
		default:
			t.Fatalf("unexpected registration error: %v", err)
		}
	}
	connections, err := f.databases.Connections(f.ctx, f.admin)
	must(t, err)
	if saved != 1 || rejected != 1 || len(connections) != 1 {
		t.Fatalf("concurrent registrations saved %d, rejected %d, persisted %d", saved, rejected, len(connections))
	}
}

func TestDuplicateDatabaseRegistrationShowsActionableConflict(t *testing.T) {
	f := newFixture(t, true)
	f.register(t)
	identity, raw := f.login(t, "it.admin")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	input := f.dbs.Connection()
	form := url.Values{"csrf": {csrf}, "action": {"save_connection"}, "dbms": {input.DBMS}, "name": {"Duplicate HTTP target"}, "host": {input.Host}, "port": {fmt.Sprint(input.Port)}, "database_name": {input.DatabaseName}, "username": {input.Username}, "password": {input.Password}, "sslmode": {input.SSLMode}, "active": {"on"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/connections", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: access.CookieName, Value: raw})
	response := httptest.NewRecorder()
	promotionHandler(t, f).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "This database is already registered. Edit or reactivate its existing connection.") {
		t.Fatalf("duplicate registration did not show the expected conflict: %d", response.Code)
	}
}

func TestConnectionEditsAndDisableRevokeAdminAndReaderSessions(t *testing.T) {
	for _, action := range []string{"save_connection", "disable_connection"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t, true)
			reader := f.user(t, "connection.reader")
			unrelated := f.user(t, "unrelated.reader")
			target := f.register(t)
			f.grant(t, f.admin, target.ID, access.ViewEdit)
			f.grant(t, reader.ID, target.ID, access.ViewOnly)
			identity, adminToken := f.login(t, "it.admin")
			_, readerToken := f.login(t, reader.Username)
			_, unrelatedToken := f.login(t, unrelated.Username)
			csrf, err := f.access.CSRFToken(identity)
			must(t, err)
			input := f.dbs.Connection()
			form := url.Values{"csrf": {csrf}, "id": {fmt.Sprint(target.ID)}, "action": {action}, "dbms": {input.DBMS}, "name": {"Renamed connection"}, "host": {input.Host}, "port": {fmt.Sprint(input.Port)}, "database_name": {input.DatabaseName}, "username": {input.Username}, "password": {input.Password}, "sslmode": {input.SSLMode}, "active": {"on"}}
			request := httptest.NewRequest(http.MethodPost, "/admin/connections", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.AddCookie(&http.Cookie{Name: access.CookieName, Value: adminToken})
			response := httptest.NewRecorder()
			handler := promotionHandler(t, f)
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusSeeOther || !strings.HasPrefix(response.Header().Get("Location"), "/admin/connections") {
				t.Fatalf("connection change returned %d", response.Code)
			}
			for _, check := range []struct{ token, path string }{{adminToken, "/admin/connections"}, {readerToken, "/dashboard"}} {
				_, _, err := f.access.Authenticate(f.ctx, check.token)
				wantError(t, err, access.ErrUnauthenticated)
				request := httptest.NewRequest(http.MethodGet, check.path, nil)
				request.AddCookie(&http.Cookie{Name: access.CookieName, Value: check.token})
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
					t.Fatalf("revoked session was not redirected to login: %s returned %d", check.path, response.Code)
				}
			}
			_, _, err = f.access.Authenticate(f.ctx, unrelatedToken)
			must(t, err)
		})
	}
}

func TestExplorerReadsCommittedManualDataAndSchemaChanges(t *testing.T) {
	f := newFixture(t, true)
	reader := f.user(t, "manual.reader")
	target := f.register(t)
	f.grant(t, reader.ID, target.ID, access.ViewOnly)
	_, token := f.login(t, reader.Username)
	handler := promotionHandler(t, f)
	path := fmt.Sprintf("/explorer?db=%d&schema=public&table=employees&tab=rows-panel", target.ID)
	get := func() *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(&http.Cookie{Name: access.CookieName, Value: token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("explorer returned %d", response.Code)
		}
		return response
	}
	if !strings.Contains(get().Body.String(), ">5000<") {
		t.Fatal("initial employee data was not displayed")
	}
	tx, err := f.dbs.Managed.BeginTx(f.ctx, nil)
	must(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(f.ctx, `UPDATE public.employees SET salary=901234 WHERE name='Alice'`)
	must(t, err)
	if strings.Contains(get().Body.String(), ">901234<") {
		t.Fatal("explorer exposed an uncommitted external update")
	}
	must(t, tx.Commit())
	if !strings.Contains(get().Body.String(), ">901234<") {
		t.Fatal("explorer did not load the committed external update")
	}
	_, err = f.dbs.Managed.ExecContext(f.ctx, `ALTER TABLE public.employees ADD COLUMN manual_department TEXT`)
	must(t, err)
	if !strings.Contains(get().Body.String(), "manual_department") {
		t.Fatal("explorer did not load the external schema change")
	}
	history, err := f.migrations.History(f.ctx, reader.ID, target.ID, 50, 0)
	must(t, err)
	if len(history) != 0 {
		t.Fatal("external changes were incorrectly recorded as platform migrations")
	}
}
