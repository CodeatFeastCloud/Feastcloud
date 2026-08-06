// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/auth"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "feastcloud-core",
		"version": s.version,
		"time":    s.now().UTC(),
	})
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if s.requireSyncReady {
		if err := s.syncRepository.Ready(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "not_ready", "service": "feastcloud-core", "version": s.version,
				"dependency": "postgresql_sync",
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ready",
		"service": "feastcloud-core",
		"version": s.version,
	})
}

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, map[string]any{
		"version": "v1",
		"resources": []string{
			"organizations",
			"outlets",
			"brands",
			"stations",
			"orders",
			"kitchen-tickets",
			"audit-events",
			"units",
			"ingredients",
			"recipes",
			"menu-items",
			"inventory-events",
			"inventory-counts",
			"inventory-summary",
			"production-batches",
			"order-imports",
			"planning-runs",
			"dashboard/daily",
			"configuration-snapshots", "edge-checkpoints", "reconciliation-cases", "incidents", "backup-evidence", "restore-drills",
			"suppliers", "purchase-orders", "goods-receipts", "temperature-logs", "checklists", "staff-members", "shifts", "tasks",
			"menu-availability", "dining-tables", "dining-sessions", "cash-shifts", "tenders", "tender-settlements",
			"guests", "reservations", "promotions", "loyalty-accounts",
			"devices",
			"sync/operations",
		},
	})
}

func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input deviceInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != metadata.OutletID {
			return outletScopeMismatch()
		}
		administration, ok := s.repository.(store.DeviceAdministration)
		if !ok {
			return errorResult(apiError{Status: http.StatusNotImplemented, Code: "device_registry_unavailable", Message: "durable device enrollment requires PostgreSQL"})
		}
		now := s.now().UTC()
		value := auth.Device{TenantID: metadata.TenantID, OutletID: input.OutletID, EdgeID: strings.TrimSpace(input.EdgeID), DeviceID: input.ID, Fingerprint: input.CertificateFingerprint, Status: "active"}
		audit, err := newAuditEvent(metadata, "device.enrolled", "identity_device", input.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := administration.RegisterDevice(ctx, value, strings.TrimSpace(input.Name), audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/devices/"+input.ID)
	})
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input revokeDeviceInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		deviceID := r.PathValue("id")
		if !domain.ValidUUID(deviceID) {
			return errorResult(apiError{Status: http.StatusUnprocessableEntity, Code: "invalid_device_id", Message: "device id must be a UUID string"})
		}
		administration, ok := s.repository.(store.DeviceAdministration)
		if !ok {
			return errorResult(apiError{Status: http.StatusNotImplemented, Code: "device_registry_unavailable", Message: "durable device revocation requires PostgreSQL"})
		}
		now := s.now().UTC()
		audit, err := newAuditEvent(metadata, "device.revoked", "identity_device", deviceID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := administration.RevokeDevice(ctx, metadata.TenantID, deviceID, metadata.ActorID, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusOK, map[string]any{"id": deviceID, "status": "revoked", "reason": strings.TrimSpace(input.Reason)}, "")
	})
}

func (s *Server) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input organizationInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.ID != metadata.TenantID {
			return errorResult(apiError{
				Status:  http.StatusUnprocessableEntity,
				Code:    "tenant_scope_mismatch",
				Message: "organization id must match envelope tenantId",
			})
		}
		now := s.now().UTC()
		value := domain.Organization{
			ID:              input.ID,
			TenantID:        metadata.TenantID,
			Name:            strings.TrimSpace(input.Name),
			LegalName:       strings.TrimSpace(input.LegalName),
			DefaultLocale:   input.DefaultLocale,
			DefaultCurrency: input.DefaultCurrency,
			Active:          boolDefaultTrue(input.Active),
			RecordMetadata:  newRecordMetadata(now),
		}
		audit, err := newAuditEvent(metadata, "organization.created", "organization", value.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := s.repository.CreateOrganization(ctx, value, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/organizations/"+value.ID)
	})
}

func (s *Server) handleCreateOutlet(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input outletInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		now := s.now().UTC()
		value := domain.Outlet{
			ID:             input.ID,
			TenantID:       metadata.TenantID,
			OrganizationID: input.OrganizationID,
			Name:           strings.TrimSpace(input.Name),
			Code:           input.Code,
			TimeZone:       input.TimeZone,
			Currency:       input.Currency,
			Active:         boolDefaultTrue(input.Active),
			RecordMetadata: newRecordMetadata(now),
		}
		audit, err := newAuditEvent(metadata, "outlet.created", "outlet", value.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := s.repository.CreateOutlet(ctx, value, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/outlets/"+value.ID)
	})
}

