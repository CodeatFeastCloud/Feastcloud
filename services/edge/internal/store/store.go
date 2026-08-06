// SPDX-License-Identifier: AGPL-3.0-only

// Package store persists live outlet state and the synchronization outbox in SQLite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/feastcloud/feastcloud/services/edge/internal/model"
	_ "modernc.org/sqlite"
)

const schemaVersion = 3

type Store struct {
	db    *sql.DB
	now   func() time.Time
	newID func() string
}

type CommandError struct {
	Code    string
	Message string
	Details map[string]string
}

func (failure *CommandError) Error() string { return failure.Message }

type StoredResponse struct {
	StatusCode  int
	ContentType string
	Location    string
	Body        []byte
	Replayed    bool
}

type SyncStats struct {
	Pending        int        `json:"pending"`
	Reconciliation int        `json:"reconciliation"`
	Synchronized   int        `json:"synchronized"`
	LastAttemptAt  *time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt  *time.Time `json:"lastSuccessAt,omitempty"`
	LastError      string     `json:"lastError,omitempty"`
}

// Open initializes a durable SQLite store. The path is a filesystem path, or
// ":memory:" for tests. A single connection deliberately serializes local writers.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store: database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("store: create database directory: %w", err)
		}
	}

	dsn := sqliteDSN(path)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	store := &Store{db: database, now: time.Now, newID: model.NewUUIDv7}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("store: ping sqlite: %w", err)
	}
	if err := store.migrate(ctx); err != nil {
		database.Close()
		return nil, err
	}
	if path != ":memory:" {
		if err := os.Chmod(path, 0o600); err != nil {
			database.Close()
			return nil, fmt.Errorf("store: restrict database permissions: %w", err)
		}
	}
	return store, nil
}

func sqliteDSN(path string) string {
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	if path == ":memory:" {
		query.Set("mode", "memory")
		query.Set("cache", "shared")
		return "file:feastcloud-edge-memory?" + query.Encode()
	}
	query.Add("_pragma", "journal_mode(WAL)")
	location := &url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}
	return location.String()
}

