package integration_test

import (
	"testing"

	"tubes1_wbd/backend/access"
)

func TestPermissionsComeFromGroupsAndHighestGrantWins(t *testing.T) {
	f := newFixture(t, true)
	user := f.user(t, "it.employee")
	target := f.register(t)
	level, err := f.access.EffectivePermission(f.ctx, f.admin, target.ID)
	must(t, err)
	if level != access.NoAccess {
		t.Fatal("platform admin gained implicit database permission")
	}
	readers := f.group(t, "IT Readers")
	editors := f.group(t, "IT Editors")
	must(t, f.access.SetMembership(f.ctx, f.admin, user.ID, readers.ID, true))
	must(t, f.access.SetMembership(f.ctx, f.admin, user.ID, editors.ID, true))
	must(t, f.access.SetGrant(f.ctx, f.admin, readers.ID, target.ID, access.ViewOnly))
	must(t, f.access.SetGrant(f.ctx, f.admin, editors.ID, target.ID, access.ViewEdit))
	level, err = f.access.EffectivePermission(f.ctx, user.ID, target.ID)
	must(t, err)
	if level != access.ViewEdit {
		t.Fatal("highest group permission did not win")
	}
	must(t, f.access.RequirePermission(f.ctx, user.ID, target.ID, access.ViewEdit))
	must(t, f.access.SetGrant(f.ctx, f.admin, editors.ID, target.ID, access.NoAccess))
	level, err = f.access.EffectivePermission(f.ctx, user.ID, target.ID)
	must(t, err)
	if level != access.ViewOnly {
		t.Fatal("remaining reader group was not respected")
	}
}
func TestViewOnlyUserCannotAnalyzeOrSaveMigration(t *testing.T) {
	f := newFixture(t, true)
	user := f.user(t, "it.reader")
	target := f.register(t)
	f.grant(t, user.ID, target.ID, access.ViewOnly)
	f.grant(t, f.editor, target.ID, access.ViewEdit)
	sql := "CREATE TABLE public.forbidden_change(id INTEGER)"
	_, err := f.migrations.Analyze(f.ctx, f.editor, target.ID, sql)
	must(t, err)
	before, err := f.databases.Snapshot(f.ctx, user.ID, target.ID)
	must(t, err)
	_, err = f.migrations.Analyze(f.ctx, user.ID, target.ID, sql)
	wantError(t, err, access.ErrForbidden)
	_, err = f.migrations.Workflow().CreateDefinition(f.ctx, user.ID, target.ID, reversible("forbidden_change"))
	wantError(t, err, access.ErrForbidden)
	if f.tableExists(t, "forbidden_change") {
		t.Fatal("denied migration changed the schema")
	}
	after, err := f.databases.Snapshot(f.ctx, user.ID, target.ID)
	must(t, err)
	if before.Fingerprint != after.Fingerprint {
		t.Fatal("denied operation changed the schema fingerprint")
	}
	history, err := f.migrations.Workflow().History(f.ctx, user.ID, target.ID, false, 50, 0)
	must(t, err)
	if len(history) != 0 {
		t.Fatal("denied operation created migration history")
	}
}
func TestRevokingGroupGrantInvalidatesAccessAndSessions(t *testing.T) {
	f := newFixture(t, true)
	user := f.user(t, "it.employee")
	target := f.register(t)
	group := f.grant(t, user.ID, target.ID, access.ViewEdit)
	identity, raw := f.login(t, user.Username)
	_, _, err := f.access.Authenticate(f.ctx, raw)
	must(t, err)
	must(t, f.access.SetGrant(f.ctx, f.admin, group.ID, target.ID, access.NoAccess))
	level, err := f.access.EffectivePermission(f.ctx, user.ID, target.ID)
	must(t, err)
	if level != access.NoAccess {
		t.Fatal("revoked group grant remains effective")
	}
	wantError(t, f.access.RequirePermission(f.ctx, user.ID, target.ID, access.ViewOnly), access.ErrForbidden)
	_, _, err = f.access.Authenticate(f.ctx, raw)
	wantError(t, err, access.ErrUnauthenticated)
	var revoked bool
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1`, identity.SessionID).Scan(&revoked))
	if !revoked {
		t.Fatal("grant revocation did not persist session invalidation")
	}
	accessible, err := f.access.AccessibleDatabases(f.ctx, user.ID)
	must(t, err)
	if len(accessible) != 0 {
		t.Fatal("revoked database remains listed")
	}
}
