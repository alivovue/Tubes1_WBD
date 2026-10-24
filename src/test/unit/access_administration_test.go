package unit_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"tubes1_wbd/backend/access"
)

func TestAdminCreatesHashedEmployee(t *testing.T) {
	insert := execStep("INSERT INTO users(")
	insert.check = func(args []driver.NamedValue) {
		wantArg(t, args, 0, "new.employee")
		wantArg(t, args, 1, "New Employee")
		wantArg(t, args, 3, false)
		wantArg(t, args, 4, int64(7))
		hash, ok := args[2].Value.(string)
		if !ok || bcrypt.CompareHashAndPassword([]byte(hash), []byte(testPassword)) != nil {
			t.Error("employee password was not hashed")
		}
	}
	steps := append(transactionSteps(true), insert, sqlStep{kind: "commit"})
	_, store, _ := newSQLStub(t, steps...)
	service := newAccess(t, store, nil)
	if err := service.CreateUser(context.Background(), 7, "new.employee", "  New Employee  ", testPassword, false); err != nil {
		t.Fatal(err)
	}
}
func TestAdminChangesRejectOrdinaryEmployee(t *testing.T) {
	_, store, stub := newSQLStub(t)
	service := newAccess(t, store, nil)
	calls := []struct {
		name string
		call func() error
	}{
		{"create employee", func() error {
			return service.CreateUser(context.Background(), 7, "employee.two", "Employee Two", testPassword, false)
		}},
		{"update employee", func() error { return service.UpdateUser(context.Background(), 7, 8, "Employee", false, true, "") }},
		{"save group", func() error { return service.SaveGroup(context.Background(), 7, 0, "Engineering", "") }},
		{"delete group", func() error { return service.DeleteGroup(context.Background(), 7, 2) }},
		{"membership", func() error { return service.SetMembership(context.Background(), 7, 8, 2, true) }},
		{"grant", func() error { return service.SetGrant(context.Background(), 7, 2, 3, access.ViewEdit) }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			stub.append(append(transactionSteps(false), sqlStep{kind: "rollback"})...)
			wantError(t, tc.call(), access.ErrForbidden)
		})
	}
}
func TestAdminInputValidationRunsBeforeStorage(t *testing.T) {
	_, store, _ := newSQLStub(t)
	service := newAccess(t, store, nil)
	for _, username := range []string{"", "ab", "bad username", "_starts_wrong", strings.Repeat("x", 101)} {
		wantError(t, service.CreateUser(context.Background(), 7, username, "Employee", testPassword, false), access.ErrInvalid)
	}
	for _, display := range []string{"", "  ", strings.Repeat("x", 151)} {
		wantError(t, service.CreateUser(context.Background(), 7, "employee", display, testPassword, false), access.ErrInvalid)
	}
	wantError(t, service.SaveGroup(context.Background(), 7, -1, "Engineering", ""), access.ErrInvalid)
	wantError(t, service.SaveGroup(context.Background(), 7, 0, "  ", ""), access.ErrInvalid)
	wantError(t, service.SaveGroup(context.Background(), 7, 0, "Engineering", strings.Repeat("x", 2001)), access.ErrInvalid)
	wantError(t, service.DeleteGroup(context.Background(), 7, 0), access.ErrInvalid)
	wantError(t, service.SetMembership(context.Background(), 7, 0, 2, true), access.ErrInvalid)
	wantError(t, service.SetGrant(context.Background(), 7, 2, 3, access.Permission("administrator")), access.ErrInvalid)
}
func TestLastActiveAdminCannotBeDemotedOrDisabled(t *testing.T) {
	for _, tc := range []struct {
		name          string
		admin, active bool
	}{{"demotion", false, true}, {"disable", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			steps := append(transactionSteps(true), userStep(true, true), scalarStep("SELECT count(*) FROM users", int64(1)), sqlStep{kind: "rollback"})
			_, store, _ := newSQLStub(t, steps...)
			service := newAccess(t, store, nil)
			wantError(t, service.UpdateUser(context.Background(), 7, 7, "Admin", tc.admin, tc.active, ""), access.ErrInvalid)
		})
	}
}
func TestUserSecurityChangesRevokeSessions(t *testing.T) {
	for _, tc := range []struct {
		name          string
		admin, active bool
		password      string
		revoke        bool
	}{
		{"display change only", false, true, "", false}, {"role increase", true, true, "", true}, {"account disable", false, false, "", true}, {"password change", false, true, testPassword, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			update := execStep("UPDATE users SET display_name=")
			update.check = func(args []driver.NamedValue) {
				wantArg(t, args, 2, tc.admin)
				wantArg(t, args, 3, tc.active)
				if tc.password != "" {
					if bcrypt.CompareHashAndPassword([]byte(args[4].Value.(string)), []byte(tc.password)) != nil {
						t.Error("replacement password was not hashed")
					}
				} else {
					wantArg(t, args, 4, "")
				}
			}
			steps := append(transactionSteps(true), userStep(false, true), update)
			if tc.revoke {
				steps = append(steps, execStep("UPDATE sessions SET revoked_at="))
			}
			steps = append(steps, sqlStep{kind: "commit"})
			_, store, _ := newSQLStub(t, steps...)
			service := newAccess(t, store, nil)
			if err := service.UpdateUser(context.Background(), 7, 7, "Employee", tc.admin, tc.active, tc.password); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestMembershipChangesRevokeAffectedSessions(t *testing.T) {
	for _, member := range []bool{true, false} {
		t.Run(debugBool(member), func(t *testing.T) {
			mutation := execStep("DELETE FROM user_groups")
			if member {
				mutation = execStep("INSERT INTO user_groups")
			}
			steps := append(transactionSteps(true), scalarStep("CROSS JOIN access_groups", true), mutation, execStep("user_id=$1"), sqlStep{kind: "commit"})
			_, store, _ := newSQLStub(t, steps...)
			service := newAccess(t, store, nil)
			if err := service.SetMembership(context.Background(), 7, 8, 2, member); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestGroupGrantChangesRevokeAffectedSessions(t *testing.T) {
	for _, permission := range []access.Permission{access.NoAccess, access.ViewOnly, access.ViewEdit} {
		t.Run(string(permission), func(t *testing.T) {
			mutation := execStep("INSERT INTO group_database_permissions")
			if permission == access.NoAccess {
				mutation = execStep("DELETE FROM group_database_permissions")
			}
			steps := append(transactionSteps(true), scalarStep("CROSS JOIN managed_databases", true), mutation, execStep("SELECT user_id FROM user_groups WHERE group_id="), sqlStep{kind: "commit"})
			_, store, _ := newSQLStub(t, steps...)
			service := newAccess(t, store, nil)
			if err := service.SetGrant(context.Background(), 7, 2, 3, permission); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestBootstrapOnlyOnEmptyPlatform(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(debugBool(exists), func(t *testing.T) {
			steps := []sqlStep{{kind: "begin"}, execStep("pg_advisory_xact_lock"), scalarStep("SELECT EXISTS(SELECT 1 FROM users)", exists)}
			if exists {
				steps = append(steps, sqlStep{kind: "rollback"})
			} else {
				insert := execStep("INSERT INTO users(")
				insert.check = func(args []driver.NamedValue) { wantArg(t, args, 3, true); wantArg(t, args, 4, nil) }
				steps = append(steps, insert, sqlStep{kind: "commit"})
			}
			_, store, _ := newSQLStub(t, steps...)
			service := newAccess(t, store, nil)
			err := service.BootstrapAdmin(context.Background(), "initial.admin", "Initial Admin", testPassword)
			if exists {
				wantError(t, err, access.ErrConflict)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func debugBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestAdminGroupCreationUpdateAndDeletion(t *testing.T) {
	for _, id := range []int64{0, 2} {
		t.Run(debugBool(id > 0), func(t *testing.T) {
			statement := "INSERT INTO access_groups"
			if id > 0 {
				statement = "UPDATE access_groups"
			}
			steps := append(transactionSteps(true), execStep(statement), sqlStep{kind: "commit"})
			_, store, _ := newSQLStub(t, steps...)
			service := newAccess(t, store, nil)
			if err := service.SaveGroup(context.Background(), 7, id, " Engineering ", "Platform team"); err != nil {
				t.Fatal(err)
			}
		})
	}
	steps := append(transactionSteps(true), execStep("UPDATE sessions SET revoked_at="), execStep("DELETE FROM group_database_permissions"), execStep("DELETE FROM user_groups"), execStep("DELETE FROM access_groups"), sqlStep{kind: "commit"})
	_, store, _ := newSQLStub(t, steps...)
	service := newAccess(t, store, nil)
	if err := service.DeleteGroup(context.Background(), 7, 2); err != nil {
		t.Fatal(err)
	}
}
