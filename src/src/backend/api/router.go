package api

import (
	"net/http"
	"strings"
	"time"

	"schakker_shared/httpapi"

	"tubes1_wbd/backend/access"
	apiauth "tubes1_wbd/backend/api/auth"
	"tubes1_wbd/backend/api/dashboard"
	"tubes1_wbd/backend/api/explorer"
	migrationsapi "tubes1_wbd/backend/api/migrations"
	"tubes1_wbd/backend/api/response"
	"tubes1_wbd/backend/api/selection"
	"tubes1_wbd/backend/api/sqlviewer"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
)

type page struct{ title, active, section string }

var pages = map[string]page{
	"/":               {"Masuk", "login", "Authentication"},
	"/login":          {"Masuk", "login", "Authentication"},
	"/dashboard":      {"Dashboard", "dashboard", "Overview"},
	"/databases":      {"My Databases", "databases", "Workspace"},
	"/explorer":       {"Database Explorer", "databases", "Workspace"},
	"/sql-viewer":     {"SQL Viewer", "sql-viewer", "Workspace"},
	"/migrations/new": {"SQL Migration", "migration-editor", "Workspace"},
	"/migrations":     {"Changelog", "migration-history", "Workspace"},
}

func HasPage(path string) bool { _, ok := pages[path]; return ok }

type router struct {
	auth       apiauth.Auth
	dashboard  dashboard.Handler
	explorer   explorer.Handler
	viewer     sqlviewer.Handler
	migrations migrationsapi.Handler
}

func NewHandler(auth *access.Service, databases *dbmanagement.Service, migrations *migration.Service, production bool) http.Handler {
	return &router{
		auth: apiauth.Auth{
			Access: auth, Production: production, SessionCookie: access.CookieName,
			LoginCookie: apiauth.UserLoginCookieName, Home: "/dashboard",
		},
		dashboard:  dashboard.Handler{Databases: databases, Migrations: migrations},
		explorer:   explorer.Handler{Databases: databases},
		viewer:     sqlviewer.Handler{Databases: databases},
		migrations: migrationsapi.Handler{Databases: databases, Migrations: migrations},
	}
}
func (h *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if migrationsapi.IsWorkflowRoute(r.URL.Path) {
		identity, _, ok := h.auth.Authenticate(w, r, false)
		if !ok {
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if err := h.auth.ValidateJSONAction(r, identity); err != nil {
				response.Fail(w, r, err)
				return
			}
		}
		h.migrations.ServeWorkflow(w, r, identity.User.ID)
		return
	}
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
	if !exists && r.URL.Path != "/logout" && r.URL.Path != "/session" {
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
	data := response.Data{Title: p.title, Active: p.active, Section: p.section, User: identity.User, CSRF: csrf, Now: time.Now(), Expires: identity.AbsoluteExpires}
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
	if r.Method == http.MethodPost {
		var handled bool
		var err error
		switch r.URL.Path {
		case "/sql-viewer":
			err = h.viewer.Action(r, &data)
		case "/migrations", "/migrations/new":
			handled, err = h.migrations.Action(w, r, &data)
		default:
			err = access.ErrInvalid
		}
		if err != nil {
			response.Fail(w, r, err)
			return
		}
		if handled {
			return
		}
	}
	if data.Message == "" {
		data.Message = r.URL.Query().Get("notice")
	}
	// Action-generated messages take precedence over a navigation notice.
	// The action may already have loaded/filtered the database selection.
	if err := h.load(r, &data); err != nil {
		response.Fail(w, r, err)
		return
	}
	response.Write(w, r, &data)
}
func (h *router) load(r *http.Request, data *response.Data) error {
	switch r.URL.Path {
	case "/dashboard":
		return h.dashboard.Load(r, data)
	case "/databases", "/explorer":
		return h.explorer.Load(r, data)
	case "/sql-viewer":
		return h.viewer.Load(r, data)
	case "/migrations", "/migrations/new":
		return h.migrations.Load(r, data)
	}
	if err := selection.List(r, data, h.explorer.Databases); err != nil {
		return err
	}
	return access.ErrNotFound
}
