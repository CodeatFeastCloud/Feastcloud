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

type unitInput struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Symbol          string `json:"symbol"`
	Dimension       string `json:"dimension"`
	BaseNumerator   int64  `json:"baseNumerator"`
	BaseDenominator int64  `json:"baseDenominator"`
	Active          *bool  `json:"active,omitempty"`
}

func (v unitInput) validate() error {
	if !domain.ValidUUID(v.ID) {
		return fmt.Errorf("id must be a UUID string")
	}
	if err := requiredText("name", v.Name, 80); err != nil {
		return err
	}
	if err := requiredText("symbol", v.Symbol, 16); err != nil {
		return err
	}
	if v.Dimension != "mass" && v.Dimension != "volume" && v.Dimension != "count" {
		return fmt.Errorf("dimension must be mass, volume, or count")
	}
	if v.BaseNumerator <= 0 || v.BaseDenominator <= 0 {
		return fmt.Errorf("base conversion values must be positive")
	}
	return nil
}

type ingredientInput struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Code          string   `json:"code"`
	BaseUnitID    string   `json:"baseUnitId"`
	Allergens     []string `json:"allergens"`
	DietaryLabels []string `json:"dietaryLabels"`
	Active        *bool    `json:"active,omitempty"`
}

func (v ingredientInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.BaseUnitID) {
		return fmt.Errorf("id and base_unit_id must be UUID strings")
	}
	if err := requiredText("name", v.Name, 160); err != nil {
		return err
	}
	if !domain.ValidCode(v.Code) {
		return fmt.Errorf("code is invalid")
	}
	if len(v.Allergens) > 64 || len(v.DietaryLabels) > 64 {
		return fmt.Errorf("allergen and dietary label lists are too long")
	}
	return nil
}

type recipeComponentInput struct {
	ID                   string  `json:"id"`
	IngredientID         string  `json:"ingredientId,omitempty"`
	ChildRecipeVersionID string  `json:"childRecipeVersionId,omitempty"`
	Quantity             float64 `json:"quantity"`
	UnitID               string  `json:"unitId"`
	PreparationNote      string  `json:"preparationNote,omitempty"`
}
type recipeVersionInput struct {
	ID                     string                 `json:"id"`
	VersionNumber          uint64                 `json:"versionNumber"`
	YieldQuantity          float64                `json:"yieldQuantity"`
	YieldUnitID            string                 `json:"yieldUnitId"`
	PreparationLossPercent float64                `json:"preparationLossPercent"`
	CookingLossPercent     float64                `json:"cookingLossPercent"`
	Instructions           string                 `json:"instructions"`
	EffectiveFrom          time.Time              `json:"effectiveFrom"`
	Components             []recipeComponentInput `json:"components"`
}

func (v recipeVersionInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.YieldUnitID) {
		return fmt.Errorf("version id and yield_unit_id must be UUID strings")
	}
	if v.VersionNumber < 1 {
		return fmt.Errorf("version_number must be positive")
	}
	if v.YieldQuantity <= 0 || math.IsNaN(v.YieldQuantity) || math.IsInf(v.YieldQuantity, 0) {
		return fmt.Errorf("yield_quantity must be a finite positive number")
	}
	if v.EffectiveFrom.IsZero() {
		return fmt.Errorf("effective_from is required")
	}
	if v.PreparationLossPercent < 0 || v.PreparationLossPercent > 100 || v.CookingLossPercent < 0 || v.CookingLossPercent > 100 {
		return fmt.Errorf("loss percentages must be between zero and 100")
	}
	if len(v.Components) == 0 || len(v.Components) > 500 {
		return fmt.Errorf("components must contain between 1 and 500 entries")
	}
	for i, c := range v.Components {
		if !domain.ValidUUID(c.ID) || !domain.ValidUUID(c.UnitID) {
			return fmt.Errorf("components[%d] id and unit_id must be UUID strings", i)
		}
		if (c.IngredientID == "") == (c.ChildRecipeVersionID == "") {
			return fmt.Errorf("components[%d] must reference exactly one ingredient or child recipe version", i)
		}
		if c.IngredientID != "" && !domain.ValidUUID(c.IngredientID) || c.ChildRecipeVersionID != "" && !domain.ValidUUID(c.ChildRecipeVersionID) {
			return fmt.Errorf("components[%d] reference must be a UUID string", i)
		}
		if c.Quantity <= 0 || math.IsNaN(c.Quantity) || math.IsInf(c.Quantity, 0) {
			return fmt.Errorf("components[%d] quantity must be finite and positive", i)
		}
	}
	return nil
}

type recipeInput struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Code    string             `json:"code"`
	Active  *bool              `json:"active,omitempty"`
	Version recipeVersionInput `json:"version"`
}

