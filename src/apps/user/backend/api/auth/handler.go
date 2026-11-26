package auth

import (
	"schakker_shared/authhttp"

	"tubes1_wbd/backend/access"
)

const UserLoginCookieName = "schakker_login_csrf"

type Auth = authhttp.Handler[access.Identity]

func New(service *access.Service, production bool) Auth {
	return Auth{
		Access: service, Production: production,
		SessionCookie: access.CookieName, LoginCookie: UserLoginCookieName, Home: "/dashboard",
		LoginEvent: "authentication.login", ForbiddenMessage: "Sign in is unavailable.",
	}
}
