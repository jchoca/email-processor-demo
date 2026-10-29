package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /v1/send-transactional", sendTransactional)
	return mux
}

// single address per field.
type transactionalEmail struct {
	From    string `json:"from"`
	To      string `json:"to"`
	CC      string `json:"cc"`
	BCC     string `json:"bcc"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

func (e transactionalEmail) validate() error {
	for _, a := range []struct {
		name     string
		value    string
		required bool
	}{
		{"from", e.From, true},
		{"to", e.To, true},
		{"cc", e.CC, false},
		{"bcc", e.BCC, false},
	} {
		if strings.TrimSpace(a.value) == "" {
			if a.required {
				return fmt.Errorf("%s is required", a.name)
			}
			continue
		}
		if _, err := mail.ParseAddress(a.value); err != nil {
			return fmt.Errorf("%s is not a valid email address", a.name)
		}
	}
	if strings.TrimSpace(e.HTML) == "" && strings.TrimSpace(e.Text) == "" {
		return errors.New("html or text body is required")
	}
	if utf8.RuneCountInString(e.Subject) > 50 {
		return errors.New("subject must be 50 characters or fewer")
	}
	return nil
}

func sendTransactional(w http.ResponseWriter, r *http.Request) {
	var e transactionalEmail
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if err := e.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := json.NewEncoder(os.Stdout).Encode(e); err != nil {
		slog.Error("log transactional email", "err", err)
	}
	w.WriteHeader(http.StatusAccepted)
}

func main() {
	srv := &http.Server{
		Addr:              ":" + port(),
		Handler:           routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}
}

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}
