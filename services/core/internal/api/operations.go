// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
	"net/http"
	"strings"
	"time"
)

func (s *Server) operations() (store.OperationalControlRepository, bool) {
	v, ok := s.repository.(store.OperationalControlRepository)
	return v, ok
}
func validSeverity(v string) bool {
	return v == "low" || v == "medium" || v == "high" || v == "critical"
}

type snapshotInput struct {
	ID       string         `json:"id"`
	OutletID string         `json:"outletId"`
	Content  map[string]any `json:"content"`
}

func (v snapshotInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) {
		return fmt.Errorf("snapshot and outlet ids must be UUID strings")
	}
	if len(v.Content) == 0 {
		return fmt.Errorf("content is required")
	}
	return nil
}
func (s *Server) handlePublishSnapshot(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input snapshotInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		if len(s.snapshotSigningKey) != ed25519.PrivateKeySize {
			return errorResult(apiError{Status: 503, Code: "snapshot_signing_unavailable", Message: "configuration snapshot signing is not configured"})
		}
		repository, ok := s.operations()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "operations_unavailable", Message: "operational control requires PostgreSQL"})
		}
		canonical, err := json.Marshal(input.Content)
		if err != nil {
			return internalOperationError()
		}
		hash := sha256.Sum256(canonical)
		signature := ed25519.Sign(s.snapshotSigningKey, hash[:])
		now := s.now().UTC()
		v := domain.ConfigurationSnapshot{ID: input.ID, TenantID: m.TenantID, OutletID: m.OutletID, Content: input.Content, ContentSHA256: hex.EncodeToString(hash[:]), Signature: base64.StdEncoding.EncodeToString(signature), PublicKey: base64.StdEncoding.EncodeToString(s.snapshotSigningKey.Public().(ed25519.PublicKey)), Algorithm: "Ed25519", Status: "published", SignedAt: now}
		audit, err := newAuditEvent(m, "configuration_snapshot.published", "configuration_snapshot", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		created, err := repository.PublishSnapshot(ctx, v, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(201, created, "/api/v1/configuration-snapshots/"+created.ID)
	})
}
func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	outlet := r.URL.Query().Get("outletId")
	repository, available := s.operations()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "operations_unavailable", Message: "operational control requires PostgreSQL"})
		return
	}
	values, err := repository.Snapshots(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writePaginated(w, r, values, func(v domain.ConfigurationSnapshot) string { return v.ID })
}
func (s *Server) handleCheckpoints(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repository, available := s.operations()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "operations_unavailable", Message: "operational control requires PostgreSQL"})
		return
	}
	values, err := repository.EdgeCheckpoints(r.Context(), tenant, r.URL.Query().Get("outletId"))
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}
func (s *Server) handleCases(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repository, available := s.operations()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "operations_unavailable", Message: "operational control requires PostgreSQL"})
		return
	}
	values, err := repository.ReconciliationCases(r.Context(), tenant, r.URL.Query().Get("outletId"))
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}

type caseActionInput struct {
	Action          string `json:"action"`
	ExpectedVersion uint64 `json:"expectedVersion"`
	Notes           string `json:"notes"`
	Assignee        string `json:"assignee"`
}

func (v caseActionInput) validate() error {
	allowed := map[string]bool{"assign": true, "retry": true, "resolve": true, "dismiss": true, "reopen": true}
	if !allowed[v.Action] || v.ExpectedVersion < 1 {
		return fmt.Errorf("action or expectedVersion is invalid")
	}
	if len(v.Notes) > 1000 || len(v.Assignee) > 128 {
		return fmt.Errorf("notes or assignee is too long")
	}
	return nil
}
func (s *Server) handleCaseAction(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		if !domain.ValidUUID(r.PathValue("id")) {
			return errorResult(apiError{Status: 422, Code: "validation_failed", Message: "case id must be a UUID"})
		}
		var input caseActionInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		repository, ok := s.operations()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		action := domain.ReconciliationAction{ID: m.ID, Action: input.Action, Notes: strings.TrimSpace(input.Notes), ExpectedVersion: input.ExpectedVersion}
		audit, err := newAuditEvent(m, "reconciliation_case."+input.Action, "reconciliation_case", r.PathValue("id"), now)
		if err != nil {
			return internalOperationError()
		}
		v, err := repository.ActOnReconciliationCase(ctx, m.TenantID, m.OutletID, r.PathValue("id"), action, strings.TrimSpace(input.Assignee), audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(200, v, "")
	})
}

type incidentInput struct {
	ID           string         `json:"id"`
	OutletID     string         `json:"outletId"`
	IncidentType string         `json:"incidentType"`
	Severity     string         `json:"severity"`
	Title        string         `json:"title"`
	Details      map[string]any `json:"details"`
}

func (v incidentInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) || !validSeverity(v.Severity) || strings.TrimSpace(v.IncidentType) == "" || strings.TrimSpace(v.Title) == "" {
		return fmt.Errorf("incident fields are invalid")
	}
	return nil
}
func (s *Server) handleCreateIncident(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input incidentInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		repository, ok := s.operations()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		v := domain.OperationalIncident{ID: input.ID, TenantID: m.TenantID, OutletID: m.OutletID, IncidentType: input.IncidentType, Severity: input.Severity, Status: "open", Title: input.Title, Details: input.Details, Version: 1, StartedAt: now, UpdatedAt: now}
		audit, err := newAuditEvent(m, "incident.opened", "operational_incident", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repository.CreateIncident(ctx, v, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/incidents/"+v.ID)
	})
}
func (s *Server) handleIncidents(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repository, available := s.operations()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "operations_unavailable", Message: "operational control requires PostgreSQL"})
		return
	}
	values, err := repository.Incidents(r.Context(), tenant, r.URL.Query().Get("outletId"))
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}

