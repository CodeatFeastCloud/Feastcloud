// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func marshalConnected(value any) ([]byte, error) { return json.Marshal(value) }

func unmarshalConnected(raw []byte, target any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	return json.Unmarshal(raw, target)
}

const salesChannelSelect = `SELECT id,tenant_id,outlet_id,code,name,channel_type,active,configuration,version,created_at,updated_at FROM sales_channels`

func scanSalesChannel(row pgx.Row, value *domain.SalesChannel) error {
	var raw []byte
	if err := row.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.Code, &value.Name, &value.Type, &value.Active, &raw, &value.Version, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return err
	}
	return unmarshalConnected(raw, &value.Configuration)
}

func (r *PostgresRepository) CreateSalesChannel(ctx context.Context, value domain.SalesChannel, audit domain.AuditEvent) error {
	return r.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		raw, err := marshalConnected(value.Configuration)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO sales_channels(id,tenant_id,outlet_id,code,name,channel_type,active,configuration,version,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, value.ID, value.TenantID, value.OutletID, value.Code, value.Name, value.Type, value.Active, raw, value.Version, value.CreatedAt, value.UpdatedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (r *PostgresRepository) SalesChannels(ctx context.Context, tenant, outlet string) ([]domain.SalesChannel, error) {
	values := []domain.SalesChannel{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, salesChannelSelect+` WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY name,id`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.SalesChannel
			if err := scanSalesChannel(rows, &value); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

const connectorSelect = `SELECT id,tenant_id,outlet_id,COALESCE(channel_id::text,''),provider,manifest_version,credential_reference,capabilities,configuration,status,last_health_at,version,created_at,updated_at FROM connector_installations`

func scanConnector(row pgx.Row, value *domain.ConnectorInstallation) error {
	var capabilities, configuration []byte
	if err := row.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.ChannelID, &value.Provider, &value.ManifestVersion, &value.CredentialReference, &capabilities, &configuration, &value.Status, &value.LastHealthAt, &value.Version, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return err
	}
	if err := unmarshalConnected(capabilities, &value.Capabilities); err != nil {
		return err
	}
	return unmarshalConnected(configuration, &value.Configuration)
}

func (r *PostgresRepository) CreateConnectorInstallation(ctx context.Context, value domain.ConnectorInstallation, audit domain.AuditEvent) error {
	return r.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		raw, err := marshalConnected(value.Capabilities)
		if err != nil {
			return err
		}
		configuration, err := marshalConnected(value.Configuration)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO connector_installations(id,tenant_id,outlet_id,channel_id,provider,manifest_version,credential_reference,capabilities,configuration,status,last_health_at,version,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, value.ID, value.TenantID, value.OutletID, nullable(value.ChannelID), value.Provider, value.ManifestVersion, value.CredentialReference, raw, configuration, value.Status, value.LastHealthAt, value.Version, value.CreatedAt, value.UpdatedAt)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (r *PostgresRepository) ConnectorInstallations(ctx context.Context, tenant, outlet string) ([]domain.ConnectorInstallation, error) {
	values := []domain.ConnectorInstallation{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, connectorSelect+` WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY provider,id`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.ConnectorInstallation
			if err := scanConnector(rows, &value); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) IngestConnectorOrder(ctx context.Context, value domain.ConnectorOrderInbox, audit domain.AuditEvent) error {
	return r.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		raw, err := marshalConnected(value.Payload)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(raw)
		if _, err = tx.Exec(ctx, `INSERT INTO connector_order_inbox(id,tenant_id,outlet_id,connector_id,external_order_id,payload,payload_sha256,status,normalized_order_id,received_at,resolved_at,error_code,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, value.ID, value.TenantID, value.OutletID, value.ConnectorID, value.ExternalOrderID, raw, hash[:], value.Status, nullable(value.NormalizedOrderID), value.ReceivedAt, value.ResolvedAt, value.ErrorCode, audit.OperationID); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (r *PostgresRepository) ConnectorOrderInbox(ctx context.Context, tenant, outlet string) ([]domain.ConnectorOrderInbox, error) {
	values := []domain.ConnectorOrderInbox{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT inbox.id,inbox.tenant_id,inbox.outlet_id,inbox.connector_id,inbox.external_order_id,inbox.payload,inbox.payload_sha256,COALESCE(decision.decision,inbox.status),COALESCE(decision.normalized_order_id::text,inbox.normalized_order_id::text,''),inbox.received_at,COALESCE(decision.occurred_at,inbox.resolved_at),COALESCE(NULLIF(decision.reason,''),inbox.error_code) FROM connector_order_inbox inbox LEFT JOIN LATERAL (SELECT decision,normalized_order_id,reason,occurred_at FROM connector_order_decisions WHERE tenant_id=inbox.tenant_id AND inbox_id=inbox.id ORDER BY occurred_at DESC,id DESC LIMIT 1) decision ON true WHERE inbox.tenant_id=$1 AND inbox.outlet_id=$2 ORDER BY inbox.received_at DESC,inbox.id DESC LIMIT 200`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.ConnectorOrderInbox
			var raw, hash []byte
			if err := rows.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.ConnectorID, &value.ExternalOrderID, &raw, &hash, &value.Status, &value.NormalizedOrderID, &value.ReceivedAt, &value.ResolvedAt, &value.ErrorCode); err != nil {
				return err
			}
			if err := unmarshalConnected(raw, &value.Payload); err != nil {
				return err
			}
			value.PayloadSHA256 = hex.EncodeToString(hash)
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) DecideConnectorOrder(ctx context.Context, tenant, outlet string, decision domain.ConnectorOrderDecision, audit domain.AuditEvent) (domain.ConnectorOrderInbox, error) {
	var value domain.ConnectorOrderInbox
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+":"+decision.InboxID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,connector_id,external_order_id,payload,payload_sha256,status,COALESCE(normalized_order_id::text,''),received_at,resolved_at,error_code FROM connector_order_inbox WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, tenant, outlet, decision.InboxID).Scan(&value.ID, &value.TenantID, &value.OutletID, &value.ConnectorID, &value.ExternalOrderID, new([]byte), new([]byte), &value.Status, &value.NormalizedOrderID, &value.ReceivedAt, &value.ResolvedAt, &value.ErrorCode); err != nil {
			return err
		}
		var current string
		if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT decision FROM connector_order_decisions WHERE tenant_id=$1 AND inbox_id=$2 ORDER BY occurred_at DESC,id DESC LIMIT 1),$3)`, tenant, decision.InboxID, value.Status).Scan(&current); err != nil {
			return err
		}
		if (current == "accepted" || current == "rejected" || current == "duplicate") && current != "needs_review" {
			return ErrInvalidTransition
		}
		if decision.Decision == "accepted" {
			var validOrder bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3)`, tenant, outlet, decision.NormalizedOrderID).Scan(&validOrder); err != nil {
				return err
			}
			if !validOrder {
				return fmt.Errorf("%w: normalized order must belong to inbox outlet", ErrInvalidReference)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO connector_order_decisions(id,tenant_id,outlet_id,inbox_id,decision,reason,normalized_order_id,occurred_at,actor_id,device_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, decision.ID, tenant, outlet, decision.InboxID, decision.Decision, decision.Reason, nullable(decision.NormalizedOrderID), decision.OccurredAt, decision.ActorID, decision.DeviceID, audit.OperationID); err != nil {
			return err
		}
		value.Status, value.ErrorCode = decision.Decision, decision.Reason
		value.NormalizedOrderID = decision.NormalizedOrderID
		value.ResolvedAt = &decision.OccurredAt
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func (r *PostgresRepository) SetStationCapacity(ctx context.Context, tenant, outlet string, value domain.StationCapacityLimit, audit domain.AuditEvent) (domain.StationCapacityLimit, error) {
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO station_capacity_limits(tenant_id,outlet_id,station_id,max_active_tickets,updated_at)VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,outlet_id,station_id) DO UPDATE SET max_active_tickets=EXCLUDED.max_active_tickets,updated_at=EXCLUDED.updated_at,version=station_capacity_limits.version+1`, tenant, outlet, value.StationID, value.MaxActiveTickets, audit.RecordedAt)
		if err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT version,updated_at FROM station_capacity_limits WHERE tenant_id=$1 AND outlet_id=$2 AND station_id=$3`, tenant, outlet, value.StationID).Scan(&value.Version, &value.UpdatedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func (r *PostgresRepository) StationCapacityLimits(ctx context.Context, tenant, outlet string) ([]domain.StationCapacityLimit, error) {
	values := []domain.StationCapacityLimit{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT station_id,max_active_tickets,version,updated_at FROM station_capacity_limits WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY station_id`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.StationCapacityLimit
			if err := rows.Scan(&value.StationID, &value.MaxActiveTickets, &value.Version, &value.UpdatedAt); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) MenuSellability(ctx context.Context, tenant, outlet, channelID string) ([]domain.MenuSellability, error) {
	values := []domain.MenuSellability{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		type sellabilitySeed struct {
			value               domain.MenuSellability
			recipeID, stationID string
			mode                string
		}
		seeds := []sellabilitySeed{}
		rows, err := tx.Query(ctx, `SELECT item.id,item.name,item.recipe_id,item.price_minor,item.currency,COALESCE(item.station_id::text,''),COALESCE(manual.available,true),COALESCE(manual.reason,''),COALESCE(channel.price_minor,item.price_minor),COALESCE(channel.availability_mode,'inherit') FROM menu_items item LEFT JOIN menu_item_availability manual ON manual.tenant_id=item.tenant_id AND manual.outlet_id=item.outlet_id AND manual.menu_item_id=item.id LEFT JOIN channel_menu_items channel ON channel.tenant_id=item.tenant_id AND channel.menu_item_id=item.id AND channel.channel_id=NULLIF($3,'')::uuid WHERE item.tenant_id=$1 AND item.outlet_id=$2 AND item.active=true AND (NULLIF($3,'') IS NULL OR COALESCE(channel.enabled,true)) ORDER BY item.name,item.id`, tenant, outlet, channelID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var value domain.MenuSellability
			var recipeID, stationID, mode string
			if err := rows.Scan(&value.MenuItemID, &value.MenuItemName, &recipeID, &value.PriceMinor, &value.Currency, &stationID, &value.ManualAvailable, &value.Reason, &value.PriceMinor, &mode); err != nil {
				rows.Close()
				return err
			}
			value.ChannelID = channelID
			seeds = append(seeds, sellabilitySeed{value: value, recipeID: recipeID, stationID: stationID, mode: mode})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, seed := range seeds {
			value, recipeID, stationID, mode := seed.value, seed.recipeID, seed.stationID, seed.mode
			if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM recipe_versions version JOIN recipe_components component ON component.tenant_id=version.tenant_id AND component.recipe_version_id=version.id JOIN units unit ON unit.tenant_id=component.tenant_id AND unit.id=component.unit_id LEFT JOIN LATERAL (SELECT COALESCE(SUM(event.quantity_base),0) AS balance FROM inventory_events event WHERE event.tenant_id=$1 AND event.outlet_id=$2 AND event.ingredient_id=component.ingredient_id) stock ON true WHERE version.tenant_id=$1 AND version.recipe_id=$3 AND version.effective_from<=clock_timestamp() AND (version.effective_to IS NULL OR version.effective_to>clock_timestamp()) AND component.ingredient_id IS NOT NULL AND stock.balance < component.quantity * unit.base_numerator::numeric / unit.base_denominator::numeric)`, tenant, outlet, recipeID).Scan(&value.StockReady); err != nil {
				return err
			}
			value.CapacityReady = true
			if stationID != "" {
				if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT max_active_tickets FROM station_capacity_limits WHERE tenant_id=$1 AND outlet_id=$2 AND station_id=$3),0),(SELECT COUNT(*) FROM kitchen_tickets WHERE tenant_id=$1 AND outlet_id=$2 AND station_id=$3 AND status IN('queued','fired','preparing'))`, tenant, outlet, stationID).Scan(&value.CapacityLimit, &value.ActiveTickets); err != nil {
					return err
				}
				value.CapacityReady = value.CapacityLimit == 0 || value.ActiveTickets < value.CapacityLimit
			}
			manual := value.ManualAvailable
			if mode == "force_available" {
				manual = true
			}
			if mode == "force_unavailable" {
				manual = false
				value.Reason = "Channel paused"
			}
			value.Available = manual && value.StockReady && value.CapacityReady
			switch {
			case mode == "force_unavailable":
				value.ReasonCode = "channel_paused"
			case !manual:
				value.ReasonCode = "manager_override"
			case !value.StockReady:
				value.ReasonCode = "stock_shortage"
				value.Reason = "Ingredient stock is below recipe requirement"
			case !value.CapacityReady:
				value.ReasonCode = "station_capacity"
				value.Reason = "Station capacity is currently full"
			}
			values = append(values, value)
		}
		return nil
	})
	return values, err
}

func (r *PostgresRepository) CreateKitchenPrintJob(ctx context.Context, value domain.KitchenPrintJob, audit domain.AuditEvent) error {
	return r.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		raw, err := marshalConnected(value.Payload)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO kitchen_print_jobs(id,tenant_id,outlet_id,ticket_id,printer_route,copy_type,payload,status,attempts,last_error,created_at,acknowledged_at,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, value.ID, value.TenantID, value.OutletID, value.TicketID, value.PrinterRoute, value.CopyType, raw, value.Status, value.Attempts, value.LastError, value.CreatedAt, value.AcknowledgedAt, audit.OperationID)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (r *PostgresRepository) KitchenPrintJobs(ctx context.Context, tenant, outlet string) ([]domain.KitchenPrintJob, error) {
	values := []domain.KitchenPrintJob{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,ticket_id,printer_route,copy_type,payload,status,attempts,last_error,created_at,acknowledged_at FROM kitchen_print_jobs WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY created_at DESC,id DESC LIMIT 200`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.KitchenPrintJob
			var raw []byte
			if err := rows.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.TicketID, &value.PrinterRoute, &value.CopyType, &raw, &value.Status, &value.Attempts, &value.LastError, &value.CreatedAt, &value.AcknowledgedAt); err != nil {
				return err
			}
			if err := unmarshalConnected(raw, &value.Payload); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) IssuePickupToken(ctx context.Context, value domain.PickupToken, audit domain.AuditEvent) (domain.PickupToken, error) {
	err := r.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO pickup_tokens(id,tenant_id,outlet_id,order_id,token,status,issued_at,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, value.ID, value.TenantID, value.OutletID, value.OrderID, value.Token, value.Status, value.IssuedAt, audit.OperationID)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func (r *PostgresRepository) PickupTokens(ctx context.Context, tenant, outlet string) ([]domain.PickupToken, error) {
	values := []domain.PickupToken{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,order_id,token,status,issued_at,collected_at,version FROM pickup_tokens WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY issued_at DESC,id DESC LIMIT 200`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.PickupToken
			if err := rows.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.OrderID, &value.Token, &value.Status, &value.IssuedAt, &value.CollectedAt, &value.Version); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) CreateQROrderingLink(ctx context.Context, value domain.QROrderingLink, audit domain.AuditEvent) error {
	return r.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO qr_ordering_links(id,tenant_id,outlet_id,channel_id,table_id,slug,active,expires_at,created_at,updated_at,version)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, value.ID, value.TenantID, value.OutletID, nullable(value.ChannelID), nullable(value.TableID), value.Slug, value.Active, value.ExpiresAt, value.CreatedAt, value.UpdatedAt, value.Version)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (r *PostgresRepository) QROrderingLinks(ctx context.Context, tenant, outlet string) ([]domain.QROrderingLink, error) {
	values := []domain.QROrderingLink{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,COALESCE(channel_id::text,''),COALESCE(table_id::text,''),slug,active,expires_at,created_at,updated_at,version FROM qr_ordering_links WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY created_at DESC,id DESC LIMIT 200`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.QROrderingLink
			if err := rows.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.ChannelID, &value.TableID, &value.Slug, &value.Active, &value.ExpiresAt, &value.CreatedAt, &value.UpdatedAt, &value.Version); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) CreateStockTransfer(ctx context.Context, value domain.StockTransfer, audit domain.AuditEvent) error {
	return r.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO stock_transfers(id,tenant_id,source_outlet_id,destination_outlet_id,status,requested_by,notes,requested_at,dispatched_at,received_at,version)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, value.ID, value.TenantID, value.SourceOutletID, value.DestinationOutletID, value.Status, value.RequestedBy, value.Notes, value.RequestedAt, value.DispatchedAt, value.ReceivedAt, value.Version)
		if err != nil {
			return err
		}
		for _, line := range value.Lines {
			if _, err = tx.Exec(ctx, `INSERT INTO stock_transfer_lines(id,tenant_id,transfer_id,ingredient_id,quantity_base,dispatched_quantity_base,received_quantity_base)VALUES($1,$2,$3,$4,$5,$6,$7)`, line.ID, value.TenantID, value.ID, line.IngredientID, line.QuantityBase, line.DispatchedQuantityBase, line.ReceivedQuantityBase); err != nil {
				return err
			}
		}
		return insertAudit(ctx, tx, audit)
	})
}

