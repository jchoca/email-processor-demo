package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"example.com/email-processor/internal/config"
	"example.com/email-processor/internal/email"
	"example.com/email-processor/internal/stats"
)

const maxBodyBytes = 1 << 20 // 1 MB

// produceFunc appends one JSON payload to the accepted topic.
type produceFunc func(ctx context.Context, value []byte) error

// hoursFunc reads the hourly message counts matching a filter.
type hoursFunc func(ctx context.Context, filter stats.Filter) ([]stats.Hour, error)

func routes(produce produceFunc, hours hoursFunc, allowedSenders []string) http.Handler {
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

// parseStatsFilter reads accountId, from, to and repeated tags. from and to are
// RFC 3339 times or plain dates (midnight UTC); to is exclusive.
func parseStatsFilter(queryParams url.Values) (stats.Filter, error) {
	filter := stats.Filter{AccountID: queryParams.Get("accountId"), Tags: queryParams["tags"]}
	if filter.AccountID != "" && !email.IsUUID(filter.AccountID) {
		return filter, errors.New("accountId must be a UUID")
	}
	for _, param := range []struct {
		name string
		dst  *time.Time
	}{{"from", &filter.From}, {"to", &filter.To}} {
		value := queryParams.Get(param.name)
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			if parsed, err = time.Parse(time.DateOnly, value); err != nil {
				return filter, errors.New(param.name + " must be an RFC 3339 time or a YYYY-MM-DD date")
			}
		}
		*param.dst = parsed
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && !filter.From.Before(filter.To) {
		return filter, errors.New("from must be before to")
	}
	return filter, nil
}
