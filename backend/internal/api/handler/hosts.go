package handler

import (
	"encoding/json"
	"net/http"

	h "github.com/Pacerino/CaddyProxyManager/internal/api/http"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/jobqueue"

	"github.com/go-playground/validator/v10"
	"gorm.io/gorm"
)

// GetHosts will return a list of Hosts
// Route: GET /hosts
func (s Handler) GetHosts() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var hosts []database.Host
		if err := s.DB.Preload("Upstreams").Find(&hosts).Error; err != nil {
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

		if err = s.DB.Where("id = ?", hostID).Preload("Upstreams").First(&host).Error; err != nil {
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
		validate := validator.New()
		if err := validate.Struct(newHost); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		result := s.DB.Create(&newHost)
		if result.Error != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, result.Error.Error(), nil)
			return
		}

		if err := jobqueue.AddJob(jobqueue.Job{
			Name: "CaddyConfigureHost",
			Action: func() error {
				return caddy.WriteHost(newHost)
			},
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
		validate := validator.New()
		if err := validate.Struct(newHost); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if err := jobqueue.AddJob(jobqueue.Job{
			Name: "CaddyConfigureHost",
			Action: func() error {
				return caddy.WriteHost(newHost)
			},
		}); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		result := s.DB.Session(&gorm.Session{FullSaveAssociations: true}).Save(&newHost)
		if result.Error != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, result.Error.Error(), nil)
			return
		}

		h.ResultResponseJSON(w, r, http.StatusOK, newHost)
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

		if err := jobqueue.AddJob(jobqueue.Job{
			Name: "CaddyConfigureHost",
			Action: func() error {
				return caddy.RemoveHost(hostID)
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
