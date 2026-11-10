package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tubes1_wbd/backend"
	"tubes1_wbd/test/data/setup"
	adminbackend "tubes1_wbd_admin/backend"
	adminaccess "tubes1_wbd_admin/backend/access"
)

const controlGrants = `GRANT USAGE ON SCHEMA public TO {{role}};
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO {{role}};
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO {{role}};`

func localRoleURL(t *testing.T, database, role string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(factory.Cluster.Directory, role+".key"))
	must(t, err)
	u, err := url.Parse(factory.Cluster.DSN(database))
	must(t, err)
	u.User = url.UserPassword(role, strings.TrimSpace(string(raw)))
	return u.String()
}
func provisionAdminRole(t *testing.T, f *fixture) string {
	t.Helper()
	db, err := setup.OpenScoped(f.ctx, factory.Cluster, f.dbs.WebsiteName+"_admin", setup.AdminAuthenticationRole, func(context.Context, *sql.DB) error { return nil }, controlGrants)
	must(t, err)
	db.Close()
	return localRoleURL(t, f.dbs.WebsiteName+"_admin", setup.AdminAuthenticationRole)
}
func adminEnvironment(t *testing.T, dsn string) {
	t.Helper()
	t.Setenv("SCHAKKER_ADMIN_DATABASE_URL", dsn)
	t.Setenv("SCHAKKER_ADMIN_MASTER_KEY", hex.EncodeToString(bytes.Repeat([]byte{21}, 32)))
	t.Setenv("SCHAKKER_INTERNAL_TOKEN", strings.Repeat("private-service-token-", 3))
	t.Setenv("SCHAKKER_ADMIN_ENV", "")
	// These deliberately invalid ordinary settings must never be consulted.
	t.Setenv("SCHAKKER_PLATFORM_DATABASE_URL", "postgres://invalid.invalid/platform")
	t.Setenv("SCHAKKER_MASTER_KEY", "")
	t.Setenv("SCHAKKER_STATE_DIR", filepath.Join(t.TempDir(), "ordinary-state"))
}
func TestAdminStartupUsesOnlyItsProvisionedControlPlaneDatabase(t *testing.T) {
	f := newFixture(t, true)
	adminEnvironment(t, provisionAdminRole(t, f))
	// The application root contains only its own assets, with no sibling src/.
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "frontend", "dist"), 0700))
	must(t, os.WriteFile(filepath.Join(root, "frontend", "dist", "index.html"), []byte(`<div id="root"></div>`), 0600))
	app, err := adminbackend.NewHandler(root)
	must(t, err)
	defer app.(io.Closer).Close()
	response := apiRequest(t, app, "GET", "/session", "", nil)
	if response.Code != 200 {
		t.Fatalf("independent startup: %d %s", response.Code, response.Body.String())
	}
	for _, table := range []string{"users", "access_groups", "user_groups", "managed_databases", "group_database_permissions"} {
		var exists bool
		must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists))
		if table != "users" && table != "managed_databases" && exists {
			t.Fatalf("platform retains authoritative %s", table)
		}
	}
	var sessions int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM sessions`).Scan(&sessions))
	if sessions != 0 {
		t.Fatal("admin startup created ordinary sessions")
	}
}
func TestAdminSessionRotationRevocationAndExpirationUseOnlyAdminDatabase(t *testing.T) {
	f := newFixture(t, true)
	now := time.Now().UTC().Truncate(time.Microsecond)
	config := adminaccess.DefaultConfig(bytes.Repeat([]byte{21}, 32))
	config.Now = func() time.Time { return now }
	service, err := adminaccess.NewService(f.adminStore, config)
	must(t, err)
	first, raw, err := service.Login(f.ctx, "it.admin", password, "")
	must(t, err)
	now = now.Add(16 * time.Minute)
	current, replacement, err := service.Authenticate(f.ctx, raw)
	must(t, err)
	if replacement == "" || !current.AbsoluteExpires.Equal(first.AbsoluteExpires) {
		t.Fatal("admin rotation lost its lifetime")
	}
	now = now.Add(31 * time.Second)
	_, _, err = service.Authenticate(f.ctx, raw)
	wantError(t, err, adminaccess.ErrUnauthenticated)
	_, _, err = service.Authenticate(f.ctx, replacement)
	must(t, err)
	must(t, service.Logout(f.ctx, first.SessionID))
	_, _, err = service.Authenticate(f.ctx, replacement)
	wantError(t, err, adminaccess.ErrUnauthenticated)
	_, raw, err = service.Login(f.ctx, "it.admin", password, "")
	must(t, err)
	now = now.Add(31 * time.Minute)
	_, _, err = service.Authenticate(f.ctx, raw)
	wantError(t, err, adminaccess.ErrUnauthenticated)
	var count int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM sessions`).Scan(&count))
	if count != 0 {
		t.Fatal("admin touched ordinary sessions")
	}
}
func TestServerStartupRejectsEmptySchemasWithoutInitializingData(t *testing.T) {
	f := newFixture(t, false)
	name := f.dbs.WebsiteName + "_empty"
	empty, err := setup.OpenScoped(f.ctx, factory.Cluster, name, setup.AdminAuthenticationRole, func(context.Context, *sql.DB) error { return nil }, `GRANT USAGE ON SCHEMA public TO {{role}};`)
	must(t, err)
	empty.Close()
	t.Cleanup(func() {
		db, err := factory.Cluster.Open(context.Background(), "postgres")
		if err == nil {
			defer db.Close()
			db.ExecContext(context.Background(), `DROP DATABASE `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`)
		}
	})
	dsn := localRoleURL(t, name, setup.AdminAuthenticationRole)
	adminEnvironment(t, dsn)
	root := t.TempDir()
	if _, err := adminbackend.NewHandler(root); err == nil {
		t.Fatal("admin accepted empty schema")
	}
	t.Setenv("SCHAKKER_PLATFORM_DATABASE_URL", dsn)
	t.Setenv("SCHAKKER_MASTER_KEY", hex.EncodeToString(bytes.Repeat([]byte{21}, 32)))
	if _, err := backend.NewHandler(root); err == nil {
		t.Fatal("user accepted empty schema")
	}
	db, err := factory.Cluster.Open(f.ctx, name)
	must(t, err)
	defer db.Close()
	var count int
	must(t, db.QueryRowContext(f.ctx, `SELECT count(*) FROM pg_tables WHERE schemaname='public'`).Scan(&count))
	if count != 0 {
		t.Fatal("server initialized schema")
	}
	entries, err := os.ReadDir(root)
	must(t, err)
	if len(entries) != 0 {
		t.Fatal("server initialized filesystem state")
	}
}
func TestStandaloneAdminBinaryRunsWithoutTheUserModule(t *testing.T) {
	f := newFixture(t, true)
	dsn := provisionAdminRole(t, f)
	root := t.TempDir()
	source, err := filepath.Abs(filepath.Join("..", "..", "..", "src-admin"))
	must(t, err)
	binary := filepath.Join(root, "admin-server")
	build := exec.CommandContext(f.ctx, "go", "build", "-o", binary, ".")
	build.Dir = source
	build.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("standalone build: %v %s", err, out)
	}
	must(t, os.MkdirAll(filepath.Join(root, "frontend", "dist"), 0700))
	must(t, os.WriteFile(filepath.Join(root, "frontend", "dist", "index.html"), []byte(`<div id="root"></div>`), 0600))
	reserve := func() string {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		must(t, err)
		defer listener.Close()
		return listener.Addr().String()
	}
	public, private := reserve(), reserve()
	_, port, _ := net.SplitHostPort(public)
	command := exec.CommandContext(f.ctx, binary)
	command.Dir = root
	command.Env = append(os.Environ(), "SCHAKKER_ADMIN_DATABASE_URL="+dsn, "SCHAKKER_ADMIN_MASTER_KEY="+hex.EncodeToString(bytes.Repeat([]byte{21}, 32)), "SCHAKKER_INTERNAL_TOKEN="+strings.Repeat("private-service-token-", 3), "SCHAKKER_ADMIN_PORT="+port, "SCHAKKER_ADMIN_INTERNAL_ADDR="+private, "SCHAKKER_ADMIN_ENV=", "SCHAKKER_ADMIN_INTERNAL_TLS_CERT=", "SCHAKKER_ADMIN_INTERNAL_TLS_KEY=", "SCHAKKER_PLATFORM_DATABASE_URL=postgres://invalid.invalid/platform", "SCHAKKER_MASTER_KEY=")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	must(t, command.Start())
	defer func() { command.Process.Kill(); command.Wait() }()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + public + "/api/session")
		if err == nil {
			defer response.Body.Close()
			var data struct{ CSRF string }
			must(t, json.NewDecoder(response.Body).Decode(&data))
			if response.StatusCode == 200 && data.CSRF != "" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("standalone binary did not become ready on %s", fmt.Sprint(public))
}
