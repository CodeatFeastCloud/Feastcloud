// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (repository *PostgresRepository) ImportOrders(ctx context.Context, value domain.OrderImport, inputs []ImportedOrderRow, audit domain.AuditEvent) (domain.OrderImport, error) {
	err := repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		value.Rows = make([]domain.OrderImportRow, 0, len(inputs))
		for _, input := range inputs {
			result := domain.OrderImportRow{ID: input.ID, RowNumber: input.RowNumber, ExternalRef: input.ExternalRef, Status: "rejected", RawData: input.RawData}
			if input.ErrorCode != "" {
				result.ErrorCode, result.ErrorMessage = input.ErrorCode, input.ErrorMessage
				value.Rows = append(value.Rows, result)
				continue
			}
			var existing bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE tenant_id=$1 AND source='csv_import' AND source_id=$2)`, value.TenantID, input.ExternalRef).Scan(&existing); err != nil {
				return err
			}
			if existing {
				result.ErrorCode, result.ErrorMessage = "duplicate_external_ref", "This external order reference was already imported."
				value.Rows = append(value.Rows, result)
				continue
			}
			var menuID, menuName, currency, stationID, recipeVersionID string
			var price int64
			err := tx.QueryRow(ctx, `SELECT item.id,item.name,item.currency,item.price_minor,COALESCE(item.station_id::text,''),version.id FROM menu_items item JOIN recipe_versions version ON version.tenant_id=item.tenant_id AND version.recipe_id=item.recipe_id AND version.effective_to IS NULL WHERE item.tenant_id=$1 AND item.outlet_id=$2 AND lower(item.code)=lower($3) AND item.active`, value.TenantID, value.OutletID, input.ItemCode).Scan(&menuID, &menuName, &currency, &price, &stationID, &recipeVersionID)
			if err != nil {
				if err == pgx.ErrNoRows {
					result.ErrorCode, result.ErrorMessage = "menu_item_not_found", "No active outlet menu item matches this itemCode."
					value.Rows = append(value.Rows, result)
					continue
				}
				return err
			}
			if stationID == "" {
				result.ErrorCode, result.ErrorMessage = "station_not_configured", "The menu item has no kitchen station."
				value.Rows = append(value.Rows, result)
				continue
			}
			orderID := inventoryEventUUID(value.TenantID, value.ID, "order:"+input.ExternalRef)
			lineID := inventoryEventUUID(value.TenantID, value.ID, "line:"+input.ExternalRef)
			ticketID := inventoryEventUUID(value.TenantID, value.ID, "ticket:"+input.ExternalRef)
			total := price * int64(input.Quantity)
			_, err = tx.Exec(ctx, `INSERT INTO orders(id,tenant_id,outlet_id,external_ref,source,source_id,order_type,status,currency,subtotal_minor,discount_total_minor,tax_total_minor,service_charge_minor,total_minor,placed_at,version,created_at,updated_at) VALUES($1,$2,$3,$4,'csv_import',$4,$5,'received',$6,$7,0,0,0,$7,$8,1,$9,$9)`, orderID, value.TenantID, value.OutletID, input.ExternalRef, input.OrderType, currency, total, input.PlacedAt, value.ImportedAt)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO order_lines(id,tenant_id,order_id,menu_item_id,name,quantity,currency,unit_price_minor,line_total_minor,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, lineID, value.TenantID, orderID, menuID, menuName, input.Quantity, currency, price, total, value.ImportedAt); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO order_line_recipe_snapshots(tenant_id,order_id,order_line_id,menu_item_id,recipe_version_id,quantity,captured_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, value.TenantID, orderID, lineID, menuID, recipeVersionID, input.Quantity, value.ImportedAt); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO kitchen_tickets(id,tenant_id,outlet_id,order_id,station_id,status,priority,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'queued',0,1,$6,$6)`, ticketID, value.TenantID, value.OutletID, orderID, stationID, value.ImportedAt); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO ticket_lines(tenant_id,ticket_id,order_id,order_line_id,created_at) VALUES($1,$2,$3,$4,$5)`, value.TenantID, ticketID, orderID, lineID, value.ImportedAt); err != nil {
				return err
			}
			result.Status, result.OrderID = "accepted", orderID
			value.Rows = append(value.Rows, result)
		}
		for _, row := range value.Rows {
			if row.Status == "accepted" {
				value.AcceptedRows++
			} else {
				value.RejectedRows++
			}
		}
		value.TotalRows = len(value.Rows)
		switch {
		case value.AcceptedRows == 0:
			value.Status = "rejected"
		case value.RejectedRows > 0:
			value.Status = "completed_with_errors"
		default:
			value.Status = "completed"
		}
		_, err := tx.Exec(ctx, `INSERT INTO order_imports(id,tenant_id,outlet_id,file_name,file_sha256,total_rows,accepted_rows,rejected_rows,status,imported_at,actor_id,device_id,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, value.ID, value.TenantID, value.OutletID, value.FileName, value.FileSHA256, value.TotalRows, value.AcceptedRows, value.RejectedRows, value.Status, value.ImportedAt, audit.ActorID, audit.DeviceID, audit.OperationID)
		if err != nil {
			return err
		}
		for _, row := range value.Rows {
			raw, err := json.Marshal(row.RawData)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO order_import_rows(id,tenant_id,import_id,row_number,external_ref,status,error_code,error_message,order_id,raw_data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, row.ID, value.TenantID, value.ID, row.RowNumber, row.ExternalRef, row.Status, row.ErrorCode, row.ErrorMessage, nullable(row.OrderID), raw); err != nil {
				return err
			}
			if row.Status == "rejected" {
				caseID := inventoryEventUUID(value.TenantID, value.ID, "import-reconciliation:"+row.ID)
				if _, err := tx.Exec(ctx, `INSERT INTO reconciliation_cases(id,tenant_id,outlet_id,source_type,source_id,category,severity,status,title,details,opened_at,updated_at) VALUES($1,$2,$3,'import',$4,$5,'medium','open','Imported order row requires review',jsonb_build_object('importId',$6,'rowNumber',$7,'externalRef',$8,'message',$9),$10,$10) ON CONFLICT(tenant_id,source_type,source_id) DO NOTHING`, caseID, value.TenantID, value.OutletID, row.ID, row.ErrorCode, value.ID, row.RowNumber, row.ExternalRef, row.ErrorMessage, value.ImportedAt); err != nil {
					return err
				}
			}
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func loadImportRows(ctx context.Context, tx pgx.Tx, value *domain.OrderImport) error {
	rows, err := tx.Query(ctx, `SELECT id,row_number,external_ref,status,error_code,error_message,COALESCE(order_id::text,''),raw_data FROM order_import_rows WHERE tenant_id=$1 AND import_id=$2 ORDER BY row_number`, value.TenantID, value.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row domain.OrderImportRow
		var raw []byte
		if err := rows.Scan(&row.ID, &row.RowNumber, &row.ExternalRef, &row.Status, &row.ErrorCode, &row.ErrorMessage, &row.OrderID, &raw); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &row.RawData); err != nil {
			return err
		}
		value.Rows = append(value.Rows, row)
	}
	return rows.Err()
}

