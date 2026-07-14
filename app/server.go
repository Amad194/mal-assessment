package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
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
	return mux
}

// StartDraining flips readiness to failing so the pod is removed from Service
// endpoints before we stop accepting new work.
func (s *Server) StartDraining() { s.draining.Store(true) }

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
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	acct, err := s.store.GetAccount(ctx, id)
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	case err != nil:
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
		ev := map[string]any{
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
