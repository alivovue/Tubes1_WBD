package unit_test

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tubes1_wbd/backend/access"
)

func TestAccessConfigurationValidation(t *testing.T) {
	_, store, _ := newSQLStub(t)
	cases := []struct {
		name   string
		change func(*access.Config)
	}{
		{"missing key", func(c *access.Config) { c.Key = nil }}, {"short key", func(c *access.Config) { c.Key = make([]byte, 31) }},
		{"idle timeout", func(c *access.Config) { c.IdleTimeout = 0 }}, {"absolute timeout", func(c *access.Config) { c.AbsoluteLifetime = -time.Second }},
		{"rotation interval", func(c *access.Config) { c.RotationInterval = 0 }}, {"grace period", func(c *access.Config) { c.GracePeriod = 0 }},
		{"grace reaches rotation", func(c *access.Config) { c.GracePeriod = c.RotationInterval }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := access.DefaultConfig(testKey)
			tc.change(&config)
			if _, err := access.NewService(store, config); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	if _, err := access.NewService(nil, access.DefaultConfig(testKey)); err == nil {
		t.Fatal("nil DAO accepted")
	}
}

func TestLoginCSRFChallenges(t *testing.T) {
	_, store, _ := newSQLStub(t)
	now := testTime
	service := newAccess(t, store, func() time.Time { return now })
	challenge, err := service.NewLoginChallenge()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ValidateLoginCSRF(challenge.CookieValue, challenge.Token); err != nil {
		t.Fatal(err)
	}
	if !challenge.Expires.Equal(testTime.Add(10 * time.Minute)) {
		t.Fatal("unexpected challenge lifetime")
	}
	parts := strings.Split(challenge.CookieValue, ".")
	nonce, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(nonce) != 32 {
		t.Fatal("challenge nonce is not 32 random bytes")
	}
	other, err := service.NewLoginChallenge()
	if err != nil {
		t.Fatal(err)
	}
	if challenge.CookieValue == other.CookieValue || challenge.Token == other.Token {
		t.Fatal("challenges reused a nonce")
	}
	cases := []struct{ name, cookie, token string }{
		{"missing cookie", "", challenge.Token}, {"missing token", challenge.CookieValue, ""}, {"malformed cookie", "bad.cookie", challenge.Token},
		{"modified nonce", "x" + challenge.CookieValue, challenge.Token}, {"modified signature", challenge.CookieValue + "x", challenge.Token},
		{"modified token", challenge.CookieValue, challenge.Token + "x"}, {"different challenge", challenge.CookieValue, other.Token},
		{"invalid expiry", parts[0] + ".never." + parts[2], challenge.Token},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { wantError(t, service.ValidateLoginCSRF(tc.cookie, tc.token), access.ErrForbidden) })
	}
	now = challenge.Expires
	wantError(t, service.ValidateLoginCSRF(challenge.CookieValue, challenge.Token), access.ErrForbidden)
}

func TestSessionCSRFIsolationAndExpiration(t *testing.T) {
	_, store, _ := newSQLStub(t)
	now := testTime
	service := newAccess(t, store, func() time.Time { return now })
	identity := access.Identity{User: access.User{ID: 7}, SessionID: testSession, AbsoluteExpires: now.Add(time.Hour)}
	token, err := service.CSRFToken(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ValidateCSRF(identity, token); err != nil {
		t.Fatal(err)
	}
	other := identity
	other.SessionID = "another-session"
	wantError(t, service.ValidateCSRF(other, token), access.ErrForbidden)
	wantError(t, service.ValidateCSRF(identity, ""), access.ErrForbidden)
	wantError(t, service.ValidateCSRF(identity, token+"tampered"), access.ErrForbidden)
	challenge, err := service.NewLoginChallenge()
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, service.ValidateCSRF(identity, challenge.Token), access.ErrForbidden)
	config := access.DefaultConfig(bytes.Repeat([]byte{4}, 32))
	config.Now = func() time.Time { return now }
	db, _ := testDatabases.Load(store)
	config.Authority = stubAuthority{db: db.(*sql.DB)}
	otherService, err := access.NewService(store, config)
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, otherService.ValidateCSRF(identity, token), access.ErrForbidden)
	for _, invalid := range []access.Identity{{SessionID: testSession, AbsoluteExpires: identity.AbsoluteExpires}, {User: identity.User, AbsoluteExpires: identity.AbsoluteExpires}, {User: identity.User, SessionID: testSession, AbsoluteExpires: now}} {
		_, err := service.CSRFToken(invalid)
		wantError(t, err, access.ErrUnauthenticated)
	}
	now = identity.AbsoluteExpires
	wantError(t, service.ValidateCSRF(identity, token), access.ErrUnauthenticated)
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), testSession) {
		t.Fatal("internal session ID exposed in JSON")
	}
}
