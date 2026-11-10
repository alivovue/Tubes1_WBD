package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	adminfixtures "schakker_tests/fixtures/admin"
	fixtures "schakker_tests/fixtures/setup"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/controlplane"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"
	adminaccess "tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/connectionconfig"
	"tubes1_wbd_admin/backend/internalapi"
	admindao "tubes1_wbd_admin/database/dao"
)

var factory *fixtures.Factory

const password = "Integration-password-123"

type fixture struct {
	ctx          context.Context
	dbs          *fixtures.Databases
	store        *admindao.Store
	runtimeStore *dao.Store
	access       *testAccess
	userAccess   *access.Service
	adminAuth    *adminaccess.Service
	adminStore   *admindao.Store
	adminDB      *sql.DB
	databases    *dbmanagement.Service
	connections  *connectionconfig.Service
	authority    *controlplane.Client
	private      *httptest.Server
	migrations   *migration.Service
	admin        int64
	editor       int64
}

func TestMain(m *testing.M) { os.Exit(runIntegration(m)) }
func runIntegration(m *testing.M) int {
	directory := os.Getenv("SCHAKKER_TEST_STATE")
	if directory == "" {
		var err error
		directory, err = os.MkdirTemp("", "schakker-integration-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Cannot create integration database directory:", err)
			return 1
		}
		defer os.RemoveAll(directory)
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	factory, err = fixtures.Open(ctx, absolute)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot prepare isolated integration databases:", err)
		return 1
	}
	defer factory.Close()
	return m.Run()
}
func newFixture(t *testing.T, admin bool) *fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	databases, err := factory.Clone(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if err := databases.Close(cleanup); err != nil {
			t.Errorf("remove isolated test databases: %v", err)
		}
	})
	store, err := dao.New(databases.Website)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{21}, 32)
	adminName := databases.WebsiteName + "_admin"
	_, err = factory.Cluster.EnsureDatabase(ctx, adminName, "", "")
	must(t, err)
	adminDB, err := factory.Cluster.Open(ctx, adminName)
	must(t, err)
	t.Cleanup(func() {
		adminDB.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		db, err := factory.Cluster.Open(cleanup, "postgres")
		if err != nil {
			t.Error(err)
			return
		}
		defer db.Close()
		if _, err := db.ExecContext(cleanup, "DROP DATABASE "+pgx.Identifier{adminName}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	must(t, adminfixtures.Initialize(ctx, adminDB))
	_, err = adminDB.ExecContext(ctx, `ALTER SEQUENCE admin_users_id_seq RESTART WITH 1000`)
	must(t, err)
	adminStore, err := admindao.New(adminDB)
	must(t, err)
	adminAuth, err := adminaccess.NewService(adminStore, adminaccess.DefaultConfig(key))
	must(t, err)
	privateHandler, err := internalapi.NewHandler(adminStore, key, strings.Repeat("private-service-token-", 3))
	must(t, err)
	private := httptest.NewServer(privateHandler)
	t.Cleanup(private.Close)
	authority, err := controlplane.New(private.URL, strings.Repeat("private-service-token-", 3), false)
	must(t, err)
	config := access.DefaultConfig(key)
	config.Authority = authority
	auth, err := access.NewService(store, config)
	must(t, err)
	management, err := dbmanagement.NewService(store, auth, dbmanagement.DefaultConfig(key, false))
	must(t, err)
	connections, err := connectionconfig.NewService(adminStore, adminAuth, connectionconfig.DefaultConfig(key, false))
	must(t, err)
	migrations, err := migration.NewService(store, auth, management)
	must(t, err)
	f := &fixture{ctx: ctx, dbs: databases, store: adminStore, runtimeStore: store, userAccess: auth, adminAuth: adminAuth, adminStore: adminStore, adminDB: adminDB, databases: management, migrations: migrations, connections: connections, authority: authority, private: private}
	f.access = &testAccess{Service: auth, admin: adminAuth}

	if admin {
		must(t, bootstrapAdmin(ctx, adminStore, "it.admin", "Integration Admin", password))
		credentials, err := adminStore.AdminCredentials(ctx, "it.admin")
		must(t, err)
		f.admin = credentials.ID
		must(t, adminAuth.CreateUser(ctx, f.admin, "it.editor", "Integration Editor", password))
		editor, err := adminStore.Credentials(ctx, "it.editor")
		must(t, err)
		f.editor = editor.ID
		must(t, store.RememberUser(ctx, toUser(editor.User)))
	}
	return f
}
func (f *fixture) user(t *testing.T, username string) dao.User {
	t.Helper()
	must(t, f.access.CreateUser(f.ctx, f.admin, username, username, password, false))
	credentials, err := f.store.Credentials(f.ctx, username)
	must(t, err)
	must(t, f.runtimeStore.RememberUser(f.ctx, toUser(credentials.User)))
	return toUser(credentials.User)
}
func (f *fixture) group(t *testing.T, name string) admindao.Group {
	t.Helper()
	must(t, f.access.SaveGroup(f.ctx, f.admin, 0, name, "Integration permission group"))
	groups, err := f.store.Groups(f.ctx)
	must(t, err)
	for _, group := range groups {
		if group.Name == name {
			return group
		}
	}
	t.Fatal("group was not persisted")
	return admindao.Group{}
}
func (f *fixture) register(t *testing.T) dao.Database {
	t.Helper()
	saved, err := f.saveConnection(f.ctx, f.admin, 0, f.dbs.Connection())
	must(t, err)
	return saved
}
func (f *fixture) grant(t *testing.T, user, database int64, permission access.Permission) admindao.Group {
	t.Helper()
	group := f.group(t, fmt.Sprintf("IT group %d", user))
	must(t, f.access.SetMembership(f.ctx, f.admin, user, group.ID, true))
	must(t, f.access.SetGrant(f.ctx, f.admin, group.ID, database, permission))
	return group
}
func (f *fixture) login(t *testing.T, username string) (access.Identity, string) {
	t.Helper()
	if username == "it.admin" {
		identity, raw, err := f.adminAuth.Login(f.ctx, username, password, "")
		must(t, err)
		return access.Identity{User: toUser(identity.User), SessionID: identity.SessionID, AbsoluteExpires: identity.AbsoluteExpires}, raw
	}
	identity, raw, err := f.userAccess.Login(f.ctx, username, password, "")
	must(t, err)
	return identity, raw
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func wantError(t *testing.T, err, want error) {
	t.Helper()
	if errors.Is(err, want) {
		return
	}
	if want != nil {
		for current := err; current != nil; current = errors.Unwrap(current) {
			if current.Error() == want.Error() {
				return
			}
		}
	}
	t.Fatalf("error = %v; want %v", err, want)
}
func (f *fixture) tableExists(t *testing.T, name string) bool {
	t.Helper()
	var exists bool
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`, name).Scan(&exists))
	return exists
}

// Test-only adapters keep administrative setup distinct from runtime services.
type testAccess struct {
	*access.Service
	admin *adminaccess.Service
}

func (s *testAccess) CreateUser(ctx context.Context, actor int64, name, display, password string, admin bool) error {
	if admin {
		return access.ErrInvalid
	}
	return s.admin.CreateUser(ctx, actor, name, display, password)
}
func (s *testAccess) UpdateUser(ctx context.Context, actor, id int64, display string, admin, active bool, password string) error {
	if admin {
		return access.ErrInvalid
	}
	return s.admin.UpdateUser(ctx, actor, id, display, active, password)
}
func (s *testAccess) SaveGroup(ctx context.Context, actor, id int64, name, description string) error {
	return s.admin.SaveGroup(ctx, actor, id, name, description)
}
func (s *testAccess) DeleteGroup(ctx context.Context, actor, id int64) error {
	return s.admin.DeleteGroup(ctx, actor, id)
}
func (s *testAccess) SetMembership(ctx context.Context, actor, user, group int64, member bool) error {
	return s.admin.SetMembership(ctx, actor, user, group, member)
}
func (s *testAccess) SetGrant(ctx context.Context, actor, group, database int64, p access.Permission) error {
	return s.admin.SetGrant(ctx, actor, group, database, admindao.Permission(p))
}
func toUser(u admindao.User) dao.User {
	return dao.User{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Active: u.Active, Version: u.Version, Rotation: u.Rotation}
}
func toDatabase(d admindao.Database) dao.Database {
	return dao.Database{ID: d.ID, DBMS: d.DBMS, Name: d.Name, Host: d.Host, Port: d.Port, DatabaseName: d.DatabaseName, SSLMode: d.SSLMode, Active: d.Active, Permission: dao.Permission(d.Permission)}
}
func (f *fixture) saveConnection(ctx context.Context, actor, id int64, in dbmanagement.ConnectionInput) (dao.Database, error) {
	result, err := f.connections.SaveConnection(ctx, actor, id, connectionconfig.ConnectionInput{DBMS: in.DBMS, Name: in.Name, Host: in.Host, Port: in.Port, DatabaseName: in.DatabaseName, Username: in.Username, Password: in.Password, SSLMode: in.SSLMode, Active: in.Active})
	if err == nil {
		err = f.runtimeStore.RememberDatabase(ctx, result.ID)
	}
	return toDatabase(result), err
}

func toAdminIdentity(i access.Identity) adminaccess.Identity {
	return adminaccess.Identity{User: admindao.User{ID: i.User.ID, Username: i.User.Username, DisplayName: i.User.DisplayName, Active: i.User.Active}, SessionID: i.SessionID, AbsoluteExpires: i.AbsoluteExpires}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source root unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
