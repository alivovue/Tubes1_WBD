package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"context"
	"database/sql"
	"github.com/jackc/pgx/v5"
	userbackend "tubes1_wbd/backend"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/httpapi"
	"tubes1_wbd/database/dao"
	fixtures "tubes1_wbd/test/data"
	adminsetup "tubes1_wbd/test/data/adminsetup"
	"tubes1_wbd/test/data/setup"
	adminbackend "tubes1_wbd_admin/backend"
)

func localRoleURL(t *testing.T, name, role string) string {
	t.Helper()
	parsed, err := url.Parse(factory.Cluster.DSN(name))
	must(t, err)
	password, err := os.ReadFile(filepath.Join(factory.Cluster.Directory, role+".key"))
	must(t, err)
	parsed.User = url.UserPassword(role, strings.TrimSpace(string(password)))
	return parsed.String()
}
func TestAdminStartupUsesProvisionedAccountsAndRestrictedDatabaseRoles(t *testing.T) {
	f := newFixture(t, false)
	hash, err := access.HashPassword(password)
	must(t, err)
	must(t, f.store.CreateUser(f.ctx, "existing.employee", "Existing Employee", hash, false, nil))
	var adminName string
	must(t, f.adminDB.QueryRowContext(f.ctx, `SELECT current_database()`).Scan(&adminName))
	userDB, err := setup.OpenScoped(f.ctx, factory.Cluster, f.dbs.WebsiteName, setup.UserRuntimeRole, setup.EnsureInitialized, setup.UserRuntimeGrants)
	must(t, err)
	defer userDB.Close()
	administrationDB, err := setup.OpenScoped(f.ctx, factory.Cluster, f.dbs.WebsiteName, setup.AdministrationRole, setup.EnsureInitialized, setup.AdministrationGrants)
	must(t, err)
	defer administrationDB.Close()
	authenticationDB, err := setup.OpenScoped(f.ctx, factory.Cluster, adminName, setup.AdminAuthenticationRole, adminsetup.EnsureInitialized, adminsetup.RuntimeGrants)
	must(t, err)
	defer authenticationDB.Close()
	must(t, setup.ValidateRuntimeRole(f.ctx, userDB))
	must(t, setup.ValidateRuntimeRole(f.ctx, administrationDB))
	must(t, setup.ValidateRuntimeRole(f.ctx, authenticationDB))
	if _, err := userDB.ExecContext(f.ctx, `UPDATE users SET display_name='Forbidden'`); err == nil {
		t.Fatal("ordinary runtime role can administer accounts")
	}
	if _, err := administrationDB.ExecContext(f.ctx, `SELECT sql_text FROM migration_drafts`); err == nil {
		t.Fatal("administration role can read migration SQL")
	}
	if _, err := administrationDB.ExecContext(f.ctx, `SELECT password_hash FROM users`); err == nil {
		t.Fatal("administration role can read ordinary password hashes")
	}
	wrong := localRoleURL(t, adminName, setup.UserRuntimeRole)
	if db, err := setup.Open(f.ctx, wrong); err == nil {
		db.Close()
		t.Fatal("ordinary server role can connect to admin database")
	}
	wrong = localRoleURL(t, f.dbs.WebsiteName, setup.AdminAuthenticationRole)
	if db, err := setup.Open(f.ctx, wrong); err == nil {
		db.Close()
		t.Fatal("admin authentication role can connect to platform database")
	}
	t.Setenv("SCHAKKER_PLATFORM_DATABASE_URL", localRoleURL(t, f.dbs.WebsiteName, setup.AdministrationRole))
	t.Setenv("SCHAKKER_ADMIN_DATABASE_URL", localRoleURL(t, adminName, setup.AdminAuthenticationRole))
	stateDirectory := filepath.Join(t.TempDir(), "uncreated-state")
	t.Setenv("SCHAKKER_STATE_DIR", stateDirectory)
	t.Setenv("SCHAKKER_MASTER_KEY", strings.Repeat("15", 32))
	t.Setenv("SCHAKKER_ENV", "")
	t.Setenv("SCHAKKER_USE_MOCK_WEBSITE", "no")
	t.Setenv("SCHAKKER_ADMIN_USERNAME", "bootstrap.admin")
	t.Setenv("SCHAKKER_ADMIN_NAME", "Bootstrap Administrator")
	t.Setenv("SCHAKKER_ADMIN_PASSWORD", password)
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "src-admin"))
	must(t, err)
	emptyHandler, err := adminbackend.NewHandler(root)
	must(t, err)
	var adminCount int
	must(t, f.adminDB.QueryRowContext(f.ctx, `SELECT count(*) FROM admin_users`).Scan(&adminCount))
	if adminCount != 0 {
		t.Fatal("startup bootstrapped an administrator from environment variables")
	}
	if _, err := os.Stat(stateDirectory); !os.IsNotExist(err) {
		t.Fatal("startup initialized local state or a key file")
	}
	must(t, emptyHandler.(io.Closer).Close())
	must(t, fixtures.BootstrapAdmin(f.ctx, f.adminStore, "existing.admin", "Existing Administrator", password))
	handler, err := adminbackend.NewHandler(root)
	must(t, err)
	defer handler.(io.Closer).Close()
	credentials, err := f.adminStore.Credentials(f.ctx, "existing.admin")
	must(t, err)
	if !credentials.IsAdmin {
		t.Fatal("provisioned administrator is unavailable")
	}
	if _, err := f.store.Credentials(f.ctx, "existing.admin"); err == nil {
		t.Fatal("startup copied admin credentials into platform users")
	}
	challenge := apiRequest(t, handler, http.MethodGet, "/session", "", nil)
	var data struct{ CSRF string }
	must(t, json.Unmarshal(challenge.Body.Bytes(), &data))
	body, _ := json.Marshal(map[string]string{"username": "existing.admin", "password": password})
	request := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", data.CSRF)
	for _, cookie := range challenge.Result().Cookies() {
		if cookie.Name == httpapi.AdminLoginCookieName {
			request.AddCookie(cookie)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("admin login against restricted database failed: %d %s", response.Code, response.Body.String())
	}
	var adminToken string
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == httpapi.AdminCookieName {
			adminToken = cookie.Value
		}
	}
	if adminToken == "" {
		t.Fatal("admin login did not issue cookie")
	}
	page := apiRequest(t, handler, http.MethodGet, "/admin/users", adminToken, nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "existing.employee") {
		t.Fatalf("admin cannot manage platform accounts: %d %s", page.Code, page.Body.String())
	}
	session := apiRequest(t, handler, http.MethodGet, "/session", adminToken, nil, httpapi.AdminCookieName)
	csrf := decodeAPI[struct{ CSRF string }](t, session).CSRF
	ordinary, err := f.store.Credentials(f.ctx, "existing.employee")
	must(t, err)
	change := apiRequest(t, handler, http.MethodPost, "/admin/users", adminToken, url.Values{"csrf": {csrf}, "action": {"update_user"}, "id": {fmt.Sprint(ordinary.ID)}, "display": {"Updated Employee"}, "active": {"on"}})
	if change.Code != 200 {
		t.Fatalf("restricted admin account update failed: %d %s", change.Code, change.Body.String())
	}
	created := apiRequest(t, handler, http.MethodPost, "/admin/administrators", adminToken, url.Values{"csrf": {csrf}, "action": {"create_admin"}, "username": {"second.admin"}, "display": {"Second Administrator"}, "password": {password}})
	if created.Code != 200 {
		t.Fatalf("separate admin account creation failed: %d %s", created.Code, created.Body.String())
	}
	input := f.dbs.Connection()
	connection := apiRequest(t, handler, http.MethodPost, "/admin/connections", adminToken, url.Values{"csrf": {csrf}, "action": {"save_connection"}, "name": {input.Name}, "dbms": {input.DBMS}, "host": {input.Host}, "port": {fmt.Sprint(input.Port)}, "database_name": {input.DatabaseName}, "username": {input.Username}, "password": {input.Password}, "sslmode": {input.SSLMode}, "active": {"on"}})
	if connection.Code != 200 {
		t.Fatalf("restricted configuration registration failed: %d %s", connection.Code, connection.Body.String())
	}
	connections, err := f.store.Databases(f.ctx)
	must(t, err)
	target := connections[0]
	group := apiRequest(t, handler, http.MethodPost, "/admin/groups", adminToken, url.Values{"csrf": {csrf}, "action": {"save_group"}, "name": {"Runtime group"}})
	if group.Code != 200 {
		t.Fatalf("restricted group creation failed: %d", group.Code)
	}
	groups, err := f.store.Groups(f.ctx)
	must(t, err)
	var groupID int64
	for _, entry := range groups {
		if entry.Name == "Runtime group" {
			groupID = entry.ID
		}
	}
	membership := apiRequest(t, handler, http.MethodPost, "/admin/groups", adminToken, url.Values{"csrf": {csrf}, "action": {"membership"}, "id": {fmt.Sprint(groupID)}, "user": {fmt.Sprint(ordinary.ID)}, "member": {"yes"}})
	if membership.Code != 200 {
		t.Fatalf("restricted membership update failed: %d %s", membership.Code, membership.Body.String())
	}
	userStore, err := dao.New(userDB)
	must(t, err)
	userAccess, err := access.NewService(userStore, access.DefaultConfig(bytes.Repeat([]byte{21}, 32)))
	must(t, err)
	for _, permission := range []access.Permission{access.ViewEdit, access.NoAccess} {
		grant := apiRequest(t, handler, http.MethodPost, "/admin/groups", adminToken, url.Values{"csrf": {csrf}, "action": {"set_grant"}, "id": {fmt.Sprint(groupID)}, "db": {fmt.Sprint(target.ID)}, "permission": {string(permission)}})
		if grant.Code != 200 {
			t.Fatalf("restricted grant update failed: %d %s", grant.Code, grant.Body.String())
		}
		effective, err := userAccess.EffectivePermission(f.ctx, ordinary.ID, target.ID)
		must(t, err)
		if effective != permission {
			t.Fatal("user role did not immediately see committed admin grant")
		}
	}

	var count int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM sessions`).Scan(&count))
	if count != 0 {
		t.Fatal("admin session leaked into platform database")
	}
}
func TestAdminSessionRotationGraceExpiryAndRevocationUseOnlyAdminDatabase(t *testing.T) {
	f := newFixture(t, true)
	now := time.Now().UTC().Truncate(time.Microsecond)
	config := access.DefaultConfig(bytes.Repeat([]byte{21}, 32))
	config.Now = func() time.Time { return now }
	service, err := access.NewService(f.adminStore, config)
	must(t, err)
	identity, old, err := service.Login(f.ctx, "it.admin", password, "")
	must(t, err)
	now = now.Add(16 * time.Minute)
	current, replacement, err := service.Authenticate(f.ctx, old)
	must(t, err)
	if replacement == "" || replacement == old || current.SessionID != identity.SessionID || !current.AbsoluteExpires.Equal(identity.AbsoluteExpires) {
		t.Fatal("admin periodic rotation changed session lifetime")
	}
	_, same, err := service.Authenticate(f.ctx, old)
	must(t, err)
	if same != replacement {
		t.Fatal("admin grace request did not recover existing replacement")
	}
	now = now.Add(config.GracePeriod)
	_, _, err = service.Authenticate(f.ctx, old)
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = service.Authenticate(f.ctx, replacement)
	must(t, err)
	must(t, service.Logout(f.ctx, identity.SessionID))
	_, _, err = service.Authenticate(f.ctx, replacement)
	wantError(t, err, access.ErrUnauthenticated)
	_, idleToken, err := service.Login(f.ctx, "it.admin", password, "")
	must(t, err)
	now = now.Add(config.IdleTimeout)
	_, _, err = service.Authenticate(f.ctx, idleToken)
	wantError(t, err, access.ErrUnauthenticated)
	var count int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM sessions`).Scan(&count))
	if count != 0 {
		t.Fatal("admin rotation touched ordinary sessions")
	}
}

