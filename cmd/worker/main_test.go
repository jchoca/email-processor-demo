package main

import (
	"errors"
	"strings"
	"testing"
)

const validPayload = `{"from":{"name":"Alice","address":"a@example.com"},"to":{"name":"Bob","address":"b@example.com"},` +
	`"cc":{"name":"","address":"c@example.com"},"bcc":{"name":"","address":"d@example.com"},"subject":"hi","text":"hi"}`

func TestHandle(t *testing.T) {
	var gotFrom string
	var gotTo []string
	var gotMsg []byte
	send := func(from string, to []string, msg []byte) error {
		gotFrom, gotTo, gotMsg = from, to, msg
		return nil
	}

	if err := handle([]byte(validPayload), send); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if gotFrom != "a@example.com" {
		t.Errorf("from = %q", gotFrom)
	}
	if len(gotTo) != 3 || gotTo[0] != "b@example.com" || gotTo[1] != "c@example.com" || gotTo[2] != "d@example.com" {
		t.Errorf("envelope recipients = %v, want to+cc+bcc", gotTo)
	}
	if !strings.Contains(string(gotMsg), "Subject: hi") || !strings.Contains(string(gotMsg), "<a@example.com>") {
		t.Errorf("message missing headers:\n%s", gotMsg)
	}
	if strings.Contains(string(gotMsg), "d@example.com") {
		t.Error("bcc recipient leaked into message headers")
	}
}

func TestHandleErrors(t *testing.T) {
	send := func(string, []string, []byte) error { return nil }

	tests := map[string]string{
		"bad json":      `nope`,
		"invalid email": `{"from":{"address":"nope"},"to":{"address":"b@example.com"},"text":"hi"}`,
		"missing body":  `{"from":{"address":"a@example.com"},"to":{"address":"b@example.com"}}`,
		"missing from":  `{"to":{"address":"b@example.com"},"text":"hi"}`,
	}
	for name, payload := range tests {
		if err := handle([]byte(payload), send); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}

	if err := handle([]byte(validPayload), func(string, []string, []byte) error {
		return errors.New("smtp down")
	}); err == nil {
		t.Error("smtp failure: want error, got nil")
	}
}
