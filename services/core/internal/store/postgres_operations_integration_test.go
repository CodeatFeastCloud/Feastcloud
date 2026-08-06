// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

func TestOperationalControlEvidenceAndVersionedTransitionsIntegration(t *testing.T) {
	databaseURL := os.Getenv("FEASTCLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set FEASTCLOUD_TEST_DATABASE_URL to run PostgreSQL integration coverage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, err := NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	now := time.Now().UTC()

	snapshotID := newIntegrationUUID(t)
	snapshot, err := repository.PublishSnapshot(ctx, domain.ConfigurationSnapshot{ID: snapshotID, TenantID: integrationTenantA, OutletID: integrationOutletA, Content: map[string]any{"menuVersion": 1}, ContentSHA256: strings.Repeat("a", 64), Signature: "signature", PublicKey: "public", Algorithm: "Ed25519", Status: "published", SignedAt: now}, integrationAudit(t, integrationTenantA, integrationOutletA, "configuration_snapshot", snapshotID, "configuration_snapshot.published", now))
	if err != nil || snapshot.Sequence < 1 {
		t.Fatalf("publish snapshot: %#v %v", snapshot, err)
	}

	operation := integrationOperation(integrationTenantA, integrationOutletA, "inventory.adjust", "inventory")
	if outcome, problem, err := repository.ApplySyncOperation(ctx, operation); err != nil || outcome != SyncRejected || problem != "unsupported_command_type" {
		t.Fatalf("reject sync: %s/%s/%v", outcome, problem, err)
	}
	checkpoints, err := repository.EdgeCheckpoints(ctx, integrationTenantA, integrationOutletA)
	if err != nil || len(checkpoints) == 0 || !checkpoints[0].Degraded {
		t.Fatalf("checkpoint evidence: %#v %v", checkpoints, err)
	}
	cases, err := repository.ReconciliationCases(ctx, integrationTenantA, integrationOutletA)
	if err != nil {
		t.Fatal(err)
	}
	var problemCase *domain.ReconciliationCase
	for index := range cases {
		if cases[index].SourceID == operation.OperationID {
			problemCase = &cases[index]
		}
	}
	if problemCase == nil {
		t.Fatal("sync rejection did not open reconciliation case")
	}
	actionID := newIntegrationUUID(t)
	actionAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "reconciliation_case", problemCase.ID, "reconciliation_case.resolve", now.Add(time.Second))
	resolved, err := repository.ActOnReconciliationCase(ctx, integrationTenantA, integrationOutletA, problemCase.ID, domain.ReconciliationAction{ID: actionID, Action: "resolve", Notes: "reviewed", ExpectedVersion: problemCase.Version}, "", actionAudit)
	if err != nil || resolved.Status != "resolved" || resolved.Version != problemCase.Version+1 {
		t.Fatalf("resolve case: %#v %v", resolved, err)
	}
	staleAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "reconciliation_case", problemCase.ID, "reconciliation_case.resolve", now.Add(2*time.Second))
	_, err = repository.ActOnReconciliationCase(ctx, integrationTenantA, integrationOutletA, problemCase.ID, domain.ReconciliationAction{ID: newIntegrationUUID(t), Action: "resolve", ExpectedVersion: problemCase.Version}, "", staleAudit)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale case action error=%v", err)
	}

	incidentID := newIntegrationUUID(t)
	incidentAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "operational_incident", incidentID, "incident.opened", now)
	incident := domain.OperationalIncident{ID: incidentID, TenantID: integrationTenantA, OutletID: integrationOutletA, IncidentType: "network", Severity: "high", Status: "open", Title: "WAN unstable", Details: map[string]any{"provider": "test"}, Version: 1, StartedAt: now, UpdatedAt: now}
	if err := repository.CreateIncident(ctx, incident, incidentAudit); err != nil {
		t.Fatal(err)
	}
	monitorAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "operational_incident", incidentID, "incident.monitoring", now.Add(time.Second))
	monitoring, err := repository.TransitionIncident(ctx, integrationTenantA, integrationOutletA, incidentID, "monitoring", 1, "watching", monitorAudit)
	if err != nil || monitoring.Version != 2 {
		t.Fatalf("monitor incident: %#v %v", monitoring, err)
	}
	_, err = repository.TransitionIncident(ctx, integrationTenantA, integrationOutletA, incidentID, "monitoring", 1, "stale", integrationAudit(t, integrationTenantA, integrationOutletA, "operational_incident", incidentID, "incident.monitoring", now.Add(2*time.Second)))
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale incident action error=%v", err)
	}

	backupID := newIntegrationUUID(t)
	backup := domain.BackupManifest{ID: backupID, TenantID: integrationTenantA, OutletID: integrationOutletA, BackupType: "edge", StorageReference: "test://backup/" + backupID, ContentSHA256: strings.Repeat("b", 64), SizeBytes: 100, Encrypted: true, Verified: true, StartedAt: now, CompletedAt: now.Add(time.Second), RecoveryPointAt: now, RecordedAt: now.Add(time.Second)}
	if err := repository.RecordBackupManifest(ctx, backup, integrationAudit(t, integrationTenantA, integrationOutletA, "backup_manifest", backupID, "backup.recorded", now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	drillID := newIntegrationUUID(t)
	drill := domain.RestoreDrill{ID: drillID, TenantID: integrationTenantA, BackupManifestID: backupID, Status: "passed", TargetEnvironment: "isolated", StartedAt: now, CompletedAt: now.Add(3 * time.Second), RecoveryTimeSeconds: 3, IntegrityVerified: true, Notes: "verified", RecordedAt: now.Add(3 * time.Second)}
	if err := repository.RecordRestoreDrill(ctx, drill, integrationAudit(t, integrationTenantA, integrationOutletA, "restore_drill", drillID, "restore_drill.recorded", now.Add(3*time.Second))); err != nil {
		t.Fatal(err)
	}
	backups, drills, err := repository.BackupEvidence(ctx, integrationTenantA)
	if err != nil || len(backups) == 0 || len(drills) == 0 {
		t.Fatalf("backup evidence: %d/%d/%v", len(backups), len(drills), err)
	}
}
