package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/diff"
	"tubes1_wbd/database/dao"
)

type page struct{ template, layout, title, active, section string }

var pages = map[string]page{
	"/":                  {"login.gohtml", "auth_layout", "Masuk", "login", "Authentication"},
	"/login":             {"login.gohtml", "auth_layout", "Masuk", "login", "Authentication"},
	"/dashboard":         {"dashboard.gohtml", "app_layout", "Dashboard", "dashboard", "Overview"},
	"/databases":         {"databases.gohtml", "app_layout", "My Databases", "databases", "Workspace"},
	"/explorer":          {"explorer.gohtml", "app_layout", "Database Explorer", "databases", "Workspace"},
	"/sql-viewer":        {"sql_viewer.gohtml", "app_layout", "SQL Viewer", "sql-viewer", "Workspace"},
	"/migrations/new":    {"migration_editor.gohtml", "app_layout", "SQL Migration", "migration-editor", "Workspace"},
	"/migrations":        {"migration_history.gohtml", "app_layout", "Changelog", "migration-history", "Workspace"},
	"/admin/users":       {"admin_users.gohtml", "app_layout", "Employees", "admin-users", "Administration"},
	"/admin/groups":      {"admin_groups.gohtml", "app_layout", "Groups", "admin-groups", "Administration"},
	"/admin/connections": {"admin_connections.gohtml", "app_layout", "Database Connections", "admin-connections", "Administration"},
	"/admin/access":      {"admin_groups.gohtml", "app_layout", "Groups", "admin-groups", "Administration"},
}

var legacyRoutes = map[string]string{
	"/index.html": "/", "/index2.html": "/databases", "/index3.html": "/databases",
	"/index4.html": "/dashboard", "/index5.html": "/dashboard",
}

type Services struct {
	Access     *access.Service
	Databases  *dbmanagement.Service
	Migrations *migration.Service
	Production bool
}

type application struct {
	services       Services
	templates      map[string]*template.Template
	static, public http.Handler
	productJSON    []byte
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
type recentExecution struct {
	dao.MigrationExecution
	DatabaseName string
}

type pageData struct {
	Title, Active, Section                         string
	User                                           dao.User
	CSRF, Error, Message                           string
	Status                                         int
	Now, Expires                                   time.Time
	Databases                                      []dao.Database
	Database                                       dao.Database
	CanEdit                                        bool
	EditableCount                                  int
	Recent                                         []recentExecution
	Admin                                          dao.AdminData
	Groups                                         []groupView
	DBMS                                           []string
	Schemas                                        []dbmanagement.Schema
	Tables                                         []dbmanagement.Table
	Schema, Table, Sort, Direction                 string
	Detail                                         *dbmanagement.TableDetail
	Data                                           *dbmanagement.DataPage
	Offset, Previous, Next                         int
	HasMore                                        bool
	History                                        []dao.MigrationExecution
	Execution                                      *dao.MigrationExecution
	Drafts                                         []dao.MigrationDraft
	DraftID                                        int64
	DraftUpdated, Description, SQL, OperationsJSON string
	Review                                         *migration.Analysis
	ReviewJSON                                     string
	ViewerResult                                   *dbmanagement.ReadResult
	ViewerSpec                                     dbmanagement.ReadQuerySpec
	ScrollTarget                                   string
}

func NewHandler(root string) (http.Handler, error) {
	services, closeDB, err := servicesFromEnvironment(root)
	if err != nil {
		return nil, err
	}
	handler, err := NewHandlerWithServices(root, services)
	if err != nil {
		closeDB()
		return nil, err
	}
	return &managedHandler{Handler: handler, close: closeDB}, nil
}

type managedHandler struct {
	http.Handler
	close func()
}

func (h *managedHandler) Close() error {
	h.close()
	return nil
}

func NewHandlerWithServices(root string, services Services) (http.Handler, error) {
	if services.Access == nil || services.Databases == nil || services.Migrations == nil {
		return nil, errors.New("access, database and migration services are required")
	}
	templates, err := loadTemplates(filepath.Join(root, "frontend", "templates"))
	if err != nil {
		return nil, err
	}
	products, err := os.ReadFile(filepath.Join(root, "backend", "Products.json"))
	if err != nil || !json.Valid(products) {
		products = []byte("[]")
	}
	app := &application{services: services, templates: templates, productJSON: products,
		static: http.FileServer(http.Dir(filepath.Join(root, "frontend", "static"))),
		public: http.StripPrefix("/public/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "public")))),
	}
	return http.HandlerFunc(app.serveHTTP), nil
}

func loadTemplates(root string) (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"permission": func(p dao.Permission) string {
			if p == access.ViewEdit {
				return "View + Edit"
			}
			if p == access.ViewOnly {
				return "View Only"
			}
			return "No Access"
		},
		"initials": func(name string) string {
			var s []rune
			for _, word := range strings.Fields(name) {
				for _, c := range word {
					s = append(s, unicode.ToUpper(c))
					break
				}
				if len(s) == 2 {
					break
				}
			}
			return string(s)
		},
		"stamp":          func(t time.Time) string { return t.Format("02 Jan 2006 15:04 MST") },
		"rfc":            func(t time.Time) string { return t.Format(time.RFC3339Nano) },
		"json":           func(v any) string { b, _ := json.Marshal(v); return string(b) },
		"pretty":         func(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) },
		"schemaDiffView": BuildSchemaDiffView,
		"hasString":      slices.Contains[[]string, string],
		"cell": func(v any) string {
			if v == nil {
				return "NULL"
			}
			if b, ok := v.([]byte); ok {
				return string(b)
			}
			if n, ok := v.(*float64); ok && n != nil {
				return strconv.FormatFloat(*n, 'f', -1, 64)
			}
			return fmt.Sprint(v)
		},
		"link": link,
	}
	result := make(map[string]*template.Template)
	list := make([]page, 0, len(pages)+1)
	for _, p := range pages {
		list = append(list, p)
	}
	list = append(list, page{template: "error.gohtml", layout: "app_layout"})
	for _, p := range list {
		if _, ok := result[p.template]; ok {
			continue
		}
		files := []string{filepath.Join(root, "layouts", p.layout+".gohtml"), filepath.Join(root, "partials", "icons.gohtml"), filepath.Join(root, "partials", "sidebar.gohtml"), filepath.Join(root, "partials", "topbar.gohtml"), filepath.Join(root, "pages", p.template)}
		tmpl, err := template.New(p.layout + ".gohtml").Funcs(funcs).ParseFiles(files...)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", p.template, err)
		}
		result[p.template] = tmpl
	}
	return result, nil
}

