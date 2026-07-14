package main

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Account is one row of the accounts table.
type Account struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	BalanceCents int64     `json:"balance_cents"`
	Currency     string    `json:"currency"`
	CreatedAt    time.Time `json:"created_at"`
}

// ErrNotFound is returned when no account matches the requested id.
var ErrNotFound = errors.New("account not found")

// Store is the persistence port. Kept as an interface so handlers can be
// tested without a live Postgres (see main_test.go).
type Store interface {
	Ping(ctx context.Context) error
	GetAccount(ctx context.Context, id string) (Account, error)
	Close()
}

// PgStore is the Postgres-backed Store, using a pgx connection pool.
type PgStore struct{ pool *pgxpool.Pool }

func NewPgStore(ctx context.Context, url string) (*PgStore, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	// Conservative pool sizing: a bank cares far more about not exhausting
	// RDS connection slots across many replicas than about raw throughput.
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &PgStore{pool: pool}, nil
}

func (s *PgStore) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *PgStore) Close() { s.pool.Close() }

func (s *PgStore) GetAccount(ctx context.Context, id string) (Account, error) {
	var a Account
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, balance_cents, currency, created_at
		   FROM accounts
		  WHERE id = $1`, id).
		Scan(&a.ID, &a.Name, &a.BalanceCents, &a.Currency, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}
