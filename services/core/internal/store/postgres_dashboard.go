// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"fmt"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

var dashboardUnavailableFields = []string{
	"online.orderCount",
	"online.prepaidMinor",
	"online.codMinor",
	"channels.aggregatorBreakdown",
	"channels.sourceMappings",
	"leakage.modifiedOrders",
	"leakage.modifiedKitchenTickets",
	"leakage.shiftedKitchenTickets",
	"leakage.reprintedBills",
	"leakage.waivedOffMinor",
	"leakage.cancellationOccurredAt",
}

// DailyDashboard builds a read-only, repeatable-read projection from durable
// operational facts. It intentionally leaves unsupported measures unavailable
// instead of inferring them from provider references or source strings.
func (r *PostgresRepository) DailyDashboard(ctx context.Context, request DailyDashboardRequest) (domain.DailyDashboard, error) {
	result := domain.DailyDashboard{
		OutletID:       request.OutletID,
		BusinessDate:   request.BusinessDate,
		AsOf:           request.AsOf.UTC(),
		TenderMix:      make([]domain.DashboardTenderMix, 0),
		FulfillmentMix: make([]domain.DashboardOrderTypeMix, 0),
		Hourly:         make([]domain.DashboardHourly, 0),
		TopItems:       make([]domain.DashboardTopItem, 0),
		Provenance: domain.DashboardProvenance{
			Sales:      "fiscal_receipts issued_at within the outlet business date",
			Orders:     "orders placed_at within the outlet business date",
			Payments:   "tenders occurred_at within the outlet business date; reversed tenders are refunds",
			Promotions: "promotion_redemptions occurred_at within the outlet business date; amounts use the outlet currency because redemption rows do not persist a separate currency",
			TopItems:   "order_lines joined to non-cancelled orders by orders.placed_at; line names and values are historical snapshots",
		},
		UnavailableFields: append([]string(nil), dashboardUnavailableFields...),
	}
	if request.AsOf.IsZero() {
		return result, fmt.Errorf("%w: dashboard asOf is required", ErrInvalidReference)
	}
	parsedDate, err := time.Parse("2006-01-02", request.BusinessDate)
	if err != nil || parsedDate.Format("2006-01-02") != request.BusinessDate {
		return result, fmt.Errorf("%w: dashboard business date must use YYYY-MM-DD", ErrInvalidReference)
	}

	err = r.withTenantReadSnapshot(ctx, request.TenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT time_zone, currency
			FROM outlets
			WHERE tenant_id=$1 AND id=$2
		`, request.TenantID, request.OutletID).Scan(&result.TimeZone, &result.Currency); err != nil {
			return err
		}

		location, err := time.LoadLocation(result.TimeZone)
		if err != nil {
			return fmt.Errorf("outlet %s has invalid time zone %q: %w", request.OutletID, result.TimeZone, err)
		}
		localStart := time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), 0, 0, 0, 0, location)
		localEnd := localStart.AddDate(0, 0, 1)
		result.Period = domain.DashboardPeriod{StartsAt: localStart.UTC(), EndsAt: localEnd.UTC(), BoundaryKind: "outlet_local_calendar_day"}

		if err := readDashboardSales(ctx, tx, request, &result); err != nil {
			return err
		}
		if err := readDashboardOrders(ctx, tx, request, &result); err != nil {
			return err
		}
		if err := readDashboardTenderMix(ctx, tx, request, &result); err != nil {
			return err
		}
		if err := readDashboardFulfillmentMix(ctx, tx, request, &result); err != nil {
			return err
		}
		if err := readDashboardHourly(ctx, tx, request, &result); err != nil {
			return err
		}
		if err := readDashboardPromotions(ctx, tx, request, &result); err != nil {
			return err
		}
		if err := readDashboardTopItems(ctx, tx, request, &result); err != nil {
			return err
		}

		if result.Orders.Unpriced > 0 {
			result.UnavailableFields = append(result.UnavailableFields, "orders.unpricedOrderValue")
		}
		return nil
	})
	return result, err
}

func (r *PostgresRepository) withTenantReadSnapshot(ctx context.Context, tenantID string, operation func(pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("postgres dashboard: begin read snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return fmt.Errorf("postgres dashboard: establish tenant: %w", err)
	}
	if err := operation(tx); err != nil {
		return repositoryErrorFromPostgres(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres dashboard: commit read snapshot: %w", err)
	}
	return nil
}

func readDashboardSales(ctx context.Context, tx pgx.Tx, request DailyDashboardRequest, result *domain.DailyDashboard) error {
	return tx.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE currency=$5),
			COALESCE(SUM(subtotal_minor) FILTER (WHERE currency=$5),0),
			COALESCE(SUM(discount_minor) FILTER (WHERE currency=$5),0),
			COALESCE(SUM(tax_minor) FILTER (WHERE currency=$5),0),
			COALESCE(SUM(service_charge_minor) FILTER (WHERE currency=$5),0),
			COALESCE(SUM(total_minor) FILTER (WHERE currency=$5),0),
			COUNT(*) FILTER (WHERE currency<>$5)
		FROM fiscal_receipts
		WHERE tenant_id=$1 AND outlet_id=$2
			AND issued_at >= $3 AND issued_at < $4 AND issued_at <= $6
	`, request.TenantID, request.OutletID, result.Period.StartsAt, result.Period.EndsAt, result.Currency, result.AsOf).Scan(
		&result.Sales.ReceiptedOrderCount,
		&result.Sales.SubtotalMinor,
		&result.Sales.DiscountMinor,
		&result.Sales.TaxMinor,
		&result.Sales.ServiceChargeMinor,
		&result.Sales.TotalMinor,
		&result.DataQuality.ReceiptCurrencyMismatchCount,
	)
}

