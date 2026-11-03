package mailmsg

import (
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"

	"example.com/email-processor/internal/email"
)

func mustParseAddress(t *testing.T, s string) *mail.Address {
	t.Helper()
	a, err := mail.ParseAddress(s)
	if err != nil {
		t.Fatalf("bad address header %q: %v", s, err)
	}
	return a
}

func TestBuild(t *testing.T) {
	e := email.Transactional{
		From:    email.Address{Name: "Alice", Address: "a@example.com"},
		To:      email.Address{Name: "Bob", Address: "b@example.com"},
		Subject: "héllo",
		HTML:    "<p>hi</p>",
		Text:    "hi\n",
	}
	raw, err := Build(e)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("not a valid message: %v\n%s", err, raw)
	}
	if from := mustParseAddress(t, msg.Header.Get("From")); from.Name != "Alice" || from.Address != "a@example.com" {
		t.Errorf("From = %+v", from)
	}
	if got, want := msg.Header.Get("Subject"), "=?utf-8?q?h=C3=A9llo?="; got != want {
		t.Errorf("Subject = %q, want %q", got, want)
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type = %q, want multipart/alternative", msg.Header.Get("Content-Type"))
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])

	want := map[string]string{
		"text/plain; charset=utf-8": "hi\r\n",
		"text/html; charset=utf-8":  "<p>hi</p>",
	}
	for range want {
		p, err := mr.NextPart()
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		body, err := io.ReadAll(quotedprintable.NewReader(p))
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		ct := p.Header.Get("Content-Type")
		if string(body) != want[ct] {
			t.Errorf("%s body = %q, want %q", ct, body, want[ct])
		}
		delete(want, ct)
	}
	if len(want) != 0 {
		t.Errorf("missing parts: %v", want)
	}

	// text-only still renders a valid, readable message
	raw, err = Build(email.Transactional{
		From: email.Address{Address: "a@example.com"},
		To:   email.Address{Address: "b@example.com"},
		Text: "plain only",
	})
	if err != nil {
		t.Fatalf("build text-only: %v", err)
	}
	msg, err = mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("not a valid message: %v", err)
	}
	_, params, _ = mime.ParseMediaType(msg.Header.Get("Content-Type"))
	p, err := multipart.NewReader(msg.Body, params["boundary"]).NextPart()
	if err != nil {
		t.Fatalf("next part: %v", err)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(p))
	if string(body) != "plain only" {
		t.Errorf("body = %q", body)
	}

	// no body at all is an error
	if _, err := Build(email.Transactional{From: email.Address{Address: "a@example.com"}}); err == nil {
		t.Error("want error for empty body")
	}
}
