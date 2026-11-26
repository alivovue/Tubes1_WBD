package access

import (
	"context"
	"fmt"
	"strings"

	"tubes1_wbd_admin/database/dao"
)

func (s *Service) CreateUser(ctx context.Context, actor int64, username, display, password string) error {
	if !usernamePattern.MatchString(username) || !cleanName(display, 150) {
		return ErrInvalid
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		return tx.CreateUser(ctx, username, strings.TrimSpace(display), hash, &actor)
	})
}

func (s *Service) UpdateUser(ctx context.Context, actor, id int64, display string, active bool, password string) error {
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
		return tx.UpdateUser(ctx, id, strings.TrimSpace(display), active, hash)
	})
}

func (s *Service) CreateAdmin(ctx context.Context, actor int64, username, display, password string) error {
	if !usernamePattern.MatchString(username) || !cleanName(display, 150) {
		return ErrInvalid
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		return tx.CreateAdmin(ctx, username, strings.TrimSpace(display), hash)
	})
}
func (s *Service) UpdateAdmin(ctx context.Context, actor, id int64, display string, active bool, password string) error {
	if id < 1 || !cleanName(display, 150) {
		return ErrInvalid
	}
	hash := ""
	if password != "" {
		var err error
		hash, err = HashPassword(password)
		if err != nil {
			return err
		}
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		previous, err := tx.Admin(ctx, id)
		if err != nil {
			return err
		}
		if previous.Active && !active {
			count, err := tx.ActiveAdminCount(ctx)
			if err != nil {
				return err
			}
			if count <= 1 {
				return fmt.Errorf("%w: at least one administrator must remain active", ErrInvalid)
			}
		}
		if err := tx.UpdateAdmin(ctx, id, strings.TrimSpace(display), active, hash); err != nil {
			return err
		}
		if previous.Active != active || !active || hash != "" {
			return tx.RevokeUserSessions(ctx, id, s.config.Now())
		}
		return nil
	})
}
func (s *Service) Administrators(ctx context.Context, actor int64) ([]User, error) {
	if err := s.RequireAdmin(ctx, actor); err != nil {
		return nil, err
	}
	return s.store.Administrators(ctx)
}
