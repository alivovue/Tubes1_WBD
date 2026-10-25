package backend

import (
	"net/http"
	"net/url"
	"strconv"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
)

func (app *application) loadSQLViewer(r *http.Request, data *pageData) error {
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
	data.Schema, data.Table = r.URL.Query().Get("schema"), r.URL.Query().Get("table")
	if r.Method == http.MethodPost {
		data.Schema, data.Table = r.PostForm.Get("schema"), r.PostForm.Get("table")
	}
	if data.Schema == "" && len(data.Schemas) > 0 {
		data.Schema = data.Schemas[0].Name
	}
	if data.Schema == "" {
		return nil
	}
	found := false
	for _, schema := range data.Schemas {
		found = found || schema.Name == data.Schema
	}
	if !found {
		return access.ErrNotFound
	}
	data.Tables, err = app.services.Databases.Tables(r.Context(), data.User.ID, data.Database.ID, data.Schema)
	if err != nil {
		return err
	}
	if data.Table == "" && len(data.Tables) > 0 {
		data.Table = data.Tables[0].Name
	}
	if data.Table == "" {
		return nil
	}
	found = false
	for _, table := range data.Tables {
		found = found || table.Name == data.Table
	}
	if !found {
		return access.ErrNotFound
	}
	detail, err := app.services.Databases.DescribeTable(r.Context(), data.User.ID, data.Database.ID, data.Schema, data.Table)
	if err != nil {
		return err
	}
	data.Detail = &detail
	if data.ViewerSpec.Limit == 0 {
		data.ViewerSpec.Limit = 100
	}
	return nil
}

func (app *application) sqlViewerAction(r *http.Request, data *pageData) error {
	data.SQL = r.PostForm.Get("sql")
	if err := app.selectDatabase(r, data, false); err != nil {
		return err
	}
	spec, err := parseReadQuerySpec(r.PostForm)
	data.ViewerSpec = spec
	if err != nil {
		return err
	}
	switch r.PostForm.Get("action") {
	case "build_query":
		query, err := app.services.Databases.BuildReadQuery(r.Context(), data.User.ID, data.Database.ID, spec)
		if err != nil {
			return err
		}
		data.SQL = query
		data.Message = "SELECT generated. Review or edit the SQL, then run the read query."
		data.ScrollTarget = "viewer-sql-editor"
		return nil
	case "run_query":
		result, err := app.services.Databases.ReadQuery(r.Context(), data.User.ID, data.Database.ID, data.SQL)
		if err != nil {
			return err
		}
		data.ViewerResult = &result
		data.ScrollTarget = "viewer-results"
		return nil
	default:
		return access.ErrInvalid
	}
}

func parseReadQuerySpec(form url.Values) (dbmanagement.ReadQuerySpec, error) {
	spec := dbmanagement.ReadQuerySpec{Schema: form.Get("schema"), Table: form.Get("table"), Columns: form["columns"], Distinct: form.Get("distinct") == "on", Limit: 100}
	var err error
	if raw := form.Get("limit"); raw != "" {
		spec.Limit, err = strconv.Atoi(raw)
		if err != nil {
			return spec, access.ErrInvalid
		}
	}
	if raw := form.Get("offset"); raw != "" {
		spec.Offset, err = strconv.Atoi(raw)
		if err != nil {
			return spec, access.ErrInvalid
		}
	}
	if direction := form.Get("direction"); direction != "" && direction != "asc" && direction != "desc" {
		return spec, access.ErrInvalid
	}
	if sort := form.Get("sort"); sort != "" {
		spec.OrderBy = []dbmanagement.SortColumn{{Name: sort, Descending: form.Get("direction") == "desc"}}
	}
	columns, ops, values, types := form["filter_column"], form["filter_operator"], form["filter_value"], form["filter_type"]
	if len(columns) > 20 || len(columns) != len(ops) || len(columns) != len(values) || len(columns) != len(types) {
		return spec, access.ErrInvalid
	}
	for i, column := range columns {
		if column != "" {
			spec.Filters = append(spec.Filters, dbmanagement.ReadFilter{Column: column, Operator: ops[i], Value: values[i], Type: types[i]})
		}
	}
	return spec, nil
}
