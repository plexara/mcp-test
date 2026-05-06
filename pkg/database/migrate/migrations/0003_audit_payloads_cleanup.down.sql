-- Reverse of 0003_audit_payloads_cleanup.up.sql.
--
-- Down migration is best-effort: the historical 0002 columns we drop
-- here can be re-created, but their original data is gone the moment
-- the .up ran, so this is purely structural restore.

ALTER TABLE audit_payloads DROP COLUMN IF EXISTS notifications_truncated;

ALTER TABLE audit_payloads ADD COLUMN IF NOT EXISTS jsonrpc_id     TEXT;
ALTER TABLE audit_payloads ADD COLUMN IF NOT EXISTS request_method TEXT;
ALTER TABLE audit_payloads ADD COLUMN IF NOT EXISTS request_path   TEXT;