func (store *Store) migrate(ctx context.Context) error {
	var current int
	if err := store.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if current > schemaVersion {
		return fmt.Errorf("store: database schema %d is newer than supported schema %d", current, schemaVersion)
	}
	if current == schemaVersion {
		return nil
	}
	if current == 1 {
		if err := store.migrateOperationCausality(ctx); err != nil {
			return err
		}
		current = 2
	}
	if current == 2 {
		return store.migrateLocalAuthentication(ctx)
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin migration: %w", err)
	}
	defer tx.Rollback()

	statements := []string{
		`CREATE TABLE orders (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			outlet_id TEXT NOT NULL,
			external_ref TEXT,
			order_number INTEGER NOT NULL,
			status TEXT NOT NULL,
			version INTEGER NOT NULL,
			data_json BLOB NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (tenant_id, outlet_id, order_number),
			UNIQUE (tenant_id, outlet_id, external_ref)
		)`,
		`CREATE INDEX orders_scope_status_idx ON orders (tenant_id, outlet_id, status, updated_at DESC)`,
		`CREATE TABLE kitchen_tickets (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			outlet_id TEXT NOT NULL,
			order_id TEXT NOT NULL REFERENCES orders(id),
			station_id TEXT NOT NULL,
			status TEXT NOT NULL,
			priority INTEGER NOT NULL,
			version INTEGER NOT NULL,
			data_json BLOB NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (order_id, station_id)
		)`,
		`CREATE INDEX tickets_station_status_idx ON kitchen_tickets (tenant_id, outlet_id, station_id, status, priority DESC, created_at)`,
		`CREATE TABLE operations (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			operation_id TEXT NOT NULL UNIQUE,
			tenant_id TEXT NOT NULL,
			outlet_id TEXT NOT NULL,
			aggregate_type TEXT NOT NULL,
			aggregate_id TEXT NOT NULL,
			causality_type TEXT NOT NULL,
			causality_id TEXT NOT NULL,
			aggregate_version INTEGER NOT NULL,
			command_type TEXT NOT NULL,
			mutation_json BLOB NOT NULL,
			recorded_at TEXT NOT NULL
		)`,
		`CREATE INDEX operations_causality_sequence_idx ON operations (tenant_id, outlet_id, causality_type, causality_id, sequence)`,
		`CREATE TABLE outbox (
			operation_id TEXT PRIMARY KEY REFERENCES operations(operation_id),
			state TEXT NOT NULL CHECK (state IN ('pending', 'synchronized', 'reconciliation')),
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TEXT NOT NULL,
			last_error TEXT,
			cloud_result TEXT,
			synced_at TEXT
		)`,
		`CREATE INDEX outbox_delivery_idx ON outbox (state, next_attempt_at, operation_id)`,
		`CREATE TABLE idempotency_records (
			tenant_id TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			route TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_hash TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			content_type TEXT NOT NULL,
			location TEXT,
			response_body BLOB NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (tenant_id, actor_id, route, idempotency_key)
		)`,
		`CREATE TABLE order_transitions (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id TEXT NOT NULL REFERENCES orders(id),
			operation_id TEXT NOT NULL REFERENCES operations(operation_id),
			from_status TEXT,
			to_status TEXT NOT NULL,
			version INTEGER NOT NULL,
			recorded_at TEXT NOT NULL
		)`,
		`CREATE TABLE ticket_transitions (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			ticket_id TEXT NOT NULL REFERENCES kitchen_tickets(id),
			operation_id TEXT NOT NULL REFERENCES operations(operation_id),
			from_status TEXT,
			to_status TEXT NOT NULL,
			version INTEGER NOT NULL,
			recorded_at TEXT NOT NULL
		)`,
		`CREATE TABLE sync_metadata (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			last_attempt_at TEXT,
			last_success_at TEXT,
			last_error TEXT
		)`,
		`INSERT INTO sync_metadata (id) VALUES (1)`,
		`CREATE TABLE pairing_codes (
			code_hash TEXT PRIMARY KEY,
			role TEXT NOT NULL CHECK (role IN ('manager', 'cashier', 'chef')),
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			consumed_at TEXT
		)`,
		`CREATE TABLE local_sessions (
			token_hash TEXT PRIMARY KEY,
			role TEXT NOT NULL CHECK (role IN ('manager', 'cashier', 'chef')),
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT
		)`,
		`CREATE INDEX local_sessions_expiry_idx ON local_sessions (expires_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("store: apply schema: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("store: record schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit schema: %w", err)
	}
	return nil
}

func (store *Store) migrateLocalAuthentication(ctx context.Context) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin local authentication migration: %w", err)
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE pairing_codes (
			code_hash TEXT PRIMARY KEY,
			role TEXT NOT NULL CHECK (role IN ('manager', 'cashier', 'chef')),
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			consumed_at TEXT
		)`,
		`CREATE TABLE local_sessions (
			token_hash TEXT PRIMARY KEY,
			role TEXT NOT NULL CHECK (role IN ('manager', 'cashier', 'chef')),
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT
		)`,
		`CREATE INDEX local_sessions_expiry_idx ON local_sessions (expires_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("store: apply local authentication migration: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("store: record local authentication schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit local authentication migration: %w", err)
	}
	return nil
}

// migrateOperationCausality adds an explicit parent stream to existing
// operation logs. Kitchen-ticket commands belong to their order's stream so a
// delayed order.create can never be overtaken by a later ticket transition.
func (store *Store) migrateOperationCausality(ctx context.Context) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin causality migration: %w", err)
	}
	defer tx.Rollback()

	statements := []string{
		`ALTER TABLE operations ADD COLUMN causality_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE operations ADD COLUMN causality_id TEXT NOT NULL DEFAULT ''`,
		`UPDATE operations
			SET causality_type = CASE
				WHEN aggregate_type IN ('order', 'kitchenTicket') THEN 'order'
				ELSE aggregate_type
			END,
			causality_id = CASE
				WHEN aggregate_type = 'kitchenTicket' THEN COALESCE(
					(SELECT ticket.order_id FROM kitchen_tickets AS ticket WHERE ticket.id = operations.aggregate_id),
					operations.aggregate_id
				)
				ELSE aggregate_id
			END`,
		`CREATE INDEX operations_causality_sequence_idx ON operations (tenant_id, outlet_id, causality_type, causality_id, sequence)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("store: apply causality migration: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
		return fmt.Errorf("store: record causality schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit causality migration: %w", err)
	}
	return nil
}

func (store *Store) Close() error { return store.db.Close() }

func (store *Store) Ping(ctx context.Context) error { return store.db.PingContext(ctx) }

func (store *Store) CreateOrder(ctx context.Context, route, requestHash string, envelope model.MutationEnvelope, input model.NewOrder) (StoredResponse, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return StoredResponse{}, err
	}
	defer tx.Rollback()

	if response, found, err := lookupIdempotency(ctx, tx, route, requestHash, envelope); err != nil {
		return StoredResponse{}, err
	} else if found {
		return response, nil
	}
	if err := ensureOperationUnused(ctx, tx, envelope.ID); err != nil {
		return StoredResponse{}, err
	}
	if exists, err := rowExists(ctx, tx, `SELECT 1 FROM orders WHERE id = ?`, input.ID); err != nil {
		return StoredResponse{}, err
	} else if exists {
		return StoredResponse{}, conflict("order_id_reused", "order id already exists", "id", input.ID)
	}
	if input.ExternalRef != "" {
		if exists, err := rowExists(ctx, tx, `SELECT 1 FROM orders WHERE tenant_id = ? AND outlet_id = ? AND external_ref = ?`, envelope.TenantID, envelope.OutletID, input.ExternalRef); err != nil {
			return StoredResponse{}, err
		} else if exists {
			return StoredResponse{}, conflict("duplicate_source_order", "external order reference already exists", "externalRef", input.ExternalRef)
		}
	}

	now := store.now().UTC()
	placedAt := input.PlacedAt.UTC()
	if input.PlacedAt.IsZero() {
		placedAt = envelope.OccurredAt.UTC()
	}
	var number int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(order_number), 0) + 1 FROM orders WHERE tenant_id = ? AND outlet_id = ?`, envelope.TenantID, envelope.OutletID).Scan(&number); err != nil {
		return StoredResponse{}, fmt.Errorf("store: allocate order number: %w", err)
	}
	order := model.Order{
		ID: input.ID, TenantID: envelope.TenantID, OutletID: envelope.OutletID,
		BrandID: input.BrandID, ExternalRef: input.ExternalRef, Number: number,
		GuestName: input.GuestName, TableLabel: input.TableLabel, Note: input.Note,
		Type: input.Type, Status: model.OrderStatusReceived, Lines: input.Lines,
		PlacedAt: placedAt, TargetAt: input.TargetAt, CreatedAt: now, UpdatedAt: now, Version: 1,
	}

	stationLines := make(map[string][]string)
	stationOrder := make([]string, 0)
	for _, line := range input.Lines {
		if _, exists := stationLines[line.StationID]; !exists {
			stationOrder = append(stationOrder, line.StationID)
		}
		stationLines[line.StationID] = append(stationLines[line.StationID], line.ID)
	}
	tickets := make([]model.KitchenTicket, 0, len(stationOrder))
	for _, stationID := range stationOrder {
		ticketID := input.StationTicketIDs[stationID]
		if ticketID == "" {
			ticketID = store.newID()
		}
		if exists, err := rowExists(ctx, tx, `SELECT 1 FROM kitchen_tickets WHERE id = ?`, ticketID); err != nil {
			return StoredResponse{}, err
		} else if exists {
			return StoredResponse{}, conflict("ticket_id_reused", "kitchen ticket id already exists", "id", ticketID)
		}
		tickets = append(tickets, model.KitchenTicket{
			ID: ticketID, TenantID: envelope.TenantID, OutletID: envelope.OutletID,
			OrderID: order.ID, StationID: stationID, LineIDs: stationLines[stationID],
			Status: model.TicketStatusQueued, Priority: input.Priority, TargetAt: input.TargetAt,
			CreatedAt: now, UpdatedAt: now, Version: 1,
		})
	}

	if err := appendOperation(ctx, tx, envelope, "order", order.ID, "order", order.ID, order.Version, "order.create", now); err != nil {
		return StoredResponse{}, err
	}
	if err := insertOrder(ctx, tx, order); err != nil {
		return StoredResponse{}, err
	}
	if err := insertOrderTransition(ctx, tx, order.ID, envelope.ID, "", string(order.Status), order.Version, now); err != nil {
		return StoredResponse{}, err
	}
	for _, ticket := range tickets {
		if err := insertTicket(ctx, tx, ticket); err != nil {
			return StoredResponse{}, err
		}
		if err := insertTicketTransition(ctx, tx, ticket.ID, envelope.ID, "", string(ticket.Status), ticket.Version, now); err != nil {
			return StoredResponse{}, err
		}
	}

	response, err := responseFor(201, "/api/v1/orders/"+url.PathEscape(order.ID), envelope.ID, model.CreateOrderResult{Order: order, Tickets: tickets})
	if err != nil {
		return StoredResponse{}, err
	}
	if err := saveIdempotency(ctx, tx, route, requestHash, envelope, response, now); err != nil {
		return StoredResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return StoredResponse{}, fmt.Errorf("store: commit order: %w", err)
	}
	return response, nil
}

func (store *Store) TransitionOrder(ctx context.Context, route, requestHash, orderID string, envelope model.MutationEnvelope, input model.TransitionOrderPayload) (StoredResponse, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return StoredResponse{}, err
	}
	defer tx.Rollback()
	if response, found, err := lookupIdempotency(ctx, tx, route, requestHash, envelope); err != nil {
		return StoredResponse{}, err
	} else if found {
		return response, nil
	}
	if err := ensureOperationUnused(ctx, tx, envelope.ID); err != nil {
		return StoredResponse{}, err
	}
	order, err := getOrder(ctx, tx, envelope.TenantID, envelope.OutletID, orderID)
	if err != nil {
		return StoredResponse{}, err
	}
	if order.Version != input.ExpectedVersion {
		return StoredResponse{}, versionConflict(input.ExpectedVersion, order.Version)
	}
	if !model.CanTransitionOrder(order.Status, input.ToStatus) {
		return StoredResponse{}, invalidTransition(string(order.Status), string(input.ToStatus))
	}
	tickets, err := listTicketsForOrder(ctx, tx, envelope.TenantID, envelope.OutletID, order.ID)
	if err != nil {
		return StoredResponse{}, err
	}
	// Validate the complete aggregate before appending the operation or changing
	// either projection. A ticket may already be at the mapped state, but every
	// ticket that would change must take one legal KDS state-machine step.
	for _, ticket := range tickets {
		desired, change := ticketStatusForOrder(input.ToStatus, ticket.Status)
		if change && !model.CanTransitionTicket(ticket.Status, desired) {
			return StoredResponse{}, invalidTransition(string(ticket.Status), string(desired))
		}
	}
	now := store.now().UTC()
	fromOrderStatus := order.Status
	order.Status = input.ToStatus
	order.Version++
	order.UpdatedAt = now
	if err := appendOperation(ctx, tx, envelope, "order", order.ID, "order", order.ID, order.Version, "order.transition", now); err != nil {
		return StoredResponse{}, err
	}
	if err := updateOrder(ctx, tx, order); err != nil {
		return StoredResponse{}, err
	}
	if err := insertOrderTransition(ctx, tx, order.ID, envelope.ID, string(fromOrderStatus), string(order.Status), order.Version, now); err != nil {
		return StoredResponse{}, err
	}
	for index := range tickets {
		desired, change := ticketStatusForOrder(input.ToStatus, tickets[index].Status)
		if !change {
			continue
		}
		from := tickets[index].Status
		tickets[index].Status = desired
		tickets[index].Version++
		tickets[index].UpdatedAt = now
		if err := updateTicket(ctx, tx, tickets[index]); err != nil {
			return StoredResponse{}, err
		}
		if err := insertTicketTransition(ctx, tx, tickets[index].ID, envelope.ID, string(from), string(desired), tickets[index].Version, now); err != nil {
			return StoredResponse{}, err
		}
	}
	response, err := responseFor(200, "", envelope.ID, model.TransitionOrderResult{Order: order, Tickets: tickets})
	if err != nil {
		return StoredResponse{}, err
	}
	if err := saveIdempotency(ctx, tx, route, requestHash, envelope, response, now); err != nil {
		return StoredResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return StoredResponse{}, fmt.Errorf("store: commit order transition: %w", err)
	}
	return response, nil
}

func (store *Store) TransitionTicket(ctx context.Context, route, requestHash, ticketID string, envelope model.MutationEnvelope, input model.TransitionTicketPayload) (StoredResponse, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return StoredResponse{}, err
	}
	defer tx.Rollback()
	if response, found, err := lookupIdempotency(ctx, tx, route, requestHash, envelope); err != nil {
		return StoredResponse{}, err
	} else if found {
		return response, nil
	}
	if err := ensureOperationUnused(ctx, tx, envelope.ID); err != nil {
		return StoredResponse{}, err
	}
	ticket, err := getTicket(ctx, tx, envelope.TenantID, envelope.OutletID, ticketID)
	if err != nil {
		return StoredResponse{}, err
	}
	if input.ExpectedOrderID != "" && ticket.OrderID != input.ExpectedOrderID {
		return StoredResponse{}, conflict("ticket_order_mismatch", "kitchen ticket does not belong to orderId", "orderId", input.ExpectedOrderID)
	}
	if ticket.Version != input.ExpectedVersion {
		return StoredResponse{}, versionConflict(input.ExpectedVersion, ticket.Version)
	}
	if !model.CanTransitionTicket(ticket.Status, input.ToStatus) {
		return StoredResponse{}, invalidTransition(string(ticket.Status), string(input.ToStatus))
	}
	order, err := getOrder(ctx, tx, envelope.TenantID, envelope.OutletID, ticket.OrderID)
	if err != nil {
		return StoredResponse{}, err
	}
	now := store.now().UTC()
	fromTicketStatus := ticket.Status
	ticket.Status = input.ToStatus
	ticket.Version++
	ticket.UpdatedAt = now
	if err := appendOperation(ctx, tx, envelope, "kitchenTicket", ticket.ID, "order", order.ID, ticket.Version, "kitchenTicket.transition", now); err != nil {
		return StoredResponse{}, err
	}
	if err := updateTicket(ctx, tx, ticket); err != nil {
		return StoredResponse{}, err
	}
	if err := insertTicketTransition(ctx, tx, ticket.ID, envelope.ID, string(fromTicketStatus), string(ticket.Status), ticket.Version, now); err != nil {
		return StoredResponse{}, err
	}

	tickets, err := listTicketsForOrder(ctx, tx, envelope.TenantID, envelope.OutletID, order.ID)
	if err != nil {
		return StoredResponse{}, err
	}
	candidate := deriveOrderStatus(tickets)
	if shouldAdvanceOrder(order.Status, candidate) {
		from := order.Status
		order.Status = candidate
		order.Version++
		order.UpdatedAt = now
		if err := updateOrder(ctx, tx, order); err != nil {
			return StoredResponse{}, err
		}
		if err := insertOrderTransition(ctx, tx, order.ID, envelope.ID, string(from), string(order.Status), order.Version, now); err != nil {
			return StoredResponse{}, err
		}
	}
	response, err := responseFor(200, "", envelope.ID, model.TransitionTicketResult{Ticket: ticket, Order: order})
	if err != nil {
		return StoredResponse{}, err
	}
	if err := saveIdempotency(ctx, tx, route, requestHash, envelope, response, now); err != nil {
		return StoredResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return StoredResponse{}, fmt.Errorf("store: commit ticket transition: %w", err)
	}
	return response, nil
}

// TransitionOrderTickets applies one PWA KDS event to every ticket belonging
// to an order. Tickets already at the requested state are left unchanged, while
// skipping a state is rejected for the complete transaction.
func (store *Store) TransitionOrderTickets(ctx context.Context, route, requestHash, orderID string, envelope model.MutationEnvelope, toStatus model.TicketStatus, expectedOrderVersion uint64) (StoredResponse, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return StoredResponse{}, err
	}
	defer tx.Rollback()
	if response, found, err := lookupIdempotency(ctx, tx, route, requestHash, envelope); err != nil {
		return StoredResponse{}, err
	} else if found {
		return response, nil
	}
	if err := ensureOperationUnused(ctx, tx, envelope.ID); err != nil {
		return StoredResponse{}, err
	}
	order, err := getOrder(ctx, tx, envelope.TenantID, envelope.OutletID, orderID)
	if err != nil {
		return StoredResponse{}, err
	}
	if expectedOrderVersion == 0 {
		return StoredResponse{}, versionConflict(expectedOrderVersion, order.Version)
	}
	if order.Version != expectedOrderVersion {
		return StoredResponse{}, versionConflict(expectedOrderVersion, order.Version)
	}
	tickets, err := listTicketsForOrder(ctx, tx, envelope.TenantID, envelope.OutletID, order.ID)
	if err != nil {
		return StoredResponse{}, err
	}
	changed := false
	changedTickets := make([]bool, len(tickets))
	previousStatuses := make([]model.TicketStatus, len(tickets))
	for index, ticket := range tickets {
		previousStatuses[index] = ticket.Status
		if ticket.Status == toStatus {
			continue
		}
		if !model.CanTransitionTicket(ticket.Status, toStatus) {
			return StoredResponse{}, invalidTransition(string(ticket.Status), string(toStatus))
		}
		changed = true
		changedTickets[index] = true
	}
	if !changed {
		return StoredResponse{}, invalidTransition(string(toStatus), string(toStatus))
	}

	now := store.now().UTC()
	for index := range tickets {
		if !changedTickets[index] {
			continue
		}
		tickets[index].Status = toStatus
		tickets[index].Version++
		tickets[index].UpdatedAt = now
	}
	candidate := deriveOrderStatus(tickets)
	orderChanged := shouldAdvanceOrder(order.Status, candidate)
	aggregateVersion := order.Version
	if orderChanged {
		aggregateVersion++
	}
	if err := appendOperation(ctx, tx, envelope, "order", order.ID, "order", order.ID, aggregateVersion, "kitchenTicket.transitionAll", now); err != nil {
		return StoredResponse{}, err
	}
	for index := range tickets {
		if !changedTickets[index] {
			continue
		}
		if err := updateTicket(ctx, tx, tickets[index]); err != nil {
			return StoredResponse{}, err
		}
		if err := insertTicketTransition(ctx, tx, tickets[index].ID, envelope.ID, string(previousStatuses[index]), string(toStatus), tickets[index].Version, now); err != nil {
			return StoredResponse{}, err
		}
	}
	if orderChanged {
		from := order.Status
		order.Status = candidate
		order.Version++
		order.UpdatedAt = now
		if err := updateOrder(ctx, tx, order); err != nil {
			return StoredResponse{}, err
		}
		if err := insertOrderTransition(ctx, tx, order.ID, envelope.ID, string(from), string(order.Status), order.Version, now); err != nil {
			return StoredResponse{}, err
		}
	}
	response, err := responseFor(200, "", envelope.ID, model.TransitionOrderResult{Order: order, Tickets: tickets})
	if err != nil {
		return StoredResponse{}, err
	}
	if err := saveIdempotency(ctx, tx, route, requestHash, envelope, response, now); err != nil {
		return StoredResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return StoredResponse{}, fmt.Errorf("store: commit order ticket transition: %w", err)
	}
	return response, nil
}

func ticketStatusForOrder(status model.OrderStatus, current model.TicketStatus) (model.TicketStatus, bool) {
	if current == model.TicketStatusCancelled || current == model.TicketStatusCompleted {
		return current, false
	}
	var desired model.TicketStatus
	switch status {
	case model.OrderStatusAccepted:
		desired = model.TicketStatusFired
	case model.OrderStatusPreparing:
		desired = model.TicketStatusPreparing
	case model.OrderStatusReady:
		desired = model.TicketStatusReady
	case model.OrderStatusCompleted:
		desired = model.TicketStatusCompleted
	case model.OrderStatusCancelled:
		desired = model.TicketStatusCancelled
	default:
		return current, false
	}
	return desired, desired != current
}

func deriveOrderStatus(tickets []model.KitchenTicket) model.OrderStatus {
	if len(tickets) == 0 {
		return model.OrderStatusReceived
	}
	allCancelled := true
	allTerminal := true
	allReady := true
	hasCompleted := false
	hasPreparing := false
	hasFired := false
	for _, ticket := range tickets {
		if ticket.Status != model.TicketStatusCancelled {
			allCancelled = false
		}
		if ticket.Status != model.TicketStatusCompleted && ticket.Status != model.TicketStatusCancelled {
			allTerminal = false
		}
		if ticket.Status != model.TicketStatusReady && ticket.Status != model.TicketStatusCompleted && ticket.Status != model.TicketStatusCancelled {
			allReady = false
		}
		switch ticket.Status {
		case model.TicketStatusCompleted:
			hasCompleted = true
		case model.TicketStatusPreparing, model.TicketStatusReady:
			hasPreparing = true
		case model.TicketStatusFired:
			hasFired = true
		}
	}
	if allCancelled {
		return model.OrderStatusCancelled
	}
	if allTerminal && hasCompleted {
		return model.OrderStatusCompleted
	}
	if allReady {
		return model.OrderStatusReady
	}
	if hasPreparing || hasCompleted {
		return model.OrderStatusPreparing
	}
	if hasFired {
		return model.OrderStatusAccepted
	}
	return model.OrderStatusReceived
}

func shouldAdvanceOrder(current, candidate model.OrderStatus) bool {
	if candidate == model.OrderStatusCancelled {
		return current != model.OrderStatusCancelled && current != model.OrderStatusCompleted
	}
	rank := map[model.OrderStatus]int{
		model.OrderStatusReceived: 0, model.OrderStatusAccepted: 1, model.OrderStatusPreparing: 2,
		model.OrderStatusReady: 3, model.OrderStatusCompleted: 4,
	}
	return rank[candidate] > rank[current]
}

func (store *Store) GetOrder(ctx context.Context, tenantID, outletID, id string) (model.Order, error) {
	return getOrder(ctx, store.db, tenantID, outletID, id)
}

func (store *Store) ListOrders(ctx context.Context, tenantID, outletID string, status model.OrderStatus, limit int) ([]model.Order, error) {
	query := `SELECT data_json FROM orders WHERE tenant_id = ? AND outlet_id = ?`
	arguments := []any{tenantID, outletID}
	if status != "" {
		query += ` AND status = ?`
		arguments = append(arguments, status)
	}
	query += ` ORDER BY updated_at DESC, order_number DESC LIMIT ?`
	arguments = append(arguments, limit)
	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("store: list orders: %w", err)
	}
	defer rows.Close()
	orders := make([]model.Order, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var order model.Order
		if err := json.Unmarshal(raw, &order); err != nil {
			return nil, fmt.Errorf("store: decode order projection: %w", err)
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func (store *Store) GetTicket(ctx context.Context, tenantID, outletID, id string) (model.KitchenTicket, error) {
	return getTicket(ctx, store.db, tenantID, outletID, id)
}

func (store *Store) ListTickets(ctx context.Context, tenantID, outletID, stationID string, status model.TicketStatus, limit int) ([]model.KitchenTicket, error) {
	query := `SELECT data_json FROM kitchen_tickets WHERE tenant_id = ? AND outlet_id = ?`
	arguments := []any{tenantID, outletID}
	if stationID != "" {
		query += ` AND station_id = ?`
		arguments = append(arguments, stationID)
	}
	if status != "" {
		query += ` AND status = ?`
		arguments = append(arguments, status)
	}
	query += ` ORDER BY priority DESC, created_at, id LIMIT ?`
	arguments = append(arguments, limit)
	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("store: list tickets: %w", err)
	}
	defer rows.Close()
	tickets := make([]model.KitchenTicket, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ticket model.KitchenTicket
		if err := json.Unmarshal(raw, &ticket); err != nil {
			return nil, fmt.Errorf("store: decode ticket projection: %w", err)
		}
		tickets = append(tickets, ticket)
	}
	return tickets, rows.Err()
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getOrder(ctx context.Context, query rowQuerier, tenantID, outletID, id string) (model.Order, error) {
	var raw []byte
	err := query.QueryRowContext(ctx, `SELECT data_json FROM orders WHERE tenant_id = ? AND outlet_id = ? AND id = ?`, tenantID, outletID, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Order{}, notFound("order", id)
	}
	if err != nil {
		return model.Order{}, fmt.Errorf("store: get order: %w", err)
	}
	var order model.Order
	if err := json.Unmarshal(raw, &order); err != nil {
		return model.Order{}, fmt.Errorf("store: decode order projection: %w", err)
	}
	return order, nil
}

func getTicket(ctx context.Context, query rowQuerier, tenantID, outletID, id string) (model.KitchenTicket, error) {
	var raw []byte
	err := query.QueryRowContext(ctx, `SELECT data_json FROM kitchen_tickets WHERE tenant_id = ? AND outlet_id = ? AND id = ?`, tenantID, outletID, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return model.KitchenTicket{}, notFound("kitchen ticket", id)
	}
	if err != nil {
		return model.KitchenTicket{}, fmt.Errorf("store: get ticket: %w", err)
	}
	var ticket model.KitchenTicket
	if err := json.Unmarshal(raw, &ticket); err != nil {
		return model.KitchenTicket{}, fmt.Errorf("store: decode ticket projection: %w", err)
	}
	return ticket, nil
}

func listTicketsForOrder(ctx context.Context, tx *sql.Tx, tenantID, outletID, orderID string) ([]model.KitchenTicket, error) {
	rows, err := tx.QueryContext(ctx, `SELECT data_json FROM kitchen_tickets WHERE tenant_id = ? AND outlet_id = ? AND order_id = ? ORDER BY created_at, id`, tenantID, outletID, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tickets []model.KitchenTicket
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ticket model.KitchenTicket
		if err := json.Unmarshal(raw, &ticket); err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	return tickets, rows.Err()
}

func insertOrder(ctx context.Context, tx *sql.Tx, order model.Order) error {
	raw, err := json.Marshal(order)
	if err != nil {
		return err
	}
	var external any
	if order.ExternalRef != "" {
		external = order.ExternalRef
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO orders (id, tenant_id, outlet_id, external_ref, order_number, status, version, data_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		order.ID, order.TenantID, order.OutletID, external, order.Number, order.Status, order.Version, raw, formatTime(order.CreatedAt), formatTime(order.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: insert order: %w", err)
	}
	return nil
}

func updateOrder(ctx context.Context, tx *sql.Tx, order model.Order) error {
	raw, err := json.Marshal(order)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE orders SET status = ?, version = ?, data_json = ?, updated_at = ? WHERE id = ? AND tenant_id = ? AND outlet_id = ?`,
		order.Status, order.Version, raw, formatTime(order.UpdatedAt), order.ID, order.TenantID, order.OutletID)
	if err != nil {
		return fmt.Errorf("store: update order: %w", err)
	}
	return requireOne(result, "order projection")
}

