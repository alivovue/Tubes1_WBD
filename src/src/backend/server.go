package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"schakker_shared/httpapi"

	"tubes1_wbd/backend/api"
)

var legacyRoutes = map[string]string{
	"/index.html": "/", "/index2.html": "/databases", "/index3.html": "/databases",
	"/index4.html": "/dashboard", "/index5.html": "/dashboard",
}

type application struct {
	entryHTML, productJSON []byte
	assets, public, api    http.Handler
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

func (h *managedHandler) Close() error { h.close(); return nil }
func NewHandlerWithServices(root string, services Services) (http.Handler, error) {
	if services.Access == nil || services.Databases == nil || services.Migrations == nil {
		return nil, errors.New("access, database and migration services are required")
	}
	entryHTML, err := os.ReadFile(filepath.Join(root, "frontend", "dist", "index.html"))
	if err != nil {
		return nil, fmt.Errorf("run npm ci and npm run build --workspace=schakker-frontend from the source workspace root (src/): %w", err)
	}
	products, err := os.ReadFile(filepath.Join(root, "backend", "Products.json"))
	if err != nil || !json.Valid(products) {
		products = []byte("[]")
	}
	app := &application{api: api.NewHandler(services.Access, services.Databases, services.Migrations, services.Production), entryHTML: entryHTML, productJSON: products,
		assets: http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "dist", "assets")))),
		public: http.StripPrefix("/public/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "public")))),
	}
	return http.HandlerFunc(app.serveHTTP), nil
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
		httpapi.Write(w, r, 200, "application/json; charset=utf-8", app.productJSON)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		// Public API routes retain the SPA's existing endpoint names.
		request := r.Clone(r.Context())
		request.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		app.api.ServeHTTP(w, request)
		return
	}
	if target, ok := legacyRoutes[r.URL.Path]; ok {
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		httpapi.Error(w, r, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !api.HasPage(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	httpapi.Write(w, r, http.StatusOK, "text/html; charset=utf-8", app.entryHTML)
}
