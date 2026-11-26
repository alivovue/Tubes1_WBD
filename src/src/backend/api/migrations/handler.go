package migrations

import (
	"net/http"

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
	return err
}

func (h Handler) Action(r *http.Request, data *response.Data) error {
	if err := selection.Database(r, data, h.Databases, true); err != nil {
		return err
	}
	db := data.Database.ID
	actor := data.User.ID
	form := r.PostForm
	data.Description = form.Get("description")
	data.SQL = form.Get("sql")
	data.OperationsJSON = form.Get("operations")
	var err error
	switch form.Get("action") {
	case "analyze", "analyze_visual", "analyze_desired":
		if r.URL.Path != "/migrations/new" {
			return access.ErrInvalid
		}
		var review migration.Analysis
		switch form.Get("action") {
		case "analyze_visual":
			var operations []diff.Operation
			if err := httpapi.DecodeStrictJSON(form.Get("operations"), &operations); err != nil {
				return err
			}
			review, err = h.Migrations.AnalyzeChanges(r.Context(), actor, db, operations)
		case "analyze_desired":
			var desired diff.Snapshot
			if err := httpapi.DecodeStrictJSON(form.Get("desired"), &desired); err != nil {
				return err
			}
			review, err = h.Migrations.AnalyzeDesired(r.Context(), actor, db, desired)
		default:
			review, err = h.Migrations.Analyze(r.Context(), actor, db, data.SQL)
		}
		if err != nil {
			return err
		}
		data.Review = &review
		data.SQL = review.SQL
		data.ScrollTarget = "migration-analysis"
		if form.Get("action") != "analyze" {
			data.ScrollTarget = "migration-sql-editor"
		}
		return nil
	}
	return access.ErrInvalid
}
