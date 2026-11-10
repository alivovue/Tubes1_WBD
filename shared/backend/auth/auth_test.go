package auth_test

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"schakker_shared/auth"
)

func TestTokenAndCSRFDomains(t *testing.T) {
	userKey, adminKey := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	now := time.Unix(1800000000, 0)
	challenge, err := auth.NewLoginChallenge(userKey, now)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.ValidLoginCSRF(userKey, now, challenge.CookieValue, challenge.Token) {
		t.Fatal("valid login challenge rejected")
	}
	for _, test := range []struct {
		key           []byte
		now           time.Time
		cookie, token string
	}{
		{adminKey, now, challenge.CookieValue, challenge.Token},
		{userKey, challenge.Expires, challenge.CookieValue, challenge.Token},
		{userKey, now, challenge.CookieValue + "x", challenge.Token},
		{userKey, now, challenge.CookieValue, auth.SessionCSRF(userKey, challenge.CookieValue)},
	} {
		if auth.ValidLoginCSRF(test.key, test.now, test.cookie, test.token) {
			t.Fatal("accepted mismatched key, expiry, cookie or token purpose")
		}
	}
	first, err := auth.RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := auth.RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil || len(raw) != 32 || first == second || len(auth.TokenHash(first)) != 32 {
		t.Fatal("invalid random session token")
	}
	hash := auth.TokenHash(first)
	if auth.RotationToken(userKey, hash) == auth.RotationToken(adminKey, hash) {
		t.Fatal("rotation token ignored caller key")
	}
}

func TestCookiePolicyIsSuppliedByCaller(t *testing.T) {
	for _, test := range []struct{ production, tls, secure bool }{
		{false, false, false}, {true, false, true}, {false, true, true},
	} {
		r := httptest.NewRequest("GET", "http://localhost", nil)
		if test.tls {
			r.TLS = &tls.ConnectionState{}
		}
		w := httptest.NewRecorder()
		expires := time.Unix(1800000000, 0)
		auth.SetCookie(w, r, test.production, "caller_session", "opaque", expires)
		auth.ClearCookie(w, r, test.production, "caller_session")
		cookies := w.Result().Cookies()
		for _, cookie := range cookies {
			if cookie.Name != "caller_session" || cookie.Path != "/" || !cookie.HttpOnly || cookie.Secure != test.secure || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("cookie policy changed: %+v", cookie)
			}
		}
		if len(cookies) != 2 || cookies[0].Value != "opaque" || !cookies[0].Expires.Equal(expires) || cookies[1].MaxAge != -1 {
			t.Fatalf("invalid session or deletion cookie: %+v", cookies)
		}
	}
}
