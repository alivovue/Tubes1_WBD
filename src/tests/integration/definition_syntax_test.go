package integration_test

import (
	"testing"

	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"
)

func TestDefinitionSyntaxRejectedBeforeReview(t *testing.T) {
	f := newWorkflowFixture(t)
	invalid := migration.DefinitionInput{Title: "Malformed definition", UpSQL: "CREATE TABLE invalid_syntax(id integer,,,);", DownSQL: "DROP TABLE invalid_syntax"}
	_, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, invalid)
	wantError(t, err, migration.ErrDefinitionInvalid)
	valid := migration.DefinitionInput{Title: "Dependent statements", UpSQL: "CREATE TABLE syntax_dependency(id integer); INSERT INTO syntax_dependency VALUES(1)", DownSQL: "DROP TABLE syntax_dependency"}
	revision, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, valid)
	must(t, err)
	valid.DownSQL = "DROP TABLE ;"
	_, err = f.workflow.EditDefinition(f.ctx, f.editor, revision.DefinitionID, revision.ID, valid)
	wantError(t, err, migration.ErrDefinitionInvalid)
	// Pre-existing malformed drafts must not bypass validation at submission.
	var old dao.MigrationRevision
	err = f.runtimeStore.Transaction(f.ctx, func(tx *dao.Store) error {
		id, err := tx.CreateDefinition(f.ctx, f.database, f.editor, invalid.Title)
		if err != nil {
			return err
		}
		old, err = tx.CreateRevision(f.ctx, dao.MigrationRevision{DefinitionID: id, RevisionNo: 1, UpSQL: invalid.UpSQL, DownSQL: invalid.DownSQL, SQLHash: migration.ContentHash(invalid.UpSQL, invalid.DownSQL, false, "")})
		return err
	})
	must(t, err)
	_, err = f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: old.ID, Action: "up", ExpectedGeneration: f.state(t).Generation})
	wantError(t, err, migration.ErrDefinitionInvalid)
	reviews, err := f.workflow.Reviews(f.ctx, f.editor, f.database, 50, 0)
	must(t, err)
	if len(reviews) != 0 {
		t.Fatal("malformed SQL reached review")
	}
}
