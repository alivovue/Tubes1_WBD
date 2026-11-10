// Package httpapi provides shared JSON, cookie, and CSRF handling for the two
// HTTP applications. Each server supplies its own cookie names and role boundary.
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"tubes1_wbd/backend/access"
)

func SecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
}
func Write(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
func JSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		Error(w, r, "The response could not be encoded.", http.StatusInternalServerError)
		return
	}
	Write(w, r, status, "application/json; charset=utf-8", body)
}
func Error(w http.ResponseWriter, r *http.Request, message string, status int) {
	JSON(w, r, status, map[string]string{"error": message})
}
func ParseAction(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return access.ErrInvalid
	}
	var values map[string]json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&values) != nil || values == nil {
		return access.ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return access.ErrInvalid
	}
	r.PostForm = url.Values{}
	for key, value := range values {
		var single string
		if string(value) != "null" && json.Unmarshal(value, &single) == nil {
			r.PostForm.Set(key, single)
			continue
		}
		var multiple []string
		if json.Unmarshal(value, &multiple) != nil || multiple == nil {
			return access.ErrInvalid
		}
		r.PostForm[key] = multiple
	}
	return nil
}
func CookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}
func SameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && parsed.Host == r.Host && (parsed.Scheme == scheme || parsed.Scheme == "https") && parsed.User == nil
}
