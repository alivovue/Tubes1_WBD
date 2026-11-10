package backend

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/connectionconfig"
	"tubes1_wbd_admin/backend/httpapi"
	"tubes1_wbd_admin/database/dao"
)

type Services struct {
	Authentication *access.Service
	Access         *access.Service
	Connections    *connectionconfig.Service
	Production     bool
}
type application struct {
	services       Services
	entry          []byte
	assets, public http.Handler
}
type page struct{ title, active string }

var pages = map[string]page{
	"/": {"Admin sign in", "login"}, "/login": {"Admin sign in", "login"},
	"/admin/users": {"Employees", "admin-users"}, "/admin/administrators": {"Administrators", "admin-administrators"}, "/admin/groups": {"Groups", "admin-groups"},
	"/admin/connections": {"Database Connections", "admin-connections"}, "/admin/access": {"Groups", "admin-groups"},
}

type groupView struct {
	dao.Group
	Members []dao.User
	Grants  []grantCell
	Editor  string
	Focus   bool
}
type grantCell struct {
	Database   dao.Database
	Permission dao.Permission
}
type pageData struct {
	Title, Active, Section string
	User                   dao.User
	CSRF, Error, Message   string
	Now, Expires           time.Time
	Administrators         []dao.User
	Admin                  dao.AdminData
	Groups                 []groupView
	DBMS                   []string
}
type managedHandler struct {
	http.Handler
	runtime *runtime
}

func (h *managedHandler) Close() error { return h.runtime.Close() }
func NewHandler(root string) (http.Handler, error) {
	services, runtime, err := servicesFromEnvironment(root)
	if err != nil {
		return nil, err
	}
	handler, err := NewHandlerWithServices(root, services)
	if err != nil {
		runtime.Close()
		return nil, err
	}
	return &managedHandler{Handler: handler, runtime: runtime}, nil
}
func NewHandlerWithServices(root string, services Services) (http.Handler, error) {
	if services.Authentication == nil || services.Access == nil || services.Connections == nil {
		return nil, errors.New("access and connection-configuration services are required")
	}
	entry, err := os.ReadFile(filepath.Join(root, "frontend", "dist", "index.html"))
	if err != nil {
		return nil, fmt.Errorf("build src-admin/frontend with npm ci and npm run build: %w", err)
	}
	app := &application{services: services, entry: entry,
		assets: http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "dist", "assets")))),
		public: http.StripPrefix("/public/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "public")))),
	}
	return http.HandlerFunc(app.serveHTTP), nil
}
func (app *application) authHTTP() httpapi.Auth {
	return httpapi.Auth{Access: app.services.Authentication, Production: app.services.Production, SessionCookie: httpapi.AdminCookieName, LoginCookie: httpapi.AdminLoginCookieName, Home: "/admin/users"}
}
func (app *application) serveHTTP(w http.ResponseWriter, r *http.Request) {
	httpapi.SecurityHeaders(w)
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		app.assets.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/public/") {
		app.public.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		request := r.Clone(r.Context())
		request.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		app.serveAPI(w, request)
		return
	}
	if _, exists := pages[r.URL.Path]; !exists {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		httpapi.Error(w, r, "Method not allowed", 405)
		return
	}
	httpapi.Write(w, r, 200, "text/html; charset=utf-8", app.entry)
}
func (app *application) serveAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/login" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			httpapi.Error(w, r, "Method not allowed", 405)
			return
		}
		app.authHTTP().Login(w, r)
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
	auth := app.authHTTP()
	identity, csrf, ok := auth.Authenticate(w, r, r.URL.Path == "/session")
	if !ok {
		return
	}
	data := pageData{Title: p.title, Active: p.active, Section: "Administration", User: identity.User, CSRF: csrf, Now: time.Now(), Expires: identity.AbsoluteExpires}
	if r.URL.Path == "/session" {
		app.json(w, r, 200, data)
		return
	}
	if r.Method == http.MethodPost {
		if err := auth.ValidateAction(w, r, identity); err != nil {
			app.fail(w, r, err)
			return
		}
	}
	if r.URL.Path == "/logout" {
		auth.Logout(w, r, identity)
		return
	}
	if err := app.services.Authentication.RequireAdmin(r.Context(), identity.User.ID); err != nil {
		app.fail(w, r, err)
		return
	}
	if r.URL.Path == "/admin/access" && r.Method != http.MethodPost {
		app.redirect(w, r, "/admin/groups", "")
		return
	}
	if r.Method == http.MethodPost {
		handled, err := app.adminAction(w, r, &data)
		if err != nil {
			app.fail(w, r, err)
			return
		}
		if handled {
			return
		}
	}
	if err := app.loadAdministration(r, &data); err != nil {
		app.fail(w, r, err)
		return
	}
	app.json(w, r, 200, data)
}
func (app *application) json(w http.ResponseWriter, r *http.Request, status int, value any) {
	httpapi.JSON(w, r, status, value)
}
func (app *application) fail(w http.ResponseWriter, r *http.Request, err error) {
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
func requestID(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, access.ErrInvalid
	}
	return id, nil
}
func (app *application) redirect(w http.ResponseWriter, r *http.Request, route, message string, args ...any) {
	values := url.Values{"notice": {message}}
	for i := 0; i+1 < len(args); i += 2 {
		values.Set(fmt.Sprint(args[i]), fmt.Sprint(args[i+1]))
	}
	app.json(w, r, 200, map[string]string{"location": route + "?" + values.Encode()})
}