func readDashboardOrders(ctx context.Context, tx pgx.Tx, request DailyDashboardRequest, result *domain.DailyDashboard) error {
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM orders
		WHERE tenant_id=$1 AND outlet_id=$2
			AND placed_at >= $3 AND placed_at < $4
			AND placed_at > $5 AND created_at <= $5
	`, request.TenantID, request.OutletID, result.Period.StartsAt, result.Period.EndsAt, result.AsOf).Scan(
		&result.DataQuality.FutureDatedOrderCount,
	); err != nil {
		return err
	}
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE status<>'cancelled'),
			COUNT(*) FILTER (WHERE status='completed'),
			COUNT(*) FILTER (WHERE status='cancelled'),
			COUNT(*) FILTER (WHERE status IN ('received','accepted','preparing','ready')),
			COUNT(*) FILTER (WHERE status<>'cancelled' AND total_minor=0),
			COALESCE(SUM(subtotal_minor) FILTER (WHERE status<>'cancelled' AND currency=$5),0),
			COALESCE(SUM(discount_total_minor) FILTER (WHERE status<>'cancelled' AND currency=$5),0),
			COALESCE(SUM(tax_total_minor) FILTER (WHERE status<>'cancelled' AND currency=$5),0),
			COALESCE(SUM(service_charge_minor) FILTER (WHERE status<>'cancelled' AND currency=$5),0),
			COALESCE(SUM(total_minor) FILTER (WHERE status<>'cancelled' AND currency=$5),0),
			COALESCE(SUM(total_minor) FILTER (WHERE status='cancelled' AND currency=$5),0),
			COUNT(*) FILTER (WHERE currency<>$5)
		FROM orders
		WHERE tenant_id=$1 AND outlet_id=$2
			AND placed_at >= $3 AND placed_at < $4 AND placed_at <= $6
	`, request.TenantID, request.OutletID, result.Period.StartsAt, result.Period.EndsAt, result.Currency, result.AsOf).Scan(
		&result.Orders.Total,
		&result.Orders.Included,
		&result.Orders.Completed,
		&result.Orders.Cancelled,
		&result.Orders.Active,
		&result.Orders.Unpriced,
		&result.Orders.SubtotalMinor,
		&result.Orders.DiscountMinor,
		&result.Orders.TaxMinor,
		&result.Orders.ServiceChargeMinor,
		&result.Orders.OrderValueMinor,
		&result.Leakage.CancelledOrderValueMinor,
		&result.DataQuality.OrderCurrencyMismatchCount,
	)
	if err != nil {
		return err
	}
	result.Leakage.CancelledOrderCount = result.Orders.Cancelled
	if result.Orders.Included > 0 && result.DataQuality.OrderCurrencyMismatchCount == 0 {
		average := result.Orders.OrderValueMinor / result.Orders.Included
		result.Orders.AverageOrderValueMinor = &average
	}
	return nil
}

