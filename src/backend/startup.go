package backend

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"
	"tubes1_wbd/database/setup"
)

func servicesFromEnvironment() (Services, func(), error) {
	dsn := os.Getenv("SCHAKKER_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		return Services{}, nil, errors.New("set SCHAKKER_DATABASE_URL to the website PostgreSQL database connection")
	}
	encoded := os.Getenv("SCHAKKER_MASTER_KEY")
	key, err := hex.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		key, err = base64.StdEncoding.DecodeString(encoded)
	}
	if err != nil || len(key) != 32 {
		return Services{}, nil, errors.New("set SCHAKKER_MASTER_KEY to a persistent 32-byte key encoded as hex or base64")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := setup.Open(ctx, dsn)
	if err != nil {
		return Services{}, nil, err
	}
	closeDB := func() { db.Close() }
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
			return fail(errors.New("the empty platform needs SCHAKKER_ADMIN_USERNAME and SCHAKKER_ADMIN_PASSWORD (12–72 bytes) for its first admin"))
		}
		if err := auth.BootstrapAdmin(ctx, username, display, password); err != nil {
			return fail(errors.New("initial admin could not be created; check the username and password settings"))
		}
	}
	production := os.Getenv("SCHAKKER_ENV") == "production"
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
