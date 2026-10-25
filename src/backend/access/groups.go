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
	return s.SetGrants(ctx, actor, group, []GrantUpdate{{DatabaseID: database, Permission: permission}})
}

type GrantUpdate struct {
	DatabaseID int64
	Permission Permission
}

func (s *Service) SetGrants(ctx context.Context, actor, group int64, updates []GrantUpdate) error {
	if group < 1 || len(updates) == 0 {
		return ErrInvalid
	}
	seen := make(map[int64]bool, len(updates))
	for _, update := range updates {
		if update.DatabaseID < 1 || seen[update.DatabaseID] || (update.Permission != NoAccess && update.Permission != ViewOnly && update.Permission != ViewEdit) {
			return ErrInvalid
		}
		seen[update.DatabaseID] = true
	}
	return s.adminChange(ctx, actor, func(tx *dao.Store) error {
		increased, reduced := false, false
		for _, update := range updates {
			previous, err := tx.GroupPermission(ctx, group, update.DatabaseID)
			if err != nil {
				return err
			}
			if err := tx.SetGrant(ctx, actor, group, update.DatabaseID, update.Permission); err != nil {
				return err
			}
			if permissionIncreased(previous, update.Permission) {
				increased = true
			} else if previous != update.Permission {
				reduced = true
			}
		}
		if reduced {
			return tx.RevokeGroupSessions(ctx, group, s.config.Now())
		}
		if increased {
			return s.rotateGroupSessions(ctx, tx, group)
		}
		return nil
	})
}
