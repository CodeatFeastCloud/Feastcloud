// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/auth"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

const (
	integrationTenantA = "11111111-1111-4111-8111-111111111111"
	integrationTenantB = "22222222-2222-4222-8222-222222222222"
	integrationOutletA = "33333333-3333-4333-8333-333333333333"
	integrationOutletB = "55555555-5555-4555-8555-555555555555"
)

func TestPostgresSyncRepositoryIntegration(t *testing.T) {
	databaseURL := os.Getenv("FEASTCLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set FEASTCLOUD_TEST_DATABASE_URL to run PostgreSQL integration coverage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repository, err := NewPostgresSyncRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL sync repository: %v", err)
	}
	if err := repository.Ready(ctx); err != nil {
		repository.Close()
		t.Fatalf("PostgreSQL sync readiness: %v", err)
	}
	assertRestrictedRuntimeRole(t, ctx, repository)

	operation := integrationOperation(integrationTenantA, integrationOutletA, "order.create", "order")
	outcome, problem, err := repository.ApplySyncOperation(ctx, operation)
	if err != nil || outcome != SyncAccepted || problem != "" {
		t.Fatalf("first apply = %q/%q/%v; want ACCEPTED", outcome, problem, err)
	}
	assertOperationEvidence(t, ctx, repository, integrationTenantA, operation.OperationID, "accepted", 1)

	outcome, problem, err = repository.ApplySyncOperation(ctx, operation)
	if err != nil || outcome != SyncDuplicate || problem != "" {
		t.Fatalf("exact replay = %q/%q/%v; want DUPLICATE", outcome, problem, err)
	}
	conflict := operation
	conflict.RequestHash = append([]byte(nil), operation.RequestHash...)
	conflict.RequestHash[0] ^= 0xff
	outcome, problem, err = repository.ApplySyncOperation(ctx, conflict)
	if err != nil || outcome != SyncConflict || problem != "operation_id_reused" {
		t.Fatalf("conflicting replay = %q/%q/%v; want CONFLICT/operation_id_reused", outcome, problem, err)
	}
	assertOperationEvidence(t, ctx, repository, integrationTenantA, operation.OperationID, "accepted", 1)

	rejected := integrationOperation(integrationTenantA, integrationOutletA, "inventory.adjust", "inventory")
	outcome, problem, err = repository.ApplySyncOperation(ctx, rejected)
	if err != nil || outcome != SyncRejected || problem != "unsupported_command_type" {
		t.Fatalf("unsupported command = %q/%q/%v; want REJECTED", outcome, problem, err)
	}
	assertOperationEvidence(t, ctx, repository, integrationTenantA, rejected.OperationID, "rejected", 0)

	missingOutlet := integrationOperation(integrationTenantA, newIntegrationUUID(t), "order.create", "order")
	if _, _, err := repository.ApplySyncOperation(ctx, missingOutlet); err == nil {
		t.Fatal("operation for an unknown outlet unexpectedly committed")
	}
	assertOperationEvidence(t, ctx, repository, integrationTenantA, missingOutlet.OperationID, "", 0)

	concurrent := integrationOperation(integrationTenantA, integrationOutletA, "order.create", "order")
	assertConcurrentExactlyOnce(t, ctx, repository, concurrent)

	tenantBOperation := integrationOperation(integrationTenantB, integrationOutletB, "order.create", "order")
	if outcome, _, err := repository.ApplySyncOperation(ctx, tenantBOperation); err != nil || outcome != SyncAccepted {
		t.Fatalf("tenant B apply = %q/%v; want ACCEPTED", outcome, err)
	}
	assertTenantIsolation(t, ctx, repository, tenantBOperation.OperationID)
	assertDomainEventCannotBeMutated(t, ctx, repository, operation.OperationID)
	assertAdversarialCausalityAndClockSkew(t, ctx, repository)

	repository.Close()
	restarted, err := NewPostgresSyncRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("restart PostgreSQL sync repository: %v", err)
	}
	defer restarted.Close()
	outcome, _, err = restarted.ApplySyncOperation(ctx, operation)
	if err != nil || outcome != SyncDuplicate {
		t.Fatalf("replay after repository restart = %q/%v; want DUPLICATE", outcome, err)
	}
}