type incidentActionInput struct {
	Status          string `json:"status"`
	ExpectedVersion uint64 `json:"expectedVersion"`
	Message         string `json:"message"`
}

func (v incidentActionInput) validate() error {
	if (v.Status != "open" && v.Status != "monitoring" && v.Status != "resolved") || v.ExpectedVersion < 1 {
		return fmt.Errorf("status or expectedVersion is invalid")
	}
	return nil
}
func (s *Server) handleIncidentAction(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		if !domain.ValidUUID(r.PathValue("id")) {
			return errorResult(apiError{Status: 422, Code: "validation_failed", Message: "incident id must be a UUID"})
		}
		var input incidentActionInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		repository, ok := s.operations()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		audit, err := newAuditEvent(m, "incident."+input.Status, "operational_incident", r.PathValue("id"), now)
		if err != nil {
			return internalOperationError()
		}
		v, err := repository.TransitionIncident(ctx, m.TenantID, m.OutletID, r.PathValue("id"), input.Status, input.ExpectedVersion, input.Message, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(200, v, "")
	})
}

type backupInput struct {
	ID               string    `json:"id"`
	OutletID         string    `json:"outletId"`
	BackupType       string    `json:"backupType"`
	StorageReference string    `json:"storageReference"`
	ContentSHA256    string    `json:"contentSha256"`
	SizeBytes        int64     `json:"sizeBytes"`
	Encrypted        bool      `json:"encrypted"`
	Verified         bool      `json:"verified"`
	StartedAt        time.Time `json:"startedAt"`
	CompletedAt      time.Time `json:"completedAt"`
	RecoveryPointAt  time.Time `json:"recoveryPointAt"`
}

func (v backupInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) || (v.BackupType != "full" && v.BackupType != "incremental" && v.BackupType != "edge") || !sha256Pattern.MatchString(v.ContentSHA256) || v.SizeBytes < 1 || v.StartedAt.IsZero() || v.CompletedAt.IsZero() || v.RecoveryPointAt.IsZero() || v.CompletedAt.Before(v.StartedAt) || strings.TrimSpace(v.StorageReference) == "" {
		return fmt.Errorf("backup evidence is invalid")
	}
	return nil
}
func (s *Server) handleRecordBackup(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input backupInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		repository, ok := s.operations()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		v := domain.BackupManifest{ID: input.ID, TenantID: m.TenantID, OutletID: input.OutletID, BackupType: input.BackupType, StorageReference: input.StorageReference, ContentSHA256: input.ContentSHA256, SizeBytes: input.SizeBytes, Encrypted: input.Encrypted, Verified: input.Verified, StartedAt: input.StartedAt, CompletedAt: input.CompletedAt, RecoveryPointAt: input.RecoveryPointAt, RecordedAt: now}
		audit, err := newAuditEvent(m, "backup.recorded", "backup_manifest", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repository.RecordBackupManifest(ctx, v, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/backup-evidence/"+v.ID)
	})
}

type drillInput struct {
	ID                  string    `json:"id"`
	BackupManifestID    string    `json:"backupManifestId"`
	Status              string    `json:"status"`
	TargetEnvironment   string    `json:"targetEnvironment"`
	StartedAt           time.Time `json:"startedAt"`
	CompletedAt         time.Time `json:"completedAt"`
	RecoveryTimeSeconds int       `json:"recoveryTimeSeconds"`
	IntegrityVerified   bool      `json:"integrityVerified"`
	Notes               string    `json:"notes"`
}

func (v drillInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.BackupManifestID) || (v.Status != "passed" && v.Status != "failed") || v.StartedAt.IsZero() || v.CompletedAt.IsZero() || v.CompletedAt.Before(v.StartedAt) || v.RecoveryTimeSeconds < 0 || strings.TrimSpace(v.TargetEnvironment) == "" || len(v.Notes) > 2000 {
		return fmt.Errorf("restore drill evidence is invalid")
	}
	return nil
}
func (s *Server) handleRecordDrill(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input drillInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		repository, ok := s.operations()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		v := domain.RestoreDrill{ID: input.ID, TenantID: m.TenantID, BackupManifestID: input.BackupManifestID, Status: input.Status, TargetEnvironment: input.TargetEnvironment, StartedAt: input.StartedAt, CompletedAt: input.CompletedAt, RecoveryTimeSeconds: input.RecoveryTimeSeconds, IntegrityVerified: input.IntegrityVerified, Notes: input.Notes, RecordedAt: now}
		audit, err := newAuditEvent(m, "restore_drill.recorded", "restore_drill", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repository.RecordRestoreDrill(ctx, v, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/restore-drills/"+v.ID)
	})
}
func (s *Server) handleBackupEvidence(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repository, available := s.operations()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "operations_unavailable", Message: "operational control requires PostgreSQL"})
		return
	}
	backups, drills, err := repository.BackupEvidence(r.Context(), tenant)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, map[string]any{"backups": backups, "restoreDrills": drills})
}
