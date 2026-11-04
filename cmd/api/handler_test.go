package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/email-processor/internal/activity"
	"example.com/email-processor/internal/email"
	"example.com/email-processor/internal/stats"
)

var noProduce = func(context.Context, []byte) error { return nil }

var noStats = func(context.Context, stats.Filter) ([]stats.Hour, error) { return nil, nil }

var noSearch = func(context.Context, activity.Filter) ([]activity.Event, int, error) { return nil, 0, nil }

func TestSendTransactionalStampsEvent(t *testing.T) {
	var produced [][]byte
	handler := routes(func(_ context.Context, value []byte) error {
		produced = append(produced, value)
		return nil
	}, noStats, noSearch, nil)
	// ids and times sent by the client must not survive
	body := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","messageId":"client","eventId":"client","timestamp":"1999-01-01T00:00:00Z","from":` + addr("", "a@example.com") +
		`,"to":` + addr("", "b@example.com") + `,"text":"hi"}`

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
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
	routes(noProduce, noStats, noSearch, nil).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestSendTransactional(t *testing.T) {
	body := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":` + addr("Alice", "a@example.com") + `,"to":` + addr("Bob", "b@example.com") +
		`,"subject":"hi","html":"<p>hi</p>","text":"hi"}`
	rec := httptest.NewRecorder()
	routes(noProduce, noStats, noSearch, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}

	rec = httptest.NewRecorder()
	routes(noProduce, noStats, noSearch, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader("not json")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}

	// old flat-string shape is no longer valid
	rec = httptest.NewRecorder()
	routes(noProduce, noStats, noSearch, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional",
		strings.NewReader(`{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":"a@example.com","to":"b@example.com","text":"hi"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}

	// validation errors surface as 400
	rec = httptest.NewRecorder()
	routes(noProduce, noStats, noSearch, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional",
		strings.NewReader(`{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":`+addr("", "nope")+`,"to":`+addr("", "b@example.com")+`,"text":"hi"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestSendTransactionalBodyTooLarge(t *testing.T) {
	body := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":{"address":"a@example.com"},"to":{"address":"b@example.com"},"text":"` +
		strings.Repeat("a", maxBodyBytes) + `"}`
	rec := httptest.NewRecorder()
	routes(noProduce, noStats, noSearch, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413", rec.Code)
	}
}

func TestSendTransactionalProduces(t *testing.T) {
	var produced [][]byte
	var produceErr error
	handler := routes(func(_ context.Context, value []byte) error {
		produced = append(produced, value)
		return produceErr
	}, noStats, noSearch, nil)
	body := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":` + addr("Alice", "a@example.com") + `,"to":` + addr("", "b@example.com") + `,"subject":"hi","text":"hi"}`

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
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
	handler.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(`{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":`+addr("", "nope")+`,"to":`+addr("", "b@example.com")+`,"text":"hi"}`)))
	if rec.Code != http.StatusBadRequest || len(produced) != 1 {
		t.Fatalf("invalid: got %d produced, code %d", len(produced), rec.Code)
	}

	// broker failure surfaces as 500, not 202
	produceErr = errors.New("broker down")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rec.Code)
	}
}

func TestSendTransactionalSenderDomains(t *testing.T) {
	body := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":` + addr("Alice", "a@example.com") + `,"to":` + addr("", "b@example.com") + `,"text":"hi"}`

	rec := httptest.NewRecorder()
	routes(noProduce, noStats, noSearch, []string{"example.com"}).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("allowed domain: got %d, want 202", rec.Code)
	}

	rec = httptest.NewRecorder()
	routes(noProduce, noStats, noSearch, []string{"other.com"}).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disallowed domain: got %d, want 400", rec.Code)
	}

	// a verified domain also authorises its subdomains
	sub := `{"accountId":"0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10","from":` + addr("", "a@mail.example.com") + `,"to":` + addr("", "b@example.com") + `,"text":"hi"}`
	rec = httptest.NewRecorder()
	routes(noProduce, noStats, noSearch, []string{"example.com"}).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/send-transactional", strings.NewReader(sub)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("subdomain: got %d, want 202", rec.Code)
	}
}

