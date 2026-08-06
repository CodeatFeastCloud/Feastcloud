// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/edge/internal/model"
	"github.com/feastcloud/feastcloud/services/edge/internal/store"
)

const (
	testTenant = "tenant-test"
	testOutlet = "outlet-test"
)

func TestOrderAndOutboxSurviveRestartAndDuplicateReplay(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "edge.db")
	repository := openTestStore(t, databasePath)
	handler := testHandler(repository)

	order := testOrder()
	createEnvelope := testMutation(model.CreateOrderPayload{Order: order})
	createBody := marshalJSON(t, createEnvelope)
	created := performMutation(handler, "/api/v1/orders", createBody, createEnvelope.IdempotencyKey)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var createResponse struct {
		Data model.CreateOrderResult `json:"data"`
	}
	decodeResponse(t, created.Body.Bytes(), &createResponse)
	if len(createResponse.Data.Tickets) != 2 {
		t.Fatalf("tickets = %d, want 2", len(createResponse.Data.Tickets))
	}
	if createResponse.Data.Order.GuestName != order.GuestName ||
		createResponse.Data.Order.TableLabel != order.TableLabel ||
		createResponse.Data.Order.Note != order.Note ||
		createResponse.Data.Order.TargetAt == nil {
		t.Fatalf("order service metadata was not preserved: %#v", createResponse.Data.Order)
	}
	firstResponse := append([]byte(nil), created.Body.Bytes()...)

	hotTickets := httptest.NewRecorder()
	handler.ServeHTTP(hotTickets, httptest.NewRequest(http.MethodGet, "/api/v1/stations/hot/tickets", nil))
	if hotTickets.Code != http.StatusOK {
		t.Fatalf("station tickets status = %d", hotTickets.Code)
	}
	var ticketList struct {
		Data []model.KitchenTicket `json:"data"`
	}
	decodeResponse(t, hotTickets.Body.Bytes(), &ticketList)
	if len(ticketList.Data) != 1 || ticketList.Data[0].StationID != "hot" {
		t.Fatalf("unexpected station tickets: %#v", ticketList.Data)
	}
	assertOutboxCounts(t, repository, 1, 0, 0)
	if err := repository.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	repository = openTestStore(t, databasePath)
	t.Cleanup(func() { repository.Close() })
	handler = testHandler(repository)
	replayed := performMutation(handler, "/api/v1/orders", createBody, createEnvelope.IdempotencyKey)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay status/header = %d/%q", replayed.Code, replayed.Header().Get("Idempotency-Replayed"))
	}
	if !bytes.Equal(replayed.Body.Bytes(), firstResponse) {
		t.Fatalf("replayed response changed\nfirst: %s\nreplay: %s", firstResponse, replayed.Body.Bytes())
	}
	orders := httptest.NewRecorder()
	handler.ServeHTTP(orders, httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil))
	var orderList struct {
		Data []model.Order `json:"data"`
	}
	decodeResponse(t, orders.Body.Bytes(), &orderList)
	if len(orderList.Data) != 1 || orderList.Data[0].ID != order.ID {
		t.Fatalf("durable order projection = %#v", orderList.Data)
	}
	assertOutboxCounts(t, repository, 1, 0, 0)

	ticket := createResponse.Data.Tickets[0]
	transition := testMutation(model.TransitionTicketPayload{ToStatus: model.TicketStatusFired, ExpectedVersion: 1})
	transitionBody := marshalJSON(t, transition)
	transitioned := performMutation(handler, "/api/v1/kitchen-tickets/"+ticket.ID+"/transitions", transitionBody, transition.IdempotencyKey)
	if transitioned.Code != http.StatusOK {
		t.Fatalf("transition status = %d, body = %s", transitioned.Code, transitioned.Body.String())
	}
	var transitionResponse struct {
		Data model.TransitionTicketResult `json:"data"`
	}
	decodeResponse(t, transitioned.Body.Bytes(), &transitionResponse)
	if transitionResponse.Data.Ticket.Status != model.TicketStatusFired || transitionResponse.Data.Order.Status != model.OrderStatusAccepted {
		t.Fatalf("unexpected derived states: ticket=%s order=%s", transitionResponse.Data.Ticket.Status, transitionResponse.Data.Order.Status)
	}
	assertOutboxCounts(t, repository, 2, 0, 0)

	skip := testMutation(model.TransitionTicketPayload{ToStatus: model.TicketStatusReady, ExpectedVersion: 2})
	skipped := performMutation(handler, "/api/v1/kitchen-tickets/"+ticket.ID+"/transitions", marshalJSON(t, skip), skip.IdempotencyKey)
	if skipped.Code != http.StatusConflict {
		t.Fatalf("skipped transition status = %d, body = %s", skipped.Code, skipped.Body.String())
	}
	assertOutboxCounts(t, repository, 2, 0, 0)
}

