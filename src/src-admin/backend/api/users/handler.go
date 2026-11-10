package users

import (
	"net/http"

	"schakker_shared/httpapi"

	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/api/response"
)

type Handler struct{ Users, Administrators *access.Service }

func (h Handler) Action(w http.ResponseWriter, r *http.Request, data *response.Data) error {
	form := r.PostForm
	actor := data.User.ID
	id, err := httpapi.ID(form.Get("id"))
	if err != nil {
		return err
	}
	switch r.URL.Path {
	case "/admin/administrators":
		switch form.Get("action") {
		case "create_admin":
			err = h.Administrators.CreateAdmin(r.Context(), actor, form.Get("username"), form.Get("display"), form.Get("password"))
		case "update_admin":
			err = h.Administrators.UpdateAdmin(r.Context(), actor, id, form.Get("display"), form.Get("active") == "on", form.Get("password"))
		default:
			return access.ErrInvalid
		}
	case "/admin/users":
		if form.Get("admin") != "" {
			return access.ErrInvalid
		}
		switch form.Get("action") {
		case "create_user":
			err = h.Users.CreateUser(r.Context(), actor, form.Get("username"), form.Get("display"), form.Get("password"))
		case "update_user":
			err = h.Users.UpdateUser(r.Context(), actor, id, form.Get("display"), form.Get("active") == "on", form.Get("password"))
		default:
			return access.ErrInvalid
		}
	default:
		return access.ErrInvalid
	}
	if err != nil {
		return err
	}
	response.Redirect(w, r, r.URL.Path, "Changes saved")
	return nil
}
func (h Handler) LoadAdministrators(r *http.Request, data *response.Data) error {
	var err error
	data.Administrators, err = h.Administrators.Administrators(r.Context(), data.User.ID)
	return err
}
