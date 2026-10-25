package data

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/database/dao"
	"tubes1_wbd/database/setup"
)

const WebsiteName = "schakker_mock_website"
const ManagedName = "schakker_mock_managed"
const managedRole = "schakker_managed"

//go:embed website/groups.sql
var groupsSQL string

//go:embed website/accounts.sql
var accountsSQL string

//go:embed website/memberships.sql
var membershipsSQL string

//go:embed managed/employees.sql
var managedSQL string

type Factory struct {
	Cluster     *setup.LocalCluster
	MariaDB     *MariaDB
	password    string
	managedName string
	id          string
}
type Options struct {
	Website, Managed, MariaDB bool
}
type Databases struct {
	Website, Managed         *sql.DB
	WebsiteName, ManagedName string
	factory                  *Factory
}

func Open(ctx context.Context, directory string) (*Factory, error) {
	return OpenSelected(ctx, directory, true, true)
}
func OpenSelected(ctx context.Context, directory string, website, managed bool) (*Factory, error) {
	return OpenWithOptions(ctx, directory, Options{Website: website, Managed: managed})
}
func OpenWithOptions(ctx context.Context, directory string, options Options) (*Factory, error) {
	if err := ensureFixtureDirectory(directory); err != nil {
		return nil, err
	}
	factory := &Factory{}
	var err error
	if options.Website || options.Managed {
		factory, err = openPostgres(ctx, directory, options.Website, options.Managed)
		if err != nil {
			return nil, err
		}
	}
	if options.MariaDB {
		factory.MariaDB, err = openMariaDB(ctx, filepath.Join(directory, "mariadb"))
		if err != nil {
			factory.Close()
			return nil, err
		}
	}
	return factory, nil
}
func ensureFixtureDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	marker := filepath.Join(directory, ".schakker-fixtures")
	if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return errors.New("mock databases require a dedicated empty fixture directory")
		}
		if err := os.WriteFile(marker, []byte("schakker-test-fixtures-v1\n"), 0600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}
