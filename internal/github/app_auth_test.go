package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func TestAppAuthenticatorJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	auth, err := NewAppAuthenticator(123, pemBytes)
	if err != nil {
		t.Fatalf("NewAppAuthenticator returned error: %v", err)
	}
	jwt, err := auth.JWT(time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("JWT returned error: %v", err)
	}
	if strings.Count(jwt, ".") != 2 {
		t.Fatalf("jwt = %q, want three segments", jwt)
	}
}
