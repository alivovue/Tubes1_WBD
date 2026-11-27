package integration_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/dbmanagement/adapters/postgres"
)

func TestInternalDatabasesCannotBeAttachedTargets(t *testing.T) {
	for _, admin := range []bool{false, true} {
		name := "platform"
		if admin {
			name = "admin"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, true)
			database, connection := f.dbs.WebsiteName, f.dbs.Website
			query := "SELECT id FROM public.users"
			if admin {
				database, connection = database+"_admin", f.adminDB
				query = "SELECT username,password_hash FROM public.admin_users"
			}
			input := factory.Connection(database)
			// Even an otherwise authorized database role must not expose app data.
			_, err := connection.ExecContext(f.ctx, "GRANT SELECT ON ALL TABLES IN SCHEMA public TO "+pgx.Identifier{input.Username}.Sanitize())
			must(t, err)
			db, err := postgres.New().Connect(f.ctx, adapters.Connection{Host: input.Host, Port: input.Port, DatabaseName: database, Username: input.Username, Password: input.Password, SSLMode: "disable", ConnectTimeout: time.Second, QueryTimeout: time.Second})
			if db != nil {
				db.Close()
				t.Fatal("internal database opened as a target")
			}
			wantError(t, err, adapters.ErrInvalid)
			target, err := f.saveConnection(f.ctx, f.admin, 0, input)
			must(t, err)
			group := f.grant(t, f.editor, target.ID, access.ViewOnly)
			_, err = f.databases.ReadQuery(f.ctx, f.editor, target.ID, query)
			wantError(t, err, adapters.ErrInvalid)
			must(t, f.access.SetGrant(f.ctx, f.admin, group.ID, target.ID, access.ViewEdit))
			_, err = f.databases.OpenMigrationTarget(f.ctx, f.editor, target.ID)
			wantError(t, err, adapters.ErrInvalid)
		})
	}
}
