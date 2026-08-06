-- SPDX-License-Identifier: AGPL-3.0-only

BEGIN;

DELETE FROM audit_events WHERE outlet_id IS NULL;
ALTER TABLE audit_events ALTER COLUMN outlet_id SET NOT NULL;

COMMIT;
