package access

import (
	"context"
	"strings"

	"tubes1_wbd/database/dao"
)

func (s *Service) SaveGroup(ctx context.Context, actor, id int64, name, description string) error {
	if id < 0 || !cleanName(name, 100) || len(description) > 2000 {
		return ErrInvalid
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		return tx.SaveGroup(ctx, id, strings.TrimSpace(name), description)
	})
}

func (s *Service) DeleteGroup(ctx context.Context, actor, id int64) error {
	if id < 1 {
		return ErrInvalid
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		if err := tx.RevokeGroupSessions(ctx, id, s.config.Now()); err != nil {
			return err
		}
		return tx.DeleteGroup(ctx, id)
	})
}

func (s *Service) SetMembership(ctx context.Context, actor, user, group int64, member bool) error {
	if user < 1 || group < 1 {
		return ErrInvalid
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		if err := tx.SetMembership(ctx, user, group, member); err != nil {
			return err
		}
		if member {
			return s.rotateUserSessions(ctx, tx, user)
		}
		return tx.RevokeUserSessions(ctx, user, s.config.Now())
	})
}

func (s *Service) SetGrant(ctx context.Context, actor, group, database int64, permission Permission) error {
	if group < 1 || database < 1 || (permission != NoAccess && permission != ViewOnly && permission != ViewEdit) {
		return ErrInvalid
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		previous, err := tx.GroupPermission(ctx, group, database)
		if err != nil {
			return err
		}
		if err := tx.SetGrant(ctx, actor, group, database, permission); err != nil {
			return err
		}
		if permissionIncreased(previous, permission) {
			return s.rotateGroupSessions(ctx, tx, group)
		}
		if previous == permission {
			return nil
		}
		return tx.RevokeGroupSessions(ctx, group, s.config.Now())
	})
}
