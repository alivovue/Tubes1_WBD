package backend

import (
	"net/http"
	"strconv"
	"strings"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/connectionconfig"
)

func (app *application) adminAction(w http.ResponseWriter, r *http.Request, data *pageData) (bool, error) {
	form := r.PostForm
	actor := data.User.ID
	id, err := requestID(form.Get("id"))
	if err != nil {
		return false, err
	}
	switch r.URL.Path {
	case "/admin/administrators":
		switch form.Get("action") {
		case "create_admin":
			err = app.services.Authentication.CreateAdmin(r.Context(), actor, form.Get("username"), form.Get("display"), form.Get("password"))
		case "update_admin":
			err = app.services.Authentication.UpdateAdmin(r.Context(), actor, id, form.Get("display"), form.Get("active") == "on", form.Get("password"))
		default:
			return false, access.ErrInvalid
		}
	case "/admin/users":
		if form.Get("admin") != "" {
			return false, access.ErrInvalid
		}
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
		case "set_grants":
			databases, permissions := form["db"], form["permission"]
			if len(databases) == 0 || len(databases) != len(permissions) {
				return false, access.ErrInvalid
			}
			updates := make([]access.GrantUpdate, 0, len(databases))
			for i, raw := range databases {
				db, e := requestID(raw)
				if e != nil {
					return false, e
				}
				updates = append(updates, access.GrantUpdate{DatabaseID: db, Permission: access.Permission(permissions[i])})
			}
			err = app.services.Access.SetGrants(r.Context(), actor, id, updates)
		default:
			return false, access.ErrInvalid
		}
	case "/admin/connections":
		if form.Get("action") == "disable_connection" {
			err = app.services.Connections.DisableConnection(r.Context(), actor, id)
		} else {
			port := 0
			if raw := form.Get("port"); raw != "" {
				port, err = strconv.Atoi(raw)
				if err != nil {
					return false, access.ErrInvalid
				}
			}
			input := connectionconfig.ConnectionInput{DBMS: form.Get("dbms"), Name: form.Get("name"), Host: form.Get("host"), Port: port, DatabaseName: form.Get("database_name"), Username: form.Get("username"), Password: form.Get("password"), SSLMode: form.Get("sslmode"), Active: form.Get("active") == "on"}
			switch form.Get("action") {
			case "save_connection":
				_, err = app.services.Connections.SaveConnection(r.Context(), actor, id, input)
			case "test_connection":
				err = app.services.Connections.TestConnection(r.Context(), actor, id, input)
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
			app.redirect(w, r, "/admin/groups", "Changes saved", "group", id, "editor", groupEditor(form.Get("action")))
		}
		return true, nil
	}
	app.redirect(w, r, r.URL.Path, "Changes saved")
	return true, nil
}
