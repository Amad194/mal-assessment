-- Chart-local copy of db/migrations (canonical source is /db/migrations).
-- CI syncs these before packaging: `cp db/migrations/*.up.sql deploy/helm/accounts/migrations/`.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS accounts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT        NOT NULL,
    balance_cents BIGINT      NOT NULL DEFAULT 0,
    currency      CHAR(3)     NOT NULL DEFAULT 'GBP',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO accounts (id, name, balance_cents, currency) VALUES
    ('11111111-1111-1111-1111-111111111111', 'Ada Lovelace',   500000, 'GBP'),
    ('22222222-2222-2222-2222-222222222222', 'Alan Turing',    142500, 'GBP')
ON CONFLICT (id) DO NOTHING;
