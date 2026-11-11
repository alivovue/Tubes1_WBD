package backend

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"schakker_shared/observability"
	"strings"

	"schakker_shared/httpapi"

	"tubes1_wbd_admin/backend/api"
)

type application struct {
	entry               []byte
	assets, public, api http.Handler
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
		return nil, fmt.Errorf("run npm ci and npm run build --workspace=schakker-admin-frontend from the source workspace root (src/): %w", err)
	}
	app := &application{api: api.NewHandler(services.Authentication, services.Access, services.Connections, services.Production), entry: entry,
		assets: http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "dist", "assets")))),
		public: http.StripPrefix("/public/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "public")))),
	}
	return observability.Middleware(http.HandlerFunc(app.serveHTTP)), nil
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
		app.api.ServeHTTP(w, request)
		return
	}
	if !api.HasPage(r.URL.Path) {
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
