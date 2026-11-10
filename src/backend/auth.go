package backend

import (
	"net/http"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/httpapi"
)

func (app *application) authHTTP() httpapi.Auth {
	return httpapi.Auth{Access: app.services.Access, Production: app.services.Production, SessionCookie: access.CookieName, LoginCookie: httpapi.UserLoginCookieName, Home: "/dashboard"}
}
func (app *application) login(w http.ResponseWriter, r *http.Request) { app.authHTTP().Login(w, r) }
