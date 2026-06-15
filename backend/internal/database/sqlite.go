package database

import (
	"fmt"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var dbInstance *gorm.DB

// NewDB creates a new connection
func NewDB() {
	logger.Info("Creating new DB instance")
	dbInstance = SqliteDB()
	dbInstance.AutoMigrate(&Host{}, &Upstream{}, &HostPlugin{}, &User{}, &ModuleConfig{})
	seedAdmin()
}

// seedAdmin creates a default admin user the first time CPM starts with an
// empty database. The credentials can be overridden via CPM_ADMIN_EMAIL and
// CPM_ADMIN_PASSWORD. This makes initial login possible without a separate
// setup step.
func seedAdmin() {
	if config.Configuration.Auth.Mode == "oidc" {
		return
	}
	var count int64
	dbInstance.Model(&User{}).Count(&count)
	if count > 0 {
		return
	}

	email := config.Configuration.Admin.Email
	password := config.Configuration.Admin.Password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("AdminSeedError", err)
		return
	}
	user := User{Name: "Admin", Email: email, Secret: string(hash), Provider: "local"}
	if err := dbInstance.Create(&user).Error; err != nil {
		logger.Error("AdminSeedError", err)
		return
	}
	logger.Info("Seeded default admin user: %s", email)
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
