package selection

import (
	"net/http"

	"schakker_shared/httpapi"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api/response"
	"tubes1_wbd/backend/dbmanagement"
)

func List(r *http.Request, data *response.Data, service *dbmanagement.Service) error {
	if data.Databases != nil {
		return nil
	}
	var err error
	data.Databases, err = service.Databases(r.Context(), data.User.ID)
	return err
}
func Database(r *http.Request, data *response.Data, service *dbmanagement.Service, edit bool) error {
	if data.Databases == nil {
		var err error
		data.Databases, err = service.Databases(r.Context(), data.User.ID)
		if err != nil {
			return err
		}
	}
	raw := r.URL.Query().Get("db")
	if r.Method == http.MethodPost {
		raw = r.PostForm.Get("db")
	}
	id, err := httpapi.ID(raw)
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
