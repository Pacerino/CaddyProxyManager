package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"

	h "github.com/Pacerino/CaddyProxyManager/internal/api/http"
	"github.com/Pacerino/CaddyProxyManager/internal/auth"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

const oidcStateCookie = "cpm_oidc_state"

// authConfigResponse tells the frontend which auth mode is active.
type authConfigResponse struct {
	Mode string `json:"mode"`
}

// AuthConfig returns the active authentication mode.
// Route: GET /auth/config
func (s Handler) AuthConfig() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		mode := "local"
		if auth.IsOIDCEnabled() {
			mode = "oidc"
		}
		h.ResultResponseJSON(w, r, http.StatusOK, authConfigResponse{Mode: mode})
	}
}

// OIDCLogin redirects the user to the OIDC provider.
// Route: GET /auth/oidc/login
func (s Handler) OIDCLogin() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.IsOIDCEnabled() {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "oidc is not enabled", nil)
			return
		}
		state, err := randomState()
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		authURL, err := auth.OIDCAuthURL(state)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     oidcStateCookie,
			Value:    state,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   300,
		})
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// OIDCCallback handles the provider redirect, exchanges the code and redirects
// back to the SPA with a CPM-issued token.
// Route: GET /auth/oidc/callback
func (s Handler) OIDCCallback() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.IsOIDCEnabled() {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "oidc is not enabled", nil)
			return
		}

		stateCookie, err := r.Cookie(oidcStateCookie)
		if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "invalid oauth state", nil)
			return
		}
		// Clear the state cookie.
		http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/", MaxAge: -1})

		code := r.URL.Query().Get("code")
		if code == "" {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "missing authorization code", nil)
			return
		}

		jwtData, err := auth.OIDCCallback(r.Context(), code)
		if err != nil {
			logger.Error("OIDCCallbackError", err)
			http.Redirect(w, r, "/login?error="+url.QueryEscape(err.Error()), http.StatusFound)
			return
		}

		// Hand the token back to the SPA via the callback route.
		redirect := "/login/callback#token=" + url.QueryEscape(jwtData.Token)
		http.Redirect(w, r, redirect, http.StatusFound)
	}
}

// randomState returns a cryptographically random state string.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
