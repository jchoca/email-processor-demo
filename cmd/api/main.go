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

	"example.com/email-processor/internal/activity"
	"example.com/email-processor/internal/config"
	"example.com/email-processor/internal/stats"

	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	kafkaClient, err := kgo.NewClient(
		kgo.SeedBrokers(config.Brokers()...),
		kgo.DefaultProduceTopic(config.TopicAccepted),
		// records retry forever by default; give up so the API can answer 500
		kgo.RecordDeliveryTimeout(10*time.Second),
	)
	if err != nil {
		slog.Error("kafka", "err", err)
		os.Exit(1)
	}
	defer kafkaClient.Close()

	clickhouse := stats.Client{
		URL:      config.ClickHouseURL(),
		User:     config.ClickHouseUser(),
		Password: config.ClickHousePassword(),
		Database: config.ClickHouseDatabase(),
	}
	opensearch := activity.Client{URL: config.OpenSearchURL()}

	srv := &http.Server{
		Addr: ":" + config.Port(),
		Handler: routes(func(ctx context.Context, value []byte) error {
			return kafkaClient.ProduceSync(ctx, &kgo.Record{Value: value}).FirstErr()
		}, clickhouse.Hours, opensearch.Search, config.SenderDomains()),
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