func (v recipeInput) validate() error {
	if !domain.ValidUUID(v.ID) {
		return fmt.Errorf("id must be a UUID string")
	}
	if err := requiredText("name", v.Name, 160); err != nil {
		return err
	}
	if !domain.ValidCode(v.Code) {
		return fmt.Errorf("code is invalid")
	}
	if v.Version.VersionNumber != 1 {
		return fmt.Errorf("initial recipe version_number must be one")
	}
	return v.Version.validate()
}

type menuItemInput struct {
	ID         string `json:"id"`
	OutletID   string `json:"outletId"`
	BrandID    string `json:"brandId,omitempty"`
	RecipeID   string `json:"recipeId"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	PriceMinor int64  `json:"priceMinor"`
	Currency   string `json:"currency"`
	StationID  string `json:"stationId,omitempty"`
	Active     *bool  `json:"active,omitempty"`
}

func (v menuItemInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) || (v.RecipeID != "" && !domain.ValidUUID(v.RecipeID)) {
		return fmt.Errorf("id and outlet_id must be UUID strings; recipe_id is optional but must be a UUID when provided")
	}
	if v.BrandID != "" && !domain.ValidUUID(v.BrandID) || v.StationID != "" && !domain.ValidUUID(v.StationID) {
		return fmt.Errorf("brand_id and station_id must be UUID strings")
	}
	if err := requiredText("name", v.Name, 160); err != nil {
		return err
	}
	if !domain.ValidCode(v.Code) || !domain.ValidCurrency(v.Currency) || v.PriceMinor < 0 {
		return fmt.Errorf("code, currency, or price is invalid")
	}
	return nil
}

type inventoryEventInput struct {
	ID              string     `json:"id"`
	OutletID        string     `json:"outletId"`
	IngredientID    string     `json:"ingredientId"`
	EventType       string     `json:"eventType"`
	Quantity        float64    `json:"quantity"`
	UnitID          string     `json:"unitId"`
	TotalCostMinor  int64      `json:"totalCostMinor"`
	Currency        string     `json:"currency"`
	ReferenceType   string     `json:"referenceType"`
	ReferenceID     string     `json:"referenceId"`
	LotCode         string     `json:"lotCode,omitempty"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	ReversesEventID string     `json:"reversesEventId,omitempty"`
}

type inventoryCountLineInput struct {
	ID              string  `json:"id"`
	IngredientID    string  `json:"ingredientId"`
	UnitID          string  `json:"unitId"`
	CountedQuantity float64 `json:"countedQuantity"`
}

type inventoryCountInput struct {
	ID       string                    `json:"id"`
	OutletID string                    `json:"outletId"`
	Notes    string                    `json:"notes,omitempty"`
	Lines    []inventoryCountLineInput `json:"lines"`
}

func (v inventoryCountInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) {
		return fmt.Errorf("count and outlet ids must be UUID strings")
	}
	if len(v.Lines) == 0 || len(v.Lines) > 500 {
		return fmt.Errorf("a count requires between 1 and 500 lines")
	}
	seen := map[string]bool{}
	for _, line := range v.Lines {
		if !domain.ValidUUID(line.ID) || !domain.ValidUUID(line.IngredientID) || !domain.ValidUUID(line.UnitID) {
			return fmt.Errorf("count line, ingredient, and unit ids must be UUID strings")
		}
		if line.CountedQuantity < 0 || math.IsNaN(line.CountedQuantity) || math.IsInf(line.CountedQuantity, 0) {
			return fmt.Errorf("counted quantity must be finite and non-negative")
		}
		if seen[line.IngredientID] {
			return fmt.Errorf("an ingredient can appear only once in a count")
		}
		seen[line.IngredientID] = true
	}
	if len(v.Notes) > 500 {
		return fmt.Errorf("notes must be at most 500 characters")
	}
	return nil
}

func (v inventoryEventInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) || !domain.ValidUUID(v.IngredientID) || !domain.ValidUUID(v.UnitID) || !domain.ValidUUID(v.ReferenceID) {
		return fmt.Errorf("event, outlet, ingredient, unit, and reference ids must be UUID strings")
	}
	allowed := map[string]bool{"receipt": true, "waste": true, "spoilage": true, "count_adjustment": true, "transfer_in": true, "transfer_out": true, "staff_meal": true, "production": true, "reversal": true}
	if !allowed[v.EventType] {
		return fmt.Errorf("event_type is not a supported manual inventory event")
	}
	if v.Quantity == 0 || math.IsNaN(v.Quantity) || math.IsInf(v.Quantity, 0) {
		return fmt.Errorf("quantity must be finite and non-zero")
	}
	if !domain.ValidCurrency(v.Currency) {
		return fmt.Errorf("currency is invalid")
	}
	if err := requiredText("reference_type", v.ReferenceType, 64); err != nil {
		return err
	}
	if v.EventType == "receipt" && v.TotalCostMinor < 0 {
		return fmt.Errorf("receipt cost cannot be negative")
	}
	if (v.EventType == "reversal") != (v.ReversesEventID != "") {
		return fmt.Errorf("reversal requires reverses_event_id and other events must omit it")
	}
	return nil
}

