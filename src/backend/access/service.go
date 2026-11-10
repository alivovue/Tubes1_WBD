package access

import (
	"context"
	"errors"
	"sync"

	"tubes1_wbd/database/dao"

	"golang.org/x/crypto/bcrypt"
)

type User = dao.User
type Group = dao.Group
type Membership = dao.Membership
type Grant = dao.Grant
type Database = dao.Database
type AdminData = dao.AdminData

// A missing-account comparison needs the same bcrypt cost in both domains.
// This immutable dummy hash is shared; real account hashes always use new salts.
var dummyPasswordHash = sync.OnceValues(func() ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte("dummy-password-not-an-account"), 12)
})

type Service struct {
	store     *dao.Store
	config    Config
	dummyHash []byte
	authority AdminAuthorizer
}

func NewService(store *dao.Store, config Config) (*Service, error) {
	if store == nil {
		return nil, errors.New("website database DAO is required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	config.Key = append([]byte(nil), config.Key...)
	dummy, err := dummyPasswordHash()
	if err != nil {
		return nil, err
	}
	return &Service{store: store, config: config, dummyHash: dummy}, nil
}

// AdminAuthorizer validates actors in the independent administrator domain.
type AdminAuthorizer interface {
	RequireAdmin(context.Context, int64) error
}

// NewAdministrationService manages platform metadata but never authenticates an
// administrator against platform users, even when numeric IDs happen to match.
func NewAdministrationService(store *dao.Store, authority AdminAuthorizer, config Config) (*Service, error) {
	if authority == nil || store == nil || store.AdminAuthentication() {
		return nil, ErrInvalid
	}
	s, err := NewService(store, config)
	if err == nil {
		s.authority = authority
	}
	return s, err
}
func (s *Service) RequireAdmin(ctx context.Context, actor int64) error {
	if s.authority != nil {
		return s.authority.RequireAdmin(ctx, actor)
	}
	if !s.store.AdminAuthentication() || actor < 1 {
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