func (s *Server) handleCreateBrand(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input brandInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		now := s.now().UTC()
		value := domain.Brand{
			ID:             input.ID,
			TenantID:       metadata.TenantID,
			OrganizationID: input.OrganizationID,
			Name:           strings.TrimSpace(input.Name),
			Code:           input.Code,
			Active:         boolDefaultTrue(input.Active),
			RecordMetadata: newRecordMetadata(now),
		}
		audit, err := newAuditEvent(metadata, "brand.created", "brand", value.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := s.repository.CreateBrand(ctx, value, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/brands/"+value.ID)
	})
}

func (s *Server) handleCreateStation(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input stationInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != metadata.OutletID {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		value := domain.Station{
			ID:             input.ID,
			TenantID:       metadata.TenantID,
			OutletID:       input.OutletID,
			Name:           strings.TrimSpace(input.Name),
			Code:           input.Code,
			Type:           input.Type,
			Active:         boolDefaultTrue(input.Active),
			RecordMetadata: newRecordMetadata(now),
		}
		audit, err := newAuditEvent(metadata, "station.created", "station", value.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := s.repository.CreateStation(ctx, value, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/stations/"+value.ID)
	})
}

func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input orderInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != metadata.OutletID {
			return outletScopeMismatch()
		}
		status := input.Status
		if status == "" {
			status = domain.OrderStatusReceived
		}
		lines := make([]domain.OrderLine, len(input.Lines))
		for index, line := range input.Lines {
			lines[index] = domain.OrderLine{
				ID:              line.ID,
				MenuItemID:      line.MenuItemID,
				Name:            strings.TrimSpace(line.Name),
				Quantity:        line.Quantity,
				UnitPrice:       line.UnitPrice,
				LineTotal:       line.LineTotal,
				PreparationNote: line.PreparationNote,
			}
		}
		now := s.now().UTC()
		value := domain.Order{
			ID:             input.ID,
			TenantID:       metadata.TenantID,
			OutletID:       input.OutletID,
			BrandID:        input.BrandID,
			ExternalRef:    input.ExternalRef,
			Type:           input.Type,
			Status:         status,
			Lines:          lines,
			Subtotal:       input.Subtotal,
			DiscountTotal:  input.DiscountTotal,
			TaxTotal:       input.TaxTotal,
			ServiceCharge:  input.ServiceCharge,
			Total:          input.Total,
			PlacedAt:       input.PlacedAt.UTC(),
			RecordMetadata: newRecordMetadata(now),
		}
		audit, err := newAuditEvent(metadata, "order.created", "order", value.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := s.repository.CreateOrder(ctx, value, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/orders/"+value.ID)
	})
}

func (s *Server) handleCreateKitchenTicket(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input ticketInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != metadata.OutletID {
			return outletScopeMismatch()
		}
		status := input.Status
		if status == "" {
			status = domain.TicketStatusQueued
		}
		now := s.now().UTC()
		value := domain.KitchenTicket{
			ID:             input.ID,
			TenantID:       metadata.TenantID,
			OutletID:       input.OutletID,
			OrderID:        input.OrderID,
			StationID:      input.StationID,
			LineIDs:        append([]string(nil), input.LineIDs...),
			Status:         status,
			Priority:       input.Priority,
			TargetAt:       input.TargetAt,
			RecordMetadata: newRecordMetadata(now),
		}
		audit, err := newAuditEvent(metadata, "kitchen_ticket.created", "kitchen_ticket", value.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := s.repository.CreateKitchenTicket(ctx, value, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/kitchen-tickets/"+value.ID)
	})
}

func (s *Server) handleOrganizations(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	values, err := s.repository.Organizations(r.Context(), tenantID)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writePaginated(w, r, values, func(value domain.Organization) string { return value.ID })
}

