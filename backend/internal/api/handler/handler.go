package handler

import (
	"github.com/Pacerino/CaddyProxyManager/internal/database"

	"gorm.io/gorm"
)

type Handler struct {
	DB *gorm.DB
}

func NewHandler() *Handler {
	db := database.GetInstance()
	handler := &Handler{
		DB: db,
	}
	return handler
}
