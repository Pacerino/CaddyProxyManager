package reconcile

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Pacerino/CaddyProxyManager/internal/database"

	"gorm.io/gorm"
)

const legacyCloudflareModule = "dns.providers.cloudflare"

// MigrateLegacyCloudflare converts the old global Cloudflare ModuleConfig into
// a DNSProvider. When the legacy row was enabled, every host is assigned to the
// new provider (the old policy applied to all hosts). The legacy row is
// deleted. Returns whether a migration happened.
func MigrateLegacyCloudflare(db *gorm.DB) (bool, error) {
	var mc database.ModuleConfig
	err := db.Where("module_id = ?", legacyCloudflareModule).First(&mc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	token, err := legacyAPIToken(mc.Config)
	if err != nil {
		return false, fmt.Errorf("legacy cloudflare config: %w", err)
	}
	cfg, err := json.Marshal(map[string]string{"api_token": token})
	if err != nil {
		return false, err
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		name := "Cloudflare"
		var n int64
		if err := tx.Model(&database.DNSProvider{}).Where("name = ?", name).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			name = "Cloudflare (migrated)"
		}
		prov := database.DNSProvider{Name: name, ModuleID: legacyCloudflareModule, Config: database.JSON(cfg)}
		if err := tx.Create(&prov).Error; err != nil {
			return err
		}
		if mc.Enabled {
			if err := tx.Model(&database.Host{}).Where("deleted_at IS NULL").Update("dns_provider_id", prov.ID).Error; err != nil {
				return err
			}
		}
		// Hard delete: module_id is a unique index, a soft-deleted row would
		// block re-creating the config.
		return tx.Unscoped().Delete(&mc).Error
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// legacyAPIToken extracts issuers[0].challenges.dns.provider.api_token from the
// stored policy, which may be a single policy object or an array of them.
func legacyAPIToken(raw database.JSON) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	if arr, ok := v.([]any); ok {
		if len(arr) == 0 {
			return "", errors.New("empty policy array")
		}
		v = arr[0]
	}
	policy, _ := v.(map[string]any)
	issuers, _ := policy["issuers"].([]any)
	if len(issuers) == 0 {
		return "", errors.New("no issuers")
	}
	issuer, _ := issuers[0].(map[string]any)
	challenges, _ := issuer["challenges"].(map[string]any)
	dns, _ := challenges["dns"].(map[string]any)
	provider, _ := dns["provider"].(map[string]any)
	token, _ := provider["api_token"].(string)
	if token == "" {
		return "", errors.New("api_token not found")
	}
	return token, nil
}
