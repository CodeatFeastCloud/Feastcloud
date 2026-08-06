-- SPDX-License-Identifier: AGPL-3.0-only
-- Direct guest orders are immutable request evidence. They are intentionally
-- separate from paid canonical orders until an approved tender handoff occurs.
BEGIN;

CREATE TABLE web_order_requests (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    qr_link_id uuid NOT NULL,
    channel_id uuid,
    menu_version_id uuid NOT NULL,
    tracking_code text NOT NULL CHECK (tracking_code ~ '^[A-Z0-9]{8,16}$'),
    guest_name text NOT NULL DEFAULT '' CHECK (length(guest_name) <= 160),
    guest_phone text NOT NULL DEFAULT '' CHECK (length(guest_phone) <= 40),
    note text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    lines jsonb NOT NULL,
    total_minor bigint NOT NULL CHECK (total_minor >= 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    payment_state text NOT NULL CHECK (payment_state IN ('pay_at_counter','payment_handoff_pending')),
    status text NOT NULL CHECK (status IN ('submitted','cancelled','expired')),
    client_request_id uuid NOT NULL,
    submitted_at timestamptz NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,tracking_code),
    UNIQUE (tenant_id,client_request_id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,qr_link_id) REFERENCES qr_ordering_links(tenant_id,id),
    FOREIGN KEY (tenant_id,channel_id) REFERENCES sales_channels(tenant_id,id),
    FOREIGN KEY (tenant_id,menu_version_id) REFERENCES menu_studio_versions(tenant_id,id)
);
CREATE INDEX web_order_requests_outlet_queue_idx ON web_order_requests(tenant_id,outlet_id,status,submitted_at DESC,id DESC);
CREATE TRIGGER web_order_requests_immutable BEFORE UPDATE OR DELETE ON web_order_requests FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

ALTER TABLE web_order_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE web_order_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY web_order_requests_isolation ON web_order_requests
    USING (tenant_id=feastcloud.current_tenant_id())
    WITH CHECK (tenant_id=feastcloud.current_tenant_id());

COMMIT;
