// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/feastcloud/feastcloud/services/core/internal/auth"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PostgresRepository is the durable implementation of both the HTTP resource
// boundary and the edge sync boundary. Every resource operation establishes
// tenant context inside its transaction so PostgreSQL RLS remains authoritative.
type PostgresRepository struct {
	*PostgresSyncRepository
}

func NewPostgresRepository(ctx context.Context, databaseURL string) (*PostgresRepository, error) {
	syncRepository, err := NewPostgresSyncRepository(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return &PostgresRepository{PostgresSyncRepository: syncRepository}, nil
}

func (repository *PostgresRepository) withTenant(
	ctx context.Context,
	tenantID string,
	operation func(pgx.Tx) error,
) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return fmt.Errorf("postgres repository: establish tenant: %w", err)
	}
	if err := operation(tx); err != nil {
		return repositoryErrorFromPostgres(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres repository: commit: %w", err)
	}
	return nil
}

func repositoryErrorFromPostgres(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return fmt.Errorf("%w: %s", ErrConflict, postgresError.ConstraintName)
		case "23503", "23514", "22P02":
			return fmt.Errorf("%w: %s", ErrInvalidReference, postgresError.ConstraintName)
		}
	}
	return err
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func insertAudit(ctx context.Context, tx pgx.Tx, audit domain.AuditEvent) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, operation_id, outlet_id, actor_id, device_id,
			source, source_id, idempotency_key, correlation_id, schema_version,
			action, entity_type, entity_id, occurred_at, recorded_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
	`, audit.ID, audit.TenantID, audit.OperationID, nullable(audit.OutletID), audit.ActorID,
		audit.DeviceID, audit.Source, nullable(audit.SourceID), audit.IdempotencyKey,
		nullable(audit.CorrelationID), audit.SchemaVersion, audit.Action, audit.EntityType,
		audit.EntityID, audit.OccurredAt, audit.RecordedAt)
	return err
}

func (repository *PostgresRepository) CreateOrganization(ctx context.Context, value domain.Organization, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO organizations
			(id,tenant_id,name,legal_name,default_locale,default_currency,active,version,created_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, value.ID, value.TenantID, value.Name,
			nullable(value.LegalName), value.DefaultLocale, value.DefaultCurrency, value.Active,
			value.Version, value.CreatedAt, value.UpdatedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

// ProvisionTenant is the operator-only, all-or-nothing customer bootstrap.
// It creates the tenant root and first executable kitchen topology in one RLS
// constrained transaction, so a half-created customer can never be billed or
// exposed to an outlet device.
func (repository *PostgresRepository) ProvisionTenant(ctx context.Context, organization domain.Organization, outlet domain.Outlet, brand domain.Brand, assignment domain.BrandOutletAssignment, stations []domain.Station, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, organization.TenantID, func(tx pgx.Tx) error {
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT has_table_privilege(current_user, 'tenants', 'INSERT')`).Scan(&allowed); err != nil { return err }
		if !allowed { return ErrPlatformProvisioningUnavailable }
		if _, err := tx.Exec(ctx, `INSERT INTO tenants(id,name,created_at) VALUES($1,$2,$3)`, organization.TenantID, organization.Name, organization.CreatedAt); err != nil { return err }
		if _, err := tx.Exec(ctx, `INSERT INTO organizations(id,tenant_id,name,legal_name,default_locale,default_currency,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, organization.ID, organization.TenantID, organization.Name, nullable(organization.LegalName), organization.DefaultLocale, organization.DefaultCurrency, organization.Active, organization.Version, organization.CreatedAt, organization.UpdatedAt); err != nil { return err }
		if _, err := tx.Exec(ctx, `INSERT INTO outlets(id,tenant_id,organization_id,name,code,time_zone,currency,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, outlet.ID, outlet.TenantID, outlet.OrganizationID, outlet.Name, outlet.Code, outlet.TimeZone, outlet.Currency, outlet.Active, outlet.Version, outlet.CreatedAt, outlet.UpdatedAt); err != nil { return err }
		if _, err := tx.Exec(ctx, `INSERT INTO brands(id,tenant_id,organization_id,name,code,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, brand.ID, brand.TenantID, brand.OrganizationID, brand.Name, brand.Code, brand.Active, brand.Version, brand.CreatedAt, brand.UpdatedAt); err != nil { return err }
		if _, err := tx.Exec(ctx, `INSERT INTO brand_outlet_assignments(tenant_id,brand_id,outlet_id,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, assignment.TenantID, assignment.BrandID, assignment.OutletID, assignment.Active, assignment.Version, assignment.CreatedAt, assignment.UpdatedAt); err != nil { return err }
		for _, station := range stations {
			if _, err := tx.Exec(ctx, `INSERT INTO stations(id,tenant_id,outlet_id,name,code,station_type,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, station.ID, station.TenantID, station.OutletID, station.Name, station.Code, station.Type, station.Active, station.Version, station.CreatedAt, station.UpdatedAt); err != nil { return err }
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (repository *PostgresRepository) Organization(ctx context.Context, tenantID, id string) (domain.Organization, error) {
	var value domain.Organization
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id,tenant_id,name,COALESCE(legal_name,''),default_locale,
			default_currency,active,version,created_at,updated_at FROM organizations
			WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&value.ID, &value.TenantID, &value.Name,
			&value.LegalName, &value.DefaultLocale, &value.DefaultCurrency, &value.Active,
			&value.Version, &value.CreatedAt, &value.UpdatedAt)
	})
	return value, err
}

func (repository *PostgresRepository) Organizations(ctx context.Context, tenantID string) ([]domain.Organization, error) {
	values := make([]domain.Organization, 0)
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,name,COALESCE(legal_name,''),default_locale,
			default_currency,active,version,created_at,updated_at FROM organizations
			WHERE tenant_id=$1 ORDER BY created_at,id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.Organization
			if err := rows.Scan(&value.ID, &value.TenantID, &value.Name, &value.LegalName, &value.DefaultLocale,
				&value.DefaultCurrency, &value.Active, &value.Version, &value.CreatedAt, &value.UpdatedAt); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (repository *PostgresRepository) CreateOutlet(ctx context.Context, value domain.Outlet, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO outlets
			(id,tenant_id,organization_id,name,code,time_zone,currency,active,version,created_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, value.ID, value.TenantID, value.OrganizationID,
			value.Name, value.Code, value.TimeZone, value.Currency, value.Active, value.Version, value.CreatedAt, value.UpdatedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func scanOutlet(row pgx.Row, value *domain.Outlet) error {
	return row.Scan(&value.ID, &value.TenantID, &value.OrganizationID, &value.Name, &value.Code, &value.TimeZone,
		&value.Currency, &value.Active, &value.Version, &value.CreatedAt, &value.UpdatedAt)
}

const outletSelect = `SELECT id,tenant_id,organization_id,name,code,time_zone,currency,active,version,created_at,updated_at FROM outlets`

func (repository *PostgresRepository) Outlet(ctx context.Context, tenantID, id string) (domain.Outlet, error) {
	var value domain.Outlet
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return scanOutlet(tx.QueryRow(ctx, outletSelect+` WHERE tenant_id=$1 AND id=$2`, tenantID, id), &value)
	})
	return value, err
}

func (repository *PostgresRepository) Outlets(ctx context.Context, tenantID string) ([]domain.Outlet, error) {
	values := make([]domain.Outlet, 0)
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, outletSelect+` WHERE tenant_id=$1 ORDER BY created_at,id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.Outlet
			if err := scanOutlet(rows, &value); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (repository *PostgresRepository) CreateBrand(ctx context.Context, value domain.Brand, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO brands (id,tenant_id,organization_id,name,code,active,version,created_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, value.ID, value.TenantID, value.OrganizationID, value.Name, value.Code, value.Active, value.Version, value.CreatedAt, value.UpdatedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func scanBrand(row pgx.Row, value *domain.Brand) error {
	return row.Scan(&value.ID, &value.TenantID, &value.OrganizationID, &value.Name, &value.Code, &value.Active, &value.Version, &value.CreatedAt, &value.UpdatedAt)
}

const brandSelect = `SELECT id,tenant_id,organization_id,name,code,active,version,created_at,updated_at FROM brands`

func (repository *PostgresRepository) Brand(ctx context.Context, tenantID, id string) (domain.Brand, error) {
	var value domain.Brand
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return scanBrand(tx.QueryRow(ctx, brandSelect+` WHERE tenant_id=$1 AND id=$2`, tenantID, id), &value)
	})
	return value, err
}
func (repository *PostgresRepository) Brands(ctx context.Context, tenantID string) ([]domain.Brand, error) {
	values := make([]domain.Brand, 0)
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, brandSelect+` WHERE tenant_id=$1 ORDER BY created_at,id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.Brand
			if err := scanBrand(rows, &value); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

const brandOutletAssignmentSelect = `SELECT tenant_id,brand_id,outlet_id,active,version,created_at,updated_at FROM brand_outlet_assignments`

func scanBrandOutletAssignment(row pgx.Row, value *domain.BrandOutletAssignment) error {
	return row.Scan(&value.TenantID, &value.BrandID, &value.OutletID, &value.Active, &value.Version, &value.CreatedAt, &value.UpdatedAt)
}

func (repository *PostgresRepository) SetBrandOutletAssignment(ctx context.Context, value domain.BrandOutletAssignment, expectedVersion uint64, audit domain.AuditEvent) (domain.BrandOutletAssignment, error) {
	var saved domain.BrandOutletAssignment
	err := repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		if expectedVersion == 0 {
			if err := scanBrandOutletAssignment(tx.QueryRow(ctx, `INSERT INTO brand_outlet_assignments
				(tenant_id,brand_id,outlet_id,active,version,created_at,updated_at)
				VALUES ($1,$2,$3,$4,1,$5,$6)
				RETURNING tenant_id,brand_id,outlet_id,active,version,created_at,updated_at`, value.TenantID, value.BrandID, value.OutletID, value.Active, value.CreatedAt, value.UpdatedAt), &saved); err != nil {
				return err
			}
		} else {
			if err := scanBrandOutletAssignment(tx.QueryRow(ctx, `UPDATE brand_outlet_assignments
				SET active=$4,version=version+1,updated_at=$5
				WHERE tenant_id=$1 AND brand_id=$2 AND outlet_id=$3 AND version=$6
				RETURNING tenant_id,brand_id,outlet_id,active,version,created_at,updated_at`, value.TenantID, value.BrandID, value.OutletID, value.Active, value.UpdatedAt, expectedVersion), &saved); err != nil {
				if err == pgx.ErrNoRows {
					var exists bool
					if checkErr := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM brand_outlet_assignments WHERE tenant_id=$1 AND brand_id=$2 AND outlet_id=$3)`, value.TenantID, value.BrandID, value.OutletID).Scan(&exists); checkErr != nil {
						return checkErr
					}
					if exists {
						return ErrVersionConflict
					}
					return ErrNotFound
				}
				return err
			}
		}
		return insertAudit(ctx, tx, audit)
	})
	return saved, err
}

