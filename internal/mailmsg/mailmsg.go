// Package mailmsg renders transactional emails as MIME messages for SMTP.
package mailmsg

import (
	"bytes"
	"errors"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"example.com/email-processor/internal/email"
)

// Build renders e as an RFC 5322 / MIME message with a multipart/alternative
// body holding whichever of the text and html parts are present.
// Bcc is left out of the headers on purpose: those recipients only belong in
// the SMTP envelope.
func Build(e email.Transactional) ([]byte, error) {
	text := strings.TrimSpace(e.Text) != ""
	html := strings.TrimSpace(e.HTML) != ""
	if !text && !html {
		return nil, errors.New("html or text body is required")
	}

	var b bytes.Buffer
	mw := multipart.NewWriter(&b) // writes nothing until the first part

	headers := []string{
		"From: " + formatAddress(e.From),
		"To: " + formatAddress(e.To),
	}
	if e.CC.Address != "" {
		headers = append(headers, "Cc: "+formatAddress(e.CC))
	}
	headers = append(headers,
		"Subject: "+mime.QEncoding.Encode("utf-8", e.Subject),
		"Date: "+time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		`Content-Type: multipart/alternative; boundary="`+mw.Boundary()+`"`,
	)
	for _, h := range headers {
		b.WriteString(h + "\r\n")
	}
	b.WriteString("\r\n")

	if text {
		if err := writePart(mw, "text/plain; charset=utf-8", e.Text); err != nil {
			return nil, err
		}
	}
	if html {
		if err := writePart(mw, "text/html; charset=utf-8", e.HTML); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writePart(mw *multipart.Writer, contentType, body string) error {
	p, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {contentType},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return err
	}
	w := quotedprintable.NewWriter(p)
	if _, err := w.Write([]byte(body)); err != nil {
		return err
	}
	return w.Close()
}

// formatAddress encodes the display name (RFC 2047) and quotes it when needed.
func formatAddress(a email.Address) string {
	return (&mail.Address{Name: a.Name, Address: a.Address}).String()
}
