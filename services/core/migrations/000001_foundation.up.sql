-- SPDX-License-Identifier: AGPL-3.0-only

BEGIN;

CREATE SCHEMA IF NOT EXISTS feastcloud;

CREATE FUNCTION feastcloud.current_tenant_id()
RETURNS uuid
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
$$;

CREATE FUNCTION feastcloud.reject_immutable_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE FUNCTION feastcloud.protect_sync_inbox_evidence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION '% evidence cannot be deleted', TG_TABLE_NAME
            USING ERRCODE = '55000';
    END IF;

    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
        OR NEW.operation_id IS DISTINCT FROM OLD.operation_id
        OR NEW.edge_id IS DISTINCT FROM OLD.edge_id
        OR NEW.outlet_id IS DISTINCT FROM OLD.outlet_id
        OR NEW.batch_id IS DISTINCT FROM OLD.batch_id
        OR NEW.aggregate_type IS DISTINCT FROM OLD.aggregate_type
        OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
        OR NEW.aggregate_version IS DISTINCT FROM OLD.aggregate_version
        OR NEW.command_type IS DISTINCT FROM OLD.command_type
        OR NEW.request_hash IS DISTINCT FROM OLD.request_hash
        OR NEW.mutation IS DISTINCT FROM OLD.mutation
        OR NEW.received_at IS DISTINCT FROM OLD.received_at THEN
        RAISE EXCEPTION '% evidence fields are immutable', TG_TABLE_NAME
            USING ERRCODE = '55000';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TABLE tenants (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE organizations (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    legal_name text,
    default_locale text NOT NULL,
    default_currency varchar(3) NOT NULL CHECK (default_currency ~ '^[A-Z]{3}$'),
    active boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT organization_is_tenant CHECK (id = tenant_id),
    CONSTRAINT organizations_tenant_id_id_key UNIQUE (tenant_id, id)
);

CREATE TABLE outlets (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    organization_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    code varchar(64) NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'),
    time_zone text NOT NULL,
    currency varchar(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    active boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT outlets_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT outlets_tenant_code_key UNIQUE (tenant_id, code),
    CONSTRAINT outlets_organization_fk FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations (tenant_id, id)
);

CREATE TABLE brands (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    organization_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    code varchar(64) NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'),
    active boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT brands_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT brands_tenant_code_key UNIQUE (tenant_id, code),
    CONSTRAINT brands_organization_fk FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations (tenant_id, id)
);

CREATE TABLE stations (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    code varchar(64) NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'),
    station_type text NOT NULL CHECK (station_type IN (
        'preparation', 'cooking', 'beverage', 'assembly', 'expo', 'packing'
    )),
    active boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT stations_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT stations_tenant_outlet_id_key UNIQUE (tenant_id, outlet_id, id),
    CONSTRAINT stations_outlet_code_key UNIQUE (tenant_id, outlet_id, code),
    CONSTRAINT stations_outlet_fk FOREIGN KEY (tenant_id, outlet_id)
        REFERENCES outlets (tenant_id, id)
);

CREATE TABLE orders (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    brand_id uuid,
    external_ref text,
    source text NOT NULL,
    source_id text,
    order_type text NOT NULL CHECK (order_type IN ('dineIn', 'takeaway', 'delivery', 'roomService')),
    status text NOT NULL CHECK (status IN (
        'received', 'accepted', 'preparing', 'ready', 'completed', 'cancelled'
    )),
    currency varchar(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    subtotal_minor bigint NOT NULL CHECK (subtotal_minor >= 0),
    discount_total_minor bigint NOT NULL DEFAULT 0 CHECK (discount_total_minor >= 0),
    tax_total_minor bigint NOT NULL DEFAULT 0 CHECK (tax_total_minor >= 0),
    service_charge_minor bigint NOT NULL DEFAULT 0 CHECK (service_charge_minor >= 0),
    total_minor bigint NOT NULL CHECK (total_minor >= 0),
    placed_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT orders_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT orders_tenant_outlet_id_key UNIQUE (tenant_id, outlet_id, id),
    CONSTRAINT orders_total_equation CHECK (
        total_minor = subtotal_minor - discount_total_minor + tax_total_minor + service_charge_minor
    ),
    CONSTRAINT orders_outlet_fk FOREIGN KEY (tenant_id, outlet_id)
        REFERENCES outlets (tenant_id, id),
    CONSTRAINT orders_brand_fk FOREIGN KEY (tenant_id, brand_id)
        REFERENCES brands (tenant_id, id)
);

CREATE UNIQUE INDEX orders_source_identity_key
    ON orders (tenant_id, source, source_id)
    WHERE source_id IS NOT NULL;
CREATE INDEX orders_outlet_placed_at_idx
    ON orders (tenant_id, outlet_id, placed_at DESC, id);

CREATE TABLE order_lines (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    order_id uuid NOT NULL,
    menu_item_id uuid,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    quantity integer NOT NULL CHECK (quantity > 0),
    currency varchar(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    unit_price_minor bigint NOT NULL CHECK (unit_price_minor >= 0),
    line_total_minor bigint NOT NULL CHECK (line_total_minor >= 0),
    preparation_note text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT order_lines_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT order_lines_order_identity_key UNIQUE (tenant_id, order_id, id),
    CONSTRAINT order_lines_total_equation CHECK (line_total_minor = unit_price_minor * quantity),
    CONSTRAINT order_lines_order_fk FOREIGN KEY (tenant_id, order_id)
        REFERENCES orders (tenant_id, id) ON DELETE RESTRICT
);

CREATE TABLE kitchen_tickets (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    order_id uuid NOT NULL,
    station_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN (
        'queued', 'fired', 'preparing', 'ready', 'completed', 'cancelled'
    )),
    priority smallint NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 100),
    target_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT kitchen_tickets_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT kitchen_tickets_order_identity_key UNIQUE (tenant_id, id, order_id),
    CONSTRAINT kitchen_tickets_order_fk FOREIGN KEY (tenant_id, outlet_id, order_id)
        REFERENCES orders (tenant_id, outlet_id, id),
    CONSTRAINT kitchen_tickets_station_fk FOREIGN KEY (tenant_id, outlet_id, station_id)
        REFERENCES stations (tenant_id, outlet_id, id),
    CONSTRAINT kitchen_tickets_outlet_fk FOREIGN KEY (tenant_id, outlet_id)
        REFERENCES outlets (tenant_id, id)
);

CREATE INDEX kitchen_tickets_station_status_idx
    ON kitchen_tickets (tenant_id, outlet_id, station_id, status, priority DESC, created_at);

CREATE TABLE ticket_lines (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    ticket_id uuid NOT NULL,
    order_id uuid NOT NULL,
    order_line_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, ticket_id, order_line_id),
    CONSTRAINT ticket_lines_ticket_fk FOREIGN KEY (tenant_id, ticket_id, order_id)
        REFERENCES kitchen_tickets (tenant_id, id, order_id),
    CONSTRAINT ticket_lines_order_line_fk FOREIGN KEY (tenant_id, order_id, order_line_id)
        REFERENCES order_lines (tenant_id, order_id, id)
);

CREATE TABLE audit_events (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    operation_id uuid NOT NULL,
    outlet_id uuid NOT NULL,
    actor_id text NOT NULL,
    device_id text NOT NULL,
    source text NOT NULL,
    source_id text,
    idempotency_key text NOT NULL,
    correlation_id text,
    schema_version text NOT NULL,
    action text NOT NULL,
    entity_type text NOT NULL,
    entity_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT audit_events_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT audit_events_operation_key UNIQUE (tenant_id, operation_id)
);

CREATE INDEX audit_events_entity_idx
    ON audit_events (tenant_id, entity_type, entity_id, recorded_at, id);

CREATE TABLE idempotency_records (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    actor_id text NOT NULL,
    route text NOT NULL,
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL,
    state text NOT NULL CHECK (state IN ('in_progress', 'completed', 'failed')),
    response_status integer,
    response_headers jsonb,
    response_body bytea,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at timestamptz,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, actor_id, route, idempotency_key)
);