func (repository *PostgresRepository) BrandOutletAssignments(ctx context.Context, tenantID string) ([]domain.BrandOutletAssignment, error) {
	values := make([]domain.BrandOutletAssignment, 0)
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, brandOutletAssignmentSelect+` WHERE tenant_id=$1 ORDER BY created_at,brand_id,outlet_id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.BrandOutletAssignment
			if err := scanBrandOutletAssignment(rows, &value); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (repository *PostgresRepository) CreateStation(ctx context.Context, value domain.Station, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO stations (id,tenant_id,outlet_id,name,code,station_type,active,version,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, value.ID, value.TenantID, value.OutletID, value.Name, value.Code, value.Type, value.Active, value.Version, value.CreatedAt, value.UpdatedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}
func scanStation(row pgx.Row, value *domain.Station) error {
	return row.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.Name, &value.Code, &value.Type, &value.Active, &value.Version, &value.CreatedAt, &value.UpdatedAt)
}

const stationSelect = `SELECT id,tenant_id,outlet_id,name,code,station_type,active,version,created_at,updated_at FROM stations`

func (repository *PostgresRepository) Station(ctx context.Context, tenantID, id string) (domain.Station, error) {
	var value domain.Station
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return scanStation(tx.QueryRow(ctx, stationSelect+` WHERE tenant_id=$1 AND id=$2`, tenantID, id), &value)
	})
	return value, err
}
func (repository *PostgresRepository) Stations(ctx context.Context, tenantID string) ([]domain.Station, error) {
	values := make([]domain.Station, 0)
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, stationSelect+` WHERE tenant_id=$1 ORDER BY created_at,id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.Station
			if err := scanStation(rows, &value); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func (repository *PostgresRepository) CreateOrder(ctx context.Context, value domain.Order, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO orders (id,tenant_id,outlet_id,brand_id,external_ref,source,source_id,order_type,status,currency,subtotal_minor,discount_total_minor,tax_total_minor,service_charge_minor,total_minor,placed_at,version,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, value.ID, value.TenantID, value.OutletID, nullable(value.BrandID), nullable(value.ExternalRef), audit.Source, nullable(audit.SourceID), value.Type, value.Status, value.Total.Currency, value.Subtotal.MinorUnits, value.DiscountTotal.MinorUnits, value.TaxTotal.MinorUnits, value.ServiceCharge.MinorUnits, value.Total.MinorUnits, value.PlacedAt, value.Version, value.CreatedAt, value.UpdatedAt); err != nil {
			return err
		}
		for _, line := range value.Lines {
			if _, err := tx.Exec(ctx, `INSERT INTO order_lines (id,tenant_id,order_id,menu_item_id,name,quantity,currency,unit_price_minor,line_total_minor,preparation_note,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, line.ID, value.TenantID, value.ID, nullable(line.MenuItemID), line.Name, line.Quantity, line.UnitPrice.Currency, line.UnitPrice.MinorUnits, line.LineTotal.MinorUnits, nullable(line.PreparationNote), value.CreatedAt); err != nil {
				return err
			}
			if line.MenuItemID != "" {
				if _, err := tx.Exec(ctx, `INSERT INTO order_line_recipe_snapshots(tenant_id,order_id,order_line_id,menu_item_id,recipe_version_id,quantity,captured_at) SELECT $1,$2,$3,item.id,version.id,$5,$6 FROM menu_items item JOIN recipe_versions version ON version.tenant_id=item.tenant_id AND version.recipe_id=item.recipe_id AND version.effective_from<=$7 AND (version.effective_to IS NULL OR version.effective_to>$7) WHERE item.tenant_id=$1 AND item.outlet_id=$4 AND item.id=$8`, value.TenantID, value.ID, line.ID, value.OutletID, line.Quantity, value.CreatedAt, value.PlacedAt, line.MenuItemID); err != nil {
					return err
				}
			}
		}
		return insertAudit(ctx, tx, audit)
	})
}

const orderSelect = `SELECT id,tenant_id,outlet_id,COALESCE(brand_id::text,''),COALESCE(external_ref,''),order_type,status,currency,subtotal_minor,discount_total_minor,tax_total_minor,service_charge_minor,total_minor,placed_at,version,created_at,updated_at FROM orders`

func scanOrder(row pgx.Row, value *domain.Order) error {
	var currency string
	if err := row.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.BrandID, &value.ExternalRef, &value.Type, &value.Status, &currency, &value.Subtotal.MinorUnits, &value.DiscountTotal.MinorUnits, &value.TaxTotal.MinorUnits, &value.ServiceCharge.MinorUnits, &value.Total.MinorUnits, &value.PlacedAt, &value.Version, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return err
	}
	value.Subtotal.Currency = currency
	value.DiscountTotal.Currency = currency
	value.TaxTotal.Currency = currency
	value.ServiceCharge.Currency = currency
	value.Total.Currency = currency
	return nil
}
func loadOrderLines(ctx context.Context, tx pgx.Tx, value *domain.Order) error {
	rows, err := tx.Query(ctx, `SELECT id,COALESCE(menu_item_id::text,''),name,quantity,currency,unit_price_minor,line_total_minor,COALESCE(preparation_note,'') FROM order_lines WHERE tenant_id=$1 AND order_id=$2 ORDER BY created_at,id`, value.TenantID, value.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	value.Lines = make([]domain.OrderLine, 0)
	for rows.Next() {
		var line domain.OrderLine
		var currency string
		if err := rows.Scan(&line.ID, &line.MenuItemID, &line.Name, &line.Quantity, &currency, &line.UnitPrice.MinorUnits, &line.LineTotal.MinorUnits, &line.PreparationNote); err != nil {
			return err
		}
		line.UnitPrice.Currency = currency
		line.LineTotal.Currency = currency
		value.Lines = append(value.Lines, line)
	}
	return rows.Err()
}
func (repository *PostgresRepository) Order(ctx context.Context, tenantID, id string) (domain.Order, error) {
	var value domain.Order
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		if err := scanOrder(tx.QueryRow(ctx, orderSelect+` WHERE tenant_id=$1 AND id=$2`, tenantID, id), &value); err != nil {
			return err
		}
		return loadOrderLines(ctx, tx, &value)
	})
	return value, err
}
func (repository *PostgresRepository) Orders(ctx context.Context, tenantID string) ([]domain.Order, error) {
	values := make([]domain.Order, 0)
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, orderSelect+` WHERE tenant_id=$1 ORDER BY created_at,id`, tenantID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var value domain.Order
			if err := scanOrder(rows, &value); err != nil {
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
			if err := loadOrderLines(ctx, tx, &values[index]); err != nil {
				return err
			}
		}
		return nil
	})
	return values, err
}

func (repository *PostgresRepository) PageOrders(ctx context.Context, request OrderPageRequest) (OrderPage, error) {
	page := OrderPage{Values: make([]domain.Order, 0)}
	err := repository.withTenant(ctx, request.TenantID, func(tx pgx.Tx) error {
		clauses := []string{"tenant_id=$1"}
		args := []any{request.TenantID}
		if request.OutletID != "" {
			args = append(args, request.OutletID)
			clauses = append(clauses, fmt.Sprintf("outlet_id=$%d", len(args)))
		}
		if request.After != nil {
			args = append(args, request.After.CreatedAt, request.After.ID)
			clauses = append(clauses, fmt.Sprintf("(created_at,id)>($%d,$%d)", len(args)-1, len(args)))
		}
		args = append(args, request.Limit+1)
		rows, err := tx.Query(ctx, orderSelect+` WHERE `+strings.Join(clauses, " AND ")+fmt.Sprintf(` ORDER BY created_at,id LIMIT $%d`, len(args)), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var value domain.Order
			if err := scanOrder(rows, &value); err != nil {
				rows.Close()
				return err
			}
			page.Values = append(page.Values, value)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(page.Values) > request.Limit {
			last := page.Values[request.Limit-1]
			page.Next = &PageCursor{CreatedAt: last.CreatedAt, ID: last.ID}
			page.Values = page.Values[:request.Limit]
		}
		for index := range page.Values {
			if err := loadOrderLines(ctx, tx, &page.Values[index]); err != nil {
				return err
			}
		}
		return nil
	})
	return page, err
}

func (repository *PostgresRepository) TransitionOrder(ctx context.Context, tenantID, outletID, id string, to domain.OrderStatus, expectedVersion uint64, audit domain.AuditEvent) (domain.Order, error) {
	var value domain.Order
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		if err := scanOrder(tx.QueryRow(ctx, orderSelect+` WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, tenantID, outletID, id), &value); err != nil {
			return err
		}
		if value.Version != expectedVersion {
			return fmt.Errorf("%w: order %q is version %d", ErrVersionConflict, id, value.Version)
		}
		if !domain.CanTransitionOrderStatus(value.Status, to) {
			return fmt.Errorf("%w: order %s to %s", ErrInvalidTransition, value.Status, to)
		}
		tag, err := tx.Exec(ctx, `UPDATE orders SET status=$4,version=version+1,updated_at=$5 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND version=$6`, tenantID, outletID, id, to, audit.RecordedAt, expectedVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrVersionConflict
		}
		if err := insertAudit(ctx, tx, audit); err != nil {
			return err
		}
		if to == domain.OrderStatusCompleted {
			if err := consumeOrderInventory(ctx, tx, tenantID, outletID, id, value.Total.Currency, audit); err != nil {
				return err
			}
		}
		value.Status = to
		value.Version++
		value.UpdatedAt = audit.RecordedAt
		return loadOrderLines(ctx, tx, &value)
	})
	return value, err
}

