// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func transferDerivedUUID(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, ":")))
	hash[6] = (hash[6] & 0x0f) | 0x40
	hash[8] = (hash[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", hash[0:4], hash[4:6], hash[6:8], hash[8:10], hash[10:16])
}

func transferActionAllowed(status, action, outlet string, value domain.StockTransfer) bool {
	switch action {
	case "approved", "dispatched":
		return outlet == value.SourceOutletID && ((action == "approved" && status == "requested") || (action == "dispatched" && status == "approved"))
	case "received":
		return outlet == value.DestinationOutletID && status == "dispatched"
	case "cancelled":
		return outlet == value.SourceOutletID && (status == "requested" || status == "approved")
	default:
		return false
	}
}

func transferLinesByIngredient(ctx context.Context, tx pgx.Tx, tenant, transferID string) (map[string]domain.StockTransferLine, error) {
	rows, err := tx.Query(ctx, `SELECT id,ingredient_id,quantity_base,dispatched_quantity_base,received_quantity_base FROM stock_transfer_lines WHERE tenant_id=$1 AND transfer_id=$2 ORDER BY id FOR UPDATE`, tenant, transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string]domain.StockTransferLine{}
	for rows.Next() {
		var line domain.StockTransferLine
		if err := rows.Scan(&line.ID, &line.IngredientID, &line.QuantityBase, &line.DispatchedQuantityBase, &line.ReceivedQuantityBase); err != nil {
			return nil, err
		}
		values[line.IngredientID] = line
	}
	return values, rows.Err()
}

func normalizedTransferExecution(lines []domain.StockTransferExecutionLine, expected map[string]domain.StockTransferLine, ceiling func(domain.StockTransferLine) float64) (map[string]float64, error) {
	if len(lines) != len(expected) {
		return nil, fmt.Errorf("%w: every transfer line needs an actual quantity", ErrInvalidReference)
	}
	values := make(map[string]float64, len(lines))
	for _, line := range lines {
		original, exists := expected[line.IngredientID]
		if !exists || line.QuantityBase <= 0 || line.QuantityBase > ceiling(original) || values[line.IngredientID] != 0 {
			return nil, fmt.Errorf("%w: transfer actual quantity is invalid", ErrInvalidReference)
		}
		values[line.IngredientID] = line.QuantityBase
	}
	return values, nil
}

func lockTransferIngredient(ctx context.Context, tx pgx.Tx, tenant, outlet, ingredient string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+":"+outlet+":"+ingredient)
	return err
}

func insertTransferLedgerEvent(ctx context.Context, tx pgx.Tx, event domain.InventoryEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO inventory_events(id,tenant_id,outlet_id,ingredient_id,event_type,quantity_base,total_cost_minor,currency,reference_type,reference_id,lot_code,expires_at,reason,occurred_at,recorded_at,actor_id,device_id,operation_id,reverses_event_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'',NULL,$11,$12,$13,$14,$15,$16,NULL)`, event.ID, event.TenantID, event.OutletID, event.IngredientID, event.EventType, event.QuantityBase, event.TotalCostMinor, event.Currency, event.ReferenceType, event.ReferenceID, event.Reason, event.OccurredAt, event.RecordedAt, event.ActorID, event.DeviceID, event.OperationID)
	return err
}

func dispatchTransferInventory(ctx context.Context, tx pgx.Tx, transfer domain.StockTransfer, quantities map[string]float64, audit domain.AuditEvent, now time.Time) error {
	for ingredientID, quantity := range quantities {
		if err := lockTransferIngredient(ctx, tx, transfer.TenantID, transfer.SourceOutletID, ingredientID); err != nil {
			return err
		}
		var balance float64
		var cost int64
		var currency string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(quantity_base),0),COALESCE(SUM(total_cost_minor),0),COALESCE((SELECT currency FROM inventory_events WHERE tenant_id=$1 AND outlet_id=$2 AND ingredient_id=$3 ORDER BY occurred_at DESC,id DESC LIMIT 1),'INR') FROM inventory_events WHERE tenant_id=$1 AND outlet_id=$2 AND ingredient_id=$3`, transfer.TenantID, transfer.SourceOutletID, ingredientID).Scan(&balance, &cost, &currency); err != nil {
			return err
		}
		if balance < quantity {
			return fmt.Errorf("%w: source stock is insufficient", ErrInvalidReference)
		}
		movedCost := int64(math.Round(float64(cost) * quantity / balance))
		event := domain.InventoryEvent{ID: transferDerivedUUID(audit.OperationID, transfer.ID, ingredientID, "transfer_out"), TenantID: transfer.TenantID, OutletID: transfer.SourceOutletID, IngredientID: ingredientID, EventType: "transfer_out", QuantityBase: -quantity, TotalCostMinor: -movedCost, Currency: currency, ReferenceType: "stock_transfer", ReferenceID: transfer.ID, Reason: "Dispatched to outlet " + transfer.DestinationOutletID, OccurredAt: now, RecordedAt: now, ActorID: audit.ActorID, DeviceID: audit.DeviceID, OperationID: audit.OperationID}
		if err := insertTransferLedgerEvent(ctx, tx, event); err != nil {
			return err
		}
	}
	return nil
}

