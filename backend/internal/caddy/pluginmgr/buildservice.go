// Package pluginmgr installs Caddy plugins at runtime by downloading custom
// Caddy builds from caddyserver.com's build service.
package pluginmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultBaseURL    = "https://caddyserver.com"
	packagesCacheTTL  = 5 * time.Minute
	downloadTimeout   = 10 * time.Minute
	maxErrorBodyBytes = 1 << 20
)

// Package is a plugin listed by the Caddy build service.
type Package struct {
	Path    string   `json:"path"`
	Repo    string   `json:"repo"`
	Modules []string `json:"modules"`
}

// PinnedPackage is a plugin path with an optional version (empty = latest).
type PinnedPackage struct {
	Path, Version string
}

// Param renders the value of a `p` query parameter: `path` or `path@version`.
func (p PinnedPackage) Param() string {
	if p.Version == "" {
		return p.Path
	}
	return p.Path + "@" + p.Version
}

// BuildRequest describes a custom Caddy build to download.
type BuildRequest struct {
	OS, Arch, CaddyVersion string
	Packages               []PinnedPackage
}

// BuildError is returned when the build service rejects a request.
type BuildError struct {
	Status  int
	Message string
}

func (e *BuildError) Error() string {
	return fmt.Sprintf("caddy build service returned %d: %s", e.Status, e.Message)
}

// BuildService is a client for caddyserver.com's package list and download API.
type BuildService struct {
	BaseURL string
	Client  *http.Client

	mu       sync.Mutex
	cached   []Package
	cachedAt time.Time
}

// NewBuildService returns a client for the public build service.
func NewBuildService() *BuildService {
	return &BuildService{
		BaseURL: defaultBaseURL,
		Client:  &http.Client{Timeout: downloadTimeout, CheckRedirect: httpsOnlyRedirect},
	}
}

// httpsOnlyRedirect follows redirects (at most 10, like the default) only to
// https URLs: the downloads carry no signature, so HTTPS is their only
// protection.
func httpsOnlyRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing redirect to non-https URL %s", req.URL.Redacted())
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
}

// Packages lists the available plugins. Successful results are cached for
// five minutes; errors are never cached.
func (s *BuildService) Packages(ctx context.Context) ([]Package, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && time.Since(s.cachedAt) < packagesCacheTTL {
		return s.cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+"/api/packages", nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, newBuildError(resp)
	}

	var body struct {
		Result []struct {
			Path    string `json:"path"`
			Repo    string `json:"repo"`
			Modules []struct {
				Name string `json:"name"`
			} `json:"modules"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode package list: %w", err)
	}
	pkgs := make([]Package, 0, len(body.Result))
	for _, r := range body.Result {
		p := Package{Path: r.Path, Repo: r.Repo, Modules: make([]string, 0, len(r.Modules))}
		for _, m := range r.Modules {
			p.Modules = append(p.Modules, m.Name)
		}
		pkgs = append(pkgs, p)
	}
	s.cached, s.cachedAt = pkgs, time.Now()
	return pkgs, nil
}

// Download streams a custom Caddy build into w. Nothing is written to w
// unless the service answers 200.
func (s *BuildService) Download(ctx context.Context, req BuildRequest, w io.Writer) error {
	q := url.Values{}
	q.Set("os", req.OS)
	q.Set("arch", req.Arch)
	q.Set("version", req.CaddyVersion)
	for _, p := range req.Packages {
		q.Add("p", p.Param())
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+"/api/download?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := s.Client.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return newBuildError(resp)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// newBuildError extracts error.message from the service's JSON error body,
// falling back to the HTTP status text.
func newBuildError(resp *http.Response) *BuildError {
	be := &BuildError{Status: resp.StatusCode}
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if json.Unmarshal(data, &body) == nil && body.Error.Message != "" {
		be.Message = body.Error.Message
	} else {
		be.Message = http.StatusText(resp.StatusCode)
	}
	return be
}
