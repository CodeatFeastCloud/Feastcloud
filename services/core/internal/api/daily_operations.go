// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
	"math"
	"net/http"
	"strings"
	"time"
)

func (s *Server) dailyOps() (store.DailyOperationsRepository, bool) {
	v, ok := s.repository.(store.DailyOperationsRepository)
	return v, ok
}
func readOutlet(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return "", "", false
	}
	outlet := r.URL.Query().Get("outletId")
	if !domain.ValidUUID(outlet) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "invalid_outlet_id", Message: "outletId must be a UUID string"})
		return "", "", false
	}
	if principal, found := principalFrom(r.Context()); found && !principal.AllowsOutlet(outlet) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 403, Code: "outlet_permission_denied", Message: "the authenticated principal is not assigned to this outlet"})
		return "", "", false
	}
	return tenant, outlet, true
}

type supplierInput struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	ContactName string `json:"contactName"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	TaxID       string `json:"taxId"`
}

func (v supplierInput) validate() error {
	if !domain.ValidUUID(v.ID) || strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Code) == "" || len(v.Name) > 160 || len(v.Code) > 64 {
		return fmt.Errorf("supplier id, name, or code is invalid")
	}
	return nil
}
func (s *Server) handleCreateSupplier(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input supplierInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		repo, ok := s.dailyOps()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		v := domain.Supplier{ID: input.ID, TenantID: m.TenantID, Name: strings.TrimSpace(input.Name), Code: strings.TrimSpace(input.Code), ContactName: strings.TrimSpace(input.ContactName), Phone: strings.TrimSpace(input.Phone), Email: strings.TrimSpace(input.Email), TaxID: strings.TrimSpace(input.TaxID), Active: true, RecordMetadata: newRecordMetadata(now)}
		a, err := newAuditEvent(m, "supplier.created", "supplier", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repo.CreateSupplier(ctx, v, a); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/suppliers/"+v.ID)
	})
}
func (s *Server) handleSuppliers(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repo, available := s.dailyOps()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "daily_operations_unavailable", Message: "daily operations require PostgreSQL"})
		return
	}
	values, err := repo.Suppliers(r.Context(), tenant)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}

type poLineInput struct {
	ID              string  `json:"id"`
	IngredientID    string  `json:"ingredientId"`
	UnitID          string  `json:"unitId"`
	OrderedQuantity float64 `json:"orderedQuantity"`
	UnitCostMinor   int64   `json:"unitCostMinor"`
}
type poInput struct {
	ID         string        `json:"id"`
	OutletID   string        `json:"outletId"`
	SupplierID string        `json:"supplierId"`
	PONumber   string        `json:"poNumber"`
	ExpectedAt *time.Time    `json:"expectedAt"`
	Notes      string        `json:"notes"`
	Lines      []poLineInput `json:"lines"`
}

func (v poInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) || !domain.ValidUUID(v.SupplierID) || strings.TrimSpace(v.PONumber) == "" || len(v.Lines) == 0 {
		return fmt.Errorf("purchase order fields are invalid")
	}
	seen := map[string]bool{}
	for _, line := range v.Lines {
		if !domain.ValidUUID(line.ID) || !domain.ValidUUID(line.IngredientID) || !domain.ValidUUID(line.UnitID) || line.OrderedQuantity <= 0 || math.IsNaN(line.OrderedQuantity) || math.IsInf(line.OrderedQuantity, 0) || line.UnitCostMinor < 0 || seen[line.IngredientID] {
			return fmt.Errorf("purchase order line is invalid or duplicated")
		}
		seen[line.IngredientID] = true
	}
	return nil
}
func (s *Server) handleCreatePO(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input poInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		lines := make([]domain.PurchaseOrderLine, len(input.Lines))
		for i, line := range input.Lines {
			lines[i] = domain.PurchaseOrderLine{ID: line.ID, IngredientID: line.IngredientID, UnitID: line.UnitID, OrderedQuantity: line.OrderedQuantity, UnitCostMinor: line.UnitCostMinor}
		}
		now := s.now().UTC()
		v := domain.PurchaseOrder{ID: input.ID, TenantID: m.TenantID, OutletID: m.OutletID, SupplierID: input.SupplierID, PONumber: strings.TrimSpace(input.PONumber), Status: "draft", ExpectedAt: input.ExpectedAt, Currency: "INR", Notes: strings.TrimSpace(input.Notes), Lines: lines, RecordMetadata: newRecordMetadata(now)}
		a, err := newAuditEvent(m, "purchase_order.created", "purchase_order", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.dailyOps()
		if !ok {
			return internalOperationError()
		}
		if err := repo.CreatePurchaseOrder(ctx, v, a); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/purchase-orders/"+v.ID)
	})
}
func (s *Server) handlePOs(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, available := s.dailyOps()
	if !available {
		return
	}
	values, err := repo.PurchaseOrders(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}

type transitionInput struct {
	Status          string `json:"status"`
	ExpectedVersion uint64 `json:"expectedVersion"`
}

func (s *Server) handlePOTransition(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input transitionInput
		if result := decodeAndValidate(p, &input, func() error {
			if input.ExpectedVersion < 1 || (input.Status != "submitted" && input.Status != "cancelled") {
				return fmt.Errorf("transition is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		id := r.PathValue("id")
		if !domain.ValidUUID(id) {
			return errorResult(apiError{Status: 422, Code: "validation_failed", Message: "purchase order id must be a UUID"})
		}
		now := s.now().UTC()
		a, err := newAuditEvent(m, "purchase_order."+input.Status, "purchase_order", id, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.dailyOps()
		if !ok {
			return internalOperationError()
		}
		v, err := repo.TransitionPurchaseOrder(ctx, m.TenantID, m.OutletID, id, input.Status, input.ExpectedVersion, a)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(200, v, "")
	})
}

type receiptLineInput struct {
	ID                  string     `json:"id"`
	PurchaseOrderLineID string     `json:"purchaseOrderLineId"`
	IngredientID        string     `json:"ingredientId"`
	UnitID              string     `json:"unitId"`
	Quantity            float64    `json:"quantity"`
	LotCode             string     `json:"lotCode"`
	ExpiresAt           *time.Time `json:"expiresAt"`
}
type receiptInput struct {
	ID               string             `json:"id"`
	OutletID         string             `json:"outletId"`
	PurchaseOrderID  string             `json:"purchaseOrderId"`
	ExpectedVersion  uint64             `json:"expectedVersion"`
	SupplierDocument string             `json:"supplierDocument"`
	Notes            string             `json:"notes"`
	Lines            []receiptLineInput `json:"lines"`
}

func (v receiptInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) || !domain.ValidUUID(v.PurchaseOrderID) || v.ExpectedVersion < 1 || len(v.Lines) == 0 {
		return fmt.Errorf("receipt is invalid")
	}
	for _, line := range v.Lines {
		if !domain.ValidUUID(line.ID) || !domain.ValidUUID(line.PurchaseOrderLineID) || !domain.ValidUUID(line.IngredientID) || !domain.ValidUUID(line.UnitID) || line.Quantity <= 0 {
			return fmt.Errorf("receipt line is invalid")
		}
	}
	return nil
}
func (s *Server) handleReceivePO(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input receiptInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		lines := make([]domain.GoodsReceiptLine, len(input.Lines))
		for i, line := range input.Lines {
			lines[i] = domain.GoodsReceiptLine{ID: line.ID, PurchaseOrderLineID: line.PurchaseOrderLineID, IngredientID: line.IngredientID, UnitID: line.UnitID, Quantity: line.Quantity, LotCode: strings.TrimSpace(line.LotCode), ExpiresAt: line.ExpiresAt}
		}
		now := s.now().UTC()
		v := domain.GoodsReceipt{ID: input.ID, TenantID: m.TenantID, OutletID: m.OutletID, PurchaseOrderID: input.PurchaseOrderID, ReceivedAt: now, SupplierDocument: strings.TrimSpace(input.SupplierDocument), Notes: strings.TrimSpace(input.Notes), Lines: lines}
		a, err := newAuditEvent(m, "goods_receipt.recorded", "goods_receipt", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.dailyOps()
		if !ok {
			return internalOperationError()
		}
		po, err := repo.ReceivePurchaseOrder(ctx, v, input.ExpectedVersion, a)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(201, po, "/api/v1/goods-receipts/"+v.ID)
	})
}

type temperatureInput struct {
	ID               string    `json:"id"`
	OutletID         string    `json:"outletId"`
	Location         string    `json:"location"`
	TemperatureC     float64   `json:"temperatureC"`
	SafeMinC         float64   `json:"safeMinC"`
	SafeMaxC         float64   `json:"safeMaxC"`
	CorrectiveAction string    `json:"correctiveAction"`
	MeasuredAt       time.Time `json:"measuredAt"`
}

func (v temperatureInput) validate() error {
	compliant := v.TemperatureC >= v.SafeMinC && v.TemperatureC <= v.SafeMaxC
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) || strings.TrimSpace(v.Location) == "" || v.SafeMinC > v.SafeMaxC || v.TemperatureC < -100 || v.TemperatureC > 300 || (!compliant && strings.TrimSpace(v.CorrectiveAction) == "") {
		return fmt.Errorf("temperature check or corrective action is invalid")
	}
	return nil
}
func (s *Server) handleRecordTemperature(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input temperatureInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		if input.MeasuredAt.IsZero() {
			input.MeasuredAt = now
		}
		v := domain.TemperatureLog{ID: input.ID, TenantID: m.TenantID, OutletID: m.OutletID, Location: strings.TrimSpace(input.Location), TemperatureC: input.TemperatureC, SafeMinC: input.SafeMinC, SafeMaxC: input.SafeMaxC, Compliant: input.TemperatureC >= input.SafeMinC && input.TemperatureC <= input.SafeMaxC, CorrectiveAction: strings.TrimSpace(input.CorrectiveAction), MeasuredAt: input.MeasuredAt, ActorID: m.ActorID}
		a, err := newAuditEvent(m, "temperature.recorded", "temperature_log", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.dailyOps()
		if !ok {
			return internalOperationError()
		}
		if err := repo.RecordTemperature(ctx, v, a); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/temperature-logs/"+v.ID)
	})
}
func (s *Server) handleTemperatureLogs(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, available := s.dailyOps()
	if !available {
		return
	}
	values, err := repo.TemperatureLogs(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}

type checklistInput struct {
	ID            string `json:"id"`
	OutletID      string `json:"outletId"`
	ChecklistType string `json:"checklistType"`
	BusinessDate  string `json:"businessDate"`
	Items         []struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		Required bool   `json:"required"`
	} `json:"items"`
}

func (v checklistInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) || (v.ChecklistType != "opening" && v.ChecklistType != "closing" && v.ChecklistType != "food_safety") || len(v.Items) == 0 {
		return fmt.Errorf("checklist is invalid")
	}
	if _, err := time.Parse("2006-01-02", v.BusinessDate); err != nil {
		return fmt.Errorf("businessDate is invalid")
	}
	for _, item := range v.Items {
		if !domain.ValidUUID(item.ID) || strings.TrimSpace(item.Label) == "" {
			return fmt.Errorf("checklist item is invalid")
		}
	}
	return nil
}
func (s *Server) handleCreateChecklist(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input checklistInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		items := make([]domain.ChecklistItem, len(input.Items))
		for i, item := range input.Items {
			items[i] = domain.ChecklistItem{ID: item.ID, Label: strings.TrimSpace(item.Label), Required: item.Required, Position: i}
		}
		now := s.now().UTC()
		v := domain.OperationalChecklist{ID: input.ID, TenantID: m.TenantID, OutletID: m.OutletID, ChecklistType: input.ChecklistType, BusinessDate: input.BusinessDate, Status: "open", Version: 1, CreatedAt: now, UpdatedAt: now, Items: items}
		a, err := newAuditEvent(m, "checklist.created", "operational_checklist", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.dailyOps()
		if !ok {
			return internalOperationError()
		}
		if err := repo.CreateChecklist(ctx, v, a); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/checklists/"+v.ID)
	})
}
func (s *Server) handleChecklists(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, available := s.dailyOps()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "daily_operations_unavailable", Message: "daily operations require PostgreSQL"})
		return
	}
	values, err := repo.Checklists(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}

type checklistCompleteInput struct {
	ExpectedVersion uint64 `json:"expectedVersion"`
}

func (s *Server) handleCompleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input checklistCompleteInput
		if result := decodeAndValidate(p, &input, func() error {
			if input.ExpectedVersion < 1 {
				return fmt.Errorf("expectedVersion is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		id, item := r.PathValue("id"), r.PathValue("itemId")
		if !domain.ValidUUID(id) || !domain.ValidUUID(item) {
			return errorResult(apiError{Status: 422, Code: "validation_failed", Message: "checklist ids must be UUIDs"})
		}
		now := s.now().UTC()
		a, err := newAuditEvent(m, "checklist.item_completed", "operational_checklist", id, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.dailyOps()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "daily_operations_unavailable", Message: "daily operations require PostgreSQL"})
		}
		v, err := repo.CompleteChecklistItem(ctx, m.TenantID, m.OutletID, id, item, input.ExpectedVersion, a)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(200, v, "")
	})
}

type staffInput struct {
	ID           string `json:"id"`
	EmployeeCode string `json:"employeeCode"`
	DisplayName  string `json:"displayName"`
	Role         string `json:"role"`
	Phone        string `json:"phone"`
}

func (s *Server) handleCreateStaff(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input staffInput
		if result := decodeAndValidate(p, &input, func() error {
			if !domain.ValidUUID(input.ID) || strings.TrimSpace(input.EmployeeCode) == "" || strings.TrimSpace(input.DisplayName) == "" || strings.TrimSpace(input.Role) == "" {
				return fmt.Errorf("staff fields are invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		now := s.now().UTC()
		v := domain.StaffMember{ID: input.ID, TenantID: m.TenantID, EmployeeCode: strings.TrimSpace(input.EmployeeCode), DisplayName: strings.TrimSpace(input.DisplayName), Role: strings.TrimSpace(input.Role), Phone: strings.TrimSpace(input.Phone), Active: true, RecordMetadata: newRecordMetadata(now)}
		a, err := newAuditEvent(m, "staff.created", "staff_member", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.dailyOps()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "daily_operations_unavailable", Message: "daily operations require PostgreSQL"})
		}
		if err := repo.CreateStaffMember(ctx, v, a); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/staff-members/"+v.ID)
	})
}
func (s *Server) handleStaff(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repo, available := s.dailyOps()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "daily_operations_unavailable", Message: "daily operations require PostgreSQL"})
		return
	}
	values, err := repo.StaffMembers(r.Context(), tenant)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}

type shiftInput struct {
	ID            string    `json:"id"`
	OutletID      string    `json:"outletId"`
	StaffMemberID string    `json:"staffMemberId"`
	StartsAt      time.Time `json:"startsAt"`
	EndsAt        time.Time `json:"endsAt"`
	StationID     string    `json:"stationId"`
}

func (s *Server) handleCreateShift(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input shiftInput
		if result := decodeAndValidate(p, &input, func() error {
			if !domain.ValidUUID(input.ID) || !domain.ValidUUID(input.OutletID) || !domain.ValidUUID(input.StaffMemberID) || input.StartsAt.IsZero() || !input.EndsAt.After(input.StartsAt) || (input.StationID != "" && !domain.ValidUUID(input.StationID)) {
				return fmt.Errorf("shift is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		v := domain.StaffShift{ID: input.ID, TenantID: m.TenantID, OutletID: m.OutletID, StaffMemberID: input.StaffMemberID, StartsAt: input.StartsAt, EndsAt: input.EndsAt, StationID: input.StationID, Status: "scheduled", RecordMetadata: newRecordMetadata(now)}
		a, err := newAuditEvent(m, "shift.created", "staff_shift", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.dailyOps()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "daily_operations_unavailable", Message: "daily operations require PostgreSQL"})
		}
		if err := repo.CreateShift(ctx, v, a); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/shifts/"+v.ID)
	})
}
func (s *Server) handleShifts(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, available := s.dailyOps()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "daily_operations_unavailable", Message: "daily operations require PostgreSQL"})
		return
	}
	values, err := repo.Shifts(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}
func (s *Server) handleShiftTransition(w http.ResponseWriter, r *http.Request) {
	s.simpleDailyTransition(w, r, "shift")
}

type taskInput struct {
	ID            string     `json:"id"`
	OutletID      string     `json:"outletId"`
	StaffMemberID string     `json:"staffMemberId"`
	Title         string     `json:"title"`
	DueAt         *time.Time `json:"dueAt"`
	Priority      string     `json:"priority"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input taskInput
		if result := decodeAndValidate(p, &input, func() error {
			if !domain.ValidUUID(input.ID) || !domain.ValidUUID(input.OutletID) || (input.StaffMemberID != "" && !domain.ValidUUID(input.StaffMemberID)) || strings.TrimSpace(input.Title) == "" || (input.Priority != "low" && input.Priority != "normal" && input.Priority != "high") {
				return fmt.Errorf("task is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		v := domain.OperationalTask{ID: input.ID, TenantID: m.TenantID, OutletID: m.OutletID, StaffMemberID: input.StaffMemberID, Title: strings.TrimSpace(input.Title), DueAt: input.DueAt, Priority: input.Priority, Status: "open", RecordMetadata: newRecordMetadata(now)}
		a, err := newAuditEvent(m, "task.created", "operational_task", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.dailyOps()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "daily_operations_unavailable", Message: "daily operations require PostgreSQL"})
		}
		if err := repo.CreateTask(ctx, v, a); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/tasks/"+v.ID)
	})
}
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, available := s.dailyOps()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "daily_operations_unavailable", Message: "daily operations require PostgreSQL"})
		return
	}
	values, err := repo.Tasks(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}
func (s *Server) handleTaskTransition(w http.ResponseWriter, r *http.Request) {
	s.simpleDailyTransition(w, r, "task")
}
func (s *Server) simpleDailyTransition(w http.ResponseWriter, r *http.Request, kind string) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input transitionInput
		if result := decodeAndValidate(p, &input, func() error {
			if input.ExpectedVersion < 1 {
				return fmt.Errorf("expectedVersion is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		id := r.PathValue("id")
		if !domain.ValidUUID(id) {
			return errorResult(apiError{Status: 422, Code: "validation_failed", Message: kind + " id must be a UUID"})
		}
		now := s.now().UTC()
		a, err := newAuditEvent(m, kind+"."+input.Status, kind, id, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.dailyOps()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "daily_operations_unavailable", Message: "daily operations require PostgreSQL"})
		}
		var value any
		if kind == "shift" {
			value, err = repo.TransitionShift(ctx, m.TenantID, m.OutletID, id, input.Status, input.ExpectedVersion, a)
		} else {
			value, err = repo.TransitionTask(ctx, m.TenantID, m.OutletID, id, input.Status, input.ExpectedVersion, a)
		}
		if err != nil {
			return repositoryError(err)
		}
		return successResult(200, value, "")
	})
}