func readDashboardTenderMix(ctx context.Context, tx pgx.Tx, request DailyDashboardRequest, result *domain.DailyDashboard) error {
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM tenders
		WHERE tenant_id=$1 AND outlet_id=$2 AND currency<>$5
			AND occurred_at >= $3 AND occurred_at < $4 AND occurred_at <= $6
	`, request.TenantID, request.OutletID, result.Period.StartsAt, result.Period.EndsAt, result.Currency, result.AsOf).Scan(
		&result.DataQuality.TenderCurrencyMismatchCount,
	); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT tender_type,
			COUNT(*) FILTER (WHERE status='captured'),
			COALESCE(SUM(amount_minor) FILTER (WHERE status='captured'),0),
			COUNT(*) FILTER (WHERE status='reversed'),
			COALESCE(SUM(amount_minor) FILTER (WHERE status='reversed'),0)
		FROM tenders
		WHERE tenant_id=$1 AND outlet_id=$2 AND currency=$5
			AND occurred_at >= $3 AND occurred_at < $4 AND occurred_at <= $6
		GROUP BY tender_type
		ORDER BY CASE tender_type WHEN 'cash' THEN 1 WHEN 'upi' THEN 2 WHEN 'card_terminal' THEN 3 ELSE 4 END
	`, request.TenantID, request.OutletID, result.Period.StartsAt, result.Period.EndsAt, result.Currency, result.AsOf)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var value domain.DashboardTenderMix
		if err := rows.Scan(&value.TenderType, &value.CapturedCount, &value.CapturedMinor, &value.RefundCount, &value.RefundMinor); err != nil {
			return err
		}
		value.NetMinor = value.CapturedMinor - value.RefundMinor
		result.TenderMix = append(result.TenderMix, value)
		result.Leakage.RefundCount += value.RefundCount
		result.Leakage.RefundMinor += value.RefundMinor
		result.PaymentFlow.CapturedCount += value.CapturedCount
		result.PaymentFlow.CapturedMinor += value.CapturedMinor
		result.PaymentFlow.RefundCount += value.RefundCount
		result.PaymentFlow.RefundMinor += value.RefundMinor
	}
	if err := rows.Err(); err != nil {
		return err
	}
	result.PaymentFlow.NetMinor = result.PaymentFlow.CapturedMinor - result.PaymentFlow.RefundMinor
	return nil
}

func readDashboardFulfillmentMix(ctx context.Context, tx pgx.Tx, request DailyDashboardRequest, result *domain.DailyDashboard) error {
	rows, err := tx.Query(ctx, `
		SELECT order_type, COUNT(*), COALESCE(SUM(total_minor),0)
		FROM orders
		WHERE tenant_id=$1 AND outlet_id=$2 AND status<>'cancelled' AND currency=$5
			AND placed_at >= $3 AND placed_at < $4 AND placed_at <= $6
		GROUP BY order_type
		ORDER BY CASE order_type WHEN 'dineIn' THEN 1 WHEN 'takeaway' THEN 2 WHEN 'delivery' THEN 3 ELSE 4 END
	`, request.TenantID, request.OutletID, result.Period.StartsAt, result.Period.EndsAt, result.Currency, result.AsOf)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var value domain.DashboardOrderTypeMix
		if err := rows.Scan(&value.OrderType, &value.OrderCount, &value.OrderValueMinor); err != nil {
			return err
		}
		result.FulfillmentMix = append(result.FulfillmentMix, value)
	}
	return rows.Err()
}

