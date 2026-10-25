package integration_test

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/database/dao"
	fixtures "tubes1_wbd/test/data"
)

func requireMariaDBTools(t *testing.T) {
	t.Helper()
	if os.Getenv("SCHAKKER_MARIADB_BIN") != "" {
		return
	}
	if !fixtures.MariaDBAvailable() {
		t.Skip("optional MariaDB fixture test requires MariaDB server tools")
	}
}

func TestMariaDBMockRetainsDataAndRegistersWithPermissions(t *testing.T) {
	requireMariaDBTools(t)
	f := newFixture(t, true)
	writer := f.user(t, "mock.writer")
	reader := f.user(t, "mock.reader")
	directory := t.TempDir()
	mocks, err := fixtures.OpenWithOptions(f.ctx, directory, fixtures.Options{MariaDB: true})
	if err != nil {
		for _, filename := range []string{"init.log", "server.log"} {
			if output, readErr := os.ReadFile(filepath.Join(directory, "mariadb", filename)); readErr == nil {
				t.Logf("%s:\n%s", filename, output)
			}
		}
	}
	must(t, err)
	t.Cleanup(func() { mocks.Close() })
	if mocks.Cluster != nil {
		t.Fatal("MariaDB-only fixtures also started PostgreSQL")
	}
	input := mocks.MariaDB.Connection()
	must(t, mocks.MariaDB.AttachManaged(f.ctx, f.store, f.access, f.databases))
	connections, err := f.databases.Connections(f.ctx, f.admin)
	must(t, err)
	var target dao.Database
	for _, connection := range connections {
		if connection.DBMS == "mariadb" && connection.DatabaseName == input.DatabaseName {
			target = connection
		}
	}
	if target.ID == 0 || !target.Active || target.Port != input.Port {
		t.Fatal("mock MariaDB was not registered as an active managed connection")
	}
	for _, check := range []struct {
		user       int64
		permission access.Permission
	}{{f.admin, access.ViewEdit}, {writer.ID, access.ViewEdit}, {reader.ID, access.ViewOnly}} {
		permission, err := f.access.EffectivePermission(f.ctx, check.user, target.ID)
		must(t, err)
		if permission != check.permission {
			t.Fatal("mock MariaDB groups did not provide the expected permission")
		}
	}
	detail, err := f.databases.DescribeTable(f.ctx, reader.ID, target.ID, input.DatabaseName, "employees")
	must(t, err)
	if len(detail.Columns) != 3 || detail.Columns[0].Name != "id" || detail.Columns[1].Name != "name" || detail.Columns[2].Name != "salary" || detail.Table.Engine != "InnoDB" {
		t.Fatal("MariaDB employees schema does not match the fixture")
	}
	page, err := f.databases.TableData(f.ctx, reader.ID, target.ID, input.DatabaseName, "employees", dbmanagement.PageOptions{Limit: 10})
	must(t, err)
	text := func(value any) string {
		if bytes, ok := value.([]byte); ok {
			return string(bytes)
		}
		return fmt.Sprint(value)
	}
	if len(page.Rows) != 2 || text(page.Rows[0][1]) != "Alice" || text(page.Rows[0][2]) != "5000" || text(page.Rows[1][1]) != "Bob" || text(page.Rows[1][2]) != "6000" {
		t.Fatal("MariaDB mock rows do not match Alice and Bob")
	}
	config := mysql.NewConfig()
	config.Net, config.Addr = "tcp", net.JoinHostPort(input.Host, strconv.Itoa(input.Port))
	config.User, config.Passwd, config.DBName = input.Username, input.Password, input.DatabaseName
	connector, err := mysql.NewConnector(config)
	must(t, err)
	managed := sql.OpenDB(connector)
	t.Cleanup(func() { managed.Close() })
	if _, err := managed.ExecContext(f.ctx, `SELECT * FROM mysql.global_priv`); err == nil {
		t.Fatal("managed mock account can access MariaDB administration tables")
	}
	_, err = managed.ExecContext(f.ctx, `UPDATE employees SET salary=7000 WHERE id=1`)
	must(t, err)
	must(t, managed.Close())
	mocks.Close()
	if connection, err := net.DialTimeout("tcp", config.Addr, time.Second); err == nil {
		connection.Close()
		t.Fatal("closing the fixture left its private MariaDB server running")
	}
	mocks, err = fixtures.OpenWithOptions(f.ctx, directory, fixtures.Options{MariaDB: true})
	must(t, err)
	if mocks.MariaDB.Connection() != input {
		t.Fatal("reopening changed retained MariaDB connection settings or credentials")
	}
	must(t, mocks.MariaDB.AttachManaged(f.ctx, f.store, f.access, f.databases))
	reopened, err := f.databases.Connections(f.ctx, f.admin)
	must(t, err)
	if len(reopened) != len(connections) {
		t.Fatal("reopening duplicated the managed MariaDB connection")
	}
	page, err = f.databases.TableData(f.ctx, reader.ID, target.ID, input.DatabaseName, "employees", dbmanagement.PageOptions{Limit: 10})
	must(t, err)
	if len(page.Rows) != 2 || text(page.Rows[0][2]) != "7000" {
		t.Fatal("reopening reset retained MariaDB data")
	}
}
