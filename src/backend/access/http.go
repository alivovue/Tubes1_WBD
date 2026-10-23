package access

import (
	"bytes"
	"context"
	"crypto/hmac"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed portal.gohtml portal.css
var assets embed.FS

type portal struct {
	service   *Service
	templates *template.Template
	limiter   loginLimiter
}
type viewData struct {
	Page, Title, CSRF, Error string
	User                     User
	Databases                []Database
	Admin                    AdminData
}

func (s *Service) Handler() (http.Handler, error) {
	t, err := template.ParseFS(assets, "portal.gohtml")
	if err != nil {
		return nil, err
	}
	p := &portal{service: s, templates: t, limiter: loginLimiter{entries: make(map[string]attempt)}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /portal.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		body, _ := assets.ReadFile("portal.css")
		w.Write(body)
	})
	mux.HandleFunc("GET /login", p.loginPage)
	mux.HandleFunc("POST /login", p.login)
	mux.Handle("GET /{$}", s.RequireSession(http.HandlerFunc(p.dashboard)))
	mux.Handle("GET /databases", s.RequireSession(http.HandlerFunc(p.databases)))
	mux.Handle("POST /logout", s.RequireSession(http.HandlerFunc(p.logout)))
	mux.Handle("GET /admin/{section}", s.RequireAdmin(http.HandlerFunc(p.adminPage)))
	for _, route := range []string{"/admin/users/create", "/admin/users/update", "/admin/groups/save", "/admin/groups/delete", "/admin/memberships", "/admin/grants", "/admin/databases/save", "/admin/databases/disable"} {
		mux.Handle("POST "+route, s.RequireAdmin(http.HandlerFunc(p.adminAction)))
	}
	mux.Handle("GET /api/me", s.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, _ := CurrentIdentity(r.Context())
		writeJSON(w, i.User)
	})))
	mux.Handle("GET /api/csrf", s.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, _ := CurrentIdentity(r.Context())
		writeJSON(w, map[string]string{"csrf_token": s.csrf(i)})
	})))
	mux.Handle("GET /api/databases", s.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, _ := CurrentIdentity(r.Context())
		ds, err := s.AccessibleDatabases(r.Context(), i.User.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, ds)
	})))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'none'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if s.config.SecureCookies {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		mux.ServeHTTP(w, r)
	}), nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
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
	http.Error(w, message, status)
}

func (s *Service) setSessionCookie(w http.ResponseWriter, token string, expiry time.Time) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", Expires: expiry, HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteLaxMode})
}
func (s *Service) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteLaxMode})
}
func (s *Service) csrf(i Identity) string { return sign(s.config.Key, "session-csrf", i.SessionID) }

// RequireSession authenticates every request and verifies CSRF on every unsafe method.
// Register new explorer/history/migration routes through these middleware functions.
func (s *Service) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		cookie, err := r.Cookie(CookieName)
		if err != nil {
			s.unauthorized(w, r)
			return
		}
		i, replacement, err := s.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, ErrUnauthenticated) {
				s.clearCookie(w, CookieName)
				s.unauthorized(w, r)
			} else {
				writeError(w, err)
			}
			return
		}
		if replacement != "" {
			s.setSessionCookie(w, replacement, i.AbsoluteExpires)
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			provided := r.Header.Get("X-CSRF-Token")
			if provided == "" {
				r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
				if err := r.ParseForm(); err != nil {
					writeError(w, ErrInvalid)
					return
				}
				provided = r.PostForm.Get("csrf_token")
			}
			if !hmac.Equal([]byte(provided), []byte(s.csrf(i))) {
				writeError(w, ErrForbidden)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, i)))
	})
}

