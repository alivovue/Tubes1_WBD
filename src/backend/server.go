package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/httpapi"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/diff"
	"tubes1_wbd/database/dao"
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
	entryHTML      []byte
	assets, public http.Handler
	productJSON    []byte
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
	SchemaDiff                                     *SchemaDiffView
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
	entryHTML, err := os.ReadFile(filepath.Join(root, "frontend", "dist", "index.html"))
	if err != nil {
		return nil, fmt.Errorf("build the React frontend with npm ci and npm run build in frontend/: %w", err)
	}
	products, err := os.ReadFile(filepath.Join(root, "backend", "Products.json"))
	if err != nil || !json.Valid(products) {
		products = []byte("[]")
	}
	app := &application{services: services, entryHTML: entryHTML, productJSON: products,
		assets: http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "dist", "assets")))),
		public: http.StripPrefix("/public/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "public")))),
	}
	return http.HandlerFunc(app.serveHTTP), nil
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
	httpapi.SecurityHeaders(w)
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		app.assets.ServeHTTP(w, r)
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
	if strings.HasPrefix(r.URL.Path, "/api/") {
		// Reuse the existing page loaders and actions with their original route names.
		request := r.Clone(r.Context())
		request.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		app.serveAPI(w, request)
		return
	}
	if target, ok := legacyRoutes[r.URL.Path]; ok {
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		app.jsonError(w, r, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, exists := pages[r.URL.Path]; !exists {
		http.NotFound(w, r)
		return
	}
	writeResponse(w, r, http.StatusOK, "text/html; charset=utf-8", app.entryHTML)
}

func (app *application) serveAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/login" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			app.jsonError(w, r, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		app.login(w, r)
		return
	}
	p, exists := pages[r.URL.Path]
	if !exists && r.URL.Path != "/logout" && r.URL.Path != "/session" {
		app.jsonError(w, r, "The requested endpoint was not found.", http.StatusNotFound)
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
		app.jsonError(w, r, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	identity, csrf, ok := app.authHTTP().Authenticate(w, r, r.URL.Path == "/session")
	if !ok {
		return
	}
	data := pageData{Title: p.title, Active: p.active, Section: p.section, User: identity.User, CSRF: csrf, Now: time.Now(), Expires: identity.AbsoluteExpires}
	if r.URL.Path == "/session" {
		app.json(w, r, http.StatusOK, data)
		return
	}
	if r.Method == http.MethodPost {
		if err := app.authHTTP().ValidateAction(w, r, identity); err != nil {
			app.fail(w, r, data, err)
			return
		}
	}
	if r.URL.Path == "/logout" {
		app.authHTTP().Logout(w, r, identity)
		return
	}
	if r.Method == http.MethodPost {
		handled, err := app.action(w, r, &data)
		if err != nil {
			app.fail(w, r, data, err)
			return
		}
		if handled {
			return
		}
	}
	if err := app.loadPage(r, &data); err != nil {
		app.fail(w, r, data, err)
		return
	}
	if data.Review != nil && data.Review.SchemaDiff != nil {
		view := BuildSchemaDiffView(data.Review.SchemaDiff.Current, data.Review.SchemaDiff.Desired)
		data.SchemaDiff = &view
	}
	// SQL drivers may return textual values as byte slices. Preserve the old
	// display semantics, including exact large integers and non-finite floats.
	if data.Data != nil {
		normalizeDisplayRows(data.Data.Rows)
	}
	if data.ViewerResult != nil {
		normalizeDisplayRows(data.ViewerResult.Rows)
	}
	app.json(w, r, http.StatusOK, data)
}

func normalizeDisplayRows(rows [][]any) {
	for _, row := range rows {
		for i, value := range row {
			if bytes, ok := value.([]byte); ok {
				row[i] = string(bytes)
			} else if value != nil {
				row[i] = fmt.Sprint(value)
			}
		}
	}
}

func (app *application) json(w http.ResponseWriter, r *http.Request, status int, value any) {
	httpapi.JSON(w, r, status, value)
}
func (app *application) jsonError(w http.ResponseWriter, r *http.Request, message string, status int) {
	httpapi.Error(w, r, message, status)
}
func (app *application) fail(w http.ResponseWriter, r *http.Request, data pageData, err error) {
	message, status := userError(err)
	app.jsonError(w, r, message, status)
}
func userError(err error) (string, int) {
	switch {
	case errors.Is(err, access.ErrForbidden):
		return "You do not have permission for this action.", 403
	case errors.Is(err, access.ErrNotFound):
		return "The requested item was not found.", 404
	case errors.Is(err, migration.ErrStale), errors.Is(err, dbmanagement.ErrStaleSnapshot):
		return "The SQL, database or connection changed. Analyze the migration again.", 409
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
	case errors.Is(err, migration.ErrUnsafeSQL):
		return "Migration SQL cannot run procedural blocks, define executable routines, change session settings, use dynamic SQL helpers, or access internal migration metadata. Use explicit schema or data statements.", 400
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
	httpapi.Write(w, r, status, contentType, body)
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
	app.json(w, r, http.StatusOK, map[string]string{"location": link(route, args...)})
}
