package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PgStore struct{ pool *pgxpool.Pool }

func NewPgStore(ctx context.Context, url string) (*PgStore, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 5
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &PgStore{pool: pool}, nil
}

func (s *PgStore) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }
func (s *PgStore) Close()                         { s.pool.Close() }

// RecordAudit is the idempotent side effect. In one transaction it claims the
// event id in processed_events (the dedupe ledger); if the id was already there
// (ON CONFLICT DO NOTHING => 0 rows), the event is a duplicate and we skip the
// insert. Otherwise we write the audit row. Both commit atomically, so a crash
// between the two can't leave a claimed-but-unaudited event.
func (s *PgStore) RecordAudit(ctx context.Context, e Event) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	ct, err := tx.Exec(ctx,
		`INSERT INTO processed_events (event_id) VALUES ($1) ON CONFLICT DO NOTHING`, e.EventID)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() == 0 {
		// Already processed — dedupe hit. Nothing to write.
		return false, tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO audit_log (event_id, account_id, event_type, occurred_at)
		 VALUES ($1, $2, $3, $4)`,
		e.EventID, e.AccountID, e.Type, e.OccurredAt()); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