func TestBrowserMutationRoutesOrderAndAllStationTickets(t *testing.T) {
	repository := openTestStore(t, filepath.Join(t.TempDir(), "edge.db"))
	t.Cleanup(func() { repository.Close() })
	handler := testHandler(repository)
	order := testOrder()
	hotTicketID := model.NewUUIDv7()
	beverageTicketID := model.NewUUIDv7()
	order.StationTicketIDs = map[string]string{
		"hot": hotTicketID, "beverage": beverageTicketID,
	}
	createdEvent := model.BrowserMutationPayload{
		EventType: "com.feastcloud.order.created.v1", AggregateType: "order",
		AggregateID: order.ID, Order: &order,
	}
	createdEnvelope := testMutation(createdEvent)
	created := performMutation(handler, "/api/v1/sync/mutations", marshalJSON(t, createdEnvelope), createdEnvelope.IdempotencyKey)
	if created.Code != http.StatusCreated {
		t.Fatalf("browser create status = %d, body = %s", created.Code, created.Body.String())
	}
	missingVersionEvent := model.BrowserMutationPayload{
		EventType: "com.feastcloud.order.status-changed.v1", AggregateType: "order",
		AggregateID: order.ID, OrderID: order.ID, ToStatus: model.TicketStatusFired,
	}
	missingVersionEnvelope := testMutation(missingVersionEvent)
	missingVersion := performMutation(handler, "/api/v1/sync/mutations", marshalJSON(t, missingVersionEnvelope), missingVersionEnvelope.IdempotencyKey)
	if missingVersion.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing order version status = %d, body = %s", missingVersion.Code, missingVersion.Body.String())
	}
	assertOutboxCounts(t, repository, 1, 0, 0)

	statusEvent := model.BrowserMutationPayload{
		EventType: "com.feastcloud.order.status-changed.v1", AggregateType: "order",
		AggregateID: order.ID, OrderID: order.ID, ToStatus: model.TicketStatusFired,
		ExpectedVersion: 1,
	}
	statusEnvelope := testMutation(statusEvent)
	statusBody := marshalJSON(t, statusEnvelope)
	advanced := performMutation(handler, "/api/v1/sync/mutations", statusBody, statusEnvelope.IdempotencyKey)
	if advanced.Code != http.StatusOK {
		t.Fatalf("browser transition status = %d, body = %s", advanced.Code, advanced.Body.String())
	}
	var response struct {
		Data model.TransitionOrderResult `json:"data"`
	}
	decodeResponse(t, advanced.Body.Bytes(), &response)
	if response.Data.Order.Status != model.OrderStatusAccepted || len(response.Data.Tickets) != 2 {
		t.Fatalf("unexpected browser transition result: %#v", response.Data)
	}
	actualTicketIDs := map[string]string{}
	for _, ticket := range response.Data.Tickets {
		actualTicketIDs[ticket.StationID] = ticket.ID
		if ticket.Status != model.TicketStatusFired {
			t.Fatalf("ticket %s status = %s", ticket.ID, ticket.Status)
		}
	}
	if actualTicketIDs["hot"] != hotTicketID || actualTicketIDs["beverage"] != beverageTicketID {
		t.Fatalf("client station ticket ids were not preserved: %#v", actualTicketIDs)
	}
	replayed := performMutation(handler, "/api/v1/sync/mutations", statusBody, statusEnvelope.IdempotencyKey)
	if replayed.Code != http.StatusOK || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("browser replay = %d/%q", replayed.Code, replayed.Header().Get("Idempotency-Replayed"))
	}
	assertOutboxCounts(t, repository, 2, 0, 0)

	remaining := []model.TicketStatus{
		model.TicketStatusPreparing,
		model.TicketStatusReady,
		model.TicketStatusCompleted,
	}
	for index, desired := range remaining {
		event := model.BrowserMutationPayload{
			EventType: "com.feastcloud.order.status-changed.v1", AggregateType: "order",
			AggregateID: order.ID, OrderID: order.ID, ToStatus: desired,
			ExpectedVersion: uint64(index + 2),
		}
		envelope := testMutation(event)
		result := performMutation(handler, "/api/v1/sync/mutations", marshalJSON(t, envelope), envelope.IdempotencyKey)
		if result.Code != http.StatusOK {
			t.Fatalf("browser transition to %s status = %d, body = %s", desired, result.Code, result.Body.String())
		}
		decodeResponse(t, result.Body.Bytes(), &response)
	}
	if response.Data.Order.Status != model.OrderStatusCompleted || response.Data.Order.Version != 5 {
		t.Fatalf("completed browser order = %#v", response.Data.Order)
	}
	for _, ticket := range response.Data.Tickets {
		if ticket.Status != model.TicketStatusCompleted || ticket.Version != 5 {
			t.Fatalf("completed ticket = %#v", ticket)
		}
	}
	assertOutboxCounts(t, repository, 5, 0, 0)
}

