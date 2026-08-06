-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;

CREATE TABLE production_batches (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    station_id uuid,
    recipe_version_id uuid NOT NULL,
    output_ingredient_id uuid NOT NULL,
    output_unit_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('planned','in_progress','completed','cancelled')),
    planned_quantity numeric(20,6) NOT NULL CHECK (planned_quantity > 0),
    actual_quantity numeric(20,6),
    planned_for timestamptz NOT NULL,
    started_at timestamptz,
    completed_at timestamptz,
    expires_at timestamptz,
    lot_code text NOT NULL DEFAULT '',
    notes text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,outlet_id,station_id) REFERENCES stations(tenant_id,outlet_id,id),
    FOREIGN KEY (tenant_id,recipe_version_id) REFERENCES recipe_versions(tenant_id,id),
    FOREIGN KEY (tenant_id,output_ingredient_id) REFERENCES ingredients(tenant_id,id),
    FOREIGN KEY (tenant_id,output_unit_id) REFERENCES units(tenant_id,id),
    CHECK (actual_quantity IS NULL OR actual_quantity > 0),
    CHECK ((status='completed') = (completed_at IS NOT NULL)),
    CHECK (expires_at IS NULL OR completed_at IS NULL OR expires_at > completed_at)
);
CREATE INDEX production_batches_queue_idx ON production_batches(tenant_id,outlet_id,status,planned_for,id);

ALTER TABLE production_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE production_batches FORCE ROW LEVEL SECURITY;
CREATE POLICY production_batches_isolation ON production_batches
USING (tenant_id=feastcloud.current_tenant_id()) WITH CHECK (tenant_id=feastcloud.current_tenant_id());

COMMIT;
