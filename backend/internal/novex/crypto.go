package novex

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

func loadKey() ([]byte, error) {
	v := os.Getenv("NOVEX_MASTER_KEY")
	if v == "" {
		v = os.Getenv("NOVEX_ENCRYPTION_KEY")
	}
	b, e := base64.StdEncoding.DecodeString(v)
	if e != nil || len(b) != 32 {
		b, e = hex.DecodeString(v)
	}
	if e != nil || len(b) != 32 {
		return nil, errors.New("NOVEX_MASTER_KEY must encode exactly 32 bytes (base64 or hex)")
	}
	return b, nil
}
func encrypt(key []byte, plain string) (string, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plain), []byte("novex-datasource-v1"))), nil
}
func decrypt(key []byte, encoded string) (string, error) {
	raw, e := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if e != nil {
		return "", e
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return "", e
	}
	if len(raw) < g.NonceSize() {
		return "", errors.New("invalid encrypted credential")
	}
	plain, e := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], []byte("novex-datasource-v1"))
	return string(plain), e
}
