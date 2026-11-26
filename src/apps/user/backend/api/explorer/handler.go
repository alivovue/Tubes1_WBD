package explorer

import (
	"net/http"

	"schakker_shared/httpapi"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api/response"
	"tubes1_wbd/backend/api/selection"
	"tubes1_wbd/backend/dbmanagement"
)

type Handler struct{ Databases *dbmanagement.Service }

func (h Handler) Load(r *http.Request, data *response.Data) error {
	if r.URL.Path == "/databases" {
		return selection.List(r, data, h.Databases)
	}
	if err := selection.Database(r, data, h.Databases, false); err != nil {
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
	data.Tables, err = h.Databases.Tables(r.Context(), data.User.ID, data.Database.ID, data.Schema)
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
	detail, err := h.Databases.DescribeTable(r.Context(), data.User.ID, data.Database.ID, data.Schema, data.Table)
	if err != nil {
		return err
	}
	data.Detail = &detail
	data.Offset, err = httpapi.Offset(r.URL.Query().Get("offset"))
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
	rows, err := h.Databases.TableData(r.Context(), data.User.ID, data.Database.ID, data.Schema, data.Table, options)
	if err != nil {
		return err
	}
	data.Data = &rows
	data.HasMore = rows.HasMore
	data.Previous = max(0, data.Offset-50)
	data.Next = data.Offset + 50
	return nil
}
