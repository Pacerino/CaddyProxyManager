package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	h "github.com/Pacerino/CaddyProxyManager/internal/api/http"
	"github.com/Pacerino/CaddyProxyManager/internal/auth"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
	"github.com/go-playground/validator/v10"
	"gorm.io/gorm"
)

type LoginData struct {
	Email  string `validate:"required,email"`
	Secret string `validate:"required"`
}

// UserLogin will login a user and return a JWT Token
// Route: GET /users/login
func (s Handler) UserLogin() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var logindata LoginData
		err := json.NewDecoder(r.Body).Decode(&logindata)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		validate := validator.New()
		if err := validate.Struct(logindata); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		jwtData, err := auth.PeformLogin(logindata.Email, logindata.Secret)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// If the User wasnt found, throw an error
				h.ResultErrorJSON(w, r, http.StatusUnauthorized, "Login incorrect", nil)
				return
			} else {
				// if the user was found but there is an error, throw it also
				h.ResultErrorJSON(w, r, http.StatusUnauthorized, err.Error(), nil)
				return
			}
		}

		h.ResultResponseJSON(w, r, http.StatusOK, jwtData)
	}
}

// GetUsers will return a list of users
// Route: GET /users
func (s Handler) GetUsers() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var users []database.User

		if err := s.DB.Select([]string{"Name", "Email", "ID", "CreatedAt", "UpdatedAt", "DeletedAt"}).Find(&users).Error; err != nil {
			logger.Error("", err)
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "could not retrieve user list", nil)
		} else {
			h.ResultResponseJSON(w, r, http.StatusOK, users)
		}
	}
}

// GetUser will return a list of users
// Route: GET /users/{userID}
func (s Handler) GetUser() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var err error
		var userID int
		var user database.User
		if userID, err = getURLParamInt(r, "userID"); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if err = s.DB.Where("id = ?", userID).First(&user).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
		} else {
			h.ResultResponseJSON(w, r, http.StatusOK, user)
		}
	}
}

// CreateUser will create a Host
// Route: POST /users
func (s Handler) CreateUser() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var newUser database.User
		err := json.NewDecoder(r.Body).Decode(&newUser)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		validate := validator.New()
		if err := validate.Struct(&newUser); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		generatedSecret, err := auth.GenerateSecret(newUser.Secret)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		newUser.Secret = generatedSecret
		result := s.DB.Create(&newUser)
		if result.Error != nil {
			logger.Error("", result.Error)
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "could not create user", nil)
			return
		}

		h.ResultResponseJSON(w, r, http.StatusOK, newUser)
	}
}

// DeleteUser removes a host
// Route: DELETE /users/{userID}
func (s Handler) DeleteUser() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var err error
		var userID int
		if userID, err = getURLParamInt(r, "userID"); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		result := s.DB.Unscoped().Delete(&database.User{}, userID)
		if result.Error != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, result.Error.Error(), nil)
			return
		}
		if result.RowsAffected > 0 {
			h.ResultResponseJSON(w, r, http.StatusOK, true)
		} else {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, h.ErrIDNotFound.Error(), nil)
		}
	}
}

// UpdateUser updates a user
// Route: PUT /users
func (s Handler) UpdateUser() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var newUser database.User
		err := json.NewDecoder(r.Body).Decode(&newUser)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		validate := validator.New()
		if err := validate.Struct(newUser); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		result := s.DB.Updates(&newUser)
		if result.Error != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, result.Error.Error(), nil)
			return
		}

		h.ResultResponseJSON(w, r, http.StatusOK, newUser)
	}
}
