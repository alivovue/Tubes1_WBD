package unit_test

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"
	"tubes1_wbd/backend/access"
)

func TestAuthorityVersionRevokesAnOtherwiseValidSession(t *testing.T) {
	remote := userStep(false, true)
	remote.rows[0][4] = int64(2)
	_, store, _ := newSQLStub(t, sqlStep{kind: "begin"}, queryStep("FOR UPDATE OF s", []string{"id", "user", "idle", "absolute", "revoked", "version", "rotation"}, []driver.Value{testSession, int64(7), testTime.Add(time.Minute), testTime.Add(time.Hour), false, int64(1), int64(1)}), queryStep("SELECT issued_at", []string{"issued", "replaced", "grace"}, []driver.Value{testTime, nil, nil}), remote, sqlStep{kind: "rollback"}, execStep("UPDATE sessions SET revoked_at="))
	_, _, err := newAccess(t, store, nil).Authenticate(context.Background(), testToken)
	wantError(t, err, access.ErrUnauthenticated)
}
func TestPrivilegeIncreaseRotatesOnNextAuthentication(t *testing.T) {
	remote := userStep(false, true)
	remote.rows[0][5] = int64(2)
	_, store, _ := newSQLStub(t, sqlStep{kind: "begin"}, queryStep("FOR UPDATE OF s", []string{"id", "user", "idle", "absolute", "revoked", "version", "rotation"}, []driver.Value{testSession, int64(7), testTime.Add(time.Minute), testTime.Add(time.Hour), false, int64(1), int64(1)}), queryStep("SELECT issued_at", []string{"issued", "replaced", "grace"}, []driver.Value{testTime, nil, nil}), remote, execStep("UPDATE session_tokens SET"), execStep("INSERT INTO session_tokens"), execStep("UPDATE sessions SET rotation_version="), execStep("UPDATE sessions SET last_seen_at="), execStep("DELETE FROM session_tokens"), sqlStep{kind: "commit"})
	identity, replacement, err := newAccess(t, store, nil).Authenticate(context.Background(), testToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(replacement) != 43 || !identity.AbsoluteExpires.Equal(testTime.Add(time.Hour)) {
		t.Fatal("privilege-change rotation lost session lifetime")
	}
}