func (repository *PostgresRepository) OrderImports(ctx context.Context, tenantID, outletID string) ([]domain.OrderImport, error) {
	values := []domain.OrderImport{}
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,file_name,file_sha256,total_rows,accepted_rows,rejected_rows,status,imported_at FROM order_imports WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY imported_at DESC,id`, tenantID, outletID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v domain.OrderImport
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.FileName, &v.FileSHA256, &v.TotalRows, &v.AcceptedRows, &v.RejectedRows, &v.Status, &v.ImportedAt); err != nil {
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
			if err := loadImportRows(ctx, tx, &values[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return values, err
}

func insertRecommendation(ctx context.Context, tx pgx.Tx, tenantID, runID string, value *domain.PlanningRecommendation) error {
	value.ID = inventoryEventUUID(tenantID, runID, value.Type+":"+value.MenuItemID+":"+value.IngredientID)
	evidence, err := json.Marshal(value.Evidence)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO planning_recommendations(id,tenant_id,run_id,recommendation_type,menu_item_id,recipe_version_id,ingredient_id,forecast_quantity,required_quantity_base,available_quantity_base,confidence,explanation,evidence) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, value.ID, tenantID, runID, value.Type, nullable(value.MenuItemID), nullable(value.RecipeVersionID), nullable(value.IngredientID), value.ForecastQuantity, value.RequiredQuantityBase, value.AvailableQuantityBase, value.Confidence, value.Explanation, evidence)
	return err
}

func (repository *PostgresRepository) GeneratePlanningRun(ctx context.Context, value domain.PlanningRun, audit domain.AuditEvent) (domain.PlanningRun, error) {
	err := repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO planning_runs(id,tenant_id,outlet_id,horizon_start,horizon_end,model_version,status,evidence_from,evidence_to,generated_at,actor_id,operation_id) VALUES($1,$2,$3,$4,$5,$6,'observed',$7,$8,$9,$10,$11)`, value.ID, value.TenantID, value.OutletID, value.HorizonStart, value.HorizonEnd, value.ModelVersion, value.EvidenceFrom, value.EvidenceTo, value.GeneratedAt, audit.ActorID, audit.OperationID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT item.id,item.name,version.id,COALESCE(SUM(line.quantity) FILTER(WHERE orders.id IS NOT NULL),0)::numeric/28,COUNT(DISTINCT orders.id) FROM menu_items item JOIN recipe_versions version ON version.tenant_id=item.tenant_id AND version.recipe_id=item.recipe_id AND version.effective_to IS NULL LEFT JOIN order_lines line ON line.tenant_id=item.tenant_id AND line.menu_item_id=item.id LEFT JOIN orders ON orders.tenant_id=line.tenant_id AND orders.id=line.order_id AND orders.outlet_id=$2 AND orders.placed_at>=$3 AND orders.placed_at<$4 AND orders.status<>'cancelled' WHERE item.tenant_id=$1 AND item.outlet_id=$2 AND item.active GROUP BY item.id,item.name,version.id HAVING COALESCE(SUM(line.quantity) FILTER(WHERE orders.id IS NOT NULL),0)>0 ORDER BY item.name`, value.TenantID, value.OutletID, value.EvidenceFrom, value.EvidenceTo)
		if err != nil {
			return err
		}
		type demand struct {
			id, name, version string
			forecast          float64
			orders            int
		}
		demands := []demand{}
		for rows.Next() {
			var d demand
			if err := rows.Scan(&d.id, &d.name, &d.version, &d.forecast, &d.orders); err != nil {
				rows.Close()
				return err
			}
			demands = append(demands, d)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, d := range demands {
			confidence := math.Min(.9, .4+float64(d.orders)/100)
			evidence := map[string]any{"lookbackDays": 28, "orders": d.orders, "method": "trailing_daily_average"}
			forecast := domain.PlanningRecommendation{Type: "demand_forecast", MenuItemID: d.id, MenuItemName: d.name, RecipeVersionID: d.version, ForecastQuantity: d.forecast, Confidence: confidence, Explanation: fmt.Sprintf("Forecast %.1f portions from %d orders in the last 28 days.", d.forecast, d.orders), Evidence: evidence}
			if err := insertRecommendation(ctx, tx, value.TenantID, value.ID, &forecast); err != nil {
				return err
			}
			value.Recommendations = append(value.Recommendations, forecast)
			prep := forecast
			prep.ID = ""
			prep.Type = "prep_suggestion"
			prep.Explanation = fmt.Sprintf("Prepare %.1f portions using the currently effective recipe; observe mode will not create a batch.", d.forecast)
			if err := insertRecommendation(ctx, tx, value.TenantID, value.ID, &prep); err != nil {
				return err
			}
			value.Recommendations = append(value.Recommendations, prep)
		}
		stockRows, err := tx.Query(ctx, `WITH RECURSIVE demand AS (SELECT item.id menu_item_id,version.id version_id,COALESCE(SUM(line.quantity) FILTER(WHERE orders.id IS NOT NULL),0)::numeric/28 forecast FROM menu_items item JOIN recipe_versions version ON version.tenant_id=item.tenant_id AND version.recipe_id=item.recipe_id AND version.effective_to IS NULL LEFT JOIN order_lines line ON line.tenant_id=item.tenant_id AND line.menu_item_id=item.id LEFT JOIN orders ON orders.tenant_id=line.tenant_id AND orders.id=line.order_id AND orders.outlet_id=$2 AND orders.placed_at>=$3 AND orders.placed_at<$4 AND orders.status<>'cancelled' WHERE item.tenant_id=$1 AND item.outlet_id=$2 AND item.active GROUP BY item.id,version.id), recipe_use(version_id,multiplier) AS (SELECT version_id,forecast/version.yield_quantity FROM demand JOIN recipe_versions version ON version.tenant_id=$1 AND version.id=demand.version_id WHERE forecast>0 UNION ALL SELECT component.child_recipe_version_id,recipe_use.multiplier*component.quantity*source.base_numerator::numeric/source.base_denominator/(child.yield_quantity*target.base_numerator::numeric/target.base_denominator) FROM recipe_use JOIN recipe_components component ON component.tenant_id=$1 AND component.recipe_version_id=recipe_use.version_id AND component.child_recipe_version_id IS NOT NULL JOIN units source ON source.tenant_id=$1 AND source.id=component.unit_id JOIN recipe_versions child ON child.tenant_id=$1 AND child.id=component.child_recipe_version_id JOIN units target ON target.tenant_id=$1 AND target.id=child.yield_unit_id), required AS (SELECT component.ingredient_id,SUM(recipe_use.multiplier*component.quantity*unit.base_numerator::numeric/unit.base_denominator) quantity FROM recipe_use JOIN recipe_components component ON component.tenant_id=$1 AND component.recipe_version_id=recipe_use.version_id AND component.ingredient_id IS NOT NULL JOIN units unit ON unit.tenant_id=$1 AND unit.id=component.unit_id GROUP BY component.ingredient_id), available AS (SELECT ingredient_id,COALESCE(SUM(quantity_base),0) quantity FROM inventory_events WHERE tenant_id=$1 AND outlet_id=$2 GROUP BY ingredient_id) SELECT ingredient.id,ingredient.name,unit.symbol,required.quantity,COALESCE(available.quantity,0) FROM required JOIN ingredients ingredient ON ingredient.tenant_id=$1 AND ingredient.id=required.ingredient_id JOIN units unit ON unit.tenant_id=ingredient.tenant_id AND unit.id=ingredient.base_unit_id LEFT JOIN available ON available.ingredient_id=required.ingredient_id WHERE required.quantity>COALESCE(available.quantity,0)`, value.TenantID, value.OutletID, value.EvidenceFrom, value.EvidenceTo)
		if err != nil {
			return err
		}
		warnings := []domain.PlanningRecommendation{}
		for stockRows.Next() {
			var id, name, symbol string
			var required, available float64
			if err := stockRows.Scan(&id, &name, &symbol, &required, &available); err != nil {
				stockRows.Close()
				return err
			}
			warning := domain.PlanningRecommendation{Type: "stockout_warning", IngredientID: id, IngredientName: name, UnitSymbol: symbol, RequiredQuantityBase: required, AvailableQuantityBase: available, Confidence: .7, Explanation: fmt.Sprintf("Forecast demand requires %.3f %s but only %.3f is available.", required, symbol, available), Evidence: map[string]any{"lookbackDays": 28, "method": "recipe_expansion"}}
			warnings = append(warnings, warning)
		}
		if err := stockRows.Err(); err != nil {
			stockRows.Close()
			return err
		}
		stockRows.Close()
		for index := range warnings {
			if err := insertRecommendation(ctx, tx, value.TenantID, value.ID, &warnings[index]); err != nil {
				return err
			}
			value.Recommendations = append(value.Recommendations, warnings[index])
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func loadRecommendations(ctx context.Context, tx pgx.Tx, value *domain.PlanningRun) error {
	rows, err := tx.Query(ctx, `SELECT recommendation.id,recommendation.recommendation_type,COALESCE(recommendation.menu_item_id::text,''),COALESCE(item.name,''),COALESCE(recommendation.recipe_version_id::text,''),COALESCE(recommendation.ingredient_id::text,''),COALESCE(ingredient.name,''),COALESCE(unit.symbol,''),recommendation.forecast_quantity,recommendation.required_quantity_base,recommendation.available_quantity_base,recommendation.confidence,recommendation.explanation,recommendation.evidence FROM planning_recommendations recommendation LEFT JOIN menu_items item ON item.tenant_id=recommendation.tenant_id AND item.id=recommendation.menu_item_id LEFT JOIN ingredients ingredient ON ingredient.tenant_id=recommendation.tenant_id AND ingredient.id=recommendation.ingredient_id LEFT JOIN units unit ON unit.tenant_id=ingredient.tenant_id AND unit.id=ingredient.base_unit_id WHERE recommendation.tenant_id=$1 AND recommendation.run_id=$2 ORDER BY recommendation.recommendation_type,recommendation.id`, value.TenantID, value.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r domain.PlanningRecommendation
		var evidence []byte
		if err := rows.Scan(&r.ID, &r.Type, &r.MenuItemID, &r.MenuItemName, &r.RecipeVersionID, &r.IngredientID, &r.IngredientName, &r.UnitSymbol, &r.ForecastQuantity, &r.RequiredQuantityBase, &r.AvailableQuantityBase, &r.Confidence, &r.Explanation, &evidence); err != nil {
			return err
		}
		if err := json.Unmarshal(evidence, &r.Evidence); err != nil {
			return err
		}
		value.Recommendations = append(value.Recommendations, r)
	}
	return rows.Err()
}

func (repository *PostgresRepository) PlanningRuns(ctx context.Context, tenantID, outletID string) ([]domain.PlanningRun, error) {
	values := []domain.PlanningRun{}
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,horizon_start,horizon_end,model_version,status,evidence_from,evidence_to,generated_at FROM planning_runs WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY generated_at DESC,id LIMIT 20`, tenantID, outletID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v domain.PlanningRun
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.HorizonStart, &v.HorizonEnd, &v.ModelVersion, &v.Status, &v.EvidenceFrom, &v.EvidenceTo, &v.GeneratedAt); err != nil {
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
			if err := loadRecommendations(ctx, tx, &values[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return values, err
}

var _ IntelligenceRepository = (*PostgresRepository)(nil)
