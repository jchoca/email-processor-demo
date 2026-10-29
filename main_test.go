package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
}

func TestSendTransactionalValidation(t *testing.T) {
	from := addr("", "a@example.com")
	to := addr("", "b@example.com")
	bad := map[string]string{
		"missing from": `{"to":` + to + `,"text":"hi"}`,
		"missing to":   `{"from":` + from + `,"text":"hi"}`,
		"blank from":   `{"from":` + addr("Alice", "  ") + `,"to":` + to + `,"text":"hi"}`,
		"bad from":     `{"from":` + addr("Alice", "nope") + `,"to":` + to + `,"text":"hi"}`,
		"bad to":       `{"from":` + from + `,"to":` + addr("", "nope") + `,"text":"hi"}`,
		"bad cc":       `{"from":` + from + `,"to":` + to + `,"cc":` + addr("", "nope") + `,"text":"hi"}`,
		"bad bcc":      `{"from":` + from + `,"to":` + to + `,"bcc":` + addr("", "nope") + `,"text":"hi"}`,
		"name in addr": `{"from":` + addr("", "Alice <a@example.com>") + `,"to":` + to + `,"text":"hi"}`,
		"long name":    `{"from":` + addr(strings.Repeat("x", 255), "a@example.com") + `,"to":` + to + `,"text":"hi"}`,
		"long address": `{"from":` + addr("", strings.Repeat("a", 243)+"@example.com") + `,"to":` + to + `,"text":"hi"}`,
		"long cc name": `{"from":` + from + `,"to":` + to + `,"cc":` + addr(strings.Repeat("x", 255), "") + `,"text":"hi"}`,
		"no body":      `{"from":` + from + `,"to":` + to + `}`,
		"blank body":   `{"from":` + from + `,"to":` + to + `,"html":" ","text":" "}`,
		"long subject": `{"from":` + from + `,"to":` + to + `,"text":"hi","subject":"` + strings.Repeat("x", 257) + `"}`,
	}
	for name, body := range bad {
		rec := httptest.NewRecorder()
		routes(noProduce).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, rec.Code)
		}
	}

	// cc/bcc optional, text-only body is fine, display name + 256-char subject ok
	rec := httptest.NewRecorder()
	body := `{"from":` + addr("Séndér", "a@example.com") + `,"to":` + to +
		`,"text":"hi","subject":"` + strings.Repeat("é", 256) + `"}`
	routes(noProduce).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}

	// exactly 254 characters must still pass
	rec = httptest.NewRecorder()
	body = `{"from":` + addr(strings.Repeat("é", 254), strings.Repeat("a", 242)+"@example.com") +
		`,"to":` + to + `,"text":"hi"}`
	routes(noProduce).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("boundary: got %d, want 202", rec.Code)
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
	var got transactionalEmail
	if err := json.Unmarshal(produced[0], &got); err != nil {
		t.Fatalf("produced payload not JSON: %v", err)
	}
	if got.From != (address{"Alice", "a@example.com"}) || got.Subject != "hi" {
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
