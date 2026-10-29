package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// addr builds the JSON object shape for from/to/cc/bcc.
func addr(name, address string) string {
	return `{"name":"` + name + `","address":"` + address + `"}`
}

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	routes().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestSendTransactional(t *testing.T) {
	body := `{"from":` + addr("Alice", "a@example.com") + `,"to":` + addr("Bob", "b@example.com") +
		`,"cc":` + addr("", "") + `,"bcc":` + addr("", "") + `,"subject":"hi","html":"<p>hi</p>","text":"hi"}`
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

	// old flat-string shape is no longer valid
	rec = httptest.NewRecorder()
	routes().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional",
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
		routes().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, rec.Code)
		}
	}

	// cc/bcc optional, text-only body is fine, display name + 256-char subject ok
	rec := httptest.NewRecorder()
	body := `{"from":` + addr("Séndér", "a@example.com") + `,"to":` + to +
		`,"text":"hi","subject":"` + strings.Repeat("é", 256) + `"}`
	routes().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}

	// exactly 254 characters must still pass
	rec = httptest.NewRecorder()
	body = `{"from":` + addr(strings.Repeat("é", 254), strings.Repeat("a", 242)+"@example.com") +
		`,"to":` + to + `,"text":"hi"}`
	routes().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("boundary: got %d, want 202", rec.Code)
	}
}
