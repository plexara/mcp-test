-- Cleanup of audit_payloads following PR #5 review.
--
-- 0002 shipped in v1.1.0 with three columns the audit middleware never
-- learned to populate (jsonrpc_id, request_method, request_path) plus a
-- missing flag for notification trimming. This migration realigns the
-- table with the current store: drops the three unused columns, adds
-- notifications_truncated.
--
-- All ALTERs use IF EXISTS / IF NOT EXISTS so re-running on a database
-- that already saw 0003 is a no-op, and on a database that skipped
-- straight to v1.1.x then upgraded mid-cycle still lands consistently.

ALTER TABLE audit_payloads DROP COLUMN IF EXISTS jsonrpc_id;
ALTER TABLE audit_payloads DROP COLUMN IF EXISTS request_method;
ALTER TABLE audit_payloads DROP COLUMN IF EXISTS request_path;

ALTER TABLE audit_payloads
    ADD COLUMN IF NOT EXISTS notifications_truncated BOOLEAN NOT NULL DEFAULT false;
