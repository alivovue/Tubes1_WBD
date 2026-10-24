package integration_test

import (
	"crypto/sha256"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"tubes1_wbd/backend/access"
)

func TestIT01CreateAdminAndLogin(t *testing.T) {
	f := newFixture(t, false)
	var before int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM users`).Scan(&before))
	if before != 0 {
		t.Fatal("website fixture already contains users")
	}
	must(t, f.access.BootstrapAdmin(f.ctx, "it.admin", "Integration Admin", password))
	credentials, err := f.store.Credentials(f.ctx, "it.admin")
	must(t, err)
	if !credentials.IsAdmin || !credentials.Active || credentials.PasswordHash == password {
		t.Fatal("persisted admin or password storage is invalid")
	}
	must(t, bcrypt.CompareHashAndPassword([]byte(credentials.PasswordHash), []byte(password)))
	identity, raw := f.login(t, "it.admin")
	if identity.User.ID != credentials.ID || identity.SessionID == "" || len(raw) != 43 {
		t.Fatal("login did not create a valid identity and opaque token")
	}
	var user int64
	var revoked bool
	var idle, absolute time.Time
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT user_id,revoked_at IS NOT NULL,idle_expires_at,absolute_expires_at FROM sessions WHERE id=$1`, identity.SessionID).Scan(&user, &revoked, &idle, &absolute))
	if user != credentials.ID || revoked || absolute.Sub(identity.AbsoluteExpires).Abs() > time.Microsecond || !idle.Before(absolute) {
		t.Fatal("persisted session does not match the login")
	}
	hash := sha256.Sum256([]byte(raw))
	var stored int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM session_tokens WHERE session_id=$1 AND token_hash=$2`, identity.SessionID, hash[:]).Scan(&stored))
	if stored != 1 {
		t.Fatal("opaque token was not stored as a hash")
	}
	authenticated, _, err := f.access.Authenticate(f.ctx, raw)
	must(t, err)
	if authenticated.SessionID != identity.SessionID {
		t.Fatal("returned token does not authenticate")
	}
}
func TestIT02IncorrectCredentialsCreateNoSession(t *testing.T) {
	f := newFixture(t, true)
	for _, input := range []struct{ username, password string }{{"it.admin", "incorrect-password"}, {"missing.user", password}} {
		identity, raw, err := f.access.Login(f.ctx, input.username, input.password, "")
		wantError(t, err, access.ErrUnauthenticated)
		if identity.User.ID != 0 || raw != "" {
			t.Fatal("rejected login returned credentials")
		}
	}
	for _, table := range []string{"sessions", "session_tokens"} {
		var count int
		must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM `+table).Scan(&count))
		if count != 0 {
			t.Fatalf("rejected login inserted into %s", table)
		}
	}
}
func TestIT03LogoutInvalidatesPersistedSession(t *testing.T) {
	f := newFixture(t, true)
	identity, raw := f.login(t, "it.admin")
	_, _, err := f.access.Authenticate(f.ctx, raw)
	must(t, err)
	must(t, f.access.Logout(f.ctx, identity.SessionID))
	_, _, err = f.access.Authenticate(f.ctx, raw)
	wantError(t, err, access.ErrUnauthenticated)
	var revoked bool
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1`, identity.SessionID).Scan(&revoked))
	if !revoked {
		t.Fatal("logout did not persist session revocation")
	}
}
func TestIT04DeactivateUserInvalidatesSessions(t *testing.T) {
	f := newFixture(t, true)
	user := f.user(t, "it.employee")
	identity, raw := f.login(t, user.Username)
	must(t, f.access.UpdateUser(f.ctx, f.admin, user.ID, user.DisplayName, false, false, ""))
	saved, err := f.store.User(f.ctx, user.ID)
	must(t, err)
	if saved.Active {
		t.Fatal("user remains active in PostgreSQL")
	}
	_, _, err = f.access.Authenticate(f.ctx, raw)
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = f.access.Login(f.ctx, user.Username, password, "")
	wantError(t, err, access.ErrUnauthenticated)
	var revoked bool
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1`, identity.SessionID).Scan(&revoked))
	if !revoked {
		t.Fatal("deactivation did not revoke the existing session")
	}
}
func TestIT05LastActiveAdminCannotBeRemoved(t *testing.T) {
	f := newFixture(t, true)
	for _, change := range []struct{ admin, active bool }{{false, true}, {true, false}} {
		wantError(t, f.access.UpdateUser(f.ctx, f.admin, f.admin, "Integration Admin", change.admin, change.active, ""), access.ErrInvalid)
		saved, err := f.store.User(f.ctx, f.admin)
		must(t, err)
		if !saved.Active || !saved.IsAdmin {
			t.Fatal("the last active admin was removed")
		}
	}
	f.login(t, "it.admin")
}
