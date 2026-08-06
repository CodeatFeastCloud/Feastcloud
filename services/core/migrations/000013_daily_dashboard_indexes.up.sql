-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;

CREATE INDEX tenders_dashboard_idx
    ON tenders (tenant_id, outlet_id, occurred_at, id)
    INCLUDE (status, tender_type, amount_minor, currency, order_id);

CREATE INDEX fiscal_receipts_dashboard_idx
    ON fiscal_receipts (tenant_id, outlet_id, issued_at, id)
    INCLUDE (subtotal_minor, discount_minor, tax_minor, service_charge_minor, total_minor, currency, order_id);

CREATE INDEX promotion_redemptions_dashboard_idx
    ON promotion_redemptions (tenant_id, outlet_id, occurred_at, id)
    INCLUDE (discount_minor, promotion_id, order_id);

COMMIT;