func consumeOrderInventory(ctx context.Context, tx pgx.Tx, tenantID, outletID, orderID, currency string, audit domain.AuditEvent) error {
	rows, err := tx.Query(ctx, `WITH RECURSIVE recipe_use(version_id,multiplier) AS (
		SELECT snapshot.recipe_version_id,snapshot.quantity::numeric/version.yield_quantity
		FROM order_line_recipe_snapshots snapshot JOIN recipe_versions version ON version.tenant_id=snapshot.tenant_id AND version.id=snapshot.recipe_version_id
		WHERE snapshot.tenant_id=$1 AND snapshot.order_id=$2
		UNION ALL
		SELECT component.child_recipe_version_id,recipe_use.multiplier * component.quantity * source.base_numerator::numeric/source.base_denominator / (child.yield_quantity * target.base_numerator::numeric/target.base_denominator)
		FROM recipe_use JOIN recipe_components component ON component.tenant_id=$1 AND component.recipe_version_id=recipe_use.version_id AND component.child_recipe_version_id IS NOT NULL
		JOIN units source ON source.tenant_id=$1 AND source.id=component.unit_id
		JOIN recipe_versions child ON child.tenant_id=$1 AND child.id=component.child_recipe_version_id
		JOIN units target ON target.tenant_id=$1 AND target.id=child.yield_unit_id
	), consumed AS (
		SELECT component.ingredient_id,SUM(recipe_use.multiplier * component.quantity * source.base_numerator::numeric/source.base_denominator) AS quantity
		FROM recipe_use JOIN recipe_components component ON component.tenant_id=$1 AND component.recipe_version_id=recipe_use.version_id AND component.ingredient_id IS NOT NULL
		JOIN units source ON source.tenant_id=$1 AND source.id=component.unit_id GROUP BY component.ingredient_id
	)
	SELECT ingredient_id,quantity FROM consumed`, tenantID, orderID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type consumption struct {
		ingredient string
		quantity   float64
	}
	values := []consumption{}
	for rows.Next() {
		var v consumption
		if err := rows.Scan(&v.ingredient, &v.quantity); err != nil {
			return err
		}
		values = append(values, v)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, item := range values {
		var stock float64
		var cost int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(quantity_base),0),COALESCE(SUM(total_cost_minor),0) FROM inventory_events WHERE tenant_id=$1 AND outlet_id=$2 AND ingredient_id=$3`, tenantID, outletID, item.ingredient).Scan(&stock, &cost); err != nil {
			return err
		}
		eventCost := int64(0)
		if stock > 0 {
			eventCost = -int64(math.Round(item.quantity * float64(cost) / stock))
		}
		eventID := inventoryEventUUID(tenantID, audit.OperationID, item.ingredient)
		_, err := tx.Exec(ctx, `INSERT INTO inventory_events(id,tenant_id,outlet_id,ingredient_id,event_type,quantity_base,total_cost_minor,currency,reference_type,reference_id,reason,occurred_at,recorded_at,actor_id,device_id,operation_id) VALUES($1,$2,$3,$4,'consumption',$5,$6,$7,'order',$8,'recipe snapshot consumption',$9,$10,$11,$12,$13) ON CONFLICT(tenant_id,operation_id,ingredient_id) DO NOTHING`, eventID, tenantID, outletID, item.ingredient, -item.quantity, eventCost, currency, orderID, audit.OccurredAt, audit.RecordedAt, audit.ActorID, audit.DeviceID, audit.OperationID)
		if err != nil {
			return err
		}
	}
	return nil
}

func inventoryEventUUID(tenantID, operationID, ingredientID string) string {
	sum := sha256.Sum256([]byte(tenantID + "|" + operationID + "|" + ingredientID))
	sum[6] = (sum[6] & 0x0f) | 0x40
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func (repository *PostgresRepository) CreateKitchenTicket(ctx context.Context, value domain.KitchenTicket, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, value.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO kitchen_tickets (id,tenant_id,outlet_id,order_id,station_id,status,priority,target_at,version,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, value.ID, value.TenantID, value.OutletID, value.OrderID, value.StationID, value.Status, value.Priority, value.TargetAt, value.Version, value.CreatedAt, value.UpdatedAt); err != nil {
			return err
		}
		for _, lineID := range value.LineIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO ticket_lines (tenant_id,ticket_id,order_id,order_line_id,created_at) VALUES ($1,$2,$3,$4,$5)`, value.TenantID, value.ID, value.OrderID, lineID, value.CreatedAt); err != nil {
				return err
			}
		}
		return insertAudit(ctx, tx, audit)
	})
}

