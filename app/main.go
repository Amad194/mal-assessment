// Command accounts-api is a trivial HTTP service backed by Postgres, built to
// exercise the surrounding bank platform (K8s, CI/CD, observability, security).
//
// It has three run modes so the distroless image (no shell) can still support a
// container HEALTHCHECK and a Kubernetes preStop hook:
//
//	accounts-api             # run the HTTP server (default)
//	accounts-api -health     # probe local /healthz, exit 0/1 (Docker HEALTHCHECK)
//	accounts-api -presleep   # flip readiness to failing, then sleep (preStop hook)
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	healthMode := flag.Bool("health", false, "probe local /healthz and exit (container HEALTHCHECK)")
	preStop := flag.Bool("presleep", false, "signal drain then sleep SHUTDOWN_DELAY (K8s preStop hook)")
	flag.Parse()

	if *healthMode {
		os.Exit(runHealthCheck())
	}
	if *preStop {
		os.Exit(runPreStop())
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := LoadConfig()
	if err != nil {
		log.Error("configuration error", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()
	shutdownTracer := initTracer(ctx, log)
	defer func() { _ = shutdownTracer(context.Background()) }()

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
		Handler:           otelhttp.NewHandler(srv.Routes(), "accounts-api"),
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
	// The preStop hook (see runPreStop) has already flipped readiness to failing
	// and slept, so by the time SIGTERM arrives the pod is out of Service
	// endpoints. Here we drain in-flight requests within ShutdownTimeout.
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

// runHealthCheck backs the Docker HEALTHCHECK: distroless has no curl, so the
// binary probes itself.
func runHealthCheck() int {
	port := getenv("PORT", "8080")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// runPreStop backs the Kubernetes preStop hook. It flips readiness to failing
// (via the local /internal/drain endpoint) BEFORE the container is signalled,
// so kube-proxy/the LB stop routing while the pod can still serve in-flight
// work, then sleeps to let endpoint removal propagate. This is what makes a
// rolling deploy zero-downtime.
func runPreStop() int {
	port := getenv("PORT", "8080")
	delay := getdur("SHUTDOWN_DELAY", 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/internal/drain", nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
	}
	time.Sleep(delay)
	return 0
}
