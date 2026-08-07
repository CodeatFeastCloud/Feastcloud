// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

const (
	tenantOne = "11111111-1111-4111-8111-111111111111"
	tenantTwo = "22222222-2222-4222-8222-222222222222"
	outletOne = "33333333-3333-4333-8333-333333333333"
	actorOne  = "manager-one"
	deviceOne = "44444444-4444-4444-8444-444444444444"
)

func TestHealthIsPublic(t *testing.T) {
	t.Parallel()

	server := newTestServer()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200; body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("health response missing ok status: %s", response.Body.String())
	}
}

func TestReadinessFailsClosedWhenRequiredSyncStoreIsUnavailable(t *testing.T) {
	t.Parallel()

	syncRepository := &testSyncRepository{hashes: map[string][]byte{}, readyError: store.ErrSyncUnavailable}
	server := NewServer(store.NewMemoryRepository(), nil, Config{
		SyncRepository: syncRepository, RequireSyncReady: true,
	})
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"dependency":"postgresql_sync"`) {
		t.Fatalf("unavailable readiness status=%d body=%s", response.Code, response.Body.String())
	}

	syncRepository.readyError = nil
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request.Clone(request.Context()))
	if response.Code != http.StatusOK {
		t.Fatalf("restored readiness status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAPIRequiresDemoAuthentication(t *testing.T) {
	t.Parallel()

	server := newTestServer()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401; body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("Content-Type = %q; want application/problem+json", got)
	}
}

func TestMenuImportValidationUsesDecodedPayload(t *testing.T) {
	t.Parallel()

	server := newTestServer()
	metadata := testMetadata("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1", tenantOne, outletOne, actorOne, "menu-import-001")
	body := mutationJSON(t, metadata, map[string]any{
		"id":              "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb1",
		"outletId":        outletOne,
		"name":            "Imported menu",
		"itemFileName":    "items.csv",
		"addonFileName":   "addons.csv",
		"sourceSha256":    strings.Repeat("a", 64),
		"itemCount":       1,
		"categoryCount":   1,
		"addonGroupCount": 0,
		"variationCount":  0,
		"draft":           map[string]any{"items": []any{}, "categories": []any{}, "addonGroups": []any{}},
	})

	response := performMutation(server, "/api/v1/menu-imports", body, tenantOne, actorOne, metadata.IdempotencyKey)
	// The development repository accepts menu imports in memory. A 422 here
	// would mean validation saw a pre-decode zero input.
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201 after decoded validation; body=%s", response.Code, response.Body.String())
	}
}

func TestOrganizationCreationIsIdempotentAndAuditedOnce(t *testing.T) {
	t.Parallel()

	repository := store.NewMemoryRepository()
	server := NewServer(repository, nil, Config{})
	metadata := testMetadata(
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		tenantOne,
		outletOne,
		actorOne,
		"create-org-0001",
	)
	body := mutationJSON(t, metadata, map[string]any{
		"id":              tenantOne,
		"name":            "FeastCloud Test Kitchens",
		"defaultLocale":   "en-IN",
		"defaultCurrency": "INR",
	})

	first := performMutation(server, "/api/v1/organizations", body, tenantOne, actorOne, metadata.IdempotencyKey)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d; want 201; body=%s", first.Code, first.Body.String())
	}
	if first.Header().Get("Location") != "/api/v1/organizations/"+tenantOne {
		t.Fatalf("unexpected Location: %s", first.Header().Get("Location"))
	}

	second := performMutation(server, "/api/v1/organizations", body, tenantOne, actorOne, metadata.IdempotencyKey)
	if second.Code != http.StatusCreated {
		t.Fatalf("replay status = %d; want 201; body=%s", second.Code, second.Body.String())
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("Idempotency-Replayed = %q; want true", second.Header().Get("Idempotency-Replayed"))
	}

	audits, err := repository.AuditEvents(context.Background(), store.AuditFilter{TenantID: tenantOne})
	if err != nil {
		t.Fatalf("read audits: %v", err)
	}
	if len(audits) != 1 {
		t.Fatalf("audit count = %d; want 1", len(audits))
	}
	if audits[0].OperationID != metadata.ID {
		t.Fatalf("audit operationId = %q; want %q", audits[0].OperationID, metadata.ID)
	}
}

func TestPlatformAdminProvisionsAnIsolatedCustomerAtomically(t *testing.T) {
	t.Parallel()
	repository := store.NewMemoryRepository()
	server := NewServer(repository, nil, Config{})
	metadata := testMetadata("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaab1", tenantTwo, "44444444-4444-4444-8444-444444444444", "platform-admin", "platform-tenant-01")
	body := mutationJSON(t, metadata, map[string]any{
		"organizationName": "Northstar Foods", "legalName": "Northstar Foods Pvt Ltd", "ownerName": "Asha Singh", "ownerEmail": "asha@northstar.example",
		"defaultLocale": "en-IN", "defaultCurrency": "INR", "outletName": "Northstar Koramangala", "outletCode": "NORTH-KOR-01", "timeZone": "Asia/Kolkata",
		"brandName": "Northstar Bowls", "brandCode": "NORTHBOWL", "template": "cloud",
	})

	denied := performMutation(server, "/api/v1/platform/tenants", body, tenantTwo, actorOne, metadata.IdempotencyKey)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-platform status=%d body=%s", denied.Code, denied.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/platform/tenants", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", metadata.IdempotencyKey)
	request.Header.Set("X-FeastCloud-Tenant-ID", tenantTwo)
	request.Header.Set("X-FeastCloud-Actor-ID", "platform-admin")
	request.Header.Set("X-FeastCloud-Platform-Admin", "true")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("provision status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status":"identity_invite_pending"`) {
		t.Fatalf("missing truthful owner handoff: %s", response.Body.String())
	}

	organizations, err := repository.Organizations(context.Background(), tenantTwo)
	if err != nil || len(organizations) != 1 {
		t.Fatalf("organizations=%v err=%v", organizations, err)
	}
	outlets, err := repository.Outlets(context.Background(), tenantTwo)
	if err != nil || len(outlets) != 1 {
		t.Fatalf("outlets=%v err=%v", outlets, err)
	}
	brands, err := repository.Brands(context.Background(), tenantTwo)
	if err != nil || len(brands) != 1 {
		t.Fatalf("brands=%v err=%v", brands, err)
	}
	assignments, err := repository.BrandOutletAssignments(context.Background(), tenantTwo)
	if err != nil || len(assignments) != 1 || !assignments[0].Active {
		t.Fatalf("assignments=%v err=%v", assignments, err)
	}
	stations, err := repository.Stations(context.Background(), tenantTwo)
	if err != nil || len(stations) != 3 {
		t.Fatalf("stations=%v err=%v", stations, err)
	}

	replayRequest := httptest.NewRequest(http.MethodPost, "/api/v1/platform/tenants", bytes.NewReader(body))
	replayRequest.Header = request.Header.Clone()
	replay := httptest.NewRecorder()
	server.ServeHTTP(replay, replayRequest)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay status=%d header=%q", replay.Code, replay.Header().Get("Idempotency-Replayed"))
	}
}

