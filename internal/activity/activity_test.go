package activity

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestQuery(t *testing.T) {
	body, err := json.Marshal(query(Filter{
		Fields: url.Values{
			"subject":   {"your invoice"},
			"tags":      {"billing", "vip"},
			"accountId": {`"}},{"match_all":{}}`},
		},
		From:  time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2026, 11, 4, 1, 0, 0, 0, time.FixedZone("", 3600)),
		Limit: 20,
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"_source":{"excludes":["text","html"]},"query":{"bool":{"filter":[` +
		`{"term":{"accountId":"\"}},{\"match_all\":{}}"}},` + // a value stays a value
		`{"match":{"subject":{"operator":"and","query":"your invoice"}}},` +
		`{"term":{"tags":"billing"}},{"term":{"tags":"vip"}},` +
		`{"range":{"timestamp":{"gte":"2026-11-03T00:00:00Z","lt":"2026-11-04T00:00:00Z"}}}]}},` +
		`"size":20,"sort":[{"timestamp":"desc"}],"track_total_hits":true}`
	if string(body) != want {
		t.Errorf("query =\n%s\nwant\n%s", body, want)
	}

	body, _ = json.Marshal(query(Filter{Limit: 5}))
	if !strings.Contains(string(body), `"filter":[]`) {
		t.Errorf("no filter: %s", body)
	}
}

func TestFilterable(t *testing.T) {
	for _, name := range []string{"accountId", "from.address", "subject", "subject.keyword", "to.name"} {
		if !Filterable(name) {
			t.Errorf("%s should be filterable", name)
		}
	}
	for _, name := range []string{"text", "html", "from", "timestamp", "limit"} {
		if Filterable(name) {
			t.Errorf("%s should not be a field filter", name)
		}
	}
}

func TestSearch(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotPath, gotBody = r.URL.Path, string(body)
		io.WriteString(w, `{"hits":{"total":{"value":2,"relation":"eq"},"hits":[`+
			`{"_index":"message-processed","_source":{"eventId":"e2","messageId":"m1","subject":"hi","tags":["vip"]}},`+
			`{"_index":"message-accepted","_source":{"eventId":"e1","messageId":"m1","subject":"hi"}}]}}`)
	}))
	defer server.Close()

	client := Client{URL: server.URL}
	events, total, err := client.Search(context.Background(), Filter{Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if gotPath != "/message-*/_search" || !strings.Contains(gotBody, `"size":10`) {
		t.Errorf("path %q, body %s", gotPath, gotBody)
	}
	if total != 2 || len(events) != 2 || events[0].Event != "processed" || events[0].EventID != "e2" ||
		events[1].Event != "accepted" || events[0].Tags[0] != "vip" {
		t.Errorf("total %d, events %+v", total, events)
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"search_phase_execution_exception"}`, http.StatusBadRequest)
	})
	if _, _, err := client.Search(context.Background(), Filter{}); err == nil || !strings.Contains(err.Error(), "search_phase") {
		t.Errorf("opensearch error: got %v", err)
	}
}
