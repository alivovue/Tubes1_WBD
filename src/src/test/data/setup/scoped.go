package setup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const UserRuntimeRole = "schakker_user"
const AdministrationRole = "schakker_administration"
const AdminAuthenticationRole = "schakker_admin_auth"

// OpenScoped uses the cluster owner only for schema/ACL setup, closes that
// connection, then returns a restricted application connection.
func OpenScoped(ctx context.Context, cluster *LocalCluster, name, role string, initialize func(context.Context, *sql.DB) error, grants string) (*sql.DB, error) {
	if role != UserRuntimeRole && role != AdministrationRole && role != AdminAuthenticationRole {
		return nil, errors.New("invalid application database role")
	}
	owner, err := cluster.Open(ctx, "postgres")
	if err != nil {
		return nil, err
	}
	defer owner.Close()
	password, err := rolePassword(cluster.Directory, role)
	if err != nil {
		return nil, err
	}
	var exists bool
	if err = owner.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, role).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		_, err = owner.ExecContext(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '"+password+"'")
		var pg *pgconn.PgError
		if err != nil && !(errors.As(err, &pg) && pg.Code == "42710") {
			return nil, errors.New("application database role could not be created")
		}
	}
	if _, err = cluster.EnsureDatabase(ctx, name, "", ""); err != nil {
		return nil, err
	}
	db, err := cluster.Open(ctx, name)
	if err != nil {
		return nil, err
	}
	if err = initialize(ctx, db); err == nil {
		_, err = db.ExecContext(ctx, "REVOKE ALL ON DATABASE "+pgx.Identifier{name}.Sanitize()+" FROM PUBLIC; GRANT CONNECT ON DATABASE "+pgx.Identifier{name}.Sanitize()+" TO "+pgx.Identifier{role}.Sanitize()+"; REVOKE CREATE ON SCHEMA public FROM PUBLIC; "+strings.ReplaceAll(grants, "{{role}}", pgx.Identifier{role}.Sanitize()))
	}
	db.Close()
	if err != nil {
		return nil, err
	}
	config, err := url.Parse(cluster.DSN(name))
	if err != nil {
		return nil, errors.New("local database configuration is invalid")
	}
	config.User = url.UserPassword(role, password)
	return Open(ctx, config.String())
}
func rolePassword(directory, role string) (string, error) {
	filename := filepath.Join(directory, role+".key")
	raw, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return "", err
		}
		file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			raw, err = os.ReadFile(filename)
		} else if err == nil {
			raw = []byte(hex.EncodeToString(key))
			_, err = file.Write(raw)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	password := strings.TrimSpace(string(raw))
	key, err := hex.DecodeString(password)
	if err != nil || len(key) != 32 {
		return "", errors.New("local application role key is invalid")
	}
	return password, nil
}
func ValidateRuntimeRole(ctx context.Context, db *sql.DB) error {
	var privileged bool
	if err := db.QueryRowContext(ctx, `SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&privileged); err != nil {
		return errors.New("application database role could not be checked")
	}
	if privileged {
		return errors.New("use a restricted application PostgreSQL role; initialize schemas and migrate with a separate owner connection (see docs/admin-auth-database.md)")
	}
	return nil
}

const UserRuntimeGrants = `
GRANT USAGE ON SCHEMA public TO {{role}};
GRANT SELECT ON users,access_groups,user_groups,group_database_permissions,managed_databases TO {{role}};
-- Row locks during login require UPDATE on one column; credentials and roles remain read-only.
GRANT UPDATE(updated_at) ON users TO {{role}};
GRANT SELECT,INSERT,UPDATE,DELETE ON sessions,session_tokens,migration_drafts,migration_executions TO {{role}};
GRANT UPDATE(connection_options) ON managed_databases TO {{role}};
GRANT USAGE ON SEQUENCE session_tokens_id_seq,migration_drafts_id_seq,migration_executions_id_seq TO {{role}};
`
const AdministrationGrants = `
GRANT USAGE ON SCHEMA public TO {{role}};
REVOKE ALL ON users FROM {{role}};
GRANT SELECT(id,username,display_name,is_active,is_legacy_admin) ON users TO {{role}};
GRANT INSERT(username,display_name,password_hash,created_by_admin_id) ON users TO {{role}};
GRANT UPDATE(display_name,is_active,password_hash,updated_at) ON users TO {{role}};
GRANT SELECT,INSERT,UPDATE,DELETE ON access_groups,user_groups,group_database_permissions,managed_databases TO {{role}};
GRANT SELECT,INSERT,UPDATE,DELETE ON sessions,session_tokens TO {{role}};
GRANT SELECT(database_id) ON migration_executions TO {{role}};
GRANT USAGE ON SEQUENCE users_id_seq,access_groups_id_seq,managed_databases_id_seq,session_tokens_id_seq TO {{role}};
`
