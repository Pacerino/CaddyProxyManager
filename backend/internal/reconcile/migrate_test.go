package reconcile

import (
	"encoding/json"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/database"

	"gorm.io/gorm"
)

const legacyObject = `{"issuers":[{"module":"acme","challenges":{"dns":{"provider":{"name":"cloudflare","api_token":"tok123"}}}}]}`

func seedLegacy(t *testing.T, cfg string, enabled bool) *gorm.DB {
	t.Helper()
	db, _, _ := setup(t)
	mc := database.ModuleConfig{ModuleID: "dns.providers.cloudflare", Path: "apps/tls/automation/policies", Config: database.JSON(cfg), Enabled: true}
	db.Create(&mc)
	if !enabled {
		// gorm skips the zero value on create because of the column default.
		db.Model(&mc).Update("enabled", false)
	}
	db.Create(&database.Host{Domains: "a.example.com"})
	db.Create(&database.Host{Domains: "b.example.com"})
	return db
}

func legacyRows(db *gorm.DB) int64 {
	var n int64
	db.Unscoped().Model(&database.ModuleConfig{}).Where("module_id = ?", "dns.providers.cloudflare").Count(&n)
	return n
}

func TestMigrateLegacyCloudflare_Enabled(t *testing.T) {
	db := seedLegacy(t, legacyObject, true)

	migrated, err := MigrateLegacyCloudflare(db)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v, want true, nil", migrated, err)
	}
	var prov database.DNSProvider
	if err := db.First(&prov).Error; err != nil {
		t.Fatalf("provider not created: %v", err)
	}
	if prov.Name != "Cloudflare" || prov.ModuleID != "dns.providers.cloudflare" {
		t.Errorf("provider = %+v", prov)
	}
	var cfg map[string]string
	_ = json.Unmarshal(prov.Config, &cfg)
	if cfg["api_token"] != "tok123" {
		t.Errorf("config = %s", prov.Config)
	}
	var hosts []database.Host
	db.Find(&hosts)
	for _, h := range hosts {
		if h.DNSProviderID == nil || *h.DNSProviderID != prov.ID {
			t.Errorf("host %d not assigned to provider", h.ID)
		}
	}
	if n := legacyRows(db); n != 0 {
		t.Errorf("legacy rows left = %d", n)
	}
}

func TestMigrateLegacyCloudflare_ArrayAndNameTaken(t *testing.T) {
	db := seedLegacy(t, "["+legacyObject+"]", true)
	db.Create(&database.DNSProvider{Name: "Cloudflare", ModuleID: "dns.providers.cloudflare"})

	migrated, err := MigrateLegacyCloudflare(db)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v", migrated, err)
	}
	var prov database.DNSProvider
	if err := db.Where("name = ?", "Cloudflare (migrated)").First(&prov).Error; err != nil {
		t.Fatalf("migrated provider missing: %v", err)
	}
}

func TestMigrateLegacyCloudflare_Disabled(t *testing.T) {
	db := seedLegacy(t, legacyObject, false)

	migrated, err := MigrateLegacyCloudflare(db)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v", migrated, err)
	}
	var n int64
	db.Model(&database.DNSProvider{}).Count(&n)
	if n != 1 {
		t.Errorf("providers = %d, want 1", n)
	}
	db.Model(&database.Host{}).Where("dns_provider_id IS NOT NULL").Count(&n)
	if n != 0 {
		t.Errorf("assigned hosts = %d, want 0", n)
	}
	if legacyRows(db) != 0 {
		t.Error("legacy row not deleted")
	}
}

func TestMigrateLegacyCloudflare_NoRow(t *testing.T) {
	db, _, _ := setup(t)
	db.Create(&database.Host{Domains: "a.example.com"})

	migrated, err := MigrateLegacyCloudflare(db)
	if err != nil || migrated {
		t.Fatalf("migrated=%v err=%v, want false, nil", migrated, err)
	}
	var n int64
	db.Model(&database.DNSProvider{}).Count(&n)
	if n != 0 {
		t.Errorf("providers = %d, want 0", n)
	}
}

func TestMigrateLegacyCloudflare_NoToken(t *testing.T) {
	db := seedLegacy(t, `{"issuers":[]}`, true)

	migrated, err := MigrateLegacyCloudflare(db)
	if err == nil || migrated {
		t.Fatalf("migrated=%v err=%v, want false and an error", migrated, err)
	}
	if legacyRows(db) != 1 {
		t.Error("legacy row must be kept when it cannot be migrated")
	}
}
