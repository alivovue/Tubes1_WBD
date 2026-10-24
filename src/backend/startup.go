package backend

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"
	"tubes1_wbd/database/setup"
)

var ErrAdminSetupRequired = errors.New("initial admin setup is required; start with ./run.sh from the repository root")

func servicesFromEnvironment(root string) (Services, func(), error) {
	production := os.Getenv("SCHAKKER_ENV") == "production"
	dsn := os.Getenv("SCHAKKER_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if production && dsn == "" {
		return Services{}, nil, errors.New("production requires SCHAKKER_DATABASE_URL")
	}
	directory := os.Getenv("SCHAKKER_STATE_DIR")
	if directory == "" {
		directory = filepath.Join(root, ".schakker")
	}
	key, err := masterKey(directory, production)
	if err != nil {
		return Services{}, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var db *sql.DB
	stopDatabase := func() {}
	if dsn == "" {
		db, stopDatabase, err = setup.OpenLocal(ctx, directory)
	} else {
		db, err = setup.Open(ctx, dsn)
	}
	if err != nil {
		return Services{}, nil, err
	}
	closeDB := func() { db.Close(); stopDatabase() }
	fail := func(err error) (Services, func(), error) { closeDB(); return Services{}, nil, err }
	if err := setup.EnsureInitialized(ctx, db); err != nil {
		return fail(err)
	}
	store, err := dao.New(db)
	if err != nil {
		return fail(err)
	}
	auth, err := access.NewService(store, access.DefaultConfig(key))
	if err != nil {
		return fail(err)
	}
	exists, err := store.UsersExist(ctx)
	if err != nil {
		return fail(err)
	}
	if !exists {
		username, password := os.Getenv("SCHAKKER_ADMIN_USERNAME"), os.Getenv("SCHAKKER_ADMIN_PASSWORD")
		display := os.Getenv("SCHAKKER_ADMIN_NAME")
		if display == "" {
			display = username
		}
		if username == "" || password == "" {
			return fail(ErrAdminSetupRequired)
		}
		if err := auth.BootstrapAdmin(ctx, username, display, password); err != nil {
			return fail(errors.New("initial admin could not be created; use a valid username and a password containing 12–72 bytes"))
		}
	}
	databases, err := dbmanagement.NewService(store, auth, dbmanagement.DefaultConfig(key, production))
	if err != nil {
		return fail(err)
	}
	migrations, err := migration.NewService(store, auth, databases)
	if err != nil {
		return fail(err)
	}
	return Services{Access: auth, Databases: databases, Migrations: migrations, Production: production}, closeDB, nil
}

func masterKey(directory string, production bool) ([]byte, error) {
	encoded := strings.TrimSpace(os.Getenv("SCHAKKER_MASTER_KEY"))
	if encoded == "" {
		if production {
			return nil, errors.New("production requires SCHAKKER_MASTER_KEY")
		}
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
		if err := os.Chmod(directory, 0700); err != nil {
			return nil, err
		}
		filename := filepath.Join(directory, "master.key")
		data, err := os.ReadFile(filename)
		if errors.Is(err, os.ErrNotExist) {
			if _, err := os.Stat(filepath.Join(directory, "postgres", "PG_VERSION")); err == nil {
				return nil, errors.New("the existing local database is missing its master key; restore .schakker/master.key")
			}
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return nil, err
			}
			data = []byte(hex.EncodeToString(key) + "\n")
			temporary, err := os.CreateTemp(directory, ".master-key-")
			if err != nil {
				return nil, err
			}
			defer os.Remove(temporary.Name())
			if _, err := temporary.Write(data); err != nil {
				temporary.Close()
				return nil, err
			}
			if err := temporary.Sync(); err != nil {
				temporary.Close()
				return nil, err
			}
			if err := temporary.Close(); err != nil {
				return nil, err
			}
			if err := os.Link(temporary.Name(), filename); err != nil && !errors.Is(err, os.ErrExist) {
				return nil, err
			}
			data, err = os.ReadFile(filename)
			if err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		if err := os.Chmod(filename, 0600); err != nil {
			return nil, err
		}
		encoded = strings.TrimSpace(string(data))
	}
	key, err := hex.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		key, err = base64.StdEncoding.DecodeString(encoded)
	}
	if err != nil || len(key) != 32 {
		return nil, errors.New("the master key must contain 32 bytes encoded as hex or base64")
	}
	return key, nil
}
