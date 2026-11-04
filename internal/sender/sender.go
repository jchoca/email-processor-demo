// Package sender delivers MIME messages to an SMTP server over TLS.
package sender

import (
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"time"
)

// Config describes the SMTP relay. An empty Username skips AUTH.
type Config struct {
	Addr       string
	Username   string
	Password   string
	RequireTLS bool          // fail instead of sending plaintext
	Timeout    time.Duration // dial and whole-session deadline; 30s when zero
}

// Send delivers msg to the envelope recipients. SMTP 5xx failures are returned
// as *textproto.Error so callers can tell permanent from transient errors.
func (config Config) Send(from string, to []string, msg []byte) error {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	host, _, err := net.SplitHostPort(config.Addr)
	if err != nil {
		return err
	}

	conn, err := net.DialTimeout("tcp", config.Addr, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	// one deadline for the whole session, so a stalled peer cannot hang us
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if config.RequireTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("smtp: server does not offer STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if config.Username != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("smtp: server does not offer AUTH")
		}
		if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, host)); err != nil {
			return err
		}
	}

	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	dataWriter, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := dataWriter.Write(msg); err != nil {
		dataWriter.Close()
		return err
	}
	if err := dataWriter.Close(); err != nil {
		return err
	}
	return client.Quit()
}
