package connections

import (
	"net/http"
	"strconv"

	"schakker_shared/httpapi"

	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/api/response"
	"tubes1_wbd_admin/backend/connectionconfig"
)

type Handler struct{ Connections *connectionconfig.Service }

func (h Handler) Action(w http.ResponseWriter, r *http.Request, data *response.Data) error {
	form := r.PostForm
	actor := data.User.ID
	id, err := httpapi.ID(form.Get("id"))
	if err != nil {
		return err
	}

	if form.Get("action") == "disable_connection" {
		err = h.Connections.DisableConnection(r.Context(), actor, id)
	} else {
		port := 0
		if raw := form.Get("port"); raw != "" {
			port, err = strconv.Atoi(raw)
			if err != nil {
				return access.ErrInvalid
			}
		}
		input := connectionconfig.ConnectionInput{DBMS: form.Get("dbms"), Name: form.Get("name"), Host: form.Get("host"), Port: port, DatabaseName: form.Get("database_name"), Username: form.Get("username"), Password: form.Get("password"), SSLMode: form.Get("sslmode"), Active: form.Get("active") == "on"}
		switch form.Get("action") {
		case "save_connection":
			_, err = h.Connections.SaveConnection(r.Context(), actor, id, input)
		default:
			return access.ErrInvalid
		}
	}
	if err != nil {
		return err
	}
	response.Redirect(w, r, r.URL.Path, "Changes saved")
	return nil
}
