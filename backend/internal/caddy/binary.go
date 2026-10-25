package caddy

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
)

// ActiveBinary returns the Caddy executable CPM should use: the managed
// binary (<datafolder>/caddy-bin/current) when one is installed, otherwise
// the configured one.
func ActiveBinary() string {
	managed := filepath.Join(config.Configuration.DataFolder, "caddy-bin", "current")
	if _, err := os.Stat(managed); err == nil {
		return managed
	}
	return config.Configuration.Caddy.Binary
}

// ActiveBinarySHA256 returns the lowercase hex SHA-256 of the active binary,
// or "" when it cannot be read (e.g. it is only resolvable via PATH).
func ActiveBinarySHA256() string {
	path := ActiveBinary()
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
