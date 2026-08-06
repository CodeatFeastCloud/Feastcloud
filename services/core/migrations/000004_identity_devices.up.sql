-- SPDX-License-Identifier: AGPL-3.0-only

BEGIN;

CREATE TABLE identity_devices (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    outlet_id uuid NOT NULL,
    edge_id text NOT NULL CHECK (length(btrim(edge_id)) BETWEEN 1 AND 128),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 160),
    certificate_fingerprint char(64) NOT NULL CHECK (certificate_fingerprint ~ '^[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('active','revoked')),
    enrolled_by text NOT NULL,
    enrolled_at timestamptz NOT NULL,
    revoked_by text,
    revoked_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    CONSTRAINT identity_devices_tenant_id_id_key UNIQUE (tenant_id,id),
    CONSTRAINT identity_devices_fingerprint_key UNIQUE (tenant_id,certificate_fingerprint),
    CONSTRAINT identity_devices_edge_key UNIQUE (tenant_id,edge_id),
    CONSTRAINT identity_devices_outlet_fk FOREIGN KEY (tenant_id,outlet_id) REFERENCES outlets(tenant_id,id),
    CONSTRAINT identity_devices_revocation_consistent CHECK (
      (status='active' AND revoked_by IS NULL AND revoked_at IS NULL) OR
      (status='revoked' AND revoked_by IS NOT NULL AND revoked_at IS NOT NULL)
    )
);

ALTER TABLE identity_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity_devices FORCE ROW LEVEL SECURITY;
CREATE POLICY identity_devices_isolation ON identity_devices
    USING (tenant_id=feastcloud.current_tenant_id())
    WITH CHECK (tenant_id=feastcloud.current_tenant_id());

COMMIT;
