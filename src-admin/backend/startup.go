package backend

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/connectionconfig"
	"tubes1_wbd_admin/backend/internalapi"
	"tubes1_wbd_admin/database/dao"
	"tubes1_wbd_admin/database/setup"
)

type runtime struct {
	db       *sql.DB
	internal http.Handler
}

func (r *runtime) Close() error                         { return r.db.Close() }
func (h *managedHandler) InternalHandler() http.Handler { return h.runtime.internal }

func servicesFromEnvironment(root string) (Services, *runtime, error) {
	dsn := strings.TrimSpace(os.Getenv("SCHAKKER_ADMIN_DATABASE_URL"))
	if dsn == "" {
		return Services{}, nil, errors.New("SCHAKKER_ADMIN_DATABASE_URL is required; provision the database and initial administrator externally")
	}
	key, err := adminMasterKey()
	if err != nil {
		return Services{}, nil, err
	}
	token := os.Getenv("SCHAKKER_INTERNAL_TOKEN")
	if len(token) < 32 {
		return Services{}, nil, errors.New("SCHAKKER_INTERNAL_TOKEN must contain at least 32 bytes")
	}
	production := os.Getenv("SCHAKKER_ADMIN_ENV") == "production"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := setup.Open(ctx, dsn)
	if err != nil {
		return Services{}, nil, err
	}
	fail := func(err error) (Services, *runtime, error) { db.Close(); return Services{}, nil, err }
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
	connections, err := connectionconfig.NewService(store, auth, connectionconfig.DefaultConfig(key, production))
	if err != nil {
		return fail(err)
	}
	internal, err := internalapi.NewHandler(store, key, token)
	if err != nil {
		return fail(err)
	}
	return Services{Authentication: auth, Access: auth, Connections: connections, Production: production}, &runtime{db: db, internal: internal}, nil
}
func adminMasterKey() ([]byte, error) {
	encoded := strings.TrimSpace(os.Getenv("SCHAKKER_ADMIN_MASTER_KEY"))
	key, err := hex.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		key, err = base64.StdEncoding.DecodeString(encoded)
	}
	if err != nil || len(key) != 32 {
		return nil, errors.New("SCHAKKER_ADMIN_MASTER_KEY must contain an existing 32-byte key encoded as hex or base64")
	}
	return key, nil
}
