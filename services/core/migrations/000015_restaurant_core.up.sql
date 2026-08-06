-- SPDX-License-Identifier: AGPL-3.0-only
-- Versioned Menu Studio plus the auditable POS -> KOT -> token transaction.
BEGIN;

CREATE TABLE menu_studios (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    status text NOT NULL CHECK (status IN ('draft','published','archived')),
    current_version_id uuid,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,outlet_id,name),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE TABLE menu_studio_versions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    menu_studio_id uuid NOT NULL,
    version_number integer NOT NULL CHECK (version_number > 0),
    status text NOT NULL CHECK (status IN ('draft','published','superseded')),
    effective_from timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    published_at timestamptz,
    published_by text NOT NULL DEFAULT '',
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,menu_studio_id,version_number),
    FOREIGN KEY (tenant_id,menu_studio_id) REFERENCES menu_studios(tenant_id,id)
);
ALTER TABLE menu_studios ADD CONSTRAINT menu_studios_current_version_fk FOREIGN KEY (tenant_id,current_version_id) REFERENCES menu_studio_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE menu_studio_categories (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    menu_version_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    sort_order integer NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,menu_version_id,name),
    FOREIGN KEY (tenant_id,menu_version_id) REFERENCES menu_studio_versions(tenant_id,id)
);

CREATE TABLE menu_modifier_groups (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    menu_version_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    selection_min integer NOT NULL DEFAULT 0 CHECK (selection_min >= 0),
    selection_max integer NOT NULL DEFAULT 1 CHECK (selection_max >= selection_min AND selection_max <= 20),
    required boolean NOT NULL DEFAULT false,
    sort_order integer NOT NULL DEFAULT 0,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,menu_version_id,name),
    FOREIGN KEY (tenant_id,menu_version_id) REFERENCES menu_studio_versions(tenant_id,id),
    CHECK ((required AND selection_min >= 1) OR NOT required)
);

CREATE TABLE menu_modifier_options (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    modifier_group_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    price_delta_minor bigint NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    sort_order integer NOT NULL DEFAULT 0,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,modifier_group_id,name),
    FOREIGN KEY (tenant_id,modifier_group_id) REFERENCES menu_modifier_groups(tenant_id,id)
);

CREATE TABLE menu_version_items (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    menu_version_id uuid NOT NULL,
    menu_item_id uuid NOT NULL,
    category_id uuid,
    display_name text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 160),
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
    sort_order integer NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    PRIMARY KEY (tenant_id,menu_version_id,menu_item_id),
    FOREIGN KEY (tenant_id,menu_version_id) REFERENCES menu_studio_versions(tenant_id,id),
    FOREIGN KEY (tenant_id,menu_item_id) REFERENCES menu_items(tenant_id,id),
    FOREIGN KEY (tenant_id,category_id) REFERENCES menu_studio_categories(tenant_id,id)
);

CREATE TABLE menu_version_item_modifiers (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    menu_version_id uuid NOT NULL,
    menu_item_id uuid NOT NULL,
    modifier_group_id uuid NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id,menu_version_id,menu_item_id,modifier_group_id),
    FOREIGN KEY (tenant_id,menu_version_id,menu_item_id) REFERENCES menu_version_items(tenant_id,menu_version_id,menu_item_id),
    FOREIGN KEY (tenant_id,modifier_group_id) REFERENCES menu_modifier_groups(tenant_id,id)
);

CREATE TABLE menu_version_item_prices (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    menu_version_id uuid NOT NULL,
    menu_item_id uuid NOT NULL,
    price_minor bigint NOT NULL CHECK (price_minor >= 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    effective_from timestamptz NOT NULL,
    effective_to timestamptz,
    UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id,menu_version_id,menu_item_id) REFERENCES menu_version_items(tenant_id,menu_version_id,menu_item_id),
    CHECK (effective_to IS NULL OR effective_to > effective_from)
);
CREATE UNIQUE INDEX menu_version_item_prices_current_idx ON menu_version_item_prices(tenant_id,menu_version_id,menu_item_id) WHERE effective_to IS NULL;

CREATE TABLE menu_publications (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    menu_version_id uuid NOT NULL,
    channel_id uuid,
    status text NOT NULL CHECK (status IN ('scheduled','live','paused','retired')),
    effective_from timestamptz NOT NULL,
    effective_to timestamptz,
    published_by text NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,menu_version_id) REFERENCES menu_studio_versions(tenant_id,id),
    FOREIGN KEY (tenant_id,channel_id) REFERENCES sales_channels(tenant_id,id),
    CHECK (effective_to IS NULL OR effective_to > effective_from)
);
CREATE UNIQUE INDEX menu_publications_one_live_idx ON menu_publications(tenant_id,outlet_id,COALESCE(channel_id,'00000000-0000-0000-0000-000000000000'::uuid)) WHERE status='live' AND effective_to IS NULL;

