package store

import (
	"context"
	"fmt"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) SetMenuAvailability(ctx context.Context, tenant, outlet string, v domain.MenuAvailability, a domain.AuditEvent) (domain.MenuAvailability, error) {
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO menu_item_availability(tenant_id,outlet_id,menu_item_id,available,reason,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,outlet_id,menu_item_id) DO UPDATE SET available=EXCLUDED.available,reason=EXCLUDED.reason,version=menu_item_availability.version+1,updated_at=EXCLUDED.updated_at`, tenant, outlet, v.MenuItemID, v.Available, v.Reason, a.RecordedAt)
		if err != nil {
			return err
		}
		v.UpdatedAt = a.RecordedAt
		if err := tx.QueryRow(ctx, `SELECT version FROM menu_item_availability WHERE tenant_id=$1 AND outlet_id=$2 AND menu_item_id=$3`, tenant, outlet, v.MenuItemID).Scan(&v.Version); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO menu_availability_events(id,tenant_id,outlet_id,menu_item_id,available,reason,actor_id,occurred_at,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.ID, tenant, outlet, v.MenuItemID, v.Available, v.Reason, a.ActorID, a.RecordedAt, a.OperationID)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
	return v, err
}
func (r *PostgresRepository) MenuAvailability(ctx context.Context, t, o string) ([]domain.MenuAvailability, error) {
	v := []domain.MenuAvailability{}
	err := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT m.id,m.name,COALESCE(a.available,true),COALESCE(a.reason,''),COALESCE(a.version,1),COALESCE(a.updated_at,m.updated_at) FROM menu_items m LEFT JOIN menu_item_availability a ON a.tenant_id=m.tenant_id AND a.outlet_id=m.outlet_id AND a.menu_item_id=m.id WHERE m.tenant_id=$1 AND m.outlet_id=$2 ORDER BY m.name`, t, o)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x domain.MenuAvailability
			if e := rows.Scan(&x.MenuItemID, &x.MenuItemName, &x.Available, &x.Reason, &x.Version, &x.UpdatedAt); e != nil {
				return e
			}
			v = append(v, x)
		}
		return rows.Err()
	})
	return v, err
}
func (r *PostgresRepository) CreateDiningTable(ctx context.Context, v domain.DiningTable, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO dining_tables(id,tenant_id,outlet_id,label,section,capacity,status,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,'available',$7,$7)`, v.ID, v.TenantID, v.OutletID, v.Label, v.Section, v.Capacity, v.CreatedAt)
		if e != nil {
			return e
		}
		return insertAudit(ctx, tx, a)
	})
}
func (r *PostgresRepository) DiningTables(ctx context.Context, t, o string) ([]domain.DiningTable, error) {
	v := []domain.DiningTable{}
	e := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,label,section,capacity,status,version,created_at,updated_at FROM dining_tables WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY section,label`, t, o)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x domain.DiningTable
			if e := rows.Scan(&x.ID, &x.TenantID, &x.OutletID, &x.Label, &x.Section, &x.Capacity, &x.Status, &x.Version, &x.CreatedAt, &x.UpdatedAt); e != nil {
				return e
			}
			v = append(v, x)
		}
		return rows.Err()
	})
	return v, e
}
func (r *PostgresRepository) TransitionDiningTable(ctx context.Context, t, o, id, status string, expected uint64, a domain.AuditEvent) (domain.DiningTable, error) {
	var v domain.DiningTable
	err := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,label,section,capacity,status,version,created_at,updated_at FROM dining_tables WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, t, o, id).Scan(&v.ID, &v.TenantID, &v.OutletID, &v.Label, &v.Section, &v.Capacity, &v.Status, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return err
		}
		if v.Version != expected {
			return ErrVersionConflict
		}
		allowed := (v.Status == "cleaning" && status == "available") || (v.Status == "available" && status == "disabled") || (v.Status == "disabled" && status == "available")
		if !allowed {
			return ErrInvalidTransition
		}
		if _, err := tx.Exec(ctx, `UPDATE dining_tables SET status=$4,version=version+1,updated_at=$5 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, t, o, id, status, a.RecordedAt); err != nil {
			return err
		}
		v.Status = status
		v.Version++
		v.UpdatedAt = a.RecordedAt
		return insertAudit(ctx, tx, a)
	})
	return v, err
}
func (r *PostgresRepository) OpenDiningSession(ctx context.Context, v domain.DiningSession, a domain.AuditEvent) (domain.DiningSession, error) {
	e := r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE dining_tables SET status='occupied',version=version+1,updated_at=$4 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND status='available'`, v.TenantID, v.OutletID, v.TableID, a.RecordedAt)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrInvalidTransition
		}
		_, e = tx.Exec(ctx, `INSERT INTO dining_sessions(id,tenant_id,outlet_id,table_id,status,guest_count,guest_name,opened_at)VALUES($1,$2,$3,$4,'open',$5,$6,$7)`, v.ID, v.TenantID, v.OutletID, v.TableID, v.GuestCount, v.GuestName, v.OpenedAt)
		if e != nil {
			return e
		}
		return insertAudit(ctx, tx, a)
	})
	return v, e
}
func (r *PostgresRepository) DiningSessions(ctx context.Context, t, o string) ([]domain.DiningSession, error) {
	values := []domain.DiningSession{}
	err := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT s.id,s.tenant_id,s.outlet_id,s.table_id,d.label,s.status,s.guest_count,s.guest_name,s.opened_at,s.closed_at,s.version FROM dining_sessions s JOIN dining_tables d ON d.tenant_id=s.tenant_id AND d.id=s.table_id WHERE s.tenant_id=$1 AND s.outlet_id=$2 ORDER BY CASE s.status WHEN 'open' THEN 0 ELSE 1 END,s.opened_at DESC LIMIT 100`, t, o)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.DiningSession
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.TableID, &v.TableLabel, &v.Status, &v.GuestCount, &v.GuestName, &v.OpenedAt, &v.ClosedAt, &v.Version); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func (r *PostgresRepository) CloseDiningSession(ctx context.Context, t, o, id string, expected uint64, a domain.AuditEvent) (domain.DiningSession, error) {
	var v domain.DiningSession
	e := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		e := tx.QueryRow(ctx, `SELECT s.id,s.tenant_id,s.outlet_id,s.table_id,d.label,s.status,s.guest_count,s.guest_name,s.opened_at,s.closed_at,s.version FROM dining_sessions s JOIN dining_tables d ON d.tenant_id=s.tenant_id AND d.id=s.table_id WHERE s.tenant_id=$1 AND s.outlet_id=$2 AND s.id=$3 FOR UPDATE OF s`, t, o, id).Scan(&v.ID, &v.TenantID, &v.OutletID, &v.TableID, &v.TableLabel, &v.Status, &v.GuestCount, &v.GuestName, &v.OpenedAt, &v.ClosedAt, &v.Version)
		if e != nil {
			return e
		}
		if v.Version != expected {
			return ErrVersionConflict
		}
		if v.Status != "open" {
			return ErrInvalidTransition
		}
		_, e = tx.Exec(ctx, `UPDATE dining_sessions SET status='closed',closed_at=$4,version=version+1 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, t, o, id, a.RecordedAt)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE dining_tables SET status='cleaning',version=version+1,updated_at=$3 WHERE tenant_id=$1 AND id=$2`, t, v.TableID, a.RecordedAt)
		if e != nil {
			return e
		}
		v.Status = "closed"
		v.Version++
		now := a.RecordedAt
		v.ClosedAt = &now
		return insertAudit(ctx, tx, a)
	})
	return v, e
}
func (r *PostgresRepository) OpenCashShift(ctx context.Context, v domain.CashShift, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO cash_shifts(id,tenant_id,outlet_id,register_label,status,opening_float_minor,expected_cash_minor,opened_at,actor_id)VALUES($1,$2,$3,$4,'open',$5,$5,$6,$7)`, v.ID, v.TenantID, v.OutletID, v.RegisterLabel, v.OpeningFloatMinor, v.OpenedAt, a.ActorID)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO cash_events(id,tenant_id,cash_shift_id,event_type,amount_minor,reason,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,'opening_float',$4,'opening float',$5,$6,$7)`, a.ID, v.TenantID, v.ID, v.OpeningFloatMinor, a.RecordedAt, a.ActorID, a.OperationID)
		if e != nil {
			return e
		}
		return insertAudit(ctx, tx, a)
	})
}
func (r *PostgresRepository) CashShifts(ctx context.Context, t, o string) ([]domain.CashShift, error) {
	v := []domain.CashShift{}
	e := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,register_label,status,opening_float_minor,expected_cash_minor,closing_count_minor,variance_minor,opened_at,closed_at,version FROM cash_shifts WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY opened_at DESC`, t, o)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x domain.CashShift
			if e := rows.Scan(&x.ID, &x.TenantID, &x.OutletID, &x.RegisterLabel, &x.Status, &x.OpeningFloatMinor, &x.ExpectedCashMinor, &x.ClosingCountMinor, &x.VarianceMinor, &x.OpenedAt, &x.ClosedAt, &x.Version); e != nil {
				return e
			}
			v = append(v, x)
		}
		return rows.Err()
	})
	return v, e
}
func (r *PostgresRepository) CloseCashShift(ctx context.Context, t, o, id string, expected uint64, count int64, a domain.AuditEvent) (domain.CashShift, error) {
	var v domain.CashShift
	e := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		e := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,register_label,status,opening_float_minor,expected_cash_minor,closing_count_minor,variance_minor,opened_at,closed_at,version FROM cash_shifts WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, t, o, id).Scan(&v.ID, &v.TenantID, &v.OutletID, &v.RegisterLabel, &v.Status, &v.OpeningFloatMinor, &v.ExpectedCashMinor, &v.ClosingCountMinor, &v.VarianceMinor, &v.OpenedAt, &v.ClosedAt, &v.Version)
		if e != nil {
			return e
		}
		if v.Version != expected {
			return ErrVersionConflict
		}
		variance := count - v.ExpectedCashMinor
		_, e = tx.Exec(ctx, `UPDATE cash_shifts SET status='closed',closing_count_minor=$4,variance_minor=$5,closed_at=$6,version=version+1 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, t, o, id, count, variance, a.RecordedAt)
		if e != nil {
			return e
		}
		v.Status = "closed"
		v.Version++
		v.ClosingCountMinor = &count
		v.VarianceMinor = &variance
		return insertAudit(ctx, tx, a)
	})
	return v, e
}
func (r *PostgresRepository) CaptureTender(ctx context.Context, v domain.Tender, receipt domain.FiscalReceipt, a domain.AuditEvent) (domain.Tender, *domain.FiscalReceipt, error) {
	var issued *domain.FiscalReceipt
	e := r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		var total, paid int64
		if err := tx.QueryRow(ctx, `SELECT total_minor,(SELECT COALESCE(SUM(CASE status WHEN 'captured' THEN amount_minor ELSE -amount_minor END),0) FROM tenders WHERE tenant_id=$1 AND order_id=$3) FROM orders WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, v.TenantID, v.OutletID, v.OrderID).Scan(&total, &paid); err != nil {
			return err
		}
		if paid+v.AmountMinor > total {
			return fmt.Errorf("%w: tender exceeds unpaid balance", ErrInvalidReference)
		}
		if v.TenderType == "cash" {
			tag, e := tx.Exec(ctx, `UPDATE cash_shifts SET expected_cash_minor=expected_cash_minor+$4 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND status='open'`, v.TenantID, v.OutletID, v.CashShiftID, v.AmountMinor)
			if e != nil {
				return e
			}
			if tag.RowsAffected() != 1 {
				return ErrInvalidReference
			}
		}
		_, e := tx.Exec(ctx, `INSERT INTO tenders(id,tenant_id,outlet_id,order_id,cash_shift_id,tender_type,amount_minor,currency,provider_reference,status,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'captured',$10,$11,$12)`, v.ID, v.TenantID, v.OutletID, v.OrderID, nullable(v.CashShiftID), v.TenderType, v.AmountMinor, v.Currency, v.ProviderReference, v.OccurredAt, a.ActorID, a.OperationID)
		if e != nil {
			return e
		}
		if v.TenderType == "cash" {
			_, e = tx.Exec(ctx, `INSERT INTO cash_events(id,tenant_id,cash_shift_id,event_type,amount_minor,reason,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,'cash_sale',$4,'order tender',$5,$6,$7)`, inventoryEventUUID(v.TenantID, a.OperationID, "cash"), v.TenantID, v.CashShiftID, v.AmountMinor, a.RecordedAt, a.ActorID, a.OperationID)
			if e != nil {
				return e
			}
		}
		paid += v.AmountMinor
		if paid >= total {
			receipt.TotalMinor = total
			e = tx.QueryRow(ctx, `SELECT subtotal_minor,discount_total_minor,tax_total_minor,service_charge_minor,currency FROM orders WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, v.TenantID, v.OutletID, v.OrderID).Scan(&receipt.SubtotalMinor, &receipt.DiscountMinor, &receipt.TaxMinor, &receipt.ServiceChargeMinor, &receipt.Currency)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `INSERT INTO fiscal_receipts(id,tenant_id,outlet_id,order_id,receipt_number,currency,subtotal_minor,discount_minor,tax_minor,service_charge_minor,total_minor,tax_snapshot,issued_at,actor_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'{"scheme":"captured-order-values"}',$12,$13,$14) ON CONFLICT(tenant_id,order_id) DO NOTHING`, receipt.ID, v.TenantID, v.OutletID, v.OrderID, receipt.ReceiptNumber, receipt.Currency, receipt.SubtotalMinor, receipt.DiscountMinor, receipt.TaxMinor, receipt.ServiceChargeMinor, receipt.TotalMinor, receipt.IssuedAt, a.ActorID, a.OperationID)
			if e != nil {
				return e
			}
			issued = &receipt
		}
		return insertAudit(ctx, tx, a)
	})
	return v, issued, e
}
func (r *PostgresRepository) Tenders(ctx context.Context, t, o string) ([]domain.Tender, error) {
	values := []domain.Tender{}
	err := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,order_id,COALESCE(cash_shift_id::text,''),tender_type,amount_minor,currency,provider_reference,status,COALESCE(reverses_tender_id::text,''),occurred_at FROM tenders WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY occurred_at DESC,id DESC LIMIT 200`, t, o)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.Tender
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.OrderID, &v.CashShiftID, &v.TenderType, &v.AmountMinor, &v.Currency, &v.ProviderReference, &v.Status, &v.ReversesTenderID, &v.OccurredAt); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func (r *PostgresRepository) ReverseTender(ctx context.Context, v domain.Tender, a domain.AuditEvent) (domain.Tender, error) {
	err := r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		var original domain.Tender
		var reversed int64
		if err := tx.QueryRow(ctx, `SELECT order_id FROM tenders WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND status='captured'`, v.TenantID, v.OutletID, v.ReversesTenderID).Scan(&original.OrderID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM orders WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, v.TenantID, v.OutletID, original.OrderID).Scan(&original.OrderID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT order_id,tender_type,amount_minor,currency,COALESCE((SELECT SUM(amount_minor) FROM tenders r WHERE r.tenant_id=$1 AND r.reverses_tender_id=$3),0) FROM tenders WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND status='captured'`, v.TenantID, v.OutletID, v.ReversesTenderID).Scan(&original.OrderID, &original.TenderType, &original.AmountMinor, &original.Currency, &reversed); err != nil {
			return err
		}
		if reversed+v.AmountMinor > original.AmountMinor {
			return fmt.Errorf("%w: refund exceeds captured tender", ErrInvalidReference)
		}
		v.OrderID = original.OrderID
		v.TenderType = original.TenderType
		v.Currency = original.Currency
		v.Status = "reversed"
		if v.TenderType == "cash" {
			tag, err := tx.Exec(ctx, `UPDATE cash_shifts SET expected_cash_minor=expected_cash_minor-$4 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND status='open'`, v.TenantID, v.OutletID, v.CashShiftID, v.AmountMinor)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrInvalidReference
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tenders(id,tenant_id,outlet_id,order_id,cash_shift_id,tender_type,amount_minor,currency,provider_reference,status,reverses_tender_id,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'reversed',$10,$11,$12,$13)`, v.ID, v.TenantID, v.OutletID, v.OrderID, nullable(v.CashShiftID), v.TenderType, v.AmountMinor, v.Currency, v.ProviderReference, v.ReversesTenderID, v.OccurredAt, a.ActorID, a.OperationID); err != nil {
			return err
		}
		if v.TenderType == "cash" {
			if _, err := tx.Exec(ctx, `INSERT INTO cash_events(id,tenant_id,cash_shift_id,event_type,amount_minor,reason,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,'cash_refund',$4,'tender reversal',$5,$6,$7)`, inventoryEventUUID(v.TenantID, a.OperationID, "cash-refund"), v.TenantID, v.CashShiftID, -v.AmountMinor, a.RecordedAt, a.ActorID, a.OperationID); err != nil {
				return err
			}
		}
		return insertAudit(ctx, tx, a)
	})
	return v, err
}
func (r *PostgresRepository) GenerateSettlements(ctx context.Context, t, o, date string, a domain.AuditEvent) ([]domain.TenderSettlement, error) {
	values := []domain.TenderSettlement{}
	e := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT tender_type,COALESCE(SUM(amount_minor) FILTER(WHERE status='captured'),0),COALESCE(SUM(amount_minor) FILTER(WHERE status='reversed'),0),COUNT(*) FROM tenders WHERE tenant_id=$1 AND outlet_id=$2 AND occurred_at::date=$3::date GROUP BY tender_type`, t, o, date)
		if e != nil {
			return e
		}
		for rows.Next() {
			var v domain.TenderSettlement
			if e := rows.Scan(&v.TenderType, &v.GrossMinor, &v.ReversedMinor, &v.TransactionCount); e != nil {
				return e
			}
			v.ID = inventoryEventUUID(t, a.OperationID, v.TenderType)
			v.BusinessDate = date
			v.NetMinor = v.GrossMinor - v.ReversedMinor
			v.GeneratedAt = a.RecordedAt
			values = append(values, v)
		}
		if e := rows.Err(); e != nil {
			rows.Close()
			return e
		}
		rows.Close()
		for _, v := range values {
			if _, e = tx.Exec(ctx, `INSERT INTO tender_settlements(id,tenant_id,outlet_id,business_date,tender_type,gross_minor,reversed_minor,net_minor,transaction_count,generated_at,actor_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(tenant_id,outlet_id,business_date,tender_type) DO NOTHING`, v.ID, t, o, date, v.TenderType, v.GrossMinor, v.ReversedMinor, v.NetMinor, v.TransactionCount, v.GeneratedAt, a.ActorID, a.OperationID); e != nil {
				return e
			}
		}
		return insertAudit(ctx, tx, a)
	})
	return values, e
}
