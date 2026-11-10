package backend

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"tubes1_wbd/backend/access"
)

const loginCookie = "schakker_login_csrf"

func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
func sameOrigin(r *http.Request) bool {
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
func (app *application) setCookie(w http.ResponseWriter, r *http.Request, name, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, HttpOnly: true, Secure: app.services.Production || r.TLS != nil, SameSite: http.SameSiteLaxMode})
}
func (app *application) clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", Expires: time.Unix(1, 0), MaxAge: -1, HttpOnly: true, Secure: app.services.Production || r.TLS != nil, SameSite: http.SameSiteLaxMode})
}
func (app *application) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if parseActionJSON(w, r) != nil || !sameOrigin(r) || app.services.Access.ValidateLoginCSRF(cookieValue(r, loginCookie), r.Header.Get("X-CSRF-Token")) != nil {
		app.loginChallenge(w, r, "The login form expired. Please try again.", http.StatusForbidden)
		return
	}
	identity, raw, err := app.services.Access.Login(r.Context(), r.PostForm.Get("username"), r.PostForm.Get("password"), cookieValue(r, access.CookieName))
	if err != nil {
		if errors.Is(err, access.ErrUnauthenticated) {
			app.loginChallenge(w, r, "Incorrect username or password, or the account is disabled.", http.StatusUnauthorized)
		} else {
			app.loginChallenge(w, r, "Login is temporarily unavailable. Please retry.", http.StatusServiceUnavailable)
		}
		return
	}
	app.setCookie(w, r, access.CookieName, raw, identity.AbsoluteExpires)
	app.clearCookie(w, r, loginCookie)
	app.redirect(w, r, "/dashboard", "")
}

func (app *application) loginChallenge(w http.ResponseWriter, r *http.Request, message string, status int) {
	challenge, err := app.services.Access.NewLoginChallenge()
	if err != nil {
		app.jsonError(w, r, "Login is temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	app.setCookie(w, r, loginCookie, challenge.CookieValue, challenge.Expires)
	app.json(w, r, status, map[string]string{"CSRF": challenge.Token, "error": message})
}
