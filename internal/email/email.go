// Package email holds the transactional email payload and its validation.
package email

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"
)

const (
	maxAddressLen = 254
	maxSubjectLen = 256
)

// Address is one mail address with an optional display name.
type Address struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// Transactional is one request to send a transactional email.
type Transactional struct {
	From    Address `json:"from"`
	To      Address `json:"to"`
	CC      Address `json:"cc"`
	BCC     Address `json:"bcc"`
	Subject string  `json:"subject"`
	HTML    string  `json:"html"`
	Text    string  `json:"text"`
}

// Validate checks required fields, address syntax and length limits.
func (e Transactional) Validate() error {
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
	return nil
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
