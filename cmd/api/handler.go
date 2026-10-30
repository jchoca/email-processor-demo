package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"example.com/email-processor/internal/config"
	"example.com/email-processor/internal/email"
)

const maxBodyBytes = 1 << 20 // 1 MB

// produceFunc appends one JSON payload to the accepted topic.
type produceFunc func(ctx context.Context, value []byte) error

func routes(produce produceFunc, allowedSenders []string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /v1/send-transactional", func(w http.ResponseWriter, r *http.Request) {
		sendTransactional(w, r, produce, allowedSenders)
	})
	return mux
}

func sendTransactional(w http.ResponseWriter, r *http.Request, produce produceFunc, allowedSenders []string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var e email.Transactional
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	// the payload goes out with ids and a timestamp the client cannot set
	e.NewMessage()
	if err := e.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !e.SenderAllowed(allowedSenders) {
		http.Error(w, "from domain is not allowed to send", http.StatusBadRequest)
		return
	}
	payload, err := json.Marshal(e)
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
