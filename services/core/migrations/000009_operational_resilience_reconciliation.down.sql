-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;
DROP TABLE IF EXISTS restore_drills; DROP TABLE IF EXISTS backup_manifests; DROP TABLE IF EXISTS incident_events; DROP TABLE IF EXISTS operational_incidents;
DROP TABLE IF EXISTS reconciliation_actions; DROP TABLE IF EXISTS reconciliation_cases; DROP TABLE IF EXISTS edge_sync_checkpoints; DROP TABLE IF EXISTS configuration_snapshots;
COMMIT;
