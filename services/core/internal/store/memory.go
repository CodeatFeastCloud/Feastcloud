// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

// MemoryRepository is a concurrency-safe, process-local repository intended for
// development, tests, and the Phase 0 offline demonstration. It is not durable.
type MemoryRepository struct {
	mu              sync.RWMutex
	organizations   map[string]domain.Organization
	outlets         map[string]domain.Outlet
	brands          map[string]domain.Brand
	brandOutlets    map[string]domain.BrandOutletAssignment
	stations        map[string]domain.Station
	units           map[string]domain.Unit
	ingredients     map[string]domain.Ingredient
	recipes         map[string]domain.Recipe
	menuItems       map[string]domain.MenuItem
	menuStudios     map[string]domain.MenuStudio
	menuImports     map[string]domain.MenuImportDraft
	orders          map[string]domain.Order
	tickets         map[string]domain.KitchenTicket
	audits          []domain.AuditEvent
	auditOperations map[string]string
	orderSources    map[string]string
}

// NewMemoryRepository returns an empty repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		organizations:   make(map[string]domain.Organization),
		outlets:         make(map[string]domain.Outlet),
		brands:          make(map[string]domain.Brand),
		brandOutlets:    make(map[string]domain.BrandOutletAssignment),
		stations:        make(map[string]domain.Station),
		units:           make(map[string]domain.Unit),
		ingredients:     make(map[string]domain.Ingredient),
		recipes:         make(map[string]domain.Recipe),
		menuItems:       make(map[string]domain.MenuItem),
		menuStudios:     make(map[string]domain.MenuStudio),
		menuImports:     make(map[string]domain.MenuImportDraft),
		orders:          make(map[string]domain.Order),
		tickets:         make(map[string]domain.KitchenTicket),
		audits:          make([]domain.AuditEvent, 0),
		auditOperations: make(map[string]string),
		orderSources:    make(map[string]string),
	}
}

func (m *MemoryRepository) CreateOrganization(ctx context.Context, value domain.Organization, audit domain.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.organizations[value.ID]; exists {
		return fmt.Errorf("%w: organization %q", ErrConflict, value.ID)
	}
	if err := validateAudit(audit, value.TenantID, audit.OutletID, "organization", value.ID); err != nil {
		return err
	}
	if err := m.ensureAuditOperationAvailableLocked(audit); err != nil {
		return err
	}
	m.organizations[value.ID] = value
	m.appendAuditLocked(audit)
	return nil
}

