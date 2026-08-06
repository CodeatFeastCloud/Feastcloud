// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func insertMenuStudioVersion(ctx context.Context, tx pgx.Tx, studio domain.MenuStudio, value domain.MenuStudioVersion) error {
	if _, err := tx.Exec(ctx, `INSERT INTO menu_studio_versions(id,tenant_id,menu_studio_id,version_number,status,effective_from,created_at,published_at,published_by)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, value.ID, studio.TenantID, studio.ID, value.VersionNumber, value.Status, value.EffectiveFrom, value.CreatedAt, value.PublishedAt, value.PublishedBy); err != nil {
		return err
	}
	categories := map[string]struct{}{}
	for _, category := range value.Categories {
		if _, err := tx.Exec(ctx, `INSERT INTO menu_studio_categories(id,tenant_id,menu_version_id,name,sort_order,active)VALUES($1,$2,$3,$4,$5,$6)`, category.ID, studio.TenantID, value.ID, category.Name, category.SortOrder, category.Active); err != nil {
			return err
		}
		categories[category.ID] = struct{}{}
	}
	groups := map[string]struct{}{}
	for _, group := range value.Modifiers {
		if _, err := tx.Exec(ctx, `INSERT INTO menu_modifier_groups(id,tenant_id,menu_version_id,name,selection_min,selection_max,required,sort_order)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, group.ID, studio.TenantID, value.ID, group.Name, group.SelectionMin, group.SelectionMax, group.Required, group.SortOrder); err != nil {
			return err
		}
		groups[group.ID] = struct{}{}
		for _, option := range group.Options {
			if _, err := tx.Exec(ctx, `INSERT INTO menu_modifier_options(id,tenant_id,modifier_group_id,name,price_delta_minor,active,sort_order)VALUES($1,$2,$3,$4,$5,$6,$7)`, option.ID, studio.TenantID, group.ID, option.Name, option.PriceDeltaMinor, option.Active, option.SortOrder); err != nil {
				return err
			}
		}
	}
	items := map[string]struct{}{}
	for _, item := range value.Items {
		var correctOutlet bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM menu_items WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3)`, studio.TenantID, studio.OutletID, item.MenuItemID).Scan(&correctOutlet); err != nil {
			return err
		}
		if !correctOutlet {
			return fmt.Errorf("%w: menu item is not owned by menu studio outlet", ErrInvalidReference)
		}
		if item.CategoryID != "" {
			if _, ok := categories[item.CategoryID]; !ok {
				return fmt.Errorf("%w: menu category is not in this version", ErrInvalidReference)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO menu_version_items(tenant_id,menu_version_id,menu_item_id,category_id,display_name,description,sort_order,active)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, studio.TenantID, value.ID, item.MenuItemID, nullable(item.CategoryID), item.DisplayName, item.Description, item.SortOrder, item.Active); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO menu_version_item_prices(id,tenant_id,menu_version_id,menu_item_id,price_minor,currency,effective_from)VALUES($1,$2,$3,$4,$5,$6,$7)`, item.PriceID, studio.TenantID, value.ID, item.MenuItemID, item.PriceMinor, item.Currency, value.EffectiveFrom); err != nil {
			return err
		}
		items[item.MenuItemID] = struct{}{}
		for _, groupID := range item.ModifierGroupIDs {
			if _, ok := groups[groupID]; !ok {
				return fmt.Errorf("%w: modifier group is not in this version", ErrInvalidReference)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO menu_version_item_modifiers(tenant_id,menu_version_id,menu_item_id,modifier_group_id,sort_order)VALUES($1,$2,$3,$4,0)`, studio.TenantID, value.ID, item.MenuItemID, groupID); err != nil {
				return err
			}
		}
	}
	for _, publication := range value.Publications {
		if publication.ChannelID != "" {
			var correctOutlet bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sales_channels WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3)`, studio.TenantID, studio.OutletID, publication.ChannelID).Scan(&correctOutlet); err != nil {
				return err
			}
			if !correctOutlet {
				return fmt.Errorf("%w: publication channel is not owned by menu studio outlet", ErrInvalidReference)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO menu_publications(id,tenant_id,outlet_id,menu_version_id,channel_id,status,effective_from,effective_to,published_by,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, publication.ID, studio.TenantID, studio.OutletID, value.ID, nullable(publication.ChannelID), publication.Status, publication.EffectiveFrom, publication.EffectiveTo, value.PublishedBy, value.CreatedAt); err != nil {
			return err
		}
	}
	_ = items
	return nil
}