func TestOneTimePairingCreatesScopedRevocableSession(t *testing.T) {
	repository := openTestStore(t, filepath.Join(t.TempDir(), "edge.db"))
	t.Cleanup(func() { repository.Close() })
	handler := NewServer(repository, nil, Config{
		Version: "test", EdgeID: "edge-test", TenantID: testTenant, OutletID: testOutlet,
		BearerToken: "bootstrap-secret", PairingTTL: time.Minute, SessionTTL: time.Hour,
	})

	create := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/codes", strings.NewReader(`{"role":"cashier"}`))
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("Authorization", "Bearer bootstrap-secret")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create pairing status=%d body=%s", created.Code, created.Body.String())
	}
	var pairing struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	decodeResponse(t, created.Body.Bytes(), &pairing)

	exchangeBody := `{"code":"` + pairing.Data.Code + `"}`
	exchange := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/sessions", strings.NewReader(exchangeBody))
	exchange.Header.Set("Content-Type", "application/json")
	exchanged := httptest.NewRecorder()
	handler.ServeHTTP(exchanged, exchange)
	if exchanged.Code != http.StatusCreated {
		t.Fatalf("exchange status=%d body=%s", exchanged.Code, exchanged.Body.String())
	}
	var session struct {
		Data struct{ AccessToken, Role string } `json:"data"`
	}
	decodeResponse(t, exchanged.Body.Bytes(), &session)
	if session.Data.Role != "cashier" || session.Data.AccessToken == "" {
		t.Fatalf("unexpected paired session: %#v", session.Data)
	}

	reused := httptest.NewRecorder()
	reuseRequest := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/sessions", strings.NewReader(exchangeBody))
	reuseRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(reused, reuseRequest)
	if reused.Code != http.StatusUnauthorized {
		t.Fatalf("reused pairing status=%d want 401", reused.Code)
	}

	denied := httptest.NewRecorder()
	deniedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/kitchen-tickets?status=queued", nil)
	deniedRequest.Header.Set("Authorization", "Bearer "+session.Data.AccessToken)
	handler.ServeHTTP(denied, deniedRequest)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("cashier kitchen access=%d want 403", denied.Code)
	}

	revoke := httptest.NewRecorder()
	revokeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/sessions/revoke", nil)
	revokeRequest.Header.Set("Authorization", "Bearer "+session.Data.AccessToken)
	handler.ServeHTTP(revoke, revokeRequest)
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d body=%s", revoke.Code, revoke.Body.String())
	}
	afterRevoke := httptest.NewRecorder()
	afterRequest := httptest.NewRequest(http.MethodGet, "/api/v1", nil)
	afterRequest.Header.Set("Authorization", "Bearer "+session.Data.AccessToken)
	handler.ServeHTTP(afterRevoke, afterRequest)
	if afterRevoke.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status=%d want 401", afterRevoke.Code)
	}
}

