package unit_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"
)

func TestPermissionMatrix(t *testing.T) {
	for _, granted := range []access.Permission{access.NoAccess, access.ViewOnly, access.ViewEdit, access.Permission("unknown")} {
		for _, required := range []access.Permission{access.NoAccess, access.ViewOnly, access.ViewEdit, access.Permission("unknown")} {
			want := required == access.ViewOnly && (granted == access.ViewOnly || granted == access.ViewEdit) || required == access.ViewEdit && granted == access.ViewEdit
			if got := granted.Allows(required); got != want {
				t.Errorf("permission %q allows %q = %v; want %v", granted, required, got, want)
			}
		}
	}
}
func TestBackendPermissionEnforcement(t *testing.T) {
	_, store, stub := newSQLStub(t)
	service := newAccess(t, store, nil)
	for _, tc := range []struct {
		granted, required access.Permission
		allowed           bool
	}{
		{access.NoAccess, access.ViewOnly, false}, {access.NoAccess, access.ViewEdit, false},
		{access.ViewOnly, access.ViewOnly, true}, {access.ViewOnly, access.ViewEdit, false},
		{access.ViewEdit, access.ViewOnly, true}, {access.ViewEdit, access.ViewEdit, true},
	} {
		stub.append(permissionStep(tc.granted))
		err := service.RequirePermission(context.Background(), 7, 3, tc.required)
		if tc.allowed {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			wantError(t, err, access.ErrForbidden)
		}
	}
	for _, ids := range [][2]int64{{0, 3}, {7, 0}, {-1, 3}} {
		_, err := service.EffectivePermission(context.Background(), ids[0], ids[1])
		wantError(t, err, access.ErrInvalid)
	}
}
func TestAdminRoleEnforcement(t *testing.T) {
	_, store, stub := newSQLStub(t)
	service := newAccess(t, store, nil)
	wantError(t, service.RequireAdmin(context.Background(), 0), access.ErrForbidden)
	for _, allowed := range []bool{false, true} {
		stub.append(scalarStep("is_platform_admin", allowed))
		err := service.RequireAdmin(context.Background(), 7)
		if allowed {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			wantError(t, err, access.ErrForbidden)
		}
	}
	failure := errors.New("admin storage unavailable")
	stub.append(sqlStep{kind: "query", query: "is_platform_admin", err: failure})
	wantError(t, service.RequireAdmin(context.Background(), 7), failure)
}
func TestAccessibleDatabasesExposeOnlyPublicConnectionData(t *testing.T) {
	columns := []string{"id", "name", "dbms", "permission"}
	step := queryStep("FROM managed_databases", columns, []driver.Value{int64(3), "Analytics", "postgresql", "view_only"})
	_, store, _ := newSQLStub(t, step)
	service := newAccess(t, store, nil)
	databases, err := service.AccessibleDatabases(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(databases) != 1 || databases[0].Permission != dao.ViewOnly || databases[0].Name != "Analytics" || databases[0].Host != "" || databases[0].DatabaseName != "" {
		t.Fatalf("incorrect accessible databases: %#v", databases)
	}
	_, err = service.AccessibleDatabases(context.Background(), 0)
	wantError(t, err, access.ErrUnauthenticated)
}
