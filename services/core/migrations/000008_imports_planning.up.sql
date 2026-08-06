-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;

CREATE TABLE order_imports (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    file_name text NOT NULL,
    file_sha256 char(64) NOT NULL CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
    total_rows integer NOT NULL CHECK (total_rows > 0),
    accepted_rows integer NOT NULL CHECK (accepted_rows >= 0),
    rejected_rows integer NOT NULL CHECK (rejected_rows >= 0),
    status text NOT NULL CHECK (status IN ('completed','completed_with_errors','rejected')),
    imported_at timestamptz NOT NULL,
    actor_id text NOT NULL,
    device_id text NOT NULL,
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,outlet_id,file_sha256),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    CHECK (accepted_rows + rejected_rows = total_rows)
);

CREATE TABLE order_import_rows (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    import_id uuid NOT NULL,
    row_number integer NOT NULL CHECK (row_number > 1),
    external_ref text NOT NULL,
    status text NOT NULL CHECK (status IN ('accepted','rejected')),
    error_code text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    order_id uuid,
    raw_data jsonb NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,import_id,row_number),
    FOREIGN KEY (tenant_id,import_id) REFERENCES order_imports(tenant_id,id),
    FOREIGN KEY (tenant_id,order_id) REFERENCES orders(tenant_id,id)
);

CREATE TABLE planning_runs (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    horizon_start timestamptz NOT NULL,
    horizon_end timestamptz NOT NULL,
    model_version text NOT NULL,
    status text NOT NULL DEFAULT 'observed' CHECK (status='observed'),
    evidence_from timestamptz NOT NULL,
    evidence_to timestamptz NOT NULL,
    generated_at timestamptz NOT NULL,
    actor_id text NOT NULL,
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    CHECK (horizon_end > horizon_start),
    CHECK (evidence_to > evidence_from)
);

CREATE TABLE planning_recommendations (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    run_id uuid NOT NULL,
    recommendation_type text NOT NULL CHECK (recommendation_type IN ('demand_forecast','prep_suggestion','stockout_warning')),
    menu_item_id uuid,
    recipe_version_id uuid,
    ingredient_id uuid,
    forecast_quantity numeric(20,6) NOT NULL DEFAULT 0,
    required_quantity_base numeric(20,6) NOT NULL DEFAULT 0,
    available_quantity_base numeric(20,6) NOT NULL DEFAULT 0,
    confidence numeric(5,4) NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    explanation text NOT NULL,
    evidence jsonb NOT NULL,
    UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id,run_id) REFERENCES planning_runs(tenant_id,id),
    FOREIGN KEY (tenant_id,menu_item_id) REFERENCES menu_items(tenant_id,id),
    FOREIGN KEY (tenant_id,recipe_version_id) REFERENCES recipe_versions(tenant_id,id),
    FOREIGN KEY (tenant_id,ingredient_id) REFERENCES ingredients(tenant_id,id)
);

CREATE TRIGGER order_imports_immutable BEFORE UPDATE OR DELETE ON order_imports FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER order_import_rows_immutable BEFORE UPDATE OR DELETE ON order_import_rows FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER planning_runs_immutable BEFORE UPDATE OR DELETE ON planning_runs FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER planning_recommendations_immutable BEFORE UPDATE OR DELETE ON planning_recommendations FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

DO $$ DECLARE table_name text; BEGIN
  FOREACH table_name IN ARRAY ARRAY['order_imports','order_import_rows','planning_runs','planning_recommendations'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',table_name);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',table_name);
    EXECUTE format('CREATE POLICY %I ON %I USING (tenant_id=feastcloud.current_tenant_id()) WITH CHECK (tenant_id=feastcloud.current_tenant_id())',table_name||'_isolation',table_name);
  END LOOP;
END $$;
COMMIT;
