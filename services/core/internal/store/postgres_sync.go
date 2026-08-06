// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

// PostgresSyncRepository commits inbox evidence and the append-only domain
// event in one PostgreSQL transaction.
type PostgresSyncRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresSyncRepository creates a lazy connection pool. Readiness reports
// connectivity and migration state without making process startup depend on WAN
// or database availability.
func NewPostgresSyncRepository(ctx context.Context, databaseURL string) (*PostgresSyncRepository, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres sync: parse configuration: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("postgres sync: create pool: %w", err)
	}
	return &PostgresSyncRepository{pool: pool}, nil
}

func (repository *PostgresSyncRepository) Close() { repository.pool.Close() }

func (repository *PostgresSyncRepository) Ready(ctx context.Context) error {
	if err := repository.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres sync: ping: %w", err)
	}
	var migrated bool
	if err := repository.pool.QueryRow(ctx, `
		SELECT to_regclass('public.sync_inbox') IS NOT NULL
		   AND to_regclass('public.domain_events') IS NOT NULL
		   AND to_regclass('public.orders') IS NOT NULL
		   AND to_regclass('public.kitchen_tickets') IS NOT NULL
		   AND to_regclass('public.idempotency_records') IS NOT NULL
		   AND to_regclass('public.inventory_events') IS NOT NULL
		   AND to_regclass('public.recipe_versions') IS NOT NULL
		   AND to_regclass('public.configuration_snapshots') IS NOT NULL
		   AND to_regclass('public.edge_sync_checkpoints') IS NOT NULL
		   AND to_regclass('public.reconciliation_cases') IS NOT NULL
		   AND to_regclass('public.operational_incidents') IS NOT NULL
		   AND to_regclass('public.backup_manifests') IS NOT NULL
		   AND to_regclass('public.purchase_orders') IS NOT NULL
		   AND to_regclass('public.temperature_logs') IS NOT NULL
		   AND to_regclass('public.operational_checklists') IS NOT NULL
		   AND to_regclass('public.staff_shifts') IS NOT NULL
		   AND to_regclass('public.tenders') IS NOT NULL
		   AND to_regclass('public.cash_shifts') IS NOT NULL
		   AND to_regclass('public.dining_sessions') IS NOT NULL
		   AND to_regclass('public.guest_profiles') IS NOT NULL
		   AND to_regclass('public.reservations') IS NOT NULL
		   AND to_regclass('public.promotions') IS NOT NULL
		   AND to_regclass('public.loyalty_events') IS NOT NULL
		   AND to_regclass('public.brand_outlet_assignments') IS NOT NULL
		   AND to_regclass('public.menu_import_drafts') IS NOT NULL
		   AND EXISTS (
		       SELECT 1 FROM information_schema.columns
		       WHERE table_schema = 'public' AND table_name = 'audit_events'
		         AND column_name = 'outlet_id' AND is_nullable = 'YES'
		   )
	`).Scan(&migrated); err != nil {
		return fmt.Errorf("postgres sync: inspect schema: %w", err)
	}
	if !migrated {
		return errors.New("postgres sync: required migrations are not applied")
	}
	return nil
}

