package unit_test

import (
	"context"
	"database/sql/driver"
	"testing"
	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/connectionconfig"
)

func TestDuplicateConnectionsAreRejectedBeforeConnecting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		host   string
		active bool
		change func(*connectionconfig.ConnectionInput)
	}{
		{"different display name", "127.0.0.1", true, nil},
		{"different username", "127.0.0.1", true, func(in *connectionconfig.ConnectionInput) { in.Username = "another-user" }},
		{"different TLS mode", "127.0.0.1", true, func(in *connectionconfig.ConnectionInput) { in.SSLMode = "require" }},
		{"default port and DBMS alias", "127.0.0.1", true, func(in *connectionconfig.ConnectionInput) { in.Port = 0; in.DBMS = "postgres" }},
		{"host case and trailing dot", "LOCALHOST.", true, func(in *connectionconfig.ConnectionInput) { in.Host = "localhost" }},
		{"hostname alias", "localhost", true, nil},
		{"IPv4 mapped address", "::ffff:127.0.0.1", true, nil},
		{"disabled existing connection", "127.0.0.1", false, nil},
		{"disabled duplicate", "127.0.0.1", true, func(in *connectionconfig.ConnectionInput) { in.Active = false }},
		{"MariaDB", "127.0.0.1", true, func(in *connectionconfig.ConnectionInput) { in.DBMS = "mariadb"; in.Port = 3306 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := connectionconfig.ConnectionInput{DBMS: "postgresql", Name: "Duplicate target", Host: "127.0.0.1", Port: 5432, DatabaseName: "target", Username: "dbuser", SSLMode: "disable", Active: true}
			if tc.change != nil {
				tc.change(&input)
			}
			dbms, port := "postgresql", 5432
			if input.DBMS == "mariadb" {
				dbms, port = "mariadb", 3306
			}
			columns := []string{"id", "display_name", "dbms_type", "host", "port", "database_name", "is_active", "sslmode"}
			existing := queryStep("FROM managed_databases ORDER BY id", columns, []driver.Value{int64(3), "Original target", dbms, tc.host, int64(port), "target", tc.active, "disable"})
			_, store, stub := newSQLStub(t, scalarStep("admin_users", true))
			stub.append(transactionSteps(true)...)
			stub.append(existing, sqlStep{kind: "rollback"})
			service, err := connectionconfig.NewService(store, newAccess(t, store, nil), connectionconfig.DefaultConfig(testKey, false))
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.SaveConnection(context.Background(), 7, 0, input)
			wantError(t, err, connectionconfig.ErrDuplicateConnection)
			wantError(t, err, access.ErrConflict)
		})
	}
}
