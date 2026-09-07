package mail

import (
	"context"
	"log/slog"
	"mime"
	stdmail "net/mail"
	"strings"
	"testing"
	"time"

	"dzforum/internal/mailtest"
)

func TestSMTPDeliveryEncodingAndLostQuit(t *testing.T) {
	smtp := mailtest.New(t)
	smtp.DropQuit.Store(true)
	m := New(Config{Host: smtp.Host, Port: smtp.Port, From: "no-reply@example.test", SiteName: "测试站"}, slog.Default())
	msg := Message{ID: 123, To: "user@example.test", Kind: "mention", FromName: "作者\r\nBcc: bad@example.test", Title: "主题", Excerpt: "测试正文", Link: "https://example.test/thread"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.Send(ctx, msg); err != nil {
		t.Fatal(err)
	}
	body := <-smtp.Messages
	parsed, err := stdmail.ReadMessage(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Get("Bcc") != "" || parsed.Header.Get("Message-ID") != "<gobbs-email-123@example.test>" {
		t.Fatal("invalid or injected header", body)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || !strings.Contains(subject, "测试站") {
		t.Fatal(subject, err)
	}
	if !strings.Contains(body, "测试正文") {
		t.Fatal(body)
	}
	msg.To = "victim@example.test\r\nBcc: bad@example.test"
	if code, permanent := ErrorCode(m.Send(ctx, msg)); code != "smtp_550" || !permanent {
		t.Fatal(code, permanent)
	}
}

func TestSMTPFailureClassification(t *testing.T) {
	smtp := mailtest.New(t)
	m := New(Config{Host: smtp.Host, Port: smtp.Port, From: "noreply@example.test"}, slog.Default())
	for _, n := range []int32{451, 550} {
		smtp.Reject.Store(n)
		err := m.Send(context.Background(), Message{To: "user@example.test", Kind: "email_verify"})
		code, permanent := ErrorCode(err)
		if err == nil || permanent != (n == 550) || strings.Contains(code, "private") {
			t.Fatal(code, permanent, err)
		}
	}
}

func TestTokenCipherRestartAndTamper(t *testing.T) {
	key := strings.Repeat("ab", 32)
	c, err := NewTokenCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Seal("secret-token")
	if err != nil || strings.Contains(sealed, "secret-token") {
		t.Fatal(err)
	}
	restarted, _ := NewTokenCipher(key)
	if raw, err := restarted.Open(sealed); err != nil || raw != "secret-token" {
		t.Fatal(raw, err)
	}
	other, _ := NewTokenCipher(strings.Repeat("cd", 32))
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err := c.Open("short"); err == nil {
		t.Fatal("corrupt token accepted")
	}
	if _, err := NewTokenCipher(""); err == nil {
		t.Fatal("missing key accepted")
	}
}
