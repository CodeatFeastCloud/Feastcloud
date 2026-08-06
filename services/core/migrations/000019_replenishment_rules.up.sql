-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;

CREATE TABLE replenishment_rules (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    ingredient_id uuid NOT NULL,
    source_outlet_id uuid NOT NULL,
    reorder_point_base numeric(20,6) NOT NULL CHECK (reorder_point_base >= 0),
    target_level_base numeric(20,6) NOT NULL CHECK (target_level_base > reorder_point_base),
    active boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id,outlet_id,ingredient_id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,source_outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,ingredient_id) REFERENCES ingredients(tenant_id,id),
    CHECK (outlet_id <> source_outlet_id)
);

CREATE INDEX replenishment_rules_source_idx
    ON replenishment_rules(tenant_id,source_outlet_id,active);

ALTER TABLE replenishment_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_rules FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_rules_isolation ON replenishment_rules
    USING (tenant_id=feastcloud.current_tenant_id())
    WITH CHECK (tenant_id=feastcloud.current_tenant_id());

COMMIT;
