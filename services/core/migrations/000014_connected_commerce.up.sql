-- SPDX-License-Identifier: AGPL-3.0-only
-- A single commerce surface can be published to the counter, QR/web and partners.
-- Credentials remain in the connector runtime; this schema stores references only.
BEGIN;

CREATE TABLE sales_channels (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    code text NOT NULL CHECK (length(btrim(code)) BETWEEN 1 AND 64),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    channel_type text NOT NULL CHECK (channel_type IN ('pos','qr','web','aggregator','call_center')),
    active boolean NOT NULL DEFAULT true,
    configuration jsonb NOT NULL DEFAULT '{}',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,outlet_id,code),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE TABLE channel_menu_items (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    channel_id uuid NOT NULL,
    menu_item_id uuid NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    price_minor bigint,
    availability_mode text NOT NULL DEFAULT 'inherit' CHECK (availability_mode IN ('inherit','force_available','force_unavailable')),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id,channel_id,menu_item_id),
    FOREIGN KEY (tenant_id,channel_id) REFERENCES sales_channels(tenant_id,id),
    FOREIGN KEY (tenant_id,menu_item_id) REFERENCES menu_items(tenant_id,id),
    CHECK (price_minor IS NULL OR price_minor >= 0)
);

CREATE TABLE connector_installations (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    channel_id uuid,
    provider text NOT NULL CHECK (length(btrim(provider)) BETWEEN 1 AND 80),
    manifest_version text NOT NULL CHECK (length(btrim(manifest_version)) BETWEEN 1 AND 64),
    credential_reference text NOT NULL DEFAULT '',
    capabilities jsonb NOT NULL DEFAULT '[]',
    status text NOT NULL CHECK (status IN ('draft','healthy','degraded','disabled')),
    last_health_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,outlet_id,provider),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,channel_id) REFERENCES sales_channels(tenant_id,id)
);

CREATE TABLE connector_order_inbox (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    connector_id uuid NOT NULL,
    external_order_id text NOT NULL CHECK (length(btrim(external_order_id)) BETWEEN 1 AND 160),
    payload jsonb NOT NULL,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256)=32),
    status text NOT NULL CHECK (status IN ('received','accepted','rejected','duplicate','needs_review')),
    normalized_order_id uuid,
    received_at timestamptz NOT NULL,
    resolved_at timestamptz,
    error_code text NOT NULL DEFAULT '',
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,connector_id,external_order_id),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,connector_id) REFERENCES connector_installations(tenant_id,id),
    FOREIGN KEY (tenant_id,normalized_order_id) REFERENCES orders(tenant_id,id)
);

CREATE TABLE station_capacity_limits (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    station_id uuid NOT NULL,
    max_active_tickets integer NOT NULL CHECK (max_active_tickets >= 0),
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    PRIMARY KEY (tenant_id,outlet_id,station_id),
    FOREIGN KEY (tenant_id,outlet_id,station_id) REFERENCES stations(tenant_id,outlet_id,id)
);

CREATE TABLE kitchen_print_jobs (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    ticket_id uuid NOT NULL,
    printer_route text NOT NULL CHECK (length(btrim(printer_route)) BETWEEN 1 AND 120),
    copy_type text NOT NULL CHECK (copy_type IN ('kot','expeditor','packing','receipt')),
    payload jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('queued','acknowledged','failed','cancelled')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    acknowledged_at timestamptz,
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,ticket_id) REFERENCES kitchen_tickets(tenant_id,id)
);

CREATE TABLE pickup_tokens (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    order_id uuid NOT NULL,
    token text NOT NULL CHECK (token ~ '^[A-Z0-9]{3,12}$'),
    status text NOT NULL CHECK (status IN ('issued','called','collected','cancelled')),
    issued_at timestamptz NOT NULL,
    collected_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,outlet_id,token),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,outlet_id,order_id) REFERENCES orders(tenant_id,outlet_id,id)
);

