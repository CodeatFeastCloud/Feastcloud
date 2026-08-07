// SPDX-License-Identifier: AGPL-3.0-only

package store

// This file keeps the infrastructure-free core useful for local development.
// Menu Studio used to be PostgreSQL-only, which made every menu screen look
// broken when developers ran the documented `core:dev` command without a
// database. These small in-memory implementations are intentionally process
// local; PostgreSQL remains the durable production implementation.

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

func memoryContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (m *MemoryRepository) CreateUnit(ctx context.Context, value domain.Unit, audit domain.AuditEvent) error {
	if err := memoryContext(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.units[value.ID]; ok {
		return fmt.Errorf("%w: unit %q", ErrConflict, value.ID)
	}
	if err := validateAudit(audit, value.TenantID, audit.OutletID, "unit", value.ID); err != nil {
		return err
	}
	m.units[value.ID] = value
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) Units(ctx context.Context, tenantID string) ([]domain.Unit, error) {
	if err := memoryContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.Unit, 0)
	for _, value := range m.units {
		if value.TenantID == tenantID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values, nil
}

func (m *MemoryRepository) CreateIngredient(ctx context.Context, value domain.Ingredient, audit domain.AuditEvent) error {
	if err := memoryContext(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.ingredients[value.ID]; ok {
		return fmt.Errorf("%w: ingredient %q", ErrConflict, value.ID)
	}
	if err := validateAudit(audit, value.TenantID, audit.OutletID, "ingredient", value.ID); err != nil {
		return err
	}
	m.ingredients[value.ID] = value
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) Ingredients(ctx context.Context, tenantID string) ([]domain.Ingredient, error) {
	if err := memoryContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.Ingredient, 0)
	for _, value := range m.ingredients {
		if value.TenantID == tenantID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values, nil
}

func (m *MemoryRepository) CreateRecipe(ctx context.Context, value domain.Recipe, version domain.RecipeVersion, audit domain.AuditEvent) error {
	if err := memoryContext(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.recipes[value.ID]; ok {
		return fmt.Errorf("%w: recipe %q", ErrConflict, value.ID)
	}
	versionCopy := version
	value.CurrentVersion = &versionCopy
	if err := validateAudit(audit, value.TenantID, audit.OutletID, "recipe", value.ID); err != nil {
		return err
	}
	m.recipes[value.ID] = value
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) AddRecipeVersion(ctx context.Context, value domain.RecipeVersion, audit domain.AuditEvent) error {
	if err := memoryContext(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	recipe, ok := m.recipes[value.RecipeID]
	if !ok {
		return fmt.Errorf("%w: recipe %q", ErrNotFound, value.RecipeID)
	}
	versionCopy := value
	recipe.CurrentVersion = &versionCopy
	recipe.UpdatedAt = audit.RecordedAt
	if err := validateAudit(audit, value.TenantID, audit.OutletID, "recipe", value.RecipeID); err != nil {
		return err
	}
	m.recipes[value.RecipeID] = recipe
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) Recipes(ctx context.Context, tenantID string) ([]domain.Recipe, error) {
	if err := memoryContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.Recipe, 0)
	for _, value := range m.recipes {
		if value.TenantID == tenantID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values, nil
}

func (m *MemoryRepository) CreateMenuItem(ctx context.Context, value domain.MenuItem, audit domain.AuditEvent) error {
	if err := memoryContext(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.menuItems[value.ID]; ok {
		return fmt.Errorf("%w: menu item %q", ErrConflict, value.ID)
	}
	if err := validateAudit(audit, value.TenantID, value.OutletID, "menu_item", value.ID); err != nil {
		return err
	}
	m.menuItems[value.ID] = value
	m.appendAuditLocked(audit)
	return nil
}

func (m *MemoryRepository) MenuItems(ctx context.Context, tenantID, outletID string) ([]domain.MenuItem, error) {
	if err := memoryContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.MenuItem, 0)
	for _, value := range m.menuItems {
		if value.TenantID == tenantID && value.OutletID == outletID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values, nil
}

func (m *MemoryRepository) CreateMenuStudio(ctx context.Context, value domain.MenuStudio, version domain.MenuStudioVersion, audit domain.AuditEvent) (domain.MenuStudio, error) {
	if err := memoryContext(ctx); err != nil {
		return domain.MenuStudio{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.menuStudios[value.ID]; ok {
		return domain.MenuStudio{}, fmt.Errorf("%w: menu studio %q", ErrConflict, value.ID)
	}
	version.MenuStudioID = value.ID
	value.Current = &version
	if version.Status == "published" {
		value.Status, value.CurrentVersionID = "published", version.ID
	}
	if err := validateAudit(audit, value.TenantID, value.OutletID, "menu_studio", value.ID); err != nil {
		return domain.MenuStudio{}, err
	}
	m.menuStudios[value.ID] = value
	m.appendAuditLocked(audit)
	return value, nil
}

func (m *MemoryRepository) AddMenuStudioVersion(ctx context.Context, tenantID, outletID string, version domain.MenuStudioVersion, expected uint64, audit domain.AuditEvent) (domain.MenuStudio, error) {
	if err := memoryContext(ctx); err != nil {
		return domain.MenuStudio{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	studio, ok := m.menuStudios[version.MenuStudioID]
	if !ok || studio.TenantID != tenantID || studio.OutletID != outletID {
		return domain.MenuStudio{}, fmt.Errorf("%w: menu studio %q", ErrNotFound, version.MenuStudioID)
	}
	if studio.Version != expected {
		return domain.MenuStudio{}, fmt.Errorf("%w: menu studio version", ErrVersionConflict)
	}
	version.MenuStudioID = studio.ID
	studio.Version++
	studio.Current = &version
	if version.Status == "published" {
		studio.Status, studio.CurrentVersionID = "published", version.ID
	}
	studio.UpdatedAt = audit.RecordedAt
	if err := validateAudit(audit, tenantID, outletID, "menu_studio", studio.ID); err != nil {
		return domain.MenuStudio{}, err
	}
	m.menuStudios[studio.ID] = studio
	m.appendAuditLocked(audit)
	return studio, nil
}

func (m *MemoryRepository) MenuStudios(ctx context.Context, tenantID, outletID string) ([]domain.MenuStudio, error) {
	if err := memoryContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.MenuStudio, 0)
	for _, value := range m.menuStudios {
		if value.TenantID == tenantID && value.OutletID == outletID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values, nil
}

func (m *MemoryRepository) LiveMenuStudio(ctx context.Context, tenantID, outletID, channelID string, at time.Time) (domain.MenuStudio, error) {
	values, err := m.MenuStudios(ctx, tenantID, outletID)
	if err != nil {
		return domain.MenuStudio{}, err
	}
	for _, value := range values {
		if value.Status == "published" && value.Current != nil {
			return value, nil
		}
	}
	return domain.MenuStudio{}, fmt.Errorf("%w: live menu", ErrNotFound)
}

func (m *MemoryRepository) CreateMenuImportDraft(ctx context.Context, value domain.MenuImportDraft, audit domain.AuditEvent) (domain.MenuImportDraft, error) {
	if err := memoryContext(ctx); err != nil {
		return domain.MenuImportDraft{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.menuImports[value.ID]; ok {
		return domain.MenuImportDraft{}, fmt.Errorf("%w: menu import %q", ErrConflict, value.ID)
	}
	if err := validateAudit(audit, value.TenantID, value.OutletID, "menu_import", value.ID); err != nil {
		return domain.MenuImportDraft{}, err
	}
	m.menuImports[value.ID] = value
	m.appendAuditLocked(audit)
	return value, nil
}

func (m *MemoryRepository) MenuImportDrafts(ctx context.Context, tenantID, outletID string) ([]domain.MenuImportDraft, error) {
	if err := memoryContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]domain.MenuImportDraft, 0)
	for _, value := range m.menuImports {
		if value.TenantID == tenantID && value.OutletID == outletID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ImportedAt.After(values[j].ImportedAt) })
	return values, nil
}

func (m *MemoryRepository) ImportOrders(ctx context.Context, value domain.OrderImport, inputs []ImportedOrderRow, audit domain.AuditEvent) (domain.OrderImport, error) {
	if err := memoryContext(ctx); err != nil {
		return domain.OrderImport{}, err
	}
	value.Rows = make([]domain.OrderImportRow, 0, len(inputs))
	value.TotalRows = len(inputs)
	value.ImportedAt = audit.RecordedAt
	for _, input := range inputs {
		status := "accepted"
		if input.ErrorCode != "" {
			status = "rejected"
			value.RejectedRows++
		} else {
			value.AcceptedRows++
		}
		value.Rows = append(value.Rows, domain.OrderImportRow{ID: input.ID, RowNumber: input.RowNumber, ExternalRef: input.ExternalRef, Status: status, ErrorCode: input.ErrorCode, ErrorMessage: input.ErrorMessage, RawData: input.RawData})
	}
	value.Status = "completed"
	return value, nil
}

func (m *MemoryRepository) OrderImports(context.Context, string, string) ([]domain.OrderImport, error) {
	return []domain.OrderImport{}, nil
}
func (m *MemoryRepository) GeneratePlanningRun(ctx context.Context, value domain.PlanningRun, audit domain.AuditEvent) (domain.PlanningRun, error) {
	if err := memoryContext(ctx); err != nil {
		return domain.PlanningRun{}, err
	}
	value.GeneratedAt = audit.RecordedAt
	return value, nil
}
func (m *MemoryRepository) PlanningRuns(context.Context, string, string) ([]domain.PlanningRun, error) {
	return []domain.PlanningRun{}, nil
}

func (m *MemoryRepository) RecordInventoryEvent(ctx context.Context, movement StockMovement, audit domain.AuditEvent) (domain.InventoryEvent, error) {
	return domain.InventoryEvent{}, fmt.Errorf("%w: inventory", ErrNotFound)
}
func (m *MemoryRepository) RecordInventoryCount(ctx context.Context, value domain.InventoryCount, requested []StockCountLine, audit domain.AuditEvent) (domain.InventoryCount, error) {
	return domain.InventoryCount{}, fmt.Errorf("%w: inventory", ErrNotFound)
}
func (m *MemoryRepository) InventorySummary(ctx context.Context, tenantID, outletID string) ([]domain.InventorySummary, error) {
	return []domain.InventorySummary{}, nil
}

func (m *MemoryRepository) CheckoutPOS(context.Context, domain.POSCheckout, domain.AuditEvent) (domain.POSCheckoutResult, error) {
	return domain.POSCheckoutResult{}, fmt.Errorf("%w: POS checkout requires the durable menu ledger", ErrNotFound)
}
func (m *MemoryRepository) AcknowledgeKitchenPrintJob(context.Context, string, string, string, string, domain.AuditEvent) (domain.KitchenPrintJob, error) {
	return domain.KitchenPrintJob{}, fmt.Errorf("%w: print job", ErrNotFound)
}
func (m *MemoryRepository) TransitionPickupToken(context.Context, string, string, string, string, uint64, domain.AuditEvent) (domain.PickupToken, error) {
	return domain.PickupToken{}, fmt.Errorf("%w: pickup token", ErrNotFound)
}

var _ KitchenGraphRepository = (*MemoryRepository)(nil)
var _ IntelligenceRepository = (*MemoryRepository)(nil)
var _ RestaurantCoreRepository = (*MemoryRepository)(nil)
