package email

import (
	"strings"
	"testing"
)

func valid() Transactional {
	return Transactional{
		From:    Address{Name: "Alice", Address: "a@example.com"},
		To:      Address{Name: "Bob", Address: "b@example.com"},
		Subject: "hi",
		Text:    "hi",
	}
}

func TestStamp(t *testing.T) {
	e := valid()
	e.NewMessage()
	messageID, eventID := e.MessageID, e.EventID
	if len(messageID) != 36 || messageID == eventID {
		t.Fatalf("messageId = %q, eventId = %q, want distinct UUIDs", messageID, eventID)
	}
	if eventID[14] != '4' || !strings.ContainsRune("89ab", rune(eventID[19])) {
		t.Fatalf("%q is not a UUIDv4", eventID)
	}
	if e.Timestamp.Nanosecond() != 0 {
		t.Fatalf("timestamp = %v, want second precision", e.Timestamp)
	}

	// the next hop mints a new event but keeps the message
	e.Stamp()
	if e.MessageID != messageID {
		t.Errorf("messageId = %q, want %q kept across events", e.MessageID, messageID)
	}
	if len(e.EventID) != 36 || e.EventID == eventID || e.EventID == messageID {
		t.Errorf("eventId = %q, want a new UUID", e.EventID)
	}
}

func TestValidate(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Transactional)
		want   string
	}{
		{"missing from", func(e *Transactional) { e.From = Address{} }, "from.address is required"},
		{"missing to", func(e *Transactional) { e.To = Address{} }, "to.address is required"},
		{"blank from", func(e *Transactional) { e.From.Address = "  " }, "from.address is required"},
		{"bad from", func(e *Transactional) { e.From.Address = "nope" }, "from.address is not a valid"},
		{"bad to", func(e *Transactional) { e.To.Address = "nope" }, "to.address is not a valid"},
		{"bad cc", func(e *Transactional) { e.CC.Address = "nope" }, "cc.address is not a valid"},
		{"bad bcc", func(e *Transactional) { e.BCC.Address = "nope" }, "bcc.address is not a valid"},
		{"display name in address", func(e *Transactional) {
			e.From.Address = "Alice <a@example.com>"
		}, "from.address is not a valid"},
		{"long name", func(e *Transactional) { e.From.Name = strings.Repeat("x", 255) }, "from.name must be 254"},
		{"long address", func(e *Transactional) {
			e.From.Address = strings.Repeat("a", 243) + "@example.com"
		}, "from.address must be 254"},
		{"long cc name without address", func(e *Transactional) {
			e.CC = Address{Name: strings.Repeat("x", 255)}
		}, "cc.name must be 254"},
		{"no body", func(e *Transactional) { e.HTML, e.Text = "", "" }, "html or text body is required"},
		{"blank body", func(e *Transactional) { e.HTML, e.Text = " ", " " }, "html or text body is required"},
		{"long subject", func(e *Transactional) { e.Subject = strings.Repeat("é", 257) }, "subject must be 256"},
		{"too many tags", func(e *Transactional) { e.Tags = []string{"a", "b", "c", "d", "e", "f"} }, "tags must be 5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := valid()
			tc.mutate(&e)
			err := e.Validate()
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestValidateBoundaries(t *testing.T) {
	e := valid()
	e.From.Name = strings.Repeat("é", 254)
	e.From.Address = strings.Repeat("a", 242) + "@example.com" // 254 runes
	e.Subject = strings.Repeat("é", 256)
	e.Tags = []string{"a", "b", "c", "d", "e"}
	if err := e.Validate(); err != nil {
		t.Fatalf("254/256 lengths and 5 tags should pass: %v", err)
	}
}

func TestSenderAllowed(t *testing.T) {
	e := valid() // a@example.com
	cases := []struct {
		allowed []string
		want    bool
	}{
		{nil, true},
		{[]string{"example.com"}, true},
		{[]string{"EXAMPLE.COM"}, true},
		{[]string{"x.com", "example.com"}, true},
		{[]string{"x.com"}, false},
		{[]string{"ample.com"}, false},
	}
	for _, tc := range cases {
		if got := e.SenderAllowed(tc.allowed); got != tc.want {
			t.Errorf("SenderAllowed(%v) = %v, want %v", tc.allowed, got, tc.want)
		}
	}

	e.From.Address = "a@mail.example.com"
	if !e.SenderAllowed([]string{"example.com"}) {
		t.Error("subdomain of allowed domain should be allowed")
	}
	if e.SenderAllowed([]string{"example.com.evil.com"}) {
		t.Error("lookalike domain should not be allowed")
	}
}
