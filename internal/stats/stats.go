// Package stats reads hourly message counts from clickhouse's sums_by_hour
// table over the clickhouse HTTP interface.
package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Filter narrows the hourly sums; zero values do not filter.
type Filter struct {
	AccountID string
	From, To  time.Time // hours whose start is in [From, To)
	Tags      []string  // rows must have every tag
}

// Hour is the number of messages accepted and processed in one hour.
type Hour struct {
	Timestamp time.Time `json:"timestamp"`
	Accepted  uint64    `json:"accepted"`
	Processed uint64    `json:"processed"`
}

// Client queries one clickhouse database.
type Client struct {
	URL      string // e.g. http://clickhouse:8123
	User     string
	Password string
	Database string
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Hours returns the summed counts per hour matching filter, oldest first.
func (client Client) Hours(ctx context.Context, filter Filter) ([]Hour, error) {
	query, params := build(filter)
	params.Set("database", client.Database)
	params.Set("date_time_output_format", "iso")
	params.Set("output_format_json_quote_64bit_integers", "0")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.URL+"/?"+params.Encode(), strings.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(client.User, client.Password)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("clickhouse: %s: %s", resp.Status, msg)
	}

	var out struct {
		Data []Hour `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("clickhouse: decode: %w", err)
	}
	return out.Data, nil
}

// tsvEscape escapes a String query parameter, which clickhouse reads with
// tab-separated escaping rules.
var tsvEscape = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`)

// build returns the query and its parameters. Filter values only travel as
// clickhouse query parameters, never as SQL text. sums_by_hour is a
// SummingMergeTree that merges in the background, so rows are summed again.
func build(filter Filter) (string, url.Values) {
	var conds []string
	params := url.Values{}
	if filter.AccountID != "" {
		conds = append(conds, "accountId = {accountId:UUID}")
		params.Set("param_accountId", filter.AccountID)
	}
	if !filter.From.IsZero() {
		conds = append(conds, "timestamp >= {from:DateTime('UTC')}")
		params.Set("param_from", filter.From.UTC().Format(time.DateTime))
	}
	if !filter.To.IsZero() {
		conds = append(conds, "timestamp < {to:DateTime('UTC')}")
		params.Set("param_to", filter.To.UTC().Format(time.DateTime))
	}
	for index, tag := range filter.Tags {
		name := fmt.Sprintf("tag%d", index)
		conds = append(conds, "has(tags, {"+name+":String})")
		params.Set("param_"+name, tsvEscape.Replace(tag))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ") + " "
	}
	return "SELECT timestamp, sum(accepted) AS accepted, sum(processed) AS processed " +
		"FROM sums_by_hour " + where +
		"GROUP BY timestamp ORDER BY timestamp FORMAT JSON", params
}
