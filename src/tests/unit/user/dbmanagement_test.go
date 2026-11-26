package unit_test

import (
	"context"
	"testing"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
)

func TestExplorerEnforcesDatabasePermissionsBeforeConnecting(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, service, _ := newServices(t, store)
	calls := []struct {
		name string
		call func() error
	}{
		{"schemas", func() error { _, err := service.Schemas(context.Background(), 7, 3); return err }},
		{"tables", func() error { _, err := service.Tables(context.Background(), 7, 3, "public"); return err }},
		{"table metadata", func() error {
			_, err := service.DescribeTable(context.Background(), 7, 3, "public", "people")
			return err
		}},
		{"table rows", func() error {
			_, err := service.TableData(context.Background(), 7, 3, "public", "people", dbmanagement.PageOptions{})
			return err
		}},
		{"snapshot", func() error { _, err := service.Snapshot(context.Background(), 7, 3); return err }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			stub.append(permissionStep(access.NoAccess))
			wantError(t, tc.call(), access.ErrForbidden)
		})
	}
}
func TestDatabaseConfigurationValidation(t *testing.T) {
	_, store, _ := newSQLStub(t)
	auth := newAccess(t, store, nil)
	for _, change := range []func(*dbmanagement.Config){func(c *dbmanagement.Config) { c.Key = nil }, func(c *dbmanagement.Config) { c.ConnectTimeout = 0 }, func(c *dbmanagement.Config) { c.QueryTimeout = -time.Second }} {
		config := dbmanagement.DefaultConfig(testKey, false)
		change(&config)
		_, err := dbmanagement.NewService(store, auth, config)
		wantError(t, err, access.ErrInvalid)
	}
}
