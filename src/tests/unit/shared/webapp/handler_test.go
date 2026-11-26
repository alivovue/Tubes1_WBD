package webapp_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"schakker_shared/webapp"
)

func TestApplicationsKeepIndependentRoutesAssetsAndAPIs(t *testing.T) {
	apps := make([]http.Handler, 2)
	for i, name := range []string{"user", "admin"} {
		root := t.TempDir()
		for file, content := range map[string]string{
			"frontend/dist/index.html":     name + " shell",
			"frontend/dist/assets/app.js":  name + " bundle",
			"frontend/public/identity.txt": name + " public",
		} {
			path := filepath.Join(root, file)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		handler, err := webapp.New(webapp.Config{
			Root: root, BuildWorkspace: name,
			HasPage: func(path string) bool { return path == "/"+name },
			API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/resource" || r.URL.Query().Get("database_id") != "7" || r.Header.Get("X-CSRF-Token") != "own-csrf" {
					t.Error("API prefix removal changed request data")
				}
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(name + " API denial"))
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		apps[i] = handler
	}
	for i, name := range []string{"user", "admin"} {
		other := []string{"admin", "user"}[i]
		for _, test := range []struct {
			method, path, body string
			status             int
		}{
			{"GET", "/" + name, name + " shell", 200},
			{"HEAD", "/" + name, "", 200},
			{"POST", "/" + name, "Method not allowed", 405},
			{"GET", "/" + other, "404", 404},
			{"GET", "/assets/app.js", name + " bundle", 200},
			{"GET", "/public/identity.txt", name + " public", 200},
			{"GET", "/assets/missing.js", "404", 404},
			{"POST", "/api/resource?database_id=7", name + " API denial", 403},
		} {
			request := httptest.NewRequest(test.method, test.path, nil)
			originalPath := request.URL.Path
			request.Header.Set("X-CSRF-Token", "own-csrf")
			response := httptest.NewRecorder()
			apps[i].ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.body) || (test.method == "HEAD" && response.Body.Len() != 0) {
				t.Fatalf("%s %s on %s: %d %q", test.method, test.path, name, response.Code, response.Body.String())
			}
			if request.URL.Path != originalPath {
				t.Fatal("API routing mutated the caller's request")
			}
			if !strings.HasPrefix(test.path, "/assets/") && !strings.HasPrefix(test.path, "/public/") && response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("application or API response lost its cache policy")
			}
			if response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("response lost security headers")
			}
		}
	}
}