func (r *PostgresRepository) CreateMenuStudio(ctx context.Context, studio domain.MenuStudio, version domain.MenuStudioVersion, audit domain.AuditEvent) (domain.MenuStudio, error) {
	err := r.withTenant(ctx, studio.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO menu_studios(id,tenant_id,outlet_id,name,status,current_version_id,version,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, studio.ID, studio.TenantID, studio.OutletID, studio.Name, studio.Status, nullable(studio.CurrentVersionID), studio.Version, studio.CreatedAt, studio.UpdatedAt); err != nil {
			return err
		}
		if err := insertMenuStudioVersion(ctx, tx, studio, version); err != nil {
			return err
		}
		if version.Status == "published" {
			if _, err := tx.Exec(ctx, `UPDATE menu_studios SET status='published',current_version_id=$3,updated_at=$4 WHERE tenant_id=$1 AND id=$2`, studio.TenantID, studio.ID, version.ID, audit.RecordedAt); err != nil {
				return err
			}
			studio.Status = "published"
			studio.CurrentVersionID = version.ID
			studio.UpdatedAt = audit.RecordedAt
		}
		return insertAudit(ctx, tx, audit)
	})
	if err == nil {
		studio.Current = &version
	}
	return studio, err
}

func loadMenuStudioVersion(ctx context.Context, tx pgx.Tx, tenant, id string) (*domain.MenuStudioVersion, error) {
	var value domain.MenuStudioVersion
	if err := tx.QueryRow(ctx, `SELECT id,menu_studio_id,version_number,status,effective_from,created_at,published_at,published_by FROM menu_studio_versions WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&value.ID, &value.MenuStudioID, &value.VersionNumber, &value.Status, &value.EffectiveFrom, &value.CreatedAt, &value.PublishedAt, &value.PublishedBy); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,name,sort_order,active FROM menu_studio_categories WHERE tenant_id=$1 AND menu_version_id=$2 ORDER BY sort_order,id`, tenant, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x domain.MenuStudioCategory
		if err := rows.Scan(&x.ID, &x.Name, &x.SortOrder, &x.Active); err != nil {
			rows.Close()
			return nil, err
		}
		value.Categories = append(value.Categories, x)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT id,name,selection_min,selection_max,required,sort_order FROM menu_modifier_groups WHERE tenant_id=$1 AND menu_version_id=$2 ORDER BY sort_order,id`, tenant, id)
	if err != nil {
		return nil, err
	}
	groups := []domain.MenuModifierGroup{}
	for rows.Next() {
		var group domain.MenuModifierGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.SelectionMin, &group.SelectionMax, &group.Required, &group.SortOrder); err != nil {
			rows.Close()
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, group := range groups {
		options, err := tx.Query(ctx, `SELECT id,name,price_delta_minor,active,sort_order FROM menu_modifier_options WHERE tenant_id=$1 AND modifier_group_id=$2 ORDER BY sort_order,id`, tenant, group.ID)
		if err != nil {
			return nil, err
		}
		for options.Next() {
			var option domain.MenuModifierOption
			if err := options.Scan(&option.ID, &option.Name, &option.PriceDeltaMinor, &option.Active, &option.SortOrder); err != nil {
				options.Close()
				return nil, err
			}
			group.Options = append(group.Options, option)
		}
		if err := options.Err(); err != nil {
			options.Close()
			return nil, err
		}
		options.Close()
		value.Modifiers = append(value.Modifiers, group)
	}
	rows, err = tx.Query(ctx, `SELECT item.menu_item_id,COALESCE(item.category_id::text,''),item.display_name,item.description,item.sort_order,item.active,price.id,price.price_minor,price.currency FROM menu_version_items item JOIN menu_version_item_prices price ON price.tenant_id=item.tenant_id AND price.menu_version_id=item.menu_version_id AND price.menu_item_id=item.menu_item_id AND price.effective_to IS NULL WHERE item.tenant_id=$1 AND item.menu_version_id=$2 ORDER BY item.sort_order,item.display_name,item.menu_item_id`, tenant, id)
	if err != nil {
		return nil, err
	}
	items := []domain.MenuStudioItem{}
	for rows.Next() {
		var x domain.MenuStudioItem
		if err := rows.Scan(&x.MenuItemID, &x.CategoryID, &x.DisplayName, &x.Description, &x.SortOrder, &x.Active, &x.PriceID, &x.PriceMinor, &x.Currency); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, item := range items {
		groupRows, err := tx.Query(ctx, `SELECT modifier_group_id FROM menu_version_item_modifiers WHERE tenant_id=$1 AND menu_version_id=$2 AND menu_item_id=$3 ORDER BY sort_order,modifier_group_id`, tenant, id, item.MenuItemID)
		if err != nil {
			return nil, err
		}
		for groupRows.Next() {
			var group string
			if err := groupRows.Scan(&group); err != nil {
				groupRows.Close()
				return nil, err
			}
			item.ModifierGroupIDs = append(item.ModifierGroupIDs, group)
		}
		if err := groupRows.Err(); err != nil {
			groupRows.Close()
			return nil, err
		}
		groupRows.Close()
		value.Items = append(value.Items, item)
	}
	rows, err = tx.Query(ctx, `SELECT id,COALESCE(channel_id::text,''),status,effective_from,effective_to FROM menu_publications WHERE tenant_id=$1 AND menu_version_id=$2 ORDER BY effective_from,id`, tenant, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x domain.MenuPublication
		if err := rows.Scan(&x.ID, &x.ChannelID, &x.Status, &x.EffectiveFrom, &x.EffectiveTo); err != nil {
			return nil, err
		}
		value.Publications = append(value.Publications, x)
	}
	err = rows.Err()
	rows.Close()
	return &value, err
}

func (r *PostgresRepository) AddMenuStudioVersion(ctx context.Context, tenant, outlet string, value domain.MenuStudioVersion, expected uint64, audit domain.AuditEvent) (domain.MenuStudio, error) {
	var studio domain.MenuStudio
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,name,status,COALESCE(current_version_id::text,''),version,created_at,updated_at FROM menu_studios WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, tenant, outlet, value.MenuStudioID).Scan(&studio.ID, &studio.TenantID, &studio.OutletID, &studio.Name, &studio.Status, &studio.CurrentVersionID, &studio.Version, &studio.CreatedAt, &studio.UpdatedAt); err != nil {
			return err
		}
		if studio.Version != expected {
			return ErrVersionConflict
		}
		var last int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number),0) FROM menu_studio_versions WHERE tenant_id=$1 AND menu_studio_id=$2`, tenant, studio.ID).Scan(&last); err != nil {
			return err
		}
		if value.VersionNumber != last+1 {
			return fmt.Errorf("%w: menu version must be %d", ErrVersionConflict, last+1)
		}
		if err := insertMenuStudioVersion(ctx, tx, studio, value); err != nil {
			return err
		}
		if value.Status == "published" {
			if _, err := tx.Exec(ctx, `UPDATE menu_studios SET status='published',current_version_id=$4,version=version+1,updated_at=$5 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, tenant, outlet, studio.ID, value.ID, audit.RecordedAt); err != nil {
				return err
			}
			studio.Status = "published"
			studio.CurrentVersionID = value.ID
		}
		studio.Version++
		studio.UpdatedAt = audit.RecordedAt
		return insertAudit(ctx, tx, audit)
	})
	if err == nil {
		studio.Current = &value
	}
	return studio, err
}

func (r *PostgresRepository) MenuStudios(ctx context.Context, tenant, outlet string) ([]domain.MenuStudio, error) {
	values := []domain.MenuStudio{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,name,status,COALESCE(current_version_id::text,''),version,created_at,updated_at FROM menu_studios WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY name,id`, tenant, outlet)
		if err != nil {
			return err
		}
		for rows.Next() {
			var value domain.MenuStudio
			if err := rows.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.Name, &value.Status, &value.CurrentVersionID, &value.Version, &value.CreatedAt, &value.UpdatedAt); err != nil {
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
			if values[index].CurrentVersionID == "" {
				continue
			}
			version, err := loadMenuStudioVersion(ctx, tx, tenant, values[index].CurrentVersionID)
			if err != nil {
				return err
			}
			values[index].Current = version
		}
		return nil
	})
	return values, err
}

