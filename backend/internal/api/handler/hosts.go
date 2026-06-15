package handler

import (
	"encoding/json"
	"net/http"

	h "github.com/Pacerino/CaddyProxyManager/internal/api/http"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
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
				Updates(map[string]any{"domains": newHost.Domains, "matcher": newHost.Matcher}).Error; err != nil {
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
		if result.RowsAffected > 0 {
			h.ResultResponseJSON(w, r, http.StatusOK, true)
		} else {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, h.ErrIDNotFound.Error(), nil)
		}
	}
}
