// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) SubmitGuestOrderRequest(ctx context.Context, value domain.GuestOrderRequest) (domain.GuestOrderRequest, error) {
	err := r.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		// A browser may lose the response after PostgreSQL has committed. Replay
		// the original, immutable request before re-validating a potentially
		// changed menu or QR expiry window.
		var existing domain.GuestOrderRequest
		var existingLines []byte
		err := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,qr_link_id,COALESCE(channel_id::text,''),menu_version_id,tracking_code,guest_name,guest_phone,note,lines,total_minor,currency,payment_state,status,client_request_id,submitted_at FROM web_order_requests WHERE tenant_id=$1 AND outlet_id=$2 AND client_request_id=$3`, value.TenantID, value.OutletID, value.ClientRequestID).Scan(&existing.ID, &existing.TenantID, &existing.OutletID, &existing.QRLinkID, &existing.ChannelID, &existing.MenuVersionID, &existing.TrackingCode, &existing.GuestName, &existing.GuestPhone, &existing.Note, &existingLines, &existing.TotalMinor, &existing.Currency, &existing.PaymentState, &existing.Status, &existing.ClientRequestID, &existing.SubmittedAt)
		if err == nil {
			if err := json.Unmarshal(existingLines, &existing.Lines); err != nil {
				return err
			}
			value = existing
			return nil
		}
		if err != pgx.ErrNoRows {
			return err
		}
		var channelID string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(channel_id::text,'') FROM qr_ordering_links WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND active=true AND (expires_at IS NULL OR expires_at>clock_timestamp())`, value.TenantID, value.OutletID, value.QRLinkID).Scan(&channelID); err != nil {
			return err
		}
		var published bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM menu_studios studio JOIN menu_studio_versions version ON version.tenant_id=studio.tenant_id AND version.id=studio.current_version_id JOIN menu_publications publication ON publication.tenant_id=version.tenant_id AND publication.menu_version_id=version.id WHERE studio.tenant_id=$1 AND studio.outlet_id=$2 AND studio.current_version_id=$3 AND studio.status='published' AND version.status='published' AND publication.status='live' AND publication.effective_from<=clock_timestamp() AND (publication.effective_to IS NULL OR publication.effective_to>clock_timestamp()) AND (publication.channel_id IS NULL OR publication.channel_id=$4::uuid))`, value.TenantID, value.OutletID, value.MenuVersionID, nullable(channelID)).Scan(&published); err != nil {
			return err
		}
		if !published {
			return fmt.Errorf("%w: published QR menu no longer exists", ErrInvalidReference)
		}
		value.ChannelID = channelID
		currency := ""
		quoted := make([]domain.GuestOrderRequestLine, 0, len(value.Lines))
		for _, requestLine := range value.Lines {
			catalog, _, err := checkoutCatalog(ctx, tx, domain.POSCheckout{TenantID: value.TenantID, OutletID: value.OutletID, MenuVersionID: value.MenuVersionID}, domain.POSCheckoutLine{MenuItemID: requestLine.MenuItemID, Quantity: requestLine.Quantity})
			if err != nil || !catalog.active || catalog.stationID == "" {
				return fmt.Errorf("%w: QR menu item is unavailable", ErrInvalidReference)
			}
			var manualAvailable bool
			if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT available FROM menu_item_availability WHERE tenant_id=$1 AND outlet_id=$2 AND menu_item_id=$3),true)`, value.TenantID, value.OutletID, catalog.id).Scan(&manualAvailable); err != nil {
				return err
			}
			var availabilityMode string
			if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT availability_mode FROM channel_menu_items WHERE tenant_id=$1 AND channel_id=NULLIF($2,'')::uuid AND menu_item_id=$3),'inherit')`, value.TenantID, channelID, catalog.id).Scan(&availabilityMode); err != nil {
				return err
			}
			if availabilityMode == "force_unavailable" || (!manualAvailable && availabilityMode != "force_available") {
				return fmt.Errorf("%w: QR menu item is unavailable", ErrInvalidReference)
			}
			var stockReady bool
			if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM recipe_versions version JOIN recipe_components component ON component.tenant_id=version.tenant_id AND component.recipe_version_id=version.id JOIN units unit ON unit.tenant_id=component.tenant_id AND unit.id=component.unit_id LEFT JOIN LATERAL (SELECT COALESCE(SUM(event.quantity_base),0) AS balance FROM inventory_events event WHERE event.tenant_id=$1 AND event.outlet_id=$2 AND event.ingredient_id=component.ingredient_id) stock ON true WHERE version.tenant_id=$1 AND version.recipe_id=$3 AND version.effective_from<=clock_timestamp() AND (version.effective_to IS NULL OR version.effective_to>clock_timestamp()) AND component.ingredient_id IS NOT NULL AND stock.balance < component.quantity * unit.base_numerator::numeric / unit.base_denominator::numeric)`, value.TenantID, value.OutletID, catalog.recipeID).Scan(&stockReady); err != nil {
				return err
			}
			if !stockReady {
				return fmt.Errorf("%w: QR menu item stock is unavailable", ErrInvalidReference)
			}
			var capacityLimit, activeTickets int
			if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT max_active_tickets FROM station_capacity_limits WHERE tenant_id=$1 AND outlet_id=$2 AND station_id=$3),0),(SELECT COUNT(*) FROM kitchen_tickets WHERE tenant_id=$1 AND outlet_id=$2 AND station_id=$3 AND status IN('queued','fired','preparing'))`, value.TenantID, value.OutletID, catalog.stationID).Scan(&capacityLimit, &activeTickets); err != nil {
				return err
			}
			if capacityLimit > 0 && activeTickets >= capacityLimit {
				return fmt.Errorf("%w: QR menu station is at capacity", ErrInvalidReference)
			}
			if currency == "" {
				currency = catalog.currency
			}
			if currency != catalog.currency {
				return fmt.Errorf("%w: QR menu currency mismatch", ErrInvalidReference)
			}
			line := domain.GuestOrderRequestLine{MenuItemID: catalog.id, Name: catalog.name, Quantity: requestLine.Quantity, UnitMinor: catalog.price, LineMinor: catalog.price * int64(requestLine.Quantity)}
			quoted = append(quoted, line)
			value.TotalMinor += line.LineMinor
		}
		value.Lines, value.Currency = quoted, currency
		raw, err := json.Marshal(value.Lines)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO web_order_requests(id,tenant_id,outlet_id,qr_link_id,channel_id,menu_version_id,tracking_code,guest_name,guest_phone,note,lines,total_minor,currency,payment_state,status,client_request_id,submitted_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, value.ID, value.TenantID, value.OutletID, value.QRLinkID, nullable(value.ChannelID), value.MenuVersionID, value.TrackingCode, value.GuestName, value.GuestPhone, value.Note, raw, value.TotalMinor, value.Currency, value.PaymentState, value.Status, value.ClientRequestID, value.SubmittedAt); err != nil {
			return err
		}
		return nil
	})
	return value, err
}

func (r *PostgresRepository) GuestOrderRequests(ctx context.Context, tenant, outlet string) ([]domain.GuestOrderRequest, error) {
	values := []domain.GuestOrderRequest{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,qr_link_id,COALESCE(channel_id::text,''),menu_version_id,tracking_code,guest_name,guest_phone,note,lines,total_minor,currency,payment_state,status,client_request_id,submitted_at FROM web_order_requests WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY submitted_at DESC,id DESC LIMIT 200`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.GuestOrderRequest
			var raw []byte
			if err := rows.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.QRLinkID, &value.ChannelID, &value.MenuVersionID, &value.TrackingCode, &value.GuestName, &value.GuestPhone, &value.Note, &raw, &value.TotalMinor, &value.Currency, &value.PaymentState, &value.Status, &value.ClientRequestID, &value.SubmittedAt); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &value.Lines); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

var _ DirectOrderingRepository = (*PostgresRepository)(nil)
