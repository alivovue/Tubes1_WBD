package access

import (
	"context"
	"fmt"
	"strings"

	"tubes1_wbd/database/dao"
)

func (s *Service) CreateUser(ctx context.Context, actor int64, username, display, password string, admin bool) error {
	if admin || s.store.AdminAuthentication() || !usernamePattern.MatchString(username) || !cleanName(display, 150) {
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
	if admin || s.store.AdminAuthentication() || id < 1 || !cleanName(display, 150) {
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
		if err := tx.UpdateUser(ctx, id, strings.TrimSpace(display), admin, active, hash); err != nil {
			return err
		}
		if previous.Active != active || !active || hash != "" {
			return tx.RevokeUserSessions(ctx, id, s.config.Now())
		}
		return nil
	})
}

func (s *Service) CreateAdmin(ctx context.Context, actor int64, username, display, password string) error {
	if !s.store.AdminAuthentication() || !usernamePattern.MatchString(username) || !cleanName(display, 150) {
		return ErrInvalid
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		return tx.CreateUser(ctx, username, strings.TrimSpace(display), hash, true, nil)
	})
}
func (s *Service) UpdateAdmin(ctx context.Context, actor, id int64, display string, active bool, password string) error {
	if !s.store.AdminAuthentication() || id < 1 || !cleanName(display, 150) {
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
		previous, err := tx.User(ctx, id)
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
		if err := tx.UpdateUser(ctx, id, strings.TrimSpace(display), true, active, hash); err != nil {
			return err
		}
		if previous.Active != active || !active || hash != "" {
			return tx.RevokeUserSessions(ctx, id, s.config.Now())
		}
		return nil
	})
}
func (s *Service) Administrators(ctx context.Context, actor int64) ([]User, error) {
	if !s.store.AdminAuthentication() {
		return nil, ErrForbidden
	}
	if err := s.RequireAdmin(ctx, actor); err != nil {
		return nil, err
	}
	return s.store.Users(ctx)
}
