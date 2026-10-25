package pluginmgr

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBuildService_RejectsRedirectToHTTP(t *testing.T) {
	var plainHits int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&plainHits, 1)
		w.Write([]byte(packagesFixture))
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer secure.Close()

	s := NewBuildService()
	s.BaseURL = secure.URL
	s.Client.Transport = secure.Client().Transport // trust the test certificate, keep CheckRedirect
	_, err := s.Packages(t.Context())
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err = %v, want a refused redirect", err)
	}
	if atomic.LoadInt32(&plainHits) != 0 {
		t.Fatal("the plain HTTP target must not be requested")
	}
}

func TestBuildService_FollowsHTTPSRedirect(t *testing.T) {
	var target *httptest.Server
	target = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			w.Write([]byte(packagesFixture))
			return
		}
		http.Redirect(w, r, target.URL+"/moved", http.StatusFound)
	}))
	defer target.Close()

	s := NewBuildService()
	s.BaseURL = target.URL
	s.Client.Transport = target.Client().Transport
	pkgs, err := s.Packages(t.Context())
	if err != nil || len(pkgs) != 2 {
		t.Fatalf("Packages = %v, %v", pkgs, err)
	}
}
