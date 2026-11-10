package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"
	fixtures "tubes1_wbd/test/data"
	adminsetup "tubes1_wbd/test/data/adminsetup"
	adminaccess "tubes1_wbd_admin/backend/access"
)

var factory *fixtures.Factory

const password = "Integration-password-123"

type fixture struct {
	ctx        context.Context
	dbs        *fixtures.Databases
	store      *dao.Store
	access     *access.Service
	userAccess *access.Service
	adminAuth  *access.Service
	adminStore *dao.Store
	adminDB    *sql.DB
	databases  *dbmanagement.Service
	migrations *migration.Service
	admin      int64
	editor     int64
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
	auth, err := access.NewService(store, access.DefaultConfig(key))
	if err != nil {
		t.Fatal(err)
	}
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
	must(t, adminsetup.EnsureInitialized(ctx, adminDB))
	_, err = adminDB.ExecContext(ctx, `ALTER SEQUENCE admin_users_id_seq RESTART WITH 1000`)
	must(t, err)
	adminStore, err := dao.NewAdminAuthentication(adminDB)
	must(t, err)
	adminAuth, err := adminaccess.NewService(adminStore, access.DefaultConfig(key))
	must(t, err)
	administration, err := access.NewAdministrationService(store, adminAuth, access.DefaultConfig(key))
	must(t, err)
	management, err := dbmanagement.NewService(store, administration, dbmanagement.DefaultConfig(key, false))
	must(t, err)
	migrations, err := migration.NewService(store, auth, management)
	must(t, err)
	f := &fixture{ctx: ctx, dbs: databases, store: store, access: administration, userAccess: auth, adminAuth: adminAuth, adminStore: adminStore, adminDB: adminDB, databases: management, migrations: migrations}
	if admin {
		must(t, fixtures.BootstrapAdmin(ctx, adminStore, "it.admin", "Integration Admin", password))
		credentials, err := adminStore.Credentials(ctx, "it.admin")
		must(t, err)
		f.admin = credentials.ID
		must(t, administration.CreateUser(ctx, f.admin, "it.editor", "Integration Editor", password, false))
		editor, err := store.Credentials(ctx, "it.editor")
		must(t, err)
		f.editor = editor.ID
	}
	return f
}
func (f *fixture) user(t *testing.T, username string) dao.User {
	t.Helper()
	must(t, f.access.CreateUser(f.ctx, f.admin, username, username, password, false))
	credentials, err := f.store.Credentials(f.ctx, username)
	must(t, err)
	return credentials.User
}
func (f *fixture) group(t *testing.T, name string) dao.Group {
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
	return dao.Group{}
}
func (f *fixture) register(t *testing.T) dao.Database {
	t.Helper()
	saved, err := f.databases.SaveConnection(f.ctx, f.admin, 0, f.dbs.Connection())
	must(t, err)
	return saved
}
func (f *fixture) grant(t *testing.T, user, database int64, permission access.Permission) dao.Group {
	t.Helper()
	group := f.group(t, fmt.Sprintf("IT group %d", user))
	must(t, f.access.SetMembership(f.ctx, f.admin, user, group.ID, true))
	must(t, f.access.SetGrant(f.ctx, f.admin, group.ID, database, permission))
	return group
}
func (f *fixture) login(t *testing.T, username string) (access.Identity, string) {
	t.Helper()
	service := f.userAccess
	if username == "it.admin" {
		service = f.adminAuth
	}
	identity, raw, err := service.Login(f.ctx, username, password, "")
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
	if !errors.Is(err, want) {
		t.Fatalf("error = %v; want %v", err, want)
	}
}
func (f *fixture) tableExists(t *testing.T, name string) bool {
	t.Helper()
	var exists bool
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`, name).Scan(&exists))
	return exists
}
