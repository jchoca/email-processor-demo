// Command worker consumes accepted messages and writes them to stdout.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"example.com/email-processor/internal/config"

	"github.com/segmentio/kafka-go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: config.Brokers(),
		Topic:   config.TopicAccepted,
		GroupID: "worker",
	})
	defer r.Close()

	slog.Info("consuming", "topic", config.TopicAccepted)
	for {
		m, err := r.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("read", "err", err)
			continue
		}
		if _, err := fmt.Fprintln(os.Stdout, string(m.Value)); err != nil {
			slog.Error("stdout", "err", err)
			return
		}
	}
}
