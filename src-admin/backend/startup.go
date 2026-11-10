package backend

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/connectionconfig"
	"tubes1_wbd/database/dao"
	"tubes1_wbd/database/platform"
	"tubes1_wbd/database/setup"
	adminaccess "tubes1_wbd_admin/backend/access"
	admindao "tubes1_wbd_admin/database/dao"
	adminsetup "tubes1_wbd_admin/database/setup"
)

type runtime struct{ platform, admin *sql.DB }

func (r *runtime) Close() error {
	first := r.admin.Close()
	second := r.platform.Close()
	if first != nil {
		return first
	}
	return second
}

func servicesFromEnvironment(root string) (Services, *runtime, error) {
	platformURL, adminURL := platform.DatabaseURL(), os.Getenv("SCHAKKER_ADMIN_DATABASE_URL")
	if platformURL == "" || adminURL == "" {
		return Services{}, nil, errors.New("SCHAKKER_PLATFORM_DATABASE_URL and SCHAKKER_ADMIN_DATABASE_URL are required; provision both databases and the initial administrator outside the application")
	}
	production := os.Getenv("SCHAKKER_ENV") == "production"
	key, err := platform.MasterKey(platform.StateDirectory(filepath.Join(root, "..", "src")), production)
	if err != nil {
		return Services{}, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state := &runtime{}
	fail := func(err error) (Services, *runtime, error) {
		if state.admin != nil {
			state.admin.Close()
		}
		if state.platform != nil {
			state.platform.Close()
		}
		return Services{}, nil, err
	}
	state.platform, err = setup.Open(ctx, platformURL)
	if err != nil {
		return fail(err)
	}
	if err := setup.ValidateRuntimeRole(ctx, state.platform); err != nil {
		return fail(err)
	}
	if err := setup.ValidateSchema(ctx, state.platform); err != nil {
		return fail(err)
	}
	state.admin, err = setup.Open(ctx, adminURL)
	if err != nil {
		return fail(err)
	}
	if err := setup.ValidateRuntimeRole(ctx, state.admin); err != nil {
		return fail(err)
	}
	if err := adminsetup.ValidateSchema(ctx, state.admin); err != nil {
		return fail(err)
	}
	adminStore, err := admindao.New(state.admin)
	if err != nil {
		return fail(err)
	}
	auth, err := adminaccess.NewService(adminStore, access.DefaultConfig(key))
	if err != nil {
		return fail(err)
	}
	store, err := dao.New(state.platform)
	if err != nil {
		return fail(err)
	}
	administration, err := access.NewAdministrationService(store, auth, access.DefaultConfig(key))
	if err != nil {
		return fail(err)
	}
	connections, err := connectionconfig.NewService(store, administration, connectionconfig.DefaultConfig(key, production))
	if err != nil {
		return fail(err)
	}
	return Services{Authentication: auth, Access: administration, Connections: connections, Production: production}, state, nil
}
