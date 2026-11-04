package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"example.com/email-processor/internal/activity"
	"example.com/email-processor/internal/config"
	"example.com/email-processor/internal/email"
	"example.com/email-processor/internal/stats"
)

const (
	maxBodyBytes         = 1 << 20 // 1 MB
	defaultActivityLimit = 50
	maxActivityLimit     = 100
)

// produceFunc appends one JSON payload to the accepted topic.
type produceFunc func(ctx context.Context, value []byte) error

// hoursFunc reads the hourly message counts matching a filter.
type hoursFunc func(ctx context.Context, filter stats.Filter) ([]stats.Hour, error)

// searchFunc returns the newest events matching a filter and the total matches.
type searchFunc func(ctx context.Context, filter activity.Filter) ([]activity.Event, int, error)

func routes(produce produceFunc, hours hoursFunc, search searchFunc, allowedSenders []string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /v1/send-transactional", func(w http.ResponseWriter, r *http.Request) {
		sendTransactional(w, r, produce, allowedSenders)
	})
	mux.HandleFunc("GET /v1/stats", func(w http.ResponseWriter, r *http.Request) {
		getStats(w, r, hours)
	})
	mux.HandleFunc("GET /v1/activity", func(w http.ResponseWriter, r *http.Request) {
		getActivity(w, r, search)
	})
	return mux
}

func sendTransactional(w http.ResponseWriter, r *http.Request, produce produceFunc, allowedSenders []string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var message email.Transactional
	if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	// the payload goes out with ids and a timestamp the client cannot set
	message.NewMessage()
	if err := message.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !message.SenderAllowed(allowedSenders) {
		http.Error(w, "from domain is not allowed to send", http.StatusBadRequest)
		return
	}
	payload, err := json.Marshal(message)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := produce(r.Context(), payload); err != nil {
		slog.Error("produce", "topic", config.TopicAccepted, "err", err)
		http.Error(w, "failed to enqueue message", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type statsResponse struct {
	Accepted  uint64       `json:"accepted"`
	Processed uint64       `json:"processed"`
	Hours     []stats.Hour `json:"hours"`
}

func getStats(w http.ResponseWriter, r *http.Request, hours hoursFunc) {
	filter, err := parseStatsFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := hours(r.Context(), filter)
	if err != nil {
		slog.Error("stats", "err", err)
		http.Error(w, "failed to query stats", http.StatusInternalServerError)
		return
	}
	resp := statsResponse{Hours: []stats.Hour{}}
	for _, hour := range rows {
		resp.Accepted += hour.Accepted
		resp.Processed += hour.Processed
		resp.Hours = append(resp.Hours, hour)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// parseStatsFilter reads accountId, from, to and repeated tags.
func parseStatsFilter(queryParams url.Values) (stats.Filter, error) {
	filter := stats.Filter{AccountID: queryParams.Get("accountId"), Tags: queryParams["tags"]}
	if filter.AccountID != "" && !email.IsUUID(filter.AccountID) {
		return filter, errors.New("accountId must be a UUID")
	}
	var err error
	filter.From, filter.To, err = parseTimeRange(queryParams)
	return filter, err
}

// parseTimeRange reads from and to as RFC 3339 times or plain dates (midnight
// UTC); to is exclusive. A missing bound is the zero time.
func parseTimeRange(queryParams url.Values) (from, to time.Time, err error) {
	for _, param := range []struct {
		name string
		dst  *time.Time
	}{{"from", &from}, {"to", &to}} {
		value := queryParams.Get(param.name)
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			if parsed, err = time.Parse(time.DateOnly, value); err != nil {
				return from, to, errors.New(param.name + " must be an RFC 3339 time or a YYYY-MM-DD date")
			}
		}
		*param.dst = parsed
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return from, to, errors.New("from must be before to")
	}
	return from, to, nil
}

type activityResponse struct {
	Total  int              `json:"total"`
	Events []activity.Event `json:"events"`
}

func getActivity(w http.ResponseWriter, r *http.Request, search searchFunc) {
	filter, err := parseActivityFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	events, total, err := search(r.Context(), filter)
	if err != nil {
		slog.Error("activity", "err", err)
		http.Error(w, "failed to search activity", http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []activity.Event{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(activityResponse{Total: total, Events: events})
}

// parseActivityFilter reads from, to, limit and any indexed field. Unknown
// params are rejected so a typo cannot silently widen the search.
func parseActivityFilter(queryParams url.Values) (activity.Filter, error) {
	filter := activity.Filter{Fields: url.Values{}, Limit: defaultActivityLimit}
	var err error
	if filter.From, filter.To, err = parseTimeRange(queryParams); err != nil {
		return filter, err
	}
	for name, values := range queryParams {
		switch {
		case name == "from" || name == "to":
		case name == "limit":
			limit, err := strconv.Atoi(queryParams.Get("limit"))
			if err != nil || limit < 1 || limit > maxActivityLimit {
				return filter, fmt.Errorf("limit must be between 1 and %d", maxActivityLimit)
			}
			filter.Limit = limit
		case activity.Filterable(name):
			filter.Fields[name] = values
		default:
			return filter, fmt.Errorf("unknown filter %q", name)
		}
	}
	return filter, nil
}
