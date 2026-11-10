package integration_test

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"tubes1_wbd/backend/access"
	adminmigrate "tubes1_wbd/test/data/adminmigration"
	adminsetup "tubes1_wbd/test/data/adminsetup"
	"tubes1_wbd/test/data/setup"
)

func legacyInstallation(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, false)
	_, err := f.adminDB.ExecContext(f.ctx, `DROP SCHEMA public CASCADE;CREATE SCHEMA public`)
	must(t, err)
	must(t, adminsetup.Initialize(f.ctx, f.adminDB))
	schema, err := os.ReadFile(filepath.Join(repositoryRoot(t), "src", "test", "data", "adminmigration", "legacy_schema_test.sql"))
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`)
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, string(schema))
	must(t, err)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `INSERT INTO users(id,username,display_name,password_hash,is_platform_admin,is_active) VALUES(10,'legacy.admin','Legacy Admin',$1,TRUE,TRUE),(20,'disabled.admin','Disabled Admin',$1,TRUE,FALSE),(30,'legacy.employee','Legacy Employee',$1,FALSE,TRUE)`, string(hash))
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE users SET created_by=10 WHERE id=30;
 INSERT INTO access_groups(id,name)VALUES(7,'Preserved group');
 INSERT INTO user_groups(user_id,group_id)VALUES(10,7),(30,7);
 INSERT INTO managed_databases(id,display_name,host,database_name,db_username,encrypted_password,created_by)VALUES(5,'Preserved database','localhost','managed','managed',decode('01','hex'),10);
 INSERT INTO group_database_permissions(group_id,database_id,access_level,granted_by)VALUES(7,5,'view_edit',10);
 INSERT INTO migration_drafts(id,database_id,created_by,title,input_mode,sql_text)VALUES(8,5,10,'Historical admin draft','sql','SELECT 1');
 INSERT INTO migration_executions(id,database_id,draft_id,executed_by,sql_text,attempted_version,applied_version,status)VALUES(9,5,8,10,'SELECT 1',1,1,'succeeded');
 INSERT INTO sessions(id,user_id,idle_expires_at,absolute_expires_at)VALUES('00000000-0000-0000-0000-000000000010',10,now()+interval '30 minutes',now()+interval '8 hours'),('00000000-0000-0000-0000-000000000030',30,now()+interval '30 minutes',now()+interval '8 hours')`)
	must(t, err)
	for _, entry := range []struct{ session, token string }{{"00000000-0000-0000-0000-000000000010", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, {"00000000-0000-0000-0000-000000000030", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}} {
		hash := sha256.Sum256([]byte(entry.token))
		_, err = f.dbs.Website.ExecContext(f.ctx, `INSERT INTO session_tokens(session_id,token_hash)VALUES($1,$2)`, entry.session, hash[:])
		must(t, err)
	}
	return f
}
func assertMigratedInstallation(t *testing.T, f *fixture) {
	t.Helper()
	must(t, setup.EnsureInitialized(f.ctx, f.dbs.Website))
	for _, query := range []string{
		`SELECT count(*)=2 FROM users WHERE is_legacy_admin AND NOT is_active AND password_hash=''`,
		`SELECT count(*)=1 FROM user_groups WHERE user_id=30 AND group_id=7`,
		`SELECT count(*)=0 FROM user_groups WHERE user_id IN(10,20)`,
		`SELECT count(*)=1 FROM group_database_permissions WHERE granted_by_admin_id=10 AND legacy_granted_by_user_id=10 AND access_level='view_edit'`,
		`SELECT count(*)=1 FROM managed_databases WHERE created_by_admin_id=10 AND legacy_created_by_user_id=10`,
		`SELECT count(*)=1 FROM users WHERE id=30 AND created_by_admin_id=10 AND legacy_created_by_user_id=10`,
		`SELECT count(*)=1 FROM migration_drafts WHERE id=8 AND created_by=10 AND sql_text='SELECT 1'`,
		`SELECT count(*)=1 FROM migration_executions WHERE id=9 AND executed_by=10 AND status='succeeded' AND applied_version=1`,
		`SELECT revoked_at IS NOT NULL FROM sessions WHERE user_id=10`,
		`SELECT count(*)=0 FROM session_tokens t JOIN sessions s ON s.id=t.session_id WHERE s.user_id=10`,
	} {
		var valid bool
		must(t, f.dbs.Website.QueryRowContext(f.ctx, query).Scan(&valid))
		if !valid {
			t.Fatalf("migration lost state: %s", query)
		}
	}
	var count int
	must(t, f.adminDB.QueryRowContext(f.ctx, `SELECT count(*) FROM admin_users`).Scan(&count))
	if count != 2 {
		t.Fatalf("import duplicated administrators: %d", count)
	}
	for _, name := range []string{"users", "sessions", "user_groups", "access_groups", "managed_databases", "migration_executions"} {
		var exists bool
		must(t, f.adminDB.QueryRowContext(f.ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&exists))
		if exists {
			t.Fatalf("admin database contains platform table %s", name)
		}
	}
	identity, _, err := f.adminAuth.Login(f.ctx, "LEGACY.ADMIN", password, "")
	must(t, err)
	if identity.User.ID != 10 {
		t.Fatal("administrator ID changed")
	}
	var ordinaryValid bool
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT is_active AND password_hash<>'' FROM users WHERE id=30 AND NOT is_legacy_admin`).Scan(&ordinaryValid))
	if !ordinaryValid {
		t.Fatal("historical migration lost ordinary credentials")
	}

	_, _, err = f.adminAuth.Login(f.ctx, "disabled.admin", password, "")
	wantError(t, err, access.ErrUnauthenticated)
	if _, err := f.dbs.Website.ExecContext(f.ctx, `UPDATE users SET is_active=TRUE WHERE id=10`); err == nil {
		t.Fatal("legacy administrator can be reactivated")
	}
}
func TestAdminDatabaseMigrationRecoversFromPlatformCutoverFailure(t *testing.T) {
	f := legacyInstallation(t)
	_, err := f.dbs.Website.ExecContext(f.ctx, `CREATE FUNCTION interrupt_cutover() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'injected cutover failure';END$$;CREATE TRIGGER interrupt_cutover BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION interrupt_cutover()`)
	must(t, err)
	if _, err := adminmigrate.Run(f.ctx, f.dbs.Website, f.adminDB); err == nil {
		t.Fatal("injected cutover failure was ignored")
	}
	var sourceUntouched bool
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT is_active AND password_hash<>'' FROM users WHERE id=10`).Scan(&sourceUntouched))
	if !sourceUntouched {
		t.Fatal("failed cutover scrubbed source credentials")
	}
	_, _, err = f.adminAuth.Login(f.ctx, "legacy.admin", password, "")
	wantError(t, err, access.ErrUnauthenticated)
	_, err = f.dbs.Website.ExecContext(f.ctx, `DROP TRIGGER interrupt_cutover ON users; DROP FUNCTION interrupt_cutover()`)
	must(t, err)
	result, err := adminmigrate.Run(f.ctx, f.dbs.Website, f.adminDB)
	must(t, err)
	if result.Administrators != 2 {
		t.Fatal("not all admins migrated")
	}
	assertMigratedInstallation(t, f)
	must(t, f.adminAuth.UpdateAdmin(f.ctx, 10, 10, "Renamed Administrator", true, "New-independent-password-123"))
	result, err = adminmigrate.Run(f.ctx, f.dbs.Website, f.adminDB)
	must(t, err)
	if !result.AlreadyComplete {
		t.Fatal("rerun did not recognize committed migration")
	}
	_, _, err = f.adminAuth.Login(f.ctx, "legacy.admin", "New-independent-password-123", "")
	must(t, err)
	_, _, err = f.adminAuth.Login(f.ctx, "legacy.admin", password, "")
	wantError(t, err, access.ErrUnauthenticated)
}
func TestAdminDatabaseMigrationRecoversFromActivationFailure(t *testing.T) {
	f := legacyInstallation(t)
	_, err := f.adminDB.ExecContext(f.ctx, `CREATE FUNCTION interrupt_activation() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF OLD.migration_pending AND NOT NEW.migration_pending THEN RAISE EXCEPTION 'injected activation failure';END IF;RETURN NEW;END$$;CREATE TRIGGER interrupt_activation BEFORE UPDATE ON admin_users FOR EACH ROW EXECUTE FUNCTION interrupt_activation()`)
	must(t, err)
	if _, err := adminmigrate.Run(f.ctx, f.dbs.Website, f.adminDB); err == nil {
		t.Fatal("injected activation failure was ignored")
	}
	must(t, setup.EnsureInitialized(f.ctx, f.dbs.Website))
	_, _, err = f.adminAuth.Login(f.ctx, "legacy.admin", password, "")
	wantError(t, err, access.ErrUnauthenticated)
	_, err = f.adminDB.ExecContext(f.ctx, `DROP TRIGGER interrupt_activation ON admin_users;DROP FUNCTION interrupt_activation()`)
	must(t, err)
	result, err := adminmigrate.Run(f.ctx, f.dbs.Website, f.adminDB)
	must(t, err)
	if !result.AlreadyComplete {
		t.Fatal("did not recover committed source cutover")
	}
	assertMigratedInstallation(t, f)
}
func TestAdminDatabaseMigrationRejectsDestinationConflictBeforeCutover(t *testing.T) {
	f := legacyInstallation(t)
	_, err := f.adminDB.ExecContext(f.ctx, `INSERT INTO admin_users(id,username,display_name,password_hash)VALUES(10,'unrelated.admin','Unrelated Admin','unusable')`)
	must(t, err)
	if _, err := adminmigrate.Run(f.ctx, f.dbs.Website, f.adminDB); err == nil {
		t.Fatal("destination ID collision silently remapped historical actors")
	}
	var intact bool
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT is_platform_admin AND is_active AND password_hash<>'' FROM users WHERE id=10`).Scan(&intact))
	if !intact {
		t.Fatal("conflict changed source")
	}
	if err := setup.EnsureInitialized(f.ctx, f.dbs.Website); err != setup.ErrAdminMigrationRequired {
		t.Fatalf("legacy schema should require cutover: %v", err)
	}
}
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
