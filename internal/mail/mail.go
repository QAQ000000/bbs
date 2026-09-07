// SPDX-License-Identifier: AGPL-3.0-or-later
// Package mail renders versioned plain-text templates and performs bounded SMTP delivery.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	stdmail "net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host                                    string
	Port                                    int
	User, Password, From, SiteName, SiteURL string
}
type Mailer struct{ cfg Config }
type Message struct {
	ID                                       int64
	To, Kind, Link, FromName, Title, Excerpt string
}

func New(cfg Config, _ *slog.Logger) *Mailer { return &Mailer{cfg: cfg} }
func (m *Mailer) Enabled() bool              { return m != nil && m.cfg.Host != "" }

func (m *Mailer) render(msg Message) (string, string, error) {
	var subject, body string
	switch msg.Kind {
	case "password_reset":
		subject = "密码重置"
		body = "有人申请了您账号的密码重置。请通过下面的链接设置新密码：\n\n" + msg.Link + "\n\n链接 15 分钟内有效。如果不是您本人操作，请忽略本邮件。"
	case "email_verify":
		subject = "邮箱验证"
		body = "请通过下面的链接验证您的邮箱地址：\n\n" + msg.Link + "\n\n链接 24 小时内有效。如果不是您本人操作，请忽略本邮件。"
	case "mention":
		subject = msg.FromName + " 在帖子中提到了你"
		body = msg.FromName + " 在帖子《" + msg.Title + "》中提到了你：\n\n" + msg.Excerpt + "\n\n" + msg.Link
	case "reply", "reply.direct", "subscription":
		subject = "主题《" + msg.Title + "》有新内容"
		body = msg.FromName + " 在主题《" + msg.Title + "》中发布了内容：\n\n" + msg.Excerpt + "\n\n" + msg.Link
	default:
		return "", "", errors.New("unsupported template")
	}
	subject = strings.NewReplacer("\r", " ", "\n", " ").Replace("[" + m.cfg.SiteName + "] " + subject)
	return subject, body + "\n\n--\n" + m.cfg.SiteName, nil
}

// ErrorCode deliberately discards SMTP server text, which can echo credentials or recipients.
func ErrorCode(err error) (string, bool) {
	var smtpErr *textproto.Error
	if errors.As(err, &smtpErr) {
		return "smtp_" + strconv.Itoa(smtpErr.Code), smtpErr.Code >= 500 && smtpErr.Code < 600
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled_transport", false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", false
	}
	return "transport_error", false
}

func (m *Mailer) Send(ctx context.Context, msg Message) error {
	if !m.Enabled() {
		return errors.New("smtp disabled")
	}
	if strings.ContainsAny(msg.To+m.cfg.From, "\r\n") {
		return &textproto.Error{Code: 550, Msg: "invalid address"}
	}
	to, err := stdmail.ParseAddress(msg.To)
	if err != nil {
		return &textproto.Error{Code: 550, Msg: "invalid recipient"}
	}
	from, err := stdmail.ParseAddress(m.cfg.From)
	if err != nil {
		return &textproto.Error{Code: 550, Msg: "invalid sender"}
	}
	subject, body, err := m.render(msg)
	if err != nil {
		return err
	}
	at := strings.LastIndex(from.Address, "@")
	if at < 1 {
		return &textproto.Error{Code: 550, Msg: "invalid sender"}
	}
	domain := from.Address[at+1:]
	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <gobbs-email-%d@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n", from.String(), to.String(), mime.QEncoding.Encode("utf-8", subject), time.Now().Format(time.RFC1123Z), msg.ID, domain)
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline := time.Now().Add(60 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	cl, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return err
	}
	defer cl.Close()
	if ok, _ := cl.Extension("STARTTLS"); ok {
		if err = cl.StartTLS(&tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if m.cfg.User != "" {
		if err = cl.Auth(smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)); err != nil {
			return err
		}
	}
	if err = cl.Mail(from.Address); err != nil {
		return err
	}
	if err = cl.Rcpt(to.Address); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write([]byte(headers + strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	// DATA acceptance is delivery success; a lost QUIT response must not trigger a retry.
	_ = cl.Quit()
	return nil
}