func assertAdversarialCausalityAndClockSkew(t *testing.T, ctx context.Context, repository *PostgresSyncRepository) {
	t.Helper()
	aggregateID := newIntegrationUUID(t)
	create := integrationOperation(integrationTenantA, integrationOutletA, "order.create", "order")
	create.AggregateID = aggregateID
	create.AggregateVersion = 1
	create.Mutation = json.RawMessage(`{"occurredAt":"2035-01-01T00:00:00Z","payload":{"test":true}}`)
	create.RequestHash = []byte(create.OperationID + "-create")
	second := integrationOperation(integrationTenantA, integrationOutletA, "order.transition", "order")
	second.AggregateID = aggregateID
	second.AggregateVersion = 2
	second.Mutation = json.RawMessage(`{"occurredAt":"2020-01-01T00:00:00Z","payload":{"test":true}}`)
	second.RequestHash = []byte(second.OperationID + "-second")
	third := integrationOperation(integrationTenantA, integrationOutletA, "order.transition", "order")
	third.AggregateID = aggregateID
	third.AggregateVersion = 3
	third.RequestHash = []byte(third.OperationID + "-third")
	if _, _, err := repository.ApplySyncOperation(ctx, third); !errors.Is(err, ErrCausalPredecessor) {
		t.Fatalf("version three before predecessors error=%v; want causal predecessor", err)
	}
	assertOperationEvidence(t, ctx, repository, integrationTenantA, third.OperationID, "", 0)
	for _, operation := range []SyncOperation{create, second, third} {
		outcome, problem, err := repository.ApplySyncOperation(ctx, operation)
		if err != nil || outcome != SyncAccepted || problem != "" {
			t.Fatalf("ordered causal apply version %d = %s/%s/%v", operation.AggregateVersion, outcome, problem, err)
		}
	}
	stale := integrationOperation(integrationTenantA, integrationOutletA, "order.transition", "order")
	stale.AggregateID = aggregateID
	stale.AggregateVersion = 2
	outcome, problem, err := repository.ApplySyncOperation(ctx, stale)
	if err != nil || outcome != SyncConflict || problem != "aggregate_version_stale" {
		t.Fatalf("stale causal apply=%s/%s/%v", outcome, problem, err)
	}
}

