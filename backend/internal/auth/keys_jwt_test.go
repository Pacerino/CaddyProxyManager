package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

// resetKeyCache clears the package-level key cache so a test controls loading.
func resetKeyCache() {
	privateKey = nil
	publicKey = nil
}

func TestEnsureKeyAndLoad(t *testing.T) {
	dir := t.TempDir()
	prev := config.Configuration.DataFolder
	t.Cleanup(func() {
		config.Configuration.DataFolder = prev
		resetKeyCache()
	})
	config.Configuration.DataFolder = dir
	resetKeyCache()

	// First call generates the key.
	if err := EnsureKey(); err != nil {
		t.Fatalf("EnsureKey: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "jwtkey.pem")); err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	// Second call is a no-op (key exists).
	if err := EnsureKey(); err != nil {
		t.Fatalf("EnsureKey idempotent: %v", err)
	}

	priv, err := GetPrivateKey()
	if err != nil {
		t.Fatalf("GetPrivateKey: %v", err)
	}
	if priv == nil {
		t.Fatal("expected a private key")
	}
}

func TestGenerateJWT(t *testing.T) {
	dir := t.TempDir()
	prev := config.Configuration.DataFolder
	t.Cleanup(func() {
		config.Configuration.DataFolder = prev
		resetKeyCache()
	})
	config.Configuration.DataFolder = dir
	resetKeyCache()
	if err := EnsureKey(); err != nil {
		t.Fatal(err)
	}

	user := &database.User{}
	user.ID = 99
	resp, err := Generate(user)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected a non-empty token")
	}
	if resp.Expires == 0 {
		t.Error("expected an expiry")
	}
}

func TestGetPrivateKeyMissing(t *testing.T) {
	prev := config.Configuration.DataFolder
	t.Cleanup(func() {
		config.Configuration.DataFolder = prev
		resetKeyCache()
	})
	config.Configuration.DataFolder = filepath.Join(t.TempDir(), "no-key-here")
	resetKeyCache()

	if _, err := GetPrivateKey(); err == nil {
		t.Error("expected error when key file is absent")
	}
}
