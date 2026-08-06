// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

func TestKitchenGraphSnapshotsRecipeAndConsumesInventoryExactlyOnce(t *testing.T) {
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
	now := time.Now().UTC().Add(-time.Minute)
	kgID, countID, ingredientID, recipeID, versionOneID, versionTwoID, componentOneID, componentTwoID, menuItemID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	metadata := domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := repository.CreateUnit(ctx, domain.Unit{ID: kgID, TenantID: integrationTenantA, Name: "Kilogram", Symbol: "kg-" + kgID[:6], Dimension: "mass", BaseNumerator: 1, BaseDenominator: 1, Active: true, RecordMetadata: metadata}, integrationAudit(t, integrationTenantA, integrationOutletA, "unit", kgID, "unit.created", now)); err != nil {
		t.Fatalf("create kg: %v", err)
	}
	if err := repository.CreateUnit(ctx, domain.Unit{ID: countID, TenantID: integrationTenantA, Name: "Serving", Symbol: "srv-" + countID[:6], Dimension: "count", BaseNumerator: 1, BaseDenominator: 1, Active: true, RecordMetadata: metadata}, integrationAudit(t, integrationTenantA, integrationOutletA, "unit", countID, "unit.created", now)); err != nil {
		t.Fatalf("create count: %v", err)
	}
	if err := repository.CreateIngredient(ctx, domain.Ingredient{ID: ingredientID, TenantID: integrationTenantA, Name: "Snapshot rice", Code: "rice-" + ingredientID[:6], BaseUnitID: kgID, Allergens: []string{}, DietaryLabels: []string{"vegan"}, Active: true, RecordMetadata: metadata}, integrationAudit(t, integrationTenantA, integrationOutletA, "ingredient", ingredientID, "ingredient.created", now)); err != nil {
		t.Fatalf("create ingredient: %v", err)
	}
	recipe := domain.Recipe{ID: recipeID, TenantID: integrationTenantA, Name: "Rice bowl", Code: "bowl-" + recipeID[:6], Active: true, RecordMetadata: metadata}
	v1 := domain.RecipeVersion{ID: versionOneID, TenantID: integrationTenantA, RecipeID: recipeID, VersionNumber: 1, YieldQuantity: 1, YieldUnitID: countID, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, Components: []domain.RecipeComponent{{ID: componentOneID, IngredientID: ingredientID, Quantity: .2, UnitID: kgID}}}
	if err := repository.CreateRecipe(ctx, recipe, v1, integrationAudit(t, integrationTenantA, integrationOutletA, "recipe", recipeID, "recipe.created", now)); err != nil {
		t.Fatalf("create recipe: %v", err)
	}
	menu := domain.MenuItem{ID: menuItemID, TenantID: integrationTenantA, OutletID: integrationOutletA, RecipeID: recipeID, Name: "Rice bowl", Code: "menu-" + menuItemID[:6], PriceMinor: 15000, Currency: "INR", Active: true, RecordMetadata: metadata}
	if err := repository.CreateMenuItem(ctx, menu, integrationAudit(t, integrationTenantA, integrationOutletA, "menu_item", menuItemID, "menu_item.created", now)); err != nil {
		t.Fatalf("create menu item: %v", err)
	}
	receiptID := newIntegrationUUID(t)
	receiptAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "inventory_event", receiptID, "inventory.receipt", now)
	_, err = repository.RecordInventoryEvent(ctx, StockMovement{Event: domain.InventoryEvent{ID: receiptID, TenantID: integrationTenantA, OutletID: integrationOutletA, IngredientID: ingredientID, EventType: "receipt", TotalCostMinor: 100000, Currency: "INR", ReferenceType: "goods_receipt", ReferenceID: newIntegrationUUID(t), ActorID: receiptAudit.ActorID, DeviceID: receiptAudit.DeviceID, OperationID: receiptAudit.OperationID, OccurredAt: now, RecordedAt: now}, Quantity: 10, UnitID: kgID}, receiptAudit)
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	orderID, lineID := newIntegrationUUID(t), newIntegrationUUID(t)
	order := domain.Order{ID: orderID, TenantID: integrationTenantA, OutletID: integrationOutletA, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusReceived, Lines: []domain.OrderLine{{ID: lineID, MenuItemID: menuItemID, Name: "Rice bowl", Quantity: 1, UnitPrice: domain.Money{MinorUnits: 15000, Currency: "INR"}, LineTotal: domain.Money{MinorUnits: 15000, Currency: "INR"}}}, Subtotal: domain.Money{MinorUnits: 15000, Currency: "INR"}, DiscountTotal: domain.Money{Currency: "INR"}, TaxTotal: domain.Money{Currency: "INR"}, ServiceCharge: domain.Money{Currency: "INR"}, Total: domain.Money{MinorUnits: 15000, Currency: "INR"}, PlacedAt: now, RecordMetadata: metadata}
	if err := repository.CreateOrder(ctx, order, integrationAudit(t, integrationTenantA, integrationOutletA, "order", orderID, "order.created", now)); err != nil {
		t.Fatalf("create order: %v", err)
	}
	v2 := domain.RecipeVersion{ID: versionTwoID, TenantID: integrationTenantA, RecipeID: recipeID, VersionNumber: 2, YieldQuantity: 1, YieldUnitID: countID, EffectiveFrom: now.Add(time.Second), CreatedAt: now.Add(time.Second), Components: []domain.RecipeComponent{{ID: componentTwoID, IngredientID: ingredientID, Quantity: .3, UnitID: kgID}}}
	if err := repository.AddRecipeVersion(ctx, v2, integrationAudit(t, integrationTenantA, integrationOutletA, "recipe", recipeID, "recipe.version_added", now.Add(time.Second))); err != nil {
		t.Fatalf("add recipe version: %v", err)
	}
	statuses := []domain.OrderStatus{domain.OrderStatusAccepted, domain.OrderStatusPreparing, domain.OrderStatusReady, domain.OrderStatusCompleted}
	for index, status := range statuses {
		audit := integrationAudit(t, integrationTenantA, integrationOutletA, "order", orderID, "order.status_changed", now.Add(time.Duration(index+2)*time.Second))
		if _, err := repository.TransitionOrder(ctx, integrationTenantA, integrationOutletA, orderID, status, uint64(index+1), audit); err != nil {
			t.Fatalf("transition %s: %v", status, err)
		}
	}
	summary, err := repository.InventorySummary(ctx, integrationTenantA, integrationOutletA)
	if err != nil {
		t.Fatal(err)
	}
	var found *domain.InventorySummary
	for i := range summary {
		if summary[i].IngredientID == ingredientID {
			found = &summary[i]
		}
	}
	if found == nil || math.Abs(found.ConsumedQuantity-.2) > .000001 || math.Abs(found.QuantityBase-9.8) > .000001 || found.TheoreticalCostMinor != 2000 || found.StockValueMinor != 98000 {
		t.Fatalf("unexpected snapshot consumption summary: %#v", found)
	}

	edgeOrderID, edgeLineID := newIntegrationUUID(t), newIntegrationUUID(t)
	createMutation, _ := json.Marshal(map[string]any{"actorId": "edge-cashier", "deviceId": "edge-tablet", "occurredAt": now.Add(10 * time.Second), "payload": map[string]any{"order": map[string]any{"id": edgeOrderID, "type": "takeaway", "placedAt": now.Add(10 * time.Second), "lines": []map[string]any{{"id": edgeLineID, "menuItemId": menuItemID, "name": "Rice bowl", "quantity": 1}}}}})
	createOperation := integrationOperation(integrationTenantA, integrationOutletA, "order.create", "order")
	createOperation.AggregateID = edgeOrderID
	createOperation.Mutation = createMutation
	createHash := sha256.Sum256(createMutation)
	createOperation.RequestHash = createHash[:]
	if outcome, problem, err := repository.ApplySyncOperation(ctx, createOperation); err != nil || outcome != SyncAccepted {
		t.Fatalf("edge create=%s/%s/%v", outcome, problem, err)
	}
	var completedOperation SyncOperation
	for index, status := range []string{"fired", "preparing", "ready", "completed"} {
		mutation, _ := json.Marshal(map[string]any{"actorId": "edge-chef", "deviceId": "edge-kds", "occurredAt": now.Add(time.Duration(11+index) * time.Second), "payload": map[string]any{"toStatus": status}})
		operation := integrationOperation(integrationTenantA, integrationOutletA, "kitchenTicket.transitionAll", "order")
		operation.AggregateID = edgeOrderID
		operation.AggregateVersion = uint64(index + 2)
		operation.Mutation = mutation
		hash := sha256.Sum256(mutation)
		operation.RequestHash = hash[:]
		if outcome, problem, err := repository.ApplySyncOperation(ctx, operation); err != nil || outcome != SyncAccepted {
			t.Fatalf("edge transition %s=%s/%s/%v", status, outcome, problem, err)
		}
		if status == "completed" {
			completedOperation = operation
		}
	}
	if outcome, problem, err := repository.ApplySyncOperation(ctx, completedOperation); err != nil || outcome != SyncDuplicate {
		t.Fatalf("completed replay=%s/%s/%v", outcome, problem, err)
	}
	summary, err = repository.InventorySummary(ctx, integrationTenantA, integrationOutletA)
	if err != nil {
		t.Fatal(err)
	}
	found = nil
	for i := range summary {
		if summary[i].IngredientID == ingredientID {
			found = &summary[i]
		}
	}
	if found == nil || math.Abs(found.ConsumedQuantity-.5) > .000001 || math.Abs(found.QuantityBase-9.5) > .000001 || found.TheoreticalCostMinor != 5000 || found.StockValueMinor != 95000 {
		t.Fatalf("unexpected edge consumption summary: %#v", found)
	}
	wasteID := newIntegrationUUID(t)
	wasteAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "inventory_event", wasteID, "inventory.waste", now.Add(20*time.Second))
	waste, err := repository.RecordInventoryEvent(ctx, StockMovement{Event: domain.InventoryEvent{ID: wasteID, TenantID: integrationTenantA, OutletID: integrationOutletA, IngredientID: ingredientID, EventType: "waste", Currency: "INR", ReferenceType: "waste_log", ReferenceID: newIntegrationUUID(t), Reason: "quality check", ActorID: wasteAudit.ActorID, DeviceID: wasteAudit.DeviceID, OperationID: wasteAudit.OperationID, OccurredAt: wasteAudit.OccurredAt, RecordedAt: wasteAudit.RecordedAt}, Quantity: .5, UnitID: kgID}, wasteAudit)
	if err != nil {
		t.Fatalf("record waste: %v", err)
	}
	if waste.TotalCostMinor != -5000 || math.Abs(waste.QuantityBase+.5) > .000001 {
		t.Fatalf("unexpected waste costing: %#v", waste)
	}
	reversalID := newIntegrationUUID(t)
	reversalAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "inventory_event", reversalID, "inventory.reversal", now.Add(21*time.Second))
	reversal, err := repository.RecordInventoryEvent(ctx, StockMovement{Event: domain.InventoryEvent{ID: reversalID, TenantID: integrationTenantA, OutletID: integrationOutletA, IngredientID: ingredientID, EventType: "reversal", Currency: "INR", ReferenceType: "correction", ReferenceID: newIntegrationUUID(t), ReversesEventID: wasteID, ActorID: reversalAudit.ActorID, DeviceID: reversalAudit.DeviceID, OperationID: reversalAudit.OperationID, OccurredAt: reversalAudit.OccurredAt, RecordedAt: reversalAudit.RecordedAt}, Quantity: 999, UnitID: kgID}, reversalAudit)
	if err != nil {
		t.Fatalf("reverse waste: %v", err)
	}
	if reversal.TotalCostMinor != 5000 || math.Abs(reversal.QuantityBase-.5) > .000001 {
		t.Fatalf("reversal did not exactly compensate original: %#v", reversal)
	}
	secondID := newIntegrationUUID(t)
	secondAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "inventory_event", secondID, "inventory.reversal", now.Add(22*time.Second))
	_, err = repository.RecordInventoryEvent(ctx, StockMovement{Event: domain.InventoryEvent{ID: secondID, TenantID: integrationTenantA, OutletID: integrationOutletA, IngredientID: ingredientID, EventType: "reversal", Currency: "INR", ReferenceType: "correction", ReferenceID: newIntegrationUUID(t), ReversesEventID: wasteID, ActorID: secondAudit.ActorID, DeviceID: secondAudit.DeviceID, OperationID: secondAudit.OperationID, OccurredAt: secondAudit.OccurredAt, RecordedAt: secondAudit.RecordedAt}, Quantity: 1, UnitID: kgID}, secondAudit)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second reversal error=%v want conflict", err)
	}

	stockCountID, stockCountLineID := newIntegrationUUID(t), newIntegrationUUID(t)
	countAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "inventory_count", stockCountID, "inventory.count_completed", now.Add(23*time.Second))
	count, err := repository.RecordInventoryCount(ctx, domain.InventoryCount{ID: stockCountID, TenantID: integrationTenantA, OutletID: integrationOutletA, Notes: "closing count", CountedAt: countAudit.OccurredAt, RecordedAt: countAudit.RecordedAt, ActorID: countAudit.ActorID, DeviceID: countAudit.DeviceID, OperationID: countAudit.OperationID}, []StockCountLine{{ID: stockCountLineID, IngredientID: ingredientID, UnitID: kgID, CountedQuantity: 9}}, countAudit)
	if err != nil {
		t.Fatalf("record stock count: %v", err)
	}
	if len(count.Lines) != 1 || math.Abs(count.Lines[0].ExpectedQuantityBase-9.5) > .000001 || math.Abs(count.Lines[0].VarianceQuantityBase+.5) > .000001 || count.Lines[0].VarianceCostMinor != -5000 {
		t.Fatalf("unexpected stock count: %#v", count)
	}
	summary, err = repository.InventorySummary(ctx, integrationTenantA, integrationOutletA)
	if err != nil {
		t.Fatal(err)
	}
	for i := range summary {
		if summary[i].IngredientID == ingredientID && math.Abs(summary[i].QuantityBase-9) > .000001 {
			t.Fatalf("count did not reconcile balance: %#v", summary[i])
		}
	}

	zeroCountID, zeroLineID := newIntegrationUUID(t), newIntegrationUUID(t)
	zeroAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "inventory_count", zeroCountID, "inventory.count_completed", now.Add(24*time.Second))
	zero, err := repository.RecordInventoryCount(ctx, domain.InventoryCount{ID: zeroCountID, TenantID: integrationTenantA, OutletID: integrationOutletA, CountedAt: zeroAudit.OccurredAt, RecordedAt: zeroAudit.RecordedAt, ActorID: zeroAudit.ActorID, DeviceID: zeroAudit.DeviceID, OperationID: zeroAudit.OperationID}, []StockCountLine{{ID: zeroLineID, IngredientID: ingredientID, UnitID: kgID, CountedQuantity: 9}}, zeroAudit)
	if err != nil || len(zero.Lines) != 1 || math.Abs(zero.Lines[0].VarianceQuantityBase) > .000001 {
		t.Fatalf("zero-variance count must remain durable: %#v / %v", zero, err)
	}

	preparedIngredientID := newIntegrationUUID(t)
	if err := repository.CreateIngredient(ctx, domain.Ingredient{ID: preparedIngredientID, TenantID: integrationTenantA, Name: "Prepared rice", Code: "prepared-" + preparedIngredientID[:6], BaseUnitID: kgID, Allergens: []string{}, DietaryLabels: []string{"vegan"}, Active: true, RecordMetadata: metadata}, integrationAudit(t, integrationTenantA, integrationOutletA, "ingredient", preparedIngredientID, "ingredient.created", now.Add(25*time.Second))); err != nil {
		t.Fatalf("create prepared ingredient: %v", err)
	}
	batchID := newIntegrationUUID(t)
	planAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "production_batch", batchID, "production_batch.planned", now.Add(26*time.Second))
	batch := domain.ProductionBatch{ID: batchID, TenantID: integrationTenantA, OutletID: integrationOutletA, RecipeVersionID: versionTwoID, OutputIngredientID: preparedIngredientID, OutputUnitID: kgID, Status: domain.ProductionBatchPlanned, PlannedQuantity: 2, PlannedFor: now.Add(time.Hour), LotCode: "prep-1", RecordMetadata: metadata}
	if err := repository.CreateProductionBatch(ctx, batch, planAudit); err != nil {
		t.Fatalf("plan production batch: %v", err)
	}
	startAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "production_batch", batchID, "production_batch.status_changed", now.Add(27*time.Second))
	started, err := repository.TransitionProductionBatch(ctx, integrationTenantA, integrationOutletA, batchID, domain.ProductionBatchInProgress, 1, nil, nil, "", "", startAudit)
	if err != nil || started.Version != 2 || started.StartedAt == nil {
		t.Fatalf("start production batch: %#v / %v", started, err)
	}
	actualYield := 1.8
	expires := now.Add(48 * time.Hour)
	completeAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "production_batch", batchID, "production_batch.status_changed", now.Add(28*time.Second))
	completed, err := repository.TransitionProductionBatch(ctx, integrationTenantA, integrationOutletA, batchID, domain.ProductionBatchCompleted, 2, &actualYield, &expires, "prep-1", "yield checked", completeAudit)
	if err != nil || completed.Version != 3 || completed.CompletedAt == nil {
		t.Fatalf("complete production batch: %#v / %v", completed, err)
	}
	summary, err = repository.InventorySummary(ctx, integrationTenantA, integrationOutletA)
	if err != nil {
		t.Fatal(err)
	}
	var inputAfter, outputAfter *domain.InventorySummary
	for index := range summary {
		if summary[index].IngredientID == ingredientID {
			inputAfter = &summary[index]
		}
		if summary[index].IngredientID == preparedIngredientID {
			outputAfter = &summary[index]
		}
	}
	if inputAfter == nil || math.Abs(inputAfter.QuantityBase-8.4) > .000001 || inputAfter.StockValueMinor != 84000 {
		t.Fatalf("production inputs not consumed from fixed recipe version: %#v", inputAfter)
	}
	if outputAfter == nil || math.Abs(outputAfter.QuantityBase-1.8) > .000001 || outputAfter.StockValueMinor != 6000 {
		t.Fatalf("actual production yield not received at consumed cost: %#v", outputAfter)
	}
	if _, err := repository.TransitionProductionBatch(ctx, integrationTenantA, integrationOutletA, batchID, domain.ProductionBatchCompleted, 2, &actualYield, &expires, "prep-1", "replay", completeAudit); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale production completion error=%v want version conflict", err)
	}
}
