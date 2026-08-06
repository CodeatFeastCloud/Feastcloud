-- SPDX-License-Identifier: AGPL-3.0-only

BEGIN;

-- Organization-scoped mutations have no outlet by definition.
ALTER TABLE audit_events ALTER COLUMN outlet_id DROP NOT NULL;

COMMIT;