func TestBrowserMutationRoutesOneStationTicket(t *testing.T) {
	repository := openTestStore(t, filepath.Join(t.TempDir(), "edge.db"))
	t.Cleanup(func() { repository.Close() })
	handler := testHandler(repository)
	order := testOrder()
	createdEvent := model.BrowserMutationPayload{
		EventType: "com.feastcloud.order.created.v1", AggregateType: "order",
		AggregateID: order.ID, Order: &order,
	}
	createdEnvelope := testMutation(createdEvent)
	created := performMutation(handler, "/api/v1/sync/mutations", marshalJSON(t, createdEnvelope), createdEnvelope.IdempotencyKey)
	if created.Code != http.StatusCreated {
		t.Fatalf("browser create status = %d, body = %s", created.Code, created.Body.String())
	}
	var createResponse struct {
		Data model.CreateOrderResult `json:"data"`
	}
	decodeResponse(t, created.Body.Bytes(), &createResponse)
	ticket := createResponse.Data.Tickets[0]

	mismatchedEvent := model.BrowserMutationPayload{
		EventType: "com.feastcloud.kitchen-ticket.status-changed.v1", AggregateType: "kitchenTicket",
		AggregateID: ticket.ID, TicketID: ticket.ID, OrderID: model.NewUUIDv7(),
		ToStatus: model.TicketStatusFired, ExpectedVersion: ticket.Version,
	}
	mismatchedEnvelope := testMutation(mismatchedEvent)
	mismatched := performMutation(handler, "/api/v1/sync/mutations", marshalJSON(t, mismatchedEnvelope), mismatchedEnvelope.IdempotencyKey)
	if mismatched.Code != http.StatusConflict {
		t.Fatalf("mismatched ticket parent status = %d, body = %s", mismatched.Code, mismatched.Body.String())
	}
	var mismatchProblem struct {
		Code string `json:"code"`
	}
	decodeResponse(t, mismatched.Body.Bytes(), &mismatchProblem)
	if mismatchProblem.Code != "ticket_order_mismatch" {
		t.Fatalf("mismatched ticket parent problem = %q", mismatchProblem.Code)
	}
	assertOutboxCounts(t, repository, 1, 0, 0)

	statusEvent := model.BrowserMutationPayload{
		EventType: "com.feastcloud.kitchen-ticket.status-changed.v1", AggregateType: "kitchenTicket",
		AggregateID: ticket.ID, TicketID: ticket.ID, OrderID: order.ID,
		ToStatus: model.TicketStatusFired, ExpectedVersion: ticket.Version,
	}
	statusEnvelope := testMutation(statusEvent)
	statusBody := marshalJSON(t, statusEnvelope)
	advanced := performMutation(handler, "/api/v1/sync/mutations", statusBody, statusEnvelope.IdempotencyKey)
	if advanced.Code != http.StatusOK {
		t.Fatalf("browser ticket transition status = %d, body = %s", advanced.Code, advanced.Body.String())
	}
	var response struct {
		Data model.TransitionTicketResult `json:"data"`
	}
	decodeResponse(t, advanced.Body.Bytes(), &response)
	if response.Data.Ticket.ID != ticket.ID || response.Data.Ticket.Status != model.TicketStatusFired {
		t.Fatalf("unexpected browser ticket result: %#v", response.Data.Ticket)
	}
	if response.Data.Order.Status != model.OrderStatusAccepted {
		t.Fatalf("derived browser order status = %s", response.Data.Order.Status)
	}
	replayed := performMutation(handler, "/api/v1/sync/mutations", statusBody, statusEnvelope.IdempotencyKey)
	if replayed.Code != http.StatusOK || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("browser ticket replay = %d/%q", replayed.Code, replayed.Header().Get("Idempotency-Replayed"))
	}
	assertOutboxCounts(t, repository, 2, 0, 0)
}

