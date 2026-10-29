// Command api accepts transactional emails and produces them to Kafka.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/email-processor/internal/config"

	"github.com/segmentio/kafka-go"
)

func main() {
	kw := &kafka.Writer{
		Addr:         kafka.TCP(config.Brokers()...),
		Topic:        config.TopicAccepted,
		RequiredAcks: kafka.RequireAll,
	}
	defer kw.Close()

	srv := &http.Server{
		Addr: ":" + config.Port(),
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
