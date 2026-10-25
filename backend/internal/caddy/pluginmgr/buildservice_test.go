package pluginmgr

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const packagesFixture = `{"status_code":200,"result":[
{"path":"github.com/caddy-dns/duckdns","repo":"https://github.com/caddy-dns/duckdns","modules":[{"name":"dns.providers.duckdns"}],"downloads":5,"published":"2024-01-01"},
{"path":"github.com/x/multi","repo":"https://github.com/x/multi","modules":[{"name":"http.handlers.a"},{"name":"http.handlers.b"}]}]}`

func newService(srv *httptest.Server) *BuildService {
	s := NewBuildService()
	s.BaseURL = srv.URL
	s.Client = srv.Client()
	return s
}

func TestPackages_Parses(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path != "/api/packages" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(packagesFixture))
	}))
	defer srv.Close()
	s := newService(srv)

	pkgs, err := s.Packages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages", len(pkgs))
	}
	if pkgs[0].Path != "github.com/caddy-dns/duckdns" || pkgs[0].Repo != "https://github.com/caddy-dns/duckdns" ||
		len(pkgs[0].Modules) != 1 || pkgs[0].Modules[0] != "dns.providers.duckdns" {
		t.Errorf("bad first package: %+v", pkgs[0])
	}
	if len(pkgs[1].Modules) != 2 || pkgs[1].Modules[1] != "http.handlers.b" {
		t.Errorf("bad second package: %+v", pkgs[1])
	}
	if _, err := s.Packages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("expected 1 server hit, got %d", n)
	}
}

func TestPackages_ErrorNotCached(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(packagesFixture))
	}))
	defer srv.Close()
	s := newService(srv)

	if _, err := s.Packages(context.Background()); err == nil {
		t.Fatal("expected error on 503")
	}
	pkgs, err := s.Packages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Errorf("got %d packages", len(pkgs))
	}
}

func TestDownload_BuildsQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/download" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("os") != "linux" || q.Get("arch") != "arm64" || q.Get("version") != "v2.11.4" {
			t.Errorf("bad query: %s", r.URL.RawQuery)
		}
		p := q["p"]
		if len(p) != 2 || p[0] != "github.com/a/b@v1.2.3" || p[1] != "github.com/c/d" {
			t.Errorf("bad p values: %v", p)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("BINARY"))
	}))
	defer srv.Close()
	s := newService(srv)

	var buf bytes.Buffer
	err := s.Download(context.Background(), BuildRequest{
		OS: "linux", Arch: "arm64", CaddyVersion: "v2.11.4",
		Packages: []PinnedPackage{{Path: "github.com/a/b", Version: "v1.2.3"}, {Path: "github.com/c/d"}},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "BINARY" {
		t.Errorf("body = %q", buf.String())
	}
}

func TestDownload_BuildError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status_code":400,"error":{"message":"x is not a registered Caddy module package path"}}`))
	}))
	defer srv.Close()
	s := newService(srv)

	var buf bytes.Buffer
	err := s.Download(context.Background(), BuildRequest{OS: "linux", Arch: "amd64", CaddyVersion: "v2.11.4"}, &buf)
	var be *BuildError
	if !errors.As(err, &be) {
		t.Fatalf("expected *BuildError, got %v", err)
	}
	if be.Status != 400 || be.Message != "x is not a registered Caddy module package path" {
		t.Errorf("bad BuildError: %+v", be)
	}
	if buf.Len() != 0 {
		t.Error("nothing should be written on error")
	}
}

func TestDownload_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("BINARY"))
	}))
	defer srv.Close()
	s := newService(srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	if err := s.Download(ctx, BuildRequest{OS: "linux", Arch: "amd64", CaddyVersion: "v2.11.4"}, &buf); err == nil {
		t.Fatal("expected error")
	}
	if buf.Len() != 0 {
		t.Error("nothing should be written")
	}
}

func TestPinnedPackage_Param(t *testing.T) {
	if got := (PinnedPackage{Path: "a/b"}).Param(); got != "a/b" {
		t.Errorf("got %q", got)
	}
	if got := (PinnedPackage{Path: "a/b", Version: "v1"}).Param(); got != "a/b@v1" {
		t.Errorf("got %q", got)
	}
}