func TestIdempotencyKeyCannotBeReusedForDifferentPayload(t *testing.T) {
	t.Parallel()

	server := newTestServer()
	metadata := testMetadata(
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaab",
		tenantOne,
		outletOne,
		actorOne,
		"create-org-0002",
	)
	firstBody := mutationJSON(t, metadata, map[string]any{
		"id": tenantOne, "name": "First Name", "defaultLocale": "en-IN", "defaultCurrency": "INR",
	})
	first := performMutation(server, "/api/v1/organizations", firstBody, tenantOne, actorOne, metadata.IdempotencyKey)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d; want 201; body=%s", first.Code, first.Body.String())
	}

	secondBody := mutationJSON(t, metadata, map[string]any{
		"id": tenantOne, "name": "Changed Name", "defaultLocale": "en-IN", "defaultCurrency": "INR",
	})
	second := performMutation(server, "/api/v1/organizations", secondBody, tenantOne, actorOne, metadata.IdempotencyKey)
	if second.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d; want 409; body=%s", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), `"code":"idempotency_key_reused"`) {
		t.Fatalf("unexpected problem body: %s", second.Body.String())
	}
}

func TestAuthenticatedScopePreventsCrossTenantAccess(t *testing.T) {
	t.Parallel()

	server := newTestServer()
	metadata := testMetadata(
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaac",
		tenantOne,
		outletOne,
		actorOne,
		"create-org-0003",
	)
	body := mutationJSON(t, metadata, map[string]any{
		"id": tenantOne, "name": "Tenant One", "defaultLocale": "en-IN", "defaultCurrency": "INR",
	})
	created := performMutation(server, "/api/v1/organizations", body, tenantOne, actorOne, metadata.IdempotencyKey)
	if created.Code != http.StatusCreated {
		t.Fatalf("setup status = %d; body=%s", created.Code, created.Body.String())
	}

	read := authenticatedRequest(server, http.MethodGet, "/api/v1/organizations/"+tenantOne, nil, tenantTwo, "other-manager")
	if read.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant read status = %d; want 404; body=%s", read.Code, read.Body.String())
	}

	list := authenticatedRequest(server, http.MethodGet, "/api/v1/organizations?tenantId="+tenantOne, nil, tenantTwo, "other-manager")
	if list.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant list status = %d; want 403; body=%s", list.Code, list.Body.String())
	}

	mutation := performMutation(server, "/api/v1/organizations", body, tenantTwo, "other-manager", metadata.IdempotencyKey)
	if mutation.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant mutation status = %d; want 403; body=%s", mutation.Code, mutation.Body.String())
	}
}

