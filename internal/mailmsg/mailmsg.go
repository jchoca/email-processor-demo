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

// Build renders message as an RFC 5322 / MIME message with a
// multipart/alternative body holding whichever of the text and html parts are
// present.
func Build(message email.Transactional) ([]byte, error) {
	text := strings.TrimSpace(message.Text) != ""
	html := strings.TrimSpace(message.HTML) != ""
	if !text && !html {
		return nil, errors.New("html or text body is required")
	}

	var buffer bytes.Buffer
	multipartWriter := multipart.NewWriter(&buffer) // writes nothing until the first part

	headers := []string{
		"From: " + formatAddress(message.From),
		"To: " + formatAddress(message.To),
		"Subject: " + mime.QEncoding.Encode("utf-8", message.Subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		`Content-Type: multipart/alternative; boundary="` + multipartWriter.Boundary() + `"`,
	}
	for _, header := range headers {
		buffer.WriteString(header + "\r\n")
	}
	buffer.WriteString("\r\n")

	if text {
		if err := writePart(multipartWriter, "text/plain; charset=utf-8", message.Text); err != nil {
			return nil, err
		}
	}
	if html {
		if err := writePart(multipartWriter, "text/html; charset=utf-8", message.HTML); err != nil {
			return nil, err
		}
	}
	if err := multipartWriter.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writePart(multipartWriter *multipart.Writer, contentType, body string) error {
	part, err := multipartWriter.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {contentType},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return err
	}
	encoder := quotedprintable.NewWriter(part)
	if _, err := encoder.Write([]byte(body)); err != nil {
		return err
	}
	return encoder.Close()
}

// formatAddress encodes the display name (RFC 2047) and quotes it when needed.
func formatAddress(address email.Address) string {
	return (&mail.Address{Name: address.Name, Address: address.Address}).String()
}
