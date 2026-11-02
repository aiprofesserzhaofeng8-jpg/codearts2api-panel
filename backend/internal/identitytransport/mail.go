package identitytransport

import (
	"codearts2api/internal/access"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"time"
)

type Client struct{ tlsConfig *tls.Config }

func (c *Client) Send(ctx context.Context, cfg access.IdentitySettings, to, code string) error {
	address := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	tlsConfig := &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12}
	if c.tlsConfig != nil {
		tlsConfig = c.tlsConfig.Clone()
		tlsConfig.ServerName = cfg.SMTPHost
	}
	if cfg.SMTPPort == 465 {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: tlsConfig}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = d.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()
	if cfg.SMTPPort != 465 {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP 必须支持 STARTTLS 或使用 465 TLS")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if cfg.SMTPUser != "" {
		if err = client.Auth(smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPHost)); err != nil {
			return err
		}
	}
	if err = client.Mail(cfg.SMTPFrom); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: Account verification code\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour verification code: %s\r\nValid for 10 minutes. If you did not request this, ignore this email.\r\n", cfg.SMTPFrom, to, code)
	if err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
