package httpapi

import (
	"errors"
	"net/http"
	"time"

	"tubes1_wbd_admin/backend/access"
)

const AdminCookieName = "schakker_admin_session"
const AdminLoginCookieName = "schakker_admin_login_csrf"
const UserLoginCookieName = "schakker_login_csrf"

type Auth struct {
	Access                           *access.Service
	Production                       bool
	SessionCookie, LoginCookie, Home string
}

func (a Auth) setCookie(w http.ResponseWriter, r *http.Request, name, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, HttpOnly: true, Secure: a.Production || r.TLS != nil, SameSite: http.SameSiteLaxMode})
}
func (a Auth) clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", Expires: time.Unix(1, 0), MaxAge: -1, HttpOnly: true, Secure: a.Production || r.TLS != nil, SameSite: http.SameSiteLaxMode})
}
func (a Auth) Challenge(w http.ResponseWriter, r *http.Request, message string, status int) {
	challenge, err := a.Access.NewLoginChallenge()
	if err != nil {
		Error(w, r, "Login is temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	a.setCookie(w, r, a.LoginCookie, challenge.CookieValue, challenge.Expires)
	JSON(w, r, status, map[string]string{"CSRF": challenge.Token, "error": message})
}
func (a Auth) Login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if ParseAction(w, r) != nil || !SameOrigin(r) || a.Access.ValidateLoginCSRF(CookieValue(r, a.LoginCookie), r.Header.Get("X-CSRF-Token")) != nil {
		a.Challenge(w, r, "The login form expired. Please try again.", http.StatusForbidden)
		return
	}
	identity, raw, err := a.Access.Login(r.Context(), r.PostForm.Get("username"), r.PostForm.Get("password"), CookieValue(r, a.SessionCookie))
	if err != nil {
		if errors.Is(err, access.ErrForbidden) {
			a.Challenge(w, r, "Administrator sign in is unavailable.", http.StatusForbidden)
		} else if errors.Is(err, access.ErrUnauthenticated) {
			a.Challenge(w, r, "Incorrect username or password, or the account is disabled.", http.StatusUnauthorized)
		} else {
			a.Challenge(w, r, "Login is temporarily unavailable. Please retry.", http.StatusServiceUnavailable)
		}
		return
	}
	a.setCookie(w, r, a.SessionCookie, raw, identity.AbsoluteExpires)
	a.clearCookie(w, r, a.LoginCookie)
	JSON(w, r, http.StatusOK, map[string]string{"location": a.Home})
}
func (a Auth) Authenticate(w http.ResponseWriter, r *http.Request, sessionBootstrap bool) (access.Identity, string, bool) {
	identity, replacement, err := a.Access.Authenticate(r.Context(), CookieValue(r, a.SessionCookie))
	if err != nil {
		if errors.Is(err, access.ErrUnauthenticated) {
			a.clearCookie(w, r, a.SessionCookie)
			if sessionBootstrap {
				a.Challenge(w, r, "", http.StatusOK)
			} else {
				Error(w, r, "Your session expired. Please sign in again.", http.StatusUnauthorized)
			}
		} else {
			Error(w, r, "Authentication is unavailable. Please retry.", http.StatusServiceUnavailable)
		}
		return access.Identity{}, "", false
	}
	if replacement != "" {
		a.setCookie(w, r, a.SessionCookie, replacement, identity.AbsoluteExpires)
	}
	csrf, err := a.Access.CSRFToken(identity)
	if err != nil {
		Error(w, r, "Session is unavailable", http.StatusUnauthorized)
		return access.Identity{}, "", false
	}
	return identity, csrf, true
}
func (a Auth) ValidateAction(w http.ResponseWriter, r *http.Request, identity access.Identity) error {
	if err := ParseAction(w, r); err != nil {
		return err
	}
	if !SameOrigin(r) || a.Access.ValidateCSRF(identity, r.Header.Get("X-CSRF-Token")) != nil {
		return access.ErrForbidden
	}
	return nil
}
func (a Auth) Logout(w http.ResponseWriter, r *http.Request, identity access.Identity) {
	if err := a.Access.Logout(r.Context(), identity.SessionID); err != nil {
		Error(w, r, "Logout is temporarily unavailable. Please retry.", http.StatusServiceUnavailable)
		return
	}
	a.clearCookie(w, r, a.SessionCookie)
	JSON(w, r, http.StatusOK, map[string]string{"location": "/login"})
}
