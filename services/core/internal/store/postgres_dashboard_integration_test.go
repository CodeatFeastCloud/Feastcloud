// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func TestDailyDashboardPostgresIntegration(t *testing.T) {
	databaseURL := os.Getenv("FEASTCLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set FEASTCLOUD_TEST_DATABASE_URL to run PostgreSQL dashboard integration coverage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, err := NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL repository: %v", err)
	}
	defer repository.Close()

	metricsOutlet := newIntegrationUUID(t)
	boundaryOutlet := newIntegrationUUID(t)
	tenantBOutlet := newIntegrationUUID(t)
	start := time.Date(2026, 8, 2, 18, 30, 0, 0, time.UTC)
	end := time.Date(2026, 8, 3, 18, 30, 0, 0, time.UTC)
	asOf := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)

	if err := repository.withTenant(ctx, integrationTenantA, func(tx pgx.Tx) error {
		if err := insertDashboardOutlet(ctx, tx, integrationTenantA, metricsOutlet, "metrics-"+metricsOutlet[:8], "Asia/Kolkata", "INR", start); err != nil {
			return err
		}
		if err := insertDashboardOutlet(ctx, tx, integrationTenantA, boundaryOutlet, "boundary-"+boundaryOutlet[:8], "Asia/Kolkata", "INR", start); err != nil {
			return err
		}

		normalOrder := dashboardOrderSeed{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: metricsOutlet, Type: domain.OrderTypeDelivery, Status: domain.OrderStatusCompleted, Currency: "INR", SubtotalMinor: 10000, DiscountMinor: 1000, TaxMinor: 900, ServiceChargeMinor: 100, TotalMinor: 10000, PlacedAt: start.Add(7*time.Hour + 30*time.Minute), CreatedAt: start.Add(7 * time.Hour), LineName: "Historical masala bowl", LineQuantity: 2, LineUnitMinor: 5000}
		cancelledOrder := dashboardOrderSeed{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: metricsOutlet, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusCancelled, Currency: "INR", SubtotalMinor: 7000, TotalMinor: 7000, PlacedAt: start.Add(8 * time.Hour), CreatedAt: start.Add(8 * time.Hour), LineName: "Cancelled platter", LineQuantity: 1, LineUnitMinor: 7000}
		activeOrder := dashboardOrderSeed{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: metricsOutlet, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusPreparing, Currency: "INR", SubtotalMinor: 3000, TotalMinor: 3000, PlacedAt: start.Add(9 * time.Hour), CreatedAt: start.Add(9 * time.Hour), LineName: "Fresh lime", LineQuantity: 1, LineUnitMinor: 3000}
		foreignOrder := dashboardOrderSeed{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: metricsOutlet, Type: domain.OrderTypeDineIn, Status: domain.OrderStatusCompleted, Currency: "USD", SubtotalMinor: 5000, TotalMinor: 5000, PlacedAt: start.Add(10 * time.Hour), CreatedAt: start.Add(10 * time.Hour), LineName: "Foreign currency line", LineQuantity: 1, LineUnitMinor: 5000}
		futureOrder := dashboardOrderSeed{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: metricsOutlet, Type: domain.OrderTypeDelivery, Status: domain.OrderStatusCompleted, Currency: "INR", SubtotalMinor: 4000, TotalMinor: 4000, PlacedAt: asOf.Add(2 * time.Hour), CreatedAt: asOf.Add(-time.Hour), LineName: "Clock skew order", LineQuantity: 1, LineUnitMinor: 4000}
		priorOrder := dashboardOrderSeed{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: metricsOutlet, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusCompleted, Currency: "INR", SubtotalMinor: 2000, TotalMinor: 2000, PlacedAt: start.Add(-6 * time.Hour), CreatedAt: start.Add(-6 * time.Hour)}
		for _, order := range []dashboardOrderSeed{normalOrder, cancelledOrder, activeOrder, foreignOrder, futureOrder, priorOrder} {
			if err := insertDashboardOrder(ctx, tx, t, order); err != nil {
				return err
			}
		}

		normalReceiptAt := normalOrder.PlacedAt.Add(5 * time.Minute)
		foreignReceiptAt := foreignOrder.PlacedAt.Add(5 * time.Minute)
		futureReceiptAt := futureOrder.PlacedAt.Add(5 * time.Minute)
		priorReceiptAt := priorOrder.PlacedAt.Add(5 * time.Minute)
		if err := insertDashboardReceipt(ctx, tx, t, normalOrder, normalReceiptAt); err != nil {
			return err
		}
		if err := insertDashboardReceipt(ctx, tx, t, foreignOrder, foreignReceiptAt); err != nil {
			return err
		}
		if err := insertDashboardReceipt(ctx, tx, t, futureOrder, futureReceiptAt); err != nil {
			return err
		}
		if err := insertDashboardReceipt(ctx, tx, t, priorOrder, priorReceiptAt); err != nil {
			return err
		}

		normalTender := newIntegrationUUID(t)
		priorTender := newIntegrationUUID(t)
		if err := insertDashboardTender(ctx, tx, t, integrationTenantA, metricsOutlet, normalOrder.ID, normalTender, "upi", 10000, "INR", "captured", "", normalReceiptAt); err != nil {
			return err
		}
		if err := insertDashboardTender(ctx, tx, t, integrationTenantA, metricsOutlet, normalOrder.ID, newIntegrationUUID(t), "upi", 2500, "INR", "reversed", normalTender, normalReceiptAt.Add(time.Hour)); err != nil {
			return err
		}
		if err := insertDashboardTender(ctx, tx, t, integrationTenantA, metricsOutlet, priorOrder.ID, priorTender, "upi", 2000, "INR", "captured", "", priorReceiptAt); err != nil {
			return err
		}
		if err := insertDashboardTender(ctx, tx, t, integrationTenantA, metricsOutlet, priorOrder.ID, newIntegrationUUID(t), "upi", 1000, "INR", "reversed", priorTender, normalReceiptAt.Add(2*time.Hour)); err != nil {
			return err
		}
		if err := insertDashboardTender(ctx, tx, t, integrationTenantA, metricsOutlet, foreignOrder.ID, newIntegrationUUID(t), "external", 5000, "USD", "captured", "", foreignReceiptAt); err != nil {
			return err
		}
		if err := insertDashboardTender(ctx, tx, t, integrationTenantA, metricsOutlet, futureOrder.ID, newIntegrationUUID(t), "upi", 4000, "INR", "captured", "", futureReceiptAt); err != nil {
			return err
		}

		promotionID := newIntegrationUUID(t)
		if _, err := tx.Exec(ctx, `INSERT INTO promotions
			(id,tenant_id,outlet_id,code,name,discount_type,discount_value,min_order_minor,starts_at,ends_at,active,version,created_at,updated_at)
			VALUES($1,$2,$3,$4,'Dashboard promotion','fixed',500,0,$5,$6,true,1,$5,$5)`,
			promotionID, integrationTenantA, metricsOutlet, "DASH-"+promotionID[:8], start.Add(-time.Hour), end.Add(time.Hour)); err != nil {
			return err
		}
		for _, redemption := range []struct {
			at       time.Time
			discount int64
		}{{normalReceiptAt.Add(30 * time.Minute), 500}, {asOf.Add(time.Hour), 800}} {
			if _, err := tx.Exec(ctx, `INSERT INTO promotion_redemptions
				(id,tenant_id,outlet_id,promotion_id,basket_minor,discount_minor,occurred_at,actor_id,operation_id)
				VALUES($1,$2,$3,$4,10000,$5,$6,'dashboard-test',$7)`,
				newIntegrationUUID(t), integrationTenantA, metricsOutlet, promotionID, redemption.discount, redemption.at, newIntegrationUUID(t)); err != nil {
				return err
			}
		}

		for _, fixture := range []dashboardOrderSeed{
			{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: boundaryOutlet, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusCompleted, Currency: "INR", SubtotalMinor: 100, TotalMinor: 100, PlacedAt: start.Add(-time.Microsecond), CreatedAt: start.Add(-time.Microsecond)},
			{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: boundaryOutlet, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusCompleted, Currency: "INR", SubtotalMinor: 200, TotalMinor: 200, PlacedAt: start, CreatedAt: start},
			{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: boundaryOutlet, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusCompleted, Currency: "INR", SubtotalMinor: 300, TotalMinor: 300, PlacedAt: end.Add(-time.Microsecond), CreatedAt: end.Add(-time.Microsecond)},
			{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: boundaryOutlet, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusCompleted, Currency: "INR", SubtotalMinor: 400, TotalMinor: 400, PlacedAt: end, CreatedAt: end},
		} {
			if err := insertDashboardOrder(ctx, tx, t, fixture); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed tenant A dashboard facts: %v", err)
	}

	if err := repository.withTenant(ctx, integrationTenantB, func(tx pgx.Tx) error {
		if err := insertDashboardOutlet(ctx, tx, integrationTenantB, tenantBOutlet, "tenant-b-"+tenantBOutlet[:8], "Asia/Kolkata", "INR", start); err != nil {
			return err
		}
		return insertDashboardOrder(ctx, tx, t, dashboardOrderSeed{ID: newIntegrationUUID(t), TenantID: integrationTenantB, OutletID: tenantBOutlet, Type: domain.OrderTypeDelivery, Status: domain.OrderStatusCompleted, Currency: "INR", SubtotalMinor: 99000, TotalMinor: 99000, PlacedAt: start.Add(9 * time.Hour), CreatedAt: start.Add(9 * time.Hour)})
	}); err != nil {
		t.Fatalf("seed tenant B dashboard facts: %v", err)
	}

	dashboard, err := repository.DailyDashboard(ctx, DailyDashboardRequest{TenantID: integrationTenantA, OutletID: metricsOutlet, BusinessDate: "2026-08-03", AsOf: asOf})
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	if !dashboard.Period.StartsAt.Equal(start) || !dashboard.Period.EndsAt.Equal(end) || dashboard.Period.BoundaryKind != "outlet_local_calendar_day" {
		t.Fatalf("period=%#v; want [%s,%s) outlet-local calendar day", dashboard.Period, start, end)
	}
	if dashboard.Currency != "INR" || dashboard.TimeZone != "Asia/Kolkata" || !dashboard.AsOf.Equal(asOf) {
		t.Fatalf("scope metadata=%s/%s/%s", dashboard.Currency, dashboard.TimeZone, dashboard.AsOf)
	}
	if dashboard.Orders.Total != 4 || dashboard.Orders.Included != 3 || dashboard.Orders.Completed != 2 || dashboard.Orders.Cancelled != 1 || dashboard.Orders.Active != 1 {
		t.Fatalf("order counts=%#v", dashboard.Orders)
	}
	if dashboard.Orders.OrderValueMinor != 13000 || dashboard.Orders.SubtotalMinor != 13000 || dashboard.Orders.AverageOrderValueMinor != nil {
		t.Fatalf("currency-safe order values=%#v", dashboard.Orders)
	}
	if dashboard.Sales.ReceiptedOrderCount != 1 || dashboard.Sales.TotalMinor != 10000 || dashboard.Sales.DiscountMinor != 1000 {
		t.Fatalf("receipt sales=%#v", dashboard.Sales)
	}
	if dashboard.PaymentFlow.CapturedCount != 1 || dashboard.PaymentFlow.CapturedMinor != 10000 || dashboard.PaymentFlow.RefundCount != 2 || dashboard.PaymentFlow.RefundMinor != 3500 || dashboard.PaymentFlow.NetMinor != 6500 {
		t.Fatalf("payment flow=%#v", dashboard.PaymentFlow)
	}
	if dashboard.Leakage.CancelledOrderCount != 1 || dashboard.Leakage.CancelledOrderValueMinor != 7000 || dashboard.Leakage.RefundMinor != 3500 || dashboard.Leakage.PromotionRedemptionCount != 1 || dashboard.Leakage.PromotionDiscountMinor != 500 {
		t.Fatalf("leakage signals=%#v", dashboard.Leakage)
	}
	if dashboard.DataQuality.FutureDatedOrderCount != 1 || dashboard.DataQuality.OrderCurrencyMismatchCount != 1 || dashboard.DataQuality.ReceiptCurrencyMismatchCount != 1 || dashboard.DataQuality.TenderCurrencyMismatchCount != 1 || dashboard.DataQuality.OrderLineCurrencyMismatchCount != 1 || dashboard.DataQuality.UnlinkedMenuItemLineCount != 3 {
		t.Fatalf("data quality=%#v", dashboard.DataQuality)
	}
	if len(dashboard.FulfillmentMix) != 2 || len(dashboard.Hourly) != 2 || len(dashboard.TopItems) != 2 || dashboard.TopItems[0].Name != "Historical masala bowl" || dashboard.TopItems[0].Quantity != 2 || dashboard.TopItems[0].LineValueMinor != 10000 {
		t.Fatalf("mix/hour/items=%#v/%#v/%#v", dashboard.FulfillmentMix, dashboard.Hourly, dashboard.TopItems)
	}
	if dashboard.Hourly[0].LocalHour != 7 || !dashboard.Hourly[0].StartsAt.Equal(time.Date(2026, 8, 3, 1, 30, 0, 0, time.UTC)) || dashboard.Hourly[1].LocalHour != 9 || !dashboard.Hourly[1].StartsAt.Equal(time.Date(2026, 8, 3, 3, 30, 0, 0, time.UTC)) {
		t.Fatalf("outlet-aligned hourly buckets=%#v", dashboard.Hourly)
	}
	for _, unavailable := range []string{"online.prepaidMinor", "online.codMinor", "channels.sourceMappings", "leakage.modifiedKitchenTickets", "leakage.shiftedKitchenTickets", "leakage.reprintedBills", "leakage.waivedOffMinor"} {
		if !slices.Contains(dashboard.UnavailableFields, unavailable) {
			t.Errorf("unavailableFields missing %q: %#v", unavailable, dashboard.UnavailableFields)
		}
	}

	boundary, err := repository.DailyDashboard(ctx, DailyDashboardRequest{TenantID: integrationTenantA, OutletID: boundaryOutlet, BusinessDate: "2026-08-03", AsOf: end.Add(24 * time.Hour)})
	if err != nil || boundary.Orders.Total != 2 || boundary.Orders.OrderValueMinor != 500 {
		t.Fatalf("half-open first date=%#v/%v", boundary.Orders, err)
	}
	nextDate, err := repository.DailyDashboard(ctx, DailyDashboardRequest{TenantID: integrationTenantA, OutletID: boundaryOutlet, BusinessDate: "2026-08-04", AsOf: end.Add(48 * time.Hour)})
	if err != nil || nextDate.Orders.Total != 1 || nextDate.Orders.OrderValueMinor != 400 {
		t.Fatalf("half-open next date=%#v/%v", nextDate.Orders, err)
	}
	empty, err := repository.DailyDashboard(ctx, DailyDashboardRequest{TenantID: integrationTenantA, OutletID: metricsOutlet, BusinessDate: "2026-08-10", AsOf: end.Add(10 * 24 * time.Hour)})
	if err != nil || empty.Orders.Total != 0 || empty.Sales.TotalMinor != 0 || empty.PaymentFlow.NetMinor != 0 || empty.TenderMix == nil || empty.FulfillmentMix == nil || empty.Hourly == nil || empty.TopItems == nil {
		t.Fatalf("empty day=%#v/%v", empty, err)
	}
	tenantB, err := repository.DailyDashboard(ctx, DailyDashboardRequest{TenantID: integrationTenantB, OutletID: tenantBOutlet, BusinessDate: "2026-08-03", AsOf: asOf})
	if err != nil || tenantB.Orders.Total != 1 || tenantB.Orders.OrderValueMinor != 99000 {
		t.Fatalf("tenant B dashboard=%#v/%v", tenantB.Orders, err)
	}
	if _, err := repository.DailyDashboard(ctx, DailyDashboardRequest{TenantID: integrationTenantB, OutletID: metricsOutlet, BusinessDate: "2026-08-03", AsOf: asOf}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant outlet read error=%v; want not found", err)
	}
}

type dashboardOrderSeed struct {
	ID, TenantID, OutletID string
	Type                   domain.OrderType
	Status                 domain.OrderStatus
	Currency               string
	SubtotalMinor          int64
	DiscountMinor          int64
	TaxMinor               int64
	ServiceChargeMinor     int64
	TotalMinor             int64
	PlacedAt, CreatedAt    time.Time
	LineName               string
	LineQuantity           int32
	LineUnitMinor          int64
}

func insertDashboardOutlet(ctx context.Context, tx pgx.Tx, tenantID, outletID, code, timeZone, currency string, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO outlets
		(id,tenant_id,organization_id,name,code,time_zone,currency,active,version,created_at,updated_at)
		VALUES($1,$2,$2,'Dashboard integration outlet',$3,$4,$5,true,1,$6,$6)`,
		outletID, tenantID, code, timeZone, currency, at)
	return err
}

func insertDashboardOrder(ctx context.Context, tx pgx.Tx, t *testing.T, value dashboardOrderSeed) error {
	_, err := tx.Exec(ctx, `INSERT INTO orders
		(id,tenant_id,outlet_id,source,order_type,status,currency,subtotal_minor,discount_total_minor,tax_total_minor,service_charge_minor,total_minor,placed_at,version,created_at,updated_at)
		VALUES($1,$2,$3,'dashboard-integration',$4,$5,$6,$7,$8,$9,$10,$11,$12,1,$13,$13)`,
		value.ID, value.TenantID, value.OutletID, value.Type, value.Status, value.Currency,
		value.SubtotalMinor, value.DiscountMinor, value.TaxMinor, value.ServiceChargeMinor, value.TotalMinor,
		value.PlacedAt, value.CreatedAt)
	if err != nil || value.LineName == "" {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO order_lines
		(id,tenant_id,order_id,name,quantity,currency,unit_price_minor,line_total_minor,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, newIntegrationUUID(t), value.TenantID, value.ID,
		value.LineName, value.LineQuantity, value.Currency, value.LineUnitMinor,
		value.LineUnitMinor*int64(value.LineQuantity), value.CreatedAt)
	return err
}

func insertDashboardReceipt(ctx context.Context, tx pgx.Tx, t *testing.T, order dashboardOrderSeed, issuedAt time.Time) error {
	receiptID := newIntegrationUUID(t)
	_, err := tx.Exec(ctx, `INSERT INTO fiscal_receipts
		(id,tenant_id,outlet_id,order_id,receipt_number,currency,subtotal_minor,discount_minor,tax_minor,service_charge_minor,total_minor,tax_snapshot,issued_at,actor_id,operation_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'{}'::jsonb,$12,'dashboard-test',$13)`,
		receiptID, order.TenantID, order.OutletID, order.ID, "DASH-"+receiptID[:8], order.Currency,
		order.SubtotalMinor, order.DiscountMinor, order.TaxMinor, order.ServiceChargeMinor, order.TotalMinor,
		issuedAt, newIntegrationUUID(t))
	return err
}

func insertDashboardTender(ctx context.Context, tx pgx.Tx, t *testing.T, tenantID, outletID, orderID, tenderID, tenderType string, amount int64, currency, status, reversesID string, occurredAt time.Time) error {
	var reverses any
	if reversesID != "" {
		reverses = reversesID
	}
	_, err := tx.Exec(ctx, `INSERT INTO tenders
		(id,tenant_id,outlet_id,order_id,tender_type,amount_minor,currency,provider_reference,status,reverses_tender_id,occurred_at,actor_id,operation_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,'',$8,$9,$10,'dashboard-test',$11)`,
		tenderID, tenantID, outletID, orderID, tenderType, amount, currency, status, reverses, occurredAt, newIntegrationUUID(t))
	return err
}
