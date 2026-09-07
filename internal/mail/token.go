package mail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

type TokenCipher struct{ aead cipher.AEAD }

func NewTokenCipher(key string) (*TokenCipher, error) {
	b, err := hex.DecodeString(key)
	if err != nil || len(b) != 32 {
		return nil, errors.New("FORUM_MAIL_KEY must contain 64 hex characters")
	}
	block, err := aes.NewCipher(b)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &TokenCipher{aead: aead}, nil
}

func (c *TokenCipher) Seal(raw string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(c.aead.Seal(nonce, nonce, []byte(raw), []byte("gobbs-auth-email-v1"))), nil
}

func (c *TokenCipher) Open(sealed string) (string, error) {
	b, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil || len(b) < c.aead.NonceSize() {
		return "", errors.New("invalid encrypted token")
	}
	raw, err := c.aead.Open(nil, b[:c.aead.NonceSize()], b[c.aead.NonceSize():], []byte("gobbs-auth-email-v1"))
	return string(raw), err
}
