-- SPDX-License-Identifier: AGPL-3.0-only

BEGIN;

CREATE TABLE inventory_counts (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    notes text NOT NULL DEFAULT '',
    counted_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    actor_id text NOT NULL,
    device_id text NOT NULL,
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE TABLE inventory_count_lines (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    count_id uuid NOT NULL,
    ingredient_id uuid NOT NULL,
    unit_id uuid NOT NULL,
    counted_quantity numeric(20,6) NOT NULL CHECK (counted_quantity >= 0),
    counted_quantity_base numeric(20,6) NOT NULL CHECK (counted_quantity_base >= 0),
    expected_quantity_base numeric(20,6) NOT NULL,
    variance_quantity_base numeric(20,6) NOT NULL,
    variance_cost_minor bigint NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,count_id,ingredient_id),
    FOREIGN KEY (tenant_id,count_id) REFERENCES inventory_counts(tenant_id,id),
    FOREIGN KEY (tenant_id,ingredient_id) REFERENCES ingredients(tenant_id,id),
    FOREIGN KEY (tenant_id,unit_id) REFERENCES units(tenant_id,id)
);

CREATE TRIGGER inventory_counts_immutable BEFORE UPDATE OR DELETE ON inventory_counts
FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER inventory_count_lines_immutable BEFORE UPDATE OR DELETE ON inventory_count_lines
FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

ALTER TABLE inventory_counts ENABLE ROW LEVEL SECURITY;
ALTER TABLE inventory_counts FORCE ROW LEVEL SECURITY;
CREATE POLICY inventory_counts_isolation ON inventory_counts
USING (tenant_id=feastcloud.current_tenant_id()) WITH CHECK (tenant_id=feastcloud.current_tenant_id());
ALTER TABLE inventory_count_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE inventory_count_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY inventory_count_lines_isolation ON inventory_count_lines
USING (tenant_id=feastcloud.current_tenant_id()) WITH CHECK (tenant_id=feastcloud.current_tenant_id());

COMMIT;
