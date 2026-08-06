// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
	"net/http"
	"strings"
	"time"
)

func (s *Server) commerce() (store.CommerceRepository, bool) {
	v, ok := s.repository.(store.CommerceRepository)
	return v, ok
}
func (s *Server) handleAvailability(w http.ResponseWriter, r *http.Request) {
	t, o, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, a := s.commerce()
	if !a {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "commerce_unavailable", Message: "commerce requires PostgreSQL"})
		return
	}
	v, e := repo.MenuAvailability(r.Context(), t, o)
	if e != nil {
		writeReadRepositoryError(w, r, e)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, v)
}

type availabilityInput struct {
	MenuItemID string `json:"menuItemId"`
	Available  bool   `json:"available"`
	Reason     string `json:"reason"`
}

func (s *Server) handleSetAvailability(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in availabilityInput
		if result := decodeAndValidate(p, &in, func() error {
			if !domain.ValidUUID(in.MenuItemID) || len(in.Reason) > 300 {
				return fmt.Errorf("availability is invalid")
			}
			if !in.Available && strings.TrimSpace(in.Reason) == "" {
				return fmt.Errorf("reason is required when unavailable")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.commerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		a, e := newAuditEvent(m, "menu.availability_changed", "menu_item", in.MenuItemID, now)
		if e != nil {
			return internalOperationError()
		}
		v, e := repo.SetMenuAvailability(ctx, m.TenantID, m.OutletID, domain.MenuAvailability{MenuItemID: in.MenuItemID, Available: in.Available, Reason: strings.TrimSpace(in.Reason)}, a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(200, v, "")
	})
}

type tableInput struct {
	ID       string `json:"id"`
	OutletID string `json:"outletId"`
	Label    string `json:"label"`
	Section  string `json:"section"`
	Capacity int    `json:"capacity"`
}

func (s *Server) handleCreateTable(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in tableInput
		if result := decodeAndValidate(p, &in, func() error {
			if !domain.ValidUUID(in.ID) || !domain.ValidUUID(in.OutletID) || strings.TrimSpace(in.Label) == "" || in.Capacity < 1 {
				return fmt.Errorf("table is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		if in.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		v := domain.DiningTable{ID: in.ID, TenantID: m.TenantID, OutletID: m.OutletID, Label: strings.TrimSpace(in.Label), Section: strings.TrimSpace(in.Section), Capacity: in.Capacity, Status: "available", Version: 1, CreatedAt: now, UpdatedAt: now}
		a, e := newAuditEvent(m, "dining_table.created", "dining_table", v.ID, now)
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.commerce()
		if e := repo.CreateDiningTable(ctx, v, a); e != nil {
			return repositoryError(e)
		}
		return successResult(201, v, "/api/v1/dining-tables/"+v.ID)
	})
}
func (s *Server) handleTables(w http.ResponseWriter, r *http.Request) {
	t, o, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, a := s.commerce()
	if !a {
		return
	}
	v, e := repo.DiningTables(r.Context(), t, o)
	if e != nil {
		writeReadRepositoryError(w, r, e)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, v)
}

type tableTransitionInput struct {
	Status          string `json:"status"`
	ExpectedVersion uint64 `json:"expectedVersion"`
}

func (s *Server) handleTableTransition(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in tableTransitionInput
		if result := decodeAndValidate(p, &in, func() error {
			if (in.Status != "available" && in.Status != "disabled") || in.ExpectedVersion < 1 {
				return fmt.Errorf("table transition is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		id := r.PathValue("id")
		now := s.now().UTC()
		a, err := newAuditEvent(m, "dining_table.status_changed", "dining_table", id, now)
		if err != nil {
			return internalOperationError()
		}
		repo, ok := s.commerce()
		if !ok {
			return internalOperationError()
		}
		v, err := repo.TransitionDiningTable(ctx, m.TenantID, m.OutletID, id, in.Status, in.ExpectedVersion, a)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(200, v, "")
	})
}

type sessionInput struct {
	ID         string `json:"id"`
	OutletID   string `json:"outletId"`
	TableID    string `json:"tableId"`
	GuestCount int    `json:"guestCount"`
	GuestName  string `json:"guestName"`
}

func (s *Server) handleOpenSession(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in sessionInput
		if result := decodeAndValidate(p, &in, func() error {
			if !domain.ValidUUID(in.ID) || !domain.ValidUUID(in.OutletID) || !domain.ValidUUID(in.TableID) || in.GuestCount < 1 {
				return fmt.Errorf("session is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		if in.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		v := domain.DiningSession{ID: in.ID, TenantID: m.TenantID, OutletID: m.OutletID, TableID: in.TableID, Status: "open", GuestCount: in.GuestCount, GuestName: strings.TrimSpace(in.GuestName), OpenedAt: now, Version: 1}
		a, e := newAuditEvent(m, "dining_session.opened", "dining_session", v.ID, now)
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.commerce()
		v, e = repo.OpenDiningSession(ctx, v, a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(201, v, "/api/v1/dining-sessions/"+v.ID)
	})
}
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	t, o, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, _ := s.commerce()
	v, e := repo.DiningSessions(r.Context(), t, o)
	if e != nil {
		writeReadRepositoryError(w, r, e)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, v)
}

type versionInput struct {
	ExpectedVersion   uint64 `json:"expectedVersion"`
	ClosingCountMinor int64  `json:"closingCountMinor"`
}

func (s *Server) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in versionInput
		if result := decodeAndValidate(p, &in, func() error {
			if in.ExpectedVersion < 1 {
				return fmt.Errorf("expectedVersion is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		id := r.PathValue("id")
		now := s.now().UTC()
		a, e := newAuditEvent(m, "dining_session.closed", "dining_session", id, now)
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.commerce()
		v, e := repo.CloseDiningSession(ctx, m.TenantID, m.OutletID, id, in.ExpectedVersion, a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(200, v, "")
	})
}

type cashInput struct {
	ID                string `json:"id"`
	OutletID          string `json:"outletId"`
	RegisterLabel     string `json:"registerLabel"`
	OpeningFloatMinor int64  `json:"openingFloatMinor"`
}

func (s *Server) handleOpenCash(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in cashInput
		if result := decodeAndValidate(p, &in, func() error {
			if !domain.ValidUUID(in.ID) || !domain.ValidUUID(in.OutletID) || strings.TrimSpace(in.RegisterLabel) == "" || in.OpeningFloatMinor < 0 {
				return fmt.Errorf("cash shift is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		if in.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		v := domain.CashShift{ID: in.ID, TenantID: m.TenantID, OutletID: m.OutletID, RegisterLabel: in.RegisterLabel, Status: "open", OpeningFloatMinor: in.OpeningFloatMinor, ExpectedCashMinor: in.OpeningFloatMinor, OpenedAt: now, Version: 1}
		a, e := newAuditEvent(m, "cash_shift.opened", "cash_shift", v.ID, now)
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.commerce()
		if e := repo.OpenCashShift(ctx, v, a); e != nil {
			return repositoryError(e)
		}
		return successResult(201, v, "/api/v1/cash-shifts/"+v.ID)
	})
}
func (s *Server) handleCashShifts(w http.ResponseWriter, r *http.Request) {
	t, o, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, _ := s.commerce()
	v, e := repo.CashShifts(r.Context(), t, o)
	if e != nil {
		writeReadRepositoryError(w, r, e)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, v)
}
func (s *Server) handleCloseCash(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in versionInput
		if result := decodeAndValidate(p, &in, func() error {
			if in.ExpectedVersion < 1 || in.ClosingCountMinor < 0 {
				return fmt.Errorf("closing count is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		id := r.PathValue("id")
		now := s.now().UTC()
		a, e := newAuditEvent(m, "cash_shift.closed", "cash_shift", id, now)
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.commerce()
		v, e := repo.CloseCashShift(ctx, m.TenantID, m.OutletID, id, in.ExpectedVersion, in.ClosingCountMinor, a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(200, v, "")
	})
}

type tenderInput struct {
	ID                string `json:"id"`
	OutletID          string `json:"outletId"`
	OrderID           string `json:"orderId"`
	CashShiftID       string `json:"cashShiftId"`
	TenderType        string `json:"tenderType"`
	AmountMinor       int64  `json:"amountMinor"`
	ProviderReference string `json:"providerReference"`
	ReceiptID         string `json:"receiptId"`
	ReceiptNumber     string `json:"receiptNumber"`
}

func (s *Server) handleCaptureTender(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in tenderInput
		if result := decodeAndValidate(p, &in, func() error {
			if !domain.ValidUUID(in.ID) || !domain.ValidUUID(in.OutletID) || !domain.ValidUUID(in.OrderID) || !domain.ValidUUID(in.ReceiptID) || in.AmountMinor < 1 {
				return fmt.Errorf("tender is invalid")
			}
			if in.TenderType != "cash" && in.TenderType != "upi" && in.TenderType != "card_terminal" && in.TenderType != "external" {
				return fmt.Errorf("tender type is invalid")
			}
			if in.TenderType == "cash" && !domain.ValidUUID(in.CashShiftID) {
				return fmt.Errorf("cash shift is required")
			}
			return nil
		}); result != nil {
			return *result
		}
		if in.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		v := domain.Tender{ID: in.ID, TenantID: m.TenantID, OutletID: m.OutletID, OrderID: in.OrderID, CashShiftID: in.CashShiftID, TenderType: in.TenderType, AmountMinor: in.AmountMinor, Currency: "INR", ProviderReference: strings.TrimSpace(in.ProviderReference), Status: "captured", OccurredAt: now}
		receipt := domain.FiscalReceipt{ID: in.ReceiptID, OrderID: in.OrderID, ReceiptNumber: in.ReceiptNumber, IssuedAt: now}
		a, e := newAuditEvent(m, "tender.captured", "tender", v.ID, now)
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.commerce()
		tender, issued, e := repo.CaptureTender(ctx, v, receipt, a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(201, map[string]any{"tender": tender, "receipt": issued}, "")
	})
}

type settlementInput struct {
	BusinessDate string `json:"businessDate"`
}

func (s *Server) handleSettle(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in settlementInput
		if result := decodeAndValidate(p, &in, func() error {
			if _, e := time.Parse("2006-01-02", in.BusinessDate); e != nil {
				return fmt.Errorf("businessDate is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		now := s.now().UTC()
		a, e := newAuditEvent(m, "settlement.generated", "tender_settlement", m.ID, now)
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.commerce()
		v, e := repo.GenerateSettlements(ctx, m.TenantID, m.OutletID, in.BusinessDate, a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(201, v, "")
	})
}