func TestTransitionOrderRejectsMixedTicketStatesAtomically(t *testing.T) {
	repository := openTestStore(t, filepath.Join(t.TempDir(), "edge.db"))
	t.Cleanup(func() { repository.Close() })
	handler := testHandler(repository)
	order := testOrder()

	createEnvelope := testMutation(model.CreateOrderPayload{Order: order})
	created := performMutation(handler, "/api/v1/orders", marshalJSON(t, createEnvelope), createEnvelope.IdempotencyKey)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var createResponse struct {
		Data model.CreateOrderResult `json:"data"`
	}
	decodeResponse(t, created.Body.Bytes(), &createResponse)

	advanceOne := testMutation(model.TransitionTicketPayload{ToStatus: model.TicketStatusFired, ExpectedVersion: 1})
	advanced := performMutation(handler, "/api/v1/kitchen-tickets/"+createResponse.Data.Tickets[0].ID+"/transitions", marshalJSON(t, advanceOne), advanceOne.IdempotencyKey)
	if advanced.Code != http.StatusOK {
		t.Fatalf("advance one ticket status = %d, body = %s", advanced.Code, advanced.Body.String())
	}

	beforeOrder, err := repository.GetOrder(t.Context(), testTenant, testOutlet, order.ID)
	if err != nil {
		t.Fatalf("get order before rejection: %v", err)
	}
	beforeTickets, err := repository.ListTickets(t.Context(), testTenant, testOutlet, "", "", 100)
	if err != nil {
		t.Fatalf("list tickets before rejection: %v", err)
	}
	if len(beforeTickets) != 2 {
		t.Fatalf("tickets before rejection = %d, want 2", len(beforeTickets))
	}
	beforeStats, err := repository.SyncStats(t.Context())
	if err != nil {
		t.Fatalf("sync stats before rejection: %v", err)
	}
	if beforeOrder.Status != model.OrderStatusAccepted || beforeTickets[0].Status == beforeTickets[1].Status {
		t.Fatalf("test did not establish mixed aggregate state: order=%s tickets=%#v", beforeOrder.Status, beforeTickets)
	}

	transition := testMutation(model.TransitionOrderPayload{ToStatus: model.OrderStatusPreparing, ExpectedVersion: beforeOrder.Version})
	rejected := performMutation(handler, "/api/v1/orders/"+order.ID+"/transitions", marshalJSON(t, transition), transition.IdempotencyKey)
	if rejected.Code != http.StatusConflict {
		t.Fatalf("mixed transition status = %d, body = %s", rejected.Code, rejected.Body.String())
	}
	var problem struct {
		Code string `json:"code"`
	}
	decodeResponse(t, rejected.Body.Bytes(), &problem)
	if problem.Code != "invalid_transition" {
		t.Fatalf("mixed transition problem = %q, body = %s", problem.Code, rejected.Body.String())
	}

	afterOrder, err := repository.GetOrder(t.Context(), testTenant, testOutlet, order.ID)
	if err != nil {
		t.Fatalf("get order after rejection: %v", err)
	}
	afterTickets, err := repository.ListTickets(t.Context(), testTenant, testOutlet, "", "", 100)
	if err != nil {
		t.Fatalf("list tickets after rejection: %v", err)
	}
	afterStats, err := repository.SyncStats(t.Context())
	if err != nil {
		t.Fatalf("sync stats after rejection: %v", err)
	}
	if !reflect.DeepEqual(afterOrder, beforeOrder) {
		t.Fatalf("order changed after rejected transition\nbefore: %#v\nafter:  %#v", beforeOrder, afterOrder)
	}
	if !reflect.DeepEqual(afterTickets, beforeTickets) {
		t.Fatalf("tickets changed after rejected transition\nbefore: %#v\nafter:  %#v", beforeTickets, afterTickets)
	}
	if !reflect.DeepEqual(afterStats, beforeStats) {
		t.Fatalf("outbox changed after rejected transition\nbefore: %#v\nafter:  %#v", beforeStats, afterStats)
	}
}

func TestSyncRetryKeepsLastSuccessAndReportsDegradedStatus(t *testing.T) {
	repository := openTestStore(t, filepath.Join(t.TempDir(), "edge.db"))
	t.Cleanup(func() { repository.Close() })
	handler := testHandler(repository)
	order := testOrder()

	createEnvelope := testMutation(model.CreateOrderPayload{Order: order})
	created := performMutation(handler, "/api/v1/orders", marshalJSON(t, createEnvelope), createEnvelope.IdempotencyKey)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	successAt := time.Date(2027, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := repository.RecordSyncResults(t.Context(), []string{createEnvelope.ID}, []model.PushResult{{OperationID: createEnvelope.ID, Status: model.PushAccepted}}, successAt); err != nil {
		t.Fatalf("record accepted create: %v", err)
	}

	transition := testMutation(model.TransitionOrderPayload{ToStatus: model.OrderStatusAccepted, ExpectedVersion: 1})
	advanced := performMutation(handler, "/api/v1/orders/"+order.ID+"/transitions", marshalJSON(t, transition), transition.IdempotencyKey)
	if advanced.Code != http.StatusOK {
		t.Fatalf("advance order status = %d, body = %s", advanced.Code, advanced.Body.String())
	}
	retryAt := successAt.Add(time.Minute)
	if err := repository.RecordSyncResults(t.Context(), []string{transition.ID}, []model.PushResult{{
		OperationID: transition.ID, Status: model.PushRetry, ProblemCode: "cloud_busy", RetryAfterSeconds: 30,
	}}, retryAt); err != nil {
		t.Fatalf("record cloud retry: %v", err)
	}

	syncHandler := testHandlerWithSync(repository)
	response := httptest.NewRecorder()
	syncHandler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/sync/status", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("sync status = %d, body = %s", response.Code, response.Body.String())
	}
	var statusResponse struct {
		Data struct {
			Enabled bool            `json:"enabled"`
			State   string          `json:"state"`
			Outbox  store.SyncStats `json:"outbox"`
		} `json:"data"`
	}
	decodeResponse(t, response.Body.Bytes(), &statusResponse)
	stats := statusResponse.Data.Outbox
	if !statusResponse.Data.Enabled || statusResponse.Data.State != "degraded" {
		t.Fatalf("sync state = enabled:%t state:%q", statusResponse.Data.Enabled, statusResponse.Data.State)
	}
	if stats.Pending != 1 || stats.Synchronized != 1 || stats.LastError != "cloud_busy" {
		t.Fatalf("retry sync stats = %#v", stats)
	}
	if stats.LastSuccessAt == nil || !stats.LastSuccessAt.Equal(successAt) {
		t.Fatalf("last success = %v, want %v", stats.LastSuccessAt, successAt)
	}
	if stats.LastAttemptAt == nil || !stats.LastAttemptAt.Equal(retryAt) {
		t.Fatalf("last attempt = %v, want %v", stats.LastAttemptAt, retryAt)
	}
}