func openPostgres(ctx context.Context, directory string, website, managed bool) (*Factory, error) {
	cluster, err := setup.StartLocalCluster(ctx, directory, setup.LocalOptions{TCPRole: managedRole})
	if err != nil {
		return nil, err
	}
	absolute, _ := filepath.Abs(directory)
	sum := sha256.Sum256([]byte(absolute))
	id := hex.EncodeToString(sum[:6])
	factory := &Factory{Cluster: cluster, id: id, managedName: ManagedName + "_" + id}
	fail := func(err error) (*Factory, error) { factory.Close(); return nil, err }
	admin, err := cluster.Open(ctx, "postgres")
	if err != nil {
		return fail(err)
	}
	defer admin.Close()
	var roleExists bool
	if err := admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, managedRole).Scan(&roleExists); err != nil {
		return fail(err)
	}
	passwordFile := filepath.Join(directory, "managed.key")
	password, err := os.ReadFile(passwordFile)
	if errors.Is(err, os.ErrNotExist) {
		if roleExists {
			return fail(errors.New("mock managed database credentials are missing; restore managed.key"))
		}
		generated := make([]byte, 32)
		if _, err := rand.Read(generated); err != nil {
			return fail(err)
		}
		password = []byte(hex.EncodeToString(generated))
		if err := os.WriteFile(passwordFile, password, 0600); err != nil {
			return fail(err)
		}
	} else if err != nil {
		return fail(err)
	}
	factory.password = strings.TrimSpace(string(password))
	if len(factory.password) != 64 {
		return fail(errors.New("mock managed database credentials are invalid"))
	}
	if !roleExists {
		statement := "CREATE ROLE " + pgx.Identifier{managedRole}.Sanitize() + " LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD '" + strings.ReplaceAll(factory.password, "'", "''") + "'"
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			return fail(errors.New("mock managed database role could not be created"))
		}
	}
	if website {
		if _, err := cluster.EnsureDatabase(ctx, WebsiteName, "", ""); err != nil {
			return fail(err)
		}
		db, err := cluster.Open(ctx, WebsiteName)
		if err != nil {
			return fail(err)
		}
		err = setup.EnsureInitialized(ctx, db)
		if err == nil {
			_, err = db.ExecContext(ctx, groupsSQL)
		}
		db.Close()
		if err != nil {
			return fail(err)
		}
	} else if err := factory.drop(ctx, WebsiteName); err != nil {
		return fail(err)
	}
	if managed {
		created, err := cluster.EnsureDatabase(ctx, factory.managedName, managedRole, "")
		if err != nil {
			return fail(err)
		}
		if created {
			db, err := setup.Open(ctx, factory.ManagedDSN(factory.managedName))
			if err != nil {
				return fail(err)
			}
			_, err = db.ExecContext(ctx, managedSQL)
			db.Close()
			if err != nil {
				return fail(err)
			}
		}
	} else if err := factory.drop(ctx, factory.managedName); err != nil {
		return fail(err)
	}
	return factory, nil
}
func (f *Factory) Close() {
	if f == nil {
		return
	}
	if f.MariaDB != nil {
		f.MariaDB.Close()
	}
	if f.Cluster != nil {
		f.Cluster.Close()
	}
}
func (f *Factory) ManagedDSN(database string) string {
	return (&url.URL{Scheme: "postgres", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(f.Cluster.Port)), User: url.UserPassword(managedRole, f.password), Path: "/" + database, RawQuery: url.Values{"sslmode": {"disable"}, "connect_timeout": {"5"}}.Encode()}).String()
}
func (f *Factory) Connection(database string) dbmanagement.ConnectionInput {
	return dbmanagement.ConnectionInput{DBMS: "postgresql", Name: "Mock Managed Database " + f.id, Host: "127.0.0.1", Port: f.Cluster.Port, DatabaseName: database, Username: managedRole, Password: f.password, SSLMode: "disable", Active: true}
}
func (f *Factory) Clone(ctx context.Context, label string) (*Databases, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	label = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, strings.ToLower(label))
	if len(label) > 12 {
		label = label[:12]
	}
	prefix := "schakker_it_" + label + "_" + hex.EncodeToString(random)
	result := &Databases{WebsiteName: prefix + "_website", ManagedName: prefix + "_managed", factory: f}
	if _, err := f.Cluster.EnsureDatabase(ctx, result.WebsiteName, "", WebsiteName); err != nil {
		return nil, err
	}
	if _, err := f.Cluster.EnsureDatabase(ctx, result.ManagedName, managedRole, f.managedName); err != nil {
		result.Close(ctx)
		return nil, err
	}
	var err error
	result.Website, err = f.Cluster.Open(ctx, result.WebsiteName)
	if err != nil {
		result.Close(ctx)
		return nil, err
	}
	result.Managed, err = setup.Open(ctx, f.ManagedDSN(result.ManagedName))
	if err != nil {
		result.Close(ctx)
		return nil, err
	}
	return result, nil
}
func (d *Databases) Connection() dbmanagement.ConnectionInput {
	return d.factory.Connection(d.ManagedName)
}
func (d *Databases) Close(ctx context.Context) error {
	if d.Website != nil {
		d.Website.Close()
	}
	if d.Managed != nil {
		d.Managed.Close()
	}
	first := d.factory.drop(ctx, d.WebsiteName)
	second := d.factory.drop(ctx, d.ManagedName)
	if first != nil {
		return first
	}
	return second
}
func (f *Factory) drop(ctx context.Context, name string) error {
	if name != WebsiteName && name != f.managedName && !strings.HasPrefix(name, "schakker_it_") {
		return errors.New("refusing to remove a database outside the mock fixtures")
	}
	admin, err := f.Cluster.Open(ctx, "postgres")
	if err != nil {
		return err
	}
	defer admin.Close()
	_, err = admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	return err
}

