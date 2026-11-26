package authhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"schakker_shared/auth"
	"schakker_shared/authhttp"
)

type identity struct{}

func (i identity) SessionInfo() auth.SessionInfo { return i.info() }
func (i identity) info() auth.SessionInfo {
	return auth.SessionInfo{UserID: 7, SessionID: "session-id", AbsoluteExpires: time.Now().Add(time.Hour)}
}

type service struct {
	challengeErr, loginErr, authenticateErr, csrfErr, actionErr, logoutErr error
	loginCalls, logoutCalls                                                int
	username, password, previous, logoutID                                 string
	replacement                                                            string
}

func (s *service) NewLoginChallenge() (auth.LoginChallenge, error) {
	return auth.LoginChallenge{CookieValue: "challenge-cookie", Token: "challenge-token", Expires: time.Now().Add(time.Minute)}, s.challengeErr
}
func (s *service) ValidateLoginCSRF(cookie, token string) error {
	if cookie != "challenge-cookie" || token != "challenge-token" {
		return auth.ErrForbidden
	}
	return nil
}
func (s *service) Login(_ context.Context, username, password, previous string) (identity, string, error) {
	s.loginCalls++
	s.username, s.password, s.previous = username, password, previous
	return identity{}, "new-session", s.loginErr
}
func (s *service) Authenticate(_ context.Context, _ string) (identity, string, error) {
	return identity{}, s.replacement, s.authenticateErr
}
func (s *service) CSRFToken(identity) (string, error) { return "session-token", s.csrfErr }
func (s *service) ValidateCSRF(_ identity, token string) error {
	if token != "session-token" {
		return auth.ErrForbidden
	}
	return s.actionErr
}
func (s *service) Logout(_ context.Context, id string) error {
	s.logoutCalls++
	s.logoutID = id
	return s.logoutErr
}
func handler(s *service) authhttp.Handler[identity] {
	return authhttp.Handler[identity]{Access: s, Production: true, SessionCookie: "user-session", LoginCookie: "user-login", Home: "/dashboard", LoginEvent: "test.login", ForbiddenMessage: "This account cannot sign in here."}
}
func request(body string) *http.Request {
	r := httptest.NewRequest("POST", "https://app.example/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://app.example")
	r.Header.Set("X-CSRF-Token", "challenge-token")
	r.AddCookie(&http.Cookie{Name: "user-login", Value: "challenge-cookie"})
	r.AddCookie(&http.Cookie{Name: "user-session", Value: "old-session"})
	return r
}
func result(t *testing.T, w *httptest.ResponseRecorder, status int) map[string]string {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}
func cookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
func TestLoginChallengeAvailabilityAndCookieSecurity(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "unavailable"}[fail], func(t *testing.T) {
			s := &service{}
			if fail {
				s.challengeErr = errors.New("database secret")
			}
			w := httptest.NewRecorder()
			handler(s).Challenge(w, request("{}"), "Try again", 401)
			if fail {
				body := result(t, w, 503)
				if strings.Contains(body["error"], "secret") || cookie(w, "user-login") != nil {
					t.Fatal("failed challenge leaked state")
				}
				return
			}
			body := result(t, w, 401)
			c := cookie(w, "user-login")
			if body["CSRF"] != "challenge-token" || c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
				t.Fatal("insecure challenge")
			}
		})
	}
}
func TestLoginValidatesOriginChallengeAndStrictActionBeforeCredentials(t *testing.T) {
	for _, scenario := range []string{"cross-origin", "missing-csrf", "wrong-cookie", "invalid-json", "oversize"} {
		t.Run(scenario, func(t *testing.T) {
			s := &service{}
			r := request(`{"username":"editor","password":"password"}`)
			switch scenario {
			case "cross-origin":
				r.Header.Set("Origin", "https://evil.example")
			case "missing-csrf":
				r.Header.Del("X-CSRF-Token")
			case "wrong-cookie":
				r.Header.Del("Cookie")
			case "invalid-json":
				r = request(`{} {}`)
			case "oversize":
				r = request(`{"password":"` + strings.Repeat("x", 17000) + `"}`)
			}
			w := httptest.NewRecorder()
			handler(s).Login(w, r)
			body := result(t, w, 403)
			if s.loginCalls != 0 || body["CSRF"] == "" {
				t.Fatal("credentials reached before request validation")
			}
		})
	}
}
func TestLoginFailuresIssueFreshChallengeWithoutSessionCookie(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		status  int
		message string
	}{{"credentials", auth.ErrUnauthenticated, 401, "Incorrect username"}, {"role", auth.ErrForbidden, 403, "cannot sign in here"}, {"outage", errors.New("secret driver detail"), 503, "temporarily unavailable"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &service{loginErr: tc.err}
			w := httptest.NewRecorder()
			handler(s).Login(w, request(`{"username":"editor","password":"password"}`))
			body := result(t, w, tc.status)
			if !strings.Contains(body["error"], tc.message) || body["CSRF"] == "" || cookie(w, "user-session") != nil {
				t.Fatal("bad failed-login response")
			}
		})
	}
}
func TestSuccessfulLoginReplacesSessionAndClearsChallenge(t *testing.T) {
	s := &service{}
	w := httptest.NewRecorder()
	handler(s).Login(w, request(`{"username":"editor","password":"password"}`))
	body := result(t, w, 200)
	c := cookie(w, "user-session")
	if s.username != "editor" || s.password != "password" || s.previous != "old-session" || body["location"] != "/dashboard" || c == nil || c.Value != "new-session" || !c.Secure || !c.HttpOnly || cookie(w, "user-login").MaxAge != -1 {
		t.Fatal("login did not rotate secure cookies")
	}
}
func TestAuthenticationFailuresDistinguishBootstrapExpiryAndOutage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		bootstrap bool
		status    int
	}{{"bootstrap", auth.ErrUnauthenticated, true, 200}, {"expired", auth.ErrUnauthenticated, false, 401}, {"outage", errors.New("private failure"), false, 503}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &service{authenticateErr: tc.err}
			w := httptest.NewRecorder()
			_, token, ok := handler(s).Authenticate(w, request("{}"), tc.bootstrap)
			body := result(t, w, tc.status)
			if ok || token != "" {
				t.Fatal("failed authentication accepted")
			}
			if errors.Is(tc.err, auth.ErrUnauthenticated) {
				if cookie(w, "user-session") == nil || cookie(w, "user-session").MaxAge != -1 {
					t.Fatal("expired cookie not cleared")
				}
			} else if cookie(w, "user-session") != nil {
				t.Fatal("outage cleared a valid session")
			}
			if tc.bootstrap && body["CSRF"] == "" {
				t.Fatal("bootstrap omitted challenge")
			}
		})
	}
}
func TestAuthenticationRefreshesCookieAndRejectsUnavailableCSRF(t *testing.T) {
	s := &service{replacement: "rotated-session"}
	w := httptest.NewRecorder()
	_, token, ok := handler(s).Authenticate(w, request("{}"), false)
	if !ok || token != "session-token" || cookie(w, "user-session").Value != "rotated-session" {
		t.Fatal("rotation missing")
	}
	s.csrfErr = auth.ErrUnauthenticated
	w = httptest.NewRecorder()
	_, _, ok = handler(s).Authenticate(w, request("{}"), false)
	result(t, w, 401)
	if ok {
		t.Fatal("invalid session csrf accepted")
	}
}
func TestActionValidationRequiresSameOriginAndSessionCSRF(t *testing.T) {
	for _, jsonOnly := range []bool{false, true} {
		for _, scenario := range []string{"valid", "origin", "csrf", "malformed"} {
			t.Run(map[bool]string{false: "form/", true: "json/"}[jsonOnly]+scenario, func(t *testing.T) {
				s := &service{}
				r := request("{}")
				r.Header.Set("X-CSRF-Token", "session-token")
				switch scenario {
				case "origin":
					r.Header.Set("Origin", "https://other.example")
				case "csrf":
					r.Header.Del("X-CSRF-Token")
				case "malformed":
					r = request("[]")
					r.Header.Set("X-CSRF-Token", "session-token")
				}
				var err error
				if jsonOnly {
					err = handler(s).ValidateJSONAction(r, identity{})
				} else {
					err = handler(s).ValidateAction(httptest.NewRecorder(), r, identity{})
				}
				valid := scenario == "valid" || (jsonOnly && scenario == "malformed")
				if (err == nil) != valid {
					t.Fatalf("validation=%v want valid=%v", err, valid)
				}
			})
		}
	}
}
func TestLogoutClearsCookieOnlyAfterPersistenceSucceeds(t *testing.T) {
	for _, fail := range []bool{false, true} {
		s := &service{}
		if fail {
			s.logoutErr = errors.New("storage unavailable")
		}
		w := httptest.NewRecorder()
		handler(s).Logout(w, request("{}"), identity{})
		if fail {
			result(t, w, 503)
			if cookie(w, "user-session") != nil {
				t.Fatal("failed logout cleared cookie")
			}
		} else {
			body := result(t, w, 200)
			if body["location"] != "/login" || cookie(w, "user-session").MaxAge != -1 {
				t.Fatal("logout missing clear/navigation")
			}
		}
		if s.logoutCalls != 1 || s.logoutID != "session-id" {
			t.Fatal("logout did not revoke authenticated session")
		}
	}
}
