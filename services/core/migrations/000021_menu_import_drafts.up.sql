-- SPDX-License-Identifier: AGPL-3.0-only
-- A menu export is evidence and a mapping workspace, never an automatically
-- published menu. Recipes and stations remain explicit approval gates.
BEGIN;

CREATE TABLE menu_import_drafts (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    item_file_name text NOT NULL CHECK (length(btrim(item_file_name)) BETWEEN 1 AND 255),
    addon_file_name text NOT NULL DEFAULT '' CHECK (length(addon_file_name) <= 255),
    source_sha256 char(64) NOT NULL CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('staged','mapping','applied','rejected')),
    item_count integer NOT NULL CHECK (item_count BETWEEN 1 AND 500),
    category_count integer NOT NULL CHECK (category_count BETWEEN 0 AND 80),
    addon_group_count integer NOT NULL CHECK (addon_group_count BETWEEN 0 AND 80),
    variation_count integer NOT NULL CHECK (variation_count BETWEEN 0 AND 1000),
    draft jsonb NOT NULL,
    imported_at timestamptz NOT NULL,
    actor_id text NOT NULL,
    device_id text NOT NULL,
    operation_id uuid NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,outlet_id,source_sha256),
    UNIQUE (tenant_id,operation_id),
    FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE INDEX menu_import_drafts_outlet_imported_idx
    ON menu_import_drafts(tenant_id,outlet_id,imported_at DESC,id DESC);

CREATE TRIGGER menu_import_drafts_immutable
    BEFORE UPDATE OR DELETE ON menu_import_drafts
    FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

ALTER TABLE menu_import_drafts ENABLE ROW LEVEL SECURITY;
ALTER TABLE menu_import_drafts FORCE ROW LEVEL SECURITY;
CREATE POLICY menu_import_drafts_isolation ON menu_import_drafts
    USING (tenant_id=feastcloud.current_tenant_id())
    WITH CHECK (tenant_id=feastcloud.current_tenant_id());

COMMIT;