func (s *Server) graph() (store.KitchenGraphRepository, bool) {
	value, ok := s.repository.(store.KitchenGraphRepository)
	return value, ok
}
func components(input []recipeComponentInput) []domain.RecipeComponent {
	result := make([]domain.RecipeComponent, len(input))
	for i, c := range input {
		result[i] = domain.RecipeComponent{ID: c.ID, IngredientID: c.IngredientID, ChildRecipeVersionID: c.ChildRecipeVersionID, Quantity: c.Quantity, UnitID: c.UnitID, PreparationNote: strings.TrimSpace(c.PreparationNote)}
	}
	return result
}
func version(input recipeVersionInput, tenantID, recipeID string, createdAt time.Time) domain.RecipeVersion {
	return domain.RecipeVersion{ID: input.ID, TenantID: tenantID, RecipeID: recipeID, VersionNumber: input.VersionNumber, YieldQuantity: input.YieldQuantity, YieldUnitID: input.YieldUnitID, PreparationLossPercent: input.PreparationLossPercent, CookingLossPercent: input.CookingLossPercent, Instructions: strings.TrimSpace(input.Instructions), EffectiveFrom: input.EffectiveFrom.UTC(), Components: components(input.Components), CreatedAt: createdAt}
}

func (s *Server) handleCreateUnit(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input unitInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		repo, ok := s.graph()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		}
		now := s.now().UTC()
		v := domain.Unit{ID: input.ID, TenantID: m.TenantID, Name: strings.TrimSpace(input.Name), Symbol: strings.TrimSpace(input.Symbol), Dimension: input.Dimension, BaseNumerator: input.BaseNumerator, BaseDenominator: input.BaseDenominator, Active: boolDefaultTrue(input.Active), RecordMetadata: newRecordMetadata(now)}
		audit, err := newAuditEvent(m, "unit.created", "unit", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repo.CreateUnit(ctx, v, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/units/"+v.ID)
	})
}
func (s *Server) handleCreateIngredient(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input ingredientInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		repo, ok := s.graph()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		}
		now := s.now().UTC()
		v := domain.Ingredient{ID: input.ID, TenantID: m.TenantID, Name: strings.TrimSpace(input.Name), Code: input.Code, BaseUnitID: input.BaseUnitID, Allergens: input.Allergens, DietaryLabels: input.DietaryLabels, Active: boolDefaultTrue(input.Active), RecordMetadata: newRecordMetadata(now)}
		audit, err := newAuditEvent(m, "ingredient.created", "ingredient", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repo.CreateIngredient(ctx, v, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/ingredients/"+v.ID)
	})
}
func (s *Server) handleCreateRecipe(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input recipeInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		repo, ok := s.graph()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		}
		now := s.now().UTC()
		v := domain.Recipe{ID: input.ID, TenantID: m.TenantID, Name: strings.TrimSpace(input.Name), Code: input.Code, Active: boolDefaultTrue(input.Active), RecordMetadata: newRecordMetadata(now)}
		rv := version(input.Version, m.TenantID, v.ID, now)
		audit, err := newAuditEvent(m, "recipe.created", "recipe", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repo.CreateRecipe(ctx, v, rv, audit); err != nil {
			return repositoryError(err)
		}
		v.CurrentVersion = &rv
		return successResult(201, v, "/api/v1/recipes/"+v.ID)
	})
}
func (s *Server) handleAddRecipeVersion(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input recipeVersionInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		recipeID := r.PathValue("id")
		if !domain.ValidUUID(recipeID) {
			return errorResult(apiError{Status: 422, Code: "invalid_recipe_id", Message: "recipe id must be a UUID string"})
		}
		repo, ok := s.graph()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		}
		now := s.now().UTC()
		rv := version(input, m.TenantID, recipeID, now)
		audit, err := newAuditEvent(m, "recipe.version_added", "recipe", recipeID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repo.AddRecipeVersion(ctx, rv, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(201, rv, "/api/v1/recipes/"+recipeID+"/versions/"+rv.ID)
	})
}
func (s *Server) handleCreateMenuItem(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input menuItemInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		repo, ok := s.graph()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		}
		now := s.now().UTC()
		v := domain.MenuItem{ID: input.ID, TenantID: m.TenantID, OutletID: input.OutletID, BrandID: input.BrandID, RecipeID: input.RecipeID, Name: strings.TrimSpace(input.Name), Code: input.Code, PriceMinor: input.PriceMinor, Currency: input.Currency, StationID: input.StationID, Active: boolDefaultTrue(input.Active), RecordMetadata: newRecordMetadata(now)}
		audit, err := newAuditEvent(m, "menu_item.created", "menu_item", v.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repo.CreateMenuItem(ctx, v, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(201, v, "/api/v1/menu-items/"+v.ID)
	})
}
func (s *Server) handleRecordInventoryEvent(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input inventoryEventInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		repo, ok := s.graph()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		}
		now := s.now().UTC()
		event := domain.InventoryEvent{ID: input.ID, TenantID: m.TenantID, OutletID: input.OutletID, IngredientID: input.IngredientID, EventType: input.EventType, TotalCostMinor: input.TotalCostMinor, Currency: input.Currency, ReferenceType: input.ReferenceType, ReferenceID: input.ReferenceID, LotCode: input.LotCode, ExpiresAt: input.ExpiresAt, Reason: strings.TrimSpace(input.Reason), ActorID: m.ActorID, DeviceID: m.DeviceID, OperationID: m.ID, ReversesEventID: input.ReversesEventID, OccurredAt: m.OccurredAt, RecordedAt: now}
		audit, err := newAuditEvent(m, "inventory."+input.EventType, "inventory_event", event.ID, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.RecordInventoryEvent(ctx, store.StockMovement{Event: event, Quantity: input.Quantity, UnitID: input.UnitID}, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(201, value, "/api/v1/inventory-events/"+value.ID)
	})
}

