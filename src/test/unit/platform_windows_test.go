package unit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tubes1_wbd/database/setup"
	fixtures "tubes1_wbd/test/data"
)

func windowsFixtureFile(t *testing.T, directory, name, contents string) {
	t.Helper()
	filename := filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func windowsPostgresTools(t *testing.T) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "Program Files", "PostgreSQL", "bin")
	for _, name := range []string{"postgres.exe", "initdb.exe", "pg_ctl.exe"} {
		windowsFixtureFile(t, directory, name, "")
	}
	t.Setenv("SCHAKKER_POSTGRES_BIN", directory)
}

func TestWindowsPostgreSQLRejectsUnixStateWithoutChangingIt(t *testing.T) {
	windowsPostgresTools(t)
	directory := t.TempDir()
	configuration := `{"Socket":"/tmp/schakker-socket","Port":5432,"TCPRole":""}`
	windowsFixtureFile(t, directory, "local.json", configuration)
	_, err := setup.StartLocalCluster(context.Background(), directory, setup.LocalOptions{})
	if err == nil || !strings.Contains(err.Error(), "separate state directories") {
		t.Fatalf("Unix database state was not rejected: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "local.json"))
	if err != nil || string(data) != configuration {
		t.Fatal("Windows startup modified the Unix configuration")
	}
}

func TestWindowsPostgreSQLDoesNotReplaceMissingExistingCredentials(t *testing.T) {
	windowsPostgresTools(t)
	directory := t.TempDir()
	windowsFixtureFile(t, directory, "local.json", `{"Socket":"127.0.0.1","Port":5432,"TCPRole":""}`)
	windowsFixtureFile(t, directory, filepath.Join("postgres", "PG_VERSION"), "17\n")
	_, err := setup.StartLocalCluster(context.Background(), directory, setup.LocalOptions{})
	if err == nil || !strings.Contains(err.Error(), "restore postgres.key") {
		t.Fatalf("missing existing credentials were not rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "postgres.key")); !os.IsNotExist(err) {
		t.Fatal("Windows startup generated replacement credentials for an existing cluster")
	}
}

func TestWindowsMariaDBDoesNotReplaceMissingExistingAdminCredentials(t *testing.T) {
	tools := filepath.Join(t.TempDir(), "Program Files", "MariaDB", "bin")
	for _, name := range []string{"mariadbd.exe", "mariadb-install-db.exe"} {
		windowsFixtureFile(t, tools, name, "")
	}
	t.Setenv("SCHAKKER_MARIADB_BIN", tools)
	directory := t.TempDir()
	windowsFixtureFile(t, directory, ".schakker-fixtures", "schakker-test-fixtures-v1\n")
	windowsFixtureFile(t, directory, filepath.Join("mariadb", "local.json"), `{"Port":3306}`)
	windowsFixtureFile(t, directory, filepath.Join("mariadb", "managed.key"), strings.Repeat("a", 64))
	if err := os.MkdirAll(filepath.Join(directory, "mariadb", "data", "mysql"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := fixtures.OpenWithOptions(context.Background(), directory, fixtures.Options{MariaDB: true})
	if err == nil || !strings.Contains(err.Error(), "restore mariadb/admin.key") {
		t.Fatalf("missing retained MariaDB admin credentials were not rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "mariadb", "admin.key")); !os.IsNotExist(err) {
		t.Fatal("Windows startup replaced the admin credentials of an existing MariaDB fixture")
	}
}
