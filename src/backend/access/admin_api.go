package access

import (
	"errors"
	"net/http"
)

func (a *api) administration(w http.ResponseWriter, r *http.Request) {
	i, _ := CurrentIdentity(r.Context())
	data, err := a.service.Administration(r.Context(), i.User.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (a *api) adminAction(w http.ResponseWriter, r *http.Request) {
	i, _ := CurrentIdentity(r.Context())
	var err error
	switch r.URL.Path {
	case "/api/admin/users/create":
		var input struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			Password    string `json:"password"`
			IsAdmin     bool   `json:"is_platform_admin"`
		}
		if err = decodeJSON(r, &input); err == nil {
			err = a.service.CreateUser(r.Context(), i.User.ID, input.Username, input.DisplayName, input.Password, input.IsAdmin)
		}
	case "/api/admin/users/update":
		var input struct {
			ID          int64  `json:"id"`
			DisplayName string `json:"display_name"`
			Password    string `json:"password"`
			IsAdmin     bool   `json:"is_platform_admin"`
			Active      bool   `json:"is_active"`
		}
		if err = decodeJSON(r, &input); err == nil {
			if input.ID < 1 {
				err = ErrInvalid
			} else {
				err = a.service.UpdateUser(r.Context(), i.User.ID, input.ID, input.DisplayName, input.IsAdmin, input.Active, input.Password)
			}
		}
	case "/api/admin/groups/save":
		var input struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err = decodeJSON(r, &input); err == nil {
			if input.ID < 0 {
				err = ErrInvalid
			} else {
				err = a.service.SaveGroup(r.Context(), i.User.ID, input.ID, input.Name, input.Description)
			}
		}
	case "/api/admin/groups/delete", "/api/admin/databases/disable":
		var input struct {
			ID int64 `json:"id"`
		}
		if err = decodeJSON(r, &input); err == nil {
			if input.ID < 1 {
				err = ErrInvalid
			} else if r.URL.Path == "/api/admin/groups/delete" {
				err = a.service.DeleteGroup(r.Context(), i.User.ID, input.ID)
			} else {
				err = a.service.DisableConnection(r.Context(), i.User.ID, input.ID)
			}
		}
	case "/api/admin/memberships":
		var input struct {
			UserID  int64 `json:"user_id"`
			GroupID int64 `json:"group_id"`
			Member  bool  `json:"member"`
		}
		if err = decodeJSON(r, &input); err == nil {
			if input.UserID < 1 || input.GroupID < 1 {
				err = ErrInvalid
			} else {
				err = a.service.SetMembership(r.Context(), i.User.ID, input.UserID, input.GroupID, input.Member)
			}
		}
	case "/api/admin/grants":
		var input struct {
			GroupID    int64      `json:"group_id"`
			DatabaseID int64      `json:"database_id"`
			Permission Permission `json:"permission"`
		}
		if err = decodeJSON(r, &input); err == nil {
			if input.GroupID < 1 || input.DatabaseID < 1 {
				err = ErrInvalid
			} else {
				err = a.service.SetGrant(r.Context(), i.User.ID, input.GroupID, input.DatabaseID, input.Permission)
			}
		}
	case "/api/admin/databases/save":
		var input struct {
			ID int64 `json:"id"`
			ConnectionInput
		}
		if err = decodeJSON(r, &input); err == nil {
			if input.ID < 0 {
				err = ErrInvalid
			} else {
				err = a.service.SaveConnection(r.Context(), i.User.ID, input.ID, input.ConnectionInput)
				if err != nil && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrConflict) && !errors.Is(err, ErrNotFound) {
					writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Koneksi target gagal. Periksa pengaturan, kredensial, dan TLS; perubahan belum disimpan."})
					return
				}
			}
		}
	default:
		err = ErrNotFound
	}
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
