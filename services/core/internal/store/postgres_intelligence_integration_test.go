// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

func TestOrderImportReconciliationAndObservedPlanningIntegration(t *testing.T) {
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
	unitID, ingredientID, recipeID, versionID, componentID, stationID, menuID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	metadata := domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := repository.CreateUnit(ctx, domain.Unit{ID: unitID, TenantID: integrationTenantA, Name: "Portion", Symbol: "portion-" + unitID[:6], Dimension: "count", BaseNumerator: 1, BaseDenominator: 1, Active: true, RecordMetadata: metadata}, integrationAudit(t, integrationTenantA, integrationOutletA, "unit", unitID, "unit.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateIngredient(ctx, domain.Ingredient{ID: ingredientID, TenantID: integrationTenantA, Name: "Planning ingredient", Code: "plan-ingredient-" + ingredientID[:6], BaseUnitID: unitID, Allergens: []string{}, DietaryLabels: []string{}, Active: true, RecordMetadata: metadata}, integrationAudit(t, integrationTenantA, integrationOutletA, "ingredient", ingredientID, "ingredient.created", now)); err != nil {
		t.Fatal(err)
	}
	recipe := domain.Recipe{ID: recipeID, TenantID: integrationTenantA, Name: "Planning recipe", Code: "plan-recipe-" + recipeID[:6], Active: true, RecordMetadata: metadata}
	version := domain.RecipeVersion{ID: versionID, TenantID: integrationTenantA, RecipeID: recipeID, VersionNumber: 1, YieldQuantity: 1, YieldUnitID: unitID, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, Components: []domain.RecipeComponent{{ID: componentID, IngredientID: ingredientID, UnitID: unitID, Quantity: 2}}}
	if err := repository.CreateRecipe(ctx, recipe, version, integrationAudit(t, integrationTenantA, integrationOutletA, "recipe", recipeID, "recipe.created", now)); err != nil {
		t.Fatal(err)
	}
	station := domain.Station{ID: stationID, TenantID: integrationTenantA, OutletID: integrationOutletA, Name: "Import line", Code: "import-" + stationID[:6], Type: domain.StationTypePreparation, Active: true, RecordMetadata: metadata}
	if err := repository.CreateStation(ctx, station, integrationAudit(t, integrationTenantA, integrationOutletA, "station", stationID, "station.created", now)); err != nil {
		t.Fatal(err)
	}
	menuCode := "import-item-" + menuID[:6]
	menu := domain.MenuItem{ID: menuID, TenantID: integrationTenantA, OutletID: integrationOutletA, RecipeID: recipeID, Name: "Imported bowl", Code: menuCode, Currency: "INR", StationID: stationID, PriceMinor: 12500, Active: true, RecordMetadata: metadata}
	if err := repository.CreateMenuItem(ctx, menu, integrationAudit(t, integrationTenantA, integrationOutletA, "menu_item", menuID, "menu_item.created", now)); err != nil {
		t.Fatal(err)
	}
	importID := newIntegrationUUID(t)
	importAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "order_import", importID, "order_import.completed", now.Add(time.Second))
	reference := "external-" + importID[:8]
	rows := []ImportedOrderRow{{ID: newIntegrationUUID(t), RowNumber: 2, ExternalRef: reference, OrderType: "takeaway", ItemCode: menuCode, Quantity: 3, PlacedAt: now, RawData: map[string]any{"itemCode": menuCode}}, {ID: newIntegrationUUID(t), RowNumber: 3, ExternalRef: reference, OrderType: "takeaway", ItemCode: menuCode, Quantity: 1, PlacedAt: now, RawData: map[string]any{"itemCode": menuCode}}, {ID: newIntegrationUUID(t), RowNumber: 4, ExternalRef: "missing-" + importID[:8], OrderType: "delivery", ItemCode: "missing-code", Quantity: 1, PlacedAt: now, RawData: map[string]any{"itemCode": "missing-code"}}}
	fileHash := sha256.Sum256([]byte(importID))
	result, err := repository.ImportOrders(ctx, domain.OrderImport{ID: importID, TenantID: integrationTenantA, OutletID: integrationOutletA, FileName: "orders.csv", FileSHA256: fmt.Sprintf("%x", fileHash), ImportedAt: now}, rows, importAudit)
	if err != nil {
		t.Fatal(err)
	}
	if result.AcceptedRows != 1 || result.RejectedRows != 2 || result.Status != "completed_with_errors" {
		t.Fatalf("unexpected import result: %#v", result)
	}
	if result.Rows[1].ErrorCode != "duplicate_external_ref" || result.Rows[2].ErrorCode != "menu_item_not_found" {
		t.Fatalf("unexpected reconciliation: %#v", result.Rows)
	}
	runID := newIntegrationUUID(t)
	runAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "planning_run", runID, "planning_run.generated", now.Add(2*time.Second))
	run, err := repository.GeneratePlanningRun(ctx, domain.PlanningRun{ID: runID, TenantID: integrationTenantA, OutletID: integrationOutletA, HorizonStart: now.Add(24 * time.Hour), HorizonEnd: now.Add(48 * time.Hour), ModelVersion: "trailing_average_recipe_graph_v1", Status: "observed", EvidenceFrom: now.Add(-28 * 24 * time.Hour), EvidenceTo: now.Add(time.Minute), GeneratedAt: now.Add(2 * time.Second)}, runAudit)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, recommendation := range run.Recommendations {
		types[recommendation.Type] = true
		if recommendation.MenuItemID == menuID && recommendation.Type == "demand_forecast" && math.Abs(recommendation.ForecastQuantity-3.0/28.0) > .000001 {
			t.Fatalf("forecast quantity=%f", recommendation.ForecastQuantity)
		}
	}
	for _, kind := range []string{"demand_forecast", "prep_suggestion", "stockout_warning"} {
		if !types[kind] {
			t.Fatalf("planning run missing %s: %#v", kind, run.Recommendations)
		}
	}
	imports, err := repository.OrderImports(ctx, integrationTenantA, integrationOutletA)
	if err != nil || len(imports) == 0 {
		t.Fatalf("load import evidence: %v %#v", err, imports)
	}
	runs, err := repository.PlanningRuns(ctx, integrationTenantA, integrationOutletA)
	if err != nil || len(runs) == 0 || len(runs[0].Recommendations) == 0 {
		t.Fatalf("load planning evidence: %v %#v", err, runs)
	}

	// A menu export is stored as an immutable mapping workspace, not converted
	// into sellable menu_items. This preserves recipe/station approval gates.
	draftID := newIntegrationUUID(t)
	draftData, err := json.Marshal(map[string]any{
		"items":       []map[string]any{{"sourceLine": 2, "name": "Imported bowl", "onlineName": "Imported bowl", "code": "imported-bowl", "category": "Bowls", "priceMinor": 12500, "addOnGroupNames": []string{}, "variations": []any{}}},
		"addonGroups": []any{}, "categories": []string{"Bowls"}, "variationCount": 0, "warnings": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	draftHash := sha256.Sum256(draftData)
	draft, err := repository.CreateMenuImportDraft(ctx, domain.MenuImportDraft{
		ID: draftID, TenantID: integrationTenantA, OutletID: integrationOutletA, Name: "Imported menu " + draftID[:6], ItemFileName: "items.csv", AddonFileName: "addons.csv", SourceSHA256: fmt.Sprintf("%x", draftHash), Status: "staged", ItemCount: 1, CategoryCount: 1, AddonGroupCount: 0, VariationCount: 0, Draft: draftData, ImportedAt: now.Add(3 * time.Second),
	}, integrationAudit(t, integrationTenantA, integrationOutletA, "menu_import", draftID, "menu_import.staged", now.Add(3*time.Second)))
	if err != nil || draft.Status != "staged" {
		t.Fatalf("stage menu import: %#v / %v", draft, err)
	}
	drafts, err := repository.MenuImportDrafts(ctx, integrationTenantA, integrationOutletA)
	if err != nil {
		t.Fatal(err)
	}
	var loadedDraft *domain.MenuImportDraft
	for index := range drafts {
		if drafts[index].ID == draftID {
			loadedDraft = &drafts[index]
		}
	}
	if loadedDraft == nil || string(loadedDraft.Draft) != string(draftData) || loadedDraft.ItemCount != 1 {
		t.Fatalf("menu import draft did not round trip: %#v", loadedDraft)
	}
}
