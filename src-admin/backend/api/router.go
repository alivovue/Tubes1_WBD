package api

import (
	"net/http"
	"strings"
	"time"

	"schakker_shared/httpapi"

	"tubes1_wbd_admin/backend/access"
	apiauth "tubes1_wbd_admin/backend/api/auth"
	"tubes1_wbd_admin/backend/api/connections"
	"tubes1_wbd_admin/backend/api/groups"
	"tubes1_wbd_admin/backend/api/permissions"
	"tubes1_wbd_admin/backend/api/response"
	"tubes1_wbd_admin/backend/api/users"
	"tubes1_wbd_admin/backend/connectionconfig"
)

type page struct{ title, active string }

var pages = map[string]page{
	"/": {"Admin sign in", "login"}, "/login": {"Admin sign in", "login"},
	"/admin/users": {"Employees", "admin-users"}, "/admin/administrators": {"Administrators", "admin-administrators"}, "/admin/groups": {"Groups", "admin-groups"},
	"/admin/connections": {"Database Connections", "admin-connections"}, "/admin/access": {"Groups", "admin-groups"},
}

func HasPage(path string) bool { _, ok := pages[path]; return ok }

type router struct {
	auth           apiauth.Auth
	authentication *access.Service
	users          users.Handler
	groups         groups.Handler
	connections    connections.Handler
}

func NewHandler(auth, administration *access.Service, registered *connectionconfig.Service, production bool) http.Handler {
	return &router{
		auth: apiauth.Auth{
			Access: auth, Production: production, SessionCookie: apiauth.AdminCookieName,
			LoginCookie: apiauth.AdminLoginCookieName, Home: "/admin/users",
		},
		authentication: auth,
		users:          users.Handler{Users: administration, Administrators: auth},
		groups: groups.Handler{
			Access: administration, Connections: registered,
			Permissions: permissions.Handler{Access: administration},
		},
		connections: connections.Handler{Connections: registered},
	}
}
func (h *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/login" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			httpapi.Error(w, r, "Method not allowed", 405)
			return
		}
		h.auth.Login(w, r)
		return
	}
	p, exists := pages[r.URL.Path]
	if (!exists || !strings.HasPrefix(r.URL.Path, "/admin/")) && r.URL.Path != "/session" && r.URL.Path != "/logout" {
		httpapi.Error(w, r, "The requested endpoint was not found.", 404)
		return
	}
	allowed := "GET, HEAD, POST"
	if r.URL.Path == "/session" {
		allowed = "GET, HEAD"
	}
	if r.URL.Path == "/logout" {
		allowed = "POST"
	}
	if !strings.Contains(", "+allowed+", ", ", "+r.Method+", ") {
		w.Header().Set("Allow", allowed)
		httpapi.Error(w, r, "Method not allowed", 405)
		return
	}
	identity, csrf, ok := h.auth.Authenticate(w, r, r.URL.Path == "/session")
	if !ok {
		return
	}
	data := response.Data{Title: p.title, Active: p.active, Section: "Administration", User: identity.User, CSRF: csrf, Now: time.Now(), Expires: identity.AbsoluteExpires}
	if r.URL.Path == "/session" {
		httpapi.JSON(w, r, 200, data)
		return
	}
	if r.Method == http.MethodPost {
		if err := h.auth.ValidateAction(w, r, identity); err != nil {
			response.Fail(w, r, err)
			return
		}
	}
	if r.URL.Path == "/logout" {
		h.auth.Logout(w, r, identity)
		return
	}
	if err := h.authentication.RequireAdmin(r.Context(), identity.User.ID); err != nil {
		response.Fail(w, r, err)
		return
	}
	if r.URL.Path == "/admin/access" && r.Method != http.MethodPost {
		response.Redirect(w, r, "/admin/groups", "")
		return
	}
	if r.Method == http.MethodPost {
		var err error
		switch r.URL.Path {
		case "/admin/users", "/admin/administrators":
			err = h.users.Action(w, r, &data)
		case "/admin/groups", "/admin/access":
			err = h.groups.Action(w, r, &data)
		case "/admin/connections":
			err = h.connections.Action(w, r, &data)
		}
		if err != nil {
			response.Fail(w, r, err)
		}
		return
	}
	var err error
	if r.URL.Path == "/admin/administrators" {
		err = h.users.LoadAdministrators(r, &data)
	} else {
		err = h.groups.Load(r, &data)
	}
	if err != nil {
		response.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, r, 200, data)
}
