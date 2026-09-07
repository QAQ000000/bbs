package mfa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
)

func NewSecret() (string, error) {
	b := make([]byte, 20)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}
func Code(secret string, step int64) string {
	raw, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if e != nil || len(raw) < 20 || step < 0 {
		return ""
	}
	code, err := hotp.GenerateCodeCustom(secret, uint64(step), hotp.ValidateOpts{Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return ""
	}
	return code
}
func Verify(secret, code string, now time.Time, last int64) (int64, bool) {
	if len(code) != 6 {
		return last, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return last, false
		}
	}
	step := now.Unix() / 30
	for _, d := range []int64{-1, 0, 1} {
		s := step + d
		if s <= last {
			continue
		}
		want := Code(secret, s)
		if len(want) == 6 && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return s, true
		}
	}
	return last, false
}

type Cipher struct{ aead cipher.AEAD }

func NewCipher(key string) (*Cipher, error) {
	b, e := hex.DecodeString(key)
	if e != nil || len(b) != 32 {
		return nil, errors.New("FORUM_MFA_KEY must contain 64 hex characters")
	}
	bl, e := aes.NewCipher(b)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(bl)
	if e != nil {
		return nil, e
	}
	return &Cipher{aead: a}, nil
}
func (c *Cipher) Seal(raw string) (string, error) {
	if c == nil {
		return "", errors.New("MFA key unavailable")
	}
	n := make([]byte, c.aead.NonceSize())
	if _, e := rand.Read(n); e != nil {
		return "", e
	}
	return base64.RawStdEncoding.EncodeToString(c.aead.Seal(n, n, []byte(raw), []byte("gobbs-mfa-v1"))), nil
}
func (c *Cipher) Open(v string) (string, error) {
	if c == nil {
		return "", errors.New("MFA key unavailable")
	}
	b, e := base64.RawStdEncoding.DecodeString(v)
	if e != nil || len(b) < c.aead.NonceSize() {
		return "", errors.New("invalid mfa secret")
	}
	x, e := c.aead.Open(nil, b[:c.aead.NonceSize()], b[c.aead.NonceSize():], []byte("gobbs-mfa-v1"))
	return string(x), e
}
func HashRecovery(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if len(v) != 26 {
		return ""
	}
	if raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(v); err != nil || len(raw) != 16 || base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw) != v {
		return ""
	}
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}

func RecoveryCodes() (codes, hashes []string, err error) {
	for i := 0; i < 10; i++ {
		b := make([]byte, 16)
		if _, err = rand.Read(b); err != nil {
			return nil, nil, err
		}
		code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
		codes = append(codes, code)
		hashes = append(hashes, HashRecovery(code))
	}
	return codes, hashes, nil
}
