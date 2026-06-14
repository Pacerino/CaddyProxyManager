package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

// APIProvider manages Caddy's configuration through its admin API by
// maintaining one JSON route per host.
type APIProvider struct {
	baseURL    string
	serverName string
	listen     []string
	client     *http.Client
}

// NewAPIProvider builds an APIProvider from the global configuration.
func NewAPIProvider() *APIProvider {
	return &APIProvider{
		baseURL:    strings.TrimRight(config.Configuration.Caddy.AdminURL, "/"),
		serverName: config.Configuration.Caddy.ServerName,
		listen:     config.Configuration.Caddy.Listen,
		client:     &http.Client{Timeout: 15 * time.Second},
	}
}

// WriteHost creates or updates the route for a host.
func (p *APIProvider) WriteHost(h database.Host) error {
	if err := p.ensureServer(); err != nil {
		return err
	}

	route := buildRoute(h)
	id := hostRouteID(h.ID)

	exists, err := p.routeExists(id)
	if err != nil {
		return err
	}

	if exists {
		// Replace the existing route in place via its @id.
		return p.do(http.MethodPatch, "/id/"+id, route, nil)
	}
	// Append a new route to the server's route list.
	path := fmt.Sprintf("/config/apps/http/servers/%s/routes", p.serverName)
	return p.do(http.MethodPost, path, route, nil)
}

// RemoveHost deletes the route for a host.
func (p *APIProvider) RemoveHost(hostID int) error {
	id := hostRouteID(uint(hostID))
	exists, err := p.routeExists(id)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return p.do(http.MethodDelete, "/id/"+id, nil, nil)
}

// configExists reports whether the config at path is present. Caddy returns
// HTTP 200 with a literal "null" body for a valid-but-empty traversal path, so
// a successful status alone is not enough.
func (p *APIProvider) configExists(path string) (bool, error) {
	status, body, err := p.request(http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	if status < 200 || status >= 300 {
		// Unknown/invalid path: treat as not present.
		return false, nil
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" {
		return false, nil
	}
	return true, nil
}

// routeExists reports whether a route with the given @id is present.
func (p *APIProvider) routeExists(id string) (bool, error) {
	return p.configExists("/id/" + id)
}

// ensureServer makes sure the http app and target server exist so routes can
// be appended. It is a no-op when the server is already present.
func (p *APIProvider) ensureServer() error {
	exists, err := p.configExists(fmt.Sprintf("/config/apps/http/servers/%s", p.serverName))
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Bootstrap a minimal http app with an empty server.
	server := map[string]any{
		"listen": p.listen,
		"routes": []any{},
	}
	httpApp := map[string]any{
		"servers": map[string]any{p.serverName: server},
	}
	// PATCH the http app so an existing (empty) app is merged rather than
	// rejected with a 409 conflict like PUT would be.
	return p.do(http.MethodPatch, "/config/apps/http", httpApp, nil)
}

// do sends a request and treats any non-2xx status as an error.
func (p *APIProvider) do(method, path string, body any, out any) error {
	status, data, err := p.request(method, path, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("caddy admin %s %s returned %d: %s", method, path, status, string(data))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// request performs an admin API call and returns the status code and body.
func (p *APIProvider) request(method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(buf)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}
