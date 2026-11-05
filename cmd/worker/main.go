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
	kafkaClient, err := kgo.NewClient(
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
	defer kafkaClient.Close()

	slog.Info("consuming", "topic", config.TopicAccepted, "smtp", smtp.Addr, "tls", smtp.RequireTLS)
	for {
		fetches := kafkaClient.PollFetches(ctx)
		if ctx.Err() != nil {
			return
		}
		fetches.EachError(func(topic string, partition int32, err error) {
			slog.Error("read", "topic", topic, "partition", partition, "err", err)
		})

		for _, record := range fetches.Records() {
			message, err := decode(record.Value, allowedSenders)

			if err != nil {
				// Unparseable or disallowed payloads never become sendable: drop.
				slog.Error("skip", "offset", record.Offset, "err", err)
			} else if deliver(ctx, message, smtp.Send) {
				// Delivery succeeded; publish a processed event.
				if err := publishProcessed(ctx, kafkaClient, message); err != nil {
					slog.Error("publish processed", "offset", record.Offset, "err", err)
					return // No commit; retry after restart.
				}
			}

			if ctx.Err() != nil {
				return // No commit; the message is redelivered after restart.
			}

			if err := kafkaClient.CommitRecords(ctx, record); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Error("commit", "offset", record.Offset, "err", err)
			}
		}
	}
}

// publishProcessed publishes a new event for a successfully delivered message.
func publishProcessed(
	ctx context.Context,
	client *kgo.Client,
	message email.Transactional,
) error {
	message.Stamp()

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal processed: %w", err)
	}

	if err := client.ProduceSync(ctx, &kgo.Record{
		Value: payload,
	}).FirstErr(); err != nil {
		return fmt.Errorf("produce processed: %w", err)
	}

	return nil
}

// deliver sends one message and reports whether it went out: transient errors
// are retried with backoff, SMTP 5xx responses are permanent and dropped so a
// bad message cannot block the topic.
func deliver(ctx context.Context, message email.Transactional, send sendFunc) bool {
	msg, err := mailmsg.Build(message)
	if err != nil {
		slog.Error("skip", "err", err)
		return false
	}
	rcpts := []string{message.To.Address}
	for attempt := 1; ; attempt++ {
		err := send(message.From.Address, rcpts, msg)
		if err == nil {
			slog.Info("sent", "to", rcpts, "subject", message.Subject)
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
	var smtpErr *textproto.Error
	return errors.As(err, &smtpErr) && smtpErr.Code >= 500 && smtpErr.Code < 600
}

// backoff doubles the wait per attempt, capped at retryMax.
func backoff(attempt int) time.Duration {
	if attempt > 16 {
		return retryMax
	}
	if delay := retryBase << (attempt - 1); delay > 0 && delay < retryMax {
		return delay
	}
	return retryMax
}

// decode parses, validates and sender-checks one accepted payload.
func decode(payload []byte, allowedSenders []string) (email.Transactional, error) {
	var message email.Transactional
	if err := json.Unmarshal(payload, &message); err != nil {
		return message, fmt.Errorf("decode: %w", err)
	}
	if err := message.Validate(); err != nil {
		return message, fmt.Errorf("validate: %w", err)
	}
	if !message.SenderAllowed(allowedSenders) {
		return message, fmt.Errorf("from domain %q is not allowed", message.From.Address)
	}
	return message, nil
}