func (repository *PostgresSyncRepository) ApplySyncOperation(
	ctx context.Context,
	operation SyncOperation,
) (SyncOutcome, string, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("postgres sync: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, operation.TenantID); err != nil {
		return "", "", fmt.Errorf("postgres sync: establish tenant: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO sync_inbox (
			tenant_id, operation_id, edge_id, outlet_id, batch_id,
			aggregate_type, aggregate_id, aggregate_version, command_type,
			request_hash, mutation, status, received_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, 'received', $12)
		ON CONFLICT (tenant_id, operation_id) DO NOTHING
	`, operation.TenantID, operation.OperationID, operation.EdgeID, operation.OutletID,
		operation.BatchID, operation.AggregateType, operation.AggregateID,
		operation.AggregateVersion, operation.CommandType, operation.RequestHash,
		operation.Mutation, operation.ReceivedAt)
	if err != nil {
		return "", "", fmt.Errorf("postgres sync: insert inbox: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return existingSyncOutcome(ctx, tx, operation)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO edge_sync_checkpoints(tenant_id,edge_id,outlet_id,last_operation_id,last_received_at,updated_at) VALUES($1,$2,$3,$4,$5,$5) ON CONFLICT(tenant_id,edge_id) DO UPDATE SET outlet_id=EXCLUDED.outlet_id,last_operation_id=EXCLUDED.last_operation_id,last_received_at=EXCLUDED.last_received_at,version=edge_sync_checkpoints.version+1,updated_at=EXCLUDED.updated_at`, operation.TenantID, operation.EdgeID, operation.OutletID, operation.OperationID, operation.ReceivedAt); err != nil {
		return "", "", fmt.Errorf("postgres sync: update received checkpoint: %w", err)
	}

	if problemCode := syncCommandProblem(operation); problemCode != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE sync_inbox
			SET status = 'rejected', problem_code = $3, processed_at = clock_timestamp()
			WHERE tenant_id = $1 AND operation_id = $2
		`, operation.TenantID, operation.OperationID, problemCode); err != nil {
			return "", "", fmt.Errorf("postgres sync: reject inbox operation: %w", err)
		}
		if err := recordSyncProblem(ctx, tx, operation, problemCode, "rejected"); err != nil {
			return "", "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", "", fmt.Errorf("postgres sync: commit rejection: %w", err)
		}
		return SyncRejected, problemCode, nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, operation.TenantID+"|"+operation.AggregateType+"|"+operation.AggregateID); err != nil {
		return "", "", fmt.Errorf("postgres sync: lock aggregate: %w", err)
	}
	causalOutcome, causalProblem, err := validateCausalVersion(ctx, tx, operation)
	if err != nil {
		return "", "", err
	}
	if causalOutcome == SyncConflict {
		if _, err := tx.Exec(ctx, `UPDATE sync_inbox SET status='conflict',problem_code=$3,processed_at=clock_timestamp() WHERE tenant_id=$1 AND operation_id=$2`, operation.TenantID, operation.OperationID, causalProblem); err != nil {
			return "", "", fmt.Errorf("postgres sync: record causal conflict: %w", err)
		}
		if err := recordSyncProblem(ctx, tx, operation, causalProblem, "conflict"); err != nil {
			return "", "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", "", fmt.Errorf("postgres sync: commit causal conflict: %w", err)
		}
		return SyncConflict, causalProblem, nil
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO domain_events (
			tenant_id, operation_id, outlet_id, aggregate_type, aggregate_id,
			aggregate_version, command_type, mutation, occurred_at, recorded_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)
	`, operation.TenantID, operation.OperationID, operation.OutletID,
		operation.AggregateType, operation.AggregateID, operation.AggregateVersion,
		operation.CommandType, operation.Mutation, mutationOccurredAt(operation),
		operationRecordedAt(operation)); err != nil {
		return "", "", fmt.Errorf("postgres sync: append domain event: %w", err)
	}
	if err := projectOperationalMutation(ctx, tx, operation); err != nil {
		return "", "", fmt.Errorf("postgres sync: project operational mutation: %w", err)
	}
	tag, err = tx.Exec(ctx, `
		UPDATE sync_inbox
		SET status = 'accepted', processed_at = clock_timestamp()
		WHERE tenant_id = $1 AND operation_id = $2 AND status = 'received'
	`, operation.TenantID, operation.OperationID)
	if err != nil {
		return "", "", fmt.Errorf("postgres sync: accept inbox operation: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return "", "", errors.New("postgres sync: inbox acceptance did not update one row")
	}
	if _, err := tx.Exec(ctx, `UPDATE edge_sync_checkpoints SET last_accepted_at=$3,degraded=false,last_problem_code='',backlog_count=(SELECT COUNT(*) FROM sync_inbox WHERE tenant_id=$1 AND edge_id=$2 AND status='received'),version=version+1,updated_at=$3 WHERE tenant_id=$1 AND edge_id=$2`, operation.TenantID, operation.EdgeID, operation.ReceivedAt); err != nil {
		return "", "", fmt.Errorf("postgres sync: accept checkpoint: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("postgres sync: commit accepted operation: %w", err)
	}
	return SyncAccepted, "", nil
}

func recordSyncProblem(ctx context.Context, tx pgx.Tx, operation SyncOperation, problemCode, status string) error {
	caseID := inventoryEventUUID(operation.TenantID, operation.OperationID, "reconciliation")
	_, err := tx.Exec(ctx, `INSERT INTO reconciliation_cases(id,tenant_id,outlet_id,source_type,source_id,category,severity,status,title,details,opened_at,updated_at) VALUES($1,$2,$3,'sync',$4::text,$5,'high','open',$6,jsonb_build_object('operationId',$4::text,'edgeId',$7::text,'commandType',$8::text,'aggregateId',$9::text,'syncStatus',$10::text),$11,$11) ON CONFLICT(tenant_id,source_type,source_id) DO NOTHING`, caseID, operation.TenantID, operation.OutletID, operation.OperationID, problemCode, "Sync operation requires reconciliation", operation.EdgeID, operation.CommandType, operation.AggregateID, status, operation.ReceivedAt)
	if err != nil {
		return fmt.Errorf("postgres sync: open reconciliation case: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE edge_sync_checkpoints SET degraded=true,last_problem_code=$3,backlog_count=(SELECT COUNT(*) FROM sync_inbox WHERE tenant_id=$1 AND edge_id=$2 AND status='received'),version=version+1,updated_at=$4 WHERE tenant_id=$1 AND edge_id=$2`, operation.TenantID, operation.EdgeID, problemCode, operation.ReceivedAt)
	return err
}

type edgeMutationEnvelope struct {
	ActorID    string    `json:"actorId"`
	DeviceID   string    `json:"deviceId"`
	OccurredAt time.Time `json:"occurredAt"`
	Payload    struct {
		ToStatus string `json:"toStatus"`
		Order    *struct {
			ID       string    `json:"id"`
			Type     string    `json:"type"`
			PlacedAt time.Time `json:"placedAt"`
			Lines    []struct {
				ID         string `json:"id"`
				MenuItemID string `json:"menuItemId"`
				Name       string `json:"name"`
				Quantity   int32  `json:"quantity"`
			} `json:"lines"`
		} `json:"order"`
	} `json:"payload"`
}

func projectOperationalMutation(ctx context.Context, tx pgx.Tx, operation SyncOperation) error {
	var envelope edgeMutationEnvelope
	if json.Unmarshal(operation.Mutation, &envelope) != nil {
		return nil
	}
	switch operation.CommandType {
	case "order.create":
		if envelope.Payload.Order == nil || envelope.Payload.Order.ID == "" {
			return nil
		}
		order := envelope.Payload.Order
		placedAt := order.PlacedAt
		if placedAt.IsZero() {
			placedAt = mutationOccurredAt(operation)
		}
		var currency string
		if err := tx.QueryRow(ctx, `SELECT currency FROM outlets WHERE tenant_id=$1 AND id=$2`, operation.TenantID, operation.OutletID).Scan(&currency); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO orders(id,tenant_id,outlet_id,source,source_id,order_type,status,currency,subtotal_minor,discount_total_minor,tax_total_minor,service_charge_minor,total_minor,placed_at,version,created_at,updated_at) VALUES($1,$2,$3,'feastcloud-edge',$4,$5,'received',$6,0,0,0,0,0,$7,$8,$9,$9) ON CONFLICT(tenant_id,id) DO NOTHING`, order.ID, operation.TenantID, operation.OutletID, order.ID, order.Type, currency, placedAt, operation.AggregateVersion, operationRecordedAt(operation))
		if err != nil {
			return err
		}
		for _, line := range order.Lines {
			if !domain.ValidUUID(line.ID) {
				continue
			}
			menuID := nullable("")
			if domain.ValidUUID(line.MenuItemID) {
				menuID = line.MenuItemID
			}
			name := line.Name
			if name == "" {
				name = line.MenuItemID
			}
			if name == "" {
				name = "Edge order item"
			}
			_, err = tx.Exec(ctx, `INSERT INTO order_lines(id,tenant_id,order_id,menu_item_id,name,quantity,currency,unit_price_minor,line_total_minor,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,0,0,$8) ON CONFLICT(id) DO NOTHING`, line.ID, operation.TenantID, order.ID, menuID, name, line.Quantity, currency, operationRecordedAt(operation))
			if err != nil {
				return err
			}
			if domain.ValidUUID(line.MenuItemID) {
				_, err = tx.Exec(ctx, `INSERT INTO order_line_recipe_snapshots(tenant_id,order_id,order_line_id,menu_item_id,recipe_version_id,quantity,captured_at) SELECT $1,$2,$3,item.id,version.id,$5,$6 FROM menu_items item JOIN recipe_versions version ON version.tenant_id=item.tenant_id AND version.recipe_id=item.recipe_id AND version.effective_from<=$7 AND(version.effective_to IS NULL OR version.effective_to>$7) WHERE item.tenant_id=$1 AND item.outlet_id=$4 AND item.id=$8 ON CONFLICT DO NOTHING`, operation.TenantID, order.ID, line.ID, operation.OutletID, line.Quantity, operationRecordedAt(operation), placedAt, line.MenuItemID)
				if err != nil {
					return err
				}
			}
		}
	case "order.transition", "kitchenTicket.transitionAll":
		status := envelope.Payload.ToStatus
		if status == "queued" {
			status = "received"
		}
		if status == "fired" {
			status = "accepted"
		}
		if status == "" {
			return nil
		}
		tag, err := tx.Exec(ctx, `UPDATE orders SET status=$4,version=$5,updated_at=$6 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 AND version<$5`, operation.TenantID, operation.OutletID, operation.AggregateID, status, operation.AggregateVersion, operationRecordedAt(operation))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 && status == "completed" {
			audit := domain.AuditEvent{OperationID: operation.OperationID, TenantID: operation.TenantID, OutletID: operation.OutletID, ActorID: envelope.ActorID, DeviceID: envelope.DeviceID, OccurredAt: mutationOccurredAt(operation), RecordedAt: operationRecordedAt(operation)}
			var currency string
			if err := tx.QueryRow(ctx, `SELECT currency FROM orders WHERE tenant_id=$1 AND id=$2`, operation.TenantID, operation.AggregateID).Scan(&currency); err != nil {
				return err
			}
			return consumeOrderInventory(ctx, tx, operation.TenantID, operation.OutletID, operation.AggregateID, currency, audit)
		}
	}
	return nil
}

func validateCausalVersion(ctx context.Context, tx pgx.Tx, operation SyncOperation) (SyncOutcome, string, error) {
	var maximum *int64
	if err := tx.QueryRow(ctx, `SELECT MAX(aggregate_version) FROM domain_events WHERE tenant_id=$1 AND aggregate_type=$2 AND aggregate_id=$3`, operation.TenantID, operation.AggregateType, operation.AggregateID).Scan(&maximum); err != nil {
		return "", "", fmt.Errorf("postgres sync: inspect aggregate version: %w", err)
	}
	if maximum == nil {
		if operation.CommandType == "order.create" && operation.AggregateVersion == 1 {
			return "", "", nil
		}
		if operation.CommandType == "kitchenTicket.transition" && operation.AggregateVersion == 2 {
			var envelope struct {
				Payload struct {
					OrderID string `json:"orderId"`
				} `json:"payload"`
			}
			if json.Unmarshal(operation.Mutation, &envelope) == nil && envelope.Payload.OrderID != "" {
				var exists bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domain_events WHERE tenant_id=$1 AND aggregate_type='order' AND aggregate_id=$2 AND aggregate_version=1)`, operation.TenantID, envelope.Payload.OrderID).Scan(&exists); err != nil {
					return "", "", err
				}
				if exists {
					return "", "", nil
				}
			}
		}
		return "", "", fmt.Errorf("%w: aggregate %s/%s requires version before %d", ErrCausalPredecessor, operation.AggregateType, operation.AggregateID, operation.AggregateVersion)
	}
	expected := uint64(*maximum) + 1
	if operation.AggregateVersion > expected {
		return "", "", fmt.Errorf("%w: aggregate %s/%s expects %d, received %d", ErrCausalPredecessor, operation.AggregateType, operation.AggregateID, expected, operation.AggregateVersion)
	}
	if operation.AggregateVersion < expected {
		return SyncConflict, "aggregate_version_stale", nil
	}
	return "", "", nil
}