func TestStats(t *testing.T) {
	var got stats.Filter
	var queryErr error
	handler := routes(noProduce, func(_ context.Context, filter stats.Filter) ([]stats.Hour, error) {
		got = filter
		return []stats.Hour{{Accepted: 3, Processed: 2}, {Accepted: 1, Processed: 1}}, queryErr
	}, noSearch, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/stats?accountId=0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10"+
		"&from=2026-11-03&to=2026-11-04T12:00:00%2B01:00&tags=billing&tags=vip", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if got.AccountID != "0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10" || len(got.Tags) != 2 || got.Tags[1] != "vip" ||
		!got.From.Equal(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)) ||
		!got.To.Equal(time.Date(2026, 11, 4, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("filter = %+v", got)
	}
	var resp statsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if resp.Accepted != 4 || resp.Processed != 3 || len(resp.Hours) != 2 {
		t.Errorf("response = %+v, want totals 4/3 and 2 hours", resp)
	}

	for _, queryString := range []string{"accountId=nope", "from=yesterday", "to=2026-13-01", "from=2026-11-04&to=2026-11-03", "from=2026-11-03&to=2026-11-03"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/stats?"+queryString, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", queryString, rec.Code)
		}
	}

	// no filters is fine, and an empty result is [] rather than null
	rec = httptest.NewRecorder()
	routes(noProduce, noStats, noSearch, nil).ServeHTTP(rec, httptest.NewRequest("GET", "/v1/stats", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"hours":[]`) {
		t.Errorf("empty: got %d %s", rec.Code, rec.Body)
	}

	queryErr = errors.New("clickhouse down")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/stats", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("query error: got %d, want 500", rec.Code)
	}
}

func TestActivity(t *testing.T) {
	var got activity.Filter
	var searchErr error
	handler := routes(noProduce, noStats, func(_ context.Context, filter activity.Filter) ([]activity.Event, int, error) {
		got = filter
		return []activity.Event{{Event: "processed", EventID: "e1"}}, 7, searchErr
	}, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/activity?accountId=0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10"+
		"&from.address=alice@example.com&subject=invoice&tags=billing&tags=vip&from=2026-11-03&limit=20", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if got.Limit != 20 || !got.From.Equal(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)) || !got.To.IsZero() ||
		got.Fields.Get("from.address") != "alice@example.com" || got.Fields.Get("subject") != "invoice" ||
		len(got.Fields["tags"]) != 2 || len(got.Fields) != 4 {
		t.Errorf("filter = %+v", got)
	}
	var resp activityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if resp.Total != 7 || len(resp.Events) != 1 || resp.Events[0].Event != "processed" {
		t.Errorf("response = %+v", resp)
	}

	// defaults: no filters, newest 50
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/activity", nil))
	if rec.Code != http.StatusOK || got.Limit != defaultActivityLimit || len(got.Fields) != 0 {
		t.Errorf("defaults: got %d, filter %+v", rec.Code, got)
	}

	for _, queryString := range []string{"text=hi", "html=x", "acountId=typo", "limit=0", "limit=101", "limit=ten",
		"from=yesterday", "from=2026-11-04&to=2026-11-03"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/activity?"+queryString, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", queryString, rec.Code)
		}
	}

	// an empty result is [] rather than null
	rec = httptest.NewRecorder()
	routes(noProduce, noStats, noSearch, nil).ServeHTTP(rec, httptest.NewRequest("GET", "/v1/activity", nil))
	if !strings.Contains(rec.Body.String(), `"events":[]`) {
		t.Errorf("empty: %s", rec.Body)
	}

	searchErr = errors.New("opensearch down")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/activity", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("search error: got %d, want 500", rec.Code)
	}
}
