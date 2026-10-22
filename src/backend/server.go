package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const weatherURL = "https://api.open-meteo.com/v1/forecast?latitude=-6.2088&longitude=106.8456&current_weather=true"

type product struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Price       float64 `json:"price"`
	Image       string  `json:"image"`
}

type page struct {
	template string
	active   string
}

var pages = map[string]page{
	"/":            {"index.gohtml", "intro"},
	"/index.html":  {"index.gohtml", "intro"},
	"/index2.html": {"index2.gohtml", "products"},
	"/index3.html": {"index3.gohtml", "store"},
	"/index4.html": {"index4.gohtml", "social"},
	"/index5.html": {"index5.gohtml", "about"},
	"/index6.html": {"index6.gohtml", "weather"},
}

type application struct {
	templates   *template.Template
	productJSON json.RawMessage
	products    []product
	client      *http.Client
	static      http.Handler
	public      http.Handler
}

// NewHandler serves the frontend templates, assets, and existing API endpoints.
// root is the directory containing backend/ and frontend/ (the root src/ folder).
func NewHandler(root string) (http.Handler, error) {
	return newHandler(root, http.DefaultClient)
}

func newHandler(root string, client *http.Client) (http.Handler, error) {
	templates, err := template.ParseGlob(filepath.Join(root, "frontend", "templates", "*.gohtml"))
	if err != nil {
		return nil, fmt.Errorf("parse frontend templates: %w", err)
	}
	productJSON, products := loadProducts(filepath.Join(root, "backend", "Products.json"))

	app := &application{
		templates:   templates,
		productJSON: productJSON,
		products:    products,
		client:      client,
		static:      http.FileServer(http.Dir(filepath.Join(root, "frontend", "static"))),
		public:      http.StripPrefix("/public/", http.FileServer(http.Dir(filepath.Join(root, "frontend", "public")))),
	}
	return http.HandlerFunc(app.serveHTTP), nil
}

// Load product data once at startup, retaining the original [] fallback on error.
func loadProducts(filename string) (json.RawMessage, []product) {
	data, err := os.ReadFile(filename)
	var compact bytes.Buffer
	if err == nil {
		err = json.Compact(&compact, data)
	}
	if err != nil {
		log.Printf("Failed to read Products.json: %v", err)
		return json.RawMessage("[]"), nil
	}

	var catalog struct {
		Products []product `json:"products"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		log.Printf("Failed to decode product cards: %v", err)
	}
	return json.RawMessage(compact.Bytes()), catalog.Products
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
	case strings.EqualFold(route, "/weather"):
		app.serveWeather(w, r)
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
		if ext := path.Ext(r.URL.Path); ext == ".css" || ext == ".js" {
			app.static.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

func (app *application) servePage(w http.ResponseWriter, r *http.Request, page page) {
	data := struct {
		Active   string
		Products []product
	}{Active: page.active, Products: app.products}

	var rendered bytes.Buffer
	if err := app.templates.ExecuteTemplate(&rendered, page.template, data); err != nil {
		log.Printf("Failed to render page: %v", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
	writeResponse(w, r, http.StatusOK, "text/html; charset=utf-8", rendered.Bytes())
}

func (app *application) serveWeather(w http.ResponseWriter, r *http.Request) {
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, weatherURL, nil)
	if err != nil {
		weatherError(w, r, err)
		return
	}
	response, err := app.client.Do(request)
	if err != nil {
		weatherError(w, r, err)
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		weatherError(w, r, err)
		return
	}
	var weather struct {
		CurrentWeather json.RawMessage `json:"current_weather"`
	}
	if err := json.Unmarshal(body, &weather); err != nil {
		weatherError(w, r, err)
		return
	}
	writeResponse(w, r, http.StatusOK, "application/json; charset=utf-8", weather.CurrentWeather)
}

func weatherError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("Failed to fetch weather data: %v", err)
	writeResponse(w, r, http.StatusInternalServerError, "text/html; charset=utf-8", []byte("Failed to fetch weather data"))
}

func writeResponse(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
