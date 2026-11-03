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

func TestSendTransactionalStampsEvent(t *testing.T) {
	var produced [][]byte
	h := routes(func(_ context.Context, value []byte) error {
		produced = append(produced, value)
		return nil
	}, nil)
	// ids and times sent by the client must not survive
	body := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","messageId":"client","eventId":"client","timestamp":"1999-01-01T00:00:00Z","from":` + addr("", "a@example.com") +
		`,"to":` + addr("", "b@example.com") + `,"text":"hi"}`

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted || len(produced) != 1 {
		t.Fatalf("got %d, produced %d, want 202 and 1", rec.Code, len(produced))
	}
	var got email.Transactional
	if err := json.Unmarshal(produced[0], &got); err != nil {
		t.Fatalf("produced payload not JSON: %v", err)
	}
	if got.EventID == "" || got.EventID == "client" {
		t.Errorf("eventId = %q, want a generated id", got.EventID)
	}
	if got.MessageID == "" || got.MessageID == "client" {
		t.Errorf("messageId = %q, want a generated id", got.MessageID)
	}
	if got.AccountID != "0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10" {
		t.Errorf("accountId = %q, want the client's value kept", got.AccountID)
	}
	if got.Timestamp.Year() == 1999 || got.Timestamp.IsZero() {
		t.Errorf("timestamp = %v, want the current time", got.Timestamp)
	}
}

// addr builds the JSON object shape for from/to.
func addr(name, address string) string {
	return `{"name":"` + name + `","address":"` + address + `"}`
}

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	routes(noProduce, nil).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestSendTransactional(t *testing.T) {
	body := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":` + addr("Alice", "a@example.com") + `,"to":` + addr("Bob", "b@example.com") +
		`,"subject":"hi","html":"<p>hi</p>","text":"hi"}`
	rec := httptest.NewRecorder()
	routes(noProduce, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}

	rec = httptest.NewRecorder()
	routes(noProduce, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader("not json")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}

	// old flat-string shape is no longer valid
	rec = httptest.NewRecorder()
	routes(noProduce, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional",
		strings.NewReader(`{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":"a@example.com","to":"b@example.com","text":"hi"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}

	// validation errors surface as 400
	rec = httptest.NewRecorder()
	routes(noProduce, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional",
		strings.NewReader(`{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":`+addr("", "nope")+`,"to":`+addr("", "b@example.com")+`,"text":"hi"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestSendTransactionalBodyTooLarge(t *testing.T) {
	body := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"address":"a@example.com"},"to":{"address":"b@example.com"},"text":"` +
		strings.Repeat("a", maxBodyBytes) + `"}`
	rec := httptest.NewRecorder()
	routes(noProduce, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
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
	}, nil)
	body := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":` + addr("Alice", "a@example.com") + `,"to":` + addr("", "b@example.com") + `,"subject":"hi","text":"hi"}`

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
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(`{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":`+addr("", "nope")+`,"to":`+addr("", "b@example.com")+`,"text":"hi"}`)))
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

func TestSendTransactionalSenderDomains(t *testing.T) {
	body := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":` + addr("Alice", "a@example.com") + `,"to":` + addr("", "b@example.com") + `,"text":"hi"}`

	rec := httptest.NewRecorder()
	routes(noProduce, []string{"example.com"}).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("allowed domain: got %d, want 202", rec.Code)
	}

	rec = httptest.NewRecorder()
	routes(noProduce, []string{"other.com"}).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disallowed domain: got %d, want 400", rec.Code)
	}

	// a verified domain also authorises its subdomains
	sub := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":` + addr("", "a@mail.example.com") + `,"to":` + addr("", "b@example.com") + `,"text":"hi"}`
	rec = httptest.NewRecorder()
	routes(noProduce, []string{"example.com"}).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(sub)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("subdomain: got %d, want 202", rec.Code)
	}
}