func TestPostgresResourceRepositoryIntegration(t *testing.T) {
	databaseURL := os.Getenv("FEASTCLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set FEASTCLOUD_TEST_DATABASE_URL to run PostgreSQL integration coverage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repository, err := NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL repository: %v", err)
	}
	assertRestrictedRuntimeRole(t, ctx, repository.PostgresSyncRepository)

	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	brandID := newIntegrationUUID(t)
	stationID := newIntegrationUUID(t)
	orderID := newIntegrationUUID(t)
	lineID := newIntegrationUUID(t)
	ticketID := newIntegrationUUID(t)
	codeSuffix := strings.ReplaceAll(brandID[:8], "-", "")

	brand := domain.Brand{
		ID: brandID, TenantID: integrationTenantA, OrganizationID: integrationTenantA,
		Name: "Durable integration brand", Code: "B" + codeSuffix, Active: true,
		RecordMetadata: domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	if err := repository.CreateBrand(ctx, brand, integrationAudit(t, integrationTenantA, integrationOutletA, "brand", brandID, "brand.created", now)); err != nil {
		t.Fatalf("create brand: %v", err)
	}
	assignment, err := repository.SetBrandOutletAssignment(ctx, domain.BrandOutletAssignment{
		TenantID: integrationTenantA, BrandID: brandID, OutletID: integrationOutletA, Active: true,
		RecordMetadata: domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1},
	}, 0, integrationAudit(t, integrationTenantA, integrationOutletA, "brand_outlet_assignment", brandID, "brand_outlet_assignment.saved", now))
	if err != nil || assignment.Version != 1 || !assignment.Active {
		t.Fatalf("create brand rollout=%#v/%v", assignment, err)
	}
	if _, err := repository.SetBrandOutletAssignment(ctx, assignment, 0, integrationAudit(t, integrationTenantA, integrationOutletA, "brand_outlet_assignment", brandID, "brand_outlet_assignment.saved", now.Add(time.Second))); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate brand rollout error=%v; want conflict", err)
	}
	assignment.Active = false
	assignment.UpdatedAt = now.Add(2 * time.Second)
	pausedAssignment, err := repository.SetBrandOutletAssignment(ctx, assignment, 1, integrationAudit(t, integrationTenantA, integrationOutletA, "brand_outlet_assignment", brandID, "brand_outlet_assignment.saved", now.Add(2*time.Second)))
	if err != nil || pausedAssignment.Version != 2 || pausedAssignment.Active {
		t.Fatalf("pause brand rollout=%#v/%v", pausedAssignment, err)
	}
	if _, err := repository.SetBrandOutletAssignment(ctx, assignment, 1, integrationAudit(t, integrationTenantA, integrationOutletA, "brand_outlet_assignment", brandID, "brand_outlet_assignment.saved", now.Add(3*time.Second))); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale brand rollout error=%v; want version conflict", err)
	}
	assignments, err := repository.BrandOutletAssignments(ctx, integrationTenantA)
	if err != nil || !containsBrandOutletAssignment(assignments, brandID, integrationOutletA, false) {
		t.Fatalf("brand rollout listing=%#v/%v", assignments, err)
	}
	station := domain.Station{
		ID: stationID, TenantID: integrationTenantA, OutletID: integrationOutletA,
		Name: "Durable hot line", Code: "S" + codeSuffix, Type: domain.StationTypeCooking, Active: true,
		RecordMetadata: domain.RecordMetadata{CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second), Version: 1},
	}
	if err := repository.CreateStation(ctx, station, integrationAudit(t, integrationTenantA, integrationOutletA, "station", stationID, "station.created", now.Add(time.Second))); err != nil {
		t.Fatalf("create station: %v", err)
	}
	order := domain.Order{
		ID: orderID, TenantID: integrationTenantA, OutletID: integrationOutletA, BrandID: brandID,
		ExternalRef: "PG-" + codeSuffix, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusReceived,
		Lines: []domain.OrderLine{{
			ID: lineID, Name: "Durable bowl", Quantity: 2,
			UnitPrice: domain.Money{MinorUnits: 25000, Currency: "INR"},
			LineTotal: domain.Money{MinorUnits: 50000, Currency: "INR"},
		}},
		Subtotal:      domain.Money{MinorUnits: 50000, Currency: "INR"},
		DiscountTotal: domain.Money{Currency: "INR"}, TaxTotal: domain.Money{MinorUnits: 2500, Currency: "INR"},
		ServiceCharge: domain.Money{Currency: "INR"}, Total: domain.Money{MinorUnits: 52500, Currency: "INR"},
		PlacedAt: now, RecordMetadata: domain.RecordMetadata{CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second), Version: 1},
	}
	orderAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "order", orderID, "order.created", now.Add(2*time.Second))
	if err := repository.CreateOrder(ctx, order, orderAudit); err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := repository.CreateOrder(ctx, order, integrationAudit(t, integrationTenantA, integrationOutletA, "order", orderID, "order.created", now.Add(3*time.Second))); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate order error = %v; want conflict", err)
	}
	ticket := domain.KitchenTicket{
		ID: ticketID, TenantID: integrationTenantA, OutletID: integrationOutletA, OrderID: orderID,
		StationID: stationID, LineIDs: []string{lineID}, Status: domain.TicketStatusQueued, Priority: 25,
		RecordMetadata: domain.RecordMetadata{CreatedAt: now.Add(4 * time.Second), UpdatedAt: now.Add(4 * time.Second), Version: 1},
	}
	if err := repository.CreateKitchenTicket(ctx, ticket, integrationAudit(t, integrationTenantA, integrationOutletA, "kitchen_ticket", ticketID, "kitchen_ticket.created", now.Add(4*time.Second))); err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	transitionedOrder, err := repository.TransitionOrder(ctx, integrationTenantA, integrationOutletA, orderID, domain.OrderStatusAccepted, 1, integrationAudit(t, integrationTenantA, integrationOutletA, "order", orderID, "order.status_changed", now.Add(5*time.Second)))
	if err != nil || transitionedOrder.Version != 2 || transitionedOrder.Status != domain.OrderStatusAccepted {
		t.Fatalf("transition order=%#v/%v", transitionedOrder, err)
	}
	if _, err := repository.TransitionOrder(ctx, integrationTenantA, integrationOutletA, orderID, domain.OrderStatusPreparing, 1, integrationAudit(t, integrationTenantA, integrationOutletA, "order", orderID, "order.status_changed", now.Add(6*time.Second))); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale order transition error=%v", err)
	}
	transitionedTicket, err := repository.TransitionKitchenTicket(ctx, integrationTenantA, integrationOutletA, ticketID, domain.TicketStatusFired, 1, integrationAudit(t, integrationTenantA, integrationOutletA, "kitchen_ticket", ticketID, "kitchen_ticket.status_changed", now.Add(7*time.Second)))
	if err != nil || transitionedTicket.Version != 2 || transitionedTicket.Status != domain.TicketStatusFired {
		t.Fatalf("transition ticket=%#v/%v", transitionedTicket, err)
	}
	secondOrder := order
	secondOrder.ID = newIntegrationUUID(t)
	secondOrder.ExternalRef = "PG2-" + codeSuffix
	secondOrder.Lines = append([]domain.OrderLine(nil), order.Lines...)
	secondOrder.Lines[0].ID = newIntegrationUUID(t)
	secondOrder.CreatedAt = now.Add(8 * time.Second)
	secondOrder.UpdatedAt = secondOrder.CreatedAt
	if err := repository.CreateOrder(ctx, secondOrder, integrationAudit(t, integrationTenantA, integrationOutletA, "order", secondOrder.ID, "order.created", secondOrder.CreatedAt)); err != nil {
		t.Fatalf("create second paged order: %v", err)
	}
	firstPage, err := repository.PageOrders(ctx, OrderPageRequest{TenantID: integrationTenantA, OutletID: integrationOutletA, Limit: 1})
	if err != nil || len(firstPage.Values) != 1 || firstPage.Next == nil {
		t.Fatalf("first order page=%#v/%v", firstPage, err)
	}
	secondPage, err := repository.PageOrders(ctx, OrderPageRequest{TenantID: integrationTenantA, OutletID: integrationOutletA, Limit: 1, After: firstPage.Next})
	if err != nil || len(secondPage.Values) != 1 || secondPage.Values[0].ID == firstPage.Values[0].ID {
		t.Fatalf("second order page=%#v/%v", secondPage, err)
	}

	storedOrder, err := repository.Order(ctx, integrationTenantA, orderID)
	if err != nil || len(storedOrder.Lines) != 1 || storedOrder.Total.MinorUnits != 52500 || storedOrder.Version != 2 {
		t.Fatalf("stored order = %#v / %v", storedOrder, err)
	}
	storedTicket, err := repository.KitchenTicket(ctx, integrationTenantA, ticketID)
	if err != nil || len(storedTicket.LineIDs) != 1 || storedTicket.LineIDs[0] != lineID || storedTicket.Version != 2 {
		t.Fatalf("stored ticket = %#v / %v", storedTicket, err)
	}
	if _, err := repository.Order(ctx, integrationTenantB, orderID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant order read error = %v; want not found", err)
	}
	audits, err := repository.AuditEvents(ctx, AuditFilter{TenantID: integrationTenantA, EntityID: orderID})
	if err != nil || len(audits) != 2 || audits[0].OperationID != orderAudit.OperationID || audits[1].Action != "order.status_changed" {
		t.Fatalf("order audits = %#v / %v", audits, err)
	}

	repository.Close()
	restarted, err := NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("restart PostgreSQL repository: %v", err)
	}
	defer restarted.Close()
	persisted, err := restarted.Order(ctx, integrationTenantA, orderID)
	if err != nil || persisted.ExternalRef != order.ExternalRef || len(persisted.Lines) != 1 {
		t.Fatalf("order after repository restart = %#v / %v", persisted, err)
	}

	deviceID := newIntegrationUUID(t)
	fingerprintBytes := sha256.Sum256([]byte(deviceID))
	device := auth.Device{TenantID: integrationTenantA, OutletID: integrationOutletA, EdgeID: "edge-" + deviceID, DeviceID: deviceID, Fingerprint: hex.EncodeToString(fingerprintBytes[:]), Status: "active"}
	if err := restarted.RegisterDevice(ctx, device, "Integration edge", integrationAudit(t, integrationTenantA, integrationOutletA, "identity_device", deviceID, "device.enrolled", now.Add(10*time.Second))); err != nil {
		t.Fatalf("register device: %v", err)
	}
	registered, err := restarted.DeviceByFingerprint(ctx, integrationTenantA, device.Fingerprint)
	if err != nil || registered.DeviceID != deviceID || registered.Status != "active" {
		t.Fatalf("registered device=%#v error=%v", registered, err)
	}
	if err := restarted.RevokeDevice(ctx, integrationTenantA, deviceID, "integration-actor", integrationAudit(t, integrationTenantA, integrationOutletA, "identity_device", deviceID, "device.revoked", now.Add(11*time.Second))); err != nil {
		t.Fatalf("revoke device: %v", err)
	}
	revoked, err := restarted.DeviceByFingerprint(ctx, integrationTenantA, device.Fingerprint)
	if err != nil || revoked.Status != "revoked" {
		t.Fatalf("revoked device=%#v error=%v", revoked, err)
	}
}

