-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;

-- Brands are organisation-wide identities. Their availability is deliberately
-- modelled separately from catalog records so a virtual brand can be rolled
-- out, paused, and audited outlet by outlet.
CREATE TABLE brand_outlet_assignments (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    brand_id uuid NOT NULL,
    outlet_id uuid NOT NULL,
    active boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, brand_id, outlet_id),
    FOREIGN KEY (tenant_id, brand_id) REFERENCES brands(tenant_id, id),
    FOREIGN KEY (tenant_id, outlet_id) REFERENCES outlets(tenant_id, id)
);

CREATE INDEX brand_outlet_assignments_outlet_idx
    ON brand_outlet_assignments(tenant_id, outlet_id, active);

ALTER TABLE brand_outlet_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE brand_outlet_assignments FORCE ROW LEVEL SECURITY;
CREATE POLICY brand_outlet_assignments_isolation ON brand_outlet_assignments
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

COMMIT;
