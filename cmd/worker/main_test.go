package main

import (
	"context"
	"errors"
	"fmt"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"example.com/email-processor/internal/email"
)

const validPayload = `{"from":{"name":"Alice","address":"a@example.com"},"to":{"name":"Bob","address":"b@example.com"},` +
	`"cc":{"name":"","address":"c@example.com"},"bcc":{"name":"","address":"d@example.com"},"subject":"hi","text":"hi"}`

func TestDecode(t *testing.T) {
	e, err := decode([]byte(validPayload), nil)
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
		if _, err := decode([]byte(payload), nil); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

func TestDeliver(t *testing.T) {
	defer func(base, max time.Duration) { retryBase, retryMax = base, max }(retryBase, retryMax)
	retryBase, retryMax = time.Millisecond, 5*time.Millisecond

	e, err := decode([]byte(validPayload), nil)
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

func TestPermanent(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&textproto.Error{Code: 550, Msg: "no such user"}, true},
		{fmt.Errorf("send: %w", &textproto.Error{Code: 554, Msg: "rejected"}), true},
		{&textproto.Error{Code: 451, Msg: "try later"}, false},
		{&textproto.Error{Code: 421, Msg: "throttled"}, false},
		{errors.New("dial tcp: connection refused"), false},
	}
	for _, tc := range cases {
		if got := permanent(tc.err); got != tc.want {
			t.Errorf("permanent(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestDeliverDropsPermanent(t *testing.T) {
	e, err := decode([]byte(validPayload), nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	attempts := 0
	deliver(context.Background(), e, func(string, []string, []byte) error {
		attempts++
		return &textproto.Error{Code: 550, Msg: "no such user"}
	})
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (5xx is not retried)", attempts)
	}
}

func TestDeliverRetriesTransient(t *testing.T) {
	defer func(base, max time.Duration) { retryBase, retryMax = base, max }(retryBase, retryMax)
	retryBase, retryMax = time.Millisecond, 5*time.Millisecond

	e, err := decode([]byte(validPayload), nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	attempts := 0
	deliver(context.Background(), e, func(string, []string, []byte) error {
		attempts++
		if attempts < 3 {
			return &textproto.Error{Code: 451, Msg: "try later"}
		}
		return nil
	})
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestDecodeRejectsDisallowedSender(t *testing.T) {
	if _, err := decode([]byte(validPayload), []string{"other.com"}); err == nil {
		t.Fatal("want error for sender outside the allowlist")
	}
	if _, err := decode([]byte(validPayload), []string{"example.com"}); err != nil {
		t.Fatalf("allowed sender rejected: %v", err)
	}
}
