// SPDX-License-Identifier: AGPL-3.0-or-later
// Package mail 异步邮件通知（net/smtp 实现，支持 STARTTLS 与可选认证）。
package mail

import (
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

type Config struct {
	Host     string // 为空则禁用
	Port     int
	User     string
	Password string
	From     string
	SiteName string
	SiteURL  string
}

// Mailer 异步发送队列。
type Mailer struct {
	cfg  Config
	ch   chan message
	log  *slog.Logger
}

type message struct {
	to      string
	subject string
	body    string
}

func New(cfg Config, logger *slog.Logger) *Mailer {
	m := &Mailer{cfg: cfg, ch: make(chan message, 128), log: logger}
	if m.Enabled() {
		go m.worker()
	}
	return m
}

func (m *Mailer) Enabled() bool { return m != nil && m.cfg.Host != "" }

// NotifyPasswordReset 密码重置邮件（异步；队列满则丢弃）。
func (m *Mailer) NotifyPasswordReset(to, link string) {
	if !m.Enabled() || to == "" {
		return
	}
	subject := fmt.Sprintf("[%s] 密码重置", m.cfg.SiteName)
	body := strings.Join([]string{
		"有人申请了您账号的密码重置。如果是您本人，请通过下面的链接设置新密码：",
		"",
		link,
		"",
		"链接 15 分钟内有效。如果不是您本人操作，请忽略本邮件。",
		"--",
		m.cfg.SiteName,
	}, "\n")
	select {
	case m.ch <- message{to: to, subject: subject, body: body}:
	default:
		m.log.Warn("邮件队列已满，丢弃一封提醒", "to", to)
	}
}

// NotifyMention 被提及邮件（异步；队列满则丢弃）。
func (m *Mailer) NotifyMention(to, fromName, threadTitle, link, excerpt string) {
	if !m.Enabled() || to == "" {
		return
	}
	subject := fmt.Sprintf("[%s] %s 在帖子中提到了你", m.cfg.SiteName, fromName)
	body := strings.Join([]string{
		fromName + " 在帖子《" + threadTitle + "》中提到了你：",
		"",
		"> " + excerpt,
		"",
		"查看完整内容：" + link,
		"",
		"--",
		m.cfg.SiteName,
	}, "\n")
	select {
	case m.ch <- message{to: to, subject: subject, body: body}:
	default:
		m.log.Warn("邮件队列已满，丢弃一封提醒", "to", to)
	}
}

func (m *Mailer) worker() {
	for msg := range m.ch {
		if err := m.send(msg); err != nil {
			m.log.Warn("邮件发送失败", "to", msg.to, "err", err)
		} else {
			m.log.Info("邮件已发送", "to", msg.to, "subject", msg.subject)
		}
	}
}

func (m *Mailer) send(msg message) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	from := m.cfg.From
	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n"+
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n", from, msg.to, msg.subject)
	body := headers + msg.body

	cl, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer cl.Close()
	if ok, _ := cl.Extension("STARTTLS"); ok {
		if err := cl.StartTLS(nil); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if m.cfg.User != "" {
		auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)
		if err := cl.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := cl.Mail(from); err != nil {
		return err
	}
	if err := cl.Rcpt(msg.to); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}
