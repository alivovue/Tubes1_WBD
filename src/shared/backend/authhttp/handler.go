package authhttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	primitive "schakker_shared/auth"
	"schakker_shared/httpapi"
	"schakker_shared/observability"
)

// Service keeps credential verification, session persistence and authorization
// in the owning application. The shared handler only implements HTTP behavior.
type Service[I primitive.Identity] interface {
	NewLoginChallenge() (primitive.LoginChallenge, error)
	ValidateLoginCSRF(string, string) error
	Login(context.Context, string, string, string) (I, string, error)
	Authenticate(context.Context, string) (I, string, error)
	CSRFToken(I) (string, error)
	ValidateCSRF(I, string) error
	Logout(context.Context, string) error
}

type Handler[I primitive.Identity] struct {
	Access                           Service[I]
	Production                       bool
	SessionCookie, LoginCookie, Home string
	LoginEvent, ForbiddenMessage     string
}

func (a Handler[I]) setCookie(w http.ResponseWriter, r *http.Request, name, value string, expires time.Time) {
	primitive.SetCookie(w, r, a.Production, name, value, expires)
}
func (a Handler[I]) clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	primitive.ClearCookie(w, r, a.Production, name)
}
func (a Handler[I]) Challenge(w http.ResponseWriter, r *http.Request, message string, status int) {
	challenge, err := a.Access.NewLoginChallenge()
	if err != nil {
		httpapi.Error(w, r, "Login is temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	a.setCookie(w, r, a.LoginCookie, challenge.CookieValue, challenge.Expires)
	httpapi.JSON(w, r, status, map[string]string{"CSRF": challenge.Token, "error": message})
}
func (a Handler[I]) Login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if httpapi.ParseAction(w, r) != nil || !httpapi.SameOrigin(r) || a.Access.ValidateLoginCSRF(httpapi.CookieValue(r, a.LoginCookie), r.Header.Get("X-CSRF-Token")) != nil {
		a.Challenge(w, r, "The login form expired. Please try again.", http.StatusForbidden)
		return
	}
	identity, raw, err := a.Access.Login(r.Context(), r.PostForm.Get("username"), r.PostForm.Get("password"), httpapi.CookieValue(r, a.SessionCookie))
	observability.Event(r.Context(), a.LoginEvent, err, "actor_id", identity.SessionInfo().UserID)
	if err != nil {
		if errors.Is(err, primitive.ErrForbidden) {
			a.Challenge(w, r, a.ForbiddenMessage, http.StatusForbidden)
		} else if errors.Is(err, primitive.ErrUnauthenticated) {
			a.Challenge(w, r, "Incorrect username or password, or the account is disabled.", http.StatusUnauthorized)
		} else {
			a.Challenge(w, r, "Login is temporarily unavailable. Please retry.", http.StatusServiceUnavailable)
		}
		return
	}
	a.setCookie(w, r, a.SessionCookie, raw, identity.SessionInfo().AbsoluteExpires)
	a.clearCookie(w, r, a.LoginCookie)
	httpapi.JSON(w, r, http.StatusOK, map[string]string{"location": a.Home})
}
func (a Handler[I]) Authenticate(w http.ResponseWriter, r *http.Request, sessionBootstrap bool) (I, string, bool) {
	identity, replacement, err := a.Access.Authenticate(r.Context(), httpapi.CookieValue(r, a.SessionCookie))
	if err != nil {
		if errors.Is(err, primitive.ErrUnauthenticated) {
			a.clearCookie(w, r, a.SessionCookie)
			if sessionBootstrap {
				a.Challenge(w, r, "", http.StatusOK)
			} else {
				httpapi.Error(w, r, "Your session expired. Please sign in again.", http.StatusUnauthorized)
			}
		} else {
			httpapi.Error(w, r, "Authentication is unavailable. Please retry.", http.StatusServiceUnavailable)
		}
		var empty I
		return empty, "", false
	}
	if replacement != "" {
		a.setCookie(w, r, a.SessionCookie, replacement, identity.SessionInfo().AbsoluteExpires)
	}
	csrf, err := a.Access.CSRFToken(identity)
	if err != nil {
		httpapi.Error(w, r, "Session is unavailable", http.StatusUnauthorized)
		var empty I
		return empty, "", false
	}
	return identity, csrf, true
}
func (a Handler[I]) ValidateAction(w http.ResponseWriter, r *http.Request, identity I) error {
	if err := httpapi.ParseAction(w, r); err != nil {
		return err
	}
	if !httpapi.SameOrigin(r) || a.Access.ValidateCSRF(identity, r.Header.Get("X-CSRF-Token")) != nil {
		return primitive.ErrForbidden
	}
	return nil
}

func (a Handler[I]) ValidateJSONAction(r *http.Request, identity I) error {
	if !httpapi.SameOrigin(r) || a.Access.ValidateCSRF(identity, r.Header.Get("X-CSRF-Token")) != nil {
		return primitive.ErrForbidden
	}
	return nil
}
func (a Handler[I]) Logout(w http.ResponseWriter, r *http.Request, identity I) {
	if err := a.Access.Logout(r.Context(), identity.SessionInfo().SessionID); err != nil {
		httpapi.Error(w, r, "Logout is temporarily unavailable. Please retry.", http.StatusServiceUnavailable)
		return
	}
	a.clearCookie(w, r, a.SessionCookie)
	httpapi.JSON(w, r, http.StatusOK, map[string]string{"location": "/login"})
}
