// Command accounts-consumer consumes audit events from Kafka (accounts.audit)
// and records them in Postgres idempotently, dead-lettering poison messages.
// Delivery is at-least-once; the processed_events dedupe ledger makes the side
// effect exactly-once. See processor.go / store.go for the semantics.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/kafka-go"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	brokers := splitCSV(os.Getenv("KAFKA_BROKERS"))
	if len(brokers) == 0 {
		log.Error("KAFKA_BROKERS is required")
		os.Exit(1)
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	topic := getenv("KAFKA_TOPIC", "accounts.audit")
	dlqTopic := getenv("KAFKA_DLQ_TOPIC", "accounts.audit.dlq")
	group := getenv("KAFKA_GROUP_ID", "accounts-audit-consumer")
	port := getenv("PORT", "8080")
	maxRetries, _ := strconv.Atoi(getenv("MAX_RETRIES", "5"))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	store, err := NewPgStore(ctx, dbURL)
	if err != nil {
		log.Error("db init failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	dlq := NewKafkaDLQ(brokers, dlqTopic)
	defer dlq.Close()

	proc := NewProcessor(store, dlq, maxRetries, log)

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		GroupID:     group, // consumer group => partition balancing + committed offsets
		Topic:       topic,
		MinBytes:    1,
		MaxBytes:    10e6,
		StartOffset: kafka.FirstOffset,
	})
	defer reader.Close()

	go serveHealth(port, store, log)

	log.Info("consumer started", "topic", topic, "group", group, "dlq", dlqTopic)
	for {
		m, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break // shutting down
			}
			log.Error("fetch failed", "err", err)
			time.Sleep(time.Second)
			continue
		}
		if err := proc.Handle(ctx, m.Key, m.Value); err != nil {
			// Transient (even the DLQ write failed): do NOT commit -> redeliver.
			log.Error("processing failed; offset not committed", "err", err)
			time.Sleep(time.Second)
			continue
		}
		if err := reader.CommitMessages(context.Background(), m); err != nil {
			log.Error("commit failed", "err", err)
		}
	}
	log.Info("consumer stopped")
}

// serveHealth exposes probes + Prometheus metrics for the (non-HTTP) consumer.
func serveHealth(port string, store *PgStore, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := store.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.Handler())
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("health server error", "err", err)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
