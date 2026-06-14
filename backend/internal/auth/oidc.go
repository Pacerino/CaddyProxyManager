package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	oidcOnce     sync.Once
	oidcProvider *oidc.Provider
	oidcVerifier *oidc.IDTokenVerifier
	oidcConfig   oauth2.Config
	oidcInitErr  error
)

// IsOIDCEnabled reports whether the auth mode is configured as oidc.
func IsOIDCEnabled() bool {
	return strings.ToLower(config.Configuration.Auth.Mode) == "oidc"
}

// initOIDC performs provider discovery exactly once.
func initOIDC() error {
	oidcOnce.Do(func() {
		c := config.Configuration.Auth.OIDC
		if c.Issuer == "" || c.ClientID == "" || c.RedirectURL == "" {
			oidcInitErr = errors.New("oidc is not fully configured (issuer, client id and redirect url are required)")
			return
		}
		provider, err := oidc.NewProvider(context.Background(), c.Issuer)
		if err != nil {
			oidcInitErr = fmt.Errorf("oidc provider discovery failed: %w", err)
			return
		}
		oidcProvider = provider
		oidcVerifier = provider.Verifier(&oidc.Config{ClientID: c.ClientID})
		oidcConfig = oauth2.Config{
			ClientID:     c.ClientID,
			ClientSecret: c.ClientSecret,
			RedirectURL:  c.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       c.Scopes,
		}
	})
	return oidcInitErr
}

// OIDCAuthURL returns the provider authorization URL for the given state.
func OIDCAuthURL(state string) (string, error) {
	if err := initOIDC(); err != nil {
		return "", err
	}
	return oidcConfig.AuthCodeURL(state), nil
}

// oidcClaims is the subset of ID token claims CPM consumes.
type oidcClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// OIDCCallback exchanges the authorization code, verifies the ID token,
// JIT-provisions the matching CPM user and returns a CPM-issued JWT.
func OIDCCallback(ctx context.Context, code string) (GeneratedResponse, error) {
	if err := initOIDC(); err != nil {
		return GeneratedResponse{}, err
	}

	token, err := oidcConfig.Exchange(ctx, code)
	if err != nil {
		return GeneratedResponse{}, fmt.Errorf("token exchange failed: %w", err)
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		return GeneratedResponse{}, errors.New("no id_token in token response")
	}
	idToken, err := oidcVerifier.Verify(ctx, rawID)
	if err != nil {
		return GeneratedResponse{}, fmt.Errorf("id token verification failed: %w", err)
	}

	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		return GeneratedResponse{}, fmt.Errorf("failed to parse claims: %w", err)
	}
	if claims.Email == "" {
		return GeneratedResponse{}, errors.New("id token has no email claim")
	}
	if !isAllowedDomain(claims.Email) {
		return GeneratedResponse{}, errors.New("email domain is not allowed")
	}

	user, err := provisionOIDCUser(claims)
	if err != nil {
		return GeneratedResponse{}, err
	}
	return Generate(user)
}

// isAllowedDomain checks the email against the configured allowlist.
// An empty allowlist permits any domain.
func isAllowedDomain(email string) bool {
	allowed := config.Configuration.Auth.OIDC.AllowedDomains
	if len(allowed) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range allowed {
		if strings.ToLower(strings.TrimSpace(d)) == domain {
			return true
		}
	}
	return false
}

// provisionOIDCUser finds or creates (JIT) the CPM user for the claims.
func provisionOIDCUser(claims oidcClaims) (*database.User, error) {
	db := database.GetInstance()
	var user database.User

	// Match by subject first, then fall back to email.
	tx := db.Where("subject = ? AND provider = ?", claims.Subject, "oidc").First(&user)
	if tx.Error != nil {
		tx = db.Where("email = ?", claims.Email).First(&user)
	}

	name := claims.Name
	if name == "" {
		name = claims.Email
	}

	if tx.Error != nil {
		// Create a new OIDC user.
		user = database.User{
			Name:     name,
			Email:    claims.Email,
			Provider: "oidc",
			Subject:  claims.Subject,
		}
		if err := db.Create(&user).Error; err != nil {
			return nil, fmt.Errorf("failed to provision oidc user: %w", err)
		}
		return &user, nil
	}

	// Update existing user to keep subject/provider in sync.
	user.Provider = "oidc"
	user.Subject = claims.Subject
	user.Name = name
	db.Save(&user)
	return &user, nil
}
