// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

// CreateMenuImportDraft stores the parsed export as immutable evidence and
// returns the existing canonical import when the same source is uploaded again.
func (repository *PostgresRepository) CreateMenuImportDraft(ctx context.Context, value domain.MenuImportDraft, audit domain.AuditEvent) (domain.MenuImportDraft, error) {
	err := repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `INSERT INTO menu_import_drafts(
			id,tenant_id,outlet_id,name,item_file_name,addon_file_name,source_sha256,status,
			item_count,category_count,addon_group_count,variation_count,draft,imported_at,
			actor_id,device_id,operation_id
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (tenant_id,outlet_id,source_sha256) DO NOTHING`,
			value.ID, value.TenantID, value.OutletID, value.Name, value.ItemFileName,
			value.AddonFileName, value.SourceSHA256, value.Status, value.ItemCount,
			value.CategoryCount, value.AddonGroupCount, value.VariationCount, []byte(value.Draft),
			value.ImportedAt, audit.ActorID, audit.DeviceID, audit.OperationID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,name,item_file_name,addon_file_name,source_sha256,status,
				item_count,category_count,addon_group_count,variation_count,draft,imported_at
				FROM menu_import_drafts WHERE tenant_id=$1 AND outlet_id=$2 AND source_sha256=$3`,
				value.TenantID, value.OutletID, value.SourceSHA256).Scan(
				&value.ID, &value.TenantID, &value.OutletID, &value.Name, &value.ItemFileName,
				&value.AddonFileName, &value.SourceSHA256, &value.Status, &value.ItemCount,
				&value.CategoryCount, &value.AddonGroupCount, &value.VariationCount, &value.Draft, &value.ImportedAt)
		}
		return insertAudit(ctx, tx, audit)
	})
	return value, err
}

func (repository *PostgresRepository) MenuImportDrafts(ctx context.Context, tenantID, outletID string) ([]domain.MenuImportDraft, error) {
	values := []domain.MenuImportDraft{}
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,name,item_file_name,addon_file_name,source_sha256,status,
			item_count,category_count,addon_group_count,variation_count,draft,imported_at
			FROM menu_import_drafts WHERE tenant_id=$1 AND outlet_id=$2
			ORDER BY imported_at DESC,id DESC`, tenantID, outletID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.MenuImportDraft
			var draft []byte
			if err := rows.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.Name,
				&value.ItemFileName, &value.AddonFileName, &value.SourceSHA256, &value.Status,
				&value.ItemCount, &value.CategoryCount, &value.AddonGroupCount, &value.VariationCount,
				&draft, &value.ImportedAt); err != nil {
				return err
			}
			value.Draft = draft
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}
