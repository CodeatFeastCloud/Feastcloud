// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

const productionBatchSelect = `SELECT batch.id,batch.tenant_id,batch.outlet_id,COALESCE(batch.station_id::text,''),batch.recipe_version_id,recipe.name,batch.output_ingredient_id,ingredient.name,batch.output_unit_id,unit.symbol,batch.status,batch.planned_quantity,batch.actual_quantity,batch.planned_for,batch.started_at,batch.completed_at,batch.expires_at,batch.lot_code,batch.notes,batch.version,batch.created_at,batch.updated_at FROM production_batches batch JOIN recipe_versions version ON version.tenant_id=batch.tenant_id AND version.id=batch.recipe_version_id JOIN recipes recipe ON recipe.tenant_id=version.tenant_id AND recipe.id=version.recipe_id JOIN ingredients ingredient ON ingredient.tenant_id=batch.tenant_id AND ingredient.id=batch.output_ingredient_id JOIN units unit ON unit.tenant_id=batch.tenant_id AND unit.id=batch.output_unit_id`

func scanProductionBatch(row pgx.Row, value *domain.ProductionBatch) error {
	return row.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.StationID, &value.RecipeVersionID, &value.RecipeName, &value.OutputIngredientID, &value.OutputIngredient, &value.OutputUnitID, &value.OutputUnitSymbol, &value.Status, &value.PlannedQuantity, &value.ActualQuantity, &value.PlannedFor, &value.StartedAt, &value.CompletedAt, &value.ExpiresAt, &value.LotCode, &value.Notes, &value.Version, &value.CreatedAt, &value.UpdatedAt)
}