// ProvisionTenant mirrors the durable provisioning transaction for local and
// API tests. One audit event represents the single operator command.
func (m *MemoryRepository) ProvisionTenant(ctx context.Context, organization domain.Organization, outlet domain.Outlet, brand domain.Brand, assignment domain.BrandOutletAssignment, stations []domain.Station, audit domain.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.organizations[organization.ID]; exists {
		return fmt.Errorf("%w: organization %q", ErrConflict, organization.ID)
	}
	if _, exists := m.outlets[outlet.ID]; exists {
		return fmt.Errorf("%w: outlet %q", ErrConflict, outlet.ID)
	}
	if _, exists := m.brands[brand.ID]; exists {
		return fmt.Errorf("%w: brand %q", ErrConflict, brand.ID)
	}
	if organization.ID != organization.TenantID || outlet.TenantID != organization.TenantID || outlet.OrganizationID != organization.ID || brand.TenantID != organization.TenantID || brand.OrganizationID != organization.ID || assignment.TenantID != organization.TenantID || assignment.BrandID != brand.ID || assignment.OutletID != outlet.ID {
		return fmt.Errorf("%w: invalid tenant provisioning hierarchy", ErrInvalidReference)
	}
	if err := validateAudit(audit, organization.TenantID, outlet.ID, "organization", organization.ID); err != nil {
		return err
	}
	if err := m.ensureAuditOperationAvailableLocked(audit); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, station := range stations {
		if seen[station.ID] {
			return fmt.Errorf("%w: station %q", ErrConflict, station.ID)
		}
		seen[station.ID] = true
		if _, exists := m.stations[station.ID]; exists {
			return fmt.Errorf("%w: station %q", ErrConflict, station.ID)
		}
		if station.TenantID != organization.TenantID || station.OutletID != outlet.ID {
			return fmt.Errorf("%w: station hierarchy", ErrInvalidReference)
		}
	}
	m.organizations[organization.ID] = organization
	m.outlets[outlet.ID] = outlet
	m.brands[brand.ID] = brand
	m.brandOutlets[brandOutletKey(assignment.TenantID, assignment.BrandID, assignment.OutletID)] = assignment
	for _, station := range stations {
		m.stations[station.ID] = station
	}
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) Organization(ctx context.Context, tenantID, id string) (domain.Organization, error) {
	if err := ctx.Err(); err != nil {
		return domain.Organization{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.organizations[id]
	if !ok || value.TenantID != tenantID {
		return domain.Organization{}, fmt.Errorf("%w: organization %q", ErrNotFound, id)
	}
	return value, nil
}

func (m *MemoryRepository) Organizations(ctx context.Context, tenantID string) ([]domain.Organization, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.Organization, 0)
	for _, value := range m.organizations {
		if value.TenantID == tenantID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values, nil
}

func (m *MemoryRepository) CreateOutlet(ctx context.Context, value domain.Outlet, audit domain.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.outlets[value.ID]; exists {
		return fmt.Errorf("%w: outlet %q", ErrConflict, value.ID)
	}
	organization, exists := m.organizations[value.OrganizationID]
	if !exists || organization.TenantID != value.TenantID {
		return fmt.Errorf("%w: organization %q", ErrInvalidReference, value.OrganizationID)
	}
	if err := validateAudit(audit, value.TenantID, audit.OutletID, "outlet", value.ID); err != nil {
		return err
	}
	if err := m.ensureAuditOperationAvailableLocked(audit); err != nil {
		return err
	}
	m.outlets[value.ID] = value
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) Outlet(ctx context.Context, tenantID, id string) (domain.Outlet, error) {
	if err := ctx.Err(); err != nil {
		return domain.Outlet{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.outlets[id]
	if !ok || value.TenantID != tenantID {
		return domain.Outlet{}, fmt.Errorf("%w: outlet %q", ErrNotFound, id)
	}
	return value, nil
}

func (m *MemoryRepository) Outlets(ctx context.Context, tenantID string) ([]domain.Outlet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.Outlet, 0)
	for _, value := range m.outlets {
		if value.TenantID == tenantID {
			values = append(values, value)
		}
	}
	sortOutlets(values)
	return values, nil
}

func (m *MemoryRepository) CreateBrand(ctx context.Context, value domain.Brand, audit domain.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.brands[value.ID]; exists {
		return fmt.Errorf("%w: brand %q", ErrConflict, value.ID)
	}
	organization, exists := m.organizations[value.OrganizationID]
	if !exists || organization.TenantID != value.TenantID {
		return fmt.Errorf("%w: organization %q", ErrInvalidReference, value.OrganizationID)
	}
	if err := validateAudit(audit, value.TenantID, audit.OutletID, "brand", value.ID); err != nil {
		return err
	}
	if err := m.ensureAuditOperationAvailableLocked(audit); err != nil {
		return err
	}
	m.brands[value.ID] = value
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) Brand(ctx context.Context, tenantID, id string) (domain.Brand, error) {
	if err := ctx.Err(); err != nil {
		return domain.Brand{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.brands[id]
	if !ok || value.TenantID != tenantID {
		return domain.Brand{}, fmt.Errorf("%w: brand %q", ErrNotFound, id)
	}
	return value, nil
}

func (m *MemoryRepository) Brands(ctx context.Context, tenantID string) ([]domain.Brand, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.Brand, 0)
	for _, value := range m.brands {
		if value.TenantID == tenantID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values, nil
}

func brandOutletKey(tenantID, brandID, outletID string) string {
	return tenantID + ":" + brandID + ":" + outletID
}

func (m *MemoryRepository) SetBrandOutletAssignment(ctx context.Context, value domain.BrandOutletAssignment, expectedVersion uint64, audit domain.AuditEvent) (domain.BrandOutletAssignment, error) {
	if err := ctx.Err(); err != nil {
		return domain.BrandOutletAssignment{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	brand, brandExists := m.brands[value.BrandID]
	outlet, outletExists := m.outlets[value.OutletID]
	if !brandExists || !outletExists || brand.TenantID != value.TenantID || outlet.TenantID != value.TenantID || brand.OrganizationID != outlet.OrganizationID {
		return domain.BrandOutletAssignment{}, fmt.Errorf("%w: brand or outlet", ErrInvalidReference)
	}
	key := brandOutletKey(value.TenantID, value.BrandID, value.OutletID)
	current, exists := m.brandOutlets[key]
	if exists && expectedVersion == 0 {
		return domain.BrandOutletAssignment{}, fmt.Errorf("%w: brand outlet assignment", ErrConflict)
	}
	if !exists && expectedVersion != 0 {
		return domain.BrandOutletAssignment{}, fmt.Errorf("%w: brand outlet assignment", ErrNotFound)
	}
	if exists && current.Version != expectedVersion {
		return domain.BrandOutletAssignment{}, fmt.Errorf("%w: brand outlet assignment", ErrVersionConflict)
	}
	if err := validateAudit(audit, value.TenantID, value.OutletID, "brand_outlet_assignment", value.BrandID); err != nil {
		return domain.BrandOutletAssignment{}, err
	}
	if err := m.ensureAuditOperationAvailableLocked(audit); err != nil {
		return domain.BrandOutletAssignment{}, err
	}
	if exists {
		value.Version = current.Version + 1
		value.CreatedAt = current.CreatedAt
	} else {
		value.Version = 1
	}
	m.brandOutlets[key] = value
	m.appendAuditLocked(audit)
	return value, nil
}

func (m *MemoryRepository) BrandOutletAssignments(ctx context.Context, tenantID string) ([]domain.BrandOutletAssignment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.BrandOutletAssignment, 0)
	for _, value := range m.brandOutlets {
		if value.TenantID == tenantID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			if values[i].BrandID == values[j].BrandID {
				return values[i].OutletID < values[j].OutletID
			}
			return values[i].BrandID < values[j].BrandID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values, nil
}

func (m *MemoryRepository) CreateStation(ctx context.Context, value domain.Station, audit domain.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.stations[value.ID]; exists {
		return fmt.Errorf("%w: station %q", ErrConflict, value.ID)
	}
	outlet, exists := m.outlets[value.OutletID]
	if !exists || outlet.TenantID != value.TenantID {
		return fmt.Errorf("%w: outlet %q", ErrInvalidReference, value.OutletID)
	}
	if err := validateAudit(audit, value.TenantID, value.OutletID, "station", value.ID); err != nil {
		return err
	}
	if err := m.ensureAuditOperationAvailableLocked(audit); err != nil {
		return err
	}
	m.stations[value.ID] = value
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) Station(ctx context.Context, tenantID, id string) (domain.Station, error) {
	if err := ctx.Err(); err != nil {
		return domain.Station{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.stations[id]
	if !ok || value.TenantID != tenantID {
		return domain.Station{}, fmt.Errorf("%w: station %q", ErrNotFound, id)
	}
	return value, nil
}

func (m *MemoryRepository) Stations(ctx context.Context, tenantID string) ([]domain.Station, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.Station, 0)
	for _, value := range m.stations {
		if value.TenantID == tenantID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values, nil
}

func (m *MemoryRepository) CreateOrder(ctx context.Context, value domain.Order, audit domain.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.orders[value.ID]; exists {
		return fmt.Errorf("%w: order %q", ErrConflict, value.ID)
	}
	outlet, exists := m.outlets[value.OutletID]
	if !exists || outlet.TenantID != value.TenantID {
		return fmt.Errorf("%w: outlet %q", ErrInvalidReference, value.OutletID)
	}
	if value.BrandID != "" {
		brand, exists := m.brands[value.BrandID]
		if !exists || brand.TenantID != value.TenantID || brand.OrganizationID != outlet.OrganizationID {
			return fmt.Errorf("%w: brand %q", ErrInvalidReference, value.BrandID)
		}
	}
	if err := validateAudit(audit, value.TenantID, value.OutletID, "order", value.ID); err != nil {
		return err
	}
	if err := m.ensureAuditOperationAvailableLocked(audit); err != nil {
		return err
	}
	if audit.SourceID != "" {
		sourceKey := value.TenantID + "\x00" + audit.Source + "\x00" + audit.SourceID
		if existingID, exists := m.orderSources[sourceKey]; exists {
			return fmt.Errorf("%w: source order already maps to %q", ErrConflict, existingID)
		}
		m.orderSources[sourceKey] = value.ID
	}
	m.orders[value.ID] = cloneOrder(value)
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) Order(ctx context.Context, tenantID, id string) (domain.Order, error) {
	if err := ctx.Err(); err != nil {
		return domain.Order{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.orders[id]
	if !ok || value.TenantID != tenantID {
		return domain.Order{}, fmt.Errorf("%w: order %q", ErrNotFound, id)
	}
	return cloneOrder(value), nil
}

func (m *MemoryRepository) Orders(ctx context.Context, tenantID string) ([]domain.Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.Order, 0)
	for _, value := range m.orders {
		if value.TenantID == tenantID {
			values = append(values, cloneOrder(value))
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values, nil
}

func (m *MemoryRepository) TransitionOrder(ctx context.Context, tenantID, outletID, id string, to domain.OrderStatus, expectedVersion uint64, audit domain.AuditEvent) (domain.Order, error) {
	if err := ctx.Err(); err != nil {
		return domain.Order{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.orders[id]
	if !ok || value.TenantID != tenantID || value.OutletID != outletID {
		return domain.Order{}, fmt.Errorf("%w: order %q", ErrNotFound, id)
	}
	if value.Version != expectedVersion {
		return domain.Order{}, fmt.Errorf("%w: order %q is version %d", ErrVersionConflict, id, value.Version)
	}
	if !domain.CanTransitionOrderStatus(value.Status, to) {
		return domain.Order{}, fmt.Errorf("%w: order %s to %s", ErrInvalidTransition, value.Status, to)
	}
	if err := validateAudit(audit, tenantID, outletID, "order", id); err != nil {
		return domain.Order{}, err
	}
	if err := m.ensureAuditOperationAvailableLocked(audit); err != nil {
		return domain.Order{}, err
	}
	value.Status = to
	value.Version++
	value.UpdatedAt = audit.RecordedAt
	m.orders[id] = value
	m.appendAuditLocked(audit)
	return cloneOrder(value), nil
}

func (m *MemoryRepository) CreateKitchenTicket(ctx context.Context, value domain.KitchenTicket, audit domain.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tickets[value.ID]; exists {
		return fmt.Errorf("%w: kitchen ticket %q", ErrConflict, value.ID)
	}
	order, orderExists := m.orders[value.OrderID]
	station, stationExists := m.stations[value.StationID]
	if !orderExists || order.TenantID != value.TenantID || order.OutletID != value.OutletID {
		return fmt.Errorf("%w: order %q", ErrInvalidReference, value.OrderID)
	}
	if !stationExists || station.TenantID != value.TenantID || station.OutletID != value.OutletID {
		return fmt.Errorf("%w: station %q", ErrInvalidReference, value.StationID)
	}
	if !orderContainsLines(order, value.LineIDs) {
		return fmt.Errorf("%w: ticket line_ids must belong to order %q", ErrInvalidReference, value.OrderID)
	}
	if err := validateAudit(audit, value.TenantID, value.OutletID, "kitchen_ticket", value.ID); err != nil {
		return err
	}
	if err := m.ensureAuditOperationAvailableLocked(audit); err != nil {
		return err
	}
	m.tickets[value.ID] = cloneTicket(value)
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) KitchenTicket(ctx context.Context, tenantID, id string) (domain.KitchenTicket, error) {
	if err := ctx.Err(); err != nil {
		return domain.KitchenTicket{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.tickets[id]
	if !ok || value.TenantID != tenantID {
		return domain.KitchenTicket{}, fmt.Errorf("%w: kitchen ticket %q", ErrNotFound, id)
	}
	return cloneTicket(value), nil
}

func (m *MemoryRepository) KitchenTickets(ctx context.Context, tenantID string) ([]domain.KitchenTicket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.KitchenTicket, 0)
	for _, value := range m.tickets {
		if value.TenantID == tenantID {
			values = append(values, cloneTicket(value))
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values, nil
}

func (m *MemoryRepository) TransitionKitchenTicket(ctx context.Context, tenantID, outletID, id string, to domain.TicketStatus, expectedVersion uint64, audit domain.AuditEvent) (domain.KitchenTicket, error) {
	if err := ctx.Err(); err != nil {
		return domain.KitchenTicket{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.tickets[id]
	if !ok || value.TenantID != tenantID || value.OutletID != outletID {
		return domain.KitchenTicket{}, fmt.Errorf("%w: kitchen ticket %q", ErrNotFound, id)
	}
	if value.Version != expectedVersion {
		return domain.KitchenTicket{}, fmt.Errorf("%w: kitchen ticket %q is version %d", ErrVersionConflict, id, value.Version)
	}
	if !domain.CanTransitionTicketStatus(value.Status, to) {
		return domain.KitchenTicket{}, fmt.Errorf("%w: ticket %s to %s", ErrInvalidTransition, value.Status, to)
	}
	if err := validateAudit(audit, tenantID, outletID, "kitchen_ticket", id); err != nil {
		return domain.KitchenTicket{}, err
	}
	if err := m.ensureAuditOperationAvailableLocked(audit); err != nil {
		return domain.KitchenTicket{}, err
	}
	value.Status = to
	value.Version++
	value.UpdatedAt = audit.RecordedAt
	m.tickets[id] = value
	m.appendAuditLocked(audit)
	return cloneTicket(value), nil
}

func (m *MemoryRepository) AuditEvents(ctx context.Context, filter AuditFilter) ([]domain.AuditEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.AuditEvent, 0)
	for _, value := range m.audits {
		if value.TenantID != filter.TenantID {
			continue
		}
		if filter.OutletID != "" && value.OutletID != filter.OutletID {
			continue
		}
		if filter.EntityType != "" && value.EntityType != filter.EntityType {
			continue
		}
		if filter.EntityID != "" && value.EntityID != filter.EntityID {
			continue
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].RecordedAt.Equal(values[j].RecordedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].RecordedAt.Before(values[j].RecordedAt)
	})
	return values, nil
}

func validateAudit(audit domain.AuditEvent, tenantID, outletID, entityType, entityID string) error {
	if audit.ID == "" || audit.OperationID == "" || audit.Action == "" || audit.RecordedAt.IsZero() {
		return fmt.Errorf("%w: incomplete audit event", ErrInvalidReference)
	}
	if audit.TenantID != tenantID || audit.OutletID != outletID ||
		audit.EntityType != entityType || audit.EntityID != entityID {
		return fmt.Errorf("%w: audit event does not match mutation", ErrInvalidReference)
	}
	return nil
}

func (m *MemoryRepository) ensureAuditOperationAvailableLocked(audit domain.AuditEvent) error {
	key := audit.TenantID + "\x00" + audit.OperationID
	if existingID, exists := m.auditOperations[key]; exists {
		return fmt.Errorf("%w: operation already audited entity %q", ErrConflict, existingID)
	}
	return nil
}

func (m *MemoryRepository) appendAuditLocked(audit domain.AuditEvent) {
	key := audit.TenantID + "\x00" + audit.OperationID
	m.auditOperations[key] = audit.EntityID
	m.audits = append(m.audits, audit)
}

func orderContainsLines(order domain.Order, lineIDs []string) bool {
	known := make(map[string]struct{}, len(order.Lines))
	for _, line := range order.Lines {
		known[line.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(lineIDs))
	for _, id := range lineIDs {
		if _, exists := known[id]; !exists {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func cloneOrder(value domain.Order) domain.Order {
	value.Lines = append([]domain.OrderLine(nil), value.Lines...)
	return value
}

func cloneTicket(value domain.KitchenTicket) domain.KitchenTicket {
	value.LineIDs = append([]string(nil), value.LineIDs...)
	if value.TargetAt != nil {
		target := *value.TargetAt
		value.TargetAt = &target
	}
	return value
}

func sortOutlets(values []domain.Outlet) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
}

var _ Repository = (*MemoryRepository)(nil)
