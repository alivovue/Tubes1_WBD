package permissions

import (
	"context"
	"net/url"

	"schakker_shared/httpapi"

	"tubes1_wbd_admin/backend/access"
)

type Handler struct{ Access *access.Service }

func (h Handler) Action(ctx context.Context, actor, id int64, form url.Values) error {
	var err error
	switch form.Get("action") {

	case "set_grant":
		db, e := httpapi.ID(form.Get("db"))
		if e != nil {
			return e
		}
		err = h.Access.SetGrant(ctx, actor, id, db, access.Permission(form.Get("permission")))
	case "set_grants":
		databases, permissions := form["db"], form["permission"]
		if len(databases) == 0 || len(databases) != len(permissions) {
			return access.ErrInvalid
		}
		updates := make([]access.GrantUpdate, 0, len(databases))
		for i, raw := range databases {
			db, e := httpapi.ID(raw)
			if e != nil {
				return e
			}
			updates = append(updates, access.GrantUpdate{DatabaseID: db, Permission: access.Permission(permissions[i])})
		}
		err = h.Access.SetGrants(ctx, actor, id, updates)
	default:
		return access.ErrInvalid
	}
	return err
}
