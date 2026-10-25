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
func TestPromotionRotatesEveryActiveSessionWithinOriginalLifetime(t *testing.T) {
	oldA := sha256.Sum256([]byte(testToken))
	oldB := sha256.Sum256([]byte("second-browser-token"))
	absoluteA := testTime.Add(8 * time.Hour)
	absoluteB := testTime.Add(5 * time.Second)
	steps := append(transactionSteps(true), userStep(false, true), execStep("UPDATE users SET"), activeSessionsStep(
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
				t.Error("promotion did not replace the hashed browser token")
			}
			hashes = append(hashes, hash)
			wantArg(t, args, 2, testTime)
		}
		steps = append(steps, current, replace, insert)
	}
	steps = append(steps, sqlStep{kind: "commit"})
	_, store, _ := newSQLStub(t, steps...)
	service := newAccess(t, store, nil)
	if err := service.UpdateUser(context.Background(), 7, 7, "Employee", true, true, ""); err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 2 || bytes.Equal(hashes[0], hashes[1]) {
		t.Fatal("active browser sessions did not receive distinct replacements")
	}
}

func TestPromotionRotationFailureRollsBackUserChange(t *testing.T) {
	failure := errors.New("token storage unavailable")
	old := sha256.Sum256([]byte(testToken))
	insert := execStep("INSERT INTO session_tokens")
	insert.err = failure
	steps := append(transactionSteps(true), userStep(false, true), execStep("UPDATE users SET"), activeSessionsStep([]driver.Value{testSession, int64(7), testTime.Add(time.Minute), testTime.Add(time.Hour), false}), scalarStep("SELECT token_hash FROM session_tokens", old[:]), execStep("UPDATE session_tokens SET"), insert, sqlStep{kind: "rollback"})
	_, store, _ := newSQLStub(t, steps...)
	service := newAccess(t, store, nil)
	wantError(t, service.UpdateUser(context.Background(), 7, 7, "Employee", true, true, ""), failure)
}

func TestGraceTokenReturnsExistingReplacementWithoutRotatingAgain(t *testing.T) {
	old := sha256.Sum256([]byte(testToken))
	var stored []byte
	insert := execStep("INSERT INTO session_tokens")
	insert.check = func(args []driver.NamedValue) { stored = append([]byte(nil), hashArgument(t, args[1].Value)...) }
	steps := append(transactionSteps(true), userStep(false, true), execStep("UPDATE users SET"), activeSessionsStep([]driver.Value{testSession, int64(7), testTime.Add(time.Minute), testTime.Add(time.Hour), false}), scalarStep("SELECT token_hash FROM session_tokens", old[:]), execStep("UPDATE session_tokens SET"), insert, sqlStep{kind: "commit"})
	_, store, stub := newSQLStub(t, steps...)
	service := newAccess(t, store, nil)
	if err := service.UpdateUser(context.Background(), 7, 7, "Employee", true, true, ""); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		scoped := queryStep("WHERE session_id=$1 AND token_hash=$2", []string{"issued", "replaced", "grace"}, []driver.Value{testTime, nil, nil})
		scoped.check = func(args []driver.NamedValue) { wantArg(t, args, 0, testSession); wantArg(t, args, 1, stored) }
		stub.append(sqlStep{kind: "begin"}, queryStep("FOR UPDATE OF s", []string{"id", "user", "idle", "absolute", "revoked"}, []driver.Value{testSession, int64(7), testTime.Add(time.Minute), testTime.Add(time.Hour), false}), queryStep("SELECT issued_at,replaced_at,grace_expires_at", []string{"issued", "replaced", "grace"}, []driver.Value{testTime.Add(-time.Minute), testTime, testTime.Add(30 * time.Second)}), userStep(true, true), scoped, execStep("UPDATE sessions SET last_seen_at="), execStep("DELETE FROM session_tokens"), sqlStep{kind: "commit"})
		identity, replacement, err := service.Authenticate(context.Background(), testToken)
		if err != nil {
			t.Fatal(err)
		}
		if !identity.User.IsAdmin || len(replacement) != 43 || replacement == testToken {
			t.Fatal("promoted user did not receive the replacement token")
		}
		hash := sha256.Sum256([]byte(replacement))
		if !bytes.Equal(hash[:], stored) {
			t.Fatal("grace delivery issued a different token than the promotion transaction")
		}
	}
}
func TestDemotionAndPasswordResetStillRevokeSessions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		admin    bool
		password string
	}{{"demotion", false, ""}, {"promotion with password reset", true, testPassword}} {
		t.Run(tc.name, func(t *testing.T) {
			steps := append(transactionSteps(true), userStep(tc.password == "", true))
			if !tc.admin {
				steps = append(steps, scalarStep("SELECT count(*) FROM users", int64(2)))
			}
			steps = append(steps, execStep("UPDATE users SET"), execStep("UPDATE sessions SET revoked_at="), sqlStep{kind: "commit"})
			_, store, _ := newSQLStub(t, steps...)
			service := newAccess(t, store, nil)
			if err := service.UpdateUser(context.Background(), 7, 7, "Employee", tc.admin, true, tc.password); err != nil {
				t.Fatal(err)
			}
		})
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
