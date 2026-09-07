package mfa

import (
	"strings"
	"testing"
	"time"
)

func TestRFCVectorsAndReplay(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	// RFC 4226 counters and RFC 6238 SHA1 vectors truncated to six digits.
	for i, want := range []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"} {
		if got := Code(secret, int64(i)); got != want {
			t.Fatalf("counter %d: %s", i, got)
		}
	}
	for _, v := range []struct {
		timestamp int64
		code      string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		now := time.Unix(v.timestamp, 0)
		if Code(secret, v.timestamp/30) != v.code {
			t.Fatal(v)
		}
		step, ok := Verify(secret, v.code, now, -1)
		if !ok {
			t.Fatal(v)
		}
		if _, ok = Verify(secret, v.code, now, step); ok {
			t.Fatal("replay")
		}
	}
	for _, bad := range []string{"", "12345", "+12345", "1234567", "12a456", " 287082"} {
		if _, ok := Verify(secret, bad, time.Unix(59, 0), -1); ok {
			t.Fatal(bad)
		}
	}
	if Code("", 1) != "" || Code("MY", 1) != "" || Code(secret, -1) != "" {
		t.Fatal("invalid secret/counter")
	}
	if _, ok := Verify(secret, Code(secret, 100), time.Unix(102*30, 0), -1); ok {
		t.Fatal("outside window")
	}
}

func TestRecoveryAndCipher(t *testing.T) {
	codes, hashes, err := RecoveryCodes()
	if err != nil || len(codes) != 10 {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i, c := range codes {
		if len(c) != 26 || len(hashes[i]) != 64 || seen[c] || HashRecovery(strings.ToLower(c)) != hashes[i] {
			t.Fatal("invalid recovery code")
		}
		seen[c] = true
	}
	if HashRecovery("bad") != "" {
		t.Fatal("invalid recovery accepted")
	}
	c, err := NewCipher(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Seal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Open(sealed); err != nil || got != secret {
		t.Fatal(err)
	}
	wrong, _ := NewCipher(strings.Repeat("cd", 32))
	if _, err := wrong.Open(sealed); err == nil {
		t.Fatal("wrong key")
	}
	if _, err := c.Open("A" + sealed); err == nil {
		t.Fatal("tamper")
	}
	if _, err := (*Cipher)(nil).Open(sealed); err == nil {
		t.Fatal("nil key")
	}
}
