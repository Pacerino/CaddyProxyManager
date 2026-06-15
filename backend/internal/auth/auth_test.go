package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestGenerateSecret(t *testing.T) {
	if _, err := GenerateSecret(""); err == nil {
		t.Error("expected error for empty password")
	}
	hash, err := GenerateSecret("hunter2")
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	if hash == "hunter2" || hash == "" {
		t.Error("secret should be a non-empty hash")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("hunter2")); err != nil {
		t.Errorf("hash does not verify: %v", err)
	}
}

func TestLoadPemPrivateAndPublicKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privPem := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	pubPem := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey),
	})

	priv, err := LoadPemPrivateKey(privPem)
	if err != nil {
		t.Fatalf("LoadPemPrivateKey: %v", err)
	}
	if priv.N.Cmp(key.N) != 0 {
		t.Error("loaded private key differs")
	}

	pub, err := LoadPemPublicKey(pubPem)
	if err != nil {
		t.Fatalf("LoadPemPublicKey: %v", err)
	}
	if pub.N.Cmp(key.PublicKey.N) != 0 {
		t.Error("loaded public key differs")
	}
}

func TestLoadPemPrivateKeyInvalid(t *testing.T) {
	if _, err := LoadPemPrivateKey([]byte("-----BEGIN RSA PRIVATE KEY-----\nbad\n-----END RSA PRIVATE KEY-----")); err == nil {
		t.Error("expected error for invalid PEM")
	}
}