func link(route string, args ...any) string {
	values := url.Values{}
	for i := 0; i+1 < len(args); i += 2 {
		values.Set(fmt.Sprint(args[i]), fmt.Sprint(args[i+1]))
	}
	if len(values) == 0 {
		return route
	}
	return route + "?" + values.Encode()
}

func (app *application) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
	if r.URL.Path == "/app.js" || r.URL.Path == "/app.css" {
		app.static.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/public/") {
		app.public.ServeHTTP(w, r)
		return
	}
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.EqualFold(strings.TrimSuffix(r.URL.Path, "/"), "/Products") {
		writeResponse(w, r, 200, "application/json; charset=utf-8", app.productJSON)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if target, ok := legacyRoutes[r.URL.Path]; ok {
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	if r.URL.Path == "/" || r.URL.Path == "/login" {
		app.login(w, r)
		return
	}
	p, exists := pages[r.URL.Path]
	if !exists && r.URL.Path != "/logout" {
		http.NotFound(w, r)
		return
	}
	identity, replacement, err := app.services.Access.Authenticate(r.Context(), cookieValue(r, access.CookieName))
	if err != nil {
		if errors.Is(err, access.ErrUnauthenticated) {
			app.clearCookie(w, r, access.CookieName)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		} else {
			http.Error(w, "Authentication is unavailable. Please retry.", 503)
		}
		return
	}
	if replacement != "" {
		app.setCookie(w, r, access.CookieName, replacement, identity.AbsoluteExpires)
	}
	csrf, err := app.services.Access.CSRFToken(identity)
	if err != nil {
		http.Error(w, "Session is unavailable", 401)
		return
	}
	data := pageData{Title: p.title, Active: p.active, Section: p.section, User: identity.User, CSRF: csrf, Now: time.Now(), Expires: identity.AbsoluteExpires}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
		if err := r.ParseForm(); err != nil {
			app.fail(w, r, data, access.ErrInvalid)
			return
		}
		if !sameOrigin(r) || app.services.Access.ValidateCSRF(identity, r.PostForm.Get("csrf")) != nil {
			app.fail(w, r, data, access.ErrForbidden)
			return
		}
	}
	if r.URL.Path == "/logout" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method not allowed", 405)
			return
		}
		if err := app.services.Access.Logout(r.Context(), identity.SessionID); err != nil {
			app.fail(w, r, data, err)
			return
		}
		app.clearCookie(w, r, access.CookieName)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/admin/") {
		if err := app.services.Access.RequireAdmin(r.Context(), identity.User.ID); err != nil {
			app.fail(w, r, data, err)
			return
		}
	}
	if r.URL.Path == "/admin/access" && r.Method != http.MethodPost {
		app.redirect(w, r, "/admin/groups", "")
		return
	}
	if r.Method == http.MethodPost {
		handled, err := app.action(w, r, &data)
		if err != nil {
			if errors.Is(err, access.ErrForbidden) || errors.Is(err, access.ErrNotFound) {
				app.fail(w, r, data, err)
				return
			}
			data.Error, data.Status = userError(err)
			data.Review = nil
			data.ReviewJSON = ""
		}
		if handled {
			return
		}
	}
	if err := app.loadPage(r, &data); err != nil {
		app.fail(w, r, data, err)
		return
	}
	status := http.StatusOK
	if data.Error != "" {
		status = data.Status
	}
	app.render(w, r, p, data, status)
}

