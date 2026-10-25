package unit_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"tubes1_wbd/backend/access"
)

func credentialsStep(hash string, active bool) sqlStep {
	return queryStep("WHERE lower(username)=lower($1)", []string{"id", "username", "display", "admin", "active", "hash"}, []driver.Value{int64(7), "employee", "Employee", false, active, hash})
}

func TestLoginRejectsWrongMissingAndDisabledAccounts(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	_, store, stub := newSQLStub(t)
	service := newAccess(t, store, nil)
	for _, tc := range []struct {
		name, password string
		step           sqlStep
	}{
		{"wrong password", "wrong-password", credentialsStep(string(hash), true)},
		{"disabled account", testPassword, credentialsStep(string(hash), false)},
		{"missing account", testPassword, queryStep("WHERE lower(username)=lower($1)", []string{"id", "username", "display", "admin", "active", "hash"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub.append(tc.step)
			identity, raw, err := service.Login(context.Background(), "employee", tc.password, "")
			wantError(t, err, access.ErrUnauthenticated)
			if raw != "" || identity.User.ID != 0 {
				t.Fatal("rejected login issued an identity or session token")
			}
		})
	}
	for _, input := range []struct{ username, password string }{{strings.Repeat("a", 101), testPassword}, {"employee", strings.Repeat("a", 73)}} {
		_, _, err := service.Login(context.Background(), input.username, input.password, "")
		wantError(t, err, access.ErrUnauthenticated)
	}
}

func TestLoginCreatesBoundedSessionAndHashesOpaqueToken(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	oldHash := sha256.Sum256([]byte("previous-browser-token"))
	var storedHash []byte
	create := scalarStep("INSERT INTO sessions", testSession)
	create.check = func(args []driver.NamedValue) {
		wantArg(t, args, 0, int64(7))
		wantArg(t, args, 1, testTime)
		wantArg(t, args, 2, testTime.Add(30*time.Minute))
		wantArg(t, args, 3, testTime.Add(8*time.Hour))
	}
	revoke := execStep("WHERE id IN (SELECT session_id FROM session_tokens")
	revoke.check = func(args []driver.NamedValue) { wantArg(t, args, 0, oldHash[:]); wantArg(t, args, 1, testTime) }
	insert := execStep("INSERT INTO session_tokens")
	insert.check = func(args []driver.NamedValue) {
		wantArg(t, args, 0, testSession)
		storedHash = append([]byte(nil), hashArgument(t, args[1].Value)...)
		wantArg(t, args, 2, testTime)
	}
	_, store, _ := newSQLStub(t, credentialsStep(string(hash), true), sqlStep{kind: "begin"}, scalarStep("FOR SHARE", true), userStep(false, true), revoke, create, insert, sqlStep{kind: "commit"})
	service := newAccess(t, store, nil)
	identity, raw, err := service.Login(context.Background(), "employee", testPassword, "previous-browser-token")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 43 || raw == "previous-browser-token" || raw == testSession {
		t.Fatal("login did not generate a new opaque session token")
	}
	expectedHash := sha256.Sum256([]byte(raw))
	if !bytes.Equal(storedHash, expectedHash[:]) {
		t.Fatal("database did not receive the session token hash")
	}
	if identity.SessionID != testSession || identity.User.ID != 7 || !identity.AbsoluteExpires.Equal(testTime.Add(8*time.Hour)) {
		t.Fatalf("incorrect session identity: %#v", identity)
	}
}

func TestLoginCredentialRaceRollsBack(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	_, store, _ := newSQLStub(t, credentialsStep(string(hash), true), sqlStep{kind: "begin"}, scalarStep("FOR SHARE", false), sqlStep{kind: "rollback"})
	service := newAccess(t, store, nil)
	_, raw, err := service.Login(context.Background(), "employee", testPassword, "")
	wantError(t, err, access.ErrUnauthenticated)
	if raw != "" {
		t.Fatal("credential race issued a session")
	}
}

func TestAuthenticationSessionLifecycle(t *testing.T) {
	cases := []struct {
		name                              string
		idle, absolute, issued            time.Time
		replaced, grace                   *time.Time
		revoked, disabled, rotate, reject bool
	}{
		{name: "valid session"}, {name: "periodic rotation", issued: testTime.Add(-16 * time.Minute), rotate: true},
		{name: "rotation preserves absolute lifetime", absolute: testTime.Add(10 * time.Second), issued: testTime.Add(-16 * time.Minute), rotate: true},
		{name: "old token accepted during grace", issued: testTime.Add(-time.Hour), replaced: timePointer(testTime.Add(-3 * time.Second)), grace: timePointer(testTime.Add(20 * time.Second))},
		{name: "grace expires at boundary", replaced: timePointer(testTime.Add(-30 * time.Second)), grace: timePointer(testTime), reject: true},
		{name: "replaced token without grace", replaced: timePointer(testTime.Add(-time.Second)), reject: true},
		{name: "idle expiry", idle: testTime, reject: true}, {name: "absolute expiry", absolute: testTime, reject: true},
		{name: "revoked session", revoked: true, reject: true}, {name: "disabled account", disabled: true, reject: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.idle.IsZero() {
				tc.idle = testTime.Add(30 * time.Minute)
			}
			if tc.absolute.IsZero() {
				tc.absolute = testTime.Add(8 * time.Hour)
			}
			if tc.issued.IsZero() {
				tc.issued = testTime.Add(-time.Minute)
			}
			oldHash := sha256.Sum256([]byte(testToken))
			lock := queryStep("FOR UPDATE OF s", []string{"id", "user", "idle", "absolute", "revoked"}, []driver.Value{testSession, int64(7), tc.idle, tc.absolute, tc.revoked})
			lock.check = func(args []driver.NamedValue) { wantArg(t, args, 0, oldHash[:]) }
			var replaced, grace driver.Value
			if tc.replaced != nil {
				replaced = *tc.replaced
			}
			if tc.grace != nil {
				grace = *tc.grace
			}
			steps := []sqlStep{{kind: "begin"}, lock, queryStep("SELECT issued_at,replaced_at,grace_expires_at", []string{"issued", "replaced", "grace"}, []driver.Value{tc.issued, replaced, grace})}
			if !tc.reject || tc.disabled {
				steps = append(steps, userStep(false, !tc.disabled))
			}
			var replacementHash []byte
			if tc.rotate {
				replace := execStep("UPDATE session_tokens SET replaced_at=")
				replace.check = func(args []driver.NamedValue) {
					wantArg(t, args, 0, oldHash[:])
					wantArg(t, args, 1, testTime)
					wantArg(t, args, 2, earlier(testTime.Add(30*time.Second), tc.absolute))
				}
				insert := execStep("INSERT INTO session_tokens")
				insert.check = func(args []driver.NamedValue) {
					wantArg(t, args, 0, testSession)
					replacementHash = append([]byte(nil), hashArgument(t, args[1].Value)...)
				}
				steps = append(steps, replace, insert)
			}
			if tc.replaced != nil && !tc.reject {
				steps = append(steps, queryStep("WHERE session_id=$1 AND token_hash=$2", []string{"issued", "replaced", "grace"}))
			}
			if tc.reject {
				steps = append(steps, sqlStep{kind: "rollback"})
			} else {
				touch := execStep("UPDATE sessions SET last_seen_at=")
				touch.check = func(args []driver.NamedValue) {
					wantArg(t, args, 0, testSession)
					wantArg(t, args, 2, earlier(testTime.Add(30*time.Minute), tc.absolute))
				}
				steps = append(steps, touch, execStep("DELETE FROM session_tokens"), sqlStep{kind: "commit"})
			}
			_, store, _ := newSQLStub(t, steps...)
			service := newAccess(t, store, nil)
			identity, raw, err := service.Authenticate(context.Background(), testToken)
			if tc.reject {
				wantError(t, err, access.ErrUnauthenticated)
				if raw != "" || identity.User.ID != 0 {
					t.Fatal("invalid session returned credentials")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if identity.SessionID != testSession || !identity.AbsoluteExpires.Equal(tc.absolute) {
				t.Fatal("authentication changed the original absolute lifetime")
			}
			if tc.rotate {
				if len(raw) != 43 || raw == testToken {
					t.Fatal("rotation did not issue a new random token")
				}
				hash := sha256.Sum256([]byte(raw))
				if !bytes.Equal(replacementHash, hash[:]) {
					t.Fatal("replacement token was not hashed")
				}
			} else if raw != "" {
				t.Fatal("token rotated before its interval or during grace")
			}
		})
	}
}

func TestAuthenticationMissingTokenAndStorageFailure(t *testing.T) {
	failure := errors.New("storage failed")
	for _, tc := range []struct {
		name string
		step sqlStep
		want error
	}{
		{"unknown token", queryStep("FOR UPDATE OF s", []string{"id", "user", "idle", "absolute", "revoked"}), access.ErrUnauthenticated},
		{"database failure", sqlStep{kind: "query", query: "FOR UPDATE OF s", err: failure}, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, store, _ := newSQLStub(t, sqlStep{kind: "begin"}, tc.step, sqlStep{kind: "rollback"})
			service := newAccess(t, store, nil)
			_, _, err := service.Authenticate(context.Background(), testToken)
			wantError(t, err, tc.want)
		})
	}
	_, store, _ := newSQLStub(t)
	service := newAccess(t, store, nil)
	for _, raw := range []string{"", "short", strings.Repeat("a", 44)} {
		_, _, err := service.Authenticate(context.Background(), raw)
		wantError(t, err, access.ErrUnauthenticated)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	step := execStep("UPDATE sessions SET revoked_at=")
	step.check = func(args []driver.NamedValue) { wantArg(t, args, 0, testSession); wantArg(t, args, 1, testTime) }
	_, store, _ := newSQLStub(t, step)
	service := newAccess(t, store, nil)
	wantError(t, service.Logout(context.Background(), ""), access.ErrUnauthenticated)
	if err := service.Logout(context.Background(), testSession); err != nil {
		t.Fatal(err)
	}
}
func timePointer(value time.Time) *time.Time { return &value }
func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
