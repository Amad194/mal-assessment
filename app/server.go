package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Server holds the handler dependencies and the drain flag.
type Server struct {
	store    Store
	pub      Publisher
	log      *slog.Logger
	draining atomic.Bool
}

func NewServer(store Store, pub Publisher, log *slog.Logger) *Server {
	return &Server{store: store, pub: pub, log: log}
}

// Routes wires the contract from the assessment brief using Go 1.22+ method
// pattern routing.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", instrument("readyz", s.handleReadyz))
	mux.Handle("GET /metrics", metricsHandler())
	mux.HandleFunc("GET /api/accounts/{id}", instrument("get_account", s.handleGetAccount))
	// Internal, in-cluster only (blocked externally by NetworkPolicy). Called by
	// the preStop hook to start failing readiness before the container stops.
	mux.HandleFunc("GET /internal/drain", s.handleDrain)
	return mux
}

// StartDraining flips readiness to failing so the pod is removed from Service
// endpoints before we stop accepting new work.
func (s *Server) StartDraining() { s.draining.Store(true) }

func (s *Server) handleDrain(w http.ResponseWriter, r *http.Request) {
	s.StartDraining()
	s.log.Info("drain requested via preStop hook; readiness now failing")
	writeJSON(w, http.StatusOK, map[string]string{"status": "draining"})
}

// handleHealthz is liveness: it only reports that the process is up. It must
// NOT depend on Postgres — a DB blip should not trigger a pod restart loop.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz is readiness: it fails while draining or while Postgres is
// unreachable, so traffic is only sent to pods that can actually serve it.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.draining.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "draining"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		dbUp.Set(0)
		s.log.Warn("readiness check failed: db unreachable", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unreachable"})
		return
	}
	dbUp.Set(1)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Child span off the otelhttp server span => a trace path HTTP -> DB.
	ctx, span := tracer().Start(r.Context(), "db.get_account")
	span.SetAttributes(attribute.String("account.id", id))
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	acct, err := s.store.GetAccount(ctx, id)
	span.End()
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	case err != nil:
		span.SetStatus(codes.Error, "db error")
		s.log.Error("get account failed", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Best-effort audit trail; never blocks or fails the read.
	s.publishAudit(acct.ID)
	writeJSON(w, http.StatusOK, acct)
}

func (s *Server) publishAudit(accountID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// event_id is the consumer's idempotency key (see the consumer service).
		ev := map[string]any{
			"event_id":   newUUID(),
			"type":       "account.read",
			"account_id": accountID,
			"ts":         time.Now().UTC().Format(time.RFC3339),
		}
		if err := s.pub.Publish(ctx, accountID, ev); err != nil {
			auditPublishFailures.Inc()
			s.log.Warn("audit publish failed", "account_id", accountID, "err", err)
		}
	}()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
