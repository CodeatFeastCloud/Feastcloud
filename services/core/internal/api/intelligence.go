// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type importRowInput struct {
	ID          string         `json:"id"`
	RowNumber   int            `json:"rowNumber"`
	ExternalRef string         `json:"externalRef"`
	PlacedAt    string         `json:"placedAt"`
	OrderType   string         `json:"orderType"`
	ItemCode    string         `json:"itemCode"`
	Quantity    float64        `json:"quantity"`
	RawData     map[string]any `json:"rawData"`
}
type orderImportInput struct {
	ID         string           `json:"id"`
	OutletID   string           `json:"outletId"`
	FileName   string           `json:"fileName"`
	FileSHA256 string           `json:"fileSha256"`
	Rows       []importRowInput `json:"rows"`
}

func (v orderImportInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) {
		return fmt.Errorf("import and outlet ids must be UUID strings")
	}
	if strings.TrimSpace(v.FileName) == "" || len(v.FileName) > 255 {
		return fmt.Errorf("fileName is required and must be at most 255 characters")
	}
	if !sha256Pattern.MatchString(v.FileSHA256) {
		return fmt.Errorf("fileSha256 must be a lowercase SHA-256 digest")
	}
	if len(v.Rows) == 0 || len(v.Rows) > 1000 {
		return fmt.Errorf("rows must contain between 1 and 1000 CSV records")
	}
	seen := map[int]bool{}
	for _, row := range v.Rows {
		if !domain.ValidUUID(row.ID) || row.RowNumber < 2 {
			return fmt.Errorf("each row requires a UUID id and source rowNumber of at least 2")
		}
		if seen[row.RowNumber] {
			return fmt.Errorf("rowNumber values must be unique")
		}
		seen[row.RowNumber] = true
	}
	return nil
}

type planningRunInput struct {
	ID       string `json:"id"`
	OutletID string `json:"outletId"`
}

func (v planningRunInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) {
		return fmt.Errorf("run and outlet ids must be UUID strings")
	}
	return nil
}

func (s *Server) intelligence() (store.IntelligenceRepository, bool) {
	repository, ok := s.repository.(store.IntelligenceRepository)
	return repository, ok
}

func (s *Server) handleImportOrders(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input orderImportInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		repository, ok := s.intelligence()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "order_import_unavailable", Message: "durable order imports require PostgreSQL"})
		}
		rows := make([]store.ImportedOrderRow, len(input.Rows))
		for index, row := range input.Rows {
			parsed := store.ImportedOrderRow{ID: row.ID, RowNumber: row.RowNumber, ExternalRef: strings.TrimSpace(row.ExternalRef), OrderType: row.OrderType, ItemCode: strings.TrimSpace(row.ItemCode), RawData: row.RawData}
			placedAt, err := time.Parse(time.RFC3339, row.PlacedAt)
			switch {
			case parsed.ExternalRef == "" || len(parsed.ExternalRef) > 128:
				parsed.ErrorCode, parsed.ErrorMessage = "invalid_external_ref", "externalRef is required and must be at most 128 characters."
			case err != nil:
				parsed.ErrorCode, parsed.ErrorMessage = "invalid_placed_at", "placedAt must be an RFC3339 timestamp."
			case !domain.ValidOrderType(domain.OrderType(parsed.OrderType)):
				parsed.ErrorCode, parsed.ErrorMessage = "invalid_order_type", "orderType must be dineIn, takeaway, delivery, or roomService."
			case parsed.ItemCode == "" || len(parsed.ItemCode) > 64:
				parsed.ErrorCode, parsed.ErrorMessage = "invalid_item_code", "itemCode is required and must be at most 64 characters."
			case row.Quantity < 1 || row.Quantity > math.MaxInt32 || row.Quantity != math.Trunc(row.Quantity):
				parsed.ErrorCode, parsed.ErrorMessage = "invalid_quantity", "quantity must be a positive whole number."
			default:
				parsed.PlacedAt = placedAt
				parsed.Quantity = int32(row.Quantity)
			}
			rows[index] = parsed
		}
		now := s.now().UTC()
		value := domain.OrderImport{ID: input.ID, TenantID: m.TenantID, OutletID: input.OutletID, FileName: strings.TrimSpace(input.FileName), FileSHA256: input.FileSHA256, ImportedAt: now}
		audit, err := newAuditEvent(m, "order_import.completed", "order_import", value.ID, now)
		if err != nil {
			return internalOperationError()
		}
		created, err := repository.ImportOrders(ctx, value, rows, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(201, created, "/api/v1/order-imports/"+created.ID)
	})
}

func (s *Server) handleOrderImports(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	outlet := r.URL.Query().Get("outletId")
	if !domain.ValidUUID(outlet) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "invalid_outlet_id", Message: "outletId must be a UUID string"})
		return
	}
	repository, available := s.intelligence()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "order_import_unavailable", Message: "durable order imports require PostgreSQL"})
		return
	}
	values, err := repository.OrderImports(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writePaginated(w, r, values, func(value domain.OrderImport) string { return value.ID })
}

func (s *Server) handleGeneratePlanningRun(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input planningRunInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		repository, ok := s.intelligence()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "planning_unavailable", Message: "planning intelligence requires PostgreSQL"})
		}
		now := s.now().UTC()
		start := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
		value := domain.PlanningRun{ID: input.ID, TenantID: m.TenantID, OutletID: input.OutletID, HorizonStart: start, HorizonEnd: start.Add(24 * time.Hour), ModelVersion: "trailing_average_recipe_graph_v1", Status: "observed", EvidenceFrom: now.Add(-28 * 24 * time.Hour), EvidenceTo: now, GeneratedAt: now}
		audit, err := newAuditEvent(m, "planning_run.generated", "planning_run", value.ID, now)
		if err != nil {
			return internalOperationError()
		}
		created, err := repository.GeneratePlanningRun(ctx, value, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(201, created, "/api/v1/planning-runs/"+created.ID)
	})
}

func (s *Server) handlePlanningRuns(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	outlet := r.URL.Query().Get("outletId")
	if !domain.ValidUUID(outlet) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "invalid_outlet_id", Message: "outletId must be a UUID string"})
		return
	}
	repository, available := s.intelligence()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "planning_unavailable", Message: "planning intelligence requires PostgreSQL"})
		return
	}
	values, err := repository.PlanningRuns(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writePaginated(w, r, values, func(value domain.PlanningRun) string { return value.ID })
}
