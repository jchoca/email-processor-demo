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

	"github.com/twmb/franz-go/pkg/kgo"
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

	// one client consumes accepted and produces processed events. Autocommit
	// would commit on a timer, before a message may be sent, so offsets move
	// forward only through CommitRecords after delivery.
	kc, err := kgo.NewClient(
		kgo.SeedBrokers(config.Brokers()...),
		kgo.ConsumerGroup("worker"),
		kgo.ConsumeTopics(config.TopicAccepted),
		kgo.DisableAutoCommit(),
		kgo.DefaultProduceTopic(config.TopicProcessed),
		// records retry forever by default; give up and log instead
		kgo.RecordDeliveryTimeout(10*time.Second),
	)
	if err != nil {
		slog.Error("kafka", "err", err)
		os.Exit(1)
	}
	defer kc.Close()

	slog.Info("consuming", "topic", config.TopicAccepted, "smtp", smtp.Addr, "tls", smtp.RequireTLS)
	for {
		fetches := kc.PollFetches(ctx)
		if ctx.Err() != nil {
			return
		}
		fetches.EachError(func(topic string, partition int32, err error) {
			slog.Error("read", "topic", topic, "partition", partition, "err", err)
		})

		for _, r := range fetches.Records() {
			e, err := decode(r.Value, allowedSenders)
			if err != nil {
				// unparseable or disallowed payloads never become sendable: drop
				slog.Error("skip", "offset", r.Offset, "err", err)
			} else if deliver(ctx, e, smtp.Send) { // retries transient failures until sent
				// the processed event is its own event: fresh id and timestamp
				e.Stamp()
				payload, err := json.Marshal(e)
				if err != nil {
					slog.Error("marshal processed", "offset", r.Offset, "err", err)
				} else if err := kc.ProduceSync(ctx, &kgo.Record{Value: payload}).FirstErr(); err != nil {
					slog.Error("publish", "topic", config.TopicProcessed, "err", err)
				}
			}
			if ctx.Err() != nil {
				return // no commit: the message is redelivered after restart
			}
			if err := kc.CommitRecords(ctx, r); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Error("commit", "offset", r.Offset, "err", err)
			}
		}
	}
}

// deliver sends one message and reports whether it went out: transient errors
// are retried with backoff, SMTP 5xx responses are permanent and dropped so a
// bad message cannot block the topic.
func deliver(ctx context.Context, e email.Transactional, send sendFunc) bool {
	msg, err := mailmsg.Build(e)
	if err != nil {
		slog.Error("skip", "err", err)
		return false
	}
	rcpts := []string{e.To.Address}
	for attempt := 1; ; attempt++ {
		err := send(e.From.Address, rcpts, msg)
		if err == nil {
			slog.Info("sent", "to", rcpts, "subject", e.Subject)
			return true
		}
		if permanent(err) {
			slog.Error("drop", "attempt", attempt, "err", err)
			return false
		}
		slog.Error("send", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return false
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
