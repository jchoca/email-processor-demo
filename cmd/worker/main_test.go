package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"example.com/email-processor/internal/email"
)

const validPayload = `{"from":{"name":"Alice","address":"a@example.com"},"to":{"name":"Bob","address":"b@example.com"},` +
	`"cc":{"name":"","address":"c@example.com"},"bcc":{"name":"","address":"d@example.com"},"subject":"hi","text":"hi"}`

func TestDecode(t *testing.T) {
	e, err := decode([]byte(validPayload))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.From.Address != "a@example.com" || e.Subject != "hi" {
		t.Errorf("unexpected payload: %+v", e)
	}

	tests := map[string]string{
		"bad json":      `nope`,
		"invalid email": `{"from":{"address":"nope"},"to":{"address":"b@example.com"},"text":"hi"}`,
		"missing body":  `{"from":{"address":"a@example.com"},"to":{"address":"b@example.com"}}`,
		"missing from":  `{"to":{"address":"b@example.com"},"text":"hi"}`,
	}
	for name, payload := range tests {
		if _, err := decode([]byte(payload)); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

func TestDeliver(t *testing.T) {
	defer func(d time.Duration) { retryDelay = d }(retryDelay)
	retryDelay = time.Millisecond

	e, err := decode([]byte(validPayload))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var gotFrom string
	var gotTo []string
	var gotMsg []byte
	attempts := 0
	send := func(from string, to []string, msg []byte) error {
		attempts++
		gotFrom, gotTo, gotMsg = from, to, msg
		if attempts < 3 {
			return errors.New("smtp down")
		}
		return nil
	}

	deliver(context.Background(), e, send)
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (retry until success)", attempts)
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

	// a failing send keeps retrying until the context is cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	attempts = 0
	deliver(ctx, e, func(string, []string, []byte) error {
		attempts++
		return errors.New("smtp down")
	})
	if attempts < 2 {
		t.Fatalf("attempts = %d, want repeated retries", attempts)
	}
}

func TestDeliverSkipsUnbuildable(t *testing.T) {
	called := false
	deliver(context.Background(), email.Transactional{}, func(string, []string, []byte) error {
		called = true
		return nil
	})
	if called {
		t.Error("send called for message with no body")
	}
}
