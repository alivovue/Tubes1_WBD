package auth

import (
	"crypto/hmac"
	"strconv"
	"strings"
	"time"
)

type LoginChallenge struct {
	CookieValue, Token string
	Expires            time.Time
}

func NewLoginChallenge(key []byte, now time.Time) (LoginChallenge, error) {
	nonce, err := RandomToken()
	if err != nil {
		return LoginChallenge{}, err
	}
	expires := now.Add(10 * time.Minute)
	value := nonce + "." + strconv.FormatInt(expires.Unix(), 10)
	value += "." + Sign(key, "login-cookie", value)
	return LoginChallenge{CookieValue: value, Token: Sign(key, "login-form", value), Expires: expires}, nil
}
func ValidLoginCSRF(key []byte, now time.Time, cookie, provided string) bool {
	parts := strings.Split(cookie, ".")
	if len(parts) != 3 {
		return false
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && now.Before(time.Unix(expires, 0)) && hmac.Equal([]byte(parts[2]), []byte(Sign(key, "login-cookie", parts[0]+"."+parts[1]))) && hmac.Equal([]byte(provided), []byte(Sign(key, "login-form", cookie)))
}
func SessionCSRF(key []byte, session string) string { return Sign(key, "session-csrf", session) }
func Equal(first, second string) bool               { return hmac.Equal([]byte(first), []byte(second)) }
