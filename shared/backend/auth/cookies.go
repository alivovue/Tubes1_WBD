package auth

import (
	"net/http"
	"time"
)

func SetCookie(w http.ResponseWriter, r *http.Request, production bool, name, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, HttpOnly: true, Secure: production || r.TLS != nil, SameSite: http.SameSiteLaxMode})
}
func ClearCookie(w http.ResponseWriter, r *http.Request, production bool, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/", Expires: time.Unix(1, 0), MaxAge: -1, HttpOnly: true, Secure: production || r.TLS != nil, SameSite: http.SameSiteLaxMode})
}
