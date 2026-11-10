package integration_test

import (
	"testing"

	"golang.org/x/crypto/bcrypt"

	"tubes1_wbd/backend/access"
)

func TestIndependentCredentialsAndTokensWithMatchingIDsAndUsernames(t *testing.T) {
	f := newFixture(t, true)
	hash, err := bcrypt.GenerateFromPassword([]byte("Independent-admin-password-123"), bcrypt.MinCost)
	must(t, err)
	_, err = f.adminDB.ExecContext(f.ctx, `INSERT INTO admin_users(id,username,display_name,password_hash)VALUES($1,'it.editor','Separate Admin',$2)`, f.editor, string(hash))
	must(t, err)
	adminIdentity, adminToken, err := f.adminAuth.Login(f.ctx, "it.editor", "Independent-admin-password-123", "")
	must(t, err)
	ordinary, userToken := f.login(t, "it.editor")
	if ordinary.User.ID != adminIdentity.User.ID {
		t.Fatal("test must use matching numeric IDs")
	}
	_, _, err = f.userAccess.Login(f.ctx, "it.editor", "Independent-admin-password-123", "")
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = f.adminAuth.Login(f.ctx, "it.editor", password, "")
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = f.userAccess.Authenticate(f.ctx, adminToken)
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = f.adminAuth.Authenticate(f.ctx, userToken)
	wantError(t, err, access.ErrUnauthenticated)
	must(t, f.adminAuth.UpdateAdmin(f.ctx, f.admin, f.editor, "Separate Admin", true, "Replacement-admin-password-123"))
	_, _, err = f.adminAuth.Authenticate(f.ctx, adminToken)
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = f.userAccess.Authenticate(f.ctx, userToken)
	must(t, err)
	var sessions int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, f.editor).Scan(&sessions))
	if sessions != 1 {
		t.Fatal("admin session leaked into platform sessions")
	}
}
