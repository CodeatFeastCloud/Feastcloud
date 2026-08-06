// SPDX-License-Identifier: AGPL-3.0-only

// Package api exposes the outlet-local HTTP API.
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/feastcloud/feastcloud/services/edge/internal/model"
	"github.com/feastcloud/feastcloud/services/edge/internal/store"
)

const defaultMaximumBodyBytes = int64(1 << 20)
const defaultPairingTTL = 10 * time.Minute
const defaultSessionTTL = 72 * time.Hour

type Config struct {
	Version       string
	EdgeID        string
	TenantID      string
	OutletID      string
	BearerToken   string
	AllowedOrigin string
	SyncEnabled   bool
	MaxBodyBytes  int64
	PairingTTL    time.Duration
	SessionTTL    time.Duration
}

type Server struct {
	store  *store.Store
	logger *slog.Logger
	config Config
	mux    *http.ServeMux
	now    func() time.Time
}

func NewServer(repository *store.Store, logger *slog.Logger, config Config) *Server {
	if repository == nil {
		panic("api: store is required")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if config.Version == "" {
		config.Version = "dev"
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = defaultMaximumBodyBytes
	}
	if config.PairingTTL <= 0 {
		config.PairingTTL = defaultPairingTTL
	}
	if config.SessionTTL <= 0 {
		config.SessionTTL = defaultSessionTTL
	}
	server := &Server{store: repository, logger: logger, config: config, mux: http.NewServeMux(), now: time.Now}
	server.routes()
	return server
}

func (server *Server) routes() {
	server.mux.HandleFunc("GET /healthz", server.handleHealth)
	server.mux.HandleFunc("GET /readyz", server.handleReadiness)
	server.mux.HandleFunc("GET /api/v1", server.handleDiscovery)
	server.mux.HandleFunc("POST /api/v1/pairing/codes", server.handleCreatePairingCode)
	server.mux.HandleFunc("POST /api/v1/pairing/sessions", server.handleExchangePairingCode)
	server.mux.HandleFunc("POST /api/v1/pairing/sessions/revoke", server.handleRevokeSession)
	server.mux.HandleFunc("GET /api/v1/orders", server.handleListOrders)
	server.mux.HandleFunc("POST /api/v1/orders", server.handleCreateOrder)
	server.mux.HandleFunc("GET /api/v1/orders/{id}", server.handleGetOrder)
	server.mux.HandleFunc("POST /api/v1/orders/{id}/transitions", server.handleTransitionOrder)
	server.mux.HandleFunc("GET /api/v1/kitchen-tickets", server.handleListTickets)
	server.mux.HandleFunc("GET /api/v1/kitchen-tickets/{id}", server.handleGetTicket)
	server.mux.HandleFunc("POST /api/v1/kitchen-tickets/{id}/transitions", server.handleTransitionTicket)
	server.mux.HandleFunc("GET /api/v1/stations/{stationId}/tickets", server.handleStationTickets)
	server.mux.HandleFunc("POST /api/v1/sync/mutations", server.handleBrowserMutation)
	server.mux.HandleFunc("GET /api/v1/sync/status", server.handleSyncStatus)
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	startedAt := server.now()
	requestID := request.Header.Get("X-Correlation-ID")
	if !model.IsUUID(requestID) {
		requestID = model.NewUUIDv7()
	}
	writer.Header().Set("X-Correlation-ID", requestID)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("Cache-Control", "no-store")

	recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
	ctx := context.WithValue(request.Context(), requestIDContextKey{}, requestID)
	defer func() {
		if recovered := recover(); recovered != nil {
			server.logger.ErrorContext(ctx, "edge HTTP handler panic", "correlation_id", requestID, "panic", recovered, "stack", string(debug.Stack()))
			if !recorder.wroteHeader {
				writeProblem(recorder, requestID, problem{Status: 500, Code: "internal_error", Detail: "an unexpected error occurred"})
			}
		}
		server.logger.InfoContext(ctx, "edge HTTP request", "correlation_id", requestID, "method", request.Method, "path", request.URL.Path, "status", recorder.status, "duration_ms", time.Since(startedAt).Milliseconds())
	}()
	request = request.WithContext(ctx)
	if !server.applyCORS(recorder, request) {
		return
	}

	if strings.HasPrefix(request.URL.Path, "/api/v1") && server.config.BearerToken != "" {
		if request.URL.Path == "/api/v1/pairing/sessions" && request.Method == http.MethodPost {
			server.mux.ServeHTTP(recorder, request)
			return
		}
		role, ok := server.authorized(request)
		if !ok {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="feastcloud-edge"`)
			writeProblem(recorder, requestID, problem{Status: 401, Code: "authentication_required", Detail: "a valid outlet-local session is required"})
			return
		}
		if !roleAllows(role, request) {
			writeProblem(recorder, requestID, problem{Status: 403, Code: "permission_denied", Detail: "this outlet role cannot perform the requested action"})
			return
		}
		request = request.WithContext(context.WithValue(request.Context(), localRoleContextKey{}, role))
	}
	server.mux.ServeHTTP(recorder, request)
}

func (server *Server) applyCORS(writer http.ResponseWriter, request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if server.config.AllowedOrigin == "" || origin != server.config.AllowedOrigin {
		writeProblem(writer, requestID(request.Context()), problem{Status: 403, Code: "cors_origin_denied", Detail: "browser origin is not allowed by this edge"})
		return false
	}
	writer.Header().Set("Access-Control-Allow-Origin", origin)
	writer.Header().Add("Vary", "Origin")
	writer.Header().Set("Access-Control-Expose-Headers", "Location, Idempotency-Replayed, X-Correlation-ID")
	if request.Method != http.MethodOptions {
		return true
	}
	requestedMethod := request.Header.Get("Access-Control-Request-Method")
	if requestedMethod != http.MethodGet && requestedMethod != http.MethodPost {
		writeProblem(writer, requestID(request.Context()), problem{Status: 405, Code: "cors_method_denied", Detail: "requested browser method is not allowed"})
		return false
	}
	writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Correlation-ID")
	writer.Header().Set("Access-Control-Max-Age", "600")
	writer.WriteHeader(http.StatusNoContent)
	return false
}

func (server *Server) authorized(request *http.Request) (string, bool) {
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return "", false
	}
	presented := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	if len(presented) == len(server.config.BearerToken) && subtle.ConstantTimeCompare([]byte(presented), []byte(server.config.BearerToken)) == 1 {
		return "manager", true
	}
	role, err := server.store.AuthenticateSession(request.Context(), hashSecret(presented))
	return role, err == nil
}

func roleAllows(role string, request *http.Request) bool {
	if request.URL.Path == "/api/v1/pairing/sessions/revoke" && request.Method == http.MethodPost {
		return true
	}
	if role == "manager" {
		return true
	}
	path := request.URL.Path
	if role == "cashier" {
		return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/orders") || path == "/api/v1/sync/mutations" || path == "/api/v1/sync/status"
	}
	if role == "chef" {
		return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/kitchen-tickets") || strings.HasPrefix(path, "/api/v1/stations/") || path == "/api/v1/sync/mutations" || path == "/api/v1/sync/status"
	}
	return false
}

type localRoleContextKey struct{}

type requestIDContextKey struct{}

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return value
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (recorder *statusRecorder) WriteHeader(status int) {
	if recorder.wroteHeader {
		return
	}
	recorder.status = status
	recorder.wroteHeader = true
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *statusRecorder) Write(body []byte) (int, error) {
	if !recorder.wroteHeader {
		recorder.WriteHeader(http.StatusOK)
	}
	return recorder.ResponseWriter.Write(body)
}

type problem struct {
	Status    int
	Code      string
	Detail    string
	Retryable bool
	Fields    map[string]string
}

type problemDocument struct {
	Type            string            `json:"type"`
	Title           string            `json:"title"`
	Status          int               `json:"status"`
	Code            string            `json:"code"`
	MessageKey      string            `json:"messageKey"`
	CorrelationID   string            `json:"correlationId"`
	Retryable       bool              `json:"retryable"`
	Detail          string            `json:"detail"`
	FieldViolations map[string]string `json:"fieldViolations,omitempty"`
}

func writeProblem(writer http.ResponseWriter, correlationID string, failure problem) {
	if failure.Status == 0 {
		failure.Status = http.StatusInternalServerError
	}
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(failure.Status)
	_ = json.NewEncoder(writer).Encode(problemDocument{
		Type: "https://feastcloud.org/problems/" + failure.Code, Title: http.StatusText(failure.Status),
		Status: failure.Status, Code: failure.Code, MessageKey: "errors." + failure.Code,
		CorrelationID: correlationID, Retryable: failure.Retryable, Detail: failure.Detail,
		FieldViolations: failure.Fields,
	})
}

func (server *Server) handleHealth(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, 200, map[string]any{"status": "ok", "service": "feastcloud-edge", "version": server.config.Version})
}

func (server *Server) handleReadiness(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), time.Second)
	defer cancel()
	if err := server.store.Ping(ctx); err != nil {
		writeProblem(writer, requestID(request.Context()), problem{Status: 503, Code: "database_unavailable", Detail: "the local durable store is unavailable", Retryable: true})
		return
	}
	writeJSON(writer, 200, map[string]any{"status": "ready", "database": "ready"})
}

func (server *Server) handleDiscovery(writer http.ResponseWriter, request *http.Request) {
	role, _ := request.Context().Value(localRoleContextKey{}).(string)
	if role == "" && server.config.BearerToken == "" {
		role = "manager"
	}
	writeJSON(writer, 200, map[string]any{
		"data": map[string]any{
			"service": "feastcloud-edge", "version": server.config.Version,
			"edgeId": server.config.EdgeID, "tenantId": server.config.TenantID, "outletId": server.config.OutletID,
			"role": role, "resources": []string{"orders", "kitchenTickets", "stationTickets", "syncMutations", "syncStatus", "pairing"},
		},
	})
}

type pairingCodeRequest struct {
	Role string `json:"role"`
}

type pairingSessionRequest struct {
	Code string `json:"code"`
}

func (server *Server) handleCreatePairingCode(writer http.ResponseWriter, request *http.Request) {
	var input pairingCodeRequest
	if err := decodeJSONBody(writer, request, server.config.MaxBodyBytes, &input); err != nil {
		writeProblem(writer, requestID(request.Context()), problem{Status: 400, Code: "invalid_pairing_request", Detail: err.Error()})
		return
	}
	if input.Role != "manager" && input.Role != "cashier" && input.Role != "chef" {
		writeProblem(writer, requestID(request.Context()), problem{Status: 422, Code: "invalid_role", Detail: "role must be manager, cashier, or chef"})
		return
	}
	code, err := randomDigits(8)
	if err != nil {
		writeProblem(writer, requestID(request.Context()), problem{Status: 500, Code: "randomness_unavailable", Detail: "a secure pairing code could not be generated"})
		return
	}
	expiresAt := server.now().UTC().Add(server.config.PairingTTL)
	if err := server.store.CreatePairingCode(request.Context(), hashSecret(code), input.Role, expiresAt); err != nil {
		writeProblem(writer, requestID(request.Context()), problem{Status: 500, Code: "pairing_persistence_failed", Detail: "the pairing code could not be saved"})
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"data": map[string]any{"code": code, "role": input.Role, "expiresAt": expiresAt}})
}

func (server *Server) handleExchangePairingCode(writer http.ResponseWriter, request *http.Request) {
	var input pairingSessionRequest
	if err := decodeJSONBody(writer, request, server.config.MaxBodyBytes, &input); err != nil {
		writeProblem(writer, requestID(request.Context()), problem{Status: 400, Code: "invalid_pairing_request", Detail: err.Error()})
		return
	}
	input.Code = strings.TrimSpace(input.Code)
	if len(input.Code) != 8 {
		writeProblem(writer, requestID(request.Context()), problem{Status: 401, Code: "invalid_pairing_code", Detail: "the pairing code is invalid or expired"})
		return
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeProblem(writer, requestID(request.Context()), problem{Status: 500, Code: "randomness_unavailable", Detail: "a secure local session could not be generated"})
		return
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := server.now().UTC().Add(server.config.SessionTTL)
	role, err := server.store.ExchangePairingCode(request.Context(), hashSecret(input.Code), hashSecret(token), expiresAt)
	if err != nil {
		writeProblem(writer, requestID(request.Context()), problem{Status: 401, Code: "invalid_pairing_code", Detail: "the pairing code is invalid or expired"})
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"data": map[string]any{"accessToken": token, "tokenType": "Bearer", "role": role, "expiresAt": expiresAt}})
}

func (server *Server) handleRevokeSession(writer http.ResponseWriter, request *http.Request) {
	presented := strings.TrimSpace(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "))
	if len(presented) == len(server.config.BearerToken) && subtle.ConstantTimeCompare([]byte(presented), []byte(server.config.BearerToken)) == 1 {
		writeProblem(writer, requestID(request.Context()), problem{Status: 422, Code: "bootstrap_token_not_revocable", Detail: "the bootstrap token must be rotated in edge configuration"})
		return
	}
	if err := server.store.RevokeSession(request.Context(), hashSecret(presented)); err != nil {
		writeProblem(writer, requestID(request.Context()), problem{Status: 401, Code: "invalid_session", Detail: "the local session is no longer active"})
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func hashSecret(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func randomDigits(length int) (string, error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	for index := range raw {
		raw[index] = '0' + raw[index]%10
	}
	return string(raw), nil
}

func decodeJSONBody(writer http.ResponseWriter, request *http.Request, maximum int64, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, maximum)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func (server *Server) handleCreateOrder(writer http.ResponseWriter, request *http.Request) {
	envelope, hash, ok := server.readMutation(writer, request)
	if !ok {
		return
	}
	var payload model.CreateOrderPayload
	if err := decodeStrict(envelope.Payload, &payload); err != nil {
		server.invalidPayload(writer, request, err)
		return
	}
	if err := payload.Order.Validate(); err != nil {
		server.invalidPayload(writer, request, err)
		return
	}
	response, err := server.store.CreateOrder(request.Context(), "POST /api/v1/orders", hash, envelope, payload.Order)
	server.writeCommandResult(writer, request, response, err)
}

func (server *Server) handleBrowserMutation(writer http.ResponseWriter, request *http.Request) {
	envelope, hash, ok := server.readMutation(writer, request)
	if !ok {
		return
	}
	var payload model.BrowserMutationPayload
	if err := decodeStrict(envelope.Payload, &payload); err != nil {
		server.invalidPayload(writer, request, err)
		return
	}
	const route = "POST /api/v1/sync/mutations"
	switch payload.EventType {
	case "com.feastcloud.order.created.v1":
		if payload.AggregateType != "order" || payload.Order == nil || payload.AggregateID == "" || payload.AggregateID != payload.Order.ID {
			server.invalidPayload(writer, request, errors.New("created event requires aggregateType order and matching aggregateId/order.id"))
			return
		}
		if err := payload.Order.Validate(); err != nil {
			server.invalidPayload(writer, request, err)
			return
		}
		response, err := server.store.CreateOrder(request.Context(), route, hash, envelope, *payload.Order)
		server.writeCommandResult(writer, request, response, err)
	case "com.feastcloud.order.status-changed.v1":
		orderID := payload.OrderID
		if orderID == "" {
			orderID = payload.AggregateID
		}
		if payload.AggregateType != "order" || !model.IsUUIDv7(orderID) || payload.AggregateID != orderID ||
			!payload.ToStatus.Valid() || payload.ExpectedVersion == 0 {
			server.invalidPayload(writer, request, errors.New("status event requires aggregateType order, matching UUIDv7 aggregateId/orderId, a valid toStatus, and expectedVersion"))
			return
		}
		response, err := server.store.TransitionOrderTickets(request.Context(), route, hash, orderID, envelope, payload.ToStatus, payload.ExpectedVersion)
		server.writeCommandResult(writer, request, response, err)
	case "com.feastcloud.kitchen-ticket.status-changed.v1":
		ticketID := payload.TicketID
		if ticketID == "" {
			ticketID = payload.AggregateID
		}
		if payload.AggregateType != "kitchenTicket" || !model.IsUUIDv7(ticketID) ||
			payload.AggregateID != ticketID || !model.IsUUIDv7(payload.OrderID) || !payload.ToStatus.Valid() ||
			payload.ExpectedVersion == 0 {
			server.invalidPayload(writer, request, errors.New("ticket status event requires aggregateType kitchenTicket, matching UUIDv7 aggregateId/ticketId, UUIDv7 orderId, a valid toStatus, and expectedVersion"))
			return
		}
		response, err := server.store.TransitionTicket(
			request.Context(),
			route,
			hash,
			ticketID,
			envelope,
			model.TransitionTicketPayload{
				ToStatus:        payload.ToStatus,
				ExpectedVersion: payload.ExpectedVersion,
				ExpectedOrderID: payload.OrderID,
			},
		)
		server.writeCommandResult(writer, request, response, err)
	default:
		server.invalidPayload(writer, request, fmt.Errorf("eventType %q is not supported", payload.EventType))
	}
}

func (server *Server) handleTransitionOrder(writer http.ResponseWriter, request *http.Request) {
	envelope, hash, ok := server.readMutation(writer, request)
	if !ok {
		return
	}
	var payload model.TransitionOrderPayload
	if err := decodeStrict(envelope.Payload, &payload); err != nil {
		server.invalidPayload(writer, request, err)
		return
	}
	if err := payload.Validate(); err != nil {
		server.invalidPayload(writer, request, err)
		return
	}
	id := request.PathValue("id")
	response, err := server.store.TransitionOrder(request.Context(), "POST /api/v1/orders/{id}/transitions", hash, id, envelope, payload)
	server.writeCommandResult(writer, request, response, err)
}

func (server *Server) handleTransitionTicket(writer http.ResponseWriter, request *http.Request) {
	envelope, hash, ok := server.readMutation(writer, request)
	if !ok {
		return
	}
	var payload model.TransitionTicketPayload
	if err := decodeStrict(envelope.Payload, &payload); err != nil {
		server.invalidPayload(writer, request, err)
		return
	}
	if err := payload.Validate(); err != nil {
		server.invalidPayload(writer, request, err)
		return
	}
	id := request.PathValue("id")
	response, err := server.store.TransitionTicket(request.Context(), "POST /api/v1/kitchen-tickets/{id}/transitions", hash, id, envelope, payload)
	server.writeCommandResult(writer, request, response, err)
}

func (server *Server) readMutation(writer http.ResponseWriter, request *http.Request) (model.MutationEnvelope, string, bool) {
	correlationID := requestID(request.Context())
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || (mediaType != "application/json" && mediaType != "application/cloudevents+json") {
		writeProblem(writer, correlationID, problem{Status: 415, Code: "unsupported_media_type", Detail: "Content-Type must be application/json"})
		return model.MutationEnvelope{}, "", false
	}
	headerKey := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if headerKey == "" {
		writeProblem(writer, correlationID, problem{Status: 400, Code: "idempotency_key_required", Detail: "Idempotency-Key is required"})
		return model.MutationEnvelope{}, "", false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, server.config.MaxBodyBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(writer, correlationID, problem{Status: 413, Code: "request_too_large", Detail: "request body exceeds the configured limit"})
		} else {
			writeProblem(writer, correlationID, problem{Status: 400, Code: "invalid_request", Detail: "request body could not be read"})
		}
		return model.MutationEnvelope{}, "", false
	}
	var envelope model.MutationEnvelope
	if err := decodeStrict(body, &envelope); err != nil {
		writeProblem(writer, correlationID, problem{Status: 400, Code: "invalid_json", Detail: err.Error()})
		return model.MutationEnvelope{}, "", false
	}
	if err := envelope.Validate(); err != nil {
		writeProblem(writer, correlationID, problem{Status: 422, Code: "invalid_mutation", Detail: err.Error()})
		return model.MutationEnvelope{}, "", false
	}
	if headerKey != envelope.IdempotencyKey {
		writeProblem(writer, correlationID, problem{Status: 422, Code: "idempotency_key_mismatch", Detail: "Idempotency-Key must match mutation.idempotencyKey"})
		return model.MutationEnvelope{}, "", false
	}
	if server.config.TenantID != "" && envelope.TenantID != server.config.TenantID {
		writeProblem(writer, correlationID, problem{Status: 403, Code: "tenant_scope_denied", Detail: "mutation tenantId is outside this edge scope"})
		return model.MutationEnvelope{}, "", false
	}
	if server.config.OutletID != "" && envelope.OutletID != server.config.OutletID {
		writeProblem(writer, correlationID, problem{Status: 403, Code: "outlet_scope_denied", Detail: "mutation outletId is outside this edge scope"})
		return model.MutationEnvelope{}, "", false
	}
	if supplied := request.Header.Get("X-Correlation-ID"); supplied != "" && envelope.CorrelationID != "" && supplied != envelope.CorrelationID {
		writeProblem(writer, correlationID, problem{Status: 422, Code: "correlation_id_mismatch", Detail: "X-Correlation-ID must match mutation.correlationId"})
		return model.MutationEnvelope{}, "", false
	}
	hash, err := canonicalHash(body)
	if err != nil {
		writeProblem(writer, correlationID, problem{Status: 400, Code: "invalid_json", Detail: err.Error()})
		return model.MutationEnvelope{}, "", false
	}
	return envelope, hash, true
}

func canonicalHash(body []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func (server *Server) invalidPayload(writer http.ResponseWriter, request *http.Request, err error) {
	writeProblem(writer, requestID(request.Context()), problem{Status: 422, Code: "invalid_payload", Detail: err.Error()})
}

func (server *Server) writeCommandResult(writer http.ResponseWriter, request *http.Request, response store.StoredResponse, err error) {
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	writer.Header().Set("Content-Type", response.ContentType)
	if response.Location != "" {
		writer.Header().Set("Location", response.Location)
	}
	if response.Replayed {
		writer.Header().Set("Idempotency-Replayed", "true")
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = writer.Write(response.Body)
}

func (server *Server) writeStoreError(writer http.ResponseWriter, request *http.Request, err error) {
	var commandError *store.CommandError
	if !errors.As(err, &commandError) {
		server.logger.ErrorContext(request.Context(), "edge store operation failed", "correlation_id", requestID(request.Context()), "error", err)
		writeProblem(writer, requestID(request.Context()), problem{Status: 500, Code: "storage_error", Detail: "the local operation could not be committed", Retryable: true})
		return
	}
	status := http.StatusConflict
	if commandError.Code == "not_found" {
		status = http.StatusNotFound
	}
	writeProblem(writer, requestID(request.Context()), problem{Status: status, Code: commandError.Code, Detail: commandError.Message, Fields: commandError.Details})
}

func (server *Server) handleListOrders(writer http.ResponseWriter, request *http.Request) {
	limit, ok := parseLimit(writer, request)
	if !ok {
		return
	}
	status := model.OrderStatus(request.URL.Query().Get("status"))
	if status != "" && !status.Valid() {
		writeProblem(writer, requestID(request.Context()), problem{Status: 422, Code: "invalid_filter", Detail: "status is not a valid order status"})
		return
	}
	orders, err := server.store.ListOrders(request.Context(), server.config.TenantID, server.config.OutletID, status, limit)
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	writeJSON(writer, 200, model.ResponseEnvelope{Data: orders, Meta: model.ResponseMeta{Count: len(orders)}})
}

func (server *Server) handleGetOrder(writer http.ResponseWriter, request *http.Request) {
	order, err := server.store.GetOrder(request.Context(), server.config.TenantID, server.config.OutletID, request.PathValue("id"))
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	writeJSON(writer, 200, model.ResponseEnvelope{Data: order})
}

func (server *Server) handleListTickets(writer http.ResponseWriter, request *http.Request) {
	server.listTickets(writer, request, request.URL.Query().Get("stationId"))
}

func (server *Server) handleStationTickets(writer http.ResponseWriter, request *http.Request) {
	server.listTickets(writer, request, request.PathValue("stationId"))
}

func (server *Server) listTickets(writer http.ResponseWriter, request *http.Request, stationID string) {
	limit, ok := parseLimit(writer, request)
	if !ok {
		return
	}
	status := model.TicketStatus(request.URL.Query().Get("status"))
	if status != "" && !status.Valid() {
		writeProblem(writer, requestID(request.Context()), problem{Status: 422, Code: "invalid_filter", Detail: "status is not a valid ticket status"})
		return
	}
	tickets, err := server.store.ListTickets(request.Context(), server.config.TenantID, server.config.OutletID, stationID, status, limit)
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	writeJSON(writer, 200, model.ResponseEnvelope{Data: tickets, Meta: model.ResponseMeta{Count: len(tickets)}})
}

func (server *Server) handleGetTicket(writer http.ResponseWriter, request *http.Request) {
	ticket, err := server.store.GetTicket(request.Context(), server.config.TenantID, server.config.OutletID, request.PathValue("id"))
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	writeJSON(writer, 200, model.ResponseEnvelope{Data: ticket})
}

func (server *Server) handleSyncStatus(writer http.ResponseWriter, request *http.Request) {
	stats, err := server.store.SyncStats(request.Context())
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	state := "disabled"
	if server.config.SyncEnabled {
		switch {
		case stats.LastError != "":
			state = "degraded"
		case stats.Reconciliation > 0:
			state = "attentionRequired"
		case stats.Pending > 0:
			state = "pending"
		default:
			state = "synchronized"
		}
	}
	writeJSON(writer, 200, map[string]any{"data": map[string]any{"enabled": server.config.SyncEnabled, "state": state, "outbox": stats}})
}

func parseLimit(writer http.ResponseWriter, request *http.Request) (int, bool) {
	value := request.URL.Query().Get("limit")
	if value == "" {
		return 100, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > 500 {
		writeProblem(writer, requestID(request.Context()), problem{Status: 422, Code: "invalid_filter", Detail: "limit must be between 1 and 500"})
		return 0, false
	}
	return limit, true
}

func decodeStrict[T any](raw []byte, destination *T) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request contains more than one JSON value")
		}
		return err
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
