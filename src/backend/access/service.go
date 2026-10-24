package access

import (
	"context"
	"errors"

	"tubes1_wbd/database/dao"

	"golang.org/x/crypto/bcrypt"
)

type User = dao.User
type Group = dao.Group
type Membership = dao.Membership
type Grant = dao.Grant
type Database = dao.Database
type AdminData = dao.AdminData

type Service struct {
	store     *dao.Store
	config    Config
	dummyHash []byte
}

func NewService(store *dao.Store, config Config) (*Service, error) {
	if store == nil {
		return nil, errors.New("website database DAO is required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	config.Key = append([]byte(nil), config.Key...)
	dummy, err := bcrypt.GenerateFromPassword([]byte("dummy-password-not-an-account"), 12)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, config: config, dummyHash: dummy}, nil
}

func (s *Service) RequireAdmin(ctx context.Context, actor int64) error {
	if actor < 1 {
		return ErrForbidden
	}
	allowed, err := s.store.IsActiveAdmin(ctx, actor)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (s *Service) adminChange(ctx context.Context, actor int64, change func(*dao.Store) error) error {
	return s.store.Transaction(ctx, func(tx *dao.Store) error {
		if err := tx.LockAdministration(ctx); err != nil {
			return err
		}
		allowed, err := tx.IsActiveAdmin(ctx, actor)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrForbidden
		}
		return change(tx)
	})
}

func (s *Service) Administration(ctx context.Context, actor int64) (AdminData, error) {
	if err := s.RequireAdmin(ctx, actor); err != nil {
		return AdminData{}, err
	}
	return s.store.Administration(ctx)
}
