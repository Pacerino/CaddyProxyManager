package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

// EnsureKey generates an RSA private key for signing JWTs if one does not
// already exist in the data folder.
func EnsureKey() error {
	path := fmt.Sprintf("%s/jwtkey.pem", config.Configuration.DataFolder)
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	logger.Info("Generating new JWT signing key at %s", path)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	encoded := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return os.WriteFile(path, encoded, 0600)
}