// LiveMenuStudio resolves the explicitly published menu for a sales channel.
// Channel publications take precedence over global publications, so a table QR
// can never accidentally inherit a counter-only menu.
func (r *PostgresRepository) LiveMenuStudio(ctx context.Context, tenant, outlet, channelID string, at time.Time) (domain.MenuStudio, error) {
	var value domain.MenuStudio
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		var menuVersionID string
		err := tx.QueryRow(ctx, `SELECT studio.id,studio.tenant_id,studio.outlet_id,studio.name,studio.status,publication.menu_version_id::text,studio.version,studio.created_at,studio.updated_at FROM menu_publications publication JOIN menu_studio_versions version ON version.tenant_id=publication.tenant_id AND version.id=publication.menu_version_id JOIN menu_studios studio ON studio.tenant_id=version.tenant_id AND studio.id=version.menu_studio_id WHERE publication.tenant_id=$1 AND publication.outlet_id=$2 AND publication.status='live' AND version.status='published' AND publication.effective_from<=$4 AND (publication.effective_to IS NULL OR publication.effective_to>$4) AND (publication.channel_id IS NULL OR publication.channel_id=NULLIF($3,'')::uuid) ORDER BY (publication.channel_id IS NOT NULL) DESC,publication.effective_from DESC,publication.id DESC LIMIT 1`, tenant, outlet, channelID, at).Scan(&value.ID, &value.TenantID, &value.OutletID, &value.Name, &value.Status, &menuVersionID, &value.Version, &value.CreatedAt, &value.UpdatedAt)
		if err != nil {
			return err
		}
		version, err := loadMenuStudioVersion(ctx, tx, tenant, menuVersionID)
		if err != nil {
			return err
		}
		value.CurrentVersionID = menuVersionID
		value.Current = version
		return nil
	})
	return value, err
}

