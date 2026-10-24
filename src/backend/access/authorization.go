package access

import "context"

func (s *Service) EffectivePermission(ctx context.Context, user, database int64) (Permission, error) {
	if user < 1 || database < 1 {
		return NoAccess, ErrInvalid
	}
	return s.store.EffectivePermission(ctx, user, database)
}

func (s *Service) RequirePermission(ctx context.Context, user, database int64, required Permission) error {
	permission, err := s.EffectivePermission(ctx, user, database)
	if err != nil {
		return err
	}
	if !permission.Allows(required) {
		return ErrForbidden
	}
	return nil
}

func (s *Service) AccessibleDatabases(ctx context.Context, user int64) ([]Database, error) {
	if user < 1 {
		return nil, ErrUnauthenticated
	}
	return s.store.AccessibleDatabases(ctx, user)
}