func containsBrandOutletAssignment(values []domain.BrandOutletAssignment, brandID, outletID string, active bool) bool {
	for _, value := range values {
		if value.BrandID == brandID && value.OutletID == outletID && value.Active == active {
			return true
		}
	}
	return false
}

func integrationAudit(t *testing.T, tenantID, outletID, entityType, entityID, action string, now time.Time) domain.AuditEvent {
	t.Helper()
	operationID := newIntegrationUUID(t)
	return domain.AuditEvent{
		ID: newIntegrationUUID(t), OperationID: operationID, TenantID: tenantID, OutletID: outletID,
		ActorID: "integration-actor", DeviceID: "integration-device", Source: "integration-test",
		SourceID: operationID, IdempotencyKey: "integration-" + operationID,
		SchemaVersion: domain.CurrentSchemaVersion, Action: action, EntityType: entityType, EntityID: entityID,
		OccurredAt: now, RecordedAt: now,
	}
}

func integrationOperation(tenantID, outletID, commandType, aggregateType string) SyncOperation {
	operationID := mustIntegrationUUID()
	now := time.Date(2026, 8, 3, 8, 0, 0, 0, time.UTC)
	mutation := json.RawMessage(`{"occurredAt":"2026-08-03T07:59:00Z","payload":{"test":true}}`)
	hash := sha256.Sum256(append([]byte(operationID), mutation...))
	return SyncOperation{
		TenantID: tenantID, OperationID: operationID, EdgeID: "edge-integration-1",
		OutletID: outletID, BatchID: "batch-" + operationID,
		AggregateType: aggregateType, AggregateID: mustIntegrationUUID(), AggregateVersion: 1,
		CommandType: commandType, RequestHash: hash[:], Mutation: mutation,
		RecordedAt: now.Add(-24 * time.Hour), ReceivedAt: now,
	}
}