func TestKitchenHierarchyCanBeCreatedAndRead(t *testing.T) {
	t.Parallel()

	server := newTestServer()
	createFixtureHierarchy(t, server)

	orders := authenticatedRequest(server, http.MethodGet, "/api/v1/orders?outletId="+outletOne, nil, tenantOne, actorOne)
	if orders.Code != http.StatusOK {
		t.Fatalf("orders status = %d; want 200; body=%s", orders.Code, orders.Body.String())
	}
	if !strings.Contains(orders.Body.String(), `"externalRef":"EDGE-1001"`) {
		t.Fatalf("order missing from response: %s", orders.Body.String())
	}

	tickets := authenticatedRequest(server, http.MethodGet, "/api/v1/kitchen-tickets?outletId="+outletOne, nil, tenantOne, actorOne)
	if tickets.Code != http.StatusOK || !strings.Contains(tickets.Body.String(), `"status":"queued"`) {
		t.Fatalf("ticket response status=%d body=%s", tickets.Code, tickets.Body.String())
	}
}

func TestBrandRolloutIsOutletScopedVersionedAndIdempotent(t *testing.T) {
	t.Parallel()

	server := newTestServer()
	createFixtureHierarchy(t, server)
	const brandID = "55555555-5555-4555-8555-555555555555"
	metadata := testMetadata("10000000-0000-4000-8000-000000000010", tenantOne, outletOne, actorOne, "brand-rollout-01")
	body := mutationJSON(t, metadata, map[string]any{
		"brandId": brandID, "outletId": outletOne, "active": true,
	})

	first := performMutation(server, "/api/v1/brand-outlet-assignments", body, tenantOne, actorOne, metadata.IdempotencyKey)
	if first.Code != http.StatusCreated || !strings.Contains(first.Body.String(), `"active":true`) {
		t.Fatalf("create rollout status=%d body=%s", first.Code, first.Body.String())
	}
	replay := performMutation(server, "/api/v1/brand-outlet-assignments", body, tenantOne, actorOne, metadata.IdempotencyKey)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("rollout replay status=%d replay=%q body=%s", replay.Code, replay.Header().Get("Idempotency-Replayed"), replay.Body.String())
	}

	listed := authenticatedRequest(server, http.MethodGet, "/api/v1/brand-outlet-assignments?outletId="+outletOne, nil, tenantOne, actorOne)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"brandId":"`+brandID+`"`) {
		t.Fatalf("rollout list status=%d body=%s", listed.Code, listed.Body.String())
	}

	pauseMetadata := testMetadata("10000000-0000-4000-8000-000000000011", tenantOne, outletOne, actorOne, "brand-rollout-02")
	pauseBody := mutationJSON(t, pauseMetadata, map[string]any{
		"brandId": brandID, "outletId": outletOne, "active": false, "expectedVersion": 1,
	})
	paused := performMutation(server, "/api/v1/brand-outlet-assignments", pauseBody, tenantOne, actorOne, pauseMetadata.IdempotencyKey)
	if paused.Code != http.StatusOK || !strings.Contains(paused.Body.String(), `"version":2`) {
		t.Fatalf("pause rollout status=%d body=%s", paused.Code, paused.Body.String())
	}

	staleMetadata := testMetadata("10000000-0000-4000-8000-000000000012", tenantOne, outletOne, actorOne, "brand-rollout-stale")
	staleBody := mutationJSON(t, staleMetadata, map[string]any{
		"brandId": brandID, "outletId": outletOne, "active": true, "expectedVersion": 1,
	})
	stale := performMutation(server, "/api/v1/brand-outlet-assignments", staleBody, tenantOne, actorOne, staleMetadata.IdempotencyKey)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"code":"version_conflict"`) {
		t.Fatalf("stale rollout status=%d body=%s", stale.Code, stale.Body.String())
	}
}