func existingSyncOutcome(ctx context.Context, tx pgx.Tx, operation SyncOperation) (SyncOutcome, string, error) {
	var requestHash []byte
	var status string
	var problemCode *string
	err := tx.QueryRow(ctx, `
		SELECT request_hash, status, problem_code
		FROM sync_inbox
		WHERE tenant_id = $1 AND operation_id = $2
	`, operation.TenantID, operation.OperationID).Scan(&requestHash, &status, &problemCode)
	if err != nil {
		return "", "", fmt.Errorf("postgres sync: inspect duplicate: %w", err)
	}
	if !bytes.Equal(requestHash, operation.RequestHash) {
		return SyncConflict, "operation_id_reused", nil
	}
	problem := ""
	if problemCode != nil {
		problem = *problemCode
	}
	switch status {
	case "accepted":
		return SyncDuplicate, "", nil
	case "rejected":
		return SyncRejected, problem, nil
	case "conflict":
		return SyncConflict, problem, nil
	default:
		return "", "", errors.New("postgres sync: operation is not terminal")
	}
}

func syncCommandProblem(operation SyncOperation) string {
	expectedAggregate := map[string]string{
		"order.create":                "order",
		"order.transition":            "order",
		"kitchenTicket.transition":    "kitchenTicket",
		"kitchenTicket.transitionAll": "order",
	}[operation.CommandType]
	if expectedAggregate == "" {
		return "unsupported_command_type"
	}
	if operation.AggregateType != expectedAggregate {
		return "command_aggregate_mismatch"
	}
	return ""
}

func mutationOccurredAt(operation SyncOperation) time.Time {
	var envelope struct {
		OccurredAt time.Time `json:"occurredAt"`
	}
	if err := json.Unmarshal(operation.Mutation, &envelope); err == nil && !envelope.OccurredAt.IsZero() {
		return envelope.OccurredAt
	}
	return operation.ReceivedAt
}

func operationRecordedAt(operation SyncOperation) time.Time {
	if !operation.RecordedAt.IsZero() {
		return operation.RecordedAt
	}
	return operation.ReceivedAt
}
