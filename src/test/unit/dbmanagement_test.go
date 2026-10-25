package unit_test

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/base64"
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
		{"fingerprint", func() error { _, err := service.Fingerprint(context.Background(), 7, 3); return err }},
		{"plans", func() error { _, err := service.QueryPlans(context.Background(), 7, 3, "SELECT 1"); return err }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			stub.append(permissionStep(access.NoAccess))
			wantError(t, tc.call(), access.ErrForbidden)
		})
	}
	stub.append(permissionStep(access.ViewOnly))
	_, err := service.QueryPlans(context.Background(), 7, 3, "SELECT 1")
	wantError(t, err, access.ErrForbidden)
}
func TestConnectionManagementRequiresAdmin(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, service, _ := newServices(t, store)
	stub.append(scalarStep("is_platform_admin", false))
	_, err := service.Connections(context.Background(), 7)
	wantError(t, err, access.ErrForbidden)
	stub.append(scalarStep("is_platform_admin", false))
	wantError(t, service.TestConnection(context.Background(), 7, 0, dbmanagement.ConnectionInput{}), access.ErrForbidden)
	stub.append(scalarStep("is_platform_admin", false))
	_, err = service.SaveConnection(context.Background(), 7, 0, dbmanagement.ConnectionInput{})
	wantError(t, err, access.ErrForbidden)
	stub.append(append(transactionSteps(false), sqlStep{kind: "rollback"})...)
	wantError(t, service.DisableConnection(context.Background(), 7, 3), access.ErrForbidden)
}
func TestConnectionInputRejectedBeforeNetworkAccess(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, service, _ := newServices(t, store)
	base := dbmanagement.ConnectionInput{DBMS: "postgresql", Name: "Target", Host: "db.internal", Port: 5432, DatabaseName: "target", Username: "dbuser", SSLMode: "verify-full", Active: true}
	for _, tc := range []struct {
		name   string
		change func(*dbmanagement.ConnectionInput)
		want   error
	}{
		{"missing host", func(in *dbmanagement.ConnectionInput) { in.Host = "" }, access.ErrInvalid},
		{"host containing DSN", func(in *dbmanagement.ConnectionInput) { in.Host = "db.internal password=secret" }, access.ErrInvalid},
		{"invalid port", func(in *dbmanagement.ConnectionInput) { in.Port = 65536 }, access.ErrInvalid},
		{"missing database", func(in *dbmanagement.ConnectionInput) { in.DatabaseName = "" }, access.ErrInvalid},
		{"missing username", func(in *dbmanagement.ConnectionInput) { in.Username = "" }, access.ErrInvalid},
		{"invalid TLS mode", func(in *dbmanagement.ConnectionInput) { in.SSLMode = "unknown" }, access.ErrInvalid},
		{"unsupported DBMS", func(in *dbmanagement.ConnectionInput) { in.DBMS = "sqlite" }, dbmanagement.ErrUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.change(&in)
			stub.append(scalarStep("is_platform_admin", true))
			wantError(t, service.TestConnection(context.Background(), 7, 0, in), tc.want)
		})
	}
	config := dbmanagement.DefaultConfig(testKey, true)
	auth := newAccess(t, store, nil)
	production, err := dbmanagement.NewService(store, auth, config)
	if err != nil {
		t.Fatal(err)
	}
	base.SSLMode = "disable"
	stub.append(scalarStep("is_platform_admin", true))
	wantError(t, production.TestConnection(context.Background(), 7, 0, base), access.ErrInvalid)
}

func TestDuplicateConnectionsAreRejectedBeforeConnecting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		host   string
		active bool
		change func(*dbmanagement.ConnectionInput)
	}{
		{"different display name", "127.0.0.1", true, nil},
		{"different username", "127.0.0.1", true, func(in *dbmanagement.ConnectionInput) { in.Username = "another-user" }},
		{"different TLS mode", "127.0.0.1", true, func(in *dbmanagement.ConnectionInput) { in.SSLMode = "require" }},
		{"default port and DBMS alias", "127.0.0.1", true, func(in *dbmanagement.ConnectionInput) { in.Port = 0; in.DBMS = "postgres" }},
		{"host case and trailing dot", "LOCALHOST.", true, func(in *dbmanagement.ConnectionInput) { in.Host = "localhost" }},
		{"hostname alias", "localhost", true, nil},
		{"IPv4 mapped address", "::ffff:127.0.0.1", true, nil},
		{"disabled existing connection", "127.0.0.1", false, nil},
		{"disabled duplicate", "127.0.0.1", true, func(in *dbmanagement.ConnectionInput) { in.Active = false }},
		{"MariaDB", "127.0.0.1", true, func(in *dbmanagement.ConnectionInput) { in.DBMS = "mariadb"; in.Port = 3306 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := dbmanagement.ConnectionInput{DBMS: "postgresql", Name: "Duplicate target", Host: "127.0.0.1", Port: 5432, DatabaseName: "target", Username: "dbuser", SSLMode: "disable", Active: true}
			if tc.change != nil {
				tc.change(&input)
			}
			dbms, port := "postgresql", 5432
			if input.DBMS == "mariadb" {
				dbms, port = "mariadb", 3306
			}
			columns := []string{"id", "display_name", "dbms_type", "host", "port", "database_name", "is_active", "sslmode"}
			existing := queryStep("FROM managed_databases ORDER BY id", columns, []driver.Value{int64(3), "Original target", dbms, tc.host, int64(port), "target", tc.active, "disable"})
			_, store, stub := newSQLStub(t, scalarStep("is_platform_admin", true))
			stub.append(transactionSteps(true)...)
			stub.append(existing, sqlStep{kind: "rollback"})
			_, service, _ := newServices(t, store)
			_, err := service.SaveConnection(context.Background(), 7, 0, input)
			wantError(t, err, dbmanagement.ErrDuplicateConnection)
			wantError(t, err, access.ErrConflict)
		})
	}
}

func TestDatabaseConfigurationAndReviewSigning(t *testing.T) {
	_, store, _ := newSQLStub(t)
	auth := newAccess(t, store, nil)
	for _, change := range []func(*dbmanagement.Config){func(c *dbmanagement.Config) { c.Key = nil }, func(c *dbmanagement.Config) { c.ConnectTimeout = 0 }, func(c *dbmanagement.Config) { c.QueryTimeout = -time.Second }} {
		config := dbmanagement.DefaultConfig(testKey, false)
		change(&config)
		_, err := dbmanagement.NewService(store, auth, config)
		wantError(t, err, access.ErrInvalid)
	}
	key := bytes.Repeat([]byte{9}, 32)
	service, err := dbmanagement.NewService(store, auth, dbmanagement.DefaultConfig(key, false))
	if err != nil {
		t.Fatal(err)
	}
	first := service.SignReview("exact SQL and metadata")
	decoded, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil || len(decoded) != 32 {
		t.Fatal("review signature is not a SHA-256 MAC")
	}
	if first != service.SignReview("exact SQL and metadata") || first == service.SignReview("edited SQL and metadata") {
		t.Fatal("review MAC does not bind the reviewed data")
	}
	key[0]++
	if first != service.SignReview("exact SQL and metadata") {
		t.Fatal("service retained the caller's mutable key slice")
	}
	other, err := dbmanagement.NewService(store, auth, dbmanagement.DefaultConfig(key, false))
	if err != nil {
		t.Fatal(err)
	}
	if first == other.SignReview("exact SQL and metadata") {
		t.Fatal("review MAC is not bound to the secret key")
	}
}
