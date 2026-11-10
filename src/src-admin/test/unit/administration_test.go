package unit_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/connectionconfig"
)

func TestOrdinaryAccountAndGroupChangesRequireActiveAdministrator(t *testing.T) {
	_, store, stub := newSQLStub(t)
	auth := newAccess(t, store, nil)
	calls := []func() error{
		func() error { return auth.CreateUser(context.Background(), 7, "employee", "Employee", testPassword) },
		func() error { return auth.UpdateUser(context.Background(), 7, 8, "Employee", true, "") },
		func() error { return auth.SaveGroup(context.Background(), 7, 0, "Engineering", "") },
		func() error { return auth.DeleteGroup(context.Background(), 7, 2) },
		func() error { return auth.SetMembership(context.Background(), 7, 8, 2, true) },
		func() error { return auth.SetGrant(context.Background(), 7, 2, 3, access.ViewEdit) },
	}
	for _, call := range calls {
		stub.append(transactionSteps(false)...)
		stub.append(sqlStep{kind: "rollback"})
		wantError(t, call(), access.ErrForbidden)
	}
}
func TestAccountAndPermissionInputsAreValidated(t *testing.T) {
	_, store, _ := newSQLStub(t)
	auth := newAccess(t, store, nil)
	for _, name := range []string{"", "a", "spaces forbidden", strings.Repeat("a", 101)} {
		wantError(t, auth.CreateUser(context.Background(), 7, name, "Employee", testPassword), access.ErrInvalid)
	}
	for _, name := range []string{"", " ", strings.Repeat("a", 151)} {
		wantError(t, auth.UpdateUser(context.Background(), 7, 8, name, true, ""), access.ErrInvalid)
	}
	wantError(t, auth.SaveGroup(context.Background(), 7, -1, "Engineering", ""), access.ErrInvalid)
	wantError(t, auth.SetMembership(context.Background(), 7, 0, 2, true), access.ErrInvalid)
	wantError(t, auth.SetGrant(context.Background(), 7, 2, 3, access.Permission("admin")), access.ErrInvalid)
	wantError(t, auth.SetGrants(context.Background(), 7, 2, []access.GrantUpdate{{DatabaseID: 3, Permission: access.ViewEdit}, {DatabaseID: 3, Permission: access.ViewOnly}}), access.ErrInvalid)
}
func TestPermissionReductionInvalidatesAuthorityVersionsInSameTransaction(t *testing.T) {
	steps := append(transactionSteps(true), scalarStep("SELECT COALESCE((SELECT access_level", string(access.ViewEdit)), scalarStep("CROSS JOIN managed_databases", true), execStep("INSERT INTO group_database_permissions"), execStep("UPDATE users SET auth_version=auth_version+1"), sqlStep{kind: "commit"})
	_, store, _ := newSQLStub(t, steps...)
	wantError(t, newAccess(t, store, nil).SetGrant(context.Background(), 7, 2, 3, access.ViewOnly), nil)
}
func TestFailedSecurityVersionUpdateRollsBackMembership(t *testing.T) {
	failure := errors.New("authority storage failure")
	fail := execStep("UPDATE users SET auth_version=")
	fail.err = failure
	steps := append(transactionSteps(true), scalarStep("CROSS JOIN access_groups", true), execStep("DELETE FROM user_groups"), fail, sqlStep{kind: "rollback"})
	_, store, _ := newSQLStub(t, steps...)
	wantError(t, newAccess(t, store, nil).SetMembership(context.Background(), 7, 8, 2, false), failure)
}
func TestLastAdministratorRemainsActive(t *testing.T) {
	steps := append(transactionSteps(true), userStep(true, true), scalarStep("count(*) FROM admin_users", int64(1)), sqlStep{kind: "rollback"})
	_, store, _ := newSQLStub(t, steps...)
	wantError(t, newAccess(t, store, nil).UpdateAdmin(context.Background(), 7, 7, "Admin", false, ""), access.ErrInvalid)
}
func TestAdministratorPasswordResetRevokesOnlyAdminSessions(t *testing.T) {
	steps := append(transactionSteps(true), userStep(true, true), execStep("UPDATE admin_users SET"), execStep("UPDATE admin_sessions SET revoked_at="), sqlStep{kind: "commit"})
	_, store, _ := newSQLStub(t, steps...)
	wantError(t, newAccess(t, store, nil).UpdateAdmin(context.Background(), 7, 7, "Admin", true, testPassword), nil)
}
func TestRegistrationSavesEncryptedMetadataWithoutManagedDatabaseConnection(t *testing.T) {
	steps := []sqlStep{scalarStep("admin_users", true)}
	steps = append(steps, transactionSteps(true)...)
	insert := scalarStep("INSERT INTO managed_databases", int64(3))
	insert.check = func(args []driver.NamedValue) {
		cipher := args[6].Value.([]byte)
		if len(cipher) <= len("secret") || string(cipher) == "secret" {
			t.Fatal("plaintext connection password stored")
		}
	}
	steps = append(steps, queryStep("FROM managed_databases ORDER BY id", []string{"id"}), insert, sqlStep{kind: "commit"})
	_, store, _ := newSQLStub(t, steps...)
	auth := newAccess(t, store, nil)
	service, err := connectionconfig.NewService(store, auth, connectionconfig.DefaultConfig(testKey, false))
	if err != nil {
		t.Fatal(err)
	}
	// A nonexistent host proves registration has no connect/ping/query path.
	_, err = service.SaveConnection(context.Background(), 7, 0, connectionconfig.ConnectionInput{DBMS: "postgresql", Name: "Offline registration", Host: "does-not-exist.invalid", DatabaseName: "target", Username: "managed", Password: "secret", SSLMode: "verify-full", Active: true})
	if err != nil {
		t.Fatal(err)
	}
}
