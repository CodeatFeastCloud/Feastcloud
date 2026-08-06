package store

import (
	"context"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"os"
	"testing"
	"time"
)

func TestNativeCommerceIntegration(t *testing.T) {
	url := os.Getenv("FEASTCLOUD_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set FEASTCLOUD_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, e := NewPostgresRepository(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	now := time.Now().UTC()
	tableID, sessionID, shiftID, orderID, lineID, tenderID, receiptID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	table := domain.DiningTable{ID: tableID, TenantID: integrationTenantA, OutletID: integrationOutletA, Label: "T-" + tableID[:6], Section: "Main", Capacity: 4, Status: "available", Version: 1, CreatedAt: now, UpdatedAt: now}
	if e := r.CreateDiningTable(ctx, table, integrationAudit(t, integrationTenantA, integrationOutletA, "dining_table", tableID, "dining_table.created", now)); e != nil {
		t.Fatal(e)
	}
	session := domain.DiningSession{ID: sessionID, TenantID: integrationTenantA, OutletID: integrationOutletA, TableID: tableID, Status: "open", GuestCount: 2, OpenedAt: now, Version: 1}
	if _, e := r.OpenDiningSession(ctx, session, integrationAudit(t, integrationTenantA, integrationOutletA, "dining_session", sessionID, "dining_session.opened", now)); e != nil {
		t.Fatal(e)
	}
	sessions, e := r.DiningSessions(ctx, integrationTenantA, integrationOutletA)
	if e != nil || len(sessions) == 0 {
		t.Fatalf("sessions: %v", e)
	}
	closed, e := r.CloseDiningSession(ctx, integrationTenantA, integrationOutletA, sessionID, 1, integrationAudit(t, integrationTenantA, integrationOutletA, "dining_session", sessionID, "dining_session.closed", now.Add(time.Second)))
	if e != nil || closed.Status != "closed" {
		t.Fatalf("close: %#v %v", closed, e)
	}
	tables, e := r.DiningTables(ctx, integrationTenantA, integrationOutletA)
	if e != nil {
		t.Fatal(e)
	}
	var cleaning domain.DiningTable
	for _, candidate := range tables {
		if candidate.ID == tableID {
			cleaning = candidate
		}
	}
	reset, e := r.TransitionDiningTable(ctx, integrationTenantA, integrationOutletA, tableID, "available", cleaning.Version, integrationAudit(t, integrationTenantA, integrationOutletA, "dining_table", tableID, "dining_table.status_changed", now.Add(1500*time.Millisecond)))
	if e != nil || reset.Status != "available" {
		t.Fatalf("reset table: %#v %v", reset, e)
	}
	shift := domain.CashShift{ID: shiftID, TenantID: integrationTenantA, OutletID: integrationOutletA, RegisterLabel: "POS-" + shiftID[:5], Status: "open", OpeningFloatMinor: 10000, ExpectedCashMinor: 10000, OpenedAt: now, Version: 1}
	if e := r.OpenCashShift(ctx, shift, integrationAudit(t, integrationTenantA, integrationOutletA, "cash_shift", shiftID, "cash_shift.opened", now)); e != nil {
		t.Fatal(e)
	}
	meta := domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}
	order := domain.Order{ID: orderID, TenantID: integrationTenantA, OutletID: integrationOutletA, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusReceived, Lines: []domain.OrderLine{{ID: lineID, Name: "Commerce test", Quantity: 1, UnitPrice: domain.Money{MinorUnits: 10500, Currency: "INR"}, LineTotal: domain.Money{MinorUnits: 10500, Currency: "INR"}}}, Subtotal: domain.Money{MinorUnits: 10000, Currency: "INR"}, DiscountTotal: domain.Money{Currency: "INR"}, TaxTotal: domain.Money{MinorUnits: 500, Currency: "INR"}, ServiceCharge: domain.Money{Currency: "INR"}, Total: domain.Money{MinorUnits: 10500, Currency: "INR"}, PlacedAt: now, RecordMetadata: meta}
	if e := r.CreateOrder(ctx, order, integrationAudit(t, integrationTenantA, integrationOutletA, "order", orderID, "order.created", now)); e != nil {
		t.Fatal(e)
	}
	tender := domain.Tender{ID: tenderID, TenantID: integrationTenantA, OutletID: integrationOutletA, OrderID: orderID, CashShiftID: shiftID, TenderType: "cash", AmountMinor: 10500, Currency: "INR", Status: "captured", OccurredAt: now}
	receipt := domain.FiscalReceipt{ID: receiptID, OrderID: orderID, ReceiptNumber: "R-" + receiptID[:8], IssuedAt: now}
	_, issued, e := r.CaptureTender(ctx, tender, receipt, integrationAudit(t, integrationTenantA, integrationOutletA, "tender", tenderID, "tender.captured", now))
	if e != nil || issued == nil || issued.TotalMinor != 10500 {
		t.Fatalf("tender: %#v %v", issued, e)
	}
	shifts, e := r.CashShifts(ctx, integrationTenantA, integrationOutletA)
	if e != nil {
		t.Fatal(e)
	}
	var current domain.CashShift
	for _, x := range shifts {
		if x.ID == shiftID {
			current = x
		}
	}
	if current.ExpectedCashMinor != 20500 {
		t.Fatalf("expected cash=%d", current.ExpectedCashMinor)
	}
	closedShift, e := r.CloseCashShift(ctx, integrationTenantA, integrationOutletA, shiftID, 1, 20500, integrationAudit(t, integrationTenantA, integrationOutletA, "cash_shift", shiftID, "cash_shift.closed", now.Add(2*time.Second)))
	if e != nil || closedShift.VarianceMinor == nil || *closedShift.VarianceMinor != 0 {
		t.Fatalf("close cash: %#v %v", closedShift, e)
	}
	settlements, e := r.GenerateSettlements(ctx, integrationTenantA, integrationOutletA, now.Format("2006-01-02"), integrationAudit(t, integrationTenantA, integrationOutletA, "tender_settlement", newIntegrationUUID(t), "settlement.generated", now.Add(3*time.Second)))
	if e != nil || len(settlements) == 0 {
		t.Fatalf("settlements: %#v %v", settlements, e)
	}
}