func receiveTransferInventory(ctx context.Context, tx pgx.Tx, transfer domain.StockTransfer, quantities map[string]float64, dispatched map[string]domain.StockTransferLine, audit domain.AuditEvent, now time.Time) error {
	for ingredientID, received := range quantities {
		if err := lockTransferIngredient(ctx, tx, transfer.TenantID, transfer.DestinationOutletID, ingredientID); err != nil {
			return err
		}
		var shippedQuantity float64
		var shippedCost int64
		var currency string
		if err := tx.QueryRow(ctx, `SELECT -quantity_base,-total_cost_minor,currency FROM inventory_events WHERE tenant_id=$1 AND outlet_id=$2 AND ingredient_id=$3 AND event_type='transfer_out' AND reference_type='stock_transfer' AND reference_id=$4 ORDER BY occurred_at DESC,id DESC LIMIT 1`, transfer.TenantID, transfer.SourceOutletID, ingredientID, transfer.ID).Scan(&shippedQuantity, &shippedCost, &currency); err != nil {
			return err
		}
		if shippedQuantity <= 0 || dispatched[ingredientID].DispatchedQuantityBase == nil || received > shippedQuantity {
			return fmt.Errorf("%w: received quantity exceeds dispatched quantity", ErrInvalidReference)
		}
		inbound := domain.InventoryEvent{ID: transferDerivedUUID(audit.OperationID, transfer.ID, ingredientID, "transfer_in"), TenantID: transfer.TenantID, OutletID: transfer.DestinationOutletID, IngredientID: ingredientID, EventType: "transfer_in", QuantityBase: shippedQuantity, TotalCostMinor: shippedCost, Currency: currency, ReferenceType: "stock_transfer", ReferenceID: transfer.ID, Reason: "Received from outlet " + transfer.SourceOutletID, OccurredAt: now, RecordedAt: now, ActorID: audit.ActorID, DeviceID: audit.DeviceID, OperationID: audit.OperationID}
		if err := insertTransferLedgerEvent(ctx, tx, inbound); err != nil {
			return err
		}
		if variance := shippedQuantity - received; variance > 0 {
			varianceCost := int64(math.Round(float64(shippedCost) * variance / shippedQuantity))
			loss := domain.InventoryEvent{ID: transferDerivedUUID(audit.OperationID, transfer.ID, ingredientID, "in_transit_variance"), TenantID: transfer.TenantID, OutletID: transfer.DestinationOutletID, IngredientID: ingredientID, EventType: "spoilage", QuantityBase: -variance, TotalCostMinor: -varianceCost, Currency: currency, ReferenceType: "stock_transfer", ReferenceID: transfer.ID, Reason: "In-transit transfer variance", OccurredAt: now, RecordedAt: now, ActorID: audit.ActorID, DeviceID: audit.DeviceID, OperationID: transferDerivedUUID(audit.OperationID, transfer.ID, ingredientID, "in_transit_variance_operation")}
			if err := insertTransferLedgerEvent(ctx, tx, loss); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *PostgresRepository) TransitionStockTransfer(ctx context.Context, tenant, outlet, transferID, action string, expectedVersion uint64, execution []domain.StockTransferExecutionLine, audit domain.AuditEvent) (domain.StockTransfer, error) {
	var value domain.StockTransfer
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,source_outlet_id,destination_outlet_id,status,requested_by,notes,requested_at,dispatched_at,received_at,version FROM stock_transfers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, transferID).Scan(&value.ID, &value.TenantID, &value.SourceOutletID, &value.DestinationOutletID, &value.Status, &value.RequestedBy, &value.Notes, &value.RequestedAt, &value.DispatchedAt, &value.ReceivedAt, &value.Version); err != nil {
			return err
		}
		if value.Version != expectedVersion {
			return ErrVersionConflict
		}
		if !transferActionAllowed(value.Status, action, outlet, value) {
			return ErrInvalidTransition
		}
		lines, err := transferLinesByIngredient(ctx, tx, tenant, transferID)
		if err != nil {
			return err
		}
		var actual map[string]float64
		if action == "dispatched" {
			actual, err = normalizedTransferExecution(execution, lines, func(line domain.StockTransferLine) float64 { return line.QuantityBase })
			if err != nil {
				return err
			}
		}
		if action == "received" {
			actual, err = normalizedTransferExecution(execution, lines, func(line domain.StockTransferLine) float64 {
				if line.DispatchedQuantityBase == nil {
					return 0
				}
				return *line.DispatchedQuantityBase
			})
			if err != nil {
				return err
			}
		}
		now := audit.RecordedAt.UTC()
		if action == "dispatched" {
			if err := dispatchTransferInventory(ctx, tx, value, actual, audit, now); err != nil {
				return err
			}
			for ingredientID, quantity := range actual {
				if _, err := tx.Exec(ctx, `UPDATE stock_transfer_lines SET dispatched_quantity_base=$3 WHERE tenant_id=$1 AND transfer_id=$2 AND ingredient_id=$4`, tenant, transferID, quantity, ingredientID); err != nil {
					return err
				}
			}
		}
		if action == "received" {
			if err := receiveTransferInventory(ctx, tx, value, actual, lines, audit, now); err != nil {
				return err
			}
			for ingredientID, quantity := range actual {
				if _, err := tx.Exec(ctx, `UPDATE stock_transfer_lines SET received_quantity_base=$3 WHERE tenant_id=$1 AND transfer_id=$2 AND ingredient_id=$4`, tenant, transferID, quantity, ingredientID); err != nil {
					return err
				}
			}
		}
		status := action
		if status == "approved" {
			status = "approved"
		}
		if status == "cancelled" {
			status = "cancelled"
		}
		if status == "dispatched" {
			value.DispatchedAt = &now
		}
		if status == "received" {
			value.ReceivedAt = &now
		}
		if _, err := tx.Exec(ctx, `UPDATE stock_transfers SET status=$3,dispatched_at=$4,received_at=$5,version=version+1 WHERE tenant_id=$1 AND id=$2`, tenant, transferID, status, value.DispatchedAt, value.ReceivedAt); err != nil {
			return err
		}
		details, err := marshalConnected(map[string]any{"fromStatus": value.Status, "toStatus": status, "lines": execution})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO stock_transfer_events(id,tenant_id,transfer_id,outlet_id,event_type,details,occurred_at,actor_id,device_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, transferDerivedUUID(audit.OperationID, transferID, action, "event"), tenant, transferID, outlet, action, details, now, audit.ActorID, audit.DeviceID, audit.OperationID); err != nil {
			return err
		}
		value.Status = status
		value.Version++
		value.Lines = nil
		if err := loadTransferLines(ctx, tx, &value); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

var _ ConnectedCommerceRepository = (*PostgresRepository)(nil)
