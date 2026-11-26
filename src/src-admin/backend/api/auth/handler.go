package auth

import (
	"schakker_shared/authhttp"

	"tubes1_wbd_admin/backend/access"
)

const AdminCookieName = access.CookieName
const AdminLoginCookieName = "schakker_admin_login_csrf"

type Auth = authhttp.Handler[access.Identity]

func New(service *access.Service, production bool) Auth {
	return Auth{
		Access: service, Production: production,
		SessionCookie: AdminCookieName, LoginCookie: AdminLoginCookieName, Home: "/admin/users",
		LoginEvent: "authentication.admin.login", ForbiddenMessage: "Administrator sign in is unavailable.",
	}
}