func (s *Server) handleRecordInventoryCount(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var input inventoryCountInput
		if result := decodeAndValidate(p, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != m.OutletID {
			return outletScopeMismatch()
		}
		repo, ok := s.graph()
		if !ok {
			return errorResult(apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "inventory counts require PostgreSQL"})
		}
		now := s.now().UTC()
		count := domain.InventoryCount{ID: input.ID, TenantID: m.TenantID, OutletID: input.OutletID, Notes: strings.TrimSpace(input.Notes), CountedAt: m.OccurredAt, RecordedAt: now, ActorID: m.ActorID, DeviceID: m.DeviceID, OperationID: m.ID}
		lines := make([]store.StockCountLine, len(input.Lines))
		for index, line := range input.Lines {
			lines[index] = store.StockCountLine{ID: line.ID, IngredientID: line.IngredientID, UnitID: line.UnitID, CountedQuantity: line.CountedQuantity}
		}
		audit, err := newAuditEvent(m, "inventory.count_completed", "inventory_count", count.ID, now)
		if err != nil {
			return internalOperationError()
		}
		created, err := repo.RecordInventoryCount(ctx, count, lines, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(201, created, "/api/v1/inventory-counts/"+created.ID)
	})
}

func (s *Server) handleUnits(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repo, ok := s.graph()
	if !ok {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		return
	}
	values, err := repo.Units(r.Context(), tenant)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}
func (s *Server) handleIngredients(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repo, ok := s.graph()
	if !ok {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		return
	}
	values, err := repo.Ingredients(r.Context(), tenant)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}
func (s *Server) handleRecipes(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repo, ok := s.graph()
	if !ok {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		return
	}
	values, err := repo.Recipes(r.Context(), tenant)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}
func (s *Server) handleMenuItems(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	outlet := r.URL.Query().Get("outletId")
	if principal, found := principalFrom(r.Context()); found && !principal.AllowsOutlet(outlet) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 403, Code: "outlet_permission_denied", Message: "the authenticated principal is not assigned to this outlet"})
		return
	}
	repo, ok := s.graph()
	if !ok {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		return
	}
	values, err := repo.MenuItems(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}
func (s *Server) handleInventorySummary(w http.ResponseWriter, r *http.Request) {
	tenant, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	outlet := r.URL.Query().Get("outletId")
	if !domain.ValidUUID(outlet) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "invalid_outlet_id", Message: "outletId must be a UUID string"})
		return
	}
	if principal, found := principalFrom(r.Context()); found && !principal.AllowsOutlet(outlet) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 403, Code: "outlet_permission_denied", Message: "the authenticated principal is not assigned to this outlet"})
		return
	}
	repo, ok := s.graph()
	if !ok {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "kitchen_graph_unavailable", Message: "Kitchen Graph requires PostgreSQL"})
		return
	}
	values, err := repo.InventorySummary(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, values)
}
