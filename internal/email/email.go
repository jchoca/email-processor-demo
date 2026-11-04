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
	Subject   string    `json:"subject"`
	HTML      string    `json:"html"`
	Text      string    `json:"text"`
	Tags      []string  `json:"tags"`
}

// NewMessage starts the event chain for one request: the messageId is shared by
// every event of that mail, while eventId and timestamp are per event.
func (message *Transactional) NewMessage() {
	message.MessageID = newEventID()
	message.Stamp()
}

// Stamp gives the event a fresh id and a millisecond-precision UTC timestamp.
// Each hop (accepted, processed) stamps its own event under the same messageId.
func (message *Transactional) Stamp() {
	message.EventID = newEventID()
	message.Timestamp = time.Now().UTC().Truncate(time.Millisecond)
}

// newEventID returns a random UUIDv4.
func newEventID() string {
	var random [16]byte
	rand.Read(random[:])              // never fails on supported platforms
	random[6] = random[6]&0x0f | 0x40 // version 4
	random[8] = random[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", random[0:4], random[4:6], random[6:8], random[8:10], random[10:16])
}

// Validate checks required fields, address syntax and length limits.
func (message Transactional) Validate() error {
	if !IsUUID(message.AccountID) {
		return errors.New("accountId must be a UUID")
	}
	for _, field := range []struct {
		name  string
		value Address
	}{
		{"from", message.From},
		{"to", message.To},
	} {
		addr := strings.TrimSpace(field.value.Address)
		if utf8.RuneCountInString(field.value.Name) > maxAddressLen {
			return fmt.Errorf("%s.name must be %d characters or fewer", field.name, maxAddressLen)
		}
		if utf8.RuneCountInString(addr) > maxAddressLen {
			return fmt.Errorf("%s.address must be %d characters or fewer", field.name, maxAddressLen)
		}
		if addr == "" {
			return fmt.Errorf("%s.address is required", field.name)
		}
		// reject display names here; they belong in .name
		parsed, err := mail.ParseAddress(addr)
		if err != nil || parsed.Address != addr {
			return fmt.Errorf("%s.address is not a valid email address", field.name)
		}
	}
	if strings.TrimSpace(message.HTML) == "" && strings.TrimSpace(message.Text) == "" {
		return errors.New("html or text body is required")
	}
	if utf8.RuneCountInString(message.Subject) > maxSubjectLen {
		return fmt.Errorf("subject must be %d characters or fewer", maxSubjectLen)
	}
	if len(message.Tags) > maxTags {
		return fmt.Errorf("tags must be %d or fewer", maxTags)
	}
	return nil
}

// IsUUID reports whether candidate is a hex UUID in the 8-4-4-4-12 form, any
// version.
func IsUUID(candidate string) bool {
	if len(candidate) != 36 {
		return false
	}
	for index, char := range candidate {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return false
		}
	}
	return true
}

// SenderAllowed reports whether the from domain is one of allowed (a domain
// also authorises its subdomains). An empty allowlist accepts any sender.
func (message Transactional) SenderAllowed(allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	host := strings.ToLower(message.From.Address[strings.LastIndex(message.From.Address, "@")+1:])
	for _, domain := range allowed {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain != "" && (host == domain || strings.HasSuffix(host, "."+domain)) {
			return true
		}
	}
	return false
}