func (s *Server) handleOrganization(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	value, err := s.repository.Organization(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	w.Header().Set("ETag", entityETag(value.Version))
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}

func (s *Server) handleOutlets(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	values, err := s.repository.Outlets(r.Context(), tenantID)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	organizationID := r.URL.Query().Get("organizationId")
	values = filterSlice(values, func(value domain.Outlet) bool {
		return organizationID == "" || value.OrganizationID == organizationID
	})
	writePaginated(w, r, values, func(value domain.Outlet) string { return value.ID })
}

func (s *Server) handleOutlet(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	value, err := s.repository.Outlet(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	w.Header().Set("ETag", entityETag(value.Version))
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}

func (s *Server) handleBrands(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	values, err := s.repository.Brands(r.Context(), tenantID)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	organizationID := r.URL.Query().Get("organizationId")
	values = filterSlice(values, func(value domain.Brand) bool {
		return organizationID == "" || value.OrganizationID == organizationID
	})
	writePaginated(w, r, values, func(value domain.Brand) string { return value.ID })
}

func (s *Server) handleBrand(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	value, err := s.repository.Brand(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	w.Header().Set("ETag", entityETag(value.Version))
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}

func (s *Server) handleBrandOutletAssignments(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	values, err := s.repository.BrandOutletAssignments(r.Context(), tenantID)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	brandID, outletID := r.URL.Query().Get("brandId"), r.URL.Query().Get("outletId")
	values = filterSlice(values, func(value domain.BrandOutletAssignment) bool {
		return (brandID == "" || value.BrandID == brandID) && (outletID == "" || value.OutletID == outletID)
	})
	writePaginated(w, r, values, func(value domain.BrandOutletAssignment) string { return value.BrandID + ":" + value.OutletID })
}

func (s *Server) handleSetBrandOutletAssignment(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input brandOutletAssignmentInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != metadata.OutletID {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		value := domain.BrandOutletAssignment{
			TenantID:       metadata.TenantID,
			BrandID:        input.BrandID,
			OutletID:       input.OutletID,
			Active:         input.Active,
			RecordMetadata: newRecordMetadata(now),
		}
		audit, err := newAuditEvent(metadata, "brand_outlet_assignment.saved", "brand_outlet_assignment", value.BrandID, now)
		if err != nil {
			return internalOperationError()
		}
		saved, err := s.repository.SetBrandOutletAssignment(ctx, value, input.ExpectedVersion, audit)
		if err != nil {
			return repositoryError(err)
		}
		status := http.StatusOK
		if input.ExpectedVersion == 0 {
			status = http.StatusCreated
		}
		return successResult(status, saved, "")
	})
}

func (s *Server) handleStations(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	values, err := s.repository.Stations(r.Context(), tenantID)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	outletID := r.URL.Query().Get("outletId")
	values = filterSlice(values, func(value domain.Station) bool {
		return outletID == "" || value.OutletID == outletID
	})
	writePaginated(w, r, values, func(value domain.Station) string { return value.ID })
}

func (s *Server) handleStation(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	value, err := s.repository.Station(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	w.Header().Set("ETag", entityETag(value.Version))
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}

func (s *Server) handleOrders(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	outletID := r.URL.Query().Get("outletId")
	if pager, ok := s.repository.(store.OperationalPager); ok {
		limit, after, valid := parseDatabasePage(w, r, "orders|outlet="+outletID)
		if !valid {
			return
		}
		page, err := pager.PageOrders(r.Context(), store.OrderPageRequest{TenantID: tenantID, OutletID: outletID, Limit: limit, After: after})
		if err != nil {
			writeReadRepositoryError(w, r, err)
			return
		}
		writeDatabasePage(w, r, page.Values, page.Next, "orders|outlet="+outletID, limit)
		return
	}
	values, err := s.repository.Orders(r.Context(), tenantID)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	values = filterSlice(values, func(value domain.Order) bool {
		return outletID == "" || value.OutletID == outletID
	})
	writePaginated(w, r, values, func(value domain.Order) string { return value.ID })
}

func (s *Server) handleOrder(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	value, err := s.repository.Order(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	w.Header().Set("ETag", entityETag(value.Version))
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}

func (s *Server) handleTransitionOrder(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input orderTransitionInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if metadata.OutletID == "" {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		audit, err := newAuditEvent(metadata, "order.status_changed", "order", r.PathValue("id"), now)
		if err != nil {
			return internalOperationError()
		}
		value, err := s.repository.TransitionOrder(ctx, metadata.TenantID, metadata.OutletID, r.PathValue("id"), input.ToStatus, input.ExpectedVersion, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusOK, value, "")
	})
}

func (s *Server) handleKitchenTickets(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	outletID := r.URL.Query().Get("outletId")
	orderID := r.URL.Query().Get("orderId")
	stationID := r.URL.Query().Get("stationId")
	queryKey := "tickets|outlet=" + outletID + "|order=" + orderID + "|station=" + stationID
	if pager, ok := s.repository.(store.OperationalPager); ok {
		limit, after, valid := parseDatabasePage(w, r, queryKey)
		if !valid {
			return
		}
		page, err := pager.PageKitchenTickets(r.Context(), store.TicketPageRequest{TenantID: tenantID, OutletID: outletID, OrderID: orderID, StationID: stationID, Limit: limit, After: after})
		if err != nil {
			writeReadRepositoryError(w, r, err)
			return
		}
		writeDatabasePage(w, r, page.Values, page.Next, queryKey, limit)
		return
	}
	values, err := s.repository.KitchenTickets(r.Context(), tenantID)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	values = filterSlice(values, func(value domain.KitchenTicket) bool {
		return (outletID == "" || value.OutletID == outletID) &&
			(orderID == "" || value.OrderID == orderID) &&
			(stationID == "" || value.StationID == stationID)
	})
	writePaginated(w, r, values, func(value domain.KitchenTicket) string { return value.ID })
}

func (s *Server) handleKitchenTicket(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	value, err := s.repository.KitchenTicket(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	w.Header().Set("ETag", entityETag(value.Version))
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}

func (s *Server) handleTransitionKitchenTicket(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input ticketTransitionInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if metadata.OutletID == "" {
			return outletScopeMismatch()
		}
		now := s.now().UTC()
		audit, err := newAuditEvent(metadata, "kitchen_ticket.status_changed", "kitchen_ticket", r.PathValue("id"), now)
		if err != nil {
			return internalOperationError()
		}
		value, err := s.repository.TransitionKitchenTicket(ctx, metadata.TenantID, metadata.OutletID, r.PathValue("id"), input.ToStatus, input.ExpectedVersion, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusOK, value, "")
	})
}

func (s *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	values, err := s.repository.AuditEvents(r.Context(), store.AuditFilter{
		TenantID:   tenantID,
		OutletID:   r.URL.Query().Get("outletId"),
		EntityType: r.URL.Query().Get("entityType"),
		EntityID:   r.URL.Query().Get("entityId"),
	})
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writePaginated(w, r, values, func(value domain.AuditEvent) string { return value.ID })
}

func decodeAndValidate(payload json.RawMessage, destination any, validate func() error) *idempotency.Result {
	if err := decodeStrict(payload, destination); err != nil {
		result := errorResult(apiError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "invalid_payload",
			Message: err.Error(),
		})
		return &result
	}
	if err := validate(); err != nil {
		result := errorResult(apiError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "validation_failed",
			Message: err.Error(),
		})
		return &result
	}
	return nil
}

