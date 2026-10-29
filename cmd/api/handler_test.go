package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/email-processor/internal/email"
)

var noProduce = func(context.Context, []byte) error { return nil }

// addr builds the JSON object shape for from/to/cc/bcc.
func addr(name, address string) string {
	return `{"name":"` + name + `","address":"` + address + `"}`
}

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	routes(noProduce).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestSendTransactional(t *testing.T) {
	body := `{"from":` + addr("Alice", "a@example.com") + `,"to":` + addr("Bob", "b@example.com") +
		`,"cc":` + addr("", "") + `,"bcc":` + addr("", "") + `,"subject":"hi","html":"<p>hi</p>","text":"hi"}`
	rec := httptest.NewRecorder()
	routes(noProduce).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}

	rec = httptest.NewRecorder()
	routes(noProduce).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader("not json")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}

	// old flat-string shape is no longer valid
	rec = httptest.NewRecorder()
	routes(noProduce).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional",
		strings.NewReader(`{"from":"a@example.com","to":"b@example.com","text":"hi"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}

	// validation errors surface as 400
	rec = httptest.NewRecorder()
	routes(noProduce).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional",
		strings.NewReader(`{"from":`+addr("", "nope")+`,"to":`+addr("", "b@example.com")+`,"text":"hi"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestSendTransactionalBodyTooLarge(t *testing.T) {
	body := `{"from":{"address":"a@example.com"},"to":{"address":"b@example.com"},"text":"` +
		strings.Repeat("a", maxBodyBytes) + `"}`
	rec := httptest.NewRecorder()
	routes(noProduce).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413", rec.Code)
	}
}

func TestSendTransactionalProduces(t *testing.T) {
	var produced [][]byte
	var produceErr error
	h := routes(func(_ context.Context, value []byte) error {
		produced = append(produced, value)
		return produceErr
	})
	body := `{"from":` + addr("Alice", "a@example.com") + `,"to":` + addr("", "b@example.com") + `,"subject":"hi","text":"hi"}`

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}
	if len(produced) != 1 {
		t.Fatalf("got %d produced messages, want 1", len(produced))
	}
	var got email.Transactional
	if err := json.Unmarshal(produced[0], &got); err != nil {
		t.Fatalf("produced payload not JSON: %v", err)
	}
	if got.From != (email.Address{Name: "Alice", Address: "a@example.com"}) || got.Subject != "hi" {
		t.Fatalf("unexpected payload: %+v", got)
	}

	// invalid payload must not be produced
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(`{"from":`+addr("", "nope")+`,"to":`+addr("", "b@example.com")+`,"text":"hi"}`)))
	if rec.Code != http.StatusBadRequest || len(produced) != 1 {
		t.Fatalf("invalid: got %d produced, code %d", len(produced), rec.Code)
	}

	// broker failure surfaces as 500, not 202
	produceErr = errors.New("broker down")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rec.Code)
	}
}
