package unit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtures "tubes1_wbd/test/data"
)

func TestFixtureSetupPreservesAnUnrelatedDirectory(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "existing-data")
	if err := os.WriteFile(filename, []byte("keep this data"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixtures.OpenWithOptions(context.Background(), directory, fixtures.Options{MariaDB: true}); err == nil {
		t.Fatal("fixture setup accepted an unrelated nonempty directory")
	}
	contents, err := os.ReadFile(filename)
	if err != nil || string(contents) != "keep this data" {
		t.Fatal("fixture setup modified unrelated data")
	}
}

func TestMariaDBFixtureReportsMissingToolsWithoutStartingPostgreSQL(t *testing.T) {
	t.Setenv("SCHAKKER_MARIADB_BIN", filepath.Join(t.TempDir(), "missing", "bin"))
	directory := t.TempDir()
	_, err := fixtures.OpenWithOptions(context.Background(), directory, fixtures.Options{MariaDB: true})
	if err == nil || !strings.Contains(err.Error(), "MariaDB server tools") {
		t.Fatalf("missing MariaDB tools did not produce an actionable error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "postgres")); !os.IsNotExist(err) {
		t.Fatal("selecting only MariaDB attempted to create a PostgreSQL fixture")
	}
}
