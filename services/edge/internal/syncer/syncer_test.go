// SPDX-License-Identifier: AGPL-3.0-only

package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/edge/internal/model"
	"github.com/feastcloud/feastcloud/services/edge/internal/store"
)

func TestCoordinatorPushesScopedBatchAndAcknowledgesOutbox(t *testing.T) {
	const (
		tenantID = "tenant-sync"
		outletID = "outlet-sync"
		edgeID   = "edge-sync"
	)
	repository, err := store.Open(filepath.Join(t.TempDir(), "edge.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { repository.Close() })
	order := model.NewOrder{
		ID: model.NewUUIDv7(), Type: model.OrderTypeTakeaway, PlacedAt: time.Now().UTC(),
		Lines: []model.OrderLine{{ID: model.NewUUIDv7(), Name: "Rice", Quantity: 1, StationID: "hot"}},
	}
	payload, _ := json.Marshal(model.CreateOrderPayload{Order: order})
	operationID := model.NewUUIDv7()
	envelope := model.MutationEnvelope{
		ID: operationID, TenantID: tenantID, OutletID: outletID, DeviceID: "device-sync",
		ActorID: "cashier-sync", OccurredAt: time.Now().UTC(), Source: "test",
		SchemaVersion: model.CurrentSchemaVersion, IdempotencyKey: "idem-" + operationID,
		Payload: payload,
	}
	if _, err := repository.CreateOrder(t.Context(), "test.create", "request-hash", envelope, order); err != nil {
		t.Fatalf("create order: %v", err)
	}

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("X-FeastCloud-Tenant-ID") != tenantID {
			t.Errorf("tenant header = %q", request.Header.Get("X-FeastCloud-Tenant-ID"))
		}
		if request.Header.Get("X-FeastCloud-Actor-ID") != "edge:"+edgeID {
			t.Errorf("actor header = %q", request.Header.Get("X-FeastCloud-Actor-ID"))
		}
		if request.Header.Get("X-Edge-ID") != edgeID {
			t.Errorf("edge header = %q", request.Header.Get("X-Edge-ID"))
		}
		var batch model.PushOperationsRequest
		if err := json.NewDecoder(request.Body).Decode(&batch); err != nil {
			t.Errorf("decode batch: %v", err)
			return nil, err
		}
		if len(batch.Operations) != 1 || batch.Operations[0].Mutation.TenantID != tenantID || batch.Operations[0].Mutation.OutletID != outletID {
			t.Errorf("unexpected operations: %#v", batch.Operations)
		}
		body, err := json.Marshal(model.PushOperationsResponse{
			BatchID: batch.BatchID,
			Results: []model.PushResult{{OperationID: operationID, Status: model.PushAccepted}},
		})
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(bytes.NewReader(body)), Request: request,
		}, nil
	})}
	adapter, err := NewHTTPAdapter(HTTPAdapterConfig{
		Endpoint: "https://cloud.example/api/v1/sync/operations", TenantID: tenantID,
		ActorID: "edge:" + edgeID, Client: client,
	})
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	coordinator := NewCoordinator(repository, adapter, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		EdgeID: edgeID, TenantID: tenantID, OutletID: outletID, BatchSize: 100,
	})
	if err := coordinator.SyncOnce(context.Background()); err != nil {
		t.Fatalf("sync once: %v", err)
	}
	stats, err := repository.SyncStats(t.Context())
	if err != nil {
		t.Fatalf("sync stats: %v", err)
	}
	if stats.Pending != 0 || stats.Synchronized != 1 || stats.LastSuccessAt == nil {
		t.Fatalf("unexpected sync stats: %#v", stats)
	}
}

