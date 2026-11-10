package backend

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/diff"
)

var errUnsavedDraft = errors.New("save the edited draft before analysis")

func (app *application) action(w http.ResponseWriter, r *http.Request, data *pageData) (bool, error) {
	if r.URL.Path == "/sql-viewer" {
		return false, app.sqlViewerAction(r, data)
	}
	if r.URL.Path != "/migrations/new" && r.URL.Path != "/migrations" {
		return false, access.ErrInvalid
	}
	if err := app.selectDatabase(r, data, true); err != nil {
		return false, err
	}
	db := data.Database.ID
	actor := data.User.ID
	form := r.PostForm
	data.Description = form.Get("description")
	data.SQL = form.Get("sql")
	data.OperationsJSON = form.Get("operations")
	var err error
	data.DraftID, err = requestID(form.Get("draft"))
	if err != nil {
		return false, err
	}
	data.DraftUpdated = form.Get("draft_updated")
	switch form.Get("action") {
	case "analyze", "analyze_visual", "analyze_desired":
		if r.URL.Path != "/migrations/new" {
			return false, access.ErrInvalid
		}
		if data.DraftID > 0 {
			draft, err := app.services.Migrations.Draft(r.Context(), actor, db, data.DraftID)
			if err != nil {
				return false, err
			}
			if draft.SQL != data.SQL {
				return false, errUnsavedDraft
			}
		}
		var review migration.Analysis
		switch form.Get("action") {
		case "analyze_visual":
			var operations []diff.Operation
			if err := decodeJSON(form.Get("operations"), &operations); err != nil {
				return false, err
			}
			review, err = app.services.Migrations.AnalyzeChanges(r.Context(), actor, db, operations)
		case "analyze_desired":
			var desired diff.Snapshot
			if err := decodeJSON(form.Get("desired"), &desired); err != nil {
				return false, err
			}
			review, err = app.services.Migrations.AnalyzeDesired(r.Context(), actor, db, desired)
		default:
			review, err = app.services.Migrations.Analyze(r.Context(), actor, db, data.SQL)
		}
		if err != nil {
			return false, err
		}
		data.Review = &review
		data.SQL = review.SQL
		data.ScrollTarget = "migration-analysis"
		if form.Get("action") != "analyze" {
			data.ScrollTarget = "migration-sql-editor"
		}
		return false, nil
	case "save_draft":
		if r.URL.Path != "/migrations/new" {
			return false, access.ErrInvalid
		}
		var updated time.Time
		if data.DraftID > 0 {
			updated, err = time.Parse(time.RFC3339Nano, data.DraftUpdated)
			if err != nil {
				return false, access.ErrInvalid
			}
		}
		draft, err := app.services.Migrations.SaveDraft(r.Context(), actor, db, migration.DraftInput{ID: data.DraftID, Title: data.Description, SQL: data.SQL, ExpectedUpdatedAt: updated})
		if err != nil {
			return false, err
		}
		app.redirect(w, r, "/migrations/new", "Draft saved", "db", db, "draft", draft.ID)
		return true, nil
	case "delete_draft":
		if err := app.services.Migrations.DeleteDraft(r.Context(), actor, db, data.DraftID); err != nil {
			return false, err
		}
		app.redirect(w, r, "/migrations/new", "Draft deleted", "db", db)
		return true, nil
	case "execute":
		if r.URL.Path != "/migrations/new" || form.Get("confirm") != "on" {
			return false, access.ErrInvalid
		}
		var review migration.Analysis
		if err := decodeJSON(form.Get("review"), &review); err != nil {
			return false, err
		}
		if data.SQL != review.SQL {
			return false, migration.ErrStale
		}
		execution, err := app.services.Migrations.Execute(r.Context(), actor, db, migration.ExecuteInput{Review: review, Description: data.Description, DraftID: data.DraftID, AcceptNonAtomic: form.Get("accept_non_atomic") == "on"})
		if execution.ID > 0 {
			message := "Migration completed"
			if err != nil {
				message, _ = userError(err)
			}
			app.redirect(w, r, "/migrations", message, "db", db, "id", execution.ID)
			return true, nil
		}
		return false, err
	case "reconcile":
		if err := app.services.Migrations.Reconcile(r.Context(), actor, db); err != nil {
			return false, err
		}
		app.redirect(w, r, "/migrations", "History reconciled", "db", db)
		return true, nil
	case "resolve":
		id, err := requestID(form.Get("id"))
		if err != nil {
			return false, err
		}
		outcome := form.Get("outcome")
		if outcome != "applied" && outcome != "failed" {
			return false, access.ErrInvalid
		}
		if err := app.services.Migrations.ResolveOutcome(r.Context(), actor, db, id, outcome == "applied", form.Get("reason")); err != nil {
			return false, err
		}
		app.redirect(w, r, "/migrations", "Execution outcome recorded", "db", db, "id", id)
		return true, nil
	}
	return false, access.ErrInvalid
}

func decodeJSON(raw string, value any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return access.ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return access.ErrInvalid
	}
	return nil
}