func insertTicket(ctx context.Context, tx *sql.Tx, ticket model.KitchenTicket) error {
	raw, err := json.Marshal(ticket)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO kitchen_tickets (id, tenant_id, outlet_id, order_id, station_id, status, priority, version, data_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ticket.ID, ticket.TenantID, ticket.OutletID, ticket.OrderID, ticket.StationID, ticket.Status, ticket.Priority, ticket.Version, raw, formatTime(ticket.CreatedAt), formatTime(ticket.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: insert ticket: %w", err)
	}
	return nil
}

func updateTicket(ctx context.Context, tx *sql.Tx, ticket model.KitchenTicket) error {
	raw, err := json.Marshal(ticket)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE kitchen_tickets SET status = ?, version = ?, data_json = ?, updated_at = ? WHERE id = ? AND tenant_id = ? AND outlet_id = ?`,
		ticket.Status, ticket.Version, raw, formatTime(ticket.UpdatedAt), ticket.ID, ticket.TenantID, ticket.OutletID)
	if err != nil {
		return fmt.Errorf("store: update ticket: %w", err)
	}
	return requireOne(result, "ticket projection")
}

func requireOne(result sql.Result, target string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("store: expected one affected %s, got %d", target, count)
	}
	return nil
}

func appendOperation(ctx context.Context, tx *sql.Tx, envelope model.MutationEnvelope, aggregateType, aggregateID, causalityType, causalityID string, aggregateVersion uint64, commandType string, recordedAt time.Time) error {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operations (operation_id, tenant_id, outlet_id, aggregate_type, aggregate_id, causality_type, causality_id, aggregate_version, command_type, mutation_json, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		envelope.ID, envelope.TenantID, envelope.OutletID, aggregateType, aggregateID, causalityType, causalityID, aggregateVersion, commandType, raw, formatTime(recordedAt))
	if err != nil {
		return fmt.Errorf("store: append operation: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox (operation_id, state, next_attempt_at) VALUES (?, 'pending', ?)`, envelope.ID, formatTime(recordedAt))
	if err != nil {
		return fmt.Errorf("store: enqueue operation: %w", err)
	}
	return nil
}

