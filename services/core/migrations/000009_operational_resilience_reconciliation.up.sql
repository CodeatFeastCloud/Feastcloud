-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;

CREATE TABLE configuration_snapshots (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), outlet_id uuid NOT NULL,
 sequence bigint NOT NULL CHECK(sequence>0), content jsonb NOT NULL, content_sha256 char(64) NOT NULL CHECK(content_sha256~'^[0-9a-f]{64}$'),
 signature text NOT NULL, public_key text NOT NULL, algorithm text NOT NULL CHECK(algorithm='Ed25519'), status text NOT NULL CHECK(status='published'),
 signed_at timestamptz NOT NULL, actor_id text NOT NULL, operation_id uuid NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,outlet_id,sequence), UNIQUE(tenant_id,operation_id),
 FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE TABLE edge_sync_checkpoints (
 tenant_id uuid NOT NULL REFERENCES tenants(id), edge_id text NOT NULL, outlet_id uuid NOT NULL,
 last_operation_id uuid, last_received_at timestamptz, last_accepted_at timestamptz,
 last_snapshot_sequence bigint NOT NULL DEFAULT 0 CHECK(last_snapshot_sequence>=0), backlog_count integer NOT NULL DEFAULT 0 CHECK(backlog_count>=0),
 degraded boolean NOT NULL DEFAULT false, last_problem_code text NOT NULL DEFAULT '', version bigint NOT NULL DEFAULT 1 CHECK(version>0), updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,edge_id), FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);

CREATE TABLE reconciliation_cases (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), outlet_id uuid NOT NULL,
 source_type text NOT NULL CHECK(source_type IN('sync','import','manual')), source_id text NOT NULL,
 category text NOT NULL, severity text NOT NULL CHECK(severity IN('low','medium','high','critical')),
 status text NOT NULL CHECK(status IN('open','in_progress','resolved','dismissed')), title text NOT NULL, details jsonb NOT NULL,
 assigned_to text NOT NULL DEFAULT '', resolution text NOT NULL DEFAULT '', version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 opened_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, resolved_at timestamptz,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,source_type,source_id), FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);
CREATE INDEX reconciliation_cases_queue_idx ON reconciliation_cases(tenant_id,outlet_id,status,severity,opened_at,id);
CREATE TABLE reconciliation_actions (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), case_id uuid NOT NULL,
 action text NOT NULL CHECK(action IN('assign','retry','resolve','dismiss','reopen')), notes text NOT NULL DEFAULT '', actor_id text NOT NULL,
 previous_status text NOT NULL, resulting_status text NOT NULL, expected_version bigint NOT NULL, occurred_at timestamptz NOT NULL, operation_id uuid NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,operation_id), FOREIGN KEY(tenant_id,case_id) REFERENCES reconciliation_cases(tenant_id,id)
);

CREATE TABLE operational_incidents (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), outlet_id uuid NOT NULL,
 incident_type text NOT NULL, severity text NOT NULL CHECK(severity IN('low','medium','high','critical')),
 status text NOT NULL CHECK(status IN('open','monitoring','resolved')), title text NOT NULL, details jsonb NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), started_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, resolved_at timestamptz,
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);
CREATE TABLE incident_events (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), incident_id uuid NOT NULL,
 event_type text NOT NULL CHECK(event_type IN('opened','note','monitoring','resolved','reopened')), message text NOT NULL,
 actor_id text NOT NULL, occurred_at timestamptz NOT NULL, operation_id uuid NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,operation_id), FOREIGN KEY(tenant_id,incident_id) REFERENCES operational_incidents(tenant_id,id)
);

CREATE TABLE backup_manifests (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), outlet_id uuid,
 backup_type text NOT NULL CHECK(backup_type IN('full','incremental','edge')), storage_reference text NOT NULL,
 content_sha256 char(64) NOT NULL CHECK(content_sha256~'^[0-9a-f]{64}$'), size_bytes bigint NOT NULL CHECK(size_bytes>=0),
 encrypted boolean NOT NULL, verified boolean NOT NULL, started_at timestamptz NOT NULL, completed_at timestamptz NOT NULL,
 recovery_point_at timestamptz NOT NULL, recorded_at timestamptz NOT NULL, actor_id text NOT NULL, operation_id uuid NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,operation_id), FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id), CHECK(completed_at>=started_at)
);
CREATE TABLE restore_drills (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), backup_manifest_id uuid NOT NULL,
 status text NOT NULL CHECK(status IN('passed','failed')), target_environment text NOT NULL,
 started_at timestamptz NOT NULL, completed_at timestamptz NOT NULL, recovery_time_seconds integer NOT NULL CHECK(recovery_time_seconds>=0),
 integrity_verified boolean NOT NULL, notes text NOT NULL DEFAULT '', recorded_at timestamptz NOT NULL, actor_id text NOT NULL, operation_id uuid NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,operation_id), FOREIGN KEY(tenant_id,backup_manifest_id) REFERENCES backup_manifests(tenant_id,id), CHECK(completed_at>=started_at)
);

CREATE TRIGGER configuration_snapshots_immutable BEFORE UPDATE OR DELETE ON configuration_snapshots FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER reconciliation_actions_immutable BEFORE UPDATE OR DELETE ON reconciliation_actions FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER incident_events_immutable BEFORE UPDATE OR DELETE ON incident_events FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER backup_manifests_immutable BEFORE UPDATE OR DELETE ON backup_manifests FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER restore_drills_immutable BEFORE UPDATE OR DELETE ON restore_drills FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

DO $$ DECLARE table_name text; BEGIN
 FOREACH table_name IN ARRAY ARRAY['configuration_snapshots','edge_sync_checkpoints','reconciliation_cases','reconciliation_actions','operational_incidents','incident_events','backup_manifests','restore_drills'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',table_name); EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',table_name);
  EXECUTE format('CREATE POLICY %I ON %I USING (tenant_id=feastcloud.current_tenant_id()) WITH CHECK (tenant_id=feastcloud.current_tenant_id())',table_name||'_isolation',table_name);
 END LOOP;
END $$;
COMMIT;