CREATE INDEX idempotency_records_expiry_idx ON idempotency_records (expires_at);

CREATE TABLE sync_inbox (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    operation_id uuid NOT NULL,
    edge_id text NOT NULL,
    outlet_id uuid NOT NULL,
    batch_id text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    aggregate_version bigint NOT NULL CHECK (aggregate_version >= 0),
    command_type text NOT NULL,
    request_hash bytea NOT NULL,
    mutation jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('received', 'accepted', 'rejected', 'conflict')),
    problem_code text,
    received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    processed_at timestamptz,
    PRIMARY KEY (tenant_id, operation_id),
    CONSTRAINT sync_inbox_outlet_fk FOREIGN KEY (tenant_id, outlet_id)
        REFERENCES outlets (tenant_id, id)
);

CREATE INDEX sync_inbox_processing_idx
    ON sync_inbox (tenant_id, status, received_at, operation_id);

CREATE TRIGGER audit_events_immutable
BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

CREATE TRIGGER sync_inbox_evidence_immutable
BEFORE UPDATE OR DELETE ON sync_inbox
FOR EACH ROW EXECUTE FUNCTION feastcloud.protect_sync_inbox_evidence();

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenants_isolation ON tenants
    USING (id = feastcloud.current_tenant_id())
    WITH CHECK (id = feastcloud.current_tenant_id());

ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations FORCE ROW LEVEL SECURITY;
CREATE POLICY organizations_isolation ON organizations
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

ALTER TABLE outlets ENABLE ROW LEVEL SECURITY;
ALTER TABLE outlets FORCE ROW LEVEL SECURITY;
CREATE POLICY outlets_isolation ON outlets
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

ALTER TABLE brands ENABLE ROW LEVEL SECURITY;
ALTER TABLE brands FORCE ROW LEVEL SECURITY;
CREATE POLICY brands_isolation ON brands
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

ALTER TABLE stations ENABLE ROW LEVEL SECURITY;
ALTER TABLE stations FORCE ROW LEVEL SECURITY;
CREATE POLICY stations_isolation ON stations
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

ALTER TABLE orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE orders FORCE ROW LEVEL SECURITY;
CREATE POLICY orders_isolation ON orders
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

ALTER TABLE order_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY order_lines_isolation ON order_lines
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

ALTER TABLE kitchen_tickets ENABLE ROW LEVEL SECURITY;
ALTER TABLE kitchen_tickets FORCE ROW LEVEL SECURITY;
CREATE POLICY kitchen_tickets_isolation ON kitchen_tickets
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

ALTER TABLE ticket_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE ticket_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY ticket_lines_isolation ON ticket_lines
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY audit_events_isolation ON audit_events
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

ALTER TABLE idempotency_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_records FORCE ROW LEVEL SECURITY;
CREATE POLICY idempotency_records_isolation ON idempotency_records
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

ALTER TABLE sync_inbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_inbox FORCE ROW LEVEL SECURITY;
CREATE POLICY sync_inbox_isolation ON sync_inbox
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

COMMIT;
