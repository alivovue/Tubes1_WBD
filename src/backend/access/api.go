package access

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type api struct {
	service *Service
	limiter loginLimiter
}

// Handler exposes only the access-control API. It serves no pages or assets and
// is independent of the website handler in package backend.
func (s *Service) Handler() http.Handler {
	a := &api{service: s, limiter: loginLimiter{entries: make(map[string]attempt)}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/login/csrf", a.loginCSRF)
	mux.HandleFunc("POST /api/login", a.login)
	mux.Handle("POST /api/logout", s.RequireSession(http.HandlerFunc(a.logout)))
	mux.Handle("GET /api/me", s.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, _ := CurrentIdentity(r.Context())
		writeJSON(w, http.StatusOK, i.User)
	})))
	mux.Handle("GET /api/csrf", s.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, _ := CurrentIdentity(r.Context())
		writeJSON(w, http.StatusOK, map[string]string{"csrf_token": s.csrf(i)})
	})))
	mux.Handle("GET /api/databases", s.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, _ := CurrentIdentity(r.Context())
		ds, err := s.AccessibleDatabases(r.Context(), i.User.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ds)
	})))
	mux.Handle("GET /api/admin", s.RequireAdmin(http.HandlerFunc(a.administration)))
	for _, route := range []string{"/api/admin/users/create", "/api/admin/users/update", "/api/admin/groups/save", "/api/admin/groups/delete", "/api/admin/memberships", "/api/admin/grants", "/api/admin/databases/save", "/api/admin/databases/disable"} {
		mux.Handle("POST "+route, s.RequireAdmin(http.HandlerFunc(a.adminAction)))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if s.config.SecureCookies {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status, message := http.StatusInternalServerError, "terjadi kesalahan server"
	switch {
	case errors.Is(err, ErrUnauthenticated):
		status, message = 401, ErrUnauthenticated.Error()
	case errors.Is(err, ErrForbidden):
		status, message = 403, ErrForbidden.Error()
	case errors.Is(err, ErrNotFound):
		status, message = 404, ErrNotFound.Error()
	case errors.Is(err, ErrConflict):
		status, message = 409, ErrConflict.Error()
	case errors.Is(err, ErrInvalid):
		status, message = 400, err.Error()
	}
	writeJSON(w, status, map[string]string{"error": message})
}

// decodeJSON rejects unknown fields, trailing values, and non-JSON payloads.
// Decoder errors are never returned to clients because they may contain input.
func decodeJSON(r *http.Request, value any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return ErrInvalid
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrInvalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func (s *Service) setSessionCookie(w http.ResponseWriter, token string, expiry time.Time) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", Expires: expiry, HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteLaxMode})
}

func (s *Service) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteLaxMode})
}

func (s *Service) csrf(i Identity) string { return sign(s.config.Key, "session-csrf", i.SessionID) }

// RequireSession authenticates every request and requires X-CSRF-Token on every
// unsafe method. Authentication failures return 401 without frontend redirects.
func (s *Service) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		cookie, err := r.Cookie(CookieName)
		if err != nil {
			writeError(w, ErrUnauthenticated)
			return
		}
		i, replacement, err := s.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, ErrUnauthenticated) {
				s.clearCookie(w, CookieName)
			}
			writeError(w, err)
			return
		}
		if replacement != "" {
			s.setSessionCookie(w, replacement, i.AbsoluteExpires)
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.csrf(i))) {
				writeError(w, ErrForbidden)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, i)))
	})
}

func (s *Service) RequireAdmin(next http.Handler) http.Handler {
	return s.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, _ := CurrentIdentity(r.Context())
		if !i.User.IsAdmin {
			writeError(w, ErrForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// RequireDatabase uses the {databaseID} path parameter. Admins receive no bypass.
func (s *Service) RequireDatabase(required Permission, next http.Handler) http.Handler {
	return s.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, _ := CurrentIdentity(r.Context())
		id, err := positiveID(r.PathValue("databaseID"))
		if err != nil {
			writeError(w, err)
			return
		}
		if err = s.RequirePermission(r.Context(), i.User.ID, id, required); err != nil {
			writeError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

const loginCookieName = "schakker_login_csrf"

func (a *api) loginCSRF(w http.ResponseWriter, r *http.Request) {
	nonce, err := randomToken()
	if err != nil {
		writeError(w, err)
		return
	}
	value := nonce + "." + strconv.FormatInt(a.service.config.Now().Add(10*time.Minute).Unix(), 10)
	value += "." + sign(a.service.config.Key, "login-cookie", value)
	http.SetCookie(w, &http.Cookie{Name: loginCookieName, Value: value, Path: "/", HttpOnly: true, Secure: a.service.config.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	writeJSON(w, http.StatusOK, map[string]string{"csrf_token": sign(a.service.config.Key, "login-form", value)})
}

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(loginCookieName)
	if err != nil {
		writeError(w, ErrForbidden)
		return
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 {
		writeError(w, ErrForbidden)
		return
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || !a.service.config.Now().Before(time.Unix(expires, 0)) || !hmac.Equal([]byte(parts[2]), []byte(sign(a.service.config.Key, "login-cookie", parts[0]+"."+parts[1]))) || !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(sign(a.service.config.Key, "login-form", cookie.Value))) {
		writeError(w, ErrForbidden)
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if !a.limiter.allow(ip, a.service.config.Now()) {
		w.Header().Set("Retry-After", "300")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "terlalu banyak percobaan; coba lagi dalam 5 menit"})
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, err)
		return
	}
	if len(input.Username) > 100 || len(input.Password) > 72 {
		writeError(w, ErrUnauthenticated)
		return
	}
	old := ""
	if c, err := r.Cookie(CookieName); err == nil {
		old = c.Value
	}
	i, raw, err := a.service.Login(r.Context(), input.Username, input.Password, old)
	if err != nil {
		writeError(w, err)
		return
	}
	a.service.setSessionCookie(w, raw, i.AbsoluteExpires)
	a.service.clearCookie(w, loginCookieName)
	writeJSON(w, http.StatusOK, i.User)
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	i, _ := CurrentIdentity(r.Context())
	if err := a.service.Logout(r.Context(), i.SessionID); err != nil {
		writeError(w, err)
		return
	}
	a.service.clearCookie(w, CookieName)
	w.WriteHeader(http.StatusNoContent)
}

func positiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		return 0, ErrInvalid
	}
	return id, nil
}

type attempt struct {
	start time.Time
	count int
}

type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]attempt
}

func (l *loginLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, a := range l.entries {
		if !now.Before(a.start.Add(5 * time.Minute)) {
			delete(l.entries, key)
		}
	}
	a, ok := l.entries[ip]
	if !ok {
		if len(l.entries) >= 4096 {
			return false
		}
		a = attempt{start: now}
	}
	a.count++
	l.entries[ip] = a
	return a.count <= 15
}
