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
	p := pages["/login"]
	data := pageData{Title: p.title, Active: p.active, Section: p.section}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if r.ParseForm() != nil || !sameOrigin(r) || app.services.Access.ValidateLoginCSRF(cookieValue(r, loginCookie), r.PostForm.Get("csrf")) != nil {
			data.Error = "The login form expired. Please try again."
			status = http.StatusForbidden
		} else {
			identity, raw, err := app.services.Access.Login(r.Context(), r.PostForm.Get("username"), r.PostForm.Get("password"), cookieValue(r, access.CookieName))
			if err == nil {
				app.setCookie(w, r, access.CookieName, raw, identity.AbsoluteExpires)
				app.clearCookie(w, r, loginCookie)
				http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
				return
			}
			if errors.Is(err, access.ErrUnauthenticated) {
				data.Error = "Incorrect username or password, or the account is disabled."
				status = http.StatusUnauthorized
			} else {
				data.Error = "Login is temporarily unavailable. Please retry."
				status = http.StatusServiceUnavailable
			}
		}
	} else if raw := cookieValue(r, access.CookieName); raw != "" {
		identity, replacement, err := app.services.Access.Authenticate(r.Context(), raw)
		if err == nil {
			if replacement != "" {
				app.setCookie(w, r, access.CookieName, replacement, identity.AbsoluteExpires)
			}
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		if !errors.Is(err, access.ErrUnauthenticated) {
			http.Error(w, "Authentication is temporarily unavailable", 503)
			return
		}
		app.clearCookie(w, r, access.CookieName)
	}
	challenge, err := app.services.Access.NewLoginChallenge()
	if err != nil {
		http.Error(w, "Login is temporarily unavailable", 503)
		return
	}
	app.setCookie(w, r, loginCookie, challenge.CookieValue, challenge.Expires)
	data.CSRF = challenge.Token
	app.render(w, r, p, data, status)
}