func TestServerStartupRejectsEmptySchemasWithoutInitializingData(t *testing.T) {
	f := newFixture(t, false)
	name := f.dbs.WebsiteName + "_empty"
	_, err := factory.Cluster.EnsureDatabase(f.ctx, name, "", "")
	must(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		db, err := factory.Cluster.Open(ctx, "postgres")
		if err != nil {
			t.Error(err)
			return
		}
		defer db.Close()
		_, err = db.ExecContext(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		if err != nil {
			t.Error(err)
		}
	})
	noInitialization := func(context.Context, *sql.DB) error { return nil }
	for _, role := range []string{setup.UserRuntimeRole, setup.AdminAuthenticationRole} {
		db, err := setup.OpenScoped(f.ctx, factory.Cluster, name, role, noInitialization, "")
		must(t, err)
		must(t, db.Close())
	}
	platformDB, err := setup.OpenScoped(f.ctx, factory.Cluster, f.dbs.WebsiteName, setup.AdministrationRole, setup.EnsureInitialized, setup.AdministrationGrants)
	must(t, err)
	defer platformDB.Close()
	state := filepath.Join(t.TempDir(), "untouched-state")
	t.Setenv("SCHAKKER_STATE_DIR", state)
	t.Setenv("SCHAKKER_MASTER_KEY", strings.Repeat("15", 32))
	t.Setenv("SCHAKKER_ENV", "")
	t.Setenv("SCHAKKER_PLATFORM_DATABASE_URL", localRoleURL(t, name, setup.UserRuntimeRole))
	userRoot, err := filepath.Abs(filepath.Join("..", ".."))
	must(t, err)
	if _, err := userbackend.NewHandler(userRoot); err == nil {
		t.Fatal("user startup accepted an empty platform schema")
	}
	t.Setenv("SCHAKKER_PLATFORM_DATABASE_URL", localRoleURL(t, f.dbs.WebsiteName, setup.AdministrationRole))
	t.Setenv("SCHAKKER_ADMIN_DATABASE_URL", localRoleURL(t, name, setup.AdminAuthenticationRole))
	adminRoot, err := filepath.Abs(filepath.Join("..", "..", "..", "src-admin"))
	must(t, err)
	if _, err := adminbackend.NewHandler(adminRoot); err == nil {
		t.Fatal("admin startup accepted an empty authentication schema")
	}
	db, err := factory.Cluster.Open(f.ctx, name)
	must(t, err)
	defer db.Close()
	var count int
	must(t, db.QueryRowContext(f.ctx, `SELECT count(*) FROM pg_tables WHERE schemaname='public'`).Scan(&count))
	if count != 0 {
		t.Fatal("application startup initialized database tables")
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("application startup wrote local state")
	}
}
