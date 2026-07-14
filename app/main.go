// Command accounts-api is a trivial HTTP service backed by Postgres, built to
// exercise the surrounding bank platform (K8s, CI/CD, observability, security).
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
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := LoadConfig()
	if err != nil {
		log.Error("configuration error", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()
	store, err := NewPgStore(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database init failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	var pub Publisher = NoopPublisher{}
	if len(cfg.KafkaBrokers) > 0 {
		pub = NewKafkaPublisher(cfg.KafkaBrokers, cfg.KafkaTopic)
		log.Info("kafka publisher enabled", "brokers", cfg.KafkaBrokers, "topic", cfg.KafkaTopic)
	} else {
		log.Info("kafka disabled (KAFKA_BROKERS unset); using noop publisher")
	}
	defer pub.Close()

	srv := NewServer(store, pub, log)
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("http server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()

	// --- Graceful shutdown ---------------------------------------------------
	// 1. Receive SIGTERM (kubelet sends it, then waits terminationGracePeriod).
	// 2. Flip readiness to failing so the pod leaves Service endpoints.
	// 3. Sleep ShutdownDelay to let kube-proxy/ELB converge (avoid 502s).
	// 4. Drain in-flight requests within ShutdownTimeout, then exit.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Info("shutdown signal received; draining")
	srv.StartDraining()
	time.Sleep(cfg.ShutdownDelay)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown timed out", "err", err)
	}
	log.Info("shutdown complete")
}
