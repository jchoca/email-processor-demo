package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	routes().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestSendTransactional(t *testing.T) {
	body := `{"from":"a@example.com","to":"b@example.com","cc":"","bcc":"","subject":"hi","html":"<p>hi</p>","text":"hi"}`
	rec := httptest.NewRecorder()
	routes().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}

	rec = httptest.NewRecorder()
	routes().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader("not json")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestSendTransactionalValidation(t *testing.T) {
	bad := map[string]string{
		"missing from": `{"to":"b@example.com","text":"hi"}`,
		"missing to":   `{"from":"a@example.com","text":"hi"}`,
		"blank from":   `{"from":"  ","to":"b@example.com","text":"hi"}`,
		"no body":      `{"from":"a@example.com","to":"b@example.com"}`,
		"blank body":   `{"from":"a@example.com","to":"b@example.com","html":" ","text":" "}`,
		"bad from":     `{"from":"nope","to":"b@example.com","text":"hi"}`,
		"bad to":       `{"from":"a@example.com","to":"nope","text":"hi"}`,
		"bad cc":       `{"from":"a@example.com","to":"b@example.com","cc":"nope","text":"hi"}`,
		"bad bcc":      `{"from":"a@example.com","to":"b@example.com","bcc":"nope","text":"hi"}`,
		"long subject": `{"from":"a@example.com","to":"b@example.com","text":"hi","subject":"` + strings.Repeat("x", 51) + `"}`,
	}
	for name, body := range bad {
		rec := httptest.NewRecorder()
		routes().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, rec.Code)
		}
	}

	// cc/bcc optional, text-only body is fine, display name + 50-char subject ok
	rec := httptest.NewRecorder()
	body := `{"from":"Sender <a@example.com>","to":"b@example.com","text":"hi","subject":"` + strings.Repeat("é", 50) + `"}`
	routes().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}
}
