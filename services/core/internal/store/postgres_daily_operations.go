// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"context"
	"fmt"
	"math"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) CreateSupplier(ctx context.Context, v domain.Supplier, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO suppliers(id,tenant_id,name,code,contact_name,phone,email,tax_id,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, v.ID, v.TenantID, v.Name, v.Code, v.ContactName, v.Phone, v.Email, v.TaxID, v.Active, v.Version, v.CreatedAt, v.UpdatedAt)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
}
func (r *PostgresRepository) Suppliers(ctx context.Context, tenant string) ([]domain.Supplier, error) {
	values := []domain.Supplier{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,name,code,contact_name,phone,email,tax_id,active,version,created_at,updated_at FROM suppliers WHERE tenant_id=$1 ORDER BY active DESC,name,id`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.Supplier
			if err := rows.Scan(&v.ID, &v.TenantID, &v.Name, &v.Code, &v.ContactName, &v.Phone, &v.Email, &v.TaxID, &v.Active, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) CreatePurchaseOrder(ctx context.Context, v domain.PurchaseOrder, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		total := int64(0)
		for _, line := range v.Lines {
			total += int64(math.Round(line.OrderedQuantity * float64(line.UnitCostMinor)))
		}
		v.TotalMinor = total
		_, err := tx.Exec(ctx, `INSERT INTO purchase_orders(id,tenant_id,outlet_id,supplier_id,po_number,status,expected_at,currency,notes,total_minor,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'draft',$6,$7,$8,$9,1,$10,$10)`, v.ID, v.TenantID, v.OutletID, v.SupplierID, v.PONumber, v.ExpectedAt, v.Currency, v.Notes, total, v.CreatedAt)
		if err != nil {
			return err
		}
		for _, line := range v.Lines {
			if _, err := tx.Exec(ctx, `INSERT INTO purchase_order_lines(id,tenant_id,purchase_order_id,ingredient_id,unit_id,ordered_quantity,unit_cost_minor) VALUES($1,$2,$3,$4,$5,$6,$7)`, line.ID, v.TenantID, v.ID, line.IngredientID, line.UnitID, line.OrderedQuantity, line.UnitCostMinor); err != nil {
				return err
			}
		}
		return insertAudit(ctx, tx, a)
	})
}
func loadPOLines(ctx context.Context, tx pgx.Tx, v *domain.PurchaseOrder) error {
	rows, err := tx.Query(ctx, `SELECT line.id,line.ingredient_id,ingredient.name,line.unit_id,unit.symbol,line.ordered_quantity,line.received_quantity,line.unit_cost_minor FROM purchase_order_lines line JOIN ingredients ingredient ON ingredient.tenant_id=line.tenant_id AND ingredient.id=line.ingredient_id JOIN units unit ON unit.tenant_id=line.tenant_id AND unit.id=line.unit_id WHERE line.tenant_id=$1 AND line.purchase_order_id=$2 ORDER BY ingredient.name,line.id`, v.TenantID, v.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var line domain.PurchaseOrderLine
		if err := rows.Scan(&line.ID, &line.IngredientID, &line.IngredientName, &line.UnitID, &line.UnitSymbol, &line.OrderedQuantity, &line.ReceivedQuantity, &line.UnitCostMinor); err != nil {
			return err
		}
		v.Lines = append(v.Lines, line)
	}
	return rows.Err()
}
func scanPO(row pgx.Row, v *domain.PurchaseOrder) error {
	return row.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.SupplierID, &v.SupplierName, &v.PONumber, &v.Status, &v.ExpectedAt, &v.Currency, &v.Notes, &v.TotalMinor, &v.Version, &v.CreatedAt, &v.UpdatedAt)
}

const poSelect = `SELECT po.id,po.tenant_id,po.outlet_id,po.supplier_id,supplier.name,po.po_number,po.status,po.expected_at,po.currency,po.notes,po.total_minor,po.version,po.created_at,po.updated_at FROM purchase_orders po JOIN suppliers supplier ON supplier.tenant_id=po.tenant_id AND supplier.id=po.supplier_id`

func (r *PostgresRepository) PurchaseOrders(ctx context.Context, tenant, outlet string) ([]domain.PurchaseOrder, error) {
	values := []domain.PurchaseOrder{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, poSelect+` WHERE po.tenant_id=$1 AND po.outlet_id=$2 ORDER BY po.created_at DESC,po.id`, tenant, outlet)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v domain.PurchaseOrder
			if err := scanPO(rows, &v); err != nil {
				rows.Close()
				return err
			}
			values = append(values, v)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for i := range values {
			if err := loadPOLines(ctx, tx, &values[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return values, err
}
func (r *PostgresRepository) TransitionPurchaseOrder(ctx context.Context, tenant, outlet, id, status string, expected uint64, a domain.AuditEvent) (domain.PurchaseOrder, error) {
	var v domain.PurchaseOrder
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if err := scanPO(tx.QueryRow(ctx, poSelect+` WHERE po.tenant_id=$1 AND po.outlet_id=$2 AND po.id=$3 FOR UPDATE OF po`, tenant, outlet, id), &v); err != nil {
			return err
		}
		if v.Version != expected {
			return ErrVersionConflict
		}
		valid := (status == "submitted" && v.Status == "draft") || (status == "cancelled" && (v.Status == "draft" || v.Status == "submitted"))
		if !valid {
			return ErrInvalidTransition
		}
		tag, err := tx.Exec(ctx, `UPDATE purchase_orders SET status=$4,version=version+1,updated_at=$5 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND version=$6`, tenant, outlet, id, status, a.RecordedAt, expected)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrVersionConflict
		}
		v.Status = status
		v.Version++
		v.UpdatedAt = a.RecordedAt
		if err := loadPOLines(ctx, tx, &v); err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
	return v, err
}
func (r *PostgresRepository) ReceivePurchaseOrder(ctx context.Context, receipt domain.GoodsReceipt, expected uint64, a domain.AuditEvent) (domain.PurchaseOrder, error) {
	var po domain.PurchaseOrder
	err := r.withTenant(ctx, receipt.TenantID, func(tx pgx.Tx) error {
		if err := scanPO(tx.QueryRow(ctx, poSelect+` WHERE po.tenant_id=$1 AND po.outlet_id=$2 AND po.id=$3 FOR UPDATE OF po`, receipt.TenantID, receipt.OutletID, receipt.PurchaseOrderID), &po); err != nil {
			return err
		}
		if po.Version != expected {
			return ErrVersionConflict
		}
		if po.Status != "submitted" && po.Status != "partially_received" {
			return ErrInvalidTransition
		}
		if _, err := tx.Exec(ctx, `INSERT INTO goods_receipts(id,tenant_id,outlet_id,purchase_order_id,received_at,supplier_document,notes,actor_id,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, receipt.ID, receipt.TenantID, receipt.OutletID, receipt.PurchaseOrderID, receipt.ReceivedAt, receipt.SupplierDocument, receipt.Notes, a.ActorID, a.OperationID); err != nil {
			return err
		}
		for _, line := range receipt.Lines {
			var ingredient, unit string
			var ordered, received float64
			var cost int64
			if err := tx.QueryRow(ctx, `SELECT ingredient_id,unit_id,ordered_quantity,received_quantity,unit_cost_minor FROM purchase_order_lines WHERE tenant_id=$1 AND purchase_order_id=$2 AND id=$3 FOR UPDATE`, receipt.TenantID, receipt.PurchaseOrderID, line.PurchaseOrderLineID).Scan(&ingredient, &unit, &ordered, &received, &cost); err != nil {
				return err
			}
			if line.IngredientID != ingredient || line.UnitID != unit || received+line.Quantity > ordered {
				return fmt.Errorf("%w: receipt line", ErrInvalidReference)
			}
			var numerator, denominator int64
			if err := tx.QueryRow(ctx, `SELECT unit.base_numerator,unit.base_denominator FROM units unit JOIN ingredients ingredient ON ingredient.tenant_id=unit.tenant_id WHERE unit.tenant_id=$1 AND unit.id=$2 AND ingredient.id=$3 AND ingredient.base_unit_id IN (SELECT id FROM units WHERE tenant_id=$1 AND dimension=unit.dimension)`, receipt.TenantID, unit, ingredient).Scan(&numerator, &denominator); err != nil {
				return err
			}
			base := line.Quantity * float64(numerator) / float64(denominator)
			line.InventoryEventID = inventoryEventUUID(receipt.TenantID, a.OperationID, ingredient)
			total := int64(math.Round(line.Quantity * float64(cost)))
			if _, err := tx.Exec(ctx, `INSERT INTO inventory_events(id,tenant_id,outlet_id,ingredient_id,event_type,quantity_base,total_cost_minor,currency,reference_type,reference_id,lot_code,expires_at,reason,occurred_at,recorded_at,actor_id,device_id,operation_id) VALUES($1,$2,$3,$4,'receipt',$5,$6,$7,'goods_receipt',$8,$9,$10,'purchase order receipt',$11,$12,$13,$14,$15)`, line.InventoryEventID, receipt.TenantID, receipt.OutletID, ingredient, base, total, po.Currency, receipt.ID, line.LotCode, line.ExpiresAt, receipt.ReceivedAt, a.RecordedAt, a.ActorID, a.DeviceID, a.OperationID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE purchase_order_lines SET received_quantity=received_quantity+$4 WHERE tenant_id=$1 AND purchase_order_id=$2 AND id=$3`, receipt.TenantID, receipt.PurchaseOrderID, line.PurchaseOrderLineID, line.Quantity); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO goods_receipt_lines(id,tenant_id,goods_receipt_id,purchase_order_line_id,ingredient_id,unit_id,quantity,unit_cost_minor,lot_code,expires_at,inventory_event_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, line.ID, receipt.TenantID, receipt.ID, line.PurchaseOrderLineID, ingredient, unit, line.Quantity, cost, line.LotCode, line.ExpiresAt, line.InventoryEventID); err != nil {
				return err
			}
		}
		var remaining bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM purchase_order_lines WHERE tenant_id=$1 AND purchase_order_id=$2 AND received_quantity<ordered_quantity)`, receipt.TenantID, receipt.PurchaseOrderID).Scan(&remaining); err != nil {
			return err
		}
		status := "received"
		if remaining {
			status = "partially_received"
		}
		if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET status=$4,version=version+1,updated_at=$5 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, receipt.TenantID, receipt.OutletID, receipt.PurchaseOrderID, status, a.RecordedAt); err != nil {
			return err
		}
		po.Status = status
		po.Version++
		po.UpdatedAt = a.RecordedAt
		if err := loadPOLines(ctx, tx, &po); err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
	return po, err
}

func (r *PostgresRepository) RecordTemperature(ctx context.Context, v domain.TemperatureLog, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO temperature_logs(id,tenant_id,outlet_id,location,temperature_c,safe_min_c,safe_max_c,compliant,corrective_action,measured_at,actor_id,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, v.ID, v.TenantID, v.OutletID, v.Location, v.TemperatureC, v.SafeMinC, v.SafeMaxC, v.Compliant, v.CorrectiveAction, v.MeasuredAt, a.ActorID, a.OperationID)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
}
func (r *PostgresRepository) TemperatureLogs(ctx context.Context, tenant, outlet string) ([]domain.TemperatureLog, error) {
	values := []domain.TemperatureLog{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,location,temperature_c,safe_min_c,safe_max_c,compliant,corrective_action,measured_at,actor_id FROM temperature_logs WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY measured_at DESC,id LIMIT 100`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.TemperatureLog
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.Location, &v.TemperatureC, &v.SafeMinC, &v.SafeMaxC, &v.Compliant, &v.CorrectiveAction, &v.MeasuredAt, &v.ActorID); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) CreateChecklist(ctx context.Context, v domain.OperationalChecklist, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO operational_checklists(id,tenant_id,outlet_id,checklist_type,business_date,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'open',1,$6,$6)`, v.ID, v.TenantID, v.OutletID, v.ChecklistType, v.BusinessDate, v.CreatedAt)
		if err != nil {
			return err
		}
		for _, item := range v.Items {
			if _, err := tx.Exec(ctx, `INSERT INTO operational_checklist_items(id,tenant_id,checklist_id,label,required,position) VALUES($1,$2,$3,$4,$5,$6)`, item.ID, v.TenantID, v.ID, item.Label, item.Required, item.Position); err != nil {
				return err
			}
		}
		return insertAudit(ctx, tx, a)
	})
}
func loadChecklistItems(ctx context.Context, tx pgx.Tx, v *domain.OperationalChecklist) error {
	rows, err := tx.Query(ctx, `SELECT id,label,required,completed,completed_by,completed_at,position FROM operational_checklist_items WHERE tenant_id=$1 AND checklist_id=$2 ORDER BY position,id`, v.TenantID, v.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var i domain.ChecklistItem
		if err := rows.Scan(&i.ID, &i.Label, &i.Required, &i.Completed, &i.CompletedBy, &i.CompletedAt, &i.Position); err != nil {
			return err
		}
		v.Items = append(v.Items, i)
	}
	return rows.Err()
}
func (r *PostgresRepository) Checklists(ctx context.Context, tenant, outlet string) ([]domain.OperationalChecklist, error) {
	values := []domain.OperationalChecklist{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,checklist_type,business_date::text,status,version,created_at,updated_at,completed_at FROM operational_checklists WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY business_date DESC,created_at DESC LIMIT 50`, tenant, outlet)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v domain.OperationalChecklist
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.ChecklistType, &v.BusinessDate, &v.Status, &v.Version, &v.CreatedAt, &v.UpdatedAt, &v.CompletedAt); err != nil {
				rows.Close()
				return err
			}
			values = append(values, v)
		}
		rows.Close()
		for i := range values {
			if err := loadChecklistItems(ctx, tx, &values[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return values, err
}
func (r *PostgresRepository) CompleteChecklistItem(ctx context.Context, tenant, outlet, id, itemID string, expected uint64, a domain.AuditEvent) (domain.OperationalChecklist, error) {
	var v domain.OperationalChecklist
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,checklist_type,business_date::text,status,version,created_at,updated_at,completed_at FROM operational_checklists WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, tenant, outlet, id).Scan(&v.ID, &v.TenantID, &v.OutletID, &v.ChecklistType, &v.BusinessDate, &v.Status, &v.Version, &v.CreatedAt, &v.UpdatedAt, &v.CompletedAt); err != nil {
			return err
		}
		if v.Version != expected {
			return ErrVersionConflict
		}
		if v.Status != "open" {
			return ErrInvalidTransition
		}
		tag, err := tx.Exec(ctx, `UPDATE operational_checklist_items SET completed=true,completed_by=$4,completed_at=$5 WHERE tenant_id=$1 AND checklist_id=$2 AND id=$3 AND completed=false`, tenant, id, itemID, a.ActorID, a.RecordedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrInvalidTransition
		}
		var remaining bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operational_checklist_items WHERE tenant_id=$1 AND checklist_id=$2 AND required AND NOT completed)`, tenant, id).Scan(&remaining); err != nil {
			return err
		}
		status := "open"
		var completed any = nil
		if !remaining {
			status = "completed"
			completed = a.RecordedAt
		}
		if _, err := tx.Exec(ctx, `UPDATE operational_checklists SET status=$4,completed_at=$5,version=version+1,updated_at=$6 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, tenant, outlet, id, status, completed, a.RecordedAt); err != nil {
			return err
		}
		v.Status = status
		v.Version++
		v.UpdatedAt = a.RecordedAt
		if !remaining {
			now := a.RecordedAt
			v.CompletedAt = &now
		}
		if err := loadChecklistItems(ctx, tx, &v); err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
	return v, err
}

func (r *PostgresRepository) CreateStaffMember(ctx context.Context, v domain.StaffMember, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO staff_members(id,tenant_id,employee_code,display_name,role,phone,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,true,1,$7,$7)`, v.ID, v.TenantID, v.EmployeeCode, v.DisplayName, v.Role, v.Phone, v.CreatedAt)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
}
func (r *PostgresRepository) StaffMembers(ctx context.Context, tenant string) ([]domain.StaffMember, error) {
	values := []domain.StaffMember{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,employee_code,display_name,role,phone,active,version,created_at,updated_at FROM staff_members WHERE tenant_id=$1 ORDER BY active DESC,display_name,id`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.StaffMember
			if err := rows.Scan(&v.ID, &v.TenantID, &v.EmployeeCode, &v.DisplayName, &v.Role, &v.Phone, &v.Active, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func (r *PostgresRepository) CreateShift(ctx context.Context, v domain.StaffShift, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO staff_shifts(id,tenant_id,outlet_id,staff_member_id,starts_at,ends_at,station_id,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,'scheduled',1,$8,$8)`, v.ID, v.TenantID, v.OutletID, v.StaffMemberID, v.StartsAt, v.EndsAt, nullable(v.StationID), v.CreatedAt)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
}
func scanShift(row pgx.Row, v *domain.StaffShift) error {
	return row.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.StaffMemberID, &v.StaffName, &v.StartsAt, &v.EndsAt, &v.StationID, &v.Status, &v.Version, &v.CreatedAt, &v.UpdatedAt)
}

