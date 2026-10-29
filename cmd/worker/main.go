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

	"example.com/email-processor/internal/config"
	"example.com/email-processor/internal/email"
	"example.com/email-processor/internal/mailmsg"

	"github.com/segmentio/kafka-go"
)

// sendFunc delivers one MIME message to the envelope recipients.
type sendFunc func(from string, to []string, msg []byte) error

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	smtpAddr := config.SMTPAddr()
	send := func(from string, to []string, msg []byte) error {
		// shortcut: plaintext SMTP only, add TLS/auth when the relay needs it
		return smtp.SendMail(smtpAddr, nil, from, to, msg)
	}

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
		// shortcut: offsets auto-commit, so a failed send is dropped;
		// add a retry/DLQ topic when losing mail is not acceptable
		if err := handle(m.Value, send); err != nil {
			slog.Error("handle", "offset", m.Offset, "err", err)
		}
	}
}

// handle decodes one accepted payload, renders MIME and sends it over SMTP.
func handle(payload []byte, send sendFunc) error {
	var e email.Transactional
	if err := json.Unmarshal(payload, &e); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if err := e.Validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	msg, err := mailmsg.Build(e)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	rcpts := e.Recipients()
	if err := send(e.From.Address, rcpts, msg); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	slog.Info("sent", "to", rcpts, "subject", e.Subject)
	return nil
}
