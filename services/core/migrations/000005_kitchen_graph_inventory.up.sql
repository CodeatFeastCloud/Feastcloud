-- SPDX-License-Identifier: AGPL-3.0-only

BEGIN;

CREATE TABLE units (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    symbol text NOT NULL CHECK (length(btrim(symbol)) BETWEEN 1 AND 16),
    dimension text NOT NULL CHECK (dimension IN ('mass','volume','count')),
    base_numerator bigint NOT NULL CHECK (base_numerator > 0),
    base_denominator bigint NOT NULL CHECK (base_denominator > 0),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,symbol)
);

CREATE TABLE ingredients (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    code text NOT NULL CHECK (length(btrim(code)) BETWEEN 1 AND 64),
    base_unit_id uuid NOT NULL,
    allergens text[] NOT NULL DEFAULT '{}',
    dietary_labels text[] NOT NULL DEFAULT '{}',
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,code),
    FOREIGN KEY (tenant_id,base_unit_id) REFERENCES units(tenant_id,id)
);

CREATE TABLE recipes (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    code text NOT NULL CHECK (length(btrim(code)) BETWEEN 1 AND 64),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,code)
);

CREATE TABLE recipe_versions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    recipe_id uuid NOT NULL,
    version_number bigint NOT NULL CHECK (version_number > 0),
    yield_quantity numeric(20,6) NOT NULL CHECK (yield_quantity > 0),
    yield_unit_id uuid NOT NULL,
    preparation_loss_percent numeric(7,4) NOT NULL DEFAULT 0 CHECK (preparation_loss_percent BETWEEN 0 AND 100),
    cooking_loss_percent numeric(7,4) NOT NULL DEFAULT 0 CHECK (cooking_loss_percent BETWEEN 0 AND 100),
    instructions text NOT NULL DEFAULT '',
    effective_from timestamptz NOT NULL,
    effective_to timestamptz,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,recipe_id,version_number),
    FOREIGN KEY (tenant_id,recipe_id) REFERENCES recipes(tenant_id,id),
    FOREIGN KEY (tenant_id,yield_unit_id) REFERENCES units(tenant_id,id),
    CHECK (effective_to IS NULL OR effective_to > effective_from)
);
CREATE UNIQUE INDEX recipe_versions_one_current_idx ON recipe_versions(tenant_id,recipe_id) WHERE effective_to IS NULL;

CREATE TABLE recipe_components (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    recipe_version_id uuid NOT NULL,
    ingredient_id uuid,
    child_recipe_version_id uuid,
    quantity numeric(20,6) NOT NULL CHECK (quantity > 0),
    unit_id uuid NOT NULL,
    preparation_note text NOT NULL DEFAULT '',
    UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id,recipe_version_id) REFERENCES recipe_versions(tenant_id,id),
    FOREIGN KEY (tenant_id,ingredient_id) REFERENCES ingredients(tenant_id,id),
    FOREIGN KEY (tenant_id,child_recipe_version_id) REFERENCES recipe_versions(tenant_id,id),
    FOREIGN KEY (tenant_id,unit_id) REFERENCES units(tenant_id,id),
    CHECK ((ingredient_id IS NOT NULL)::integer + (child_recipe_version_id IS NOT NULL)::integer = 1)
);

CREATE TABLE menu_items (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    brand_id uuid,
    recipe_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    code text NOT NULL CHECK (length(btrim(code)) BETWEEN 1 AND 64),
    price_minor bigint NOT NULL CHECK (price_minor >= 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    station_id uuid,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,outlet_id,code),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,brand_id) REFERENCES brands(tenant_id,id),
    FOREIGN KEY (tenant_id,recipe_id) REFERENCES recipes(tenant_id,id),
    FOREIGN KEY (tenant_id,station_id) REFERENCES stations(tenant_id,id)
);

CREATE TABLE order_line_recipe_snapshots (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    order_id uuid NOT NULL,
    order_line_id uuid NOT NULL,
    menu_item_id uuid NOT NULL,
    recipe_version_id uuid NOT NULL,
    quantity integer NOT NULL CHECK (quantity > 0),
    captured_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id,order_line_id),
    FOREIGN KEY (tenant_id,order_id) REFERENCES orders(tenant_id,id),
    FOREIGN KEY (tenant_id,menu_item_id) REFERENCES menu_items(tenant_id,id),
    FOREIGN KEY (tenant_id,recipe_version_id) REFERENCES recipe_versions(tenant_id,id)
);

CREATE TABLE inventory_events (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    ingredient_id uuid NOT NULL,
    event_type text NOT NULL CHECK (event_type IN ('receipt','consumption','waste','spoilage','count_adjustment','transfer_in','transfer_out','staff_meal','production','reversal')),
    quantity_base numeric(20,6) NOT NULL CHECK (quantity_base <> 0),
    total_cost_minor bigint NOT NULL,
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    reference_type text NOT NULL,
    reference_id uuid NOT NULL,
    lot_code text NOT NULL DEFAULT '',
    expires_at timestamptz,
    reason text NOT NULL DEFAULT '',
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    actor_id text NOT NULL,
    device_id text NOT NULL,
    operation_id uuid NOT NULL,
    reverses_event_id uuid,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,operation_id,ingredient_id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,ingredient_id) REFERENCES ingredients(tenant_id,id),
    FOREIGN KEY (tenant_id,reverses_event_id) REFERENCES inventory_events(tenant_id,id),
    CHECK ((event_type='reversal') = (reverses_event_id IS NOT NULL))
);
CREATE INDEX inventory_events_balance_idx ON inventory_events(tenant_id,outlet_id,ingredient_id,occurred_at,id);
CREATE INDEX inventory_events_expiry_idx ON inventory_events(tenant_id,outlet_id,expires_at) WHERE expires_at IS NOT NULL;
CREATE UNIQUE INDEX inventory_events_one_reversal_idx ON inventory_events(tenant_id,reverses_event_id) WHERE reverses_event_id IS NOT NULL;

CREATE TRIGGER inventory_events_immutable BEFORE UPDATE OR DELETE ON inventory_events
FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER order_line_recipe_snapshots_immutable BEFORE UPDATE OR DELETE ON order_line_recipe_snapshots
FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

DO $$
DECLARE table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY['units','ingredients','recipes','recipe_versions','recipe_components','menu_items','order_line_recipe_snapshots','inventory_events']
  LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
    EXECUTE format('CREATE POLICY %I ON %I USING (tenant_id=feastcloud.current_tenant_id()) WITH CHECK (tenant_id=feastcloud.current_tenant_id())', table_name || '_isolation', table_name);
  END LOOP;
END $$;

COMMIT;
