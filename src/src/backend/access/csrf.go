package access

import (
	"schakker_shared/auth"
)

type LoginChallenge = auth.LoginChallenge

func (s *Service) NewLoginChallenge() (LoginChallenge, error) {
	return auth.NewLoginChallenge(s.config.Key, s.config.Now())
}
func (s *Service) ValidateLoginCSRF(cookie, provided string) error {
	if !auth.ValidLoginCSRF(s.config.Key, s.config.Now(), cookie, provided) {
		return ErrForbidden
	}
	return nil
}
func (s *Service) CSRFToken(identity Identity) (string, error) {
	if identity.User.ID < 1 || identity.SessionID == "" || !s.config.Now().Before(identity.AbsoluteExpires) {
		return "", ErrUnauthenticated
	}
	return auth.SessionCSRF(s.config.Key, identity.SessionID), nil
}
func (s *Service) ValidateCSRF(identity Identity, provided string) error {
	expected, err := s.CSRFToken(identity)
	if err != nil {
		return err
	}
	if !auth.Equal(provided, expected) {
		return ErrForbidden
	}
	return nil
}
