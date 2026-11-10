package backend

import (
	"net/http"
	"strconv"
)

func (app *application) loadAdministration(r *http.Request, data *pageData) error {
	var err error
	if r.URL.Path == "/admin/administrators" {
		data.Administrators, err = app.services.Authentication.Administrators(r.Context(), data.User.ID)
		return err
	}
	data.Admin, err = app.services.Access.Administration(r.Context(), data.User.ID)
	if err != nil {
		return err
	}
	data.DBMS = app.services.Connections.SupportedDBMS()
	for _, group := range data.Admin.Groups {
		view := groupView{Group: group}
		groupID := strconv.FormatInt(group.ID, 10)
		view.Focus = data.Error == "" && r.URL.Query().Get("created") == group.Name
		if data.Error == "" && (r.URL.Query().Get("group") == groupID || view.Focus) {
			view.Editor = r.URL.Query().Get("editor")
			if r.URL.Query().Get("created") == group.Name {
				view.Editor = "access"
			}
		}
		if r.Method == http.MethodPost && r.PostForm.Get("id") == groupID {
			view.Editor = groupEditor(r.PostForm.Get("action"))
		}
		switch view.Editor {
		case "membership", "access", "information":
		default:
			view.Editor = ""
		}
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
		for _, db := range data.Admin.Databases {
			cell := grantCell{Database: db}
			for _, grant := range data.Admin.Grants {
				if grant.GroupID == group.ID && grant.DatabaseID == db.ID {
					cell.Permission = grant.Permission
					break
				}
			}
			view.Grants = append(view.Grants, cell)
		}
		data.Groups = append(data.Groups, view)
	}
	return nil
}
func groupEditor(action string) string {
	switch action {
	case "membership":
		return "membership"
	case "set_grant", "set_grants":
		return "access"
	case "save_group", "delete_group":
		return "information"
	default:
		return ""
	}
}
