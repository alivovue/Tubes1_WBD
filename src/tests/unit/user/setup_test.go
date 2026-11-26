package unit_test

import (
	"context"
	"testing"

	"schakker_tests/fixtures/setup"
)

func TestDatabaseSetupRejectsMissingDependencies(t *testing.T) {
	if _, err := setup.OpenConnection(context.Background(), ""); err == nil {
		t.Fatal("missing database configuration accepted")
	}
}
