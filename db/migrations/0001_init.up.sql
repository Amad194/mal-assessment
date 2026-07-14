-- 0001_init.up.sql
-- Applied by a Kubernetes Job (Helm pre-install/pre-upgrade hook) running the
-- golang-migrate image against RDS. See deploy/helm/accounts/templates/migrate-job.yaml.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS accounts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT        NOT NULL,
    balance_cents BIGINT      NOT NULL DEFAULT 0,
    currency      CHAR(3)     NOT NULL DEFAULT 'GBP',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed a couple of deterministic rows so the read endpoint is demoable.
INSERT INTO accounts (id, name, balance_cents, currency) VALUES
    ('11111111-1111-1111-1111-111111111111', 'Ada Lovelace',   500000, 'GBP'),
    ('22222222-2222-2222-2222-222222222222', 'Alan Turing',    142500, 'GBP')
ON CONFLICT (id) DO NOTHING;
