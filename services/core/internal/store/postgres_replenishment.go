// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"math"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) SetReplenishmentRule(ctx context.Context, tenant, outlet string, value domain.ReplenishmentRule, audit domain.AuditEvent) (domain.ReplenishmentRule, error) {
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO replenishment_rules(tenant_id,outlet_id,ingredient_id,source_outlet_id,reorder_point_base,target_level_base,active,updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(tenant_id,outlet_id,ingredient_id) DO UPDATE SET source_outlet_id=EXCLUDED.source_outlet_id,reorder_point_base=EXCLUDED.reorder_point_base,target_level_base=EXCLUDED.target_level_base,active=EXCLUDED.active,updated_at=EXCLUDED.updated_at,version=replenishment_rules.version+1`, tenant, outlet, value.IngredientID, value.SourceOutletID, value.ReorderPointBase, value.TargetLevelBase, value.Active, audit.RecordedAt); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT outlet_id,ingredient_id,source_outlet_id,reorder_point_base,target_level_base,active,version,updated_at FROM replenishment_rules WHERE tenant_id=$1 AND outlet_id=$2 AND ingredient_id=$3`, tenant, outlet, value.IngredientID).Scan(&value.OutletID, &value.IngredientID, &value.SourceOutletID, &value.ReorderPointBase, &value.TargetLevelBase, &value.Active, &value.Version, &value.UpdatedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func (r *PostgresRepository) ReplenishmentRules(ctx context.Context, tenant, outlet string) ([]domain.ReplenishmentRule, error) {
	values := []domain.ReplenishmentRule{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT outlet_id,ingredient_id,source_outlet_id,reorder_point_base,target_level_base,active,version,updated_at FROM replenishment_rules WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY updated_at DESC,ingredient_id`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.ReplenishmentRule
			if err := rows.Scan(&value.OutletID, &value.IngredientID, &value.SourceOutletID, &value.ReorderPointBase, &value.TargetLevelBase, &value.Active, &value.Version, &value.UpdatedAt); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) ReplenishmentSuggestions(ctx context.Context, tenant, outlet string) ([]domain.ReplenishmentSuggestion, error) {
	values := []domain.ReplenishmentSuggestion{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT rule.outlet_id,rule.ingredient_id,ingredient.name,unit.symbol,rule.source_outlet_id,COALESCE(destination.balance,0),rule.reorder_point_base,rule.target_level_base,COALESCE(source.balance,0) FROM replenishment_rules rule JOIN ingredients ingredient ON ingredient.tenant_id=rule.tenant_id AND ingredient.id=rule.ingredient_id JOIN units unit ON unit.tenant_id=ingredient.tenant_id AND unit.id=ingredient.base_unit_id LEFT JOIN LATERAL (SELECT SUM(quantity_base) balance FROM inventory_events WHERE tenant_id=rule.tenant_id AND outlet_id=rule.outlet_id AND ingredient_id=rule.ingredient_id) destination ON true LEFT JOIN LATERAL (SELECT SUM(quantity_base) balance FROM inventory_events WHERE tenant_id=rule.tenant_id AND outlet_id=rule.source_outlet_id AND ingredient_id=rule.ingredient_id) source ON true WHERE rule.tenant_id=$1 AND rule.outlet_id=$2 AND rule.active=true ORDER BY ingredient.name,rule.ingredient_id`, tenant, outlet)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.ReplenishmentSuggestion
			if err := rows.Scan(&value.OutletID, &value.IngredientID, &value.IngredientName, &value.UnitSymbol, &value.SourceOutletID, &value.OnHandBase, &value.ReorderPointBase, &value.TargetLevelBase, &value.SourceAvailableBase); err != nil {
				return err
			}
			if value.OnHandBase > value.ReorderPointBase {
				continue
			}
			need := value.TargetLevelBase - value.OnHandBase
			value.SuggestedQuantityBase = math.Max(0, math.Min(need, value.SourceAvailableBase))
			if value.SuggestedQuantityBase == 0 {
				value.Status = "source_empty"
			} else if value.SuggestedQuantityBase < need {
				value.Status = "source_short"
			} else {
				value.Status = "ready"
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

var _ ConnectedCommerceRepository = (*PostgresRepository)(nil)
