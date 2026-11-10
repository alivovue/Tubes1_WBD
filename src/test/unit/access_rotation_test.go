package unit_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"tubes1_wbd/backend/access"
)

func activeSessionsStep(rows ...[]driver.Value) sqlStep {
	return queryStep("FROM sessions WHERE", []string{"id", "user", "idle", "absolute", "revoked"}, rows...)
}
func TestMembershipIncreaseRotatesEveryActiveSessionWithinOriginalLifetime(t *testing.T) {
	oldA := sha256.Sum256([]byte(testToken))
	oldB := sha256.Sum256([]byte("second-browser-token"))
	absoluteA := testTime.Add(8 * time.Hour)
	absoluteB := testTime.Add(5 * time.Second)
	steps := append(transactionSteps(true), scalarStep("CROSS JOIN access_groups", true), execStep("INSERT INTO user_groups"), activeSessionsStep(
		[]driver.Value{"browser-a", int64(7), testTime.Add(time.Minute), absoluteA, false},
		[]driver.Value{"browser-b", int64(7), absoluteB, absoluteB, false},
	))
	var hashes [][]byte
	for _, entry := range []struct {
		id      string
		hash    []byte
		expires time.Time
	}{{"browser-a", oldA[:], absoluteA}, {"browser-b", oldB[:], absoluteB}} {
		current := scalarStep("SELECT token_hash FROM session_tokens", entry.hash)
		current.check = func(args []driver.NamedValue) { wantArg(t, args, 0, entry.id) }
		replace := execStep("UPDATE session_tokens SET replaced_at=")
		replace.check = func(args []driver.NamedValue) {
			wantArg(t, args, 0, entry.hash)
			wantArg(t, args, 1, testTime)
			wantArg(t, args, 2, earlier(testTime.Add(30*time.Second), entry.expires))
		}
		insert := execStep("INSERT INTO session_tokens")
		insert.check = func(args []driver.NamedValue) {
			wantArg(t, args, 0, entry.id)
			hash := append([]byte(nil), hashArgument(t, args[1].Value)...)
			if len(hash) != 32 || bytes.Equal(hash, entry.hash) {
				t.Error("membership increase did not replace the hashed browser token")
			}
			hashes = append(hashes, hash)
			wantArg(t, args, 2, testTime)
		}
		steps = append(steps, current, replace, insert)
	}
	steps = append(steps, sqlStep{kind: "commit"})
	_, store, _ := newSQLStub(t, steps...)
	service := newAccess(t, store, nil)
	if err := service.SetMembership(context.Background(), 7, 7, 2, true); err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 2 || bytes.Equal(hashes[0], hashes[1]) {
		t.Fatal("active browser sessions did not receive distinct replacements")
	}
}

func TestMembershipRotationFailureRollsBackChange(t *testing.T) {
	failure := errors.New("token storage unavailable")
	old := sha256.Sum256([]byte(testToken))
	insert := execStep("INSERT INTO session_tokens")
	insert.err = failure
	steps := append(transactionSteps(true), scalarStep("CROSS JOIN access_groups", true), execStep("INSERT INTO user_groups"), activeSessionsStep([]driver.Value{testSession, int64(7), testTime.Add(time.Minute), testTime.Add(time.Hour), false}), scalarStep("SELECT token_hash FROM session_tokens", old[:]), execStep("UPDATE session_tokens SET"), insert, sqlStep{kind: "rollback"})
	_, store, _ := newSQLStub(t, steps...)
	service := newAccess(t, store, nil)
	wantError(t, service.SetMembership(context.Background(), 7, 7, 2, true), failure)
}

func TestGraceTokenReturnsExistingReplacementWithoutRotatingAgain(t *testing.T) {
	old := sha256.Sum256([]byte(testToken))
	var stored []byte
	insert := execStep("INSERT INTO session_tokens")
	insert.check = func(args []driver.NamedValue) { stored = append([]byte(nil), hashArgument(t, args[1].Value)...) }
	steps := append(transactionSteps(true), scalarStep("CROSS JOIN access_groups", true), execStep("INSERT INTO user_groups"), activeSessionsStep([]driver.Value{testSession, int64(7), testTime.Add(time.Minute), testTime.Add(time.Hour), false}), scalarStep("SELECT token_hash FROM session_tokens", old[:]), execStep("UPDATE session_tokens SET"), insert, sqlStep{kind: "commit"})
	_, store, stub := newSQLStub(t, steps...)
	service := newAccess(t, store, nil)
	if err := service.SetMembership(context.Background(), 7, 7, 2, true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		scoped := queryStep("WHERE session_id=$1 AND token_hash=$2", []string{"issued", "replaced", "grace"}, []driver.Value{testTime, nil, nil})
		scoped.check = func(args []driver.NamedValue) { wantArg(t, args, 0, testSession); wantArg(t, args, 1, stored) }
		stub.append(sqlStep{kind: "begin"}, queryStep("FOR UPDATE OF s", []string{"id", "user", "idle", "absolute", "revoked"}, []driver.Value{testSession, int64(7), testTime.Add(time.Minute), testTime.Add(time.Hour), false}), queryStep("SELECT issued_at,replaced_at,grace_expires_at", []string{"issued", "replaced", "grace"}, []driver.Value{testTime.Add(-time.Minute), testTime, testTime.Add(30 * time.Second)}), userStep(false, true), scoped, execStep("UPDATE sessions SET last_seen_at="), execStep("DELETE FROM session_tokens"), sqlStep{kind: "commit"})
		identity, replacement, err := service.Authenticate(context.Background(), testToken)
		if err != nil {
			t.Fatal(err)
		}
		if identity.User.IsAdmin || len(replacement) != 43 || replacement == testToken {
			t.Fatal("ordinary user did not receive the replacement token")
		}
		hash := sha256.Sum256([]byte(replacement))
		if !bytes.Equal(hash[:], stored) {
			t.Fatal("grace delivery issued a different token than the membership transaction")
		}
	}
}
func TestAdministratorPasswordResetRevokesAdminSessions(t *testing.T) {
	steps := []sqlStep{{kind: "begin"}, execStep("pg_advisory_xact_lock"), scalarStep("admin_users", true), queryStep("FROM admin_users WHERE id=", []string{"id", "username", "display", "admin", "active"}, []driver.Value{int64(7), "admin", "Admin", true, true}), execStep("UPDATE admin_users SET"), execStep("UPDATE admin_sessions SET revoked_at="), {kind: "commit"}}
	_, store, _ := newSQLStub(t, steps...)
	service := newAdminAccess(t, store)
	if err := service.UpdateAdmin(context.Background(), 7, 7, "Admin", true, testPassword); err != nil {
		t.Fatal(err)
	}
}
func TestUnchangedGroupGrantDoesNotRotateOrRevokeSessions(t *testing.T) {
	steps := append(transactionSteps(true), scalarStep("SELECT COALESCE((SELECT access_level", string(access.ViewEdit)), scalarStep("CROSS JOIN managed_databases", true), execStep("INSERT INTO group_database_permissions"), sqlStep{kind: "commit"})
	_, store, _ := newSQLStub(t, steps...)
	service := newAccess(t, store, nil)
	if err := service.SetGrant(context.Background(), 7, 2, 3, access.ViewEdit); err != nil {
		t.Fatal(err)
	}
}