func (s *Service) unauthorized(w http.ResponseWriter, r *http.Request) {
	if (r.Method == "GET" || r.Method == "HEAD") && !strings.HasPrefix(r.URL.Path, "/api/") {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	writeError(w, ErrUnauthenticated)
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

func (p *portal) render(w http.ResponseWriter, status int, data viewData) {
	var buf bytes.Buffer
	if err := p.templates.ExecuteTemplate(&buf, "portal", data); err != nil {
		writeError(w, errors.New("render failed"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

const loginCookieName = "schakker_login_csrf"

func (p *portal) loginPage(w http.ResponseWriter, r *http.Request) {
	// Login never reflects the opaque authentication cookie into the page.
	nonce, err := randomToken()
	if err != nil {
		writeError(w, err)
		return
	}
	value := nonce + "." + strconv.FormatInt(p.service.config.Now().Add(10*time.Minute).Unix(), 10)
	value += "." + sign(p.service.config.Key, "login-cookie", value)
	http.SetCookie(w, &http.Cookie{Name: loginCookieName, Value: value, Path: "/", HttpOnly: true, Secure: p.service.config.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	p.render(w, 200, viewData{Page: "login", Title: "Masuk", CSRF: sign(p.service.config.Key, "login-form", value)})
}

func (p *portal) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, ErrInvalid)
		return
	}
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
	if err != nil || !p.service.config.Now().Before(time.Unix(expires, 0)) || !hmac.Equal([]byte(parts[2]), []byte(sign(p.service.config.Key, "login-cookie", parts[0]+"."+parts[1]))) || !hmac.Equal([]byte(r.PostForm.Get("csrf_token")), []byte(sign(p.service.config.Key, "login-form", cookie.Value))) {
		writeError(w, ErrForbidden)
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if !p.limiter.allow(ip, p.service.config.Now()) {
		w.Header().Set("Retry-After", "300")
		http.Error(w, "terlalu banyak percobaan; coba lagi dalam 5 menit", 429)
		return
	}
	username, password := r.PostForm.Get("username"), r.PostForm.Get("password")
	if len(username) > 100 || len(password) > 72 {
		p.render(w, 401, viewData{Page: "login", Title: "Masuk", CSRF: r.PostForm.Get("csrf_token"), Error: "Username atau password salah, atau akun tidak aktif."})
		return
	}
	old := ""
	if c, e := r.Cookie(CookieName); e == nil {
		old = c.Value
	}
	i, raw, err := p.service.Login(r.Context(), username, password, old)
	if errors.Is(err, ErrUnauthenticated) {
		p.render(w, 401, viewData{Page: "login", Title: "Masuk", CSRF: r.PostForm.Get("csrf_token"), Error: "Username atau password salah, atau akun tidak aktif."})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	p.service.setSessionCookie(w, raw, i.AbsoluteExpires)
	p.service.clearCookie(w, loginCookieName)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (p *portal) logout(w http.ResponseWriter, r *http.Request) {
	i, _ := CurrentIdentity(r.Context())
	if err := p.service.Logout(r.Context(), i.SessionID); err != nil {
		writeError(w, err)
		return
	}
	p.service.clearCookie(w, CookieName)
	http.Redirect(w, r, "/login", 303)
}
func (p *portal) dashboard(w http.ResponseWriter, r *http.Request) {
	i, _ := CurrentIdentity(r.Context())
	ds, err := p.service.AccessibleDatabases(r.Context(), i.User.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	p.render(w, 200, viewData{Page: "dashboard", Title: "Dashboard", User: i.User, CSRF: p.service.csrf(i), Databases: ds})
}
func (p *portal) databases(w http.ResponseWriter, r *http.Request) {
	i, _ := CurrentIdentity(r.Context())
	ds, err := p.service.AccessibleDatabases(r.Context(), i.User.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	p.render(w, 200, viewData{Page: "databases", Title: "Database Saya", User: i.User, CSRF: p.service.csrf(i), Databases: ds})
}
func (p *portal) adminPage(w http.ResponseWriter, r *http.Request) {
	section := r.PathValue("section")
	titles := map[string]string{"users": "Karyawan", "groups": "Grup & Keanggotaan", "databases": "Koneksi Database", "access": "Akses Grup ke Database"}
	title, ok := titles[section]
	if !ok {
		http.NotFound(w, r)
		return
	}
	i, _ := CurrentIdentity(r.Context())
	data, err := p.service.Administration(r.Context(), i.User.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	page := section
	if section == "databases" {
		page = "database-admin"
	}
	p.render(w, 200, viewData{Page: page, Title: title, User: i.User, CSRF: p.service.csrf(i), Admin: data})
}

func positiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		return 0, ErrInvalid
	}
	return id, nil
}
func optionalID(value string) (int64, error) {
	if value == "" || value == "0" {
		return 0, nil
	}
	return positiveID(value)
}

func (p *portal) adminAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, ErrInvalid)
		return
	}
	i, _ := CurrentIdentity(r.Context())
	f := r.PostForm
	var err error
	redirect := "/admin/users"
	// IDs are parsed explicitly; malformed updates must never become inserts.
	switch r.URL.Path {
	case "/admin/users/create":
		err = p.service.CreateUser(r.Context(), i.User.ID, f.Get("username"), f.Get("display_name"), f.Get("password"), f.Get("admin") == "on")
	case "/admin/users/update":
		id, e := positiveID(f.Get("id"))
		err = e
		if err == nil {
			err = p.service.UpdateUser(r.Context(), i.User.ID, id, f.Get("display_name"), f.Get("admin") == "on", f.Get("active") == "on", f.Get("password"))
		}
	case "/admin/groups/save":
		redirect = "/admin/groups"
		id, e := optionalID(f.Get("id"))
		err = e
		if err == nil {
			err = p.service.SaveGroup(r.Context(), i.User.ID, id, f.Get("name"), f.Get("description"))
		}
	case "/admin/groups/delete":
		redirect = "/admin/groups"
		id, e := positiveID(f.Get("id"))
		err = e
		if err == nil {
			err = p.service.DeleteGroup(r.Context(), i.User.ID, id)
		}
	case "/admin/memberships":
		redirect = "/admin/groups"
		user, e := positiveID(f.Get("user_id"))
		group, e2 := positiveID(f.Get("group_id"))
		if e != nil || e2 != nil {
			err = ErrInvalid
		} else {
			err = p.service.SetMembership(r.Context(), i.User.ID, user, group, f.Get("member") == "on")
		}
	case "/admin/grants":
		redirect = "/admin/access"
		group, e := positiveID(f.Get("group_id"))
		db, e2 := positiveID(f.Get("database_id"))
		if e != nil || e2 != nil {
			err = ErrInvalid
		} else {
			err = p.service.SetGrant(r.Context(), i.User.ID, group, db, Permission(f.Get("permission")))
		}
	case "/admin/databases/save":
		redirect = "/admin/databases"
		id, e := optionalID(f.Get("id"))
		port, e2 := strconv.Atoi(f.Get("port"))
		if e != nil || e2 != nil {
			err = ErrInvalid
		} else {
			err = p.service.SaveConnection(r.Context(), i.User.ID, id, ConnectionInput{Name: f.Get("name"), Host: f.Get("host"), Port: port, DatabaseName: f.Get("database_name"), Username: f.Get("db_username"), Password: f.Get("password"), SSLMode: f.Get("sslmode"), Active: f.Get("active") == "on"})
			if err != nil && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrConflict) && !errors.Is(err, ErrNotFound) {
				err = errors.New("koneksi target tidak dapat disimpan")
				p.render(w, 502, viewData{Page: "error", Title: "Koneksi gagal", User: i.User, CSRF: p.service.csrf(i), Error: "Koneksi target gagal. Periksa pengaturan, kredensial, dan TLS; perubahan belum disimpan."})
				return
			}
		}
	case "/admin/databases/disable":
		redirect = "/admin/databases"
		id, e := positiveID(f.Get("id"))
		err = e
		if err == nil {
			err = p.service.DisableConnection(r.Context(), i.User.ID, id)
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	http.Redirect(w, r, redirect, 303)
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
