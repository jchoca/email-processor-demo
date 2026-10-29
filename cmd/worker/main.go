// Command worker consumes accepted messages and delivers them over SMTP.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/textproto"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/email-processor/internal/config"
	"example.com/email-processor/internal/email"
	"example.com/email-processor/internal/mailmsg"
	"example.com/email-processor/internal/sender"

	"github.com/segmentio/kafka-go"
)

// sendFunc delivers one MIME message to the envelope recipients.
type sendFunc func(from string, to []string, msg []byte) error

var (
	retryBase = time.Second
	retryMax  = time.Minute
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	smtp := sender.Config{
		Addr:       config.SMTPAddr(),
		Username:   config.SMTPUsername(),
		Password:   config.SMTPPassword(),
		RequireTLS: config.SMTPRequireTLS(),
	}
	allowedSenders := config.SenderDomains()

	// FetchMessage does not commit, and ReadMessage would commit before the
	// message is sent, so offsets move forward only after delivery.
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: config.Brokers(),
		Topic:   config.TopicAccepted,
		GroupID: "worker",
	})
	defer r.Close()

	slog.Info("consuming", "topic", config.TopicAccepted, "smtp", smtp.Addr, "tls", smtp.RequireTLS)
	for {
		m, err := r.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("read", "err", err)
			continue
		}

		e, err := decode(m.Value, allowedSenders)
		if err != nil {
			// unparseable or disallowed payloads never become sendable: drop
			slog.Error("skip", "offset", m.Offset, "err", err)
		} else {
			deliver(ctx, e, smtp.Send) // retries transient failures until sent
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

// deliver sends one message: transient errors are retried with backoff, SMTP
// 5xx responses are permanent and dropped so a bad message cannot block the topic.
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
		if permanent(err) {
			slog.Error("drop", "attempt", attempt, "err", err)
			return
		}
		slog.Error("send", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
}

// permanent reports whether err is an SMTP 5xx reply, which retrying cannot fix.
func permanent(err error) bool {
	var tp *textproto.Error
	return errors.As(err, &tp) && tp.Code >= 500 && tp.Code < 600
}

// backoff doubles the wait per attempt, capped at retryMax.
func backoff(attempt int) time.Duration {
	if attempt > 16 {
		return retryMax
	}
	if d := retryBase << (attempt - 1); d > 0 && d < retryMax {
		return d
	}
	return retryMax
}

// decode parses, validates and sender-checks one accepted payload.
func decode(payload []byte, allowedSenders []string) (email.Transactional, error) {
	var e email.Transactional
	if err := json.Unmarshal(payload, &e); err != nil {
		return e, fmt.Errorf("decode: %w", err)
	}
	if err := e.Validate(); err != nil {
		return e, fmt.Errorf("validate: %w", err)
	}
	if !e.SenderAllowed(allowedSenders) {
		return e, fmt.Errorf("from domain %q is not allowed", e.From.Address)
	}
	return e, nil
}
