package access

import (
	"context"
	"errors"
	"schakker_shared/authority"
	"tubes1_wbd/database/dao"
)

type User = dao.User
type Database = dao.Database

// Authority is implemented by the private HTTP client. No administrative
// operations or password hashes cross this interface.
type Authority interface {
	AuthenticateUser(context.Context, string, string) (User, error)
	User(context.Context, int64) (User, error)
	EffectivePermission(context.Context, int64, int64) (Permission, error)
	AccessibleDatabases(context.Context, int64) ([]Database, error)
	Connection(context.Context, int64, int64, Permission) (dao.DatabaseMetadata, error)
}

func (s *Service) MigrationAuthority(ctx context.Context, actor, database int64) (authority.Snapshot, error) {
	provider, ok := s.authority.(authority.Reviewer)
	if !ok {
		return authority.Snapshot{}, errors.New("migration review authority is unavailable")
	}
	return provider.MigrationAuthority(ctx, actor, database)
}

type Service struct {
	store     *dao.Store
	config    Config
	authority Authority
}

func NewService(store *dao.Store, config Config) (*Service, error) {
	if store == nil || config.Authority == nil {
		return nil, errors.New("runtime store and control-plane authority are required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	config.Key = append([]byte(nil), config.Key...)
	return &Service{store: store, config: config, authority: config.Authority}, nil
}
func (s *Service) DatabaseConnection(ctx context.Context, user, database int64, required Permission) (dao.DatabaseMetadata, error) {
	if user < 1 || database < 1 || (required != ViewOnly && required != ViewEdit) {
		return dao.DatabaseMetadata{}, ErrInvalid
	}
	m, err := s.authority.Connection(ctx, user, database, required)
	if err != nil {
		return m, err
	}
	if !m.Active || m.ID != database {
		return dao.DatabaseMetadata{}, ErrForbidden
	}
	if err := s.store.RememberDatabase(ctx, database); err != nil {
		return dao.DatabaseMetadata{}, err
	}
	return m, nil
}
