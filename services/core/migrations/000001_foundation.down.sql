-- SPDX-License-Identifier: AGPL-3.0-only

BEGIN;

DROP TABLE IF EXISTS sync_inbox;
DROP TABLE IF EXISTS idempotency_records;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS ticket_lines;
DROP TABLE IF EXISTS kitchen_tickets;
DROP TABLE IF EXISTS order_lines;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS stations;
DROP TABLE IF EXISTS brands;
DROP TABLE IF EXISTS outlets;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS tenants;

DROP FUNCTION IF EXISTS feastcloud.reject_immutable_change();
DROP FUNCTION IF EXISTS feastcloud.protect_sync_inbox_evidence();
DROP FUNCTION IF EXISTS feastcloud.current_tenant_id();
DROP SCHEMA IF EXISTS feastcloud;

COMMIT;
