package email

import (
	"strings"
	"testing"
	"time"
)

func valid() Transactional {
	return Transactional{
		AccountID: "0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10",
		From:      Address{Name: "Alice", Address: "a@example.com"},
		To:        Address{Name: "Bob", Address: "b@example.com"},
		Subject:   "hi",
		Text:      "hi",
	}
}

func TestStamp(t *testing.T) {
	message := valid()
	message.NewMessage()
	messageID, eventID := message.MessageID, message.EventID
	if len(messageID) != 36 || messageID == eventID {
		t.Fatalf("messageId = %q, eventId = %q, want distinct UUIDs", messageID, eventID)
	}
	if eventID[14] != '4' || !strings.ContainsRune("89ab", rune(eventID[19])) {
		t.Fatalf("%q is not a UUIDv4", eventID)
	}
	if message.Timestamp.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatalf("timestamp = %v, want millisecond precision", message.Timestamp)
	}

	// the next hop mints a new event but keeps the message
	message.Stamp()
	if message.MessageID != messageID {
		t.Errorf("messageId = %q, want %q kept across events", message.MessageID, messageID)
	}
	if len(message.EventID) != 36 || message.EventID == eventID || message.EventID == messageID {
		t.Errorf("eventId = %q, want a new UUID", message.EventID)
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
		{"missing accountId", func(message *Transactional) { message.AccountID = "" }, "accountId must be a UUID"},
		{"bad accountId", func(message *Transactional) { message.AccountID = "acct-123" }, "accountId must be a UUID"},
		{"accountId without dashes", func(message *Transactional) {
			message.AccountID = "0b6f1c1e6f434c9e9a8e2f1d3b7a5c10xxxx"
		}, "accountId must be a UUID"},
		{"missing from", func(message *Transactional) { message.From = Address{} }, "from.address is required"},
		{"missing to", func(message *Transactional) { message.To = Address{} }, "to.address is required"},
		{"blank from", func(message *Transactional) { message.From.Address = "  " }, "from.address is required"},
		{"bad from", func(message *Transactional) { message.From.Address = "nope" }, "from.address is not a valid"},
		{"bad to", func(message *Transactional) { message.To.Address = "nope" }, "to.address is not a valid"},
		{"display name in address", func(message *Transactional) {
			message.From.Address = "Alice <a@example.com>"
		}, "from.address is not a valid"},
		{"long name", func(message *Transactional) { message.From.Name = strings.Repeat("x", 255) }, "from.name must be 254"},
		{"long address", func(message *Transactional) {
			message.From.Address = strings.Repeat("a", 243) + "@example.com"
		}, "from.address must be 254"},
		{"no body", func(message *Transactional) { message.HTML, message.Text = "", "" }, "html or text body is required"},
		{"blank body", func(message *Transactional) { message.HTML, message.Text = " ", " " }, "html or text body is required"},
		{"long subject", func(message *Transactional) { message.Subject = strings.Repeat("é", 257) }, "subject must be 256"},
		{"too many tags", func(message *Transactional) { message.Tags = []string{"a", "b", "c", "d", "e", "f"} }, "tags must be 5"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			message := valid()
			testCase.mutate(&message)
			err := message.Validate()
			if err == nil {
				t.Fatalf("want error containing %q, got nil", testCase.want)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("got %q, want it to contain %q", err, testCase.want)
			}
		})
	}
}

func TestValidateBoundaries(t *testing.T) {
	message := valid()
	message.From.Name = strings.Repeat("é", 254)
	message.From.Address = strings.Repeat("a", 242) + "@example.com" // 254 runes
	message.Subject = strings.Repeat("é", 256)
	message.Tags = []string{"a", "b", "c", "d", "e"}
	if err := message.Validate(); err != nil {
		t.Fatalf("254/256 lengths and 5 tags should pass: %v", err)
	}
}

func TestSenderAllowed(t *testing.T) {
	message := valid() // a@example.com
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
	for _, testCase := range cases {
		if got := message.SenderAllowed(testCase.allowed); got != testCase.want {
			t.Errorf("SenderAllowed(%v) = %v, want %v", testCase.allowed, got, testCase.want)
		}
	}

	message.From.Address = "a@mail.example.com"
	if !message.SenderAllowed([]string{"example.com"}) {
		t.Error("subdomain of allowed domain should be allowed")
	}
	if message.SenderAllowed([]string{"example.com.evil.com"}) {
		t.Error("lookalike domain should not be allowed")
	}
}
