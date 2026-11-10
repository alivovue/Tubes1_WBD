package access

import (
	"crypto/hmac"
	"strconv"
	"strings"
	"time"
)

type LoginChallenge struct {
	CookieValue string
	Token       string
	Expires     time.Time
}

func (s *Service) NewLoginChallenge() (LoginChallenge, error) {
	nonce, err := randomToken()
	if err != nil {
		return LoginChallenge{}, err
	}
	expires := s.config.Now().Add(10 * time.Minute)
	value := nonce + "." + strconv.FormatInt(expires.Unix(), 10)
	value += "." + sign(s.config.Key, "login-cookie", value)
	return LoginChallenge{CookieValue: value, Token: sign(s.config.Key, "login-form", value), Expires: expires}, nil
}

func (s *Service) ValidateLoginCSRF(cookieValue, provided string) error {
	parts := strings.Split(cookieValue, ".")
	if len(parts) != 3 {
		return ErrForbidden
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || !s.config.Now().Before(time.Unix(expires, 0)) || !hmac.Equal([]byte(parts[2]), []byte(sign(s.config.Key, "login-cookie", parts[0]+"."+parts[1]))) || !hmac.Equal([]byte(provided), []byte(sign(s.config.Key, "login-form", cookieValue))) {
		return ErrForbidden
	}
	return nil
}

func (s *Service) CSRFToken(identity Identity) (string, error) {
	if identity.User.ID < 1 || identity.SessionID == "" || !s.config.Now().Before(identity.AbsoluteExpires) {
		return "", ErrUnauthenticated
	}
	return sign(s.config.Key, "session-csrf", identity.SessionID), nil
}

func (s *Service) ValidateCSRF(identity Identity, provided string) error {
	expected, err := s.CSRFToken(identity)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(provided), []byte(expected)) {
		return ErrForbidden
	}
	return nil
}
