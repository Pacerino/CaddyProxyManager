package middleware

import (
	"context"
	"net/http"

	c "github.com/Pacerino/CaddyProxyManager/internal/api/context"
	h "github.com/Pacerino/CaddyProxyManager/internal/api/http"
	"github.com/Pacerino/CaddyProxyManager/internal/auth"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
	"github.com/go-chi/jwtauth/v5"
	"github.com/lestrrat-go/jwx/jwt"
)

// DecodeAuth decodes an auth header
func DecodeAuth() func(http.Handler) http.Handler {
	privateKey, privateKeyParseErr := auth.GetPrivateKey()
	if privateKeyParseErr != nil && privateKey == nil {
		logger.Error("PrivateKeyParseError", privateKeyParseErr)
	}

	/* publicKey, publicKeyParseErr := auth.GetPublicKey()
	if publicKeyParseErr != nil && publicKey == nil {
		logger.Error("PublicKeyParseError", publicKeyParseErr)
	} */

	tokenAuth := jwtauth.New("RS256", privateKey, privateKey.PublicKey)
	return jwtauth.Verifier(tokenAuth)
}

// Enforce is a authentication middleware to enforce access from the
// jwtauth.Verifier middleware request context values. The Authenticator sends a 401 Unauthorised
// response for any unverified tokens and passes the good ones through.
func Enforce() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			token, claims, err := jwtauth.FromContext(ctx)

			if err != nil {
				h.ResultErrorJSON(w, r, http.StatusUnauthorized, err.Error(), nil)
				return
			}

			userID := int(claims["uid"].(float64))
			if token == nil || jwt.Validate(token) != nil {
				h.ResultErrorJSON(w, r, http.StatusUnauthorized, "Unauthorised", nil)
				return
			}

			// Add claims to context
			ctx = context.WithValue(ctx, c.UserIDCtxKey, userID)

			// Token is authenticated, continue as normal
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
