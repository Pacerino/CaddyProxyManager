package auth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
)

var (
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
)

func openKey(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fileKey, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return fileKey, nil
}

// GetPrivateKey will load the key from config package and return a usable object
// It should only load from file once per program execution
func GetPrivateKey() (*rsa.PrivateKey, error) {
	if privateKey == nil {
		var blankKey *rsa.PrivateKey

		var err error
		key, err := openKey(fmt.Sprintf("%s/jwtkey.pem", config.Configuration.DataFolder))
		if err != nil {
			return blankKey, err
		}
		privateKey, err = LoadPemPrivateKey(key)
		if err != nil {
			return blankKey, err
		}
	}

	/* pub, pubErr := GetPublicKey()
	if pubErr != nil {
		return privateKey, pubErr
	}

	privateKey.PublicKey = *pub */

	return privateKey, nil
}

// LoadPemPrivateKey reads a key from a PEM encoded string and returns a private key
func LoadPemPrivateKey(content []byte) (*rsa.PrivateKey, error) {
	var key *rsa.PrivateKey
	data, _ := pem.Decode(content)
	var err error
	key, err = x509.ParsePKCS1PrivateKey(data.Bytes)
	if err != nil {
		return key, err
	}
	return key, nil
}

// GetPublicKey will load the key from config package and return a usable object
// It should only load once per program execution
func GetPublicKey() (*rsa.PublicKey, error) {
	if publicKey == nil {
		var blankKey *rsa.PublicKey

		var err error
		key, err := openKey(fmt.Sprintf("%s/jwtkey.pem", config.Configuration.DataFolder))
		if err != nil {
			return blankKey, err
		}
		publicKey, err = LoadPemPublicKey(key)
		if err != nil {
			return blankKey, err
		}
	}

	return publicKey, nil
}

// LoadPemPublicKey reads a key from a PEM encoded string and returns a public key
func LoadPemPublicKey(content []byte) (*rsa.PublicKey, error) {
	var key *rsa.PublicKey
	data, _ := pem.Decode(content)
	publicKeyFileImported, err := x509.ParsePKCS1PublicKey(data.Bytes)
	if err != nil {
		return key, err
	}

	return publicKeyFileImported, nil
}