func loadTransferLines(ctx context.Context, tx pgx.Tx, value *domain.StockTransfer) error {
	rows, err := tx.Query(ctx, `SELECT id,ingredient_id,quantity_base,dispatched_quantity_base,received_quantity_base FROM stock_transfer_lines WHERE tenant_id=$1 AND transfer_id=$2 ORDER BY id`, value.TenantID, value.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var line domain.StockTransferLine
		if err := rows.Scan(&line.ID, &line.IngredientID, &line.QuantityBase, &line.DispatchedQuantityBase, &line.ReceivedQuantityBase); err != nil {
			return err
		}
		value.Lines = append(value.Lines, line)
	}
	return rows.Err()
}

func (r *PostgresRepository) StockTransfers(ctx context.Context, tenant, outlet string) ([]domain.StockTransfer, error) {
	values := []domain.StockTransfer{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,source_outlet_id,destination_outlet_id,status,requested_by,notes,requested_at,dispatched_at,received_at,version FROM stock_transfers WHERE tenant_id=$1 AND (source_outlet_id=$2 OR destination_outlet_id=$2) ORDER BY requested_at DESC,id DESC LIMIT 200`, tenant, outlet)
		if err != nil {
			return err
		}
		for rows.Next() {
			var value domain.StockTransfer
			if err := rows.Scan(&value.ID, &value.TenantID, &value.SourceOutletID, &value.DestinationOutletID, &value.Status, &value.RequestedBy, &value.Notes, &value.RequestedAt, &value.DispatchedAt, &value.ReceivedAt, &value.Version); err != nil {
				rows.Close()
				return err
			}
			values = append(values, value)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for index := range values {
			if err := loadTransferLines(ctx, tx, &values[index]); err != nil {
				return err
			}
		}
		return nil
	})
	return values, err
}

func (r *PostgresRepository) SetOutletControlProfile(ctx context.Context, tenant, outlet string, value domain.OutletControlProfile, audit domain.AuditEvent) (domain.OutletControlProfile, error) {
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		approvals, err := marshalConnected(value.ApprovalPolicy)
		if err != nil {
			return err
		}
		features, err := marshalConnected(value.FeatureProfile)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO outlet_control_profiles(tenant_id,outlet_id,profile_name,approval_policy,feature_profile,updated_at)VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,outlet_id) DO UPDATE SET profile_name=EXCLUDED.profile_name,approval_policy=EXCLUDED.approval_policy,feature_profile=EXCLUDED.feature_profile,updated_at=EXCLUDED.updated_at,version=outlet_control_profiles.version+1`, tenant, outlet, value.ProfileName, approvals, features, audit.RecordedAt)
		if err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT version,updated_at FROM outlet_control_profiles WHERE tenant_id=$1 AND outlet_id=$2`, tenant, outlet).Scan(&value.Version, &value.UpdatedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func (r *PostgresRepository) OutletControlProfile(ctx context.Context, tenant, outlet string) (domain.OutletControlProfile, error) {
	var value domain.OutletControlProfile
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		var approvals, features []byte
		if err := tx.QueryRow(ctx, `SELECT outlet_id,profile_name,approval_policy,feature_profile,version,updated_at FROM outlet_control_profiles WHERE tenant_id=$1 AND outlet_id=$2`, tenant, outlet).Scan(&value.OutletID, &value.ProfileName, &approvals, &features, &value.Version, &value.UpdatedAt); err != nil {
			return err
		}
		if err := unmarshalConnected(approvals, &value.ApprovalPolicy); err != nil {
			return err
		}
		return unmarshalConnected(features, &value.FeatureProfile)
	})
	return value, err
}

func (r *PostgresRepository) RegisterHardwareDevice(ctx context.Context, value domain.HardwareDevice, audit domain.AuditEvent) error {
	return r.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO hardware_devices(id,tenant_id,outlet_id,device_type,manufacturer,model,serial_number,certification_status,gateway_reference,last_seen_at,created_at,updated_at,version)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, value.ID, value.TenantID, value.OutletID, value.DeviceType, value.Manufacturer, value.Model, value.SerialNumber, value.CertificationStatus, value.GatewayReference, value.LastSeenAt, value.CreatedAt, value.UpdatedAt, value.Version)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (r *PostgresRepository) HardwareDevices(ctx context.Context, tenant, outlet string) ([]domain.HardwareDevice, error) {
	values := []domain.HardwareDevice{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,device_type,manufacturer,model,serial_number,certification_status,gateway_reference,last_seen_at,created_at,updated_at,version FROM hardware_devices WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY device_type,manufacturer,model`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.HardwareDevice
			if err := rows.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.DeviceType, &value.Manufacturer, &value.Model, &value.SerialNumber, &value.CertificationStatus, &value.GatewayReference, &value.LastSeenAt, &value.CreatedAt, &value.UpdatedAt, &value.Version); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) CreateImplementationRunbook(ctx context.Context, value domain.ImplementationRunbook, audit domain.AuditEvent) error {
	return r.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		raw, err := marshalConnected(value.Checklist)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO implementation_runbooks(id,tenant_id,outlet_id,template_code,status,checklist,owner,due_at,created_at,updated_at,version)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, value.ID, value.TenantID, value.OutletID, value.TemplateCode, value.Status, raw, value.Owner, value.DueAt, value.CreatedAt, value.UpdatedAt, value.Version)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (r *PostgresRepository) ImplementationRunbooks(ctx context.Context, tenant, outlet string) ([]domain.ImplementationRunbook, error) {
	values := []domain.ImplementationRunbook{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,template_code,status,checklist,owner,due_at,created_at,updated_at,version FROM implementation_runbooks WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY created_at DESC,id DESC LIMIT 100`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.ImplementationRunbook
			var raw []byte
			if err := rows.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.TemplateCode, &value.Status, &raw, &value.Owner, &value.DueAt, &value.CreatedAt, &value.UpdatedAt, &value.Version); err != nil {
				return err
			}
			if err := unmarshalConnected(raw, &value.Checklist); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func settlementRows(ctx context.Context, tx pgx.Tx, tenant, outlet, date string) ([]domain.TenderSettlement, error) {
	values := []domain.TenderSettlement{}
	rows, err := tx.Query(ctx, `SELECT tender_type,COALESCE(SUM(amount_minor) FILTER(WHERE status='captured'),0),COALESCE(SUM(amount_minor) FILTER(WHERE status='reversed'),0),COUNT(*) FROM tenders WHERE tenant_id=$1 AND outlet_id=$2 AND occurred_at::date=$3::date GROUP BY tender_type ORDER BY tender_type`, tenant, outlet, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var value domain.TenderSettlement
		if err := rows.Scan(&value.TenderType, &value.GrossMinor, &value.ReversedMinor, &value.TransactionCount); err != nil {
			return nil, err
		}
		value.BusinessDate = date
		value.NetMinor = value.GrossMinor - value.ReversedMinor
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r *PostgresRepository) GSTReport(ctx context.Context, tenant, outlet, date string) (domain.GSTReport, error) {
	var value domain.GSTReport
	value.BusinessDate = date
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(MIN(currency),'INR'),COUNT(*),COALESCE(SUM(subtotal_minor-discount_minor),0),COALESCE(SUM(tax_minor),0),COALESCE(SUM(total_minor),0) FROM fiscal_receipts WHERE tenant_id=$1 AND outlet_id=$2 AND issued_at::date=$3::date`, tenant, outlet, date).Scan(&value.Currency, &value.InvoiceCount, &value.TaxableMinor, &value.GSTMinor, &value.GrossMinor)
	})
	return value, err
}

func (r *PostgresRepository) DayEndReport(ctx context.Context, tenant, outlet, date string) (domain.DayEndReport, error) {
	var value domain.DayEndReport
	value.BusinessDate = date
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MIN(currency),'INR'),COUNT(*),COALESCE(SUM(total_minor),0),COALESCE(SUM(discount_minor),0),COALESCE(SUM(tax_minor),0),COALESCE(SUM(service_charge_minor),0) FROM fiscal_receipts WHERE tenant_id=$1 AND outlet_id=$2 AND issued_at::date=$3::date`, tenant, outlet, date).Scan(&value.Currency, &value.ReceiptCount, &value.GrossMinor, &value.DiscountMinor, &value.TaxMinor, &value.ServiceChargeMinor); err != nil {
			return err
		}
		settlements, err := settlementRows(ctx, tx, tenant, outlet, date)
		if err != nil {
			return err
		}
		value.Settlements = settlements
		return nil
	})
	return value, err
}

var _ ConnectedCommerceRepository = (*PostgresRepository)(nil)
