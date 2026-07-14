package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// AuditStore is the persistence port (kept an interface so the delivery-semantics
// tests run without a real Postgres).
type AuditStore interface {
	// RecordAudit performs the side effect idempotently, keyed by e.EventID.
	// applied is false when the event was already processed (a dedupe hit) — the
	// caller treats that as success, so redelivery causes the effect exactly once.
	RecordAudit(ctx context.Context, e Event) (applied bool, err error)
	Ping(ctx context.Context) error
	Close()
}

// DeadLetter routes messages that must not block the stream.
type DeadLetter interface {
	Publish(ctx context.Context, key, value []byte, reason string) error
}

// Processor applies the delivery semantics: idempotency, bounded retry for
// transient faults, and dead-lettering for poison / persistently-failing messages.
type Processor struct {
	store      AuditStore
	dlq        DeadLetter
	maxRetries int
	log        *slog.Logger
}

func NewProcessor(store AuditStore, dlq DeadLetter, maxRetries int, log *slog.Logger) *Processor {
	return &Processor{store: store, dlq: dlq, maxRetries: maxRetries, log: log}
}

// Handle processes one message. It returns nil when the offset MAY be committed
// (the effect happened, was a dedupe no-op, or the message was parked to the DLQ),
// and a non-nil error only when the message must be redelivered (do not commit) —
// currently just when the DLQ write itself fails, so we never silently drop an
// audit event.
func (p *Processor) Handle(ctx context.Context, key, value []byte) error {
	start := time.Now()
	defer func() { processingDuration.Observe(time.Since(start).Seconds()) }()

	var e Event
	if err := json.Unmarshal(value, &e); err != nil {
		// Unparseable => poison. Never retry; park immediately.
		return p.park(ctx, key, value, "malformed json: "+err.Error())
	}
	if !e.valid() {
		return p.park(ctx, key, value, "missing/invalid required fields")
	}

	// Bounded retry for TRANSIENT faults (e.g. Postgres briefly unreachable).
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		applied, err := p.store.RecordAudit(ctx, e)
		if err == nil {
			if applied {
				messagesProcessed.Inc()
				p.log.Info("audit recorded", "event_id", e.EventID, "account_id", e.AccountID)
			} else {
				messagesDeduped.Inc()
				p.log.Info("duplicate event ignored", "event_id", e.EventID)
			}
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff(attempt)):
		}
	}

	// Retries exhausted: park so one message can't block the partition
	// (head-of-line blocking). DLQ items are replayable once the root cause is fixed.
	p.log.Error("max retries exceeded; dead-lettering", "event_id", e.EventID, "err", lastErr)
	return p.park(ctx, key, value, "max retries exceeded: "+lastErr.Error())
}

func (p *Processor) park(ctx context.Context, key, value []byte, reason string) error {
	if err := p.dlq.Publish(ctx, key, value, reason); err != nil {
		// If even the DLQ write fails, force redelivery rather than lose the event.
		p.log.Error("dead-letter publish failed; will redeliver", "err", err)
		return err
	}
	messagesDeadLettered.Inc()
	return nil
}

// backoff is exponential with a cap: 100ms, 200ms, 400ms, ... max 5s.
func backoff(attempt int) time.Duration {
	d := 100 * time.Millisecond << attempt
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