func newRecordMetadata(now time.Time) domain.RecordMetadata {
	return domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}
}

func newAuditEvent(
	metadata domain.MutationMetadata,
	action string,
	entityType string,
	entityID string,
	now time.Time,
) (domain.AuditEvent, error) {
	id, err := newUUIDv7(now)
	if err != nil {
		return domain.AuditEvent{}, err
	}
	return domain.AuditEvent{
		ID:             id,
		OperationID:    metadata.ID,
		TenantID:       metadata.TenantID,
		OutletID:       metadata.OutletID,
		ActorID:        metadata.ActorID,
		DeviceID:       metadata.DeviceID,
		Source:         metadata.Source,
		SourceID:       metadata.SourceID,
		IdempotencyKey: metadata.IdempotencyKey,
		CorrelationID:  metadata.CorrelationID,
		SchemaVersion:  metadata.SchemaVersion,
		Action:         action,
		EntityType:     entityType,
		EntityID:       entityID,
		OccurredAt:     metadata.OccurredAt,
		RecordedAt:     now,
	}, nil
}

func outletScopeMismatch() idempotency.Result {
	return errorResult(apiError{
		Status:  http.StatusUnprocessableEntity,
		Code:    "outlet_scope_mismatch",
		Message: "payload.outletId must match envelope outletId",
	})
}

func internalOperationError() idempotency.Result {
	return errorResult(apiError{
		Status:    http.StatusInternalServerError,
		Code:      "identifier_generation_failed",
		Message:   "the service could not generate a secure identifier",
		Retryable: true,
	})
}

func requireTenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	requestPrincipal, ok := principalFrom(r.Context())
	if !ok {
		writeError(w, requestIDFrom(r.Context()), apiError{
			Status:  http.StatusUnauthorized,
			Code:    "authentication_required",
			Message: "an authenticated principal is required",
		})
		return "", false
	}
	queryTenantID := strings.TrimSpace(r.URL.Query().Get("tenantId"))
	if queryTenantID != "" && queryTenantID != requestPrincipal.TenantID {
		writeError(w, requestIDFrom(r.Context()), apiError{
			Status:  http.StatusForbidden,
			Code:    "tenant_scope_mismatch",
			Message: "tenantId query parameter must match the authenticated tenant",
		})
		return "", false
	}
	return requestPrincipal.TenantID, true
}

func writeReadRepositoryError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, requestIDFrom(r.Context()), apiError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "the requested resource was not found",
		})
		return
	}
	writeError(w, requestIDFrom(r.Context()), apiError{
		Status:    http.StatusInternalServerError,
		Code:      "persistence_error",
		Message:   "the requested data could not be read",
		Retryable: true,
	})
}

type pageMeta struct {
	Limit      int    `json:"limit"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type databaseCursor struct {
	Version   int       `json:"v"`
	Query     string    `json:"q"`
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"id"`
}

func parseDatabasePage(w http.ResponseWriter, r *http.Request, query string) (int, *store.PageCursor, bool) {
	requestID := requestIDFrom(r.Context())
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(w, requestID, apiError{Status: http.StatusBadRequest, Code: "invalid_page_limit", Message: "limit must be an integer between 1 and 200"})
			return 0, nil, false
		}
		limit = parsed
	}
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return limit, nil, true
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	var cursor databaseCursor
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Version != 2 || cursor.Query != query || cursor.CreatedAt.IsZero() || !domain.ValidUUID(cursor.ID) {
		writeError(w, requestID, invalidCursorError())
		return limit, nil, false
	}
	return limit, &store.PageCursor{CreatedAt: cursor.CreatedAt, ID: cursor.ID}, true
}
func writeDatabasePage[T any](w http.ResponseWriter, r *http.Request, values []T, next *store.PageCursor, query string, limit int) {
	nextCursor := ""
	if next != nil {
		encoded, _ := json.Marshal(databaseCursor{Version: 2, Query: query, CreatedAt: next.CreatedAt, ID: next.ID})
		nextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	writeJSON(w, http.StatusOK, collectionEnvelope{Data: values, Meta: collectionMeta{RequestID: requestIDFrom(r.Context()), Page: pageMeta{Limit: limit, NextCursor: nextCursor}}})
}

type collectionMeta struct {
	RequestID string   `json:"requestId"`
	Page      pageMeta `json:"page"`
}

type collectionEnvelope struct {
	Data any            `json:"data"`
	Meta collectionMeta `json:"meta"`
}

func writePaginated[T any](w http.ResponseWriter, r *http.Request, values []T, identifier func(T) string) {
	requestID := requestIDFrom(r.Context())
	limit := 50
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(w, requestID, apiError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_page_limit",
				Message: "limit must be an integer between 1 and 200",
			})
			return
		}
		limit = parsed
	}
	start := 0
	if rawCursor := r.URL.Query().Get("cursor"); rawCursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(rawCursor)
		if err != nil || !strings.HasPrefix(string(decoded), "v1:") {
			writeError(w, requestID, invalidCursorError())
			return
		}
		lastID := strings.TrimPrefix(string(decoded), "v1:")
		found := false
		for index, value := range values {
			if identifier(value) == lastID {
				start = index + 1
				found = true
				break
			}
		}
		if !found {
			writeError(w, requestID, invalidCursorError())
			return
		}
	}
	end := start + limit
	if end > len(values) {
		end = len(values)
	}
	page := values[start:end]
	nextCursor := ""
	if end < len(values) && len(page) > 0 {
		nextCursor = base64.RawURLEncoding.EncodeToString([]byte("v1:" + identifier(page[len(page)-1])))
	}
	writeJSON(w, http.StatusOK, collectionEnvelope{
		Data: page,
		Meta: collectionMeta{
			RequestID: requestID,
			Page:      pageMeta{Limit: limit, NextCursor: nextCursor},
		},
	})
}

func invalidCursorError() apiError {
	return apiError{
		Status:  http.StatusBadRequest,
		Code:    "invalid_cursor",
		Message: "cursor is invalid or no longer available for this query",
	}
}

func filterSlice[T any](values []T, keep func(T) bool) []T {
	filtered := make([]T, 0, len(values))
	for _, value := range values {
		if keep(value) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func entityETag(version uint64) string {
	return fmt.Sprintf("\"%d\"", version)
}