func insertOrderTransition(ctx context.Context, tx *sql.Tx, orderID, operationID, from, to string, version uint64, recordedAt time.Time) error {
	var fromValue any
	if from != "" {
		fromValue = from
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO order_transitions (order_id, operation_id, from_status, to_status, version, recorded_at) VALUES (?, ?, ?, ?, ?, ?)`, orderID, operationID, fromValue, to, version, formatTime(recordedAt))
	return err
}

func insertTicketTransition(ctx context.Context, tx *sql.Tx, ticketID, operationID, from, to string, version uint64, recordedAt time.Time) error {
	var fromValue any
	if from != "" {
		fromValue = from
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO ticket_transitions (ticket_id, operation_id, from_status, to_status, version, recorded_at) VALUES (?, ?, ?, ?, ?, ?)`, ticketID, operationID, fromValue, to, version, formatTime(recordedAt))
	return err
}

func responseFor(status int, location, operationID string, data any) (StoredResponse, error) {
	body, err := json.Marshal(model.ResponseEnvelope{Data: data, Meta: model.ResponseMeta{OperationID: operationID}})
	if err != nil {
		return StoredResponse{}, err
	}
	body = append(body, '\n')
	return StoredResponse{StatusCode: status, ContentType: "application/json", Location: location, Body: body}, nil
}

