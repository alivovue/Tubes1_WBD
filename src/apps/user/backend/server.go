package backend

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"schakker_shared/httpapi"
	"schakker_shared/webapp"

	"tubes1_wbd/backend/api"
)

var legacyRoutes = map[string]string{
	"/index.html": "/", "/index2.html": "/databases", "/index3.html": "/databases",
	"/index4.html": "/dashboard", "/index5.html": "/dashboard",
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
	products, err := os.ReadFile(filepath.Join(root, "backend", "Products.json"))
	if err != nil || !json.Valid(products) {
		products = []byte("[]")
	}
	return webapp.New(webapp.Config{
		Root: root, BuildWorkspace: "schakker-frontend",
		API:     api.NewHandler(services.Access, services.Databases, services.Migrations, services.Production, services.Jobs),
		HasPage: api.HasPage, Redirects: legacyRoutes,
		PublicResponse: func(w http.ResponseWriter, r *http.Request) bool {
			if (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.EqualFold(strings.TrimSuffix(r.URL.Path, "/"), "/Products") {
				httpapi.Write(w, r, http.StatusOK, "application/json; charset=utf-8", products)
				return true
			}
			return false
		},
	})
}