const ticketSelect = `SELECT id,tenant_id,outlet_id,order_id,station_id,status,priority,target_at,version,created_at,updated_at FROM kitchen_tickets`

func scanTicket(row pgx.Row, value *domain.KitchenTicket) error {
	return row.Scan(&value.ID, &value.TenantID, &value.OutletID, &value.OrderID, &value.StationID, &value.Status, &value.Priority, &value.TargetAt, &value.Version, &value.CreatedAt, &value.UpdatedAt)
}
func loadTicketLines(ctx context.Context, tx pgx.Tx, value *domain.KitchenTicket) error {
	rows, err := tx.Query(ctx, `SELECT order_line_id FROM ticket_lines WHERE tenant_id=$1 AND ticket_id=$2 ORDER BY created_at,order_line_id`, value.TenantID, value.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	value.LineIDs = make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		value.LineIDs = append(value.LineIDs, id)
	}
	return rows.Err()
}
func (repository *PostgresRepository) KitchenTicket(ctx context.Context, tenantID, id string) (domain.KitchenTicket, error) {
	var value domain.KitchenTicket
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		if err := scanTicket(tx.QueryRow(ctx, ticketSelect+` WHERE tenant_id=$1 AND id=$2`, tenantID, id), &value); err != nil {
			return err
		}
		return loadTicketLines(ctx, tx, &value)
	})
	return value, err
}
func (repository *PostgresRepository) KitchenTickets(ctx context.Context, tenantID string) ([]domain.KitchenTicket, error) {
	values := make([]domain.KitchenTicket, 0)
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, ticketSelect+` WHERE tenant_id=$1 ORDER BY created_at,id`, tenantID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var value domain.KitchenTicket
			if err := scanTicket(rows, &value); err != nil {
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
			if err := loadTicketLines(ctx, tx, &values[index]); err != nil {
				return err
			}
		}
		return nil
	})
	return values, err
}

