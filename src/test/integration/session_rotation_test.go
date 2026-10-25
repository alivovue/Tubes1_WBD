package integration_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"tubes1_wbd/backend"
	"tubes1_wbd/backend/access"
)

func rotationClock(t *testing.T, f *fixture) *time.Time {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	config := access.DefaultConfig(bytes.Repeat([]byte{21}, 32))
	config.Now = func() time.Time { return now }
	service, err := access.NewService(f.store, config)
	must(t, err)
	f.access = service
	return &now
}
func currentHash(t *testing.T, f *fixture, session string) []byte {
	t.Helper()
	var hash []byte
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT token_hash FROM session_tokens WHERE session_id=$1 AND replaced_at IS NULL`, session).Scan(&hash))
	return hash
}
func promotionHandler(t *testing.T, f *fixture) http.Handler {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	must(t, err)
	handler, err := backend.NewHandlerWithServices(root, backend.Services{Access: f.access, Databases: f.databases, Migrations: f.migrations})
	must(t, err)
	return handler
}
func TestPrivilegePromotionRotatesAllActiveSessionsAndDeliversCookie(t *testing.T) {
	f := newFixture(t, true)
	now := rotationClock(t, f)
	user := f.user(t, "promotion.employee")
	first, oldFirst := f.login(t, user.Username)
	second, oldSecond := f.login(t, user.Username)
	expired, oldExpired := f.login(t, user.Username)
	_, err := f.dbs.Website.ExecContext(f.ctx, `UPDATE sessions SET idle_expires_at=$2 WHERE id=$1`, expired.SessionID, now.Add(-time.Second))
	must(t, err)
	firstHash := currentHash(t, f, first.SessionID)
	secondHash := currentHash(t, f, second.SessionID)
	expiredHash := currentHash(t, f, expired.SessionID)
	*now = now.Add(3 * time.Second)
	must(t, f.access.UpdateUser(f.ctx, f.admin, user.ID, user.DisplayName, true, true, ""))
	if bytes.Equal(currentHash(t, f, first.SessionID), firstHash) || bytes.Equal(currentHash(t, f, second.SessionID), secondHash) {
		t.Fatal("promotion did not immediately rotate every active session")
	}
	if !bytes.Equal(currentHash(t, f, expired.SessionID), expiredHash) {
		t.Fatal("promotion rotated an expired session")
	}
	for _, identity := range []access.Identity{first, second} {
		var revoked bool
		var absolute, idle time.Time
		must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT revoked_at IS NOT NULL,absolute_expires_at,idle_expires_at FROM sessions WHERE id=$1`, identity.SessionID).Scan(&revoked, &absolute, &idle))
		if revoked || !absolute.Equal(identity.AbsoluteExpires) || !idle.Equal(first.AbsoluteExpires.Add(-8*time.Hour).Add(30*time.Minute)) {
			t.Fatal("promotion revoked the session or changed its original expiration")
		}
	}
	request := httptest.NewRequest(http.MethodGet, "https://schakker.test/admin/users", nil)
	request.AddCookie(&http.Cookie{Name: access.CookieName, Value: oldFirst})
	response := httptest.NewRecorder()
	promotionHandler(t, f).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("promoted user's existing session cannot open admin pages: %d", response.Code)
	}
	var replacement *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == access.CookieName {
			replacement = cookie
		}
	}
	if replacement == nil || replacement.Value == oldFirst || !replacement.HttpOnly || !replacement.Secure || replacement.SameSite != http.SameSiteLaxMode || !replacement.Expires.Equal(first.AbsoluteExpires.Truncate(time.Second)) {
		t.Fatal("replacement cookie or its lifecycle flags are incorrect")
	}
	hashed := sha256.Sum256([]byte(replacement.Value))
	if !bytes.Equal(hashed[:], currentHash(t, f, first.SessionID)) {
		t.Fatal("browser did not receive the persisted replacement token")
	}
	if bytes.Contains(response.Body.Bytes(), []byte(replacement.Value)) {
		t.Fatal("session token was exposed in the response body")
	}
	identity, newSecond, err := f.access.Authenticate(f.ctx, oldSecond)
	must(t, err)
	if !identity.User.IsAdmin || newSecond == "" || newSecond == oldSecond || !identity.AbsoluteExpires.Equal(second.AbsoluteExpires) {
		t.Fatal("second browser did not receive its replacement session")
	}
	_, _, err = f.access.Authenticate(f.ctx, oldExpired)
	wantError(t, err, access.ErrUnauthenticated)
	*now = now.Add(30 * time.Second)
	_, _, err = f.access.Authenticate(f.ctx, oldFirst)
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = f.access.Authenticate(f.ctx, oldSecond)
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = f.access.Authenticate(f.ctx, replacement.Value)
	must(t, err)
	_, _, err = f.access.Authenticate(f.ctx, newSecond)
	must(t, err)
}
func TestPrivilegeRotationsRecoverLatestTokenForConcurrentGraceRequests(t *testing.T) {
	f := newFixture(t, true)
	now := rotationClock(t, f)
	user := f.user(t, "concurrent.employee")
	identity, old := f.login(t, user.Username)
	must(t, f.access.UpdateUser(f.ctx, f.admin, user.ID, user.DisplayName, true, true, ""))
	target := f.register(t)
	group := f.grant(t, user.ID, target.ID, access.ViewOnly)
	must(t, f.access.SetGrant(f.ctx, f.admin, group.ID, target.ID, access.ViewEdit))
	expected := currentHash(t, f, identity.SessionID)
	var before time.Time
	oldHash := sha256.Sum256([]byte(old))
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT grace_expires_at FROM session_tokens WHERE token_hash=$1`, oldHash[:]).Scan(&before))
	var wait sync.WaitGroup
	failures := make(chan error, 12)
	replacements := make(chan string, 12)
	for range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			current, replacement, err := f.access.Authenticate(f.ctx, old)
			if err != nil {
				failures <- err
				return
			}
			hash := sha256.Sum256([]byte(replacement))
			if !bytes.Equal(hash[:], expected) || !current.User.IsAdmin || !current.AbsoluteExpires.Equal(identity.AbsoluteExpires) {
				failures <- errors.New("parallel request did not recover the latest token")
				return
			}
			replacements <- replacement
		}()
	}
	wait.Wait()
	close(failures)
	close(replacements)
	for err := range failures {
		t.Error(err)
	}
	var after time.Time
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT grace_expires_at FROM session_tokens WHERE token_hash=$1`, oldHash[:]).Scan(&after))
	if !before.Equal(after) {
		t.Fatal("grace handoff extended the old token's validity")
	}
	var count int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM session_tokens WHERE session_id=$1 AND replaced_at IS NULL`, identity.SessionID).Scan(&count))
	if count != 1 {
		t.Fatal("parallel requests created multiple current tokens")
	}
	*now = before
	_, _, err := f.access.Authenticate(f.ctx, old)
	wantError(t, err, access.ErrUnauthenticated)
	for replacement := range replacements {
		_, _, err := f.access.Authenticate(f.ctx, replacement)
		must(t, err)
	}
}
func TestPrivilegeRotationFailureRollsBackPromotionAndTokens(t *testing.T) {
	f := newFixture(t, true)
	rotationClock(t, f)
	user := f.user(t, "rollback.employee")
	identity, old := f.login(t, user.Username)
	before := currentHash(t, f, identity.SessionID)
	_, err := f.dbs.Website.ExecContext(f.ctx, `CREATE FUNCTION reject_rotation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated token storage failure'; END $$; CREATE TRIGGER reject_rotation BEFORE INSERT ON session_tokens FOR EACH ROW EXECUTE FUNCTION reject_rotation()`)
	must(t, err)
	if err := f.access.UpdateUser(f.ctx, f.admin, user.ID, user.DisplayName, true, true, ""); err == nil {
		t.Fatal("promotion succeeded despite a failed token rotation")
	}
	saved, err := f.store.User(f.ctx, user.ID)
	must(t, err)
	if saved.IsAdmin {
		t.Fatal("failed rotation committed the increased privilege")
	}
	if !bytes.Equal(before, currentHash(t, f, identity.SessionID)) {
		t.Fatal("failed promotion committed a replacement token")
	}
	current, replacement, err := f.access.Authenticate(f.ctx, old)
	must(t, err)
	if current.User.IsAdmin || replacement != "" {
		t.Fatal("rolled-back promotion changed the existing session")
	}
}
func TestPrivilegeRotationGraceDoesNotOutliveAbsoluteExpiration(t *testing.T) {
	f := newFixture(t, true)
	now := rotationClock(t, f)
	user := f.user(t, "expiration.employee")
	identity, old := f.login(t, user.Username)
	_, err := f.dbs.Website.ExecContext(f.ctx, `UPDATE sessions SET idle_expires_at=absolute_expires_at WHERE id=$1`, identity.SessionID)
	must(t, err)
	*now = identity.AbsoluteExpires.Add(-5 * time.Second)
	must(t, f.access.UpdateUser(f.ctx, f.admin, user.ID, user.DisplayName, true, true, ""))
	hash := sha256.Sum256([]byte(old))
	var grace time.Time
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT grace_expires_at FROM session_tokens WHERE token_hash=$1`, hash[:]).Scan(&grace))
	if !grace.Equal(identity.AbsoluteExpires) {
		t.Fatal("promotion grace period exceeded the absolute expiration")
	}
	current, replacement, err := f.access.Authenticate(f.ctx, old)
	must(t, err)
	if replacement == "" || !current.AbsoluteExpires.Equal(identity.AbsoluteExpires) {
		t.Fatal("rotation changed the absolute expiration")
	}
	*now = identity.AbsoluteExpires
	_, _, err = f.access.Authenticate(f.ctx, old)
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = f.access.Authenticate(f.ctx, replacement)
	wantError(t, err, access.ErrUnauthenticated)
}
