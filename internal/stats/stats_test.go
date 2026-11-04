package stats

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuild(t *testing.T) {
	query, params := build(Filter{})
	if strings.Contains(query, "WHERE") || len(params) != 0 {
		t.Errorf("no filter: query %q, params %v", query, params)
	}

	query, params = build(Filter{
		AccountID: "0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10",
		From:      time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC),
		To:        time.Date(2026, 11, 3, 13, 0, 0, 0, time.FixedZone("", 3600)),
		Tags:      []string{"vip", "a\\b\tc'"},
	})
	want := "WHERE accountId = {accountId:UUID} AND timestamp >= {from:DateTime('UTC')} AND " +
		"timestamp < {to:DateTime('UTC')} AND has(tags, {tag0:String}) AND has(tags, {tag1:String}) GROUP BY"
	if !strings.Contains(query, want) {
		t.Errorf("query = %q, want it to contain %q", query, want)
	}
	for name, expected := range map[string]string{
		"param_accountId": "0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10",
		"param_from":      "2026-11-03 10:00:00",
		"param_to":        "2026-11-03 12:00:00", // converted to UTC
		"param_tag0":      "vip",
		"param_tag1":      `a\\b\tc'`, // escaped, never spliced into the SQL
	} {
		if got := params.Get(name); got != expected {
			t.Errorf("%s = %q, want %q", name, got, expected)
		}
	}
}

func TestHours(t *testing.T) {
	var gotQuery, gotDB, gotUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotQuery, gotDB = string(body), r.URL.Query().Get("database")
		gotUser, _, _ = r.BasicAuth()
		io.WriteString(w, `{"meta":[],"data":[{"timestamp":"2026-11-03T22:00:00Z","accepted":3,"processed":2}],"rows":1}`)
	}))
	defer srv.Close()

	client := Client{URL: srv.URL, User: "u", Password: "p", Database: "db"}
	hours, err := client.Hours(context.Background(), Filter{})
	if err != nil {
		t.Fatalf("hours: %v", err)
	}
	if len(hours) != 1 || hours[0].Accepted != 3 || hours[0].Processed != 2 ||
		!hours[0].Timestamp.Equal(time.Date(2026, 11, 3, 22, 0, 0, 0, time.UTC)) {
		t.Errorf("hours = %+v", hours)
	}
	if !strings.Contains(gotQuery, "FROM sums_by_hour") || gotDB != "db" || gotUser != "u" {
		t.Errorf("query %q, database %q, user %q", gotQuery, gotDB, gotUser)
	}

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Code: 60. UNKNOWN_TABLE", http.StatusNotFound)
	})
	if _, err := client.Hours(context.Background(), Filter{}); err == nil || !strings.Contains(err.Error(), "UNKNOWN_TABLE") {
		t.Errorf("clickhouse error: got %v", err)
	}
}