func TestCORSAllowsOnlyConfiguredOrigin(t *testing.T) {
	repository := openTestStore(t, filepath.Join(t.TempDir(), "edge.db"))
	t.Cleanup(func() { repository.Close() })
	handler := testHandler(repository)

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/orders", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	request.Header.Set("Access-Control-Request-Method", "POST")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("allowed preflight = %d, origin %q", response.Code, response.Header().Get("Access-Control-Allow-Origin"))
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	request.Header.Set("Origin", "https://untrusted.invalid")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("denied origin = %d, header %q", response.Code, response.Header().Get("Access-Control-Allow-Origin"))
	}
}

func testOrder() model.NewOrder {
	targetAt := time.Now().UTC().Add(15 * time.Minute)
	return model.NewOrder{
		ID: model.NewUUIDv7(), Type: model.OrderTypeDelivery, PlacedAt: time.Now().UTC(),
		GuestName: "Riya", TableLabel: "Pickup A", Note: "Call at handoff", TargetAt: &targetAt,
		Lines: []model.OrderLine{
			{ID: model.NewUUIDv7(), MenuItemID: "dish-1", Name: "Dal", Quantity: 2, StationID: "hot"},
			{ID: model.NewUUIDv7(), MenuItemID: "drink-1", Name: "Lassi", Quantity: 1, StationID: "beverage"},
		},
		Priority: 3,
	}
}

func testMutation(payload any) model.MutationEnvelope {
	operationID := model.NewUUIDv7()
	return model.MutationEnvelope{
		ID: operationID, TenantID: testTenant, OutletID: testOutlet,
		DeviceID: "device-test", ActorID: "actor-test", OccurredAt: time.Now().UTC(),
		Source: "test-suite", SchemaVersion: model.CurrentSchemaVersion,
		IdempotencyKey: "idem-" + operationID, Payload: marshalJSONForHelper(payload),
	}
}

func marshalJSONForHelper(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func marshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return encoded
}

func decodeResponse(t *testing.T, body []byte, destination any) {
	t.Helper()
	if err := json.Unmarshal(body, destination); err != nil {
		t.Fatalf("decode response %s: %v", body, err)
	}
}

func performMutation(handler http.Handler, path string, body []byte, idempotencyKey string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func openTestStore(t *testing.T, path string) *store.Store {
	t.Helper()
	repository, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return repository
}

func testHandler(repository *store.Store) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(repository, logger, Config{
		Version: "test", EdgeID: "edge-test", TenantID: testTenant,
		OutletID: testOutlet, AllowedOrigin: "http://localhost:5173",
	})
}

func testHandlerWithSync(repository *store.Store) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(repository, logger, Config{
		Version: "test", EdgeID: "edge-test", TenantID: testTenant,
		OutletID: testOutlet, AllowedOrigin: "http://localhost:5173", SyncEnabled: true,
	})
}

func assertOutboxCounts(t *testing.T, repository *store.Store, pending, reconciliation, synchronized int) {
	t.Helper()
	stats, err := repository.SyncStats(t.Context())
	if err != nil {
		t.Fatalf("sync stats: %v", err)
	}
	if stats.Pending != pending || stats.Reconciliation != reconciliation || stats.Synchronized != synchronized {
		t.Fatalf("outbox counts = pending:%d reconciliation:%d synchronized:%d", stats.Pending, stats.Reconciliation, stats.Synchronized)
	}
}