const shiftSelect = `SELECT shift.id,shift.tenant_id,shift.outlet_id,shift.staff_member_id,staff.display_name,shift.starts_at,shift.ends_at,COALESCE(shift.station_id::text,''),shift.status,shift.version,shift.created_at,shift.updated_at FROM staff_shifts shift JOIN staff_members staff ON staff.tenant_id=shift.tenant_id AND staff.id=shift.staff_member_id`

func (r *PostgresRepository) Shifts(ctx context.Context, tenant, outlet string) ([]domain.StaffShift, error) {
	values := []domain.StaffShift{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, shiftSelect+` WHERE shift.tenant_id=$1 AND shift.outlet_id=$2 ORDER BY shift.starts_at DESC,shift.id LIMIT 100`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.StaffShift
			if err := scanShift(rows, &v); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func (r *PostgresRepository) TransitionShift(ctx context.Context, tenant, outlet, id, status string, expected uint64, a domain.AuditEvent) (domain.StaffShift, error) {
	var v domain.StaffShift
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if err := scanShift(tx.QueryRow(ctx, shiftSelect+` WHERE shift.tenant_id=$1 AND shift.outlet_id=$2 AND shift.id=$3 FOR UPDATE OF shift`, tenant, outlet, id), &v); err != nil {
			return err
		}
		if v.Version != expected {
			return ErrVersionConflict
		}
		valid := (status == "checked_in" && v.Status == "scheduled") || (status == "completed" && v.Status == "checked_in") || (status == "cancelled" && v.Status == "scheduled")
		if !valid {
			return ErrInvalidTransition
		}
		if _, err := tx.Exec(ctx, `UPDATE staff_shifts SET status=$4,version=version+1,updated_at=$5 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, tenant, outlet, id, status, a.RecordedAt); err != nil {
			return err
		}
		v.Status = status
		v.Version++
		v.UpdatedAt = a.RecordedAt
		return insertAudit(ctx, tx, a)
	})
	return v, err
}
func (r *PostgresRepository) CreateTask(ctx context.Context, v domain.OperationalTask, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO operational_tasks(id,tenant_id,outlet_id,staff_member_id,title,due_at,priority,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,'open',1,$8,$8)`, v.ID, v.TenantID, v.OutletID, nullable(v.StaffMemberID), v.Title, v.DueAt, v.Priority, v.CreatedAt)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
}
func scanTask(row pgx.Row, v *domain.OperationalTask) error {
	return row.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.StaffMemberID, &v.StaffName, &v.Title, &v.DueAt, &v.Priority, &v.Status, &v.Version, &v.CreatedAt, &v.UpdatedAt, &v.CompletedAt)
}

const taskSelect = `SELECT task.id,task.tenant_id,task.outlet_id,COALESCE(task.staff_member_id::text,''),COALESCE(staff.display_name,''),task.title,task.due_at,task.priority,task.status,task.version,task.created_at,task.updated_at,task.completed_at FROM operational_tasks task LEFT JOIN staff_members staff ON staff.tenant_id=task.tenant_id AND staff.id=task.staff_member_id`

func (r *PostgresRepository) Tasks(ctx context.Context, tenant, outlet string) ([]domain.OperationalTask, error) {
	values := []domain.OperationalTask{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, taskSelect+` WHERE task.tenant_id=$1 AND task.outlet_id=$2 ORDER BY CASE task.status WHEN 'open' THEN 0 ELSE 1 END,task.due_at NULLS LAST,task.id LIMIT 100`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.OperationalTask
			if err := scanTask(rows, &v); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func (r *PostgresRepository) TransitionTask(ctx context.Context, tenant, outlet, id, status string, expected uint64, a domain.AuditEvent) (domain.OperationalTask, error) {
	var v domain.OperationalTask
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if err := scanTask(tx.QueryRow(ctx, taskSelect+` WHERE task.tenant_id=$1 AND task.outlet_id=$2 AND task.id=$3 FOR UPDATE OF task`, tenant, outlet, id), &v); err != nil {
			return err
		}
		if v.Version != expected {
			return ErrVersionConflict
		}
		if v.Status != "open" || (status != "completed" && status != "cancelled") {
			return ErrInvalidTransition
		}
		var completed any = nil
		if status == "completed" {
			completed = a.RecordedAt
		}
		if _, err := tx.Exec(ctx, `UPDATE operational_tasks SET status=$4,completed_at=$5,version=version+1,updated_at=$6 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, tenant, outlet, id, status, completed, a.RecordedAt); err != nil {
			return err
		}
		v.Status = status
		v.Version++
		v.UpdatedAt = a.RecordedAt
		if status == "completed" {
			now := a.RecordedAt
			v.CompletedAt = &now
		}
		return insertAudit(ctx, tx, a)
	})
	return v, err
}
