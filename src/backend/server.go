package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type page struct {
	template string
	layout   string
	title    string
	active   string
	section  string
}

var pages = map[string]page{
	"/":                  {"login.gohtml", "auth_layout", "Masuk", "login", "Authentication"},
	"/login":             {"login.gohtml", "auth_layout", "Masuk", "login", "Authentication"},
	"/dashboard":         {"dashboard.gohtml", "app_layout", "Dashboard", "dashboard", "Overview"},
	"/databases":         {"databases.gohtml", "app_layout", "My Databases", "databases", "Workspace"},
	"/explorer":          {"explorer.gohtml", "app_layout", "Database Explorer", "databases", "Workspace"},
	"/migrations/new":    {"migration_editor.gohtml", "app_layout", "SQL Migration", "migration-editor", "Workspace"},
	"/migrations":        {"migration_history.gohtml", "app_layout", "Changelog", "migration-history", "Workspace"},
	"/admin/users":       {"admin_users.gohtml", "app_layout", "Employees", "admin-users", "Administration"},
	"/admin/groups":      {"admin_groups.gohtml", "app_layout", "Groups", "admin-groups", "Administration"},
	"/admin/connections": {"admin_connections.gohtml", "app_layout", "Database Connections", "admin-connections", "Administration"},
	"/admin/access":      {"admin_access.gohtml", "app_layout", "Access Control", "admin-access", "Administration"},
}

var legacyRoutes = map[string]string{
	"/index.html":  "/",
	"/index2.html": "/databases",
	"/index3.html": "/databases",
	"/index4.html": "/dashboard",
	"/index5.html": "/dashboard",
}

type application struct {
	templates   map[string]*template.Template
	productJSON json.RawMessage
	static      http.Handler
	public      http.Handler
}

// NewHandler serves the frontend templates, assets, and existing API endpoints.
// root is the directory containing backend/ and frontend/ (the root src/ folder).
func NewHandler(root string) (http.Handler, error) {
	templates, err := loadTemplates(filepath.Join(root, "frontend", "templates"))
	if err != nil {
		return nil, fmt.Errorf("parse frontend templates: %w", err)
	}
	productJSON := loadProducts(filepath.Join(root, "backend", "Products.json"))

	app := &application{
		templates:   templates,
		productJSON: productJSON,
		static:      http.FileServer(http.Dir(filepath.Join(root, "frontend", "static"))),
		public:      http.StripPrefix("/public/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "public")))),
	}
	return http.HandlerFunc(app.serveHTTP), nil
}

// Parse every page with its layout and shared partials. Keeping each page in a
// separate template set lets all page files define the same "content" block.
func loadTemplates(root string) (map[string]*template.Template, error) {
	shared := []string{
		filepath.Join(root, "partials", "icons.gohtml"),
		filepath.Join(root, "partials", "sidebar.gohtml"),
		filepath.Join(root, "partials", "topbar.gohtml"),
	}
	parsed := make(map[string]*template.Template)
	for _, page := range pages {
		if _, exists := parsed[page.template]; exists {
			continue
		}
		files := []string{filepath.Join(root, "layouts", page.layout+".gohtml")}
		files = append(files, shared...)
		files = append(files, filepath.Join(root, "pages", page.template))
		tmpl, err := template.ParseFiles(files...)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", page.template, err)
		}
		parsed[page.template] = tmpl
	}
	return parsed, nil
}

// Load product data once at startup, retaining the original [] fallback on error.
func loadProducts(filename string) json.RawMessage {
	data, err := os.ReadFile(filename)
	var compact bytes.Buffer
	if err == nil {
		err = json.Compact(&compact, data)
	}
	if err != nil {
		log.Printf("Failed to read Products.json: %v", err)
		return json.RawMessage("[]")
	}
	return json.RawMessage(compact.Bytes())
}

func (app *application) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}

	// Express accepted API paths with either casing and an optional trailing slash.
	route := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case strings.EqualFold(route, "/Products"):
		writeResponse(w, r, http.StatusOK, "application/json; charset=utf-8", app.productJSON)
	case strings.HasPrefix(r.URL.Path, "/public/"):
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		app.public.ServeHTTP(w, r)
	default:
		if page, ok := pages[r.URL.Path]; ok {
			app.servePage(w, r, page)
			return
		}
		if target, ok := legacyRoutes[r.URL.Path]; ok {
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		if ext := path.Ext(r.URL.Path); ext == ".css" || ext == ".js" {
			app.static.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

func (app *application) servePage(w http.ResponseWriter, r *http.Request, page page) {
	data := struct {
		Title   string
		Active  string
		Section string
	}{Title: page.title, Active: page.active, Section: page.section}

	var rendered bytes.Buffer
	tmpl, ok := app.templates[page.template]
	if !ok {
		http.Error(w, "Page template is unavailable", http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(&rendered, page.layout, data); err != nil {
		log.Printf("Failed to render page: %v", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
	writeResponse(w, r, http.StatusOK, "text/html; charset=utf-8", rendered.Bytes())
}

func writeResponse(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