func TestCoordinatorRetriesCausalHeadWithoutBlockingAnotherOrder(t *testing.T) {
	const (
		tenantID = "tenant-causality"
		outletID = "outlet-causality"
		edgeID   = "edge-causality"
	)
	repository, err := store.Open(filepath.Join(t.TempDir(), "edge.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { repository.Close() })

	firstCreate, firstTransition := appendSyncOrderAndTransition(t, repository, tenantID, outletID, "first")
	secondCreate, secondTransition := appendSyncOrderAndTransition(t, repository, tenantID, outletID, "second")

	clock := time.Now().UTC().Add(time.Minute)
	pushes := make([][]string, 0, 4)
	adapter := adapterFunc(func(_ context.Context, request model.PushOperationsRequest) (model.PushOperationsResponse, error) {
		ids := operationIDs(request.Operations)
		pushes = append(pushes, ids)
		results := make([]model.PushResult, 0, len(ids))
		switch len(pushes) {
		case 1:
			assertSyncOperationIDs(t, ids, firstCreate, secondCreate)
			results = append(results,
				model.PushResult{OperationID: firstCreate, Status: model.PushRetry, ProblemCode: "cloud_busy", RetryAfterSeconds: 60},
				model.PushResult{OperationID: secondCreate, Status: model.PushAccepted},
			)
		case 2:
			assertSyncOperationIDs(t, ids, secondTransition)
			results = append(results, model.PushResult{OperationID: secondTransition, Status: model.PushAccepted})
		case 3:
			assertSyncOperationIDs(t, ids, firstCreate)
			results = append(results, model.PushResult{OperationID: firstCreate, Status: model.PushAccepted})
		case 4:
			assertSyncOperationIDs(t, ids, firstTransition)
			results = append(results, model.PushResult{OperationID: firstTransition, Status: model.PushAccepted})
		default:
			t.Fatalf("unexpected push %d with operations %v", len(pushes), ids)
		}
		return model.PushOperationsResponse{BatchID: request.BatchID, Results: results}, nil
	})
	coordinator := NewCoordinator(repository, adapter, nil, Config{
		EdgeID: edgeID, TenantID: tenantID, OutletID: outletID, BatchSize: 100,
	})
	coordinator.now = func() time.Time { return clock }

	if err := coordinator.SyncOnce(t.Context()); err != nil {
		t.Fatalf("initial mixed sync: %v", err)
	}
	clock = clock.Add(time.Second)
	if err := coordinator.SyncOnce(t.Context()); err != nil {
		t.Fatalf("independent order sync: %v", err)
	}
	if len(pushes) != 2 {
		t.Fatalf("pushes before retry = %d, want 2", len(pushes))
	}

	clock = clock.Add(58 * time.Second)
	if err := coordinator.SyncOnce(t.Context()); err != nil {
		t.Fatalf("sync before retry due: %v", err)
	}
	if len(pushes) != 2 {
		t.Fatalf("backed-off stream was pushed early; pushes = %d", len(pushes))
	}

	clock = clock.Add(time.Second)
	if err := coordinator.SyncOnce(t.Context()); err != nil {
		t.Fatalf("retry causal head: %v", err)
	}
	if err := coordinator.SyncOnce(t.Context()); err != nil {
		t.Fatalf("sync dependent operation: %v", err)
	}
	if len(pushes) != 4 {
		t.Fatalf("pushes after retry = %d, want 4", len(pushes))
	}

	stats, err := repository.SyncStats(t.Context())
	if err != nil {
		t.Fatalf("sync stats: %v", err)
	}
	if stats.Pending != 0 || stats.Synchronized != 4 || stats.Reconciliation != 0 {
		t.Fatalf("unexpected final sync stats: %#v", stats)
	}
}

func appendSyncOrderAndTransition(t *testing.T, repository *store.Store, tenantID, outletID, label string) (string, string) {
	t.Helper()
	now := time.Now().UTC()
	order := model.NewOrder{
		ID: model.NewUUIDv7(), Type: model.OrderTypeTakeaway, PlacedAt: now,
		Lines: []model.OrderLine{{
			ID: model.NewUUIDv7(), Name: label + " rice", Quantity: 1, StationID: "hot",
		}},
	}
	createPayload, err := json.Marshal(model.CreateOrderPayload{Order: order})
	if err != nil {
		t.Fatalf("marshal create payload: %v", err)
	}
	createID := model.NewUUIDv7()
	create := model.MutationEnvelope{
		ID: createID, TenantID: tenantID, OutletID: outletID,
		DeviceID: "device-causality", ActorID: "actor-causality", OccurredAt: now,
		Source: "syncer-test", SchemaVersion: model.CurrentSchemaVersion,
		IdempotencyKey: "idem-" + createID, Payload: createPayload,
	}
	response, err := repository.CreateOrder(t.Context(), "test.create", "hash-"+createID, create, order)
	if err != nil {
		t.Fatalf("create %s order: %v", label, err)
	}
	var created struct {
		Data model.CreateOrderResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &created); err != nil {
		t.Fatalf("decode %s create response: %v", label, err)
	}
	if len(created.Data.Tickets) != 1 {
		t.Fatalf("%s order tickets = %d, want 1", label, len(created.Data.Tickets))
	}

	transitionInput := model.TransitionTicketPayload{ToStatus: model.TicketStatusFired, ExpectedVersion: 1}
	transitionPayload, err := json.Marshal(transitionInput)
	if err != nil {
		t.Fatalf("marshal transition payload: %v", err)
	}
	transitionID := model.NewUUIDv7()
	transition := model.MutationEnvelope{
		ID: transitionID, TenantID: tenantID, OutletID: outletID,
		DeviceID: "device-causality", ActorID: "actor-causality", OccurredAt: now,
		Source: "syncer-test", SchemaVersion: model.CurrentSchemaVersion,
		IdempotencyKey: "idem-" + transitionID, Payload: transitionPayload,
	}
	if _, err := repository.TransitionTicket(
		t.Context(),
		"test.transition-ticket",
		"hash-"+transitionID,
		created.Data.Tickets[0].ID,
		transition,
		transitionInput,
	); err != nil {
		t.Fatalf("transition %s ticket: %v", label, err)
	}
	return createID, transitionID
}

func operationIDs(operations []model.Operation) []string {
	ids := make([]string, len(operations))
	for index := range operations {
		ids[index] = operations[index].OperationID
	}
	return ids
}

func assertSyncOperationIDs(t *testing.T, actual []string, expected ...string) {
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

type adapterFunc func(context.Context, model.PushOperationsRequest) (model.PushOperationsResponse, error)

func (adapter adapterFunc) Push(ctx context.Context, request model.PushOperationsRequest) (model.PushOperationsResponse, error) {
	return adapter(ctx, request)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
