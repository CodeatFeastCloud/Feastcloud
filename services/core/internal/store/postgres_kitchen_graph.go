// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (repository *PostgresRepository) CreateUnit(ctx context.Context, value domain.Unit, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO units(id,tenant_id,name,symbol,dimension,base_numerator,base_denominator,active,created_at,updated_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, value.ID, value.TenantID, value.Name, value.Symbol, value.Dimension, value.BaseNumerator, value.BaseDenominator, value.Active, value.CreatedAt, value.UpdatedAt, value.Version)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (repository *PostgresRepository) Units(ctx context.Context, tenantID string) ([]domain.Unit, error) {
	values := []domain.Unit{}
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,name,symbol,dimension,base_numerator,base_denominator,active,created_at,updated_at,version FROM units WHERE tenant_id=$1 ORDER BY name,id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.Unit
			if err := rows.Scan(&v.ID, &v.TenantID, &v.Name, &v.Symbol, &v.Dimension, &v.BaseNumerator, &v.BaseDenominator, &v.Active, &v.CreatedAt, &v.UpdatedAt, &v.Version); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}

func (repository *PostgresRepository) CreateIngredient(ctx context.Context, value domain.Ingredient, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO ingredients(id,tenant_id,name,code,base_unit_id,allergens,dietary_labels,active,created_at,updated_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, value.ID, value.TenantID, value.Name, value.Code, value.BaseUnitID, value.Allergens, value.DietaryLabels, value.Active, value.CreatedAt, value.UpdatedAt, value.Version)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (repository *PostgresRepository) Ingredients(ctx context.Context, tenantID string) ([]domain.Ingredient, error) {
	values := []domain.Ingredient{}
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,name,code,base_unit_id,allergens,dietary_labels,active,created_at,updated_at,version FROM ingredients WHERE tenant_id=$1 ORDER BY name,id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.Ingredient
			if err := rows.Scan(&v.ID, &v.TenantID, &v.Name, &v.Code, &v.BaseUnitID, &v.Allergens, &v.DietaryLabels, &v.Active, &v.CreatedAt, &v.UpdatedAt, &v.Version); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}

func insertRecipeVersion(ctx context.Context, tx pgx.Tx, value domain.RecipeVersion) error {
	_, err := tx.Exec(ctx, `INSERT INTO recipe_versions(id,tenant_id,recipe_id,version_number,yield_quantity,yield_unit_id,preparation_loss_percent,cooking_loss_percent,instructions,effective_from,effective_to,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, value.ID, value.TenantID, value.RecipeID, value.VersionNumber, value.YieldQuantity, value.YieldUnitID, value.PreparationLossPercent, value.CookingLossPercent, value.Instructions, value.EffectiveFrom, value.EffectiveTo, value.CreatedAt)
	if err != nil {
		return err
	}
	for _, component := range value.Components {
		_, err = tx.Exec(ctx, `INSERT INTO recipe_components(id,tenant_id,recipe_version_id,ingredient_id,child_recipe_version_id,quantity,unit_id,preparation_note) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, component.ID, value.TenantID, value.ID, nullable(component.IngredientID), nullable(component.ChildRecipeVersionID), component.Quantity, component.UnitID, component.PreparationNote)
		if err != nil {
			return err
		}
	}
	var incompatible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM recipe_components component JOIN units source ON source.tenant_id=component.tenant_id AND source.id=component.unit_id LEFT JOIN ingredients ingredient ON ingredient.tenant_id=component.tenant_id AND ingredient.id=component.ingredient_id LEFT JOIN units ingredient_unit ON ingredient_unit.tenant_id=ingredient.tenant_id AND ingredient_unit.id=ingredient.base_unit_id LEFT JOIN recipe_versions child ON child.tenant_id=component.tenant_id AND child.id=component.child_recipe_version_id LEFT JOIN units yield_unit ON yield_unit.tenant_id=child.tenant_id AND yield_unit.id=child.yield_unit_id WHERE component.tenant_id=$1 AND component.recipe_version_id=$2 AND source.dimension<>COALESCE(ingredient_unit.dimension,yield_unit.dimension))`, value.TenantID, value.ID).Scan(&incompatible)
	if err != nil {
		return err
	}
	if incompatible {
		return fmt.Errorf("%w: recipe component unit dimension", ErrInvalidReference)
	}
	var cyclic bool
	err = tx.QueryRow(ctx, `WITH RECURSIVE dependency(id) AS (SELECT child_recipe_version_id FROM recipe_components WHERE tenant_id=$1 AND recipe_version_id=$2 AND child_recipe_version_id IS NOT NULL UNION SELECT component.child_recipe_version_id FROM recipe_components component JOIN dependency ON component.recipe_version_id=dependency.id WHERE component.tenant_id=$1 AND component.child_recipe_version_id IS NOT NULL) SELECT EXISTS(SELECT 1 FROM dependency JOIN recipe_versions version ON version.tenant_id=$1 AND version.id=dependency.id WHERE version.recipe_id=$3)`, value.TenantID, value.ID, value.RecipeID).Scan(&cyclic)
	if err != nil {
		return err
	}
	if cyclic {
		return fmt.Errorf("%w: recipe dependency cycle", ErrInvalidReference)
	}
	return nil
}

func (repository *PostgresRepository) CreateRecipe(ctx context.Context, value domain.Recipe, version domain.RecipeVersion, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO recipes(id,tenant_id,name,code,active,created_at,updated_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, value.ID, value.TenantID, value.Name, value.Code, value.Active, value.CreatedAt, value.UpdatedAt, value.Version)
		if err != nil {
			return err
		}
		if err := insertRecipeVersion(ctx, tx, version); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (repository *PostgresRepository) AddRecipeVersion(ctx context.Context, value domain.RecipeVersion, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id FROM recipes WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, value.TenantID, value.RecipeID).Scan(new(string)); err != nil {
			return err
		}
		var latest uint64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number),0) FROM recipe_versions WHERE tenant_id=$1 AND recipe_id=$2`, value.TenantID, value.RecipeID).Scan(&latest); err != nil {
			return err
		}
		if value.VersionNumber != latest+1 {
			return fmt.Errorf("%w: recipe version must be %d", ErrVersionConflict, latest+1)
		}
		tag, err := tx.Exec(ctx, `UPDATE recipe_versions SET effective_to=$3 WHERE tenant_id=$1 AND recipe_id=$2 AND effective_to IS NULL AND effective_from<$3`, value.TenantID, value.RecipeID, value.EffectiveFrom)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: current recipe version", ErrInvalidReference)
		}
		if err := insertRecipeVersion(ctx, tx, value); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE recipes SET version=version+1,updated_at=$3 WHERE tenant_id=$1 AND id=$2`, value.TenantID, value.RecipeID, value.CreatedAt)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func loadRecipeVersion(ctx context.Context, tx pgx.Tx, tenantID, recipeID string) (*domain.RecipeVersion, error) {
	var v domain.RecipeVersion
	err := tx.QueryRow(ctx, `SELECT id,tenant_id,recipe_id,version_number,yield_quantity,yield_unit_id,preparation_loss_percent,cooking_loss_percent,instructions,effective_from,effective_to,created_at FROM recipe_versions WHERE tenant_id=$1 AND recipe_id=$2 AND effective_to IS NULL`, tenantID, recipeID).Scan(&v.ID, &v.TenantID, &v.RecipeID, &v.VersionNumber, &v.YieldQuantity, &v.YieldUnitID, &v.PreparationLossPercent, &v.CookingLossPercent, &v.Instructions, &v.EffectiveFrom, &v.EffectiveTo, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,COALESCE(ingredient_id::text,''),COALESCE(child_recipe_version_id::text,''),quantity,unit_id,preparation_note FROM recipe_components WHERE tenant_id=$1 AND recipe_version_id=$2 ORDER BY id`, tenantID, v.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c domain.RecipeComponent
		if err := rows.Scan(&c.ID, &c.IngredientID, &c.ChildRecipeVersionID, &c.Quantity, &c.UnitID, &c.PreparationNote); err != nil {
			return nil, err
		}
		v.Components = append(v.Components, c)
	}
	return &v, rows.Err()
}

