// Package email adapts outbound mail to ports.EmailSender: Log for local
// development (the message is logged, never sent), SMTP otherwise.
package email

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

// Log writes emails to the log. Only for APP_ENV=local: magic links appear
// in the api logs so a developer can sign in without a mail server.
type Log struct{ L *slog.Logger }

// Send logs the message.
func (l Log) Send(_ context.Context, to, subject, text string) error {
	l.L.Info("email (log provider, not sent)", "to_domain", domainOf(to), "subject", subject, "body", text)
	return nil
}

func domainOf(addr string) string {
	if i := strings.LastIndex(addr, "@"); i >= 0 {
		return addr[i+1:]
	}
	return ""
}

// SMTP sends through an SMTP relay with PLAIN auth.
type SMTP struct {
	Addr, User, Pass, From string
}

// Send delivers one plain-text message.
func (s SMTP) Send(_ context.Context, to, subject, text string) error {
	host := s.Addr
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s", s.From, to, subject, text)
	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Pass, host)
	}
	return smtp.SendMail(s.Addr, auth, extractAddr(s.From), []string{to}, []byte(msg))
}

func extractAddr(from string) string {
	if i := strings.Index(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j > 0 {
			return from[i+1 : i+j]
		}
	}
	return from
}
