// Command worker consumes accepted messages and delivers them over SMTP.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/smtp"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/email-processor/internal/config"
	"example.com/email-processor/internal/email"
	"example.com/email-processor/internal/mailmsg"

	"github.com/segmentio/kafka-go"
)

// sendFunc delivers one MIME message to the envelope recipients.
type sendFunc func(from string, to []string, msg []byte) error

// retryDelay is the wait between send attempts for one message.
var retryDelay = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	smtpAddr := config.SMTPAddr()
	send := func(from string, to []string, msg []byte) error {
		// shortcut: plaintext SMTP only, add TLS/auth when the relay needs it
		return smtp.SendMail(smtpAddr, nil, from, to, msg)
	}

	// Commits are explicit (kafka-go only commits via CommitMessages), so an
	// offset moves forward only after the message was delivered.
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: config.Brokers(),
		Topic:   config.TopicAccepted,
		GroupID: "worker",
	})
	defer r.Close()

	slog.Info("consuming", "topic", config.TopicAccepted, "smtp", smtpAddr)
	for {
		m, err := r.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("read", "err", err)
			continue
		}

		e, err := decode(m.Value)
		if err != nil {
			// unparseable payloads never become sendable: drop instead of blocking
			slog.Error("skip", "offset", m.Offset, "err", err)
		} else {
			deliver(ctx, e, send) // retries until sent or shutting down
		}
		if ctx.Err() != nil {
			return // no commit: the message is redelivered after restart
		}
		if err := r.CommitMessages(ctx, m); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("commit", "offset", m.Offset, "err", err)
		}
	}
}

// deliver retries sending until it succeeds or ctx is done.
func deliver(ctx context.Context, e email.Transactional, send sendFunc) {
	msg, err := mailmsg.Build(e)
	if err != nil {
		slog.Error("skip", "err", err)
		return
	}
	rcpts := e.Recipients()
	for attempt := 1; ; attempt++ {
		err := send(e.From.Address, rcpts, msg)
		if err == nil {
			slog.Info("sent", "to", rcpts, "subject", e.Subject)
			return
		}
		slog.Error("send", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}

// decode parses and validates one accepted payload.
func decode(payload []byte) (email.Transactional, error) {
	var e email.Transactional
	if err := json.Unmarshal(payload, &e); err != nil {
		return e, fmt.Errorf("decode: %w", err)
	}
	if err := e.Validate(); err != nil {
		return e, fmt.Errorf("validate: %w", err)
	}
	return e, nil
}
