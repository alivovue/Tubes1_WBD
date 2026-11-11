package groups

import (
	"net/http"
	"strconv"
	"strings"

	"schakker_shared/httpapi"

	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/api/permissions"
	"tubes1_wbd_admin/backend/api/response"
	"tubes1_wbd_admin/backend/connectionconfig"
)

type Handler struct {
	Access      *access.Service
	Connections *connectionconfig.Service
	Permissions permissions.Handler
}

func (h Handler) Load(r *http.Request, data *response.Data) error {
	var err error
	data.Admin, err = h.Access.Administration(r.Context(), data.User.ID)
	if err != nil {
		return err
	}
	data.DBMS = h.Connections.SupportedDBMS()
	for _, group := range data.Admin.Groups {
		view := response.GroupView{Group: group}
		groupID := strconv.FormatInt(group.ID, 10)
		view.Focus = data.Error == "" && r.URL.Query().Get("created") == group.Name
		if data.Error == "" && (r.URL.Query().Get("group") == groupID || view.Focus) {
			view.Editor = r.URL.Query().Get("editor")
			if r.URL.Query().Get("created") == group.Name {
				view.Editor = "access"
			}
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
			cell := response.GrantCell{Database: db}
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
	case "membership", "set_group_leader":
		return "membership"
	case "set_grant", "set_grants":
		return "access"
	case "save_group", "delete_group":
		return "information"
	default:
		return ""
	}
}
func (h Handler) Action(w http.ResponseWriter, r *http.Request, data *response.Data) error {
	form := r.PostForm
	actor := data.User.ID
	id, err := httpapi.ID(form.Get("id"))
	if err != nil {
		return err
	}

	if r.URL.Path == "/admin/access" && form.Get("action") != "set_grant" {
		return access.ErrInvalid
	}
	switch form.Get("action") {
	case "save_group":
		leader, e := httpapi.ID(form.Get("leader_user_id"))
		if e != nil {
			return e
		}
		if leader > 0 {
			err = h.Access.SaveGroupWithLeader(r.Context(), actor, id, form.Get("name"), form.Get("description"), leader)
		} else {
			err = h.Access.SaveGroup(r.Context(), actor, id, form.Get("name"), form.Get("description"))
		}
	case "set_group_leader":
		leader, e := httpapi.ID(form.Get("leader_user_id"))
		if e != nil {
			return e
		}
		err = h.Access.SetGroupLeader(r.Context(), actor, id, leader)
	case "delete_group":
		err = h.Access.DeleteGroup(r.Context(), actor, id)
	case "membership":
		user, e := httpapi.ID(form.Get("user"))
		if e != nil {
			return e
		}
		member := form.Get("member")
		if member != "yes" && member != "no" {
			return access.ErrInvalid
		}
		err = h.Access.SetMembership(r.Context(), actor, user, id, member == "yes")
	case "set_grant", "set_grants":
		err = h.Permissions.Action(r.Context(), actor, id, form)
	default:
		return access.ErrInvalid
	}
	if err != nil {
		return err
	}

	switch {
	case form.Get("action") == "save_group" && id == 0:
		response.Redirect(w, r, "/admin/groups", "Group created. Assign database permissions below.", "created", strings.TrimSpace(form.Get("name")))
	case form.Get("action") == "delete_group":
		response.Redirect(w, r, "/admin/groups", "Group deleted")
	default:
		response.Redirect(w, r, "/admin/groups", "Changes saved", "group", id, "editor", groupEditor(form.Get("action")))
	}
	return nil
}
