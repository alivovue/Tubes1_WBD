package migrations

import (
	"net/http"
	"time"

	"schakker_shared/httpapi"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api/response"
	"tubes1_wbd/backend/api/selection"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/diff"
)

type Handler struct {
	Databases  *dbmanagement.Service
	Migrations *migration.Service
}

func (h Handler) Load(r *http.Request, data *response.Data) error {
	if err := selection.Database(r, data, h.Databases, true); err != nil {
		return err
	}
	if data.Database.ID == 0 {
		return nil
	}
	var err error
	data.Schemas, err = h.Databases.Schemas(r.Context(), data.User.ID, data.Database.ID)
	if err != nil {
		return err
	}
	if r.Method != http.MethodPost {
		data.DraftID, err = httpapi.ID(r.URL.Query().Get("draft"))
		if err != nil {
			return err
		}
		if data.DraftID > 0 {
			draft, err := h.Migrations.Draft(r.Context(), data.User.ID, data.Database.ID, data.DraftID)
			if err != nil {
				return err
			}
			data.Description = draft.Title
			data.SQL = draft.SQL
			data.DraftUpdated = draft.UpdatedAt.Format(time.RFC3339Nano)
		}
	}
	data.Drafts, err = h.Migrations.Drafts(r.Context(), data.User.ID, data.Database.ID, 100, 0)
	return err
}

func (h Handler) Action(w http.ResponseWriter, r *http.Request, data *response.Data) (bool, error) {
	if err := selection.Database(r, data, h.Databases, true); err != nil {
		return false, err
	}
	db := data.Database.ID
	actor := data.User.ID
	form := r.PostForm
	data.Description = form.Get("description")
	data.SQL = form.Get("sql")
	data.OperationsJSON = form.Get("operations")
	var err error
	data.DraftID, err = httpapi.ID(form.Get("draft"))
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
			draft, err := h.Migrations.Draft(r.Context(), actor, db, data.DraftID)
			if err != nil {
				return false, err
			}
			if draft.SQL != data.SQL {
				return false, response.ErrUnsavedDraft
			}
		}
		var review migration.Analysis
		switch form.Get("action") {
		case "analyze_visual":
			var operations []diff.Operation
			if err := httpapi.DecodeStrictJSON(form.Get("operations"), &operations); err != nil {
				return false, err
			}
			review, err = h.Migrations.AnalyzeChanges(r.Context(), actor, db, operations)
		case "analyze_desired":
			var desired diff.Snapshot
			if err := httpapi.DecodeStrictJSON(form.Get("desired"), &desired); err != nil {
				return false, err
			}
			review, err = h.Migrations.AnalyzeDesired(r.Context(), actor, db, desired)
		default:
			review, err = h.Migrations.Analyze(r.Context(), actor, db, data.SQL)
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
		draft, err := h.Migrations.SaveDraft(r.Context(), actor, db, migration.DraftInput{ID: data.DraftID, Title: data.Description, SQL: data.SQL, ExpectedUpdatedAt: updated})
		if err != nil {
			return false, err
		}
		response.Redirect(w, r, "/migrations/new", "Draft saved", "db", db, "draft", draft.ID)
		return true, nil
	case "delete_draft":
		if err := h.Migrations.DeleteDraft(r.Context(), actor, db, data.DraftID); err != nil {
			return false, err
		}
		response.Redirect(w, r, "/migrations/new", "Draft deleted", "db", db)
		return true, nil
	}
	return false, access.ErrInvalid
}