func (app *application) render(w http.ResponseWriter, r *http.Request, p page, data pageData, status int) {
	var body bytes.Buffer
	if err := app.templates[p.template].ExecuteTemplate(&body, p.layout, data); err != nil {
		http.Error(w, "Page could not be rendered", 500)
		return
	}
	writeResponse(w, r, status, "text/html; charset=utf-8", body.Bytes())
}
func (app *application) fail(w http.ResponseWriter, r *http.Request, data pageData, err error) {
	data.Error, _ = userError(err)
	_, status := userError(err)
	data.Title = http.StatusText(status)
	app.render(w, r, page{template: "error.gohtml", layout: "app_layout"}, data, status)
}
func userError(err error) (string, int) {
	switch {
	case errors.Is(err, access.ErrForbidden):
		return "You do not have permission for this action.", 403
	case errors.Is(err, access.ErrNotFound):
		return "The requested item was not found.", 404
	case errors.Is(err, migration.ErrStale), errors.Is(err, dbmanagement.ErrStaleSnapshot):
		return "The SQL, database or connection changed. Analyze the migration again.", 409
	case errors.Is(err, dbmanagement.ErrDuplicateConnection):
		return "This database is already registered. Edit or reactivate its existing connection.", 409
	case errors.Is(err, access.ErrConflict):
		return "This item changed or already exists. Reload and try again.", 409
	case errors.Is(err, errUnsavedDraft):
		return "This draft has unsaved SQL. Save the draft, then analyze the saved SQL before execution.", 409
	case errors.Is(err, migration.ErrNonAtomicAcceptance):
		return "Accept the non-atomic execution warning before continuing.", 400
	case errors.Is(err, migration.ErrOutcomeUnknown), errors.Is(err, migration.ErrHistoryPending):
		return "The execution outcome needs reconciliation. Check the changelog before retrying.", 409
	case errors.Is(err, migration.ErrExecution):
		return "Migration failed. Check its changelog entry for the outcome and completed statements.", 400
	case errors.Is(err, diff.ErrDependency):
		return err.Error(), 409
	case errors.Is(err, diff.ErrNoChanges):
		return "The change set has no schema or data changes.", 400
	case errors.Is(err, dbmanagement.ErrReadOnly):
		return "SQL Viewer accepts one read-only SELECT query, including read-only CTEs and supported built-in functions. Writes, locking reads, output files and custom functions are not allowed.", 400
	case errors.Is(err, dbmanagement.ErrViewerResultSize):
		return "The result is too large. Select fewer columns, add a filter, or lower the LIMIT.", 400
	case errors.Is(err, dbmanagement.ErrViewerQuery):
		return "The read query failed. Check SQL syntax, table and column names, and database permissions, or simplify the query if it timed out.", 400
	case errors.Is(err, dbmanagement.ErrUnsupported):
		return "This operation is not supported for the selected database or schema.", 400
	case errors.Is(err, access.ErrInvalid):
		return "Check the supplied values and required fields.", 400
	case errors.Is(err, dbmanagement.ErrConnection):
		return "Cannot connect to the target database. Check its settings and availability.", 502
	case errors.Is(err, dbmanagement.ErrQuery):
		return "The database operation failed. Refresh the explorer and check the selected object.", 502
	default:
		return "The operation is temporarily unavailable. Please retry.", 500
	}
}
func writeResponse(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
func requestID(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return 0, access.ErrInvalid
	}
	return n, nil
}
func pageOffset(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > 100000000 {
		return 0, access.ErrInvalid
	}
	return n, nil
}
func (app *application) redirect(w http.ResponseWriter, r *http.Request, route, message string, args ...any) {
	args = append(args, "notice", message)
	http.Redirect(w, r, link(route, args...), http.StatusSeeOther)
}
