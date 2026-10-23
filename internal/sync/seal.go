package sync

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ErrSealedSecret reports a sealed value that cannot be opened, either because
// the stored bytes were tampered with or because the deployment encryption key
// changed. Publishers resolve it by re-entering the secret.
var ErrSealedSecret = errors.New("sealed sync secret is unreadable")

// Sealer seals adapter secrets with AES-256-GCM.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer accepts a base64 (standard or raw) 32-byte key or 64 hex
// characters.
func NewSealer(key string) (*Sealer, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("sync encryption key is required")
	}
	raw, err := decodeKey(key)
	if err != nil {
		return nil, err
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("sync encryption key must decode to 32 bytes, got %d", len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("create sync cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create sync GCM: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

func decodeKey(key string) ([]byte, error) {
	if len(key) == 64 {
		if raw, err := hex.DecodeString(key); err == nil {
			return raw, nil
		}
	}
	if raw, err := base64.StdEncoding.DecodeString(key); err == nil {
		return raw, nil
	}
	if raw, err := base64.RawStdEncoding.DecodeString(key); err == nil {
		return raw, nil
	}
	if raw, err := base64.URLEncoding.DecodeString(key); err == nil {
		return raw, nil
	}
	if raw, err := base64.RawURLEncoding.DecodeString(key); err == nil {
		return raw, nil
	}
	return nil, fmt.Errorf("sync encryption key must be base64 or hex encoded")
}

// Seal returns base64(nonce||ciphertext).
func (s *Sealer) Seal(plain []byte) (string, error) {
	if s == nil || s.aead == nil {
		return "", fmt.Errorf("sync sealer is not configured")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate sync secret nonce: %w", err)
	}
	sealed := s.aead.Seal(nonce, nonce, plain, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Open reverses Seal. Any tampering or key change yields ErrSealedSecret.
func (s *Sealer) Open(sealed string) ([]byte, error) {
	if s == nil || s.aead == nil {
		return nil, ErrSealedSecret
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sealed))
	if err != nil {
		return nil, ErrSealedSecret
	}
	nonceSize := s.aead.NonceSize()
	if len(raw) < nonceSize+1 {
		return nil, ErrSealedSecret
	}
	plain, err := s.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return nil, ErrSealedSecret
	}
	return plain, nil
}
