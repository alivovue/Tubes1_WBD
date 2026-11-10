package access

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"

	"schakker_shared/auth"

	"tubes1_wbd_admin/database/dao"
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
		return nil, errors.New("admin metadata DAO is required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, config.Key)
	mac.Write([]byte("schakker/admin-auth/v1"))
	config.Key = mac.Sum(nil)
	dummy, err := auth.DummyPasswordHash()
	if err != nil {
		return nil, err
	}
	return &Service{store: store, config: config, dummyHash: []byte(dummy)}, nil
}

func (s *Service) RequireAdmin(ctx context.Context, actor int64) error {
	if actor < 1 {
		return ErrForbidden
	}
	ok, err := s.store.IsActiveAdmin(ctx, actor)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func (s *Service) adminChange(ctx context.Context, actor int64, change func(*dao.Store) error) error {
	return s.store.Transaction(ctx, func(tx *dao.Store) error {
		if err := tx.LockAdministration(ctx); err != nil {
			return err
		}
		if err := s.RequireAdmin(ctx, actor); err != nil {
			return err
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

// DummyPasswordHash is used only by the private ordinary-user verifier. It
// shares bcrypt cost with real passwords without exposing any account hash.
func DummyPasswordHash() string { hash, _ := auth.DummyPasswordHash(); return hash }