func assertRestrictedRuntimeRole(t *testing.T, ctx context.Context, repository *PostgresSyncRepository) {
	t.Helper()
	var superuser, bypassRLS bool
	if err := repository.pool.QueryRow(ctx, `
		SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user
	`).Scan(&superuser, &bypassRLS); err != nil {
		t.Fatalf("inspect runtime role: %v", err)
	}
	if superuser || bypassRLS {
		t.Fatalf("integration connection is over-privileged: superuser=%t bypassRLS=%t", superuser, bypassRLS)
	}
}

func assertOperationEvidence(
	t *testing.T,
	ctx context.Context,
	repository *PostgresSyncRepository,
	tenantID, operationID, wantStatus string,
	wantEvents int,
) {
	t.Helper()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin evidence query: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		t.Fatalf("scope evidence query: %v", err)
	}
	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM sync_inbox WHERE tenant_id = $1 AND operation_id = $2
	`, tenantID, operationID).Scan(&status)
	if wantStatus == "" {
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("missing operation query error = %v; want no rows", err)
		}
		return
	}
	if err != nil || status != wantStatus {
		t.Fatalf("inbox status = %q/%v; want %q", status, err, wantStatus)
	}
	var events int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM domain_events WHERE tenant_id = $1 AND operation_id = $2
	`, tenantID, operationID).Scan(&events); err != nil {
		t.Fatalf("count domain events: %v", err)
	}
	if events != wantEvents {
		t.Fatalf("domain event count = %d; want %d", events, wantEvents)
	}
}

