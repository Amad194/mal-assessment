package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeStore lets us exercise the handlers without a live Postgres.
type fakeStore struct {
	pingErr error
	acct    Account
	getErr  error
}

func (f fakeStore) Ping(context.Context) error                        { return f.pingErr }
func (f fakeStore) GetAccount(context.Context, string) (Account, error) { return f.acct, f.getErr }
func (f fakeStore) Close()                                            {}

func newTestServer(s Store) *Server {
	return NewServer(s, NoopPublisher{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func do(t *testing.T, srv *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	return rr
}

func TestHealthzAlwaysOK(t *testing.T) {
	srv := newTestServer(fakeStore{pingErr: errors.New("db down")})
	if rr := do(t, srv, http.MethodGet, "/healthz"); rr.Code != http.StatusOK {
		t.Fatalf("healthz should be 200 even when DB is down, got %d", rr.Code)
	}
}

func TestReadyzOK(t *testing.T) {
	srv := newTestServer(fakeStore{})
	if rr := do(t, srv, http.MethodGet, "/readyz"); rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
}

func TestReadyzDBDown(t *testing.T) {
	srv := newTestServer(fakeStore{pingErr: errors.New("connection refused")})
	if rr := do(t, srv, http.MethodGet, "/readyz"); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when DB unreachable, got %d", rr.Code)
	}
}

func TestReadyzDraining(t *testing.T) {
	srv := newTestServer(fakeStore{})
	srv.StartDraining()
	if rr := do(t, srv, http.MethodGet, "/readyz"); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 while draining, got %d", rr.Code)
	}
}

func TestGetAccountOK(t *testing.T) {
	want := Account{ID: "abc", Name: "Ada Lovelace", BalanceCents: 12345, Currency: "GBP", CreatedAt: time.Unix(0, 0).UTC()}
	srv := newTestServer(fakeStore{acct: want})
	rr := do(t, srv, http.MethodGet, "/api/accounts/abc")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var got Account
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if got.ID != want.ID || got.BalanceCents != want.BalanceCents {
		t.Fatalf("unexpected body: %+v", got)
	}
}

func TestGetAccountNotFound(t *testing.T) {
	srv := newTestServer(fakeStore{getErr: ErrNotFound})
	if rr := do(t, srv, http.MethodGet, "/api/accounts/missing"); rr.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rr.Code)
	}
}
