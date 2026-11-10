// Package platform connects ordinary-user authentication to existing metadata.
package platform

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"
	"tubes1_wbd/database/setup"
)

type Runtime struct {
	DB         *sql.DB
	Store      *dao.Store
	Access     *access.Service
	Key        []byte
	Production bool
}

// Open connects to externally provisioned metadata and validates its schema.
// It never starts a database server or initializes persistent state.
func Open(root string) (*Runtime, error) {
	dsn := DatabaseURL()
	if dsn == "" {
		return nil, errors.New("SCHAKKER_PLATFORM_DATABASE_URL is required; provision the platform database before starting the application")
	}
	production := os.Getenv("SCHAKKER_ENV") == "production"
	key, err := MasterKey(StateDirectory(root), production)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := setup.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Runtime, error) { db.Close(); return nil, err }
	if err := setup.ValidateRuntimeRole(ctx, db); err != nil {
		return fail(err)
	}
	if err := setup.ValidateSchema(ctx, db); err != nil {
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
	return &Runtime{DB: db, Store: store, Access: auth, Key: key, Production: production}, nil
}
func (r *Runtime) Close() error { return r.DB.Close() }

// MasterKey reads supplied configuration. It does not generate or write keys.
func MasterKey(directory string, production bool) ([]byte, error) {
	encoded := strings.TrimSpace(os.Getenv("SCHAKKER_MASTER_KEY"))
	if encoded == "" {
		if production {
			return nil, errors.New("production requires SCHAKKER_MASTER_KEY")
		}
		raw, err := os.ReadFile(filepath.Join(directory, "master.key"))
		if err != nil {
			return nil, errors.New("provide SCHAKKER_MASTER_KEY or an existing master.key; key generation is external to the application")
		}
		encoded = strings.TrimSpace(string(raw))
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
func DatabaseURL() string {
	for _, name := range []string{"SCHAKKER_PLATFORM_DATABASE_URL", "SCHAKKER_DATABASE_URL", "DATABASE_URL"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
func StateDirectory(root string) string {
	if value := os.Getenv("SCHAKKER_STATE_DIR"); value != "" {
		return value
	}
	return filepath.Join(root, ".schakker")
}
