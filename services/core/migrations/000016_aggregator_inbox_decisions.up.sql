-- SPDX-License-Identifier: AGPL-3.0-only
-- Immutable operator decisions complete the connector inbox contract without
-- mutating the original provider payload or its evidence hash.
BEGIN;

CREATE TABLE connector_order_decisions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    inbox_id uuid NOT NULL,
    decision text NOT NULL CHECK (decision IN ('accepted','rejected','needs_review','duplicate')),
    reason text NOT NULL DEFAULT '' CHECK (length(reason) <= 500),
    normalized_order_id uuid,
    occurred_at timestamptz NOT NULL,
    actor_id text NOT NULL,
    device_id text NOT NULL,
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,inbox_id) REFERENCES connector_order_inbox(tenant_id,id),
    FOREIGN KEY (tenant_id,normalized_order_id) REFERENCES orders(tenant_id,id),
    CHECK ((decision <> 'accepted') OR normalized_order_id IS NOT NULL)
);
CREATE INDEX connector_order_decisions_current_idx ON connector_order_decisions(tenant_id,inbox_id,occurred_at DESC,id DESC);
CREATE TRIGGER connector_order_decisions_immutable BEFORE UPDATE OR DELETE ON connector_order_decisions FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

ALTER TABLE connector_order_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_order_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_order_decisions_isolation ON connector_order_decisions
    USING (tenant_id=feastcloud.current_tenant_id())
    WITH CHECK (tenant_id=feastcloud.current_tenant_id());

COMMIT;