func (repository *PostgresRepository) PageKitchenTickets(ctx context.Context, request TicketPageRequest) (TicketPage, error) {
	page := TicketPage{Values: make([]domain.KitchenTicket, 0)}
	err := repository.withTenant(ctx, request.TenantID, func(tx pgx.Tx) error {
		clauses := []string{"tenant_id=$1"}
		args := []any{request.TenantID}
		for column, value := range map[string]string{"outlet_id": request.OutletID, "order_id": request.OrderID, "station_id": request.StationID} {
			if value != "" {
				args = append(args, value)
				clauses = append(clauses, fmt.Sprintf("%s=$%d", column, len(args)))
			}
		}
		if request.After != nil {
			args = append(args, request.After.CreatedAt, request.After.ID)
			clauses = append(clauses, fmt.Sprintf("(created_at,id)>($%d,$%d)", len(args)-1, len(args)))
		}
		args = append(args, request.Limit+1)
		rows, err := tx.Query(ctx, ticketSelect+` WHERE `+strings.Join(clauses, " AND ")+fmt.Sprintf(` ORDER BY created_at,id LIMIT $%d`, len(args)), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var value domain.KitchenTicket
			if err := scanTicket(rows, &value); err != nil {
				rows.Close()
				return err
			}
			page.Values = append(page.Values, value)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(page.Values) > request.Limit {
			last := page.Values[request.Limit-1]
			page.Next = &PageCursor{CreatedAt: last.CreatedAt, ID: last.ID}
			page.Values = page.Values[:request.Limit]
		}
		for index := range page.Values {
			if err := loadTicketLines(ctx, tx, &page.Values[index]); err != nil {
				return err
			}
		}
		return nil
	})
	return page, err
}

func (repository *PostgresRepository) TransitionKitchenTicket(ctx context.Context, tenantID, outletID, id string, to domain.TicketStatus, expectedVersion uint64, audit domain.AuditEvent) (domain.KitchenTicket, error) {
	var value domain.KitchenTicket
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		if err := scanTicket(tx.QueryRow(ctx, ticketSelect+` WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, tenantID, outletID, id), &value); err != nil {
			return err
		}
		if value.Version != expectedVersion {
			return fmt.Errorf("%w: kitchen ticket %q is version %d", ErrVersionConflict, id, value.Version)
		}
		if !domain.CanTransitionTicketStatus(value.Status, to) {
			return fmt.Errorf("%w: ticket %s to %s", ErrInvalidTransition, value.Status, to)
		}
		tag, err := tx.Exec(ctx, `UPDATE kitchen_tickets SET status=$4,version=version+1,updated_at=$5 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND version=$6`, tenantID, outletID, id, to, audit.RecordedAt, expectedVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrVersionConflict
		}
		if err := insertAudit(ctx, tx, audit); err != nil {
			return err
		}
		value.Status = to
		value.Version++
		value.UpdatedAt = audit.RecordedAt
		return loadTicketLines(ctx, tx, &value)
	})
	return value, err
}

func (repository *PostgresRepository) AuditEvents(ctx context.Context, filter AuditFilter) ([]domain.AuditEvent, error) {
	values := make([]domain.AuditEvent, 0)
	err := repository.withTenant(ctx, filter.TenantID, func(tx pgx.Tx) error {
		clauses := []string{"tenant_id=$1"}
		arguments := []any{filter.TenantID}
		if filter.OutletID != "" {
			arguments = append(arguments, filter.OutletID)
			clauses = append(clauses, fmt.Sprintf("outlet_id=$%d", len(arguments)))
		}
		if filter.EntityType != "" {
			arguments = append(arguments, filter.EntityType)
			clauses = append(clauses, fmt.Sprintf("entity_type=$%d", len(arguments)))
		}
		if filter.EntityID != "" {
			arguments = append(arguments, filter.EntityID)
			clauses = append(clauses, fmt.Sprintf("entity_id=$%d", len(arguments)))
		}
		rows, err := tx.Query(ctx, `SELECT id,operation_id,tenant_id,COALESCE(outlet_id::text,''),actor_id,device_id,source,COALESCE(source_id,''),idempotency_key,COALESCE(correlation_id,''),schema_version,action,entity_type,entity_id,occurred_at,recorded_at FROM audit_events WHERE `+strings.Join(clauses, " AND ")+` ORDER BY recorded_at,id`, arguments...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.AuditEvent
			if err := rows.Scan(&value.ID, &value.OperationID, &value.TenantID, &value.OutletID, &value.ActorID, &value.DeviceID, &value.Source, &value.SourceID, &value.IdempotencyKey, &value.CorrelationID, &value.SchemaVersion, &value.Action, &value.EntityType, &value.EntityID, &value.OccurredAt, &value.RecordedAt); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

var _ Repository = (*PostgresRepository)(nil)
var _ SyncRepository = (*PostgresRepository)(nil)
var _ OperationalPager = (*PostgresRepository)(nil)

func (repository *PostgresRepository) DeviceByFingerprint(ctx context.Context, tenantID, fingerprint string) (auth.Device, error) {
	var device auth.Device
	err := repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT tenant_id,outlet_id,edge_id,id,certificate_fingerprint,status FROM identity_devices WHERE tenant_id=$1 AND certificate_fingerprint=$2`, tenantID, fingerprint).Scan(&device.TenantID, &device.OutletID, &device.EdgeID, &device.DeviceID, &device.Fingerprint, &device.Status)
	})
	return device, err
}
func (repository *PostgresRepository) RegisterDevice(ctx context.Context, device auth.Device, name string, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, device.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO identity_devices(id,tenant_id,outlet_id,edge_id,name,certificate_fingerprint,status,enrolled_by,enrolled_at) VALUES($1,$2,$3,$4,$5,$6,'active',$7,$8)`, device.DeviceID, device.TenantID, device.OutletID, device.EdgeID, name, device.Fingerprint, audit.ActorID, audit.RecordedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}
func (repository *PostgresRepository) RevokeDevice(ctx context.Context, tenantID, deviceID, actorID string, audit domain.AuditEvent) error {
	return repository.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE identity_devices SET status='revoked',revoked_by=$3,revoked_at=$4,version=version+1 WHERE tenant_id=$1 AND id=$2 AND status='active'`, tenantID, deviceID, actorID, audit.RecordedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		return insertAudit(ctx, tx, audit)
	})
}

var _ auth.DeviceRegistry = (*PostgresRepository)(nil)
var _ DeviceAdministration = (*PostgresRepository)(nil)
