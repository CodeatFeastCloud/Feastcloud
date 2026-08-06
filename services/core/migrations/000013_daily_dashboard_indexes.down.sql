-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;
DROP INDEX IF EXISTS promotion_redemptions_dashboard_idx;
DROP INDEX IF EXISTS fiscal_receipts_dashboard_idx;
DROP INDEX IF EXISTS tenders_dashboard_idx;
COMMIT;
