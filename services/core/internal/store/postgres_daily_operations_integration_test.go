// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"context"
	"errors"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"os"
	"testing"
	"time"
)

func TestDailyOperationsMVPIntegration(t *testing.T) {
	databaseURL := os.Getenv("FEASTCLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set FEASTCLOUD_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	now := time.Now().UTC()
	meta := domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}
	unitID, ingredientID, supplierID, poID, lineID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	if err := r.CreateUnit(ctx, domain.Unit{ID: unitID, TenantID: integrationTenantA, Name: "Ops kilogram", Symbol: "opkg-" + unitID[:5], Dimension: "mass", BaseNumerator: 1, BaseDenominator: 1, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "unit", unitID, "unit.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateIngredient(ctx, domain.Ingredient{ID: ingredientID, TenantID: integrationTenantA, Name: "Ops produce", Code: "ops-" + ingredientID[:5], BaseUnitID: unitID, Allergens: []string{}, DietaryLabels: []string{}, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "ingredient", ingredientID, "ingredient.created", now)); err != nil {
		t.Fatal(err)
	}
	supplier := domain.Supplier{ID: supplierID, TenantID: integrationTenantA, Name: "Farm " + supplierID[:5], Code: "F-" + supplierID[:5], Active: true, RecordMetadata: meta}
	if err := r.CreateSupplier(ctx, supplier, integrationAudit(t, integrationTenantA, integrationOutletA, "supplier", supplierID, "supplier.created", now)); err != nil {
		t.Fatal(err)
	}
	po := domain.PurchaseOrder{ID: poID, TenantID: integrationTenantA, OutletID: integrationOutletA, SupplierID: supplierID, PONumber: "PO-" + poID[:8], Status: "draft", Currency: "INR", Lines: []domain.PurchaseOrderLine{{ID: lineID, IngredientID: ingredientID, UnitID: unitID, OrderedQuantity: 5, UnitCostMinor: 1000}}, RecordMetadata: meta}
	if err := r.CreatePurchaseOrder(ctx, po, integrationAudit(t, integrationTenantA, integrationOutletA, "purchase_order", poID, "purchase_order.created", now)); err != nil {
		t.Fatal(err)
	}
	submitted, err := r.TransitionPurchaseOrder(ctx, integrationTenantA, integrationOutletA, poID, "submitted", 1, integrationAudit(t, integrationTenantA, integrationOutletA, "purchase_order", poID, "purchase_order.submitted", now.Add(time.Second)))
	if err != nil || submitted.Version != 2 {
		t.Fatalf("submit: %#v %v", submitted, err)
	}
	_, err = r.TransitionPurchaseOrder(ctx, integrationTenantA, integrationOutletA, poID, "cancelled", 1, integrationAudit(t, integrationTenantA, integrationOutletA, "purchase_order", poID, "purchase_order.cancelled", now.Add(2*time.Second)))
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale PO transition=%v", err)
	}
	receiptID := newIntegrationUUID(t)
	received, err := r.ReceivePurchaseOrder(ctx, domain.GoodsReceipt{ID: receiptID, TenantID: integrationTenantA, OutletID: integrationOutletA, PurchaseOrderID: poID, ReceivedAt: now.Add(2 * time.Second), Lines: []domain.GoodsReceiptLine{{ID: newIntegrationUUID(t), PurchaseOrderLineID: lineID, IngredientID: ingredientID, UnitID: unitID, Quantity: 5, LotCode: "LOT-1"}}}, 2, integrationAudit(t, integrationTenantA, integrationOutletA, "goods_receipt", receiptID, "goods_receipt.recorded", now.Add(2*time.Second)))
	if err != nil || received.Status != "received" {
		t.Fatalf("receive: %#v %v", received, err)
	}
	summary, err := r.InventorySummary(ctx, integrationTenantA, integrationOutletA)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range summary {
		if row.IngredientID == ingredientID && row.QuantityBase == 5 && row.StockValueMinor == 5000 {
			found = true
		}
	}
	if !found {
		t.Fatalf("receipt not reflected in inventory: %#v", summary)
	}
	tempID := newIntegrationUUID(t)
	temp := domain.TemperatureLog{ID: tempID, TenantID: integrationTenantA, OutletID: integrationOutletA, Location: "Walk-in", TemperatureC: 4, SafeMinC: 1, SafeMaxC: 5, Compliant: true, MeasuredAt: now, ActorID: "tester"}
	if err := r.RecordTemperature(ctx, temp, integrationAudit(t, integrationTenantA, integrationOutletA, "temperature_log", tempID, "temperature.recorded", now)); err != nil {
		t.Fatal(err)
	}
	checkID, itemID := newIntegrationUUID(t), newIntegrationUUID(t)
	check := domain.OperationalChecklist{ID: checkID, TenantID: integrationTenantA, OutletID: integrationOutletA, ChecklistType: "opening", BusinessDate: now.Format("2006-01-02"), Status: "open", Version: 1, CreatedAt: now, UpdatedAt: now, Items: []domain.ChecklistItem{{ID: itemID, Label: "Check cold room", Required: true}}}
	if err := r.CreateChecklist(ctx, check, integrationAudit(t, integrationTenantA, integrationOutletA, "operational_checklist", checkID, "checklist.created", now)); err != nil {
		t.Fatal(err)
	}
	completed, err := r.CompleteChecklistItem(ctx, integrationTenantA, integrationOutletA, checkID, itemID, 1, integrationAudit(t, integrationTenantA, integrationOutletA, "operational_checklist", checkID, "checklist.item_completed", now.Add(time.Second)))
	if err != nil || completed.Status != "completed" {
		t.Fatalf("checklist: %#v %v", completed, err)
	}
	staffID, shiftID, taskID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	staff := domain.StaffMember{ID: staffID, TenantID: integrationTenantA, EmployeeCode: "E-" + staffID[:6], DisplayName: "Ops Chef", Role: "chef", Active: true, RecordMetadata: meta}
	if err := r.CreateStaffMember(ctx, staff, integrationAudit(t, integrationTenantA, integrationOutletA, "staff_member", staffID, "staff.created", now)); err != nil {
		t.Fatal(err)
	}
	shift := domain.StaffShift{ID: shiftID, TenantID: integrationTenantA, OutletID: integrationOutletA, StaffMemberID: staffID, StartsAt: now, EndsAt: now.Add(8 * time.Hour), Status: "scheduled", RecordMetadata: meta}
	if err := r.CreateShift(ctx, shift, integrationAudit(t, integrationTenantA, integrationOutletA, "staff_shift", shiftID, "shift.created", now)); err != nil {
		t.Fatal(err)
	}
	checked, err := r.TransitionShift(ctx, integrationTenantA, integrationOutletA, shiftID, "checked_in", 1, integrationAudit(t, integrationTenantA, integrationOutletA, "staff_shift", shiftID, "shift.checked_in", now.Add(time.Second)))
	if err != nil || checked.Status != "checked_in" {
		t.Fatalf("shift: %#v %v", checked, err)
	}
	task := domain.OperationalTask{ID: taskID, TenantID: integrationTenantA, OutletID: integrationOutletA, StaffMemberID: staffID, Title: "Sanitize line", Priority: "normal", Status: "open", RecordMetadata: meta}
	if err := r.CreateTask(ctx, task, integrationAudit(t, integrationTenantA, integrationOutletA, "operational_task", taskID, "task.created", now)); err != nil {
		t.Fatal(err)
	}
	done, err := r.TransitionTask(ctx, integrationTenantA, integrationOutletA, taskID, "completed", 1, integrationAudit(t, integrationTenantA, integrationOutletA, "operational_task", taskID, "task.completed", now.Add(time.Second)))
	if err != nil || done.Status != "completed" {
		t.Fatalf("task: %#v %v", done, err)
	}
}
