package backend

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/diff"
)

var errUnsavedDraft = errors.New("save the edited draft before analysis")

func (app *application) action(w http.ResponseWriter, r *http.Request, data *pageData) (bool, error) {
	if strings.HasPrefix(r.URL.Path, "/admin/") {
		return app.adminAction(w, r, data)
	}
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
		raw, err := json.Marshal(review)
		if err != nil {
			return false, err
		}
		data.ReviewJSON = string(raw)
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

func (app *application) adminAction(w http.ResponseWriter, r *http.Request, data *pageData) (bool, error) {
	form := r.PostForm
	actor := data.User.ID
	id, err := requestID(form.Get("id"))
	if err != nil {
		return false, err
	}
	switch r.URL.Path {
	case "/admin/users":
		switch form.Get("action") {
		case "create_user":
			err = app.services.Access.CreateUser(r.Context(), actor, form.Get("username"), form.Get("display"), form.Get("password"), form.Get("admin") == "on")
		case "update_user":
			err = app.services.Access.UpdateUser(r.Context(), actor, id, form.Get("display"), form.Get("admin") == "on", form.Get("active") == "on", form.Get("password"))
		default:
			return false, access.ErrInvalid
		}
	case "/admin/groups", "/admin/access":
		if r.URL.Path == "/admin/access" && form.Get("action") != "set_grant" {
			return false, access.ErrInvalid
		}
		switch form.Get("action") {
		case "save_group":
			err = app.services.Access.SaveGroup(r.Context(), actor, id, form.Get("name"), form.Get("description"))
		case "delete_group":
			err = app.services.Access.DeleteGroup(r.Context(), actor, id)
		case "membership":
			user, e := requestID(form.Get("user"))
			if e != nil {
				return false, e
			}
			member := form.Get("member")
			if member != "yes" && member != "no" {
				return false, access.ErrInvalid
			}
			err = app.services.Access.SetMembership(r.Context(), actor, user, id, member == "yes")
		case "set_grant":
			db, e := requestID(form.Get("db"))
			if e != nil {
				return false, e
			}
			err = app.services.Access.SetGrant(r.Context(), actor, id, db, access.Permission(form.Get("permission")))
		default:
			return false, access.ErrInvalid
		}
	case "/admin/connections":
		if form.Get("action") == "disable_connection" {
			err = app.services.Databases.DisableConnection(r.Context(), actor, id)
		} else {
			port := 0
			if raw := form.Get("port"); raw != "" {
				port, err = strconv.Atoi(raw)
				if err != nil {
					return false, access.ErrInvalid
				}
			}
			input := dbmanagement.ConnectionInput{DBMS: form.Get("dbms"), Name: form.Get("name"), Host: form.Get("host"), Port: port, DatabaseName: form.Get("database_name"), Username: form.Get("username"), Password: form.Get("password"), SSLMode: form.Get("sslmode"), Active: form.Get("active") == "on"}
			switch form.Get("action") {
			case "save_connection":
				_, err = app.services.Databases.SaveConnection(r.Context(), actor, id, input)
			case "test_connection":
				err = app.services.Databases.TestConnection(r.Context(), actor, id, input)
				if err == nil {
					data.Message = "Connection verified"
					return false, nil
				}
			default:
				return false, access.ErrInvalid
			}
		}
	default:
		return false, access.ErrInvalid
	}
	if err != nil {
		return false, err
	}
	if r.URL.Path == "/admin/groups" || r.URL.Path == "/admin/access" {
		switch {
		case form.Get("action") == "save_group" && id == 0:
			app.redirect(w, r, "/admin/groups", "Group created. Assign database permissions below.", "created", strings.TrimSpace(form.Get("name")))
		case form.Get("action") == "delete_group":
			app.redirect(w, r, "/admin/groups", "Group deleted")
		default:
			app.redirect(w, r, "/admin/groups", "Changes saved", "group", id)
		}
		return true, nil
	}
	app.redirect(w, r, r.URL.Path, "Changes saved")
	return true, nil
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