type checkoutCatalogItem struct {
	id, name, stationID, recipeID string
	price                         int64
	currency                      string
	active                        bool
}
type checkoutModifierOption struct {
	id, groupID, groupName, name string
	min, max                     int
	required, active             bool
	delta                        int64
}

func checkoutCatalog(ctx context.Context, tx pgx.Tx, checkout domain.POSCheckout, line domain.POSCheckoutLine) (checkoutCatalogItem, []checkoutModifierOption, error) {
	var item checkoutCatalogItem
	if checkout.MenuVersionID == "" {
		err := tx.QueryRow(ctx, `SELECT id,name,COALESCE(station_id::text,''),recipe_id,price_minor,currency,active FROM menu_items WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, checkout.TenantID, checkout.OutletID, line.MenuItemID).Scan(&item.id, &item.name, &item.stationID, &item.recipeID, &item.price, &item.currency, &item.active)
		return item, nil, err
	}
	err := tx.QueryRow(ctx, `SELECT menu.id,version_item.display_name,COALESCE(menu.station_id::text,''),menu.recipe_id,price.price_minor,price.currency,version_item.active FROM menu_version_items version_item JOIN menu_items menu ON menu.tenant_id=version_item.tenant_id AND menu.id=version_item.menu_item_id JOIN menu_version_item_prices price ON price.tenant_id=version_item.tenant_id AND price.menu_version_id=version_item.menu_version_id AND price.menu_item_id=version_item.menu_item_id AND price.effective_to IS NULL WHERE version_item.tenant_id=$1 AND version_item.menu_version_id=$2 AND menu.outlet_id=$3 AND version_item.menu_item_id=$4`, checkout.TenantID, checkout.MenuVersionID, checkout.OutletID, line.MenuItemID).Scan(&item.id, &item.name, &item.stationID, &item.recipeID, &item.price, &item.currency, &item.active)
	if err != nil {
		return item, nil, err
	}
	options := []checkoutModifierOption{}
	rows, err := tx.Query(ctx, `SELECT option.id,group_item.modifier_group_id,grouping.name,option.name,grouping.selection_min,grouping.selection_max,grouping.required,option.active,option.price_delta_minor FROM menu_version_item_modifiers group_item JOIN menu_modifier_groups grouping ON grouping.tenant_id=group_item.tenant_id AND grouping.id=group_item.modifier_group_id JOIN menu_modifier_options option ON option.tenant_id=grouping.tenant_id AND option.modifier_group_id=grouping.id WHERE group_item.tenant_id=$1 AND group_item.menu_version_id=$2 AND group_item.menu_item_id=$3`, checkout.TenantID, checkout.MenuVersionID, line.MenuItemID)
	if err != nil {
		return item, nil, err
	}
	for rows.Next() {
		var option checkoutModifierOption
		if err := rows.Scan(&option.id, &option.groupID, &option.groupName, &option.name, &option.min, &option.max, &option.required, &option.active, &option.delta); err != nil {
			rows.Close()
			return item, nil, err
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return item, nil, err
	}
	rows.Close()
	return item, options, nil
}

func (r *PostgresRepository) CheckoutPOS(ctx context.Context, checkout domain.POSCheckout, audit domain.AuditEvent) (domain.POSCheckoutResult, error) {
	var result domain.POSCheckoutResult
	err := r.withTenant(ctx, checkout.TenantID, func(tx pgx.Tx) error {
		now := audit.RecordedAt
		order := domain.Order{ID: checkout.OrderID, TenantID: checkout.TenantID, OutletID: checkout.OutletID, BrandID: checkout.BrandID, ExternalRef: checkout.ExternalRef, Type: checkout.OrderType, Status: domain.OrderStatusReceived, PlacedAt: checkout.PlacedAt, RecordMetadata: domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}}
		if order.PlacedAt.IsZero() {
			order.PlacedAt = now
		}
		stations := map[string][]string{}
		type modifierSnapshot struct {
			orderLineID string
			option      checkoutModifierOption
		}
		modifierSnapshots := []modifierSnapshot{}
		currency := ""
		for _, input := range checkout.Lines {
			item, allowed, err := checkoutCatalog(ctx, tx, checkout, input)
			if err != nil {
				return err
			}
			if !item.active || item.stationID == "" {
				return fmt.Errorf("%w: menu item is not sellable", ErrInvalidReference)
			}
			var managerAvailable bool
			if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT available FROM menu_item_availability WHERE tenant_id=$1 AND outlet_id=$2 AND menu_item_id=$3),true)`, checkout.TenantID, checkout.OutletID, item.id).Scan(&managerAvailable); err != nil {
				return err
			}
			if !managerAvailable {
				return fmt.Errorf("%w: menu item is unavailable", ErrInvalidReference)
			}
			if currency == "" {
				currency = item.currency
			}
			if currency != item.currency {
				return fmt.Errorf("%w: order currency mismatch", ErrInvalidReference)
			}
			optionsByID := map[string]checkoutModifierOption{}
			groups := map[string]checkoutModifierOption{}
			for _, option := range allowed {
				optionsByID[option.id] = option
				groups[option.groupID] = option
			}
			seen := map[string]bool{}
			counts := map[string]int{}
			delta := int64(0)
			selected := []checkoutModifierOption{}
			for _, optionID := range input.ModifierOptionIDs {
				option, ok := optionsByID[optionID]
				if !ok || !option.active || seen[optionID] {
					return fmt.Errorf("%w: invalid modifier selection", ErrInvalidReference)
				}
				seen[optionID] = true
				counts[option.groupID]++
				delta += option.delta
				selected = append(selected, option)
			}
			for groupID, group := range groups {
				if counts[groupID] < group.min || counts[groupID] > group.max {
					return fmt.Errorf("%w: modifier selection count", ErrInvalidReference)
				}
			}
			unit := item.price + delta
			if unit < 0 {
				return fmt.Errorf("%w: modifier price underflow", ErrInvalidReference)
			}
			line := domain.OrderLine{ID: input.ID, MenuItemID: item.id, Name: item.name, Quantity: input.Quantity, UnitPrice: domain.Money{MinorUnits: unit, Currency: currency}, LineTotal: domain.Money{MinorUnits: unit * int64(input.Quantity), Currency: currency}, PreparationNote: input.PreparationNote}
			order.Lines = append(order.Lines, line)
			order.Subtotal.MinorUnits += line.LineTotal.MinorUnits
			stations[item.stationID] = append(stations[item.stationID], line.ID)
			for _, option := range selected {
				modifierSnapshots = append(modifierSnapshots, modifierSnapshot{orderLineID: line.ID, option: option})
			}
		}
		if checkout.DiscountMinor > order.Subtotal.MinorUnits {
			return fmt.Errorf("%w: discount exceeds subtotal", ErrInvalidReference)
		}
		order.Subtotal.Currency = currency
		order.DiscountTotal = domain.Money{MinorUnits: checkout.DiscountMinor, Currency: currency}
		order.TaxTotal = domain.Money{MinorUnits: checkout.TaxMinor, Currency: currency}
		order.ServiceCharge = domain.Money{MinorUnits: checkout.ServiceChargeMinor, Currency: currency}
		order.Total = domain.Money{MinorUnits: order.Subtotal.MinorUnits - checkout.DiscountMinor + checkout.TaxMinor + checkout.ServiceChargeMinor, Currency: currency}
		if _, err := tx.Exec(ctx, `INSERT INTO orders(id,tenant_id,outlet_id,brand_id,external_ref,source,source_id,order_type,status,currency,subtotal_minor,discount_total_minor,tax_total_minor,service_charge_minor,total_minor,placed_at,version,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,1,$17,$17)`, order.ID, order.TenantID, order.OutletID, nullable(order.BrandID), nullable(order.ExternalRef), audit.Source, nullable(audit.SourceID), order.Type, order.Status, currency, order.Subtotal.MinorUnits, order.DiscountTotal.MinorUnits, order.TaxTotal.MinorUnits, order.ServiceCharge.MinorUnits, order.Total.MinorUnits, order.PlacedAt, now); err != nil {
			return err
		}
		for _, line := range order.Lines {
			if _, err := tx.Exec(ctx, `INSERT INTO order_lines(id,tenant_id,order_id,menu_item_id,name,quantity,currency,unit_price_minor,line_total_minor,preparation_note,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, line.ID, checkout.TenantID, order.ID, line.MenuItemID, line.Name, line.Quantity, currency, line.UnitPrice.MinorUnits, line.LineTotal.MinorUnits, nullable(line.PreparationNote), now); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO order_line_recipe_snapshots(tenant_id,order_id,order_line_id,menu_item_id,recipe_version_id,quantity,captured_at) SELECT $1,$2,$3,item.id,version.id,$5,$6 FROM menu_items item JOIN recipe_versions version ON version.tenant_id=item.tenant_id AND version.recipe_id=item.recipe_id AND version.effective_from<=$6 AND (version.effective_to IS NULL OR version.effective_to>$6) WHERE item.tenant_id=$1 AND item.outlet_id=$4 AND item.id=$7`, checkout.TenantID, order.ID, line.ID, checkout.OutletID, line.Quantity, order.PlacedAt, line.MenuItemID); err != nil {
				return err
			}
		}
		for _, snapshot := range modifierSnapshots {
			option := snapshot.option
			if _, err := tx.Exec(ctx, `INSERT INTO order_line_modifier_snapshots(tenant_id,order_line_id,modifier_group_id,modifier_group_name,modifier_option_id,modifier_option_name,price_delta_minor,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, checkout.TenantID, snapshot.orderLineID, option.groupID, option.groupName, option.id, option.name, option.delta, now); err != nil {
				return err
			}
		}
		stationIDs := make([]string, 0, len(stations))
		for stationID := range stations {
			stationIDs = append(stationIDs, stationID)
		}
		sort.Strings(stationIDs)
		for _, stationID := range stationIDs {
			ticketID := inventoryEventUUID(checkout.TenantID, checkout.ID, "ticket:"+stationID)
			ticket := domain.KitchenTicket{ID: ticketID, TenantID: checkout.TenantID, OutletID: checkout.OutletID, OrderID: order.ID, StationID: stationID, LineIDs: stations[stationID], Status: domain.TicketStatusQueued, RecordMetadata: domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}}
			if _, err := tx.Exec(ctx, `INSERT INTO kitchen_tickets(id,tenant_id,outlet_id,order_id,station_id,status,priority,version,created_at,updated_at)VALUES($1,$2,$3,$4,$5,'queued',0,1,$6,$6)`, ticket.ID, ticket.TenantID, ticket.OutletID, ticket.OrderID, ticket.StationID, now); err != nil {
				return err
			}
			for _, lineID := range ticket.LineIDs {
				if _, err := tx.Exec(ctx, `INSERT INTO ticket_lines(tenant_id,ticket_id,order_id,order_line_id,created_at)VALUES($1,$2,$3,$4,$5)`, checkout.TenantID, ticket.ID, order.ID, lineID, now); err != nil {
					return err
				}
			}
			result.Tickets = append(result.Tickets, ticket)
			printID := inventoryEventUUID(checkout.TenantID, checkout.ID, "print:"+stationID)
			printOperation := inventoryEventUUID(checkout.TenantID, audit.OperationID, "print:"+stationID)
			payload := []byte(fmt.Sprintf(`{"orderId":%q,"ticketId":%q,"stationId":%q}`, order.ID, ticket.ID, stationID))
			if _, err := tx.Exec(ctx, `INSERT INTO kitchen_print_jobs(id,tenant_id,outlet_id,ticket_id,printer_route,copy_type,payload,status,attempts,last_error,created_at,operation_id)VALUES($1,$2,$3,$4,$5,'kot',$6,'queued',0,'',$7,$8)`, printID, checkout.TenantID, checkout.OutletID, ticket.ID, checkout.PrinterRoute, payload, now, printOperation); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO kitchen_print_job_events(id,tenant_id,print_job_id,event_type,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,'queued',$4,$5,$6)`, inventoryEventUUID(checkout.TenantID, checkout.ID, "print-event:"+stationID), checkout.TenantID, printID, now, audit.ActorID, printOperation); err != nil {
				return err
			}
			result.PrintJobs = append(result.PrintJobs, domain.KitchenPrintJob{ID: printID, TenantID: checkout.TenantID, OutletID: checkout.OutletID, TicketID: ticket.ID, PrinterRoute: checkout.PrinterRoute, CopyType: "kot", Payload: map[string]any{"orderId": order.ID, "ticketId": ticket.ID, "stationId": stationID}, Status: "queued", CreatedAt: now})
		}
		if checkout.PickupTokenID != "" {
			token := domain.PickupToken{ID: checkout.PickupTokenID, TenantID: checkout.TenantID, OutletID: checkout.OutletID, OrderID: order.ID, Token: checkout.PickupToken, Status: "issued", IssuedAt: now, Version: 1}
			tokenOperation := inventoryEventUUID(checkout.TenantID, audit.OperationID, "token")
			if _, err := tx.Exec(ctx, `INSERT INTO pickup_tokens(id,tenant_id,outlet_id,order_id,token,status,issued_at,operation_id)VALUES($1,$2,$3,$4,$5,'issued',$6,$7)`, token.ID, token.TenantID, token.OutletID, token.OrderID, token.Token, now, tokenOperation); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO pickup_token_events(id,tenant_id,pickup_token_id,event_type,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,'issued',$4,$5,$6)`, inventoryEventUUID(checkout.TenantID, checkout.ID, "token-event"), checkout.TenantID, token.ID, now, audit.ActorID, tokenOperation); err != nil {
				return err
			}
			result.PickupToken = &token
		}
		paid := int64(0)
		for _, input := range checkout.Tenders {
			if input.AmountMinor < 1 {
				return fmt.Errorf("%w: tender amount", ErrInvalidReference)
			}
			if input.TenderType == "cash" {
				tag, err := tx.Exec(ctx, `UPDATE cash_shifts SET expected_cash_minor=expected_cash_minor+$4 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND status='open'`, checkout.TenantID, checkout.OutletID, input.CashShiftID, input.AmountMinor)
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 1 {
					return fmt.Errorf("%w: cash shift", ErrInvalidReference)
				}
			}
			tenderOperation := inventoryEventUUID(checkout.TenantID, audit.OperationID, "tender:"+input.ID)
			if _, err := tx.Exec(ctx, `INSERT INTO tenders(id,tenant_id,outlet_id,order_id,cash_shift_id,tender_type,amount_minor,currency,provider_reference,status,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'captured',$10,$11,$12)`, input.ID, checkout.TenantID, checkout.OutletID, order.ID, nullable(input.CashShiftID), input.TenderType, input.AmountMinor, currency, input.ProviderReference, now, audit.ActorID, tenderOperation); err != nil {
				return err
			}
			if input.TenderType == "cash" {
				if _, err := tx.Exec(ctx, `INSERT INTO cash_events(id,tenant_id,cash_shift_id,event_type,amount_minor,reason,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,'cash_sale',$4,'POS checkout',$5,$6,$7)`, inventoryEventUUID(checkout.TenantID, checkout.ID, "cash:"+input.ID), checkout.TenantID, input.CashShiftID, input.AmountMinor, now, audit.ActorID, tenderOperation); err != nil {
					return err
				}
			}
			paid += input.AmountMinor
			result.Tenders = append(result.Tenders, domain.Tender{ID: input.ID, TenantID: checkout.TenantID, OutletID: checkout.OutletID, OrderID: order.ID, CashShiftID: input.CashShiftID, TenderType: input.TenderType, AmountMinor: input.AmountMinor, Currency: currency, ProviderReference: input.ProviderReference, Status: "captured", OccurredAt: now})
		}
		if paid != order.Total.MinorUnits {
			return fmt.Errorf("%w: split tenders must equal order total", ErrInvalidReference)
		}
		receipt := domain.FiscalReceipt{ID: checkout.ReceiptID, OrderID: order.ID, ReceiptNumber: checkout.ReceiptNumber, Currency: currency, SubtotalMinor: order.Subtotal.MinorUnits, DiscountMinor: order.DiscountTotal.MinorUnits, TaxMinor: order.TaxTotal.MinorUnits, ServiceChargeMinor: order.ServiceCharge.MinorUnits, TotalMinor: order.Total.MinorUnits, IssuedAt: now}
		if _, err := tx.Exec(ctx, `INSERT INTO fiscal_receipts(id,tenant_id,outlet_id,order_id,receipt_number,currency,subtotal_minor,discount_minor,tax_minor,service_charge_minor,total_minor,tax_snapshot,issued_at,actor_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'{"scheme":"pos-checkout-v1"}',$12,$13,$14)`, receipt.ID, checkout.TenantID, checkout.OutletID, order.ID, receipt.ReceiptNumber, receipt.Currency, receipt.SubtotalMinor, receipt.DiscountMinor, receipt.TaxMinor, receipt.ServiceChargeMinor, receipt.TotalMinor, receipt.IssuedAt, audit.ActorID, audit.OperationID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pos_checkout_records(id,tenant_id,outlet_id,order_id,menu_version_id,receipt_id,pickup_token_id,completed_at,actor_id,device_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, checkout.ID, checkout.TenantID, checkout.OutletID, order.ID, nullable(checkout.MenuVersionID), receipt.ID, nullable(checkout.PickupTokenID), now, audit.ActorID, audit.DeviceID, audit.OperationID); err != nil {
			return err
		}
		if err := insertAudit(ctx, tx, audit); err != nil {
			return err
		}
		result.Order = order
		result.Receipt = &receipt
		return nil
	})
	return result, err
}

func (r *PostgresRepository) AcknowledgeKitchenPrintJob(ctx context.Context, tenant, outlet, id, action string, audit domain.AuditEvent) (domain.KitchenPrintJob, error) {
	var value domain.KitchenPrintJob
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,ticket_id,printer_route,copy_type,payload,status,attempts,last_error,created_at,acknowledged_at FROM kitchen_print_jobs WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, tenant, outlet, id).Scan(&value.ID, &value.TenantID, &value.OutletID, &value.TicketID, &value.PrinterRoute, &value.CopyType, &raw, &value.Status, &value.Attempts, &value.LastError, &value.CreatedAt, &value.AcknowledgedAt); err != nil {
			return err
		}
		if err := unmarshalConnected(raw, &value.Payload); err != nil {
			return err
		}
		next := action
		allowed := (value.Status == "queued" && (next == "acknowledged" || next == "failed" || next == "cancelled")) || (value.Status == "failed" && next == "requeued") || (value.Status == "acknowledged" && next == "reprinted")
		if !allowed {
			return ErrInvalidTransition
		}
		if next == "requeued" || next == "reprinted" {
			value.Status = "queued"
			value.Attempts++
		} else {
			value.Status = next
		}
		if value.Status == "acknowledged" {
			now := audit.RecordedAt
			value.AcknowledgedAt = &now
		}
		if _, err := tx.Exec(ctx, `UPDATE kitchen_print_jobs SET status=$4,attempts=$5,acknowledged_at=$6 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, tenant, outlet, id, value.Status, value.Attempts, value.AcknowledgedAt); err != nil {
			return err
		}
		eventOperation := inventoryEventUUID(tenant, audit.OperationID, "print-event")
		if _, err := tx.Exec(ctx, `INSERT INTO kitchen_print_job_events(id,tenant_id,print_job_id,event_type,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7)`, audit.ID, tenant, id, action, audit.RecordedAt, audit.ActorID, eventOperation); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func (r *PostgresRepository) TransitionPickupToken(ctx context.Context, tenant, outlet, id, status string, expected uint64, audit domain.AuditEvent) (domain.PickupToken, error) {
	var value domain.PickupToken
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,order_id,token,status,issued_at,collected_at,version FROM pickup_tokens WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, tenant, outlet, id).Scan(&value.ID, &value.TenantID, &value.OutletID, &value.OrderID, &value.Token, &value.Status, &value.IssuedAt, &value.CollectedAt, &value.Version); err != nil {
			return err
		}
		if value.Version != expected {
			return ErrVersionConflict
		}
		allowed := (value.Status == "issued" && (status == "called" || status == "cancelled")) || (value.Status == "called" && (status == "collected" || status == "cancelled"))
		if !allowed {
			return ErrInvalidTransition
		}
		if status == "collected" {
			now := audit.RecordedAt
			value.CollectedAt = &now
		}
		if _, err := tx.Exec(ctx, `UPDATE pickup_tokens SET status=$4,collected_at=$5,version=version+1 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, tenant, outlet, id, status, value.CollectedAt); err != nil {
			return err
		}
		value.Status = status
		value.Version++
		eventOperation := inventoryEventUUID(tenant, audit.OperationID, "token-event")
		if _, err := tx.Exec(ctx, `INSERT INTO pickup_token_events(id,tenant_id,pickup_token_id,event_type,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7)`, audit.ID, tenant, id, status, audit.RecordedAt, audit.ActorID, eventOperation); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

var _ RestaurantCoreRepository = (*PostgresRepository)(nil)
