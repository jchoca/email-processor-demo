// Package activity searches the accepted and processed events in opensearch.
package activity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"example.com/email-processor/internal/email"
)

// matchers says how each filterable field is queried, mirroring
// opensearch/template.json: keyword fields match exactly, text fields by words.
var matchers = map[string]string{
	"messageId":       "term",
	"eventId":         "term",
	"accountId":       "term",
	"tags":            "term",
	"from.address":    "term",
	"to.address":      "term",
	"subject.keyword": "term",
	"subject":         "match",
	"from.name":       "match",
	"to.name":         "match",
}

// Filterable reports whether name is an indexed field that can be filtered on.
func Filterable(name string) bool {
	_, ok := matchers[name]
	return ok
}

// Filter narrows the events; zero values do not filter.
type Filter struct {
	Fields   url.Values // field name to values; every value must match
	From, To time.Time  // timestamp in [From, To)
	Limit    int        // number of events returned, newest first
}

// Event is one accepted or processed event, without the body.
type Event struct {
	Event     string        `json:"event"` // accepted or processed
	EventID   string        `json:"eventId"`
	MessageID string        `json:"messageId"`
	AccountID string        `json:"accountId"`
	Timestamp time.Time     `json:"timestamp"`
	From      email.Address `json:"from"`
	To        email.Address `json:"to"`
	Subject   string        `json:"subject"`
	Tags      []string      `json:"tags"`
}

// Client searches one opensearch cluster.
type Client struct {
	URL string // e.g. http://opensearch:9200
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Search returns the newest events matching filter and how many match in total.
func (client Client) Search(ctx context.Context, filter Filter) ([]Event, int, error) {
	body, err := json.Marshal(query(filter))
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.URL+"/message-*/_search", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, 0, fmt.Errorf("opensearch: %s: %s", resp.Status, msg)
	}

	var out struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Hits []struct {
				Index  string `json:"_index"`
				Source Event  `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, fmt.Errorf("opensearch: decode: %w", err)
	}
	events := []Event{}
	for _, hit := range out.Hits.Hits {
		event := hit.Source
		event.Event = strings.TrimPrefix(hit.Index, "message-")
		events = append(events, event)
	}
	return events, out.Hits.Total.Value, nil
}

type object = map[string]any

// query builds the search body. Filter values are JSON-encoded, so they can
// never change the shape of the query.
func query(filter Filter) object {
	clauses := []object{}
	for _, name := range slices.Sorted(maps.Keys(filter.Fields)) {
		for _, value := range filter.Fields[name] {
			if matchers[name] == "match" {
				clauses = append(clauses, object{"match": object{name: object{"query": value, "operator": "and"}}})
			} else {
				clauses = append(clauses, object{"term": object{name: value}})
			}
		}
	}
	timeRange := object{}
	if !filter.From.IsZero() {
		timeRange["gte"] = filter.From.UTC().Format(time.RFC3339Nano)
	}
	if !filter.To.IsZero() {
		timeRange["lt"] = filter.To.UTC().Format(time.RFC3339Nano)
	}
	if len(timeRange) > 0 {
		clauses = append(clauses, object{"range": object{"timestamp": timeRange}})
	}
	return object{
		"size":             filter.Limit,
		"sort":             []object{{"timestamp": "desc"}},
		"track_total_hits": true,
		"_source":          object{"excludes": []string{"text", "html"}},
		"query":            object{"bool": object{"filter": clauses}},
	}
}
