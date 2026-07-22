package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
)

// RSADecryptor holds a parsed RSA private key and can decrypt
// Base64-encoded ciphertexts produced by the front-end.
type RSADecryptor struct {
	priv *rsa.PrivateKey
}

// NewRSADecryptorFromBase64 creates a decryptor from a Base64-encoded
// PKCS#8 private key, compatible with the Java SecureUtils configuration.
func NewRSADecryptorFromBase64(b64Key string) (*RSADecryptor, error) {
	if b64Key == "" {
		return nil, errors.New("rsa private key is empty")
	}
	der, err := base64.StdEncoding.DecodeString(b64Key)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse pkcs8 private key: %w", err)
	}
	priv, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("unexpected private key type %T", k)
	}
	if priv.N.BitLen() < 2048 {
		return nil, errors.New("rsa private key must contain at least 2048 bits")
	}
	return &RSADecryptor{priv: priv}, nil
}

// DecryptBase64 decrypts a Base64-encoded ciphertext using RSA/PKCS1v15.
// It returns the UTF-8 plaintext password.
func (d *RSADecryptor) DecryptBase64(cipherB64 string) (string, error) {
	if d == nil || d.priv == nil {
		return "", errors.New("rsa decryptor not initialized")
	}
	cipherBytes, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return "", fmt.Errorf("decode cipher text: %w", err)
	}
	plain, err := rsa.DecryptPKCS1v15(rand.Reader, d.priv, cipherBytes)
	if err != nil {
		return "", fmt.Errorf("rsa decrypt: %w", err)
	}
	return string(plain), nil
}
