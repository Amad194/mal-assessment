-- roles.sql — least-privilege runtime database roles.
--
-- Run ONCE by an admin/owner (the RDS master user or a dedicated `accounts_migrator`
-- role that owns the tables and runs migrations). The passwords are injected from
-- AWS Secrets Manager at provisioning time; never hard-coded. In production these
-- roles use RDS IAM auth or Secrets-Manager-sourced credentials (see CREDENTIALS.md).
--
-- Design: DDL/ownership lives with the migrator; the two runtime roles get the
-- minimum grants their code path needs and nothing else.

-- Remove the permissive PUBLIC defaults first.
REVOKE ALL ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;

-- ---------------------------------------------------------------------------
-- accounts_api — the read service. Reads one row from `accounts`.
-- ---------------------------------------------------------------------------
CREATE ROLE accounts_api LOGIN PASSWORD :'api_password';
GRANT CONNECT ON DATABASE accounts TO accounts_api;
GRANT USAGE  ON SCHEMA public       TO accounts_api;
GRANT SELECT ON accounts            TO accounts_api;
-- Deliberately CANNOT: INSERT/UPDATE/DELETE anything, run DDL, read audit_log or
-- processed_events, or own any object. A read-path RCE cannot mutate or exfiltrate
-- the audit trail, only read the accounts it already serves.

-- ---------------------------------------------------------------------------
-- accounts_consumer — the Kafka consumer. Appends idempotent audit records.
-- ---------------------------------------------------------------------------
CREATE ROLE accounts_consumer LOGIN PASSWORD :'consumer_password';
GRANT CONNECT ON DATABASE accounts   TO accounts_consumer;
GRANT USAGE  ON SCHEMA public        TO accounts_consumer;
GRANT SELECT, INSERT ON audit_log         TO accounts_consumer;
GRANT SELECT, INSERT ON processed_events  TO accounts_consumer;
GRANT USAGE, SELECT ON SEQUENCE audit_log_id_seq TO accounts_consumer;
-- Deliberately CANNOT: UPDATE or DELETE (audit + dedupe ledger are immutable /
-- append-only, so a compromised consumer cannot rewrite or erase history), touch
-- the `accounts` table (no customer PII), run DDL, or own any object.
