package integration_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/controlplane"
	runtimeSetup "tubes1_wbd/database/setup"
	"tubes1_wbd/test/data/setup"
	"tubes1_wbd_admin/backend/connectionconfig"
)

// Execute the exact operator SQL documented in Markdown against isolated
// databases, including its interruption/retry and conflicting-row guards.
func TestDocumentedControlPlaneCutoverPreservesDataAndRecovers(t *testing.T) {
	f := newFixture(t, false)
	_, err := f.dbs.Website.ExecContext(f.ctx, `DROP SCHEMA public CASCADE;CREATE SCHEMA public`)
	must(t, err)
	must(t, setup.Initialize(f.ctx, f.dbs.Website))
	must(t, bootstrapAdmin(f.ctx, f.adminStore, "old.admin", "Old administrator", password))
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	must(t, err)
	input := f.dbs.Connection()
	cipher, err := connectionconfig.EncryptPassword(bytes.Repeat([]byte{21}, 32), input.Password)
	must(t, err)
	seedSQL := `INSERT INTO users(id,username,display_name,password_hash,is_active,is_legacy_admin)VALUES(1000,'old.admin','Historical administrator','',FALSE,TRUE),(30,'old.employee','Preserved employee',$1,TRUE,FALSE);
 UPDATE users SET created_by_admin_id=1000,legacy_created_by_user_id=1000 WHERE id=30;
 INSERT INTO access_groups(id,name,description)VALUES(7,'Preserved group','Preserved description');
 INSERT INTO user_groups(user_id,group_id)VALUES(30,7);
 INSERT INTO managed_databases(id,display_name,dbms_type,host,port,database_name,db_username,encrypted_password,connection_options,created_by_admin_id,legacy_created_by_user_id)VALUES(5,'Preserved target','postgresql',$2,$3,$4,$5,$6,'{"sslmode":"disable","migration_namespace":"preserved-receipt-namespace"}',1000,1000);
 INSERT INTO group_database_permissions(group_id,database_id,access_level,granted_by_admin_id,legacy_granted_by_user_id)VALUES(7,5,'view_edit',1000,1000);
 INSERT INTO migration_drafts(id,database_id,created_by,title,input_mode,sql_text)VALUES(8,5,1000,'Historical draft','sql','SELECT 1');
 INSERT INTO migration_executions(id,database_id,draft_id,executed_by,sql_text,attempted_version,applied_version,status)VALUES(9,5,8,1000,'SELECT 1',1,1,'succeeded');
 INSERT INTO sessions(id,user_id,idle_expires_at,absolute_expires_at)VALUES('00000000-0000-0000-0000-000000000030',30,now()+interval '30 minutes',now()+interval '8 hours'),('00000000-0000-0000-0000-000000001000',1000,now()+interval '30 minutes',now()+interval '8 hours')`
	for _, statement := range strings.Split(seedSQL, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		switch {
		case strings.Contains(statement, "INSERT INTO users("):
			_, err = f.dbs.Website.ExecContext(f.ctx, statement, string(hash))
		case strings.Contains(statement, "INSERT INTO managed_databases("):
			statement = strings.NewReplacer("$2", "$1", "$3", "$2", "$4", "$3", "$5", "$4", "$6", "$5").Replace(statement)
			_, err = f.dbs.Website.ExecContext(f.ctx, statement, input.Host, input.Port, input.DatabaseName, input.Username, cipher)
		default:
			_, err = f.dbs.Website.ExecContext(f.ctx, statement)
		}
		must(t, err)
	}
	ordinaryToken := strings.Repeat("o", 43)
	sum := sha256.Sum256([]byte(ordinaryToken))
	_, err = f.dbs.Website.ExecContext(f.ctx, `INSERT INTO session_tokens(session_id,token_hash)VALUES('00000000-0000-0000-0000-000000000030',$1)`, sum[:])
	must(t, err)
	document, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs", "control-plane-cutover.md"))
	must(t, err)
	var blocks []string
	for _, part := range strings.Split(string(document), "```sql\n")[1:] {
		blocks = append(blocks, strings.SplitN(part, "```", 2)[0])
	}
	if len(blocks) != 3 {
		t.Fatalf("expected three executable operator SQL blocks, got %d", len(blocks))
	}
	directory := t.TempDir()
	for i := range blocks {
		blocks[i] = strings.ReplaceAll(blocks[i], "/secure/cutover", filepath.ToSlash(directory))
	}
	psql, err := exec.LookPath("psql")
	must(t, err)
	run := func(database, sql string) error {
		command := exec.CommandContext(f.ctx, psql, "--no-psqlrc", "--set=ON_ERROR_STOP=1", "--dbname="+factory.Cluster.DSN(database))
		command.Stdin = strings.NewReader(sql)
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("operator block failed: %w: %s", err, output)
		}
		return nil
	}
	must(t, run(f.dbs.WebsiteName, blocks[0]))
	// A failed destination transaction leaves no authoritative transfer behind.
	_, err = f.adminDB.ExecContext(f.ctx, `CREATE FUNCTION reject_transfer() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'injected import failure';END$$;CREATE TRIGGER reject_transfer BEFORE INSERT ON users FOR EACH ROW EXECUTE FUNCTION reject_transfer()`)
	must(t, err)
	if run(f.dbs.WebsiteName+"_admin", blocks[1]) == nil {
		t.Fatal("destination import ignored failure")
	}
	var count int
	must(t, f.adminDB.QueryRowContext(f.ctx, `SELECT count(*) FROM users`).Scan(&count))
	if count != 0 {
		t.Fatal("failed import partially committed")
	}
	_, err = f.adminDB.ExecContext(f.ctx, `DROP TRIGGER reject_transfer ON users;DROP FUNCTION reject_transfer()`)
	must(t, err)
	must(t, run(f.dbs.WebsiteName+"_admin", blocks[1]))
	must(t, run(f.dbs.WebsiteName+"_admin", blocks[1]))
	// Ciphertext/key compatibility is checked before deleting source authority.
	m, err := f.authority.Connection(f.ctx, 30, 5, access.ViewEdit)
	must(t, err)
	if m.Password != input.Password {
		t.Fatal("transferred connection cannot be decrypted with existing key")
	}
	_, err = f.dbs.Website.ExecContext(f.ctx, `CREATE FUNCTION reject_cutover() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'injected source failure';END$$;CREATE TRIGGER reject_cutover BEFORE UPDATE ON managed_databases FOR EACH ROW EXECUTE FUNCTION reject_cutover()`)
	must(t, err)
	if run(f.dbs.WebsiteName, blocks[2]) == nil {
		t.Fatal("source cutover ignored failure")
	}
	var sourceIntact bool
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*)=1 FROM users WHERE password_hash<>''`).Scan(&sourceIntact))
	if !sourceIntact {
		t.Fatal("interrupted cutover removed source credentials")
	}
	_, err = f.dbs.Website.ExecContext(f.ctx, `DROP TRIGGER reject_cutover ON managed_databases;DROP FUNCTION reject_cutover()`)
	must(t, err)
	must(t, run(f.dbs.WebsiteName, blocks[2]))
	must(t, run(f.dbs.WebsiteName, blocks[2]))
	must(t, runtimeSetup.ValidateSchema(f.ctx, f.dbs.Website))
	identity, _, err := f.userAccess.Authenticate(f.ctx, ordinaryToken)
	must(t, err)
	if identity.User.ID != 30 {
		t.Fatal("ordinary session identity changed")
	}
	_, _, err = f.userAccess.Login(f.ctx, "old.admin", password, "")
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = f.userAccess.Login(f.ctx, "old.employee", password, "")
	must(t, err)
	history, err := f.runtimeStore.MigrationHistory(f.ctx, 5, 50, 0)
	must(t, err)
	if len(history) != 1 || history[0].ExecutedBy != 1000 || history[0].Username != "old.admin" {
		t.Fatal("historical attribution changed")
	}
	var namespace string
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT connection_options->>'migration_namespace' FROM managed_databases WHERE id=5`).Scan(&namespace))
	if namespace != "preserved-receipt-namespace" {
		t.Fatal("receipt namespace changed")
	}

	// Identity allocation must reserve IDs of historical administrator tombstones,
	// even though those accounts were excluded from ordinary authority storage.
	must(t, f.adminAuth.CreateUser(f.ctx, 1000, "after.cutover", "New ordinary account", password))
	newIdentity, _, err := f.userAccess.Login(f.ctx, "after.cutover", password, "")
	must(t, err)
	if newIdentity.User.ID <= 1000 {
		t.Fatal("ordinary account reused a historical actor ID")
	}
	var historicalName string
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT username FROM users WHERE id=1000`).Scan(&historicalName))
	if historicalName != "old.admin" {
		t.Fatal("new user rewrote historical attribution")
	}
	// Rerunning old exports cannot overwrite later administrative edits.
	_, err = f.adminDB.ExecContext(f.ctx, `UPDATE users SET display_name='Later edit' WHERE id=30`)
	must(t, err)
	if run(f.dbs.WebsiteName+"_admin", blocks[1]) == nil {
		t.Fatal("old export overwrote a later authority edit")
	}
}
func TestControlPlaneOutageFailsClosedWithoutStalePermissions(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewEdit)
	_, raw := f.login(t, "it.editor")
	_, err := f.databases.Schemas(f.ctx, f.editor, target.ID)
	must(t, err)
	f.private.Close()
	_, _, err = f.userAccess.Authenticate(f.ctx, raw)
	wantError(t, err, controlplane.ErrUnavailable)
	_, err = f.databases.Schemas(f.ctx, f.editor, target.ID)
	wantError(t, err, controlplane.ErrUnavailable)
	response := apiRequest(t, userHandler(t, f), "GET", "/dashboard", raw, nil)
	if response.Code != 503 {
		t.Fatalf("authority outage returned %d", response.Code)
	}
}