func TestOrderAndTicketTransitionsRequireCurrentVersion(t *testing.T) {
	t.Parallel()
	server := newTestServer()
	createFixtureHierarchy(t, server)
	orderMetadata := testMetadata("10000000-0000-4000-8000-000000000007", tenantOne, outletOne, actorOne, "transition-order-01")
	orderBody := mutationJSON(t, orderMetadata, map[string]any{"toStatus": "accepted", "expectedVersion": 1})
	orderResponse := performMutation(server, "/api/v1/orders/77777777-7777-4777-8777-777777777777/transitions", orderBody, tenantOne, actorOne, orderMetadata.IdempotencyKey)
	if orderResponse.Code != http.StatusOK || !strings.Contains(orderResponse.Body.String(), `"status":"accepted"`) || !strings.Contains(orderResponse.Body.String(), `"version":2`) {
		t.Fatalf("order transition status=%d body=%s", orderResponse.Code, orderResponse.Body.String())
	}
	staleMetadata := testMetadata("10000000-0000-4000-8000-000000000008", tenantOne, outletOne, actorOne, "transition-order-stale-01")
	staleBody := mutationJSON(t, staleMetadata, map[string]any{"toStatus": "preparing", "expectedVersion": 1})
	stale := performMutation(server, "/api/v1/orders/77777777-7777-4777-8777-777777777777/transitions", staleBody, tenantOne, actorOne, staleMetadata.IdempotencyKey)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"code":"version_conflict"`) {
		t.Fatalf("stale transition status=%d body=%s", stale.Code, stale.Body.String())
	}
	ticketMetadata := testMetadata("10000000-0000-4000-8000-000000000009", tenantOne, outletOne, actorOne, "transition-ticket-01")
	ticketBody := mutationJSON(t, ticketMetadata, map[string]any{"toStatus": "fired", "expectedVersion": 1})
	ticket := performMutation(server, "/api/v1/kitchen-tickets/99999999-9999-4999-8999-999999999999/transitions", ticketBody, tenantOne, actorOne, ticketMetadata.IdempotencyKey)
	if ticket.Code != http.StatusOK || !strings.Contains(ticket.Body.String(), `"status":"fired"`) || !strings.Contains(ticket.Body.String(), `"version":2`) {
		t.Fatalf("ticket transition status=%d body=%s", ticket.Code, ticket.Body.String())
	}
}

func TestSyncInboxKeepsValidOperationsPendingWithoutDurableRepository(t *testing.T) {
	t.Parallel()

	server := newTestServer()
	operationID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaad"
	metadata := testMetadata(operationID, tenantOne, outletOne, actorOne, "sync-operation-01")
	body := mustJSON(t, map[string]any{
		"batchId":  "batch-1",
		"edgeId":   deviceOne,
		"outletId": outletOne,
		"operations": []any{map[string]any{
			"operationId":      operationID,
			"aggregateType":    "order",
			"aggregateId":      "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
			"aggregateVersion": 1,
			"commandType":      "order.create",
			"recordedAt":       "2026-08-03T10:00:01Z",
			"mutation":         envelopeMap(metadata, map[string]any{}),
		}},
	})

	first := authenticatedRequest(server, http.MethodPost, "/api/v1/sync/operations", body, tenantOne, "edge:"+deviceOne)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"status":"RETRY"`) ||
		!strings.Contains(first.Body.String(), `"problemCode":"sync_inbox_unavailable"`) {
		t.Fatalf("first sync response status=%d body=%s", first.Code, first.Body.String())
	}
	second := authenticatedRequest(server, http.MethodPost, "/api/v1/sync/operations", body, tenantOne, "edge:"+deviceOne)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"status":"RETRY"`) ||
		!strings.Contains(second.Body.String(), `"problemCode":"sync_inbox_unavailable"`) {
		t.Fatalf("second sync response status=%d body=%s", second.Code, second.Body.String())
	}
	for _, response := range []*httptest.ResponseRecorder{first, second} {
		if strings.Contains(response.Body.String(), `"status":"ACCEPTED"`) ||
			strings.Contains(response.Body.String(), `"status":"DUPLICATE"`) {
			t.Fatalf("Phase 0 must not return a terminal sync result: %s", response.Body.String())
		}
	}
}

func TestSyncInboxReturnsAcceptedDuplicateAndConflictFromDurableRepository(t *testing.T) {
	t.Parallel()

	syncRepository := &testSyncRepository{hashes: map[string][]byte{}}
	server := NewServer(store.NewMemoryRepository(), nil, Config{SyncRepository: syncRepository})
	operationID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaade"
	metadata := testMetadata(operationID, tenantOne, outletOne, actorOne, "sync-operation-durable-01")
	operation := map[string]any{
		"operationId": operationID, "aggregateType": "order",
		"aggregateId": "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbde", "aggregateVersion": 1,
		"commandType": "order.create", "recordedAt": "2026-08-03T10:00:01Z",
		"mutation": envelopeMap(metadata, map[string]any{"order": map[string]any{"id": "example"}}),
	}
	body := mustJSON(t, map[string]any{
		"batchId": "batch-durable-1", "edgeId": deviceOne, "outletId": outletOne,
		"operations": []any{operation},
	})

	first := authenticatedRequest(server, http.MethodPost, "/api/v1/sync/operations", body, tenantOne, "edge:"+deviceOne)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"status":"ACCEPTED"`) {
		t.Fatalf("first durable response status=%d body=%s", first.Code, first.Body.String())
	}
	second := authenticatedRequest(server, http.MethodPost, "/api/v1/sync/operations", body, tenantOne, "edge:"+deviceOne)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"status":"DUPLICATE"`) {
		t.Fatalf("duplicate durable response status=%d body=%s", second.Code, second.Body.String())
	}
	operation["commandType"] = "order.transition"
	conflictingBody := mustJSON(t, map[string]any{
		"batchId": "batch-durable-2", "edgeId": deviceOne, "outletId": outletOne,
		"operations": []any{operation},
	})
	conflict := authenticatedRequest(server, http.MethodPost, "/api/v1/sync/operations", conflictingBody, tenantOne, "edge:"+deviceOne)
	if conflict.Code != http.StatusOK || !strings.Contains(conflict.Body.String(), `"status":"CONFLICT"`) ||
		!strings.Contains(conflict.Body.String(), `"problemCode":"operation_id_reused"`) {
		t.Fatalf("conflicting durable response status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

type testSyncRepository struct {
	hashes     map[string][]byte
	readyError error
}

func (repository *testSyncRepository) ApplySyncOperation(_ context.Context, operation store.SyncOperation) (store.SyncOutcome, string, error) {
	if previous, exists := repository.hashes[operation.OperationID]; exists {
		if !bytes.Equal(previous, operation.RequestHash) {
			return store.SyncConflict, "operation_id_reused", nil
		}
		return store.SyncDuplicate, "", nil
	}
	repository.hashes[operation.OperationID] = append([]byte(nil), operation.RequestHash...)
	return store.SyncAccepted, "", nil
}

func (repository *testSyncRepository) Ready(context.Context) error { return repository.readyError }

func TestSyncInboxRequiresMatchingEdgePrincipal(t *testing.T) {
	t.Parallel()

	server := newTestServer()
	operationID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaae"
	metadata := testMetadata(operationID, tenantOne, outletOne, actorOne, "sync-operation-02")
	body := mustJSON(t, map[string]any{
		"batchId": "batch-2", "edgeId": deviceOne, "outletId": outletOne,
		"operations": []any{map[string]any{
			"operationId": operationID, "aggregateType": "order",
			"aggregateId": "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbc", "aggregateVersion": 0,
			"commandType": "order.create", "mutation": envelopeMap(metadata, map[string]any{}),
		}},
	})

	response := authenticatedRequest(server, http.MethodPost, "/api/v1/sync/operations", body, tenantOne, actorOne)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"edge_principal_mismatch"`) {
		t.Fatalf("mismatched edge principal status=%d body=%s", response.Code, response.Body.String())
	}
}

