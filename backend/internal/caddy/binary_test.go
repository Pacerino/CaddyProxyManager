package caddy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
)

func setupActive(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prevData, prevBin := config.Configuration.DataFolder, config.Configuration.Caddy.Binary
	config.Configuration.DataFolder = dir
	config.Configuration.Caddy.Binary = "/shipped/caddy"
	t.Cleanup(func() {
		config.Configuration.DataFolder, config.Configuration.Caddy.Binary = prevData, prevBin
	})
	return dir
}

func TestActiveBinary_PrefersManaged(t *testing.T) {
	dir := setupActive(t)
	bin := filepath.Join(dir, "caddy-bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(bin, "caddy-abc")
	if err := os.WriteFile(target, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(bin, "current")); err != nil {
		t.Fatal(err)
	}
	if got, want := ActiveBinary(), filepath.Join(bin, "current"); got != want {
		t.Fatalf("ActiveBinary = %q, want %q", got, want)
	}
}

func TestActiveBinary_FallsBackToConfig(t *testing.T) {
	setupActive(t)
	if got := ActiveBinary(); got != "/shipped/caddy" {
		t.Fatalf("ActiveBinary = %q", got)
	}
}