func (repository *PostgresRepository) CreateProductionBatch(ctx context.Context, value domain.ProductionBatch, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		var compatible bool
		if err := tx.QueryRow(ctx, `SELECT output_unit.dimension=base_unit.dimension FROM recipe_versions version JOIN units output_unit ON output_unit.tenant_id=version.tenant_id AND output_unit.id=$3 JOIN ingredients ingredient ON ingredient.tenant_id=version.tenant_id AND ingredient.id=$4 JOIN units base_unit ON base_unit.tenant_id=ingredient.tenant_id AND base_unit.id=ingredient.base_unit_id WHERE version.tenant_id=$1 AND version.id=$2`, value.TenantID, value.RecipeVersionID, value.OutputUnitID, value.OutputIngredientID).Scan(&compatible); err != nil {
			return err
		}
		if !compatible {
			return fmt.Errorf("%w: production output unit dimension", ErrInvalidReference)
		}
		var outputConsumed bool
		if err := tx.QueryRow(ctx, `WITH RECURSIVE dependency(id) AS (SELECT $2::uuid UNION SELECT component.child_recipe_version_id FROM recipe_components component JOIN dependency ON component.recipe_version_id=dependency.id WHERE component.tenant_id=$1 AND component.child_recipe_version_id IS NOT NULL) SELECT EXISTS(SELECT 1 FROM dependency JOIN recipe_components component ON component.tenant_id=$1 AND component.recipe_version_id=dependency.id WHERE component.ingredient_id=$3)`, value.TenantID, value.RecipeVersionID, value.OutputIngredientID).Scan(&outputConsumed); err != nil {
			return err
		}
		if outputConsumed {
			return fmt.Errorf("%w: production output cannot also be a consumed ingredient", ErrInvalidReference)
		}
		_, err := tx.Exec(ctx, `INSERT INTO production_batches(id,tenant_id,outlet_id,station_id,recipe_version_id,output_ingredient_id,output_unit_id,status,planned_quantity,planned_for,lot_code,notes,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, value.ID, value.TenantID, value.OutletID, nullable(value.StationID), value.RecipeVersionID, value.OutputIngredientID, value.OutputUnitID, value.Status, value.PlannedQuantity, value.PlannedFor, value.LotCode, value.Notes, value.Version, value.CreatedAt, value.UpdatedAt)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (repository *PostgresRepository) ProductionBatches(ctx context.Context, tenantID, outletID string) ([]domain.ProductionBatch, error) {
	values := []domain.ProductionBatch{}
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, productionBatchSelect+` WHERE batch.tenant_id=$1 AND batch.outlet_id=$2 ORDER BY CASE batch.status WHEN 'in_progress' THEN 0 WHEN 'planned' THEN 1 ELSE 2 END,batch.planned_for,batch.id`, tenantID, outletID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.ProductionBatch
			if err := scanProductionBatch(rows, &value); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func consumeProductionRecipe(ctx context.Context, tx pgx.Tx, batch domain.ProductionBatch, audit domain.AuditEvent) (int64, string, error) {
	rows, err := tx.Query(ctx, `WITH RECURSIVE recipe_use(version_id,multiplier) AS (
		SELECT version.id,$3::numeric/version.yield_quantity FROM recipe_versions version WHERE version.tenant_id=$1 AND version.id=$2
		UNION ALL
		SELECT component.child_recipe_version_id,recipe_use.multiplier * component.quantity * source.base_numerator::numeric/source.base_denominator / (child.yield_quantity * target.base_numerator::numeric/target.base_denominator)
		FROM recipe_use JOIN recipe_components component ON component.tenant_id=$1 AND component.recipe_version_id=recipe_use.version_id AND component.child_recipe_version_id IS NOT NULL
		JOIN units source ON source.tenant_id=$1 AND source.id=component.unit_id JOIN recipe_versions child ON child.tenant_id=$1 AND child.id=component.child_recipe_version_id JOIN units target ON target.tenant_id=$1 AND target.id=child.yield_unit_id
	), consumed AS (
		SELECT component.ingredient_id,SUM(recipe_use.multiplier * component.quantity * source.base_numerator::numeric/source.base_denominator) quantity
		FROM recipe_use JOIN recipe_components component ON component.tenant_id=$1 AND component.recipe_version_id=recipe_use.version_id AND component.ingredient_id IS NOT NULL JOIN units source ON source.tenant_id=$1 AND source.id=component.unit_id GROUP BY component.ingredient_id
	) SELECT ingredient_id,quantity FROM consumed`, batch.TenantID, batch.RecipeVersionID, batch.PlannedQuantity)
	if err != nil {
		return 0, "", err
	}
	type consumed struct {
		ingredient string
		quantity   float64
	}
	items := []consumed{}
	for rows.Next() {
		var item consumed
		if err := rows.Scan(&item.ingredient, &item.quantity); err != nil {
			rows.Close()
			return 0, "", err
		}
		if item.ingredient == batch.OutputIngredientID {
			rows.Close()
			return 0, "", fmt.Errorf("%w: production output cannot also be a consumed ingredient", ErrInvalidReference)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, "", err
	}
	rows.Close()
	var currency string
	if err := tx.QueryRow(ctx, `SELECT currency FROM outlets WHERE tenant_id=$1 AND id=$2`, batch.TenantID, batch.OutletID).Scan(&currency); err != nil {
		return 0, "", err
	}
	var totalCost int64
	for _, item := range items {
		var stock float64
		var cost int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(quantity_base),0),COALESCE(SUM(total_cost_minor),0) FROM inventory_events WHERE tenant_id=$1 AND outlet_id=$2 AND ingredient_id=$3`, batch.TenantID, batch.OutletID, item.ingredient).Scan(&stock, &cost); err != nil {
			return 0, "", err
		}
		eventCost := int64(0)
		if stock > 0 {
			eventCost = -int64(math.Round(item.quantity * float64(cost) / stock))
		}
		totalCost -= eventCost
		eventID := inventoryEventUUID(batch.TenantID, audit.OperationID, item.ingredient)
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_events(id,tenant_id,outlet_id,ingredient_id,event_type,quantity_base,total_cost_minor,currency,reference_type,reference_id,reason,occurred_at,recorded_at,actor_id,device_id,operation_id) VALUES($1,$2,$3,$4,'consumption',$5,$6,$7,'production_batch',$8,'production recipe consumption',$9,$10,$11,$12,$13)`, eventID, batch.TenantID, batch.OutletID, item.ingredient, -item.quantity, eventCost, currency, batch.ID, audit.OccurredAt, audit.RecordedAt, audit.ActorID, audit.DeviceID, audit.OperationID); err != nil {
			return 0, "", err
		}
	}
	return totalCost, currency, nil
}

func (repository *PostgresRepository) TransitionProductionBatch(ctx context.Context, tenantID, outletID, id string, to domain.ProductionBatchStatus, expectedVersion uint64, actualQuantity *float64, expiresAt *time.Time, lotCode, notes string, audit domain.AuditEvent) (domain.ProductionBatch, error) {
	var value domain.ProductionBatch
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		if err := scanProductionBatch(tx.QueryRow(ctx, productionBatchSelect+` WHERE batch.tenant_id=$1 AND batch.outlet_id=$2 AND batch.id=$3 FOR UPDATE OF batch`, tenantID, outletID, id), &value); err != nil {
			return err
		}
		if value.Version != expectedVersion {
			return fmt.Errorf("%w: production batch %q is version %d", ErrVersionConflict, id, value.Version)
		}
		if !domain.CanTransitionProductionBatchStatus(value.Status, to) {
			return fmt.Errorf("%w: production batch %s to %s", ErrInvalidTransition, value.Status, to)
		}
		now := audit.RecordedAt
		if to == domain.ProductionBatchCompleted {
			if actualQuantity == nil || *actualQuantity <= 0 {
				return fmt.Errorf("%w: actual yield is required", ErrInvalidReference)
			}
			if expiresAt != nil && !expiresAt.After(now) {
				return fmt.Errorf("%w: expiry must be after completion", ErrInvalidReference)
			}
			value.ActualQuantity, value.CompletedAt, value.ExpiresAt = actualQuantity, &now, expiresAt
			value.LotCode, value.Notes = lotCode, notes
			totalCost, currency, err := consumeProductionRecipe(ctx, tx, value, audit)
			if err != nil {
				return err
			}
			var numerator, denominator int64
			if err := tx.QueryRow(ctx, `SELECT base_numerator,base_denominator FROM units WHERE tenant_id=$1 AND id=$2`, tenantID, value.OutputUnitID).Scan(&numerator, &denominator); err != nil {
				return err
			}
			quantityBase := *actualQuantity * float64(numerator) / float64(denominator)
			eventID := inventoryEventUUID(tenantID, audit.OperationID, "output:"+value.OutputIngredientID)
			if _, err := tx.Exec(ctx, `INSERT INTO inventory_events(id,tenant_id,outlet_id,ingredient_id,event_type,quantity_base,total_cost_minor,currency,reference_type,reference_id,lot_code,expires_at,reason,occurred_at,recorded_at,actor_id,device_id,operation_id) VALUES($1,$2,$3,$4,'production',$5,$6,$7,'production_batch',$8,$9,$10,'completed preparation batch',$11,$12,$13,$14,$15)`, eventID, tenantID, outletID, value.OutputIngredientID, quantityBase, totalCost, currency, value.ID, lotCode, expiresAt, audit.OccurredAt, audit.RecordedAt, audit.ActorID, audit.DeviceID, audit.OperationID); err != nil {
				return err
			}
		}
		if to == domain.ProductionBatchInProgress {
			value.StartedAt = &now
		}
		value.Status, value.Version, value.UpdatedAt = to, value.Version+1, now
		_, err := tx.Exec(ctx, `UPDATE production_batches SET status=$4,actual_quantity=$5,started_at=$6,completed_at=$7,expires_at=$8,lot_code=$9,notes=$10,version=version+1,updated_at=$11 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND version=$12`, tenantID, outletID, id, to, value.ActualQuantity, value.StartedAt, value.CompletedAt, value.ExpiresAt, value.LotCode, value.Notes, now, expectedVersion)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

var _ ProductionRepository = (*PostgresRepository)(nil)