func readDashboardHourly(ctx context.Context, tx pgx.Tx, request DailyDashboardRequest, result *domain.DailyDashboard) error {
	rows, err := tx.Query(ctx, `
		SELECT EXTRACT(HOUR FROM placed_at AT TIME ZONE $6)::integer AS local_hour,
			placed_at - (
				(placed_at AT TIME ZONE $6)
				- date_trunc('hour', placed_at AT TIME ZONE $6)
			) AS starts_at,
			COUNT(*), COALESCE(SUM(total_minor),0)
		FROM orders
		WHERE tenant_id=$1 AND outlet_id=$2 AND status<>'cancelled' AND currency=$5
			AND placed_at >= $3 AND placed_at < $4 AND placed_at <= $7
		GROUP BY 1,2
		ORDER BY 2
	`, request.TenantID, request.OutletID, result.Period.StartsAt, result.Period.EndsAt, result.Currency, result.TimeZone, result.AsOf)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var value domain.DashboardHourly
		if err := rows.Scan(&value.LocalHour, &value.StartsAt, &value.OrderCount, &value.OrderValueMinor); err != nil {
			return err
		}
		value.StartsAt = value.StartsAt.UTC()
		result.Hourly = append(result.Hourly, value)
	}
	return rows.Err()
}

func readDashboardPromotions(ctx context.Context, tx pgx.Tx, request DailyDashboardRequest, result *domain.DailyDashboard) error {
	return tx.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(discount_minor),0)
		FROM promotion_redemptions
		WHERE tenant_id=$1 AND outlet_id=$2
			AND occurred_at >= $3 AND occurred_at < $4 AND occurred_at <= $5
	`, request.TenantID, request.OutletID, result.Period.StartsAt, result.Period.EndsAt, result.AsOf).Scan(
		&result.Leakage.PromotionRedemptionCount,
		&result.Leakage.PromotionDiscountMinor,
	)
}

func readDashboardTopItems(ctx context.Context, tx pgx.Tx, request DailyDashboardRequest, result *domain.DailyDashboard) error {
	if err := tx.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE lines.menu_item_id IS NULL OR item.id IS NULL),
			COUNT(*) FILTER (WHERE lines.currency<>$5)
		FROM order_lines lines
		JOIN orders placed
			ON placed.tenant_id=lines.tenant_id AND placed.id=lines.order_id
		LEFT JOIN menu_items item
			ON item.tenant_id=lines.tenant_id AND item.outlet_id=placed.outlet_id AND item.id=lines.menu_item_id
		WHERE placed.tenant_id=$1 AND placed.outlet_id=$2 AND placed.status<>'cancelled'
			AND placed.placed_at >= $3 AND placed.placed_at < $4 AND placed.placed_at <= $6
	`, request.TenantID, request.OutletID, result.Period.StartsAt, result.Period.EndsAt, result.Currency, result.AsOf).Scan(
		&result.DataQuality.UnlinkedMenuItemLineCount,
		&result.DataQuality.OrderLineCurrencyMismatchCount,
	); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT COALESCE(lines.menu_item_id::text,''), lines.name,
			SUM(lines.quantity)::bigint, SUM(lines.line_total_minor)::bigint
		FROM order_lines lines
		JOIN orders placed
			ON placed.tenant_id=lines.tenant_id AND placed.id=lines.order_id
		WHERE placed.tenant_id=$1 AND placed.outlet_id=$2 AND placed.status<>'cancelled'
			AND placed.currency=$5 AND lines.currency=$5
			AND placed.placed_at >= $3 AND placed.placed_at < $4 AND placed.placed_at <= $6
		GROUP BY lines.menu_item_id, lines.name
		ORDER BY SUM(lines.line_total_minor) DESC, SUM(lines.quantity) DESC, lines.name, lines.menu_item_id NULLS LAST
		LIMIT 10
	`, request.TenantID, request.OutletID, result.Period.StartsAt, result.Period.EndsAt, result.Currency, result.AsOf)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var value domain.DashboardTopItem
		if err := rows.Scan(&value.MenuItemID, &value.Name, &value.Quantity, &value.LineValueMinor); err != nil {
			return err
		}
		result.TopItems = append(result.TopItems, value)
	}
	return rows.Err()
}