func PopulateWebsite(ctx context.Context, website *sql.DB, store *dao.Store, auth *access.Service, password string) error {
	users, err := store.Users(ctx)
	if err != nil {
		return err
	}
	var admin int64
	for _, user := range users {
		if user.Active && user.IsAdmin {
			admin = user.ID
			break
		}
	}
	if admin == 0 {
		return errors.New("mock website setup requires an active admin")
	}
	for _, entry := range []struct{ name, description string }{{"Mock Editors", "Sample editors"}, {"Mock Readers", "Sample readers"}} {
		groups, err := store.Groups(ctx)
		if err != nil {
			return err
		}
		exists := false
		for _, group := range groups {
			if group.Name == entry.name {
				exists = true
				break
			}
		}
		if !exists {
			if err := auth.SaveGroup(ctx, admin, 0, entry.name, entry.description); err != nil {
				return err
			}
		}
	}
	if password != "" {
		writer, err := access.HashPassword(password)
		if err != nil {
			return err
		}
		reader, err := access.HashPassword(password)
		if err != nil {
			return err
		}
		_, err = website.ExecContext(ctx, accountsSQL, writer, reader, admin)
		if err != nil {
			return err
		}
		_, err = website.ExecContext(ctx, membershipsSQL)
		return err
	}
	return nil
}
func (f *Factory) AttachManaged(ctx context.Context, store *dao.Store, auth *access.Service, databases *dbmanagement.Service) error {
	return attachManaged(ctx, store, auth, databases, f.Connection(f.managedName))
}
func attachManaged(ctx context.Context, store *dao.Store, auth *access.Service, databases *dbmanagement.Service, input dbmanagement.ConnectionInput) error {
	if err := PopulateWebsite(ctx, nil, store, auth, ""); err != nil {
		return err
	}
	users, err := store.Users(ctx)
	if err != nil {
		return err
	}
	var actor int64
	for _, user := range users {
		if user.IsAdmin && user.Active {
			actor = user.ID
			break
		}
	}
	connections, err := databases.Connections(ctx, actor)
	if err != nil {
		return err
	}
	var target dao.Database
	for _, connection := range connections {
		if connection.DBMS == input.DBMS && connection.Host == input.Host && connection.Port == input.Port && connection.DatabaseName == input.DatabaseName {
			target = connection
			break
		}
	}
	if target.ID == 0 {
		target, err = databases.SaveConnection(ctx, actor, 0, input)
		if err != nil {
			return err
		}
	}
	groups, err := store.Groups(ctx)
	if err != nil {
		return err
	}
	memberships, err := store.Memberships(ctx)
	if err != nil {
		return err
	}
	grants, err := store.Grants(ctx)
	if err != nil {
		return err
	}
	for _, group := range groups {
		permission := access.NoAccess
		if group.Name == "Mock Editors" {
			permission = access.ViewEdit
		} else if group.Name == "Mock Readers" {
			permission = access.ViewOnly
		} else {
			continue
		}
		for _, user := range users {
			belongs := group.Name == "Mock Editors" && (user.IsAdmin || user.Username == "mock.writer") || group.Name == "Mock Readers" && user.Username == "mock.reader"
			if !belongs || !user.Active {
				continue
			}
			exists := false
			for _, membership := range memberships {
				if membership.UserID == user.ID && membership.GroupID == group.ID {
					exists = true
					break
				}
			}
			if !exists {
				if err := auth.SetMembership(ctx, actor, user.ID, group.ID, true); err != nil {
					return err
				}
			}
		}
		exists := false
		for _, grant := range grants {
			if grant.GroupID == group.ID && grant.DatabaseID == target.ID && grant.Permission == permission {
				exists = true
				break
			}
		}
		if !exists {
			if err := auth.SetGrant(ctx, actor, group.ID, target.ID, permission); err != nil {
				return err
			}
		}
	}
	return nil
}
