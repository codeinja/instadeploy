package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// SecretBox encrypts environment variables, secrets and registry passwords
// before they're stored (AES-256-GCM). The key comes from SECRETS_KEY, or
// is generated once into DATA_DIR/secrets.key.
//
// Losing the key means losing every stored secret, so back it up together
// with the database.
type SecretBox struct {
	aead cipher.AEAD
}

const secretBoxVersion = 1

func loadSecretBox(cfg Config) (*SecretBox, error) {
	key, err := secretsKey(cfg)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

func secretsKey(cfg Config) ([]byte, error) {
	if cfg.SecretsKey != "" {
		return decodeKey(cfg.SecretsKey)
	}
	path := filepath.Join(cfg.DataDir, "secrets.key")
	if raw, err := os.ReadFile(path); err == nil {
		return decodeKey(strings.TrimSpace(string(raw)))
	}
	key := make([]byte, 32)
	rand.Read(key)
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return nil, fmt.Errorf("save generated secrets key: %w", err)
	}
	log.Printf("generated a new secrets key in %s (back it up, or set SECRETS_KEY)", path)
	return key, nil
}

// decodeKey accepts 32 bytes as hex (64 chars) or base64.
func decodeKey(s string) ([]byte, error) {
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, errors.New("SECRETS_KEY must be 32 bytes, hex or base64 encoded (generate one with: openssl rand -hex 32)")
}

// Encrypt returns version || nonce || ciphertext.
func (b *SecretBox) Encrypt(plain string) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	rand.Read(nonce)
	out := append([]byte{secretBoxVersion}, nonce...)
	return b.aead.Seal(out, nonce, []byte(plain), nil)
}

func (b *SecretBox) Decrypt(data []byte) (string, error) {
	n := b.aead.NonceSize()
	if len(data) < 1+n || data[0] != secretBoxVersion {
		return "", errors.New("unrecognized encrypted value")
	}
	plain, err := b.aead.Open(nil, data[1:1+n], data[1+n:], nil)
	if err != nil {
		return "", errors.New("cannot decrypt value (was SECRETS_KEY changed?)")
	}
	return string(plain), nil
}
