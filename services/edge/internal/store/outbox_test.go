// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/edge/internal/model"
)

const (
	outboxTestTenant = "tenant-outbox"
	outboxTestOutlet = "outlet-outbox"
)

func TestPendingOperationsPreservesOrderCausalityAcrossRetry(t *testing.T) {
	repository, err := Open(filepath.Join(t.TempDir(), "edge.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { repository.Close() })

	clock := time.Date(2027, time.March, 4, 5, 6, 7, 0, time.UTC)
	repository.now = func() time.Time { return clock }
	first := appendOrderAndTicketTransition(t, repository, &clock, "first")
	second := appendOrderAndTicketTransition(t, repository, &clock, "second")

	initial := pendingOperationIDs(t, repository, 10, clock.Add(time.Minute))
	assertOperationIDs(t, initial, first.createID, second.createID)

	attemptedAt := clock.Add(2 * time.Minute)
	if err := repository.RecordSyncResults(t.Context(), initial, []model.PushResult{
		{OperationID: first.createID, Status: model.PushRetry, ProblemCode: "cloud_busy", RetryAfterSeconds: 60},
		{OperationID: second.createID, Status: model.PushAccepted},
	}, attemptedAt); err != nil {
		t.Fatalf("record mixed cloud results: %v", err)
	}

	// The first order's transition is blocked behind its backed-off create, but
	// the independent second order can continue immediately.
	independent := pendingOperationIDs(t, repository, 10, attemptedAt.Add(time.Second))
	assertOperationIDs(t, independent, second.transitionID)
	if err := repository.RecordSyncResults(t.Context(), independent, []model.PushResult{{
		OperationID: second.transitionID,
		Status:      model.PushAccepted,
	}}, attemptedAt.Add(time.Second)); err != nil {
		t.Fatalf("acknowledge independent transition: %v", err)
	}

	beforeRetry := pendingOperationIDs(t, repository, 10, attemptedAt.Add(59*time.Second))
	assertOperationIDs(t, beforeRetry)

	retryDue := attemptedAt.Add(60 * time.Second)
	retrying := pendingOperationIDs(t, repository, 10, retryDue)
	assertOperationIDs(t, retrying, first.createID)
	if err := repository.RecordSyncResults(t.Context(), retrying, []model.PushResult{{
		OperationID: first.createID,
		Status:      model.PushAccepted,
	}}, retryDue); err != nil {
		t.Fatalf("acknowledge retried create: %v", err)
	}

	dependent := pendingOperationIDs(t, repository, 10, retryDue)
	assertOperationIDs(t, dependent, first.transitionID)
}

func TestVersionOneMigrationBackfillsTicketOrderCausality(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "edge-v1.db")
	database, err := sql.Open("sqlite", sqliteDSN(databasePath))
	if err != nil {
		t.Fatalf("open version one database: %v", err)
	}
	createID := model.NewUUIDv7()
	transitionID := model.NewUUIDv7()
	orderID := model.NewUUIDv7()
	ticketID := model.NewUUIDv7()
	recordedAt := time.Date(2027, time.April, 5, 6, 7, 8, 0, time.UTC)
	mutation, err := json.Marshal(outboxMutation(struct{}{}, recordedAt))
	if err != nil {
		t.Fatalf("marshal migration mutation: %v", err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{query: `CREATE TABLE kitchen_tickets (id TEXT PRIMARY KEY, order_id TEXT NOT NULL)`},
		{query: `CREATE TABLE operations (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			operation_id TEXT NOT NULL UNIQUE,
			tenant_id TEXT NOT NULL,
			outlet_id TEXT NOT NULL,
			aggregate_type TEXT NOT NULL,
			aggregate_id TEXT NOT NULL,
			aggregate_version INTEGER NOT NULL,
			command_type TEXT NOT NULL,
			mutation_json BLOB NOT NULL,
			recorded_at TEXT NOT NULL
		)`},
		{query: `CREATE TABLE outbox (
			operation_id TEXT PRIMARY KEY REFERENCES operations(operation_id),
			state TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TEXT NOT NULL,
			last_error TEXT,
			cloud_result TEXT,
			synced_at TEXT
		)`},
		{query: `INSERT INTO kitchen_tickets (id, order_id) VALUES (?, ?)`, args: []any{ticketID, orderID}},
		{query: `INSERT INTO operations (operation_id, tenant_id, outlet_id, aggregate_type, aggregate_id, aggregate_version, command_type, mutation_json, recorded_at) VALUES (?, ?, ?, 'order', ?, 1, 'order.create', ?, ?)`, args: []any{createID, outboxTestTenant, outboxTestOutlet, orderID, mutation, formatTime(recordedAt)}},
		{query: `INSERT INTO operations (operation_id, tenant_id, outlet_id, aggregate_type, aggregate_id, aggregate_version, command_type, mutation_json, recorded_at) VALUES (?, ?, ?, 'kitchenTicket', ?, 2, 'kitchenTicket.transition', ?, ?)`, args: []any{transitionID, outboxTestTenant, outboxTestOutlet, ticketID, mutation, formatTime(recordedAt.Add(time.Second))}},
		{query: `INSERT INTO outbox (operation_id, state, next_attempt_at) VALUES (?, 'pending', ?)`, args: []any{createID, formatTime(recordedAt)}},
		{query: `INSERT INTO outbox (operation_id, state, next_attempt_at) VALUES (?, 'pending', ?)`, args: []any{transitionID, formatTime(recordedAt.Add(time.Second))}},
		{query: `PRAGMA user_version = 1`},
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement.query, statement.args...); err != nil {
			database.Close()
			t.Fatalf("prepare version one database with %q: %v", statement.query, err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close version one database: %v", err)
	}

	repository, err := Open(databasePath)
	if err != nil {
		t.Fatalf("migrate version one store: %v", err)
	}
	t.Cleanup(func() { repository.Close() })
	var version int
	if err := repository.db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read migrated schema version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}
	var causalityType, causalityID string
	if err := repository.db.QueryRowContext(t.Context(), `SELECT causality_type, causality_id FROM operations WHERE operation_id = ?`, transitionID).Scan(&causalityType, &causalityID); err != nil {
		t.Fatalf("read migrated ticket causality: %v", err)
	}
	if causalityType != "order" || causalityID != orderID {
		t.Fatalf("ticket causality = %s/%s, want order/%s", causalityType, causalityID, orderID)
	}
	assertOperationIDs(t, pendingOperationIDs(t, repository, 10, recordedAt.Add(time.Minute)), createID)
}

type orderOutboxOperations struct {
	createID     string
	transitionID string
}

func appendOrderAndTicketTransition(t *testing.T, repository *Store, clock *time.Time, label string) orderOutboxOperations {
	t.Helper()
	order := model.NewOrder{
		ID:       model.NewUUIDv7(),
		Type:     model.OrderTypeTakeaway,
		PlacedAt: *clock,
		Lines: []model.OrderLine{{
			ID: model.NewUUIDv7(), Name: label + " rice", Quantity: 1, StationID: "hot",
		}},
	}
	create := outboxMutation(model.CreateOrderPayload{Order: order}, *clock)
	response, err := repository.CreateOrder(t.Context(), "test.create", "hash-"+create.ID, create, order)
	if err != nil {
		t.Fatalf("create %s order: %v", label, err)
	}
	var created struct {
		Data model.CreateOrderResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &created); err != nil {
		t.Fatalf("decode %s order response: %v", label, err)
	}
	if len(created.Data.Tickets) != 1 {
		t.Fatalf("%s order tickets = %d, want 1", label, len(created.Data.Tickets))
	}

	*clock = clock.Add(time.Second)
	transitionInput := model.TransitionTicketPayload{ToStatus: model.TicketStatusFired, ExpectedVersion: 1}
	transition := outboxMutation(transitionInput, *clock)
	if _, err := repository.TransitionTicket(
		t.Context(),
		"test.transition-ticket",
		"hash-"+transition.ID,
		created.Data.Tickets[0].ID,
		transition,
		transitionInput,
	); err != nil {
		t.Fatalf("transition %s ticket: %v", label, err)
	}
	*clock = clock.Add(time.Second)
	return orderOutboxOperations{createID: create.ID, transitionID: transition.ID}
}

func outboxMutation(payload any, occurredAt time.Time) model.MutationEnvelope {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	id := model.NewUUIDv7()
	return model.MutationEnvelope{
		ID: id, TenantID: outboxTestTenant, OutletID: outboxTestOutlet,
		DeviceID: "device-outbox", ActorID: "actor-outbox", OccurredAt: occurredAt,
		Source: "outbox-test", SchemaVersion: model.CurrentSchemaVersion,
		IdempotencyKey: "idem-" + id, Payload: raw,
	}
}

func pendingOperationIDs(t *testing.T, repository *Store, limit int, now time.Time) []string {
	t.Helper()
	operations, err := repository.PendingOperations(t.Context(), limit, now)
	if err != nil {
		t.Fatalf("list pending operations: %v", err)
	}
	ids := make([]string, len(operations))
	for index := range operations {
		ids[index] = operations[index].OperationID
	}
	return ids
}

func assertOperationIDs(t *testing.T, actual []string, expected ...string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("operation ids = %v, want %v", actual, expected)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("operation ids = %v, want %v", actual, expected)
		}
	}
}
