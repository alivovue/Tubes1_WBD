package backend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func request(handler http.Handler, method, route string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, route, nil))
	return response
}

func TestPageTemplates(t *testing.T) {
	handler, err := NewHandler("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []struct{ route, title, active string }{
		{"/", "Masuk", "Employee portal"},
		{"/login", "Masuk", "Employee portal"},
		{"/dashboard", "Dashboard", `href="/dashboard" aria-current="page"`},
		{"/databases", "My Databases", `href="/databases" aria-current="page"`},
		{"/explorer", "Database Explorer", `href="/databases" aria-current="page"`},
		{"/migrations/new", "SQL Migration", `href="/migrations/new" aria-current="page"`},
		{"/migrations", "Changelog", `href="/migrations" aria-current="page"`},
		{"/admin/users", "Employees", `href="/admin/users" aria-current="page"`},
		{"/admin/groups", "Groups", `href="/admin/groups" aria-current="page"`},
		{"/admin/connections", "Database Connections", `href="/admin/connections" aria-current="page"`},
		{"/admin/access", "Access Control", `href="/admin/access" aria-current="page"`},
	} {
		t.Run(page.route, func(t *testing.T) {
			response := request(handler, http.MethodGet, page.route)
			body := response.Body.String()
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, body)
			}
			for _, expected := range []string{
				"<title>" + page.title + " · Schakker Data Platform</title>",
				page.active,
			} {
				if !strings.Contains(body, expected) {
					t.Errorf("missing %q", expected)
				}
			}
			if strings.Contains(body, "{{") {
				t.Error("template actions remain in rendered page")
			}
		})
	}
	for route, target := range legacyRoutes {
		response := request(handler, http.MethodGet, route)
		if response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != target {
			t.Errorf("legacy route %s = %d %q, want 301 %q", route, response.Code, response.Header().Get("Location"), target)
		}
	}
}

func TestFrontendDoesNotClaimUnavailableBackendWork(t *testing.T) {
	handler, err := NewHandler("..")
	if err != nil {
		t.Fatal(err)
	}

	for route, expected := range map[string]string{
		"/login":             "Analysis backend not connected",
		"/explorer":          "data-object-unavailable",
		"/migrations/new":    "Draft not saved",
		"/admin/connections": "Not verified",
	} {
		body := request(handler, http.MethodGet, route).Body.String()
		if !strings.Contains(body, expected) {
			t.Errorf("%s: missing backend-unavailable state %q", route, expected)
		}
	}

	script, err := os.ReadFile(filepath.Join("..", "frontend", "static", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(script)
	for _, forbidden := range []string{"window.location.assign", "Connection test queued", "All systems operational"} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("frontend still contains unsupported success behavior %q", forbidden)
		}
	}
}

func TestProductAPI(t *testing.T) {
	handler, err := NewHandler("..")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("Products.json")
	if err != nil {
		t.Fatal(err)
	}
	var expectedJSON any
	if err := json.Unmarshal(expected, &expectedJSON); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/Products", "/Products/", "/products", "/PRODUCTS/?page=1"} {
		response := request(handler, http.MethodGet, route)
		var actualJSON any
		if err := json.Unmarshal(response.Body.Bytes(), &actualJSON); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || !reflect.DeepEqual(actualJSON, expectedJSON) {
			t.Errorf("%s: product response changed", route)
		}
		if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: missing JSON content type", route)
		}
	}
	for _, route := range []string{"/", "/dashboard", "/Products"} {
		response := request(handler, http.MethodHead, route)
		if response.Code != http.StatusOK || response.Body.Len() != 0 {
			t.Errorf("HEAD %s: status = %d, body length = %d", route, response.Code, response.Body.Len())
		}
	}
	if response := request(handler, http.MethodPost, "/Products"); response.Code != http.StatusNotFound {
		t.Errorf("POST /Products: status = %d, want 404", response.Code)
	}
}

func TestAssetsAndPrivateFiles(t *testing.T) {
	handler, err := NewHandler("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"static", "public"} {
		files, err := filepath.Glob(filepath.Join("..", "frontend", directory, "*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no files in %s", directory)
		}
		for _, filename := range files {
			route := "/" + filepath.Base(filename)
			if directory == "public" {
				route = "/public" + route
			}
			response := request(handler, http.MethodGet, route)
			expected, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), expected) {
				t.Errorf("%s: static asset response changed", route)
			}
		}
	}
	for _, route := range []string{
		"/missing", "/Products.json", "/go.mod", "/main.go", "/backend/server.go",
		"/backend/Products.json", "/frontend/templates/pages/dashboard.gohtml", "/public/",
		"/index6.html", "/weather", "/script2.js", "/style6.css",
	} {
		if response := request(handler, http.MethodGet, route); response.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", route, response.Code)
		}
	}
}

func fixtureRoot(t *testing.T, products []byte) string {
	t.Helper()
	root := t.TempDir()
	templateSource := filepath.Join("..", "frontend", "templates")
	templateDir := filepath.Join(root, "frontend", "templates")
	if err := filepath.WalkDir(templateSource, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(templateSource, filename)
		if err != nil {
			return err
		}
		target := filepath.Join(templateDir, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if products != nil {
		if err := os.WriteFile(filepath.Join(root, "backend", "Products.json"), products, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestProductFallback(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"missing file", nil},
		{"invalid JSON", []byte("{broken JSON")},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, err := NewHandler(fixtureRoot(t, test.data))
			if err != nil {
				t.Fatal(err)
			}
			response := request(handler, http.MethodGet, "/Products")
			if response.Code != http.StatusOK || response.Body.String() != "[]" {
				t.Errorf("fallback = %d %s, want 200 []", response.Code, response.Body.String())
			}
		})
	}
}

func TestProductSnapshot(t *testing.T) {
	initial := []byte(`{"products":[{"id":1,"title":"<script>alert(1)</script>","description":"<b>Text</b>\nSecond line","price":9899.99,"image":"public/image11.png"}]}`)
	root := fixtureRoot(t, initial)
	handler, err := NewHandler(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "Products.json"), []byte(`{"products":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	response := request(handler, http.MethodGet, "/Products")
	if !bytes.Equal(response.Body.Bytes(), initial) {
		t.Error("products should remain the snapshot loaded at startup")
	}
}
