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

const validPayload = `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"name":"Alice","address":"a@example.com"},"to":{"name":"Bob","address":"b@example.com"},` +
	`"subject":"hi","text":"hi"}`

func TestDecode(t *testing.T) {
	message, err := decode([]byte(validPayload), nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if message.From.Address != "a@example.com" || message.Subject != "hi" {
		t.Errorf("unexpected payload: %+v", message)
	}

	tests := map[string]string{
		"bad json":      `nope`,
		"invalid email": `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"address":"nope"},"to":{"address":"b@example.com"},"text":"hi"}`,
		"missing body":  `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"address":"a@example.com"},"to":{"address":"b@example.com"}}`,
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

	message, err := decode([]byte(validPayload), nil)
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

	deliver(context.Background(), message, send)
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (retry until success)", attempts)
	}
	if gotFrom != "a@example.com" {
		t.Errorf("from = %q", gotFrom)
	}
	if len(gotTo) != 1 || gotTo[0] != "b@example.com" {
		t.Errorf("envelope recipients = %v, want [b@example.com]", gotTo)
	}
	if !strings.Contains(string(gotMsg), "Subject: hi") || !strings.Contains(string(gotMsg), "<a@example.com>") {
		t.Errorf("message missing headers:\n%s", gotMsg)
	}

	// a failing send keeps retrying until the context is cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	attempts = 0
	if deliver(ctx, message, func(string, []string, []byte) error {
		attempts++
		return errors.New("smtp down")
	}) {
		t.Error("deliver reported success for an unsent message")
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want repeated retries", attempts)
	}
}

func TestDeliverSkipsUnbuildable(t *testing.T) {
	called := false
	if deliver(context.Background(), email.Transactional{}, func(string, []string, []byte) error {
		called = true
		return nil
	}) {
		t.Error("deliver reported success for an unbuildable message")
	}
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
	for _, testCase := range cases {
		if got := permanent(testCase.err); got != testCase.want {
			t.Errorf("permanent(%v) = %v, want %v", testCase.err, got, testCase.want)
		}
	}
}

func TestDeliverDropsPermanent(t *testing.T) {
	message, err := decode([]byte(validPayload), nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	attempts := 0
	if deliver(context.Background(), message, func(string, []string, []byte) error {
		attempts++
		return &textproto.Error{Code: 550, Msg: "no such user"}
	}) {
		t.Error("deliver reported success for a dropped message")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (5xx is not retried)", attempts)
	}
}

func TestDeliverRetriesTransient(t *testing.T) {
	defer func(base, max time.Duration) { retryBase, retryMax = base, max }(retryBase, retryMax)
	retryBase, retryMax = time.Millisecond, 5*time.Millisecond

	message, err := decode([]byte(validPayload), nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	attempts := 0
	deliver(context.Background(), message, func(string, []string, []byte) error {
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
