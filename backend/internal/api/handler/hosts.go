package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	h "github.com/Pacerino/CaddyProxyManager/internal/api/http"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/jobqueue"

	"gorm.io/gorm"
)

// GetHosts will return a list of Hosts
// Route: GET /hosts
func (s Handler) GetHosts() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var hosts []database.Host
		if err := s.DB.Preload("Upstreams").Preload("Plugins").Find(&hosts).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
		}
		h.ResultResponseJSON(w, r, http.StatusOK, hosts)
	}
}

// GetHost will return a single Host
// Route: GET /hosts/{hostID}
func (s Handler) GetHost() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var err error
		var hostID int
		var host database.Host
		if hostID, err = getURLParamInt(r, "hostID"); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if err = s.DB.Where("id = ?", hostID).Preload("Upstreams").Preload("Plugins").First(&host).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
		} else {
			h.ResultResponseJSON(w, r, http.StatusOK, host)
		}
	}
}

// CreateHost will create a Host
// Route: POST /hosts
func (s Handler) CreateHost() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var newHost database.Host
		err := json.NewDecoder(r.Body).Decode(&newHost)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if err := validate.Struct(newHost); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if !s.checkHostDNSProvider(w, r, newHost) {
			return
		}

		result := s.DB.Create(&newHost)
		if result.Error != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, result.Error.Error(), nil)
			return
		}

		provider, err := caddy.GetProvider()
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}

		created := newHost
		if err := jobqueue.AddJob(jobqueue.Job{
			Name:   "CaddyConfigureHost",
			Action: func() error { return provider.WriteHost(created) },
		}); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if !s.enqueueHostSyncTLS(w, r) {
			return
		}

		h.ResultResponseJSON(w, r, http.StatusOK, newHost)
	}
}

// UpdateHost updates a host
// Route: PUT /hosts
func (s Handler) UpdateHost() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var newHost database.Host
		err := json.NewDecoder(r.Body).Decode(&newHost)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if err := validate.Struct(newHost); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if !s.checkHostDNSProvider(w, r, newHost) {
			return
		}

		provider, err := caddy.GetProvider()
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}

		// Persist the host fields and replace its upstreams. Replace deletes
		// upstreams that are no longer present and inserts the new set, so
		// repeated edits don't accumulate duplicates.
		if err := s.DB.Transaction(func(tx *gorm.DB) error {
			upstreams := newHost.Upstreams
			newHost.Upstreams = nil
			if err := tx.Model(&database.Host{}).
				Where("id = ?", newHost.ID).
				Updates(map[string]any{"domains": newHost.Domains, "matcher": newHost.Matcher, "dns_provider_id": newHost.DNSProviderID}).Error; err != nil {
				return err
			}
			return tx.Model(&newHost).Association("Upstreams").Replace(upstreams)
		}); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		// Re-read the persisted host (with associations) and apply that
		// snapshot to Caddy. Reading a fresh copy avoids sharing the request's
		// mutable struct with the async job goroutine.
		var persisted database.Host
		if err := s.DB.Where("id = ?", newHost.ID).Preload("Upstreams").Preload("Plugins").First(&persisted).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if err := jobqueue.AddJob(jobqueue.Job{
			Name:   "CaddyConfigureHost",
			Action: func() error { return provider.WriteHost(persisted) },
		}); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if !s.enqueueHostSyncTLS(w, r) {
			return
		}

		h.ResultResponseJSON(w, r, http.StatusOK, persisted)
	}
}

// DeleteHost removes a host
// Route: DELETE /hosts/{hostID}
func (s Handler) DeleteHost() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var err error
		var hostID int
		if hostID, err = getURLParamInt(r, "hostID"); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		provider, err := caddy.GetProvider()
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}

		if err := jobqueue.AddJob(jobqueue.Job{
			Name: "CaddyConfigureHost",
			Action: func() error {
				return provider.RemoveHost(hostID)
			},
		}); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		result := s.DB.Delete(&database.Host{}, hostID)
		if result.Error != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, result.Error.Error(), nil)
			return
		}
		// The sync reads the hosts table, so it is queued after the row is gone.
		if !s.enqueueHostSyncTLS(w, r) {
			return
		}
		if result.RowsAffected > 0 {
			h.ResultResponseJSON(w, r, http.StatusOK, true)
		} else {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, h.ErrIDNotFound.Error(), nil)
		}
	}
}

// apiMode reports whether CPM manages Caddy through the admin API.
func apiMode() bool {
	return strings.ToLower(config.Configuration.Caddy.Mode) == "api"
}

// enqueueHostSyncTLS queues a TLS policy sync after a host change. Caddyfile
// mode has no TLS policies to manage, so nothing is queued there.
func (s Handler) enqueueHostSyncTLS(w http.ResponseWriter, r *http.Request) bool {
	if !apiMode() {
		return true
	}
	return s.enqueueSyncTLS(w, r, caddy.NewAPIProvider())
}

// checkHostDNSProvider validates the host's DNS provider reference and the
// domain conflict rule, writing a 400 and returning false on failure. A domain
// may be listed by several hosts only if they all use the same provider,
// because Caddy matches one TLS policy per subject.
func (s Handler) checkHostDNSProvider(w http.ResponseWriter, r *http.Request, host database.Host) bool {
	fail := func(msg string) bool {
		h.ResultErrorJSON(w, r, http.StatusBadRequest, msg, nil)
		return false
	}
	if !apiMode() {
		// Stored selections are kept but ignored outside API mode, so an edit
		// may resubmit the stored value; only setting or changing one is rejected.
		var stored *uint
		if host.ID != 0 {
			var existing database.Host
			if err := s.DB.Select("id", "dns_provider_id").Limit(1).Find(&existing, host.ID).Error; err != nil {
				return fail(err.Error())
			}
			stored = existing.DNSProviderID
		}
		if !sameProvider(host.DNSProviderID, stored) {
			return fail("DNS providers are only available when CPM_CADDY_MODE=api")
		}
	}
	if host.DNSProviderID != nil && apiMode() {
		var p database.DNSProvider
		if err := s.DB.First(&p, *host.DNSProviderID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fail(fmt.Sprintf("DNS provider %d does not exist", *host.DNSProviderID))
			}
			return fail(err.Error())
		}
	}

	var others []database.Host
	if err := s.DB.Where("id <> ?", host.ID).Find(&others).Error; err != nil {
		return fail(err.Error())
	}
	mine := map[string]bool{}
	for _, d := range strings.Fields(host.Domains) {
		mine[d] = true
	}
	for _, o := range others {
		if sameProvider(host.DNSProviderID, o.DNSProviderID) {
			continue
		}
		for _, d := range strings.Fields(o.Domains) {
			if mine[d] {
				return fail(fmt.Sprintf("domain %s is already used by host %d with a different DNS provider", d, o.ID))
			}
		}
	}
	return true
}

func sameProvider(a, b *uint) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
