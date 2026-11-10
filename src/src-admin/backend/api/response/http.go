package response

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"schakker_shared/httpapi"

	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/connectionconfig"
)

func Fail(w http.ResponseWriter, r *http.Request, err error) {
	message, status := "The operation is temporarily unavailable. Please retry.", 500
	switch {
	case errors.Is(err, access.ErrForbidden):
		message, status = "You do not have permission for this action.", 403
	case errors.Is(err, access.ErrNotFound):
		message, status = "The requested item was not found.", 404
	case errors.Is(err, connectionconfig.ErrDuplicateConnection):
		message, status = "This database is already registered. Edit or reactivate its existing connection.", 409
	case errors.Is(err, access.ErrConflict):
		message, status = "This item changed or already exists. Reload and try again.", 409
	case errors.Is(err, access.ErrInvalid):
		message, status = "Check the supplied values and required fields.", 400
	case errors.Is(err, connectionconfig.ErrConnection):
		message, status = "Cannot connect to the target database. Check its settings and availability.", 502
	case errors.Is(err, connectionconfig.ErrUnsupported):
		message, status = "This database type is not supported.", 400
	}
	httpapi.Error(w, r, message, status)
}
func Redirect(w http.ResponseWriter, r *http.Request, route, message string, args ...any) {
	values := url.Values{"notice": {message}}
	for i := 0; i+1 < len(args); i += 2 {
		values.Set(fmt.Sprint(args[i]), fmt.Sprint(args[i+1]))
	}
	httpapi.JSON(w, r, 200, map[string]string{"location": route + "?" + values.Encode()})
}