CREATE TABLE qr_ordering_links (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    channel_id uuid,
    table_id uuid,
    slug text NOT NULL CHECK (slug ~ '^[A-Za-z0-9_-]{6,96}$'),
    active boolean NOT NULL DEFAULT true,
    expires_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,slug),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,channel_id) REFERENCES sales_channels(tenant_id,id),
    FOREIGN KEY (tenant_id,table_id) REFERENCES dining_tables(tenant_id,id)
);

CREATE TABLE stock_transfers (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    source_outlet_id uuid NOT NULL,
    destination_outlet_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('requested','approved','dispatched','received','cancelled')),
    requested_by text NOT NULL,
    notes text NOT NULL DEFAULT '',
    requested_at timestamptz NOT NULL,
    dispatched_at timestamptz,
    received_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (tenant_id,id),
    CHECK (source_outlet_id <> destination_outlet_id),
    FOREIGN KEY (tenant_id,source_outlet_id) REFERENCES outlets(tenant_id,id),
    FOREIGN KEY (tenant_id,destination_outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE TABLE stock_transfer_lines (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    transfer_id uuid NOT NULL,
    ingredient_id uuid NOT NULL,
    quantity_base numeric(20,6) NOT NULL CHECK (quantity_base > 0),
    dispatched_quantity_base numeric(20,6),
    received_quantity_base numeric(20,6),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,transfer_id,ingredient_id),
    FOREIGN KEY (tenant_id,transfer_id) REFERENCES stock_transfers(tenant_id,id),
    FOREIGN KEY (tenant_id,ingredient_id) REFERENCES ingredients(tenant_id,id),
    CHECK (dispatched_quantity_base IS NULL OR dispatched_quantity_base >= 0),
    CHECK (received_quantity_base IS NULL OR received_quantity_base >= 0)
);

CREATE TABLE outlet_control_profiles (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    profile_name text NOT NULL CHECK (length(btrim(profile_name)) BETWEEN 1 AND 120),
    approval_policy jsonb NOT NULL DEFAULT '{}',
    feature_profile jsonb NOT NULL DEFAULT '{}',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id,outlet_id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE TABLE hardware_devices (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    device_type text NOT NULL CHECK (device_type IN ('printer','cash_drawer','scanner','scale','customer_display','payment_terminal')),
    manufacturer text NOT NULL,
    model text NOT NULL,
    serial_number text NOT NULL,
    certification_status text NOT NULL CHECK (certification_status IN ('candidate','certified','deprecated','blocked')),
    gateway_reference text NOT NULL DEFAULT '',
    last_seen_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,outlet_id,serial_number),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE TABLE implementation_runbooks (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    template_code text NOT NULL CHECK (length(btrim(template_code)) BETWEEN 1 AND 80),
    status text NOT NULL CHECK (status IN ('draft','in_progress','ready','blocked')),
    checklist jsonb NOT NULL DEFAULT '[]',
    owner text NOT NULL,
    due_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE INDEX connector_order_inbox_queue_idx ON connector_order_inbox(tenant_id,outlet_id,status,received_at,id);
CREATE INDEX kitchen_print_jobs_queue_idx ON kitchen_print_jobs(tenant_id,outlet_id,status,created_at,id);
CREATE INDEX stock_transfers_route_idx ON stock_transfers(tenant_id,source_outlet_id,destination_outlet_id,status,requested_at,id);
CREATE INDEX qr_ordering_links_active_idx ON qr_ordering_links(tenant_id,outlet_id,active,expires_at);

CREATE TRIGGER connector_order_inbox_immutable BEFORE UPDATE OR DELETE ON connector_order_inbox FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

DO $$ DECLARE n text; BEGIN
  FOREACH n IN ARRAY ARRAY['sales_channels','channel_menu_items','connector_installations','connector_order_inbox','station_capacity_limits','kitchen_print_jobs','pickup_tokens','qr_ordering_links','stock_transfers','stock_transfer_lines','outlet_control_profiles','hardware_devices','implementation_runbooks'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', n);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', n);
    EXECUTE format('CREATE POLICY %I ON %I USING (tenant_id=feastcloud.current_tenant_id()) WITH CHECK (tenant_id=feastcloud.current_tenant_id())', n || '_isolation', n);
  END LOOP;
END $$;

COMMIT;
