package access

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type fixture struct {
	s     *Service
	db    *sql.DB
	admin int64
	clock atomic.Int64
	dsn   string
}

func integration(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to a dedicated PostgreSQL database ending in _test")
	}
	ctx := context.Background()
	db, err := OpenDatabase(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err = db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, "_test") {
		db.Close()
		t.Fatal("integration database must end in _test; refusing to use this database")
	}
	schema := "access_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err = db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); db.Close() })
	ddl, err := os.ReadFile("../../database/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(ddl), "public.", schema+".")
	script = strings.ReplaceAll(script, "pg_catalog, public", "pg_catalog, "+schema)
	if _, err = db.Exec(script); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" && u.Scheme != "postgresql" {
		t.Fatal("TEST_DATABASE_URL must be a postgres:// URL")
	}
	q := u.Query()
	q.Set("search_path", schema+",pg_catalog")
	u.RawQuery = q.Encode()
	isolated, err := OpenDatabase(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { isolated.Close() })
	f := &fixture{db: isolated, dsn: dsn}
	f.clock.Store(time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC).UnixNano())
	cfg := DefaultConfig(bytes.Repeat([]byte{9}, 32), false)
	cfg.Now = func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }
	f.s, err = NewService(isolated, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.BootstrapAdmin(ctx, "admin", "Platform Admin", "admin-password-123"); err != nil {
		t.Fatal(err)
	}
	if err = isolated.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&f.admin); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) user(t *testing.T, name string, admin bool) int64 {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("employee-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	err = f.db.QueryRow(`INSERT INTO users(username,display_name,password_hash,is_platform_admin) VALUES($1,$1,$2,$3) RETURNING id`, name, string(hash), admin).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *fixture) group(t *testing.T, name string) int64 {
	t.Helper()
	if err := f.s.SaveGroup(context.Background(), f.admin, 0, name, ""); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.db.QueryRow(`SELECT id FROM access_groups WHERE name=$1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *fixture) database(t *testing.T, name string) int64 {
	t.Helper()
	encrypted, err := encryptSecret(f.s.config.Key, "do-not-expose")
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	err = f.db.QueryRow(`INSERT INTO managed_databases(display_name,host,port,database_name,db_username,encrypted_password) VALUES($1,'localhost',5432,'target','sensitive-db-user',$2) RETURNING id`, name, encrypted).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *fixture) login(t *testing.T, name, password string) (Identity, string) {
	t.Helper()
	i, raw, err := f.s.Login(context.Background(), name, password, "")
	if err != nil {
		t.Fatal(err)
	}
	return i, raw
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticationIntegration(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	if err := f.s.BootstrapAdmin(ctx, "second", "Second", "admin-password-123"); !errors.Is(err, ErrConflict) {
		t.Fatal("bootstrap reopened")
	}
	must(t, f.s.CreateUser(ctx, f.admin, "alice", "Alice", "employee-password", false))
	var hash string
	must(t, f.db.QueryRow(`SELECT password_hash FROM users WHERE username='alice'`).Scan(&hash))
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil || cost != 12 {
		t.Fatal("employee password was not hashed")
	}
	for _, creds := range [][2]string{{"alice", "wrong-password"}, {"missing", "employee-password"}} {
		if _, _, err := f.s.Login(ctx, creds[0], creds[1], ""); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("invalid login accepted")
		}
	}
	i, raw := f.login(t, "ALICE", "employee-password")
	var stored []byte
	must(t, f.db.QueryRow(`SELECT token_hash FROM session_tokens WHERE session_id=$1`, i.SessionID).Scan(&stored))
	if bytes.Equal(stored, []byte(raw)) || !bytes.Equal(stored, tokenHash(raw)) {
		t.Fatal("raw token persisted")
	}
	_, newToken, err := f.s.Login(ctx, "alice", "employee-password", raw)
	must(t, err)
	if raw == newToken {
		t.Fatal("identifier reused at login")
	}
	if _, _, err = f.s.Authenticate(ctx, raw); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("previous login session remains valid")
	}
	i, _, err = f.s.Authenticate(ctx, newToken)
	must(t, err)
	must(t, f.s.Logout(ctx, i.SessionID))
	if _, _, err = f.s.Authenticate(ctx, newToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("logout did not revoke")
	}
	_, err = f.db.Exec(`UPDATE users SET is_active=FALSE WHERE username='alice'`)
	must(t, err)
	if _, _, err = f.s.Login(ctx, "alice", "employee-password", ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("disabled login accepted")
	}
}

func TestSessionLifecycleIntegration(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	i, raw := f.login(t, "admin", "admin-password-123")
	f.clock.Add(int64(15 * time.Minute))
	rotated, newRaw, err := f.s.Authenticate(ctx, raw)
	must(t, err)
	if newRaw == "" || newRaw == raw || !rotated.AbsoluteExpires.Equal(i.AbsoluteExpires) {
		t.Fatal("rotation missing or extended absolute expiry")
	}
	_, replacement, err := f.s.Authenticate(ctx, raw)
	must(t, err)
	if replacement != "" {
		t.Fatal("grace request reset cookie")
	}
	f.clock.Add(int64(30 * time.Second))
	if _, _, err = f.s.Authenticate(ctx, raw); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("grace token accepted at expiry")
	}
	_, _, err = f.s.Authenticate(ctx, newRaw)
	must(t, err)
	f.clock.Add(int64(30 * time.Minute))
	if _, _, err = f.s.Authenticate(ctx, newRaw); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("idle expiration ignored")
	}
	_, raw = f.login(t, "admin", "admin-password-123")
	for n := 0; n < 31; n++ {
		f.clock.Add(int64(15 * time.Minute))
		_, next, err := f.s.Authenticate(ctx, raw)
		must(t, err)
		if next != "" {
			raw = next
		}
	}
	f.clock.Add(int64(15 * time.Minute))
	if _, _, err = f.s.Authenticate(ctx, raw); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("absolute expiration extended by activity")
	}
}

func TestConcurrentRotationIntegration(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	i, raw := f.login(t, "admin", "admin-password-123")
	f.clock.Add(int64(15 * time.Minute))
	var wg sync.WaitGroup
	var rotations atomic.Int64
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, replacement, err := f.s.Authenticate(ctx, raw)
			if err != nil {
				t.Error(err)
			}
			if replacement != "" {
				rotations.Add(1)
			}
		}()
	}
	wg.Wait()
	var count int
	must(t, f.db.QueryRow(`SELECT count(*) FROM session_tokens WHERE session_id=$1 AND replaced_at IS NULL`, i.SessionID).Scan(&count))
	if rotations.Load() != 1 || count != 1 {
		t.Fatalf("concurrent rotation created %d cookies, %d current tokens", rotations.Load(), count)
	}
}

func TestAuthorizationAndRevocationIntegration(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	alice := f.user(t, "alice", false)
	bob := f.user(t, "bob", false)
	g1 := f.group(t, "Analysts")
	g2 := f.group(t, "Developers")
	d1 := f.database(t, "Products")
	d2 := f.database(t, "Finance")
	must(t, f.s.SetMembership(ctx, f.admin, alice, g1, true))
	must(t, f.s.SetMembership(ctx, f.admin, alice, g2, true))
	must(t, f.s.SetMembership(ctx, f.admin, bob, g1, true))
	must(t, f.s.SetGrant(ctx, f.admin, g1, d1, ViewOnly))
	must(t, f.s.SetGrant(ctx, f.admin, g2, d1, ViewEdit))
	must(t, f.s.SetGrant(ctx, f.admin, g2, d2, ViewOnly))
	for _, test := range []struct {
		user, db int64
		want     Permission
	}{{alice, d1, ViewEdit}, {alice, d2, ViewOnly}, {bob, d1, ViewOnly}, {bob, d2, NoAccess}, {f.admin, d1, NoAccess}} {
		got, err := f.s.EffectivePermission(ctx, test.user, test.db)
		must(t, err)
		if got != test.want {
			t.Errorf("effective access = %s, want %s", got, test.want)
		}
	}
	if err := f.s.RequirePermission(ctx, bob, d1, ViewEdit); !errors.Is(err, ErrForbidden) {
		t.Fatal("view-only write allowed")
	}
	if _, err := f.s.OpenTarget(ctx, bob, d1, ViewEdit); !errors.Is(err, ErrForbidden) {
		t.Fatal("target credential opened without write access")
	}
	dbs, err := f.s.AccessibleDatabases(ctx, bob)
	must(t, err)
	if len(dbs) != 1 || dbs[0].ID != d1 {
		t.Fatal("unauthorized database leaked")
	}
	_, raw := f.login(t, "alice", "employee-password")
	must(t, f.s.SetMembership(ctx, f.admin, alice, g2, false))
	if _, _, err = f.s.Authenticate(ctx, raw); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("membership removal did not revoke")
	}
	got, err := f.s.EffectivePermission(ctx, alice, d1)
	must(t, err)
	if got != ViewOnly {
		t.Fatal("removed group still grants edit")
	}
	_, raw = f.login(t, "alice", "employee-password")
	must(t, f.s.SetGrant(ctx, f.admin, g1, d1, NoAccess))
	if _, _, err = f.s.Authenticate(ctx, raw); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("grant revocation did not revoke session")
	}
	if err = f.s.SetGrant(ctx, f.admin, g1, d1, Permission("superuser")); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid grant accepted")
	}
	must(t, f.s.SetGrant(ctx, f.admin, g1, d1, ViewOnly))
	must(t, f.s.DeleteGroup(ctx, f.admin, g1))
	got, err = f.s.EffectivePermission(ctx, bob, d1)
	must(t, err)
	if got != NoAccess {
		t.Fatal("deleted group still grants access")
	}
}

func TestAdminMutationsIntegration(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	employee := f.user(t, "employee", false)
	if err := f.s.CreateUser(ctx, employee, "intruder", "Intruder", "employee-password", true); !errors.Is(err, ErrForbidden) {
		t.Fatal("employee created admin")
	}
	if err := f.s.UpdateUser(ctx, f.admin, f.admin, "Admin", false, true, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("last active admin removed")
	}
	if err := f.s.CreateUser(ctx, f.admin, "EMPLOYEE", "Duplicate", "employee-password", false); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate username accepted")
	}
	if err := f.s.CreateUser(ctx, f.admin, "bad username", "Bad", "employee-password", false); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid username accepted")
	}
	_, raw := f.login(t, "employee", "employee-password")
	must(t, f.s.UpdateUser(ctx, f.admin, employee, "Employee", true, true, ""))
	if _, _, err := f.s.Authenticate(ctx, raw); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("elevation reused session identifier")
	}
	i, next := f.login(t, "employee", "employee-password")
	if !i.User.IsAdmin || next == raw {
		t.Fatal("elevation failed to issue fresh identity")
	}
	must(t, f.s.UpdateUser(ctx, f.admin, employee, "Employee", true, true, "new-employee-password"))
	if _, _, err := f.s.Authenticate(ctx, next); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("password reset did not revoke")
	}
	must(t, f.s.UpdateUser(ctx, f.admin, employee, "Employee", false, false, ""))
	if _, _, err := f.s.Login(ctx, "employee", "new-employee-password", ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("disabled account logged in")
	}
}

func perform(handler http.Handler, method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var body string
	if form != nil {
		body = form.Encode()
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}
func status(t *testing.T, res *httptest.ResponseRecorder, want int) {
	t.Helper()
	if res.Code != want {
		t.Fatalf("status %d want %d; body %s", res.Code, want, res.Body.String())
	}
}

func TestHTTPAccessControlIntegration(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	handler, err := f.s.Handler()
	must(t, err)
	employee := f.user(t, "employee", false)
	status(t, perform(handler, "GET", "/", nil), 303)
	status(t, perform(handler, "GET", "/api/me", nil), 401)
	status(t, perform(handler, "POST", "/login", url.Values{"username": {"admin"}, "password": {"admin-password-123"}}), 403)
	page := perform(handler, "GET", "/login", nil)
	status(t, page, 200)
	re := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	match := re.FindStringSubmatch(page.Body.String())
	if len(match) != 2 {
		t.Fatal("login CSRF field missing")
	}
	cookies := page.Result().Cookies()
	status(t, perform(handler, "POST", "/login", url.Values{"username": {"missing"}, "password": {"wrong"}, "csrf_token": {match[1]}}, cookies...), 401)
	login := perform(handler, "POST", "/login", url.Values{"username": {"admin"}, "password": {"admin-password-123"}, "csrf_token": {match[1]}}, cookies...)
	status(t, login, 303)
	var session *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == CookieName {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode || session.Path != "/" {
		t.Fatal("invalid session cookie flags")
	}
	if strings.Contains(login.Body.String(), session.Value) {
		t.Fatal("raw session identifier leaked in response")
	}
	for _, path := range []string{"/", "/databases", "/admin/users", "/admin/groups", "/admin/databases", "/admin/access", "/api/me", "/api/csrf", "/api/databases"} {
		res := perform(handler, "GET", path, nil, session)
		status(t, res, 200)
		if strings.Contains(res.Body.String(), session.Value) {
			t.Fatal("session token reflected")
		}
	}
	i, raw := f.login(t, "employee", "employee-password")
	employeeCookie := &http.Cookie{Name: CookieName, Value: raw}
	status(t, perform(handler, "GET", "/admin/users", nil, employeeCookie), 403)
	status(t, perform(handler, "POST", "/admin/users/create", url.Values{"csrf_token": {f.s.csrf(i)}, "username": {"attacker"}, "password": {"employee-password"}}, employeeCookie), 403)
	status(t, perform(handler, "POST", "/logout", url.Values{}, employeeCookie), 403)
	status(t, perform(handler, "POST", "/logout?csrf_token="+url.QueryEscape(f.s.csrf(i)), url.Values{}, employeeCookie), 403)
	status(t, perform(handler, "POST", "/logout", url.Values{"csrf_token": {f.s.csrf(i)}}, employeeCookie), 303)
	status(t, perform(handler, "GET", "/api/me", nil, employeeCookie), 401)
	adminIdentity, adminRaw := f.login(t, "admin", "admin-password-123")
	adminCookie := &http.Cookie{Name: CookieName, Value: adminRaw}
	status(t, perform(handler, "POST", "/admin/groups/save", url.Values{"name": {"Test"}}, adminCookie), 403)
	status(t, perform(handler, "POST", "/admin/groups/save", url.Values{"name": {"Test"}, "csrf_token": {f.s.csrf(adminIdentity)}}, adminCookie), 303)
	status(t, perform(handler, "POST", "/admin/groups/save", url.Values{"id": {"broken"}, "name": {"Invalid"}, "csrf_token": {f.s.csrf(adminIdentity)}}, adminCookie), 400)
	status(t, perform(handler, "GET", "/register", nil), 404)
	// Exported middleware denies database writes to View Only, and has no admin bypass.
	g := f.group(t, "Readers")
	d := f.database(t, "Sensitive")
	must(t, f.s.SetMembership(ctx, f.admin, employee, g, true))
	must(t, f.s.SetGrant(ctx, f.admin, g, d, ViewOnly))
	i, raw = f.login(t, "employee", "employee-password")
	employeeCookie = &http.Cookie{Name: CookieName, Value: raw}
	mux := http.NewServeMux()
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.Handle("GET /db/{databaseID}", f.s.RequireDatabase(ViewOnly, endpoint))
	mux.Handle("POST /db/{databaseID}", f.s.RequireDatabase(ViewEdit, endpoint))
	status(t, perform(mux, "GET", fmt.Sprint("/db/", d), nil, employeeCookie), 204)
	status(t, perform(mux, "POST", fmt.Sprint("/db/", d), url.Values{"csrf_token": {f.s.csrf(i)}}, employeeCookie), 403)
	status(t, perform(mux, "GET", fmt.Sprint("/db/", d), nil, adminCookie), 403)
	res := perform(handler, "GET", "/api/databases", nil, employeeCookie)
	status(t, res, 200)
	if strings.Contains(res.Body.String(), "do-not-expose") || strings.Contains(res.Body.String(), "sensitive-db-user") {
		t.Fatal("database credentials leaked")
	}
}

func TestTargetConnectionsIntegration(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	u, err := url.Parse(f.dsn)
	must(t, err)
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 5432
	}
	password, _ := u.User.Password()
	in := ConnectionInput{Name: "Test Target", Host: u.Hostname(), Port: port, DatabaseName: strings.TrimPrefix(u.Path, "/"), Username: u.User.Username(), Password: password, SSLMode: "disable", Active: true}
	must(t, f.s.SaveConnection(ctx, f.admin, 0, in))
	var id int64
	var encrypted []byte
	must(t, f.db.QueryRow(`SELECT id,encrypted_password FROM managed_databases WHERE display_name=$1`, in.Name).Scan(&id, &encrypted))
	plain, err := decryptSecret(f.s.config.Key, encrypted)
	must(t, err)
	if plain != password {
		t.Fatal("stored credential cannot be decrypted")
	}
	in.Name = "Updated Target"
	in.Password = ""
	must(t, f.s.SaveConnection(ctx, f.admin, id, in))
	g := f.group(t, "DBA")
	must(t, f.s.SetMembership(ctx, f.admin, f.admin, g, true))
	must(t, f.s.SetGrant(ctx, f.admin, g, id, ViewEdit))
	target, err := f.s.OpenTarget(ctx, f.admin, id, ViewEdit)
	must(t, err)
	must(t, target.PingContext(ctx))
	target.Close()
	_, raw := f.login(t, "admin", "admin-password-123")
	must(t, f.s.DisableConnection(ctx, f.admin, id))
	if _, _, err = f.s.Authenticate(ctx, raw); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("connection disable did not revoke")
	}
	if _, err = f.s.OpenTarget(ctx, f.admin, id, ViewOnly); !errors.Is(err, ErrForbidden) {
		t.Fatal("disabled target accessible")
	}
	in.Name = "Unreachable"
	in.Port = 1
	if err = f.s.SaveConnection(ctx, f.admin, 0, in); err == nil {
		t.Fatal("unavailable target saved")
	}
	var count int
	must(t, f.db.QueryRow(`SELECT count(*) FROM managed_databases WHERE display_name='Unreachable'`).Scan(&count))
	if count != 0 {
		t.Fatal("failed connection persisted")
	}
	in.Port = 5432
	in.SSLMode = "unexpected"
	if err = f.s.SaveConnection(ctx, f.admin, 0, in); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid connection accepted")
	}
}