func createFixtureHierarchy(t *testing.T, server *Server) {
	t.Helper()

	steps := []struct {
		path     string
		metadata domain.MutationMetadata
		payload  any
	}{
		{
			path:     "/api/v1/organizations",
			metadata: testMetadata("10000000-0000-4000-8000-000000000001", tenantOne, outletOne, actorOne, "fixture-org-01"),
			payload: map[string]any{
				"id": tenantOne, "name": "Fixture Org", "defaultLocale": "en-IN", "defaultCurrency": "INR",
			},
		},
		{
			path:     "/api/v1/outlets",
			metadata: testMetadata("10000000-0000-4000-8000-000000000002", tenantOne, outletOne, actorOne, "fixture-outlet-01"),
			payload: map[string]any{
				"id": outletOne, "organizationId": tenantOne, "name": "Test Kitchen", "code": "BLR-01", "timeZone": "Asia/Kolkata", "currency": "INR",
			},
		},
		{
			path:     "/api/v1/brands",
			metadata: testMetadata("10000000-0000-4000-8000-000000000003", tenantOne, outletOne, actorOne, "fixture-brand-01"),
			payload: map[string]any{
				"id": "55555555-5555-4555-8555-555555555555", "organizationId": tenantOne, "name": "Test Brand", "code": "TEST",
			},
		},
		{
			path:     "/api/v1/stations",
			metadata: testMetadata("10000000-0000-4000-8000-000000000004", tenantOne, outletOne, actorOne, "fixture-station-01"),
			payload: map[string]any{
				"id": "66666666-6666-4666-8666-666666666666", "outletId": outletOne, "name": "Hot Line", "code": "HOT", "type": "cooking",
			},
		},
		{
			path:     "/api/v1/orders",
			metadata: testMetadata("10000000-0000-4000-8000-000000000005", tenantOne, outletOne, actorOne, "fixture-order-01"),
			payload: map[string]any{
				"id": "77777777-7777-4777-8777-777777777777", "outletId": outletOne,
				"brandId": "55555555-5555-4555-8555-555555555555", "externalRef": "EDGE-1001",
				"type": "delivery", "placedAt": "2026-08-03T10:00:00Z",
				"lines": []any{map[string]any{
					"id": "88888888-8888-4888-8888-888888888888", "name": "Paneer Bowl", "quantity": 2,
					"unitPrice": map[string]any{"minorUnits": 25000, "currency": "INR"},
					"lineTotal": map[string]any{"minorUnits": 50000, "currency": "INR"},
				}},
				"subtotal":      map[string]any{"minorUnits": 50000, "currency": "INR"},
				"discountTotal": map[string]any{"minorUnits": 0, "currency": "INR"},
				"taxTotal":      map[string]any{"minorUnits": 2500, "currency": "INR"},
				"serviceCharge": map[string]any{"minorUnits": 0, "currency": "INR"},
				"total":         map[string]any{"minorUnits": 52500, "currency": "INR"},
			},
		},
		{
			path:     "/api/v1/kitchen-tickets",
			metadata: testMetadata("10000000-0000-4000-8000-000000000006", tenantOne, outletOne, actorOne, "fixture-ticket-01"),
			payload: map[string]any{
				"id": "99999999-9999-4999-8999-999999999999", "outletId": outletOne,
				"orderId":   "77777777-7777-4777-8777-777777777777",
				"stationId": "66666666-6666-4666-8666-666666666666",
				"lineIds":   []string{"88888888-8888-4888-8888-888888888888"}, "priority": 25,
			},
		},
	}

	for _, step := range steps {
		body := mutationJSON(t, step.metadata, step.payload)
		response := performMutation(server, step.path, body, tenantOne, actorOne, step.metadata.IdempotencyKey)
		if response.Code != http.StatusCreated {
			t.Fatalf("POST %s status=%d body=%s", step.path, response.Code, response.Body.String())
		}
	}
}

