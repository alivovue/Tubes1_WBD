// Bootstrap helpers belong only to isolated test setup, never application startup.
package integration_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/database/dao"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{2,99}$`)

func bootstrapAdmin(ctx context.Context, store *dao.Store, username, display, password string) error {
	if !usernamePattern.MatchString(username) || (strings.TrimSpace(display) == "" || len(display) > 150) {
		return access.ErrInvalid
	}
	hash, err := access.HashPassword(password)
	if err != nil {
		return err
	}
	return store.Transaction(ctx, func(tx *dao.Store) error {
		if err := tx.LockAdministration(ctx); err != nil {
			return err
		}
		users, err := tx.Administrators(ctx)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			return fmt.Errorf("%w: bootstrap hanya untuk platform kosong", access.ErrConflict)
		}
		return tx.CreateAdmin(ctx, username, strings.TrimSpace(display), hash)
	})
}
