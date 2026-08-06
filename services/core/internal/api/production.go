// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

type productionBatchInput struct {
	ID                 string    `json:"id"`
	OutletID           string    `json:"outletId"`
	StationID          string    `json:"stationId,omitempty"`
	RecipeVersionID    string    `json:"recipeVersionId"`
	OutputIngredientID string    `json:"outputIngredientId"`
	OutputUnitID       string    `json:"outputUnitId"`
	PlannedQuantity    float64   `json:"plannedQuantity"`
	PlannedFor         time.Time `json:"plannedFor"`
	LotCode            string    `json:"lotCode,omitempty"`
	Notes              string    `json:"notes,omitempty"`
}

func (v productionBatchInput) validate() error {
	for name, id := range map[string]string{"id": v.ID, "outletId": v.OutletID, "recipeVersionId": v.RecipeVersionID, "outputIngredientId": v.OutputIngredientID, "outputUnitId": v.OutputUnitID} {
		if !domain.ValidUUID(id) {
			return fmt.Errorf("%s must be a UUID string", name)
		}
	}
	if v.StationID != "" && !domain.ValidUUID(v.StationID) {
		return fmt.Errorf("stationId must be a UUID string")
	}
	if v.PlannedQuantity <= 0 || math.IsNaN(v.PlannedQuantity) || math.IsInf(v.PlannedQuantity, 0) {
		return fmt.Errorf("plannedQuantity must be finite and positive")
	}
	if v.PlannedFor.IsZero() {
		return fmt.Errorf("plannedFor is required")
	}
	if len(v.LotCode) > 128 || len(v.Notes) > 1000 {
		return fmt.Errorf("lotCode or notes is too long")
	}
	return nil
}

type productionTransitionInput struct {
	ToStatus        domain.ProductionBatchStatus `json:"toStatus"`
	ExpectedVersion uint64                       `json:"expectedVersion"`
	ActualQuantity  *float64                     `json:"actualQuantity,omitempty"`
	ExpiresAt       *time.Time                   `json:"expiresAt,omitempty"`
	LotCode         string                       `json:"lotCode,omitempty"`
	Notes           string                       `json:"notes,omitempty"`
}

func (v productionTransitionInput) validate() error {
	if !domain.ValidProductionBatchStatus(v.ToStatus) {
		return fmt.Errorf("toStatus is not supported")
	}
	if v.ExpectedVersion < 1 {
		return fmt.Errorf("expectedVersion must be at least one")
	}
	if v.ActualQuantity != nil && (*v.ActualQuantity <= 0 || math.IsNaN(*v.ActualQuantity) || math.IsInf(*v.ActualQuantity, 0)) {
		return fmt.Errorf("actualQuantity must be finite and positive")
	}
	if len(v.LotCode) > 128 || len(v.Notes) > 1000 {
		return fmt.Errorf("lotCode or notes is too long")
	}
	return nil
}

func (s *Server) production() (store.ProductionRepository, bool) {
	repository, ok := s.repository.(store.ProductionRepository)
	return repository, ok
}

func (s *Server) handleCreateProductionBatch(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input productionBatchInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		repository, ok := s.production()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "production_unavailable", Message: "production batches require PostgreSQL"})
		}
		now := s.now().UTC()
		value := domain.ProductionBatch{ID: input.ID, TenantID: m.TenantID, OutletID: input.OutletID, StationID: input.StationID, RecipeVersionID: input.RecipeVersionID, OutputIngredientID: input.OutputIngredientID, OutputUnitID: input.OutputUnitID, Status: domain.ProductionBatchPlanned, PlannedQuantity: input.PlannedQuantity, PlannedFor: input.PlannedFor, LotCode: strings.TrimSpace(input.LotCode), Notes: strings.TrimSpace(input.Notes), RecordMetadata: newRecordMetadata(now)}
		audit, err := newAuditEvent(m, "production_batch.planned", "production_batch", value.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repository.CreateProductionBatch(ctx, value, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(201, value, "/api/v1/production-batches/"+value.ID)
	})
}

func (s *Server) handleProductionBatches(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	outletID := r.URL.Query().Get("outletId")
	if !domain.ValidUUID(outletID) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "invalid_outlet_id", Message: "outletId must be a UUID string"})
		return
	}
	if principal, found := principalFrom(r.Context()); found && !principal.AllowsOutlet(outletID) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 403, Code: "outlet_permission_denied", Message: "the authenticated principal is not assigned to this outlet"})
		return
	}
	repository, available := s.production()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "production_unavailable", Message: "production batches require PostgreSQL"})
		return
	}
	values, err := repository.ProductionBatches(r.Context(), tenantID, outletID)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writePaginated(w, r, values, func(value domain.ProductionBatch) string { return value.ID })
}

func (s *Server) handleTransitionProductionBatch(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input productionTransitionInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		id := r.PathValue("id")
		if !domain.ValidUUID(id) {
			return errorResult(apiError{Status: 422, Code: "invalid_production_batch_id", Message: "production batch id must be a UUID string"})
		}
		repository, ok := s.production()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "production_unavailable", Message: "production batches require PostgreSQL"})
		}
		now := s.now().UTC()
		audit, err := newAuditEvent(m, "production_batch.status_changed", "production_batch", id, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repository.TransitionProductionBatch(ctx, m.TenantID, m.OutletID, id, input.ToStatus, input.ExpectedVersion, input.ActualQuantity, input.ExpiresAt, strings.TrimSpace(input.LotCode), strings.TrimSpace(input.Notes), audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(200, value, "")
	})
}
