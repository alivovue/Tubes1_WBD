package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
		{"/", "Schakker", "Pengantar"},
		{"/index.html", "Schakker", "Pengantar"},
		{"/index2.html", "Produk", "Produk"},
		{"/index3.html", "Toko", "Toko"},
		{"/index4.html", "Medsos", "Medsos"},
		{"/index5.html", "Tentang Kami", "Tentang Kami"},
		{"/index6.html", "Weather API", "Cuaca"},
	} {
		t.Run(page.route, func(t *testing.T) {
			response := request(handler, http.MethodGet, page.route)
			body := response.Body.String()
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, body)
			}
			for _, expected := range []string{
				"<title>" + page.title + "</title>",
				`href="#" class="active">` + page.active + "</a>",
				`<footer class="footer">`,
			} {
				if !strings.Contains(body, expected) {
					t.Errorf("missing %q", expected)
				}
			}
			if strings.Contains(body, "{{") || strings.Count(body, `class="active"`) != 1 {
				t.Error("template actions remain or active navigation is incorrect")
			}
		})
	}
	store := request(handler, http.MethodGet, "/index3.html").Body.String()
	var catalog struct{ Products []product }
	data, err := os.ReadFile("Products.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(store, `class="product"`); count != len(catalog.Products) {
		t.Errorf("rendered %d product cards, want %d", count, len(catalog.Products))
	}
	if strings.Contains(store, "script.js") {
		t.Error("store still depends on the replaced product rendering script")
	}
	weather := request(handler, http.MethodGet, "/index6.html").Body.String()
	if !strings.Contains(weather, "Loading weather...") || !strings.Contains(weather, `src="script2.js"`) {
		t.Error("weather loading behavior changed")
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
	for _, route := range []string{"/", "/index3.html", "/Products"} {
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
		"/backend/Products.json", "/frontend/templates/index.gohtml", "/public/",
	} {
		if response := request(handler, http.MethodGet, route); response.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", route, response.Code)
		}
	}
}

func fixtureRoot(t *testing.T, products []byte) string {
	t.Helper()
	root := t.TempDir()
	templateDir := filepath.Join(root, "frontend", "templates")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	templates, err := filepath.Glob(filepath.Join("..", "frontend", "templates", "*.gohtml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range templates {
		contents, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(templateDir, filepath.Base(filename)), contents, 0o644); err != nil {
			t.Fatal(err)
		}
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
			store := request(handler, http.MethodGet, "/index3.html")
			if store.Code != http.StatusOK || strings.Contains(store.Body.String(), `class="product"`) {
				t.Error("store should render without product cards when data is unavailable")
			}
		})
	}
}

func TestProductSnapshotAndTemplateEscaping(t *testing.T) {
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
	body := request(handler, http.MethodGet, "/index3.html").Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") || !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("product title must be rendered as text")
	}
	if !strings.Contains(body, "&lt;b&gt;Text&lt;/b&gt;\nSecond line") || !strings.Contains(body, "$9899.99") {
		t.Error("product descriptions or prices changed")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestWeatherProxy(t *testing.T) {
	for _, test := range []struct {
		name, upstream, expected string
		failure                  error
		upstreamStatus, status   int
	}{
		{name: "current weather only", upstream: `{"latitude":-6.2,"current_weather":{"temperature":30.5,"windspeed":12,"weathercode":3}}`, expected: `{"temperature":30.5,"windspeed":12,"weathercode":3}`, upstreamStatus: 200, status: 200},
		{name: "upstream status preserves JSON handling", upstream: `{"current_weather":{"temperature":28}}`, expected: `{"temperature":28}`, upstreamStatus: 503, status: 200},
		{name: "missing current weather", upstream: `{"latitude":-6.2}`, upstreamStatus: 200, status: 200},
		{name: "invalid upstream JSON", upstream: `not JSON`, expected: "Failed to fetch weather data", upstreamStatus: 200, status: 500},
		{name: "unreachable upstream", failure: errors.New("upstream unavailable"), expected: "Failed to fetch weather data", status: 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodGet || request.URL.String() != weatherURL {
					t.Errorf("unexpected upstream request: %s %s", request.Method, request.URL)
				}
				if test.failure != nil {
					return nil, test.failure
				}
				return &http.Response{StatusCode: test.upstreamStatus, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.upstream))}, nil
			})}
			handler, err := newHandler("..", client)
			if err != nil {
				t.Fatal(err)
			}
			response := request(handler, http.MethodGet, "/WEATHER/")
			if response.Code != test.status || response.Body.String() != test.expected {
				t.Errorf("response = %d %s, want %d %s", response.Code, response.Body.String(), test.status, test.expected)
			}
		})
	}
}