CREATE TABLE order_line_modifier_snapshots (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    order_line_id uuid NOT NULL,
    modifier_group_id uuid NOT NULL,
    modifier_group_name text NOT NULL,
    modifier_option_id uuid NOT NULL,
    modifier_option_name text NOT NULL,
    price_delta_minor bigint NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id,order_line_id,modifier_option_id),
    FOREIGN KEY (tenant_id,order_line_id) REFERENCES order_lines(tenant_id,id)
);

CREATE TABLE pos_checkout_records (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    order_id uuid NOT NULL,
    menu_version_id uuid,
    receipt_id uuid,
    pickup_token_id uuid,
    completed_at timestamptz NOT NULL,
    actor_id text NOT NULL,
    device_id text NOT NULL,
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,operation_id),
    UNIQUE (tenant_id,order_id),
    FOREIGN KEY (tenant_id,outlet_id,order_id) REFERENCES orders(tenant_id,outlet_id,id),
    FOREIGN KEY (tenant_id,menu_version_id) REFERENCES menu_studio_versions(tenant_id,id),
    FOREIGN KEY (tenant_id,receipt_id) REFERENCES fiscal_receipts(tenant_id,id),
    FOREIGN KEY (tenant_id,pickup_token_id) REFERENCES pickup_tokens(tenant_id,id)
);

CREATE TABLE kitchen_print_job_events (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    print_job_id uuid NOT NULL,
    event_type text NOT NULL CHECK (event_type IN ('queued','acknowledged','failed','requeued','cancelled','reprinted')),
    details jsonb NOT NULL DEFAULT '{}',
    occurred_at timestamptz NOT NULL,
    actor_id text NOT NULL,
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,print_job_id) REFERENCES kitchen_print_jobs(tenant_id,id)
);

CREATE TABLE pickup_token_events (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    pickup_token_id uuid NOT NULL,
    event_type text NOT NULL CHECK (event_type IN ('issued','called','collected','cancelled')),
    occurred_at timestamptz NOT NULL,
    actor_id text NOT NULL,
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,pickup_token_id) REFERENCES pickup_tokens(tenant_id,id)
);

CREATE TRIGGER menu_studio_versions_immutable BEFORE UPDATE OR DELETE ON menu_studio_versions FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER menu_studio_categories_immutable BEFORE UPDATE OR DELETE ON menu_studio_categories FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER menu_modifier_groups_immutable BEFORE UPDATE OR DELETE ON menu_modifier_groups FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER menu_modifier_options_immutable BEFORE UPDATE OR DELETE ON menu_modifier_options FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER menu_version_items_immutable BEFORE UPDATE OR DELETE ON menu_version_items FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER menu_version_item_modifiers_immutable BEFORE UPDATE OR DELETE ON menu_version_item_modifiers FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER menu_version_item_prices_immutable BEFORE UPDATE OR DELETE ON menu_version_item_prices FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER menu_publications_immutable BEFORE UPDATE OR DELETE ON menu_publications FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER order_line_modifier_snapshots_immutable BEFORE UPDATE OR DELETE ON order_line_modifier_snapshots FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER pos_checkout_records_immutable BEFORE UPDATE OR DELETE ON pos_checkout_records FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER kitchen_print_job_events_immutable BEFORE UPDATE OR DELETE ON kitchen_print_job_events FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER pickup_token_events_immutable BEFORE UPDATE OR DELETE ON pickup_token_events FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

DO $$ DECLARE n text; BEGIN
  FOREACH n IN ARRAY ARRAY['menu_studios','menu_studio_versions','menu_studio_categories','menu_modifier_groups','menu_modifier_options','menu_version_items','menu_version_item_modifiers','menu_version_item_prices','menu_publications','order_line_modifier_snapshots','pos_checkout_records','kitchen_print_job_events','pickup_token_events'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', n);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', n);
    EXECUTE format('CREATE POLICY %I ON %I USING (tenant_id=feastcloud.current_tenant_id()) WITH CHECK (tenant_id=feastcloud.current_tenant_id())', n || '_isolation', n);
  END LOOP;
END $$;

COMMIT;
