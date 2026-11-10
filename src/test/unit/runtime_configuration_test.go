package unit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tubes1_wbd/database/platform"
	runtimeSetup "tubes1_wbd/database/setup"
)

func TestRuntimeRequiresAnExternallyConfiguredDatabase(t *testing.T) {
	for _, name := range []string{"SCHAKKER_PLATFORM_DATABASE_URL", "SCHAKKER_DATABASE_URL", "DATABASE_URL"} {
		t.Setenv(name, "")
	}
	directory := t.TempDir()
	if _, err := platform.Open(directory); err == nil {
		t.Fatal("runtime accepted missing database configuration")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("runtime initialized local state without database configuration")
	}
}
func TestRuntimeKeyLoadingNeverInitializesPersistentState(t *testing.T) {
	t.Setenv("SCHAKKER_MASTER_KEY", "")
	directory := filepath.Join(t.TempDir(), "not-created")
	if _, err := platform.MasterKey(directory, false); err == nil {
		t.Fatal("missing key was generated automatically")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("loading a missing key created a state directory")
	}
	t.Setenv("SCHAKKER_MASTER_KEY", strings.Repeat("15", 32))
	key, err := platform.MasterKey(directory, false)
	if err != nil || len(key) != 32 {
		t.Fatalf("configured master key is unavailable: %v", err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("configured key caused a file write")
	}
	t.Setenv("SCHAKKER_MASTER_KEY", "")
	directory = t.TempDir()
	filename := filepath.Join(directory, "master.key")
	original := []byte(strings.Repeat("15", 32) + "\n")
	if err := os.WriteFile(filename, original, 0640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := platform.MasterKey(directory, false); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("reading an existing key changed the file")
	}
}
func TestMissingPlatformSchemaIsRejectedWithoutInitialization(t *testing.T) {
	db, _, _ := newSQLStub(t, scalarStep("to_regclass('public.users')", false))
	if err := runtimeSetup.ValidateSchema(context.Background(), db); err == nil {
		t.Fatal("runtime accepted an unprovisioned schema")
	}
}
