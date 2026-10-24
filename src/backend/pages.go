package backend

import (
	"net/http"
	"sort"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
)

func (app *application) loadPage(r *http.Request, data *pageData) error {
	if data.Message == "" {
		data.Message = r.URL.Query().Get("notice")
	}
	if len(r.URL.Path) >= 7 && r.URL.Path[:7] == "/admin/" {
		return app.loadAdministration(r, data)
	}
	var err error
	if data.Databases == nil {
		data.Databases, err = app.services.Databases.Databases(r.Context(), data.User.ID)
		if err != nil {
			return err
		}
	}
	switch r.URL.Path {
	case "/databases":
		return nil
	case "/dashboard":
		for _, db := range data.Databases {
			if db.Permission == access.ViewEdit {
				data.EditableCount++
			}
			entries, err := app.services.Migrations.History(r.Context(), data.User.ID, db.ID, 5, 0)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				data.Recent = append(data.Recent, recentExecution{MigrationExecution: entry, DatabaseName: db.Name})
			}
		}
		sort.Slice(data.Recent, func(i, j int) bool { return data.Recent[i].StartedAt.After(data.Recent[j].StartedAt) })
		if len(data.Recent) > 8 {
			data.Recent = data.Recent[:8]
		}
		return nil
	case "/explorer":
		return app.loadExplorer(r, data)
	case "/migrations", "/migrations/new":
		return app.loadMigrations(r, data)
	}
	return access.ErrNotFound
}
func (app *application) selectDatabase(r *http.Request, data *pageData, edit bool) error {
	if data.Databases == nil {
		var err error
		data.Databases, err = app.services.Databases.Databases(r.Context(), data.User.ID)
		if err != nil {
			return err
		}
	}
	raw := r.URL.Query().Get("db")
	if r.Method == http.MethodPost {
		raw = r.PostForm.Get("db")
	}
	id, err := requestID(raw)
	if err != nil {
		return err
	}
	if r.Method == http.MethodPost && id == 0 {
		return access.ErrInvalid
	}
	var found bool
	for _, db := range data.Databases {
		if id == db.ID {
			data.Database = db
			found = true
			break
		}
	}
	if id > 0 && (!found || edit && data.Database.Permission != access.ViewEdit) {
		return access.ErrForbidden
	}
	if edit {
		filtered := data.Databases[:0:0]
		for _, db := range data.Databases {
			if db.Permission == access.ViewEdit {
				filtered = append(filtered, db)
			}
		}
		data.Databases = filtered
	}
	if id == 0 && len(data.Databases) > 0 {
		data.Database = data.Databases[0]
	}
	data.CanEdit = data.Database.Permission == access.ViewEdit
	return nil
}
func (app *application) loadAdministration(r *http.Request, data *pageData) error {
	var err error
	data.Admin, err = app.services.Access.Administration(r.Context(), data.User.ID)
	if err != nil {
		return err
	}
	data.DBMS = app.services.Databases.SupportedDBMS()
	for _, group := range data.Admin.Groups {
		view := groupView{Group: group}
		for _, membership := range data.Admin.Memberships {
			if membership.GroupID == group.ID {
				for _, user := range data.Admin.Users {
					if user.ID == membership.UserID {
						view.Members = append(view.Members, user)
						break
					}
				}
			}
		}
		data.Groups = append(data.Groups, view)
		row := grantRow{Group: group}
		for _, db := range data.Admin.Databases {
			cell := grantCell{Database: db}
			for _, grant := range data.Admin.Grants {
				if grant.GroupID == group.ID && grant.DatabaseID == db.ID {
					cell.Permission = grant.Permission
					break
				}
			}
			row.Cells = append(row.Cells, cell)
		}
		data.GrantRows = append(data.GrantRows, row)
	}
	return nil
}
func (app *application) loadExplorer(r *http.Request, data *pageData) error {
	if err := app.selectDatabase(r, data, false); err != nil {
		return err
	}
	if data.Database.ID == 0 {
		return nil
	}
	var err error
	data.Schemas, err = app.services.Databases.Schemas(r.Context(), data.User.ID, data.Database.ID)
	if err != nil {
		return err
	}
	data.Schema = r.URL.Query().Get("schema")
	if data.Schema == "" && len(data.Schemas) > 0 {
		data.Schema = data.Schemas[0].Name
	}
	if data.Schema == "" {
		return nil
	}
	found := false
	for _, schema := range data.Schemas {
		if schema.Name == data.Schema {
			found = true
			break
		}
	}
	if !found {
		return access.ErrNotFound
	}
	data.Tables, err = app.services.Databases.Tables(r.Context(), data.User.ID, data.Database.ID, data.Schema)
	if err != nil {
		return err
	}
	data.Table = r.URL.Query().Get("table")
	if data.Table == "" && len(data.Tables) > 0 {
		data.Table = data.Tables[0].Name
	}
	if data.Table == "" {
		return nil
	}
	found = false
	for _, table := range data.Tables {
		if table.Name == data.Table {
			found = true
			break
		}
	}
	if !found {
		return access.ErrNotFound
	}
	detail, err := app.services.Databases.DescribeTable(r.Context(), data.User.ID, data.Database.ID, data.Schema, data.Table)
	if err != nil {
		return err
	}
	data.Detail = &detail
	data.Offset, err = pageOffset(r.URL.Query().Get("offset"))
	if err != nil {
		return err
	}
	data.Sort = r.URL.Query().Get("sort")
	data.Direction = r.URL.Query().Get("direction")
	options := dbmanagement.PageOptions{Limit: 50, Offset: data.Offset}
	if data.Direction != "" && data.Direction != "asc" && data.Direction != "desc" {
		return access.ErrInvalid
	}
	if data.Sort != "" {
		options.OrderBy = []dbmanagement.SortColumn{{Name: data.Sort, Descending: data.Direction == "desc"}}
	}
	rows, err := app.services.Databases.TableData(r.Context(), data.User.ID, data.Database.ID, data.Schema, data.Table, options)
	if err != nil {
		return err
	}
	data.Data = &rows
	data.HasMore = rows.HasMore
	data.Previous = max(0, data.Offset-50)
	data.Next = data.Offset + 50
	return nil
}
func (app *application) loadMigrations(r *http.Request, data *pageData) error {
	editor := r.URL.Path == "/migrations/new"
	if err := app.selectDatabase(r, data, editor); err != nil {
		return err
	}
	if data.Database.ID == 0 {
		return nil
	}
	var err error
	if editor {
		data.Schemas, err = app.services.Databases.Schemas(r.Context(), data.User.ID, data.Database.ID)
		if err != nil {
			return err
		}
		if r.Method != http.MethodPost {
			data.DraftID, err = requestID(r.URL.Query().Get("draft"))
			if err != nil {
				return err
			}
			if data.DraftID > 0 {
				draft, err := app.services.Migrations.Draft(r.Context(), data.User.ID, data.Database.ID, data.DraftID)
				if err != nil {
					return err
				}
				data.Description = draft.Title
				data.SQL = draft.SQL
				data.DraftUpdated = draft.UpdatedAt.Format(time.RFC3339Nano)
			}
		}
		data.Drafts, err = app.services.Migrations.Drafts(r.Context(), data.User.ID, data.Database.ID, 100, 0)
		return err
	}
	data.Offset, err = pageOffset(r.URL.Query().Get("offset"))
	if err != nil {
		return err
	}
	data.History, err = app.services.Migrations.History(r.Context(), data.User.ID, data.Database.ID, 51, data.Offset)
	if err != nil {
		return err
	}
	data.HasMore = len(data.History) > 50
	if data.HasMore {
		data.History = data.History[:50]
	}
	data.Previous = max(0, data.Offset-50)
	data.Next = data.Offset + 50
	id, err := requestID(r.URL.Query().Get("id"))
	if err != nil {
		return err
	}
	if id > 0 {
		execution, err := app.services.Migrations.Detail(r.Context(), data.User.ID, data.Database.ID, id)
		if err != nil {
			return err
		}
		data.Execution = &execution
	}
	return nil
}
