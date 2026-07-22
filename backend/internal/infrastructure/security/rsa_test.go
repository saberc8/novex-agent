package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"testing"
)

func TestRSADecryptorRoundTrip(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	decryptor, err := NewRSADecryptorFromBase64(base64.StdEncoding.EncodeToString(encodedKey))
	if err != nil {
		t.Fatalf("NewRSADecryptorFromBase64: %v", err)
	}
	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, &privateKey.PublicKey, []byte("Strong-123!"))
	if err != nil {
		t.Fatalf("EncryptPKCS1v15: %v", err)
	}
	plaintext, err := decryptor.DecryptBase64(base64.StdEncoding.EncodeToString(ciphertext))
	if err != nil {
		t.Fatalf("DecryptBase64: %v", err)
	}
	if plaintext != "Strong-123!" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestRSADecryptorRejectsWeakKey(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	if _, err := NewRSADecryptorFromBase64(base64.StdEncoding.EncodeToString(encodedKey)); err == nil {
		t.Fatal("expected weak RSA key to be rejected")
	}
}
