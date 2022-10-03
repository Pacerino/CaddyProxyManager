package auth

import (
	"fmt"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/logger"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/golang-jwt/jwt/v4"
)

// UserJWTClaims is the structure of a JWT for a User
type UserJWTClaims struct {
	UserID uint `json:"uid"`
	jwt.RegisteredClaims
}

// GeneratedResponse is the response of a generated token, usually used in http response
type GeneratedResponse struct {
	Expires int64  `json:"expires"`
	Token   string `json:"token"`
}

// Generate will create a JWT
func Generate(userObj *database.User) (GeneratedResponse, error) {
	var response GeneratedResponse

	key, err := GetPrivateKey()
	if err != nil {
		logger.Error("JWTError", fmt.Errorf("error signing token: %v", err))
		return response, err
	}
	expires := time.Now().AddDate(0, 0, 1) // 1 day

	// Create the Claims
	claims := UserJWTClaims{
		userObj.ID,
		jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expires),
			Issuer:    "api",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	//var err error
	token.Signature, err = token.SignedString(key)
	if err != nil {
		logger.Error("JWTError", fmt.Errorf("error signing token: %v", err))
		return response, err
	}

	response = GeneratedResponse{
		Expires: expires.Unix(),
		Token:   token.Signature,
	}

	return response, nil
}
