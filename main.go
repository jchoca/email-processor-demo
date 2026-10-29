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

	"github.com/segmentio/kafka-go"
)

func routes(produce produceFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /v1/send-transactional", func(w http.ResponseWriter, r *http.Request) {
		sendTransactional(w, r, produce)
	})
	return mux
}

const maxAddressLen = 254

type address struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// single address per field.
type transactionalEmail struct {
	From    address `json:"from"`
	To      address `json:"to"`
	CC      address `json:"cc"`
	BCC     address `json:"bcc"`
	Subject string  `json:"subject"`
	HTML    string  `json:"html"`
	Text    string  `json:"text"`
}

func (e transactionalEmail) validate() error {
	for _, a := range []struct {
		name     string
		value    address
		required bool
	}{
		{"from", e.From, true},
		{"to", e.To, true},
		{"cc", e.CC, false},
		{"bcc", e.BCC, false},
	} {
		addr := strings.TrimSpace(a.value.Address)
		if utf8.RuneCountInString(a.value.Name) > maxAddressLen {
			return fmt.Errorf("%s.name must be %d characters or fewer", a.name, maxAddressLen)
		}
		if utf8.RuneCountInString(addr) > maxAddressLen {
			return fmt.Errorf("%s.address must be %d characters or fewer", a.name, maxAddressLen)
		}
		if addr == "" {
			if a.required {
				return fmt.Errorf("%s.address is required", a.name)
			}
			continue
		}
		// reject display names here; they belong in .name
		parsed, err := mail.ParseAddress(addr)
		if err != nil || parsed.Address != addr {
			return fmt.Errorf("%s.address is not a valid email address", a.name)
		}
	}
	if strings.TrimSpace(e.HTML) == "" && strings.TrimSpace(e.Text) == "" {
		return errors.New("html or text body is required")
	}
	if utf8.RuneCountInString(e.Subject) > 256 {
		return errors.New("subject must be 256 characters or fewer")
	}
	return nil
}

const (
	maxBodyBytes  = 1 << 20 // 1 MB
	topicAccepted = "message-accepted"
)

// produceFunc appends one JSON payload to the accepted topic.
type produceFunc func(ctx context.Context, value []byte) error

func sendTransactional(w http.ResponseWriter, r *http.Request, produce produceFunc) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var e transactionalEmail
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if err := e.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	payload, err := json.Marshal(e)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := produce(r.Context(), payload); err != nil {
		slog.Error("produce", "topic", topicAccepted, "err", err)
		http.Error(w, "failed to enqueue message", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func main() {
	kw := &kafka.Writer{
		Addr:         kafka.TCP(brokers()...),
		Topic:        topicAccepted,
		RequiredAcks: kafka.RequireAll,
	}
	defer kw.Close()

	srv := &http.Server{
		Addr: ":" + port(),
		Handler: routes(func(ctx context.Context, value []byte) error {
			return kw.WriteMessages(ctx, kafka.Message{Value: value})
		}),
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

func brokers() []string {
	if b := os.Getenv("KAFKA_BROKERS"); b != "" {
		return strings.Split(b, ",")
	}
	return []string{"localhost:9092"}
}
