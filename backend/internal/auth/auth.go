package auth

import (
	"errors"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func GenerateSecret(pass string) (string, error) {
	if pass == "" {
		return "", errors.New("no password has been submitted")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func PeformLogin(email, pass string) (GeneratedResponse, error) {
	var user database.User
	var err error
	db := database.GetInstance()
	// Find user based on email
	err = db.Where("email = ?", email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return GeneratedResponse{}, err
	}
	// Compare stored PW and given PW
	err = bcrypt.CompareHashAndPassword([]byte(user.Secret), []byte(pass))
	if err != nil {
		return GeneratedResponse{}, errors.New("username or password is wrong")
	}
	// Everything seems legit, generate JWT
	jwtData, err := Generate(&user)
	if err != nil {
		return GeneratedResponse{}, err
	}
	return jwtData, nil
}
