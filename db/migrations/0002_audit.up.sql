-- 0002_audit.up.sql
-- Tables the messaging consumer writes to. Run by the migrator/owner role, NOT
-- by the runtime app roles (which have no DDL rights).

-- Append-only audit trail (the consumer's side effect).
CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    event_id    UUID        NOT NULL,
    account_id  UUID        NOT NULL,
    event_type  TEXT        NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Dedupe ledger: the consumer's idempotency key store. A UNIQUE/PK on event_id
-- makes "insert the event id" the atomic guard that turns at-least-once
-- delivery into exactly-once side effects.
CREATE TABLE IF NOT EXISTS processed_events (
    event_id     UUID PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
