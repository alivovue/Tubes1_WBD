package access

import (
	"context"
	"fmt"
	"strings"

	"tubes1_wbd/database/dao"
)

func (s *Service) BootstrapAdmin(ctx context.Context, username, display, password string) error {
	if !usernamePattern.MatchString(username) || !cleanName(display, 150) {
		return ErrInvalid
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.store.Transaction(ctx, func(tx *dao.Store) error {
		if err := tx.LockAdministration(ctx); err != nil {
			return err
		}
		exists, err := tx.UsersExist(ctx)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("%w: bootstrap hanya untuk platform kosong", ErrConflict)
		}
		return tx.CreateUser(ctx, username, strings.TrimSpace(display), hash, true, nil)
	})
}

func (s *Service) CreateUser(ctx context.Context, actor int64, username, display, password string, admin bool) error {
	if !usernamePattern.MatchString(username) || !cleanName(display, 150) {
		return ErrInvalid
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		return tx.CreateUser(ctx, username, strings.TrimSpace(display), hash, admin, &actor)
	})
}

func (s *Service) UpdateUser(ctx context.Context, actor, id int64, display string, admin, active bool, password string) error {
	if id < 1 || !cleanName(display, 150) {
		return ErrInvalid
	}
	var hash string
	if password != "" {
		var err error
		hash, err = HashPassword(password)
		if err != nil {
			return err
		}
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		previous, err := tx.User(ctx, id)
		if err != nil {
			return err
		}
		if previous.IsAdmin && previous.Active && (!admin || !active) {
			count, err := tx.ActiveAdminCount(ctx)
			if err != nil {
				return err
			}
			if count <= 1 {
				return fmt.Errorf("%w: platform harus memiliki minimal satu admin aktif", ErrInvalid)
			}
		}
		if err := tx.UpdateUser(ctx, id, strings.TrimSpace(display), admin, active, hash); err != nil {
			return err
		}
		if previous.IsAdmin && !admin || previous.Active != active || !active || hash != "" {
			return tx.RevokeUserSessions(ctx, id, s.config.Now())
		}
		if !previous.IsAdmin && admin {
			return s.rotateUserSessions(ctx, tx, id)
		}
		return nil
	})
}
