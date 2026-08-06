// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"context"
	"encoding/json"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (repository *PostgresRepository) PublishSnapshot(ctx context.Context, v domain.ConfigurationSnapshot, a domain.AuditEvent) (domain.ConfigurationSnapshot, error) {
	err := repository.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2, 0))`, v.TenantID, v.OutletID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM configuration_snapshots WHERE tenant_id=$1 AND outlet_id=$2`, v.TenantID, v.OutletID).Scan(&v.Sequence); err != nil {
			return err
		}
		raw, err := json.Marshal(v.Content)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO configuration_snapshots(id,tenant_id,outlet_id,sequence,content,content_sha256,signature,public_key,algorithm,status,signed_at,actor_id,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'Ed25519','published',$9,$10,$11)`, v.ID, v.TenantID, v.OutletID, v.Sequence, raw, v.ContentSHA256, v.Signature, v.PublicKey, v.SignedAt, a.ActorID, a.OperationID)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
	return v, err
}
func (repository *PostgresRepository) Snapshots(ctx context.Context, tenant, outlet string) ([]domain.ConfigurationSnapshot, error) {
	values := []domain.ConfigurationSnapshot{}
	err := repository.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,sequence,content,content_sha256,signature,public_key,algorithm,status,signed_at FROM configuration_snapshots WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY sequence DESC LIMIT 50`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.ConfigurationSnapshot
			var raw []byte
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.Sequence, &raw, &v.ContentSHA256, &v.Signature, &v.PublicKey, &v.Algorithm, &v.Status, &v.SignedAt); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &v.Content); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func (repository *PostgresRepository) EdgeCheckpoints(ctx context.Context, tenant, outlet string) ([]domain.EdgeCheckpoint, error) {
	values := []domain.EdgeCheckpoint{}
	err := repository.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,edge_id,outlet_id,COALESCE(last_operation_id::text,''),last_received_at,last_accepted_at,last_snapshot_sequence,backlog_count,degraded,last_problem_code,version,updated_at FROM edge_sync_checkpoints WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY edge_id`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.EdgeCheckpoint
			if err := rows.Scan(&v.TenantID, &v.EdgeID, &v.OutletID, &v.LastOperationID, &v.LastReceivedAt, &v.LastAcceptedAt, &v.LastSnapshotSequence, &v.BacklogCount, &v.Degraded, &v.LastProblemCode, &v.Version, &v.UpdatedAt); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func loadCaseActions(ctx context.Context, tx pgx.Tx, v *domain.ReconciliationCase) error {
	rows, err := tx.Query(ctx, `SELECT id,action,notes,actor_id,previous_status,resulting_status,expected_version,occurred_at FROM reconciliation_actions WHERE tenant_id=$1 AND case_id=$2 ORDER BY occurred_at,id`, v.TenantID, v.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a domain.ReconciliationAction
		if err := rows.Scan(&a.ID, &a.Action, &a.Notes, &a.ActorID, &a.PreviousStatus, &a.ResultingStatus, &a.ExpectedVersion, &a.OccurredAt); err != nil {
			return err
		}
		v.Actions = append(v.Actions, a)
	}
	return rows.Err()
}
func (repository *PostgresRepository) ReconciliationCases(ctx context.Context, tenant, outlet string) ([]domain.ReconciliationCase, error) {
	values := []domain.ReconciliationCase{}
	err := repository.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,source_type,source_id,category,severity,status,title,details,assigned_to,resolution,version,opened_at,updated_at,resolved_at FROM reconciliation_cases WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'in_progress' THEN 1 ELSE 2 END,opened_at DESC,id LIMIT 200`, tenant, outlet)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v domain.ReconciliationCase
			var raw []byte
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.SourceType, &v.SourceID, &v.Category, &v.Severity, &v.Status, &v.Title, &raw, &v.AssignedTo, &v.Resolution, &v.Version, &v.OpenedAt, &v.UpdatedAt, &v.ResolvedAt); err != nil {
				rows.Close()
				return err
			}
			if err := json.Unmarshal(raw, &v.Details); err != nil {
				rows.Close()
				return err
			}
			values = append(values, v)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for i := range values {
			if err := loadCaseActions(ctx, tx, &values[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return values, err
}
func (repository *PostgresRepository) ActOnReconciliationCase(ctx context.Context, tenant, outlet, id string, a domain.ReconciliationAction, assignee string, audit domain.AuditEvent) (domain.ReconciliationCase, error) {
	var v domain.ReconciliationCase
	err := repository.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,source_type,source_id,category,severity,status,title,details,assigned_to,resolution,version,opened_at,updated_at,resolved_at FROM reconciliation_cases WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, tenant, outlet, id).Scan(&v.ID, &v.TenantID, &v.OutletID, &v.SourceType, &v.SourceID, &v.Category, &v.Severity, &v.Status, &v.Title, &raw, &v.AssignedTo, &v.Resolution, &v.Version, &v.OpenedAt, &v.UpdatedAt, &v.ResolvedAt); err != nil {
			return err
		}
		if v.Version != a.ExpectedVersion {
			return ErrVersionConflict
		}
		if err := json.Unmarshal(raw, &v.Details); err != nil {
			return err
		}
		a.PreviousStatus = v.Status
		switch a.Action {
		case "assign":
			if v.Status != "open" && v.Status != "in_progress" {
				return ErrInvalidTransition
			}
			v.Status = "in_progress"
			v.AssignedTo = assignee
		case "retry":
			if v.Status != "open" && v.Status != "in_progress" {
				return ErrInvalidTransition
			}
			v.Status = "open"
			v.ResolvedAt = nil
		case "reopen":
			if v.Status != "resolved" && v.Status != "dismissed" {
				return ErrInvalidTransition
			}
			v.Status = "open"
			v.ResolvedAt = nil
		case "resolve":
			if v.Status != "open" && v.Status != "in_progress" {
				return ErrInvalidTransition
			}
			v.Status = "resolved"
			v.Resolution = a.Notes
			now := audit.RecordedAt
			v.ResolvedAt = &now
		case "dismiss":
			if v.Status != "open" && v.Status != "in_progress" {
				return ErrInvalidTransition
			}
			v.Status = "dismissed"
			v.Resolution = a.Notes
			now := audit.RecordedAt
			v.ResolvedAt = &now
		default:
			return ErrInvalidTransition
		}
		a.ResultingStatus = v.Status
		a.ActorID = audit.ActorID
		a.OccurredAt = audit.RecordedAt
		tag, err := tx.Exec(ctx, `UPDATE reconciliation_cases SET status=$4,assigned_to=$5,resolution=$6,resolved_at=$7,version=version+1,updated_at=$8 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND version=$9`, tenant, outlet, id, v.Status, v.AssignedTo, v.Resolution, v.ResolvedAt, audit.RecordedAt, a.ExpectedVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrVersionConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO reconciliation_actions(id,tenant_id,case_id,action,notes,actor_id,previous_status,resulting_status,expected_version,occurred_at,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, a.ID, tenant, id, a.Action, a.Notes, a.ActorID, a.PreviousStatus, a.ResultingStatus, a.ExpectedVersion, a.OccurredAt, audit.OperationID)
		if err != nil {
			return err
		}
		v.Version++
		v.UpdatedAt = audit.RecordedAt
		v.Actions = append(v.Actions, a)
		return insertAudit(ctx, tx, audit)
	})
	return v, err
}
func (repository *PostgresRepository) CreateIncident(ctx context.Context, v domain.OperationalIncident, a domain.AuditEvent) error {
	return repository.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		raw, _ := json.Marshal(v.Details)
		_, err := tx.Exec(ctx, `INSERT INTO operational_incidents(id,tenant_id,outlet_id,incident_type,severity,status,title,details,version,started_at,updated_at) VALUES($1,$2,$3,$4,$5,'open',$6,$7,1,$8,$8)`, v.ID, v.TenantID, v.OutletID, v.IncidentType, v.Severity, v.Title, raw, v.StartedAt)
		if err != nil {
			return err
		}
		eventID := inventoryEventUUID(v.TenantID, a.OperationID, "incident-opened")
		_, err = tx.Exec(ctx, `INSERT INTO incident_events(id,tenant_id,incident_id,event_type,message,actor_id,occurred_at,operation_id) VALUES($1,$2,$3,'opened',$4,$5,$6,$7)`, eventID, v.TenantID, v.ID, v.Title, a.ActorID, a.RecordedAt, a.OperationID)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
}
func (repository *PostgresRepository) Incidents(ctx context.Context, tenant, outlet string) ([]domain.OperationalIncident, error) {
	values := []domain.OperationalIncident{}
	err := repository.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,incident_type,severity,status,title,details,version,started_at,updated_at,resolved_at FROM operational_incidents WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'monitoring' THEN 1 ELSE 2 END,started_at DESC LIMIT 200`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.OperationalIncident
			var raw []byte
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.IncidentType, &v.Severity, &v.Status, &v.Title, &raw, &v.Version, &v.StartedAt, &v.UpdatedAt, &v.ResolvedAt); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &v.Details); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func (repository *PostgresRepository) TransitionIncident(ctx context.Context, tenant, outlet, id, status string, expected uint64, message string, a domain.AuditEvent) (domain.OperationalIncident, error) {
	var v domain.OperationalIncident
	err := repository.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,incident_type,severity,status,title,details,version,started_at,updated_at,resolved_at FROM operational_incidents WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, tenant, outlet, id).Scan(&v.ID, &v.TenantID, &v.OutletID, &v.IncidentType, &v.Severity, &v.Status, &v.Title, &raw, &v.Version, &v.StartedAt, &v.UpdatedAt, &v.ResolvedAt); err != nil {
			return err
		}
		if v.Version != expected {
			return ErrVersionConflict
		}
		if (status == "open" && v.Status != "resolved") || (status == "monitoring" && v.Status != "open" && v.Status != "monitoring") || (status == "resolved" && v.Status != "open" && v.Status != "monitoring") {
			return ErrInvalidTransition
		}
		eventType := "monitoring"
		if status == "resolved" {
			eventType = "resolved"
			now := a.RecordedAt
			v.ResolvedAt = &now
		} else if status == "open" {
			eventType = "reopened"
			v.ResolvedAt = nil
		} else if status != "monitoring" {
			return ErrInvalidTransition
		}
		tag, err := tx.Exec(ctx, `UPDATE operational_incidents SET status=$4,resolved_at=$5,version=version+1,updated_at=$6 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND version=$7`, tenant, outlet, id, status, v.ResolvedAt, a.RecordedAt, expected)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrVersionConflict
		}
		eventID := inventoryEventUUID(tenant, a.OperationID, "incident-event")
		_, err = tx.Exec(ctx, `INSERT INTO incident_events(id,tenant_id,incident_id,event_type,message,actor_id,occurred_at,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, eventID, tenant, id, eventType, message, a.ActorID, a.RecordedAt, a.OperationID)
		if err != nil {
			return err
		}
		v.Status = status
		v.Version++
		v.UpdatedAt = a.RecordedAt
		if err := json.Unmarshal(raw, &v.Details); err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
	return v, err
}
func (repository *PostgresRepository) RecordBackupManifest(ctx context.Context, v domain.BackupManifest, a domain.AuditEvent) error {
	return repository.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO backup_manifests(id,tenant_id,outlet_id,backup_type,storage_reference,content_sha256,size_bytes,encrypted,verified,started_at,completed_at,recovery_point_at,recorded_at,actor_id,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, v.ID, v.TenantID, nullable(v.OutletID), v.BackupType, v.StorageReference, v.ContentSHA256, v.SizeBytes, v.Encrypted, v.Verified, v.StartedAt, v.CompletedAt, v.RecoveryPointAt, v.RecordedAt, a.ActorID, a.OperationID)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
}
func (repository *PostgresRepository) RecordRestoreDrill(ctx context.Context, v domain.RestoreDrill, a domain.AuditEvent) error {
	return repository.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO restore_drills(id,tenant_id,backup_manifest_id,status,target_environment,started_at,completed_at,recovery_time_seconds,integrity_verified,notes,recorded_at,actor_id,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, v.ID, v.TenantID, v.BackupManifestID, v.Status, v.TargetEnvironment, v.StartedAt, v.CompletedAt, v.RecoveryTimeSeconds, v.IntegrityVerified, v.Notes, v.RecordedAt, a.ActorID, a.OperationID)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
}
func (repository *PostgresRepository) BackupEvidence(ctx context.Context, tenant string) ([]domain.BackupManifest, []domain.RestoreDrill, error) {
	backups := []domain.BackupManifest{}
	drills := []domain.RestoreDrill{}
	err := repository.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,COALESCE(outlet_id::text,''),backup_type,storage_reference,content_sha256,size_bytes,encrypted,verified,started_at,completed_at,recovery_point_at,recorded_at FROM backup_manifests WHERE tenant_id=$1 ORDER BY completed_at DESC LIMIT 100`, tenant)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v domain.BackupManifest
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.BackupType, &v.StorageReference, &v.ContentSHA256, &v.SizeBytes, &v.Encrypted, &v.Verified, &v.StartedAt, &v.CompletedAt, &v.RecoveryPointAt, &v.RecordedAt); err != nil {
				rows.Close()
				return err
			}
			backups = append(backups, v)
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT id,tenant_id,backup_manifest_id,status,target_environment,started_at,completed_at,recovery_time_seconds,integrity_verified,notes,recorded_at FROM restore_drills WHERE tenant_id=$1 ORDER BY completed_at DESC LIMIT 100`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.RestoreDrill
			if err := rows.Scan(&v.ID, &v.TenantID, &v.BackupManifestID, &v.Status, &v.TargetEnvironment, &v.StartedAt, &v.CompletedAt, &v.RecoveryTimeSeconds, &v.IntegrityVerified, &v.Notes, &v.RecordedAt); err != nil {
				return err
			}
			drills = append(drills, v)
		}
		return rows.Err()
	})
	return backups, drills, err
}

var _ OperationalControlRepository = (*PostgresRepository)(nil)
