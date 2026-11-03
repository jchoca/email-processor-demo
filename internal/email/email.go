// Package email holds the transactional email payload and its validation.
package email

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxAddressLen = 254
	maxSubjectLen = 256
	maxTags       = 5
)

// Address is one mail address with an optional display name.
type Address struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// Transactional is one request to send a transactional email. The client sends
// accountId; messageId, eventId and timestamp are stamped by the API.
type Transactional struct {
	MessageID string    `json:"messageId"`
	EventID   string    `json:"eventId"`
	Timestamp time.Time `json:"timestamp"`
	AccountID string    `json:"accountId"`
	From      Address   `json:"from"`
	To        Address   `json:"to"`
	CC        Address   `json:"cc"`
	BCC       Address   `json:"bcc"`
	Subject   string    `json:"subject"`
	HTML      string    `json:"html"`
	Text      string    `json:"text"`
	Tags      []string  `json:"tags"`
}

// NewMessage starts the event chain for one request: the messageId is shared by
// every event of that mail, while eventId and timestamp are per event.
func (e *Transactional) NewMessage() {
	e.MessageID = newEventID()
	e.Stamp()
}

// Stamp gives the event a fresh id and a second-precision UTC timestamp. Each
// hop (accepted, processed) stamps its own event under the same messageId.
func (e *Transactional) Stamp() {
	e.EventID = newEventID()
	e.Timestamp = time.Now().UTC().Truncate(time.Second)
}

// newEventID returns a random UUIDv4.
func newEventID() string {
	var b [16]byte
	rand.Read(b[:])         // never fails on supported platforms
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Validate checks required fields, address syntax and length limits.
func (e Transactional) Validate() error {
	if !isUUID(e.AccountID) {
		return errors.New("accountId must be a UUID")
	}
	for _, a := range []struct {
		name     string
		value    Address
		required bool
	}{
		{"from", e.From, true},
		{"to", e.To, true},
		{"cc", e.CC, false},
		{"bcc", e.BCC, false},
	} {
		addr := strings.TrimSpace(a.value.Address)
		if utf8.RuneCountInString(a.value.Name) > maxAddressLen {
			return fmt.Errorf("%s.name must be %d characters or fewer", a.name, maxAddressLen)
		}
		if utf8.RuneCountInString(addr) > maxAddressLen {
			return fmt.Errorf("%s.address must be %d characters or fewer", a.name, maxAddressLen)
		}
		if addr == "" {
			if a.required {
				return fmt.Errorf("%s.address is required", a.name)
			}
			continue
		}
		// reject display names here; they belong in .name
		parsed, err := mail.ParseAddress(addr)
		if err != nil || parsed.Address != addr {
			return fmt.Errorf("%s.address is not a valid email address", a.name)
		}
	}
	if strings.TrimSpace(e.HTML) == "" && strings.TrimSpace(e.Text) == "" {
		return errors.New("html or text body is required")
	}
	if utf8.RuneCountInString(e.Subject) > maxSubjectLen {
		return fmt.Errorf("subject must be %d characters or fewer", maxSubjectLen)
	}
	if len(e.Tags) > maxTags {
		return fmt.Errorf("tags must be %d or fewer", maxTags)
	}
	return nil
}

// isUUID reports whether s is a hex UUID in the 8-4-4-4-12 form, any version.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// Recipients returns the SMTP envelope recipients: to, then cc, then bcc.
func (e Transactional) Recipients() []string {
	rcpts := []string{e.To.Address}
	if e.CC.Address != "" {
		rcpts = append(rcpts, e.CC.Address)
	}
	if e.BCC.Address != "" {
		rcpts = append(rcpts, e.BCC.Address)
	}
	return rcpts
}

// SenderAllowed reports whether the from domain is one of allowed (a domain
// also authorises its subdomains). An empty allowlist accepts any sender.
func (e Transactional) SenderAllowed(allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	host := strings.ToLower(e.From.Address[strings.LastIndex(e.From.Address, "@")+1:])
	for _, d := range allowed {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" && (host == d || strings.HasSuffix(host, "."+d)) {
			return true
		}
	}
	return false
}
