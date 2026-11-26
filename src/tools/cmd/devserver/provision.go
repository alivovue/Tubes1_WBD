package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	adminschema "schakker_schemas/admin"
	platformschema "schakker_schemas/platform"
	"schakker_tests/fixtures/postgre"
	fixtures "schakker_tests/fixtures/setup"
	platformsetup "tubes1_wbd/database/setup"
	adminaccess "tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/connectionconfig"
	admindao "tubes1_wbd_admin/database/dao"
	adminsetup "tubes1_wbd_admin/database/setup"
)

const roleGrants = `GRANT USAGE ON SCHEMA public TO {{role}};
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO {{role}};
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO {{role}};`
const targetRole = "schakker_dev_target"

type configuration struct {
	Version        int    `json:"version"`
	Key            string `json:"key"`
	Token          string `json:"token"`
	Password       string `json:"password"`
	TargetPassword string `json:"target_password"`
	DatabaseID     int64  `json:"database_id"`
	Initialized    bool   `json:"initialized"`
}

func readConfiguration(state string) (*configuration, error) {
	path := filepath.Join(state, "config.json")
	raw, err := os.ReadFile(path)
	if err == nil {
		var config configuration
		if json.Unmarshal(raw, &config) != nil || config.Version != 1 {
			return nil, errors.New("development config is invalid; restore its original config.json")
		}
		for _, value := range []string{config.Key, config.Token, config.TargetPassword} {
			key, err := hex.DecodeString(value)
			if err != nil || len(key) != 32 {
				return nil, errors.New("development keys are invalid; restore the original config.json")
			}
		}
		if len(config.Password) < 12 || len(config.Password) > 72 {
			return nil, errors.New("development account configuration is invalid")
		}
		return &config, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(state, "cluster")); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("development database exists without config.json; restore that file before continuing")
	}
	config := &configuration{Version: 1, Key: randomHex(32), Token: randomHex(32), TargetPassword: randomHex(32), Password: "Dev-" + randomHex(16) + "!"}
	return config, writeJSON(path, config)
}
func randomHex(size int) string {
	value := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value)
}
func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".dev-config-")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}
func roleURL(cluster *fixtures.LocalCluster, database, role string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(cluster.Directory, role+".key"))
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(cluster.DSN(database))
	if err != nil {
		return "", err
	}
	parsed.User = url.UserPassword(role, strings.TrimSpace(string(raw)))
	return parsed.String(), nil
}
func initializeOnce(ctx context.Context, db *sql.DB, table string, initialize func(context.Context, *sql.DB) error) error {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	return initialize(ctx, db)
}
func provision(ctx context.Context, state string, cluster *fixtures.LocalCluster, config *configuration) error {
	setupLog, err := os.OpenFile(filepath.Join(state, "setup.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer setupLog.Close()
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(setupLog, nil)))
	defer slog.SetDefault(previousLogger)
	adminDB, err := fixtures.OpenScoped(ctx, cluster, "schakker_dev_admin", fixtures.AdminAuthenticationRole, func(ctx context.Context, db *sql.DB) error {
		return initializeOnce(ctx, db, "admin_users", adminschema.Initialize)
	}, roleGrants)
	if err != nil {
		return err
	}
	defer adminDB.Close()
	if err := adminsetup.ValidateSchema(ctx, adminDB); err != nil {
		return err
	}
	userDB, err := fixtures.OpenScoped(ctx, cluster, "schakker_dev_platform", fixtures.UserRuntimeRole, func(ctx context.Context, db *sql.DB) error {
		return initializeOnce(ctx, db, "users", platformschema.Initialize)
	}, roleGrants)
	if err != nil {
		return err
	}
	defer userDB.Close()
	if err := platformsetup.ValidateSchema(ctx, userDB); err != nil {
		return err
	}
	if !config.Initialized {
		if err := seed(ctx, cluster, config, adminDB); err != nil {
			return err
		}
		config.Initialized = true
		if err := writeJSON(filepath.Join(state, "config.json"), config); err != nil {
			return err
		}
	}
	credentials := "LOCAL DEVELOPMENT ONLY\n\nAdmin http://localhost:3001: dev.admin\nEditor http://localhost:3000: dev.editor\nView Only leader http://localhost:3000: dev.reader\n\nPassword for these local accounts: " + config.Password + "\n\nThese accounts belong only to this checkout's private development database.\n"
	return os.WriteFile(filepath.Join(state, "credentials.txt"), []byte(credentials), 0600)
}
func seed(ctx context.Context, cluster *fixtures.LocalCluster, config *configuration, adminDB *sql.DB) error {
	owner, err := cluster.Open(ctx, "postgres")
	if err != nil {
		return err
	}
	defer owner.Close()
	var exists bool
	if err := owner.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, targetRole).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := owner.ExecContext(ctx, "CREATE ROLE "+pgx.Identifier{targetRole}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '"+config.TargetPassword+"'"); err != nil {
			return errors.New("cannot create development target role")
		}
	}
	if _, err := cluster.EnsureDatabase(ctx, "schakker_dev_target", targetRole, ""); err != nil {
		return err
	}
	target, err := cluster.Open(ctx, "schakker_dev_target")
	if err != nil {
		return err
	}
	defer target.Close()
	if err := initializeOnce(ctx, target, "employees", postgre.Seed); err != nil {
		return err
	}
	if _, err := target.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS public.demo_counters(id BIGINT PRIMARY KEY,value BIGINT NOT NULL); INSERT INTO public.demo_counters SELECT n,0 FROM generate_series(1,10000) n ON CONFLICT DO NOTHING; ALTER TABLE public.employees OWNER TO schakker_dev_target; ALTER SEQUENCE public.employees_id_seq OWNER TO schakker_dev_target; ALTER TABLE public.demo_counters OWNER TO schakker_dev_target;`); err != nil {
		return err
	}
	store, err := admindao.New(adminDB)
	if err != nil {
		return err
	}
	key, _ := hex.DecodeString(config.Key)
	authConfig := adminaccess.DefaultConfig(key)
	authConfig.Logger = slog.Default()
	auth, err := adminaccess.NewService(store, authConfig)
	if err != nil {
		return err
	}
	if _, err := store.AdminCredentials(ctx, "dev.admin"); errors.Is(err, admindao.ErrNotFound) {
		hash, err := adminaccess.HashPassword(config.Password)
		if err != nil {
			return err
		}
		if err := store.CreateAdmin(ctx, "dev.admin", "Local Administrator", hash); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	admin, err := store.AdminCredentials(ctx, "dev.admin")
	if err != nil {
		return err
	}
	for _, user := range []struct{ name, display string }{{"dev.editor", "Local Editor"}, {"dev.reader", "Local View Only Leader"}} {
		if _, err := store.Credentials(ctx, user.name); errors.Is(err, admindao.ErrNotFound) {
			if err := auth.CreateUser(ctx, admin.ID, user.name, user.display, config.Password); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	connections, err := connectionconfig.NewService(store, auth, connectionconfig.DefaultConfig(key, false))
	if err != nil {
		return err
	}
	databases, err := store.Databases(ctx)
	if err != nil {
		return err
	}
	for _, database := range databases {
		if database.Name == "Local PostgreSQL demo" {
			config.DatabaseID = database.ID
		}
	}
	if config.DatabaseID == 0 {
		database, err := connections.SaveConnection(ctx, admin.ID, 0, connectionconfig.ConnectionInput{DBMS: "postgresql", Name: "Local PostgreSQL demo", Host: "127.0.0.1", Port: cluster.Port, DatabaseName: "schakker_dev_target", Username: targetRole, Password: config.TargetPassword, SSLMode: "disable", Active: true})
		if err != nil {
			return err
		}
		config.DatabaseID = database.ID
	}
	for _, spec := range []struct {
		name, leader string
		permission   admindao.Permission
	}{{"Local editors", "dev.editor", admindao.ViewEdit}, {"Local auditors", "dev.reader", admindao.ViewOnly}} {
		leader, err := store.Credentials(ctx, spec.leader)
		if err != nil {
			return err
		}
		groups, err := store.Groups(ctx)
		if err != nil {
			return err
		}
		var id int64
		for _, group := range groups {
			if group.Name == spec.name {
				id = group.ID
			}
		}
		if id == 0 {
			if err := auth.SaveGroupWithLeader(ctx, admin.ID, 0, spec.name, "Local development", leader.ID); err != nil {
				return err
			}
			groups, err = store.Groups(ctx)
			if err != nil {
				return err
			}
			for _, group := range groups {
				if group.Name == spec.name {
					id = group.ID
				}
			}
		}
		if err := auth.SetGrant(ctx, admin.ID, id, config.DatabaseID, spec.permission); err != nil {
			return err
		}
	}
	return nil
}
