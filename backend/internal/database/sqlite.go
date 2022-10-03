package database

import (
	"fmt"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var dbInstance *gorm.DB

// NewDB creates a new connection
func NewDB() {
	logger.Info("Creating new DB instance")
	dbInstance = SqliteDB()
	dbInstance.AutoMigrate(&Host{}, &Upstream{}, &User{})
}

// GetInstance returns an existing or new instance
func GetInstance() *gorm.DB {
	if dbInstance == nil {
		NewDB()
	}
	return dbInstance
}

// SqliteDB Create sqlite client
func SqliteDB() *gorm.DB {
	dbFile := fmt.Sprintf("%s/caddyproxymanager.db", config.Configuration.DataFolder)
	db, err := gorm.Open(sqlite.Open(dbFile), &gorm.Config{})
	if err != nil {
		logger.Error("SqliteError", err)
		return nil
	}

	return db
}