func (repository *PostgresRepository) Recipes(ctx context.Context, tenantID string) ([]domain.Recipe, error) {
	values := []domain.Recipe{}
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,name,code,active,created_at,updated_at,version FROM recipes WHERE tenant_id=$1 ORDER BY name,id`, tenantID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v domain.Recipe
			if err := rows.Scan(&v.ID, &v.TenantID, &v.Name, &v.Code, &v.Active, &v.CreatedAt, &v.UpdatedAt, &v.Version); err != nil {
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
			version, err := loadRecipeVersion(ctx, tx, tenantID, values[i].ID)
			if err != nil {
				return err
			}
			values[i].CurrentVersion = version
		}
		return nil
	})
	return values, err
}

func (repository *PostgresRepository) CreateMenuItem(ctx context.Context, value domain.MenuItem, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO menu_items(id,tenant_id,outlet_id,brand_id,recipe_id,name,code,price_minor,currency,station_id,active,created_at,updated_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, value.ID, value.TenantID, value.OutletID, nullable(value.BrandID), nullable(value.RecipeID), value.Name, value.Code, value.PriceMinor, value.Currency, nullable(value.StationID), value.Active, value.CreatedAt, value.UpdatedAt, value.Version)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (repository *PostgresRepository) MenuItems(ctx context.Context, tenantID, outletID string) ([]domain.MenuItem, error) {
	values := []domain.MenuItem{}
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,COALESCE(brand_id::text,''),COALESCE(recipe_id::text,''),name,code,price_minor,currency,COALESCE(station_id::text,''),active,created_at,updated_at,version FROM menu_items WHERE tenant_id=$1 AND ($2='' OR outlet_id=$2::uuid) ORDER BY name,id`, tenantID, outletID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.MenuItem
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.BrandID, &v.RecipeID, &v.Name, &v.Code, &v.PriceMinor, &v.Currency, &v.StationID, &v.Active, &v.CreatedAt, &v.UpdatedAt, &v.Version); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}

