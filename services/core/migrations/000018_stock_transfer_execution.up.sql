-- SPDX-License-Identifier: AGPL-3.0-only
-- Transfer state remains an operational projection. This immutable event log
-- preserves the who/when/why evidence behind every lifecycle change, while
-- inventory_events remain the financial and quantity ledger.
BEGIN;

CREATE TABLE stock_transfer_events (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    transfer_id uuid NOT NULL,
    outlet_id uuid NOT NULL,
    event_type text NOT NULL CHECK (event_type IN ('approved','dispatched','received','cancelled')),
    details jsonb NOT NULL DEFAULT '{}',
    occurred_at timestamptz NOT NULL,
    actor_id text NOT NULL,
    device_id text NOT NULL,
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,transfer_id) REFERENCES stock_transfers(tenant_id,id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE INDEX stock_transfer_events_timeline_idx
    ON stock_transfer_events(tenant_id,transfer_id,occurred_at,id);

CREATE TRIGGER stock_transfer_events_immutable
    BEFORE UPDATE OR DELETE ON stock_transfer_events
    FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

ALTER TABLE stock_transfer_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE stock_transfer_events FORCE ROW LEVEL SECURITY;
CREATE POLICY stock_transfer_events_isolation ON stock_transfer_events
    USING (tenant_id=feastcloud.current_tenant_id())
    WITH CHECK (tenant_id=feastcloud.current_tenant_id());

COMMIT;
