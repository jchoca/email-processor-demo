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
func (c Config) Send(from string, to []string, msg []byte) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return err
	}

	conn, err := net.DialTimeout("tcp", c.Addr, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	// one deadline for the whole session, so a stalled peer cannot hang us
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}

	cl, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer cl.Close()

	if c.RequireTLS {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errors.New("smtp: server does not offer STARTTLS")
		}
		if err := cl.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if c.Username != "" {
		if ok, _ := cl.Extension("AUTH"); !ok {
			return errors.New("smtp: server does not offer AUTH")
		}
		if err := cl.Auth(smtp.PlainAuth("", c.Username, c.Password, host)); err != nil {
			return err
		}
	}

	if err := cl.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := cl.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}
