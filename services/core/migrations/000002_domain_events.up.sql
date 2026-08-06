-- SPDX-License-Identifier: AGPL-3.0-only

BEGIN;

-- domain_events is the append-only cloud source of truth for edge mutations.
-- Command-specific projections are built from this ledger only after the inbox
-- evidence and event have committed in the same transaction.
CREATE TABLE domain_events (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    operation_id uuid NOT NULL,
    outlet_id uuid NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    aggregate_version bigint NOT NULL CHECK (aggregate_version >= 0),
    command_type text NOT NULL,
    mutation jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    accepted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, operation_id),
    CONSTRAINT domain_events_inbox_fk FOREIGN KEY (tenant_id, operation_id)
        REFERENCES sync_inbox (tenant_id, operation_id),
    CONSTRAINT domain_events_outlet_fk FOREIGN KEY (tenant_id, outlet_id)
        REFERENCES outlets (tenant_id, id)
);

CREATE INDEX domain_events_aggregate_idx
    ON domain_events (tenant_id, aggregate_type, aggregate_id, accepted_at, operation_id);
CREATE INDEX domain_events_outlet_sequence_idx
    ON domain_events (tenant_id, outlet_id, accepted_at, operation_id);

CREATE TRIGGER domain_events_immutable
BEFORE UPDATE OR DELETE ON domain_events
FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

ALTER TABLE domain_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE domain_events FORCE ROW LEVEL SECURITY;
CREATE POLICY domain_events_isolation ON domain_events
    USING (tenant_id = feastcloud.current_tenant_id())
    WITH CHECK (tenant_id = feastcloud.current_tenant_id());

COMMIT;