func newTestServer() *Server {
	return NewServer(store.NewMemoryRepository(), nil, Config{})
}

func testMetadata(id, tenantID, outletID, actorID, key string) domain.MutationMetadata {
	return domain.MutationMetadata{
		ID:             id,
		TenantID:       tenantID,
		OutletID:       outletID,
		DeviceID:       deviceOne,
		ActorID:        actorID,
		OccurredAt:     time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC),
		Source:         "feastcloud-test",
		SourceID:       id,
		SchemaVersion:  domain.CurrentSchemaVersion,
		IdempotencyKey: key,
	}
}

func mutationJSON(t *testing.T, metadata domain.MutationMetadata, payload any) []byte {
	t.Helper()
	return mustJSON(t, envelopeMap(metadata, payload))
}

func envelopeMap(metadata domain.MutationMetadata, payload any) map[string]any {
	return map[string]any{
		"id":             metadata.ID,
		"tenantId":       metadata.TenantID,
		"outletId":       metadata.OutletID,
		"deviceId":       metadata.DeviceID,
		"actorId":        metadata.ActorID,
		"occurredAt":     metadata.OccurredAt.Format(time.RFC3339Nano),
		"source":         metadata.Source,
		"sourceId":       metadata.SourceID,
		"schemaVersion":  metadata.SchemaVersion,
		"idempotencyKey": metadata.IdempotencyKey,
		"payload":        payload,
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return body
}

func performMutation(server *Server, path string, body []byte, tenantID, actorID, idempotencyKey string) *httptest.ResponseRecorder {
	response := authenticatedRequest(server, http.MethodPost, path, body, tenantID, actorID)
	return response
}

func authenticatedRequest(
	server *Server,
	method string,
	path string,
	body []byte,
	tenantID string,
	actorID string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("X-FeastCloud-Tenant-ID", tenantID)
	request.Header.Set("X-FeastCloud-Actor-ID", actorID)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
		if path != "/api/v1/sync/operations" {
			var envelope mutationEnvelope
			if err := json.Unmarshal(body, &envelope); err == nil {
				request.Header.Set("Idempotency-Key", envelope.IdempotencyKey)
			}
		}
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}