func (repository *PostgresRepository) RecordInventoryEvent(ctx context.Context, movement StockMovement, audit domain.AuditEvent) (domain.InventoryEvent, error) {
	value := movement.Event
	err := repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		var numerator, denominator int64
		var ingredientBase, dimension, unitDimension string
		err := tx.QueryRow(ctx, `SELECT source.base_numerator,source.base_denominator,ingredient.base_unit_id,source.dimension,target.dimension FROM units source JOIN ingredients ingredient ON ingredient.tenant_id=source.tenant_id AND ingredient.id=$2 JOIN units target ON target.tenant_id=ingredient.tenant_id AND target.id=ingredient.base_unit_id WHERE source.tenant_id=$1 AND source.id=$3`, value.TenantID, value.IngredientID, movement.UnitID).Scan(&numerator, &denominator, &ingredientBase, &unitDimension, &dimension)
		if err != nil {
			return err
		}
		if dimension != unitDimension {
			return fmt.Errorf("%w: incompatible unit dimension", ErrInvalidReference)
		}
		value.QuantityBase = movement.Quantity * float64(numerator) / float64(denominator)
		if value.EventType == "reversal" {
			var originalQuantity float64
			var originalCost int64
			var originalOutlet, originalIngredient, originalCurrency string
			if err := tx.QueryRow(ctx, `SELECT outlet_id,ingredient_id,quantity_base,total_cost_minor,currency FROM inventory_events WHERE tenant_id=$1 AND id=$2`, value.TenantID, value.ReversesEventID).Scan(&originalOutlet, &originalIngredient, &originalQuantity, &originalCost, &originalCurrency); err != nil {
				return err
			}
			if originalOutlet != value.OutletID || originalIngredient != value.IngredientID || originalCurrency != value.Currency {
				return fmt.Errorf("%w: reversal scope differs from original event", ErrInvalidReference)
			}
			value.QuantityBase = -originalQuantity
			value.TotalCostMinor = -originalCost
		}
		outbound := map[string]bool{"consumption": true, "waste": true, "spoilage": true, "transfer_out": true, "staff_meal": true}[value.EventType]
		if outbound && value.QuantityBase > 0 {
			value.QuantityBase = -value.QuantityBase
		}
		if !outbound && value.EventType != "count_adjustment" && value.EventType != "reversal" && value.QuantityBase < 0 {
			return fmt.Errorf("%w: event quantity sign", ErrInvalidReference)
		}
		if value.EventType != "reversal" && (outbound || value.EventType == "count_adjustment") {
			var quantity float64
			var cost int64
			if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(quantity_base),0),COALESCE(SUM(total_cost_minor),0) FROM inventory_events WHERE tenant_id=$1 AND outlet_id=$2 AND ingredient_id=$3`, value.TenantID, value.OutletID, value.IngredientID).Scan(&quantity, &cost); err != nil {
				return err
			}
			if quantity > 0 {
				value.TotalCostMinor = int64(math.Round(value.QuantityBase * float64(cost) / quantity))
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO inventory_events(id,tenant_id,outlet_id,ingredient_id,event_type,quantity_base,total_cost_minor,currency,reference_type,reference_id,lot_code,expires_at,reason,occurred_at,recorded_at,actor_id,device_id,operation_id,reverses_event_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, value.ID, value.TenantID, value.OutletID, value.IngredientID, value.EventType, value.QuantityBase, value.TotalCostMinor, value.Currency, value.ReferenceType, value.ReferenceID, value.LotCode, value.ExpiresAt, value.Reason, value.OccurredAt, value.RecordedAt, value.ActorID, value.DeviceID, value.OperationID, nullable(value.ReversesEventID))
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func (repository *PostgresRepository) RecordInventoryCount(ctx context.Context, value domain.InventoryCount, requested []StockCountLine, audit domain.AuditEvent) (domain.InventoryCount, error) {
	err := repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO inventory_counts(id,tenant_id,outlet_id,notes,counted_at,recorded_at,actor_id,device_id,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, value.ID, value.TenantID, value.OutletID, value.Notes, value.CountedAt, value.RecordedAt, value.ActorID, value.DeviceID, value.OperationID)
		if err != nil {
			return err
		}
		value.Lines = make([]domain.InventoryCountLine, 0, len(requested))
		for _, input := range requested {
			// Serialize the balance read and adjustment for this outlet ingredient.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, value.TenantID+":"+value.OutletID+":"+input.IngredientID); err != nil {
				return err
			}
			var numerator, denominator int64
			var sourceDimension, baseDimension string
			if err := tx.QueryRow(ctx, `SELECT source.base_numerator,source.base_denominator,source.dimension,target.dimension FROM units source JOIN ingredients ingredient ON ingredient.tenant_id=source.tenant_id AND ingredient.id=$2 JOIN units target ON target.tenant_id=ingredient.tenant_id AND target.id=ingredient.base_unit_id WHERE source.tenant_id=$1 AND source.id=$3`, value.TenantID, input.IngredientID, input.UnitID).Scan(&numerator, &denominator, &sourceDimension, &baseDimension); err != nil {
				return err
			}
			if sourceDimension != baseDimension {
				return fmt.Errorf("%w: incompatible count unit dimension", ErrInvalidReference)
			}
			var expected float64
			var currentCost int64
			var currency string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(event.quantity_base),0),COALESCE(SUM(event.total_cost_minor),0),COALESCE(MAX(event.currency),outlet.currency) FROM outlets outlet LEFT JOIN inventory_events event ON event.tenant_id=outlet.tenant_id AND event.outlet_id=outlet.id AND event.ingredient_id=$3 WHERE outlet.tenant_id=$1 AND outlet.id=$2 GROUP BY outlet.currency`, value.TenantID, value.OutletID, input.IngredientID).Scan(&expected, &currentCost, &currency); err != nil {
				return err
			}
			countedBase := input.CountedQuantity * float64(numerator) / float64(denominator)
			variance := countedBase - expected
			varianceCost := int64(0)
			if expected > 0 {
				varianceCost = int64(math.Round(variance * float64(currentCost) / expected))
			}
			line := domain.InventoryCountLine{ID: input.ID, IngredientID: input.IngredientID, UnitID: input.UnitID, CountedQuantity: input.CountedQuantity, CountedQuantityBase: countedBase, ExpectedQuantityBase: expected, VarianceQuantityBase: variance, VarianceCostMinor: varianceCost}
			_, err = tx.Exec(ctx, `INSERT INTO inventory_count_lines(id,tenant_id,count_id,ingredient_id,unit_id,counted_quantity,counted_quantity_base,expected_quantity_base,variance_quantity_base,variance_cost_minor) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, line.ID, value.TenantID, value.ID, line.IngredientID, line.UnitID, line.CountedQuantity, line.CountedQuantityBase, line.ExpectedQuantityBase, line.VarianceQuantityBase, line.VarianceCostMinor)
			if err != nil {
				return err
			}
			if math.Abs(variance) > 0.0000005 {
				_, err = tx.Exec(ctx, `INSERT INTO inventory_events(id,tenant_id,outlet_id,ingredient_id,event_type,quantity_base,total_cost_minor,currency,reference_type,reference_id,reason,occurred_at,recorded_at,actor_id,device_id,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'inventory_count',$9,$10,$11,$12,$13,$14,$15)`, line.ID, value.TenantID, value.OutletID, line.IngredientID, "count_adjustment", variance, varianceCost, currency, value.ID, value.Notes, value.CountedAt, value.RecordedAt, value.ActorID, value.DeviceID, value.OperationID)
				if err != nil {
					return err
				}
			}
			value.Lines = append(value.Lines, line)
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func (repository *PostgresRepository) InventorySummary(ctx context.Context, tenantID, outletID string) ([]domain.InventorySummary, error) {
	values := []domain.InventorySummary{}
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT ingredient.id,ingredient.base_unit_id,ingredient.name,unit.symbol,COALESCE(MAX(event.currency),'INR'),COALESCE(SUM(event.quantity_base),0),COALESCE(SUM(event.quantity_base) FILTER(WHERE event.event_type IN('receipt','transfer_in')),0),COALESCE(-SUM(event.quantity_base) FILTER(WHERE event.event_type='consumption'),0),COALESCE(-SUM(event.quantity_base) FILTER(WHERE event.event_type IN('waste','spoilage')),0),COALESCE(SUM(event.quantity_base) FILTER(WHERE event.event_type='count_adjustment'),0),COALESCE(SUM(event.total_cost_minor),0),COALESCE(-SUM(event.total_cost_minor) FILTER(WHERE event.event_type IN('waste','spoilage')),0),COALESCE(-SUM(event.total_cost_minor) FILTER(WHERE event.event_type='consumption'),0),COALESCE(SUM(event.total_cost_minor) FILTER(WHERE event.event_type='count_adjustment'),0) FROM ingredients ingredient JOIN units unit ON unit.tenant_id=ingredient.tenant_id AND unit.id=ingredient.base_unit_id LEFT JOIN inventory_events event ON event.tenant_id=ingredient.tenant_id AND event.ingredient_id=ingredient.id AND event.outlet_id=$2 WHERE ingredient.tenant_id=$1 GROUP BY ingredient.id,ingredient.base_unit_id,ingredient.name,unit.symbol ORDER BY ingredient.name`, tenantID, outletID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.InventorySummary
			if err := rows.Scan(&v.IngredientID, &v.BaseUnitID, &v.IngredientName, &v.UnitSymbol, &v.Currency, &v.QuantityBase, &v.ReceivedQuantity, &v.ConsumedQuantity, &v.WasteQuantity, &v.CountVarianceQuantity, &v.StockValueMinor, &v.WasteValueMinor, &v.TheoreticalCostMinor, &v.CountVarianceValueMinor); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}

var _ KitchenGraphRepository = (*PostgresRepository)(nil)
