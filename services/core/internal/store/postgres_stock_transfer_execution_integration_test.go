// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

func TestStockTransferExecutionIntegration(t *testing.T) {
	url := os.Getenv("FEASTCLOUD_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set FEASTCLOUD_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, err := NewPostgresRepository(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	now := time.Now().UTC().Truncate(time.Second)
	meta := domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}
	destinationID, unitID, ingredientID, receiptID, transferID, lineID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	if err := repository.CreateOutlet(ctx, domain.Outlet{ID: destinationID, TenantID: integrationTenantA, OrganizationID: integrationTenantA, Name: "Transfer receiving outlet", Code: "receive-" + destinationID[:6], TimeZone: "Asia/Kolkata", Currency: "INR", Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "outlet", destinationID, "outlet.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateUnit(ctx, domain.Unit{ID: unitID, TenantID: integrationTenantA, Name: "Transfer each", Symbol: "tx-" + unitID[:6], Dimension: "count", BaseNumerator: 1, BaseDenominator: 1, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "unit", unitID, "unit.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateIngredient(ctx, domain.Ingredient{ID: ingredientID, TenantID: integrationTenantA, Name: "Transfer tomatoes", Code: "transfer-" + ingredientID[:6], BaseUnitID: unitID, Allergens: []string{}, DietaryLabels: []string{}, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "ingredient", ingredientID, "ingredient.created", now)); err != nil {
		t.Fatal(err)
	}
	receiptAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "inventory_event", receiptID, "inventory.receipt", now)
	if _, err := repository.RecordInventoryEvent(ctx, StockMovement{Event: domain.InventoryEvent{ID: receiptID, TenantID: integrationTenantA, OutletID: integrationOutletA, IngredientID: ingredientID, EventType: "receipt", TotalCostMinor: 1200, Currency: "INR", ReferenceType: "goods_receipt", ReferenceID: newIntegrationUUID(t), ActorID: receiptAudit.ActorID, DeviceID: receiptAudit.DeviceID, OperationID: receiptAudit.OperationID, OccurredAt: now, RecordedAt: now}, Quantity: 12, UnitID: unitID}, receiptAudit); err != nil {
		t.Fatal(err)
	}
	requested := domain.StockTransfer{ID: transferID, TenantID: integrationTenantA, SourceOutletID: integrationOutletA, DestinationOutletID: destinationID, Status: "requested", RequestedBy: "Central kitchen", RequestedAt: now, Version: 1, Lines: []domain.StockTransferLine{{ID: lineID, IngredientID: ingredientID, QuantityBase: 5}}}
	if err := repository.CreateStockTransfer(ctx, requested, integrationAudit(t, integrationTenantA, integrationOutletA, "stock_transfer", transferID, "stock_transfer.requested", now)); err != nil {
		t.Fatal(err)
	}
	approved, err := repository.TransitionStockTransfer(ctx, integrationTenantA, integrationOutletA, transferID, "approved", 1, nil, integrationAudit(t, integrationTenantA, integrationOutletA, "stock_transfer", transferID, "stock_transfer.approved", now.Add(time.Second)))
	if err != nil || approved.Status != "approved" || approved.Version != 2 {
		t.Fatalf("approve=%#v err=%v", approved, err)
	}
	if _, err := repository.TransitionStockTransfer(ctx, integrationTenantA, integrationOutletA, transferID, "dispatched", 1, []domain.StockTransferExecutionLine{{IngredientID: ingredientID, QuantityBase: 5}}, integrationAudit(t, integrationTenantA, integrationOutletA, "stock_transfer", transferID, "stock_transfer.dispatched", now.Add(2*time.Second))); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale dispatch err=%v want version conflict", err)
	}
	dispatched, err := repository.TransitionStockTransfer(ctx, integrationTenantA, integrationOutletA, transferID, "dispatched", 2, []domain.StockTransferExecutionLine{{IngredientID: ingredientID, QuantityBase: 5}}, integrationAudit(t, integrationTenantA, integrationOutletA, "stock_transfer", transferID, "stock_transfer.dispatched", now.Add(3*time.Second)))
	if err != nil || dispatched.Status != "dispatched" || dispatched.Version != 3 || len(dispatched.Lines) != 1 || dispatched.Lines[0].DispatchedQuantityBase == nil || *dispatched.Lines[0].DispatchedQuantityBase != 5 {
		t.Fatalf("dispatch=%#v err=%v", dispatched, err)
	}
	received, err := repository.TransitionStockTransfer(ctx, integrationTenantA, destinationID, transferID, "received", 3, []domain.StockTransferExecutionLine{{IngredientID: ingredientID, QuantityBase: 4}}, integrationAudit(t, integrationTenantA, destinationID, "stock_transfer", transferID, "stock_transfer.received", now.Add(4*time.Second)))
	if err != nil || received.Status != "received" || received.Version != 4 || received.Lines[0].ReceivedQuantityBase == nil || *received.Lines[0].ReceivedQuantityBase != 4 {
		t.Fatalf("receive=%#v err=%v", received, err)
	}
	sourceSummary, err := repository.InventorySummary(ctx, integrationTenantA, integrationOutletA)
	if err != nil {
		t.Fatal(err)
	}
	destinationSummary, err := repository.InventorySummary(ctx, integrationTenantA, destinationID)
	if err != nil {
		t.Fatal(err)
	}
	var sourceQuantity, destinationQuantity float64
	for _, summary := range sourceSummary {
		if summary.IngredientID == ingredientID {
			sourceQuantity = summary.QuantityBase
		}
	}
	for _, summary := range destinationSummary {
		if summary.IngredientID == ingredientID {
			destinationQuantity = summary.QuantityBase
		}
	}
	if sourceQuantity != 7 || destinationQuantity != 4 {
		t.Fatalf("transfer ledger source=%v destination=%v want 7/4", sourceQuantity, destinationQuantity)
	}
	rule, err := repository.SetReplenishmentRule(ctx, integrationTenantA, destinationID, domain.ReplenishmentRule{OutletID: destinationID, IngredientID: ingredientID, SourceOutletID: integrationOutletA, ReorderPointBase: 4, TargetLevelBase: 9, Active: true}, integrationAudit(t, integrationTenantA, destinationID, "replenishment_rule", ingredientID, "replenishment_rule.saved", now.Add(5*time.Second)))
	if err != nil || rule.Version != 1 {
		t.Fatalf("replenishment rule=%#v err=%v", rule, err)
	}
	suggestions, err := repository.ReplenishmentSuggestions(ctx, integrationTenantA, destinationID)
	if err != nil || len(suggestions) != 1 || suggestions[0].IngredientID != ingredientID || suggestions[0].SuggestedQuantityBase != 5 || suggestions[0].Status != "ready" {
		t.Fatalf("replenishment suggestions=%#v err=%v", suggestions, err)
	}
	if transfers, err := repository.StockTransfers(ctx, integrationTenantA, destinationID); err != nil || len(transfers) == 0 {
		t.Fatalf("destination transfer queue=%#v err=%v", transfers, err)
	}
}