func assertConcurrentExactlyOnce(
	t *testing.T,
	ctx context.Context,
	repository *PostgresSyncRepository,
	operation SyncOperation,
) {
	t.Helper()
	const callers = 8
	results := make(chan SyncOutcome, callers)
	errorsFound := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			outcome, _, err := repository.ApplySyncOperation(ctx, operation)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- outcome
		}()
	}
	group.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent apply failed: %v", err)
	}
	accepted, duplicates := 0, 0
	for outcome := range results {
		switch outcome {
		case SyncAccepted:
			accepted++
		case SyncDuplicate:
			duplicates++
		default:
			t.Errorf("concurrent outcome = %q", outcome)
		}
	}
	if accepted != 1 || duplicates != callers-1 {
		t.Fatalf("concurrent outcomes accepted=%d duplicates=%d", accepted, duplicates)
	}
	assertOperationEvidence(t, ctx, repository, operation.TenantID, operation.OperationID, "accepted", 1)
}

func assertTenantIsolation(t *testing.T, ctx context.Context, repository *PostgresSyncRepository, tenantBOperationID string) {
	t.Helper()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin isolation query: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, integrationTenantA); err != nil {
		t.Fatalf("scope isolation query: %v", err)
	}
	var visible int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM domain_events WHERE operation_id = $1
	`, tenantBOperationID).Scan(&visible); err != nil {
		t.Fatalf("query cross-tenant event: %v", err)
	}
	if visible != 0 {
		t.Fatalf("tenant A can see %d tenant B events", visible)
	}
}

func assertDomainEventCannotBeMutated(t *testing.T, ctx context.Context, repository *PostgresSyncRepository, operationID string) {
	t.Helper()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin immutable event check: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, integrationTenantA); err != nil {
		t.Fatalf("scope immutable event check: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE domain_events SET command_type = 'tampered' WHERE operation_id = $1
	`, operationID); err == nil {
		t.Fatal("restricted runtime role mutated an append-only domain event")
	}
}

func newIntegrationUUID(t *testing.T) string {
	t.Helper()
	return mustIntegrationUUID()
}

func mustIntegrationUUID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded)
}