func lookupIdempotency(ctx context.Context, tx *sql.Tx, route, requestHash string, envelope model.MutationEnvelope) (StoredResponse, bool, error) {
	var storedHash, contentType string
	var statusCode int
	var location sql.NullString
	var body []byte
	err := tx.QueryRowContext(ctx, `SELECT request_hash, status_code, content_type, location, response_body FROM idempotency_records WHERE tenant_id = ? AND actor_id = ? AND route = ? AND idempotency_key = ?`,
		envelope.TenantID, envelope.ActorID, route, envelope.IdempotencyKey).Scan(&storedHash, &statusCode, &contentType, &location, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredResponse{}, false, nil
	}
	if err != nil {
		return StoredResponse{}, false, fmt.Errorf("store: read idempotency record: %w", err)
	}
	if storedHash != requestHash {
		return StoredResponse{}, false, conflict("idempotency_key_reused", "idempotency key was already used with a different command", "idempotencyKey", envelope.IdempotencyKey)
	}
	return StoredResponse{StatusCode: statusCode, ContentType: contentType, Location: location.String, Body: body, Replayed: true}, true, nil
}

func saveIdempotency(ctx context.Context, tx *sql.Tx, route, requestHash string, envelope model.MutationEnvelope, response StoredResponse, now time.Time) error {
	var location any
	if response.Location != "" {
		location = response.Location
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records (tenant_id, actor_id, route, idempotency_key, request_hash, status_code, content_type, location, response_body, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		envelope.TenantID, envelope.ActorID, route, envelope.IdempotencyKey, requestHash, response.StatusCode, response.ContentType, location, response.Body, formatTime(now))
	if err != nil {
		return fmt.Errorf("store: save idempotency response: %w", err)
	}
	return nil
}

func ensureOperationUnused(ctx context.Context, tx *sql.Tx, operationID string) error {
	exists, err := rowExists(ctx, tx, `SELECT 1 FROM operations WHERE operation_id = ?`, operationID)
	if err != nil {
		return err
	}
	if exists {
		return conflict("operation_id_reused", "operation id was already used", "id", operationID)
	}
	return nil
}

func rowExists(ctx context.Context, tx *sql.Tx, query string, arguments ...any) (bool, error) {
	var marker int
	err := tx.QueryRowContext(ctx, query, arguments...).Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func conflict(code, message, field, value string) *CommandError {
	return &CommandError{Code: code, Message: message, Details: map[string]string{field: value}}
}

func notFound(kind, id string) *CommandError {
	return &CommandError{Code: "not_found", Message: kind + " was not found", Details: map[string]string{"id": id}}
}

func invalidTransition(from, to string) *CommandError {
	return &CommandError{Code: "invalid_transition", Message: "requested state transition is not allowed", Details: map[string]string{"fromStatus": from, "toStatus": to}}
}

func versionConflict(expected, actual uint64) *CommandError {
	return &CommandError{Code: "version_conflict", Message: "aggregate version does not match expectedVersion", Details: map[string]string{"expectedVersion": fmt.Sprint(expected), "actualVersion": fmt.Sprint(actual)}}
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// PendingOperations returns due causal heads in durable sequence order. Only
// the earliest unresolved operation for a parent stream is eligible, ensuring
// a retried or reconciliation-bound predecessor cannot be overtaken. Distinct
// parent streams remain independently eligible.
func (store *Store) PendingOperations(ctx context.Context, limit int, now time.Time) ([]model.Operation, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT o.operation_id, o.aggregate_type, o.aggregate_id, o.aggregate_version, o.command_type, o.mutation_json, o.recorded_at
		FROM operations o JOIN outbox b ON b.operation_id = o.operation_id
		WHERE b.state = 'pending' AND b.next_attempt_at <= ?
			AND NOT EXISTS (
				SELECT 1
				FROM operations predecessor
				JOIN outbox predecessor_outbox ON predecessor_outbox.operation_id = predecessor.operation_id
				WHERE predecessor.tenant_id = o.tenant_id
					AND predecessor.outlet_id = o.outlet_id
					AND predecessor.causality_type = o.causality_type
					AND predecessor.causality_id = o.causality_id
					AND predecessor.sequence < o.sequence
					AND predecessor_outbox.state != 'synchronized'
			)
		ORDER BY o.sequence LIMIT ?`, formatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("store: list pending operations: %w", err)
	}
	defer rows.Close()
	operations := make([]model.Operation, 0)
	for rows.Next() {
		var operation model.Operation
		var mutationRaw []byte
		var recordedAt string
		if err := rows.Scan(&operation.OperationID, &operation.AggregateType, &operation.AggregateID, &operation.AggregateVersion, &operation.CommandType, &mutationRaw, &recordedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(mutationRaw, &operation.Mutation); err != nil {
			return nil, fmt.Errorf("store: decode pending mutation: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, recordedAt)
		if err != nil {
			return nil, fmt.Errorf("store: decode operation time: %w", err)
		}
		operation.RecordedAt = parsed
		operations = append(operations, operation)
	}
	return operations, rows.Err()
}

// RecordSyncResults applies one cloud result per pushed operation atomically.
func (store *Store) RecordSyncResults(ctx context.Context, operationIDs []string, results []model.PushResult, attemptedAt time.Time) error {
	if len(operationIDs) == 0 {
		return nil
	}
	resultByID := make(map[string]model.PushResult, len(results))
	for _, result := range results {
		resultByID[result.OperationID] = result
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	successfulBatch := true
	retryProblem := ""
	for _, operationID := range operationIDs {
		result, exists := resultByID[operationID]
		if !exists {
			result = model.PushResult{OperationID: operationID, Status: model.PushRetry, ProblemCode: "missing_cloud_result"}
		}
		switch result.Status {
		case model.PushAccepted, model.PushDuplicate:
			if _, err := tx.ExecContext(ctx, `UPDATE outbox SET state = 'synchronized', attempts = attempts + 1, last_error = NULL, cloud_result = ?, synced_at = ? WHERE operation_id = ? AND state = 'pending'`, result.Status, formatTime(attemptedAt), operationID); err != nil {
				return err
			}
		case model.PushRejected, model.PushConflict:
			successfulBatch = false
			problem := conciseSyncProblem(result.ProblemCode, "cloud_"+strings.ToLower(string(result.Status)))
			if _, err := tx.ExecContext(ctx, `UPDATE outbox SET state = 'reconciliation', attempts = attempts + 1, last_error = ?, cloud_result = ? WHERE operation_id = ? AND state = 'pending'`, problem, result.Status, operationID); err != nil {
				return err
			}
		default:
			successfulBatch = false
			problem := conciseSyncProblem(result.ProblemCode, "cloud_retry")
			if retryProblem == "" {
				retryProblem = problem
			}
			delay := time.Duration(result.RetryAfterSeconds) * time.Second
			if delay <= 0 || delay > 15*time.Minute {
				delay = time.Second
			}
			if _, err := tx.ExecContext(ctx, `UPDATE outbox SET attempts = attempts + 1, next_attempt_at = ?, last_error = ?, cloud_result = ? WHERE operation_id = ? AND state = 'pending'`, formatTime(attemptedAt.Add(delay)), problem, model.PushRetry, operationID); err != nil {
				return err
			}
		}
	}
	switch {
	case successfulBatch:
		_, err = tx.ExecContext(ctx, `UPDATE sync_metadata SET last_attempt_at = ?, last_success_at = ?, last_error = NULL WHERE id = 1`, formatTime(attemptedAt), formatTime(attemptedAt))
	case retryProblem != "":
		_, err = tx.ExecContext(ctx, `UPDATE sync_metadata SET last_attempt_at = ?, last_error = ? WHERE id = 1`, formatTime(attemptedAt), retryProblem)
	default:
		// Rejected/conflicting operations are visible through reconciliation and
		// must not advance the timestamp of the last fully successful batch.
		_, err = tx.ExecContext(ctx, `UPDATE sync_metadata SET last_attempt_at = ?, last_error = NULL WHERE id = 1`, formatTime(attemptedAt))
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func conciseSyncProblem(problem, fallback string) string {
	problem = strings.TrimSpace(problem)
	if problem == "" {
		problem = fallback
	}
	if len(problem) > 256 {
		problem = problem[:256]
	}
	return problem
}

// RecordSyncFailure preserves pending operations and schedules a bounded retry.
func (store *Store) RecordSyncFailure(ctx context.Context, operationIDs []string, message string, attemptedAt time.Time) error {
	if len(message) > 1_000 {
		message = message[:1_000]
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, operationID := range operationIDs {
		var attempts int
		if err := tx.QueryRowContext(ctx, `SELECT attempts FROM outbox WHERE operation_id = ? AND state = 'pending'`, operationID).Scan(&attempts); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return err
		}
		delay := retryDelay(attempts + 1)
		if _, err := tx.ExecContext(ctx, `UPDATE outbox SET attempts = attempts + 1, next_attempt_at = ?, last_error = ? WHERE operation_id = ? AND state = 'pending'`, formatTime(attemptedAt.Add(delay)), message, operationID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sync_metadata SET last_attempt_at = ?, last_error = ? WHERE id = 1`, formatTime(attemptedAt), message); err != nil {
		return err
	}
	return tx.Commit()
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 9 {
		attempt = 9
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}

func (store *Store) SyncStats(ctx context.Context) (SyncStats, error) {
	var stats SyncStats
	err := store.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN state = 'pending' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN state = 'reconciliation' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN state = 'synchronized' THEN 1 ELSE 0 END), 0)
		FROM outbox`).Scan(&stats.Pending, &stats.Reconciliation, &stats.Synchronized)
	if err != nil {
		return SyncStats{}, fmt.Errorf("store: count outbox: %w", err)
	}
	var attempt, success, lastError sql.NullString
	if err := store.db.QueryRowContext(ctx, `SELECT last_attempt_at, last_success_at, last_error FROM sync_metadata WHERE id = 1`).Scan(&attempt, &success, &lastError); err != nil {
		return SyncStats{}, fmt.Errorf("store: read sync metadata: %w", err)
	}
	stats.LastAttemptAt, err = parseOptionalTime(attempt)
	if err != nil {
		return SyncStats{}, err
	}
	stats.LastSuccessAt, err = parseOptionalTime(success)
	if err != nil {
		return SyncStats{}, err
	}
	stats.LastError = lastError.String
	return stats, nil
}
