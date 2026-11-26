// Package webapp serves a built React application and its owning backend API.
package webapp

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"schakker_shared/httpapi"
	"schakker_shared/observability"
)

type Config struct {
	Root, BuildWorkspace string
	API                  http.Handler
	HasPage              func(string) bool
	Redirects            map[string]string
	// PublicResponse serves optional application-specific public endpoints.
	// It returns true only when it has handled the request.
	PublicResponse func(http.ResponseWriter, *http.Request) bool
}

func New(config Config) (http.Handler, error) {
	if config.API == nil || config.HasPage == nil {
		return nil, errors.New("API handler and page routes are required")
	}
	entry, err := os.ReadFile(filepath.Join(config.Root, "frontend", "dist", "index.html"))
	if err != nil {
		return nil, fmt.Errorf("run npm ci and npm run build --workspace=%s from the repository root: %w", config.BuildWorkspace, err)
	}
	assets := http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(config.Root, "frontend", "dist", "assets"))))
	public := http.StripPrefix("/public/", http.FileServer(http.Dir(filepath.Join(config.Root, "frontend", "public"))))
	return observability.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.SecurityHeaders(w)
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			assets.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/public/") {
			public.ServeHTTP(w, r)
			return
		}
		if config.PublicResponse != nil && config.PublicResponse(w, r) {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			request := r.Clone(r.Context())
			request.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
			config.API.ServeHTTP(w, request)
			return
		}
		if target, ok := config.Redirects[r.URL.Path]; ok {
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		if !config.HasPage(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			httpapi.Error(w, r, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		httpapi.Write(w, r, http.StatusOK, "text/html; charset=utf-8", entry)
	})), nil
}
