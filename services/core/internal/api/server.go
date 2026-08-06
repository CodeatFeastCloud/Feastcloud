// SPDX-License-Identifier: AGPL-3.0-only

// Package api exposes FeastCloud's versioned REST interface.
package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/auth"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

const (
	defaultMaxBodyBytes   = int64(1 << 20)
	defaultVersion        = "dev"
	defaultIdempotencyTTL = 90 * 24 * time.Hour
)

type contextKey string

const requestIDKey contextKey = "request_id"

const principalKey contextKey = "principal"

type principal = auth.Principal

// Config controls transport behavior without changing domain semantics.
type Config struct {
	Version             string
	MaxBodyBytes        int64
	IdempotencyTTL      time.Duration
	IdempotencyStore    idempotency.Store
	UserAuthenticator   auth.Authenticator
	DeviceAuthenticator auth.Authenticator
	AuthMode            string
	AllowedOrigin       string
	SyncRepository      store.SyncRepository
	RequireSyncReady    bool
	SnapshotSigningKey  ed25519.PrivateKey
}

// Server is the HTTP adapter for the core repository.
type Server struct {
	repository          store.Repository
	syncRepository      store.SyncRepository
	requireSyncReady    bool
	idempotency         idempotency.Store
	logger              *slog.Logger
	version             string
	maxBody             int64
	now                 func() time.Time
	mux                 *http.ServeMux
	userAuthenticator   auth.Authenticator
	deviceAuthenticator auth.Authenticator
	authMode            string
	allowedOrigin       string
	snapshotSigningKey  ed25519.PrivateKey
}

// NewServer constructs a fully routed HTTP server.
func NewServer(repository store.Repository, logger *slog.Logger, config Config) *Server {
	if repository == nil {
		panic("api: repository is required")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if config.Version == "" {
		config.Version = defaultVersion
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = defaultMaxBodyBytes
	}
	if config.IdempotencyTTL <= 0 {
		config.IdempotencyTTL = defaultIdempotencyTTL
	}
	if config.SyncRepository == nil {
		config.SyncRepository = store.NewUnavailableSyncRepository()
	}
	if config.IdempotencyStore == nil {
		config.IdempotencyStore = idempotency.NewMemoryStore(config.IdempotencyTTL)
	}
	if config.UserAuthenticator == nil {
		config.UserAuthenticator = auth.DemoAuthenticator{}
	}
	if config.DeviceAuthenticator == nil {
		config.DeviceAuthenticator = auth.DemoAuthenticator{}
	}
	if config.AuthMode == "" {
		config.AuthMode = "demo"
	}
	if config.AllowedOrigin == "" {
		config.AllowedOrigin = "http://localhost:5173"
	}
	if len(config.SnapshotSigningKey) == 0 && config.AuthMode == "demo" {
		seed := sha256.Sum256([]byte("FeastCloud development snapshot signer; never use in production"))
		config.SnapshotSigningKey = ed25519.NewKeyFromSeed(seed[:])
	}
	server := &Server{
		repository:        repository,
		syncRepository:    config.SyncRepository,
		requireSyncReady:  config.RequireSyncReady,
		idempotency:       config.IdempotencyStore,
		logger:            logger,
		version:           config.Version,
		maxBody:           config.MaxBodyBytes,
		now:               time.Now,
		mux:               http.NewServeMux(),
		userAuthenticator: config.UserAuthenticator, deviceAuthenticator: config.DeviceAuthenticator, authMode: config.AuthMode, allowedOrigin: config.AllowedOrigin,
		snapshotSigningKey: config.SnapshotSigningKey,
	}
	server.routes()
	return server
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReadiness)
	s.mux.HandleFunc("GET /api/v1", s.handleDiscovery)
	s.mux.HandleFunc("GET /api/v1/public/ordering/{slug}", s.handlePublicOrderingMenu)
	s.mux.HandleFunc("POST /api/v1/public/ordering/{slug}/requests", s.handleSubmitPublicOrder)
	s.mux.HandleFunc("GET /api/v1/web-order-requests", s.handleGuestOrderRequests)

	s.mux.HandleFunc("GET /api/v1/organizations", s.handleOrganizations)
	s.mux.HandleFunc("POST /api/v1/organizations", s.handleCreateOrganization)
	s.mux.HandleFunc("GET /api/v1/organizations/{id}", s.handleOrganization)

	s.mux.HandleFunc("GET /api/v1/outlets", s.handleOutlets)
	s.mux.HandleFunc("POST /api/v1/outlets", s.handleCreateOutlet)
	s.mux.HandleFunc("GET /api/v1/outlets/{id}", s.handleOutlet)

	s.mux.HandleFunc("GET /api/v1/brands", s.handleBrands)
	s.mux.HandleFunc("POST /api/v1/brands", s.handleCreateBrand)
	s.mux.HandleFunc("GET /api/v1/brands/{id}", s.handleBrand)
	s.mux.HandleFunc("GET /api/v1/brand-outlet-assignments", s.handleBrandOutletAssignments)
	s.mux.HandleFunc("POST /api/v1/brand-outlet-assignments", s.handleSetBrandOutletAssignment)
	s.mux.HandleFunc("POST /api/v1/platform/tenants", s.handleProvisionTenant)

	s.mux.HandleFunc("GET /api/v1/stations", s.handleStations)
	s.mux.HandleFunc("POST /api/v1/stations", s.handleCreateStation)
	s.mux.HandleFunc("GET /api/v1/stations/{id}", s.handleStation)

	s.mux.HandleFunc("GET /api/v1/orders", s.handleOrders)
	s.mux.HandleFunc("POST /api/v1/orders", s.handleCreateOrder)
	s.mux.HandleFunc("GET /api/v1/orders/{id}", s.handleOrder)
	s.mux.HandleFunc("POST /api/v1/orders/{id}/transitions", s.handleTransitionOrder)

	s.mux.HandleFunc("GET /api/v1/kitchen-tickets", s.handleKitchenTickets)
	s.mux.HandleFunc("POST /api/v1/kitchen-tickets", s.handleCreateKitchenTicket)
	s.mux.HandleFunc("GET /api/v1/kitchen-tickets/{id}", s.handleKitchenTicket)
	s.mux.HandleFunc("POST /api/v1/kitchen-tickets/{id}/transitions", s.handleTransitionKitchenTicket)

	s.mux.HandleFunc("GET /api/v1/audit-events", s.handleAuditEvents)
	s.mux.HandleFunc("GET /api/v1/units", s.handleUnits)
	s.mux.HandleFunc("POST /api/v1/units", s.handleCreateUnit)
	s.mux.HandleFunc("GET /api/v1/ingredients", s.handleIngredients)
	s.mux.HandleFunc("POST /api/v1/ingredients", s.handleCreateIngredient)
	s.mux.HandleFunc("GET /api/v1/recipes", s.handleRecipes)
	s.mux.HandleFunc("POST /api/v1/recipes", s.handleCreateRecipe)
	s.mux.HandleFunc("POST /api/v1/recipes/{id}/versions", s.handleAddRecipeVersion)
	s.mux.HandleFunc("GET /api/v1/menu-items", s.handleMenuItems)
	s.mux.HandleFunc("POST /api/v1/menu-items", s.handleCreateMenuItem)
	s.mux.HandleFunc("GET /api/v1/menu-studios", s.handleMenuStudios)
	s.mux.HandleFunc("POST /api/v1/menu-studios", s.handleCreateMenuStudio)
	s.mux.HandleFunc("POST /api/v1/menu-studios/{id}/versions", s.handleAddMenuStudioVersion)
	s.mux.HandleFunc("POST /api/v1/pos-checkouts", s.handleCheckoutPOS)
	s.mux.HandleFunc("POST /api/v1/inventory-events", s.handleRecordInventoryEvent)
	s.mux.HandleFunc("POST /api/v1/inventory-counts", s.handleRecordInventoryCount)
	s.mux.HandleFunc("GET /api/v1/inventory-summary", s.handleInventorySummary)
	s.mux.HandleFunc("GET /api/v1/production-batches", s.handleProductionBatches)
	s.mux.HandleFunc("POST /api/v1/production-batches", s.handleCreateProductionBatch)
	s.mux.HandleFunc("POST /api/v1/production-batches/{id}/transitions", s.handleTransitionProductionBatch)
	s.mux.HandleFunc("GET /api/v1/order-imports", s.handleOrderImports)
	s.mux.HandleFunc("POST /api/v1/order-imports", s.handleImportOrders)
	s.mux.HandleFunc("GET /api/v1/menu-imports", s.handleMenuImportDrafts)
	s.mux.HandleFunc("POST /api/v1/menu-imports", s.handleCreateMenuImportDraft)
	s.mux.HandleFunc("GET /api/v1/planning-runs", s.handlePlanningRuns)
	s.mux.HandleFunc("POST /api/v1/planning-runs", s.handleGeneratePlanningRun)
	s.mux.HandleFunc("GET /api/v1/dashboard/daily", s.handleDailyDashboard)
	s.mux.HandleFunc("GET /api/v1/configuration-snapshots", s.handleSnapshots)
	s.mux.HandleFunc("POST /api/v1/configuration-snapshots", s.handlePublishSnapshot)
	s.mux.HandleFunc("GET /api/v1/edge-checkpoints", s.handleCheckpoints)
	s.mux.HandleFunc("GET /api/v1/reconciliation-cases", s.handleCases)
	s.mux.HandleFunc("POST /api/v1/reconciliation-cases/{id}/actions", s.handleCaseAction)
	s.mux.HandleFunc("GET /api/v1/incidents", s.handleIncidents)
	s.mux.HandleFunc("POST /api/v1/incidents", s.handleCreateIncident)
	s.mux.HandleFunc("POST /api/v1/incidents/{id}/actions", s.handleIncidentAction)
	s.mux.HandleFunc("GET /api/v1/backup-evidence", s.handleBackupEvidence)
	s.mux.HandleFunc("POST /api/v1/backup-evidence", s.handleRecordBackup)
	s.mux.HandleFunc("POST /api/v1/restore-drills", s.handleRecordDrill)
	s.mux.HandleFunc("GET /api/v1/suppliers", s.handleSuppliers)
	s.mux.HandleFunc("POST /api/v1/suppliers", s.handleCreateSupplier)
	s.mux.HandleFunc("GET /api/v1/purchase-orders", s.handlePOs)
	s.mux.HandleFunc("POST /api/v1/purchase-orders", s.handleCreatePO)
	s.mux.HandleFunc("POST /api/v1/purchase-orders/{id}/transitions", s.handlePOTransition)
	s.mux.HandleFunc("POST /api/v1/goods-receipts", s.handleReceivePO)
	s.mux.HandleFunc("GET /api/v1/temperature-logs", s.handleTemperatureLogs)
	s.mux.HandleFunc("POST /api/v1/temperature-logs", s.handleRecordTemperature)
	s.mux.HandleFunc("GET /api/v1/checklists", s.handleChecklists)
	s.mux.HandleFunc("POST /api/v1/checklists", s.handleCreateChecklist)
	s.mux.HandleFunc("POST /api/v1/checklists/{id}/items/{itemId}/complete", s.handleCompleteChecklistItem)
	s.mux.HandleFunc("GET /api/v1/staff-members", s.handleStaff)
	s.mux.HandleFunc("POST /api/v1/staff-members", s.handleCreateStaff)
	s.mux.HandleFunc("GET /api/v1/shifts", s.handleShifts)
	s.mux.HandleFunc("POST /api/v1/shifts", s.handleCreateShift)
	s.mux.HandleFunc("POST /api/v1/shifts/{id}/transitions", s.handleShiftTransition)
	s.mux.HandleFunc("GET /api/v1/tasks", s.handleTasks)
	s.mux.HandleFunc("POST /api/v1/tasks", s.handleCreateTask)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/transitions", s.handleTaskTransition)
	s.mux.HandleFunc("GET /api/v1/menu-availability", s.handleAvailability)
	s.mux.HandleFunc("POST /api/v1/menu-availability", s.handleSetAvailability)
	s.mux.HandleFunc("GET /api/v1/dining-tables", s.handleTables)
	s.mux.HandleFunc("POST /api/v1/dining-tables", s.handleCreateTable)
	s.mux.HandleFunc("POST /api/v1/dining-tables/{id}/transitions", s.handleTableTransition)
	s.mux.HandleFunc("GET /api/v1/dining-sessions", s.handleSessions)
	s.mux.HandleFunc("POST /api/v1/dining-sessions", s.handleOpenSession)
	s.mux.HandleFunc("POST /api/v1/dining-sessions/{id}/close", s.handleCloseSession)
	s.mux.HandleFunc("GET /api/v1/cash-shifts", s.handleCashShifts)
	s.mux.HandleFunc("POST /api/v1/cash-shifts", s.handleOpenCash)
	s.mux.HandleFunc("POST /api/v1/cash-shifts/{id}/close", s.handleCloseCash)
	s.mux.HandleFunc("POST /api/v1/tenders", s.handleCaptureTender)
	s.mux.HandleFunc("GET /api/v1/tenders", s.handleTenders)
	s.mux.HandleFunc("POST /api/v1/tenders/{id}/reverse", s.handleReverseTender)
	s.mux.HandleFunc("POST /api/v1/tender-settlements", s.handleSettle)
	s.mux.HandleFunc("GET /api/v1/sales-channels", s.handleSalesChannels)
	s.mux.HandleFunc("POST /api/v1/sales-channels", s.handleCreateSalesChannel)
	s.mux.HandleFunc("GET /api/v1/connector-installations", s.handleConnectors)
	s.mux.HandleFunc("POST /api/v1/connector-installations", s.handleCreateConnector)
	s.mux.HandleFunc("GET /api/v1/connector-order-inbox", s.handleConnectorInbox)
	s.mux.HandleFunc("GET /api/v1/connector-order-inbox/stream", s.handleConnectorInboxStream)
	s.mux.HandleFunc("POST /api/v1/connector-order-inbox", s.handleIngestConnectorOrder)
	s.mux.HandleFunc("POST /api/v1/connector-order-inbox/{id}/decisions", s.handleConnectorInboxDecision)
	s.mux.HandleFunc("GET /api/v1/menu-sellability", s.handleSellability)
	s.mux.HandleFunc("GET /api/v1/station-capacity-limits", s.handleStationCapacity)
	s.mux.HandleFunc("POST /api/v1/station-capacity-limits", s.handleSetStationCapacity)
	s.mux.HandleFunc("GET /api/v1/kitchen-print-jobs", s.handlePrintJobs)
	s.mux.HandleFunc("POST /api/v1/kitchen-print-jobs", s.handleCreatePrintJob)
	s.mux.HandleFunc("POST /api/v1/kitchen-print-jobs/{id}/actions", s.handlePrintJobAction)
	s.mux.HandleFunc("GET /api/v1/pickup-tokens", s.handlePickupTokens)
	s.mux.HandleFunc("POST /api/v1/pickup-tokens", s.handleIssuePickupToken)
	s.mux.HandleFunc("POST /api/v1/pickup-tokens/{id}/transitions", s.handlePickupTokenTransition)
	s.mux.HandleFunc("GET /api/v1/qr-ordering-links", s.handleQROrderingLinks)
	s.mux.HandleFunc("POST /api/v1/qr-ordering-links", s.handleCreateQROrderingLink)
	s.mux.HandleFunc("GET /api/v1/stock-transfers", s.handleStockTransfers)
	s.mux.HandleFunc("POST /api/v1/stock-transfers", s.handleCreateStockTransfer)
	s.mux.HandleFunc("POST /api/v1/stock-transfers/{id}/transitions", s.handleStockTransferTransition)
	s.mux.HandleFunc("GET /api/v1/replenishment-rules", s.handleReplenishmentRules)
	s.mux.HandleFunc("POST /api/v1/replenishment-rules", s.handleSetReplenishmentRule)
	s.mux.HandleFunc("GET /api/v1/replenishment-suggestions", s.handleReplenishmentSuggestions)
	s.mux.HandleFunc("GET /api/v1/outlet-control-profile", s.handleOutletControlProfile)
	s.mux.HandleFunc("POST /api/v1/outlet-control-profile", s.handleSetOutletControlProfile)
	s.mux.HandleFunc("GET /api/v1/hardware-devices", s.handleHardwareDevices)
	s.mux.HandleFunc("POST /api/v1/hardware-devices", s.handleRegisterHardwareDevice)
	s.mux.HandleFunc("GET /api/v1/implementation-runbooks", s.handleRunbooks)
	s.mux.HandleFunc("POST /api/v1/implementation-runbooks", s.handleCreateRunbook)
	s.mux.HandleFunc("GET /api/v1/reports/gst", s.handleGSTReport)
	s.mux.HandleFunc("GET /api/v1/reports/day-end", s.handleDayEndReport)
	s.mux.HandleFunc("GET /api/v1/guests", s.handleGuests)
	s.mux.HandleFunc("POST /api/v1/guests", s.handleCreateGuest)
	s.mux.HandleFunc("POST /api/v1/guests/{id}/consent", s.handleGuestConsent)
	s.mux.HandleFunc("GET /api/v1/reservations", s.handleReservations)
	s.mux.HandleFunc("POST /api/v1/reservations", s.handleCreateReservation)
	s.mux.HandleFunc("POST /api/v1/reservations/{id}/transitions", s.handleReservationTransition)
	s.mux.HandleFunc("GET /api/v1/promotions", s.handlePromotions)
	s.mux.HandleFunc("POST /api/v1/promotions", s.handleCreatePromotion)
	s.mux.HandleFunc("POST /api/v1/promotions/redeem", s.handleRedeemPromotion)
	s.mux.HandleFunc("GET /api/v1/loyalty-accounts", s.handleLoyaltyAccounts)
	s.mux.HandleFunc("POST /api/v1/loyalty-accounts/{id}/events", s.handleLoyaltyEvent)
	s.mux.HandleFunc("POST /api/v1/devices", s.handleRegisterDevice)
	s.mux.HandleFunc("POST /api/v1/devices/{id}/revoke", s.handleRevokeDevice)
	s.mux.HandleFunc("POST /api/v1/sync/operations", s.handleSyncOperations)
}

// ServeHTTP adds request identity, security headers, recovery, and structured logging.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := s.now()
	requestID := cleanRequestID(r.Header.Get("X-Request-ID"))
	if requestID == "" {
		requestID = mustRequestID()
	}
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")

	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	ctx := context.WithValue(r.Context(), requestIDKey, requestID)
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.ErrorContext(ctx, "http handler panic",
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"panic", recovered,
				"stack", string(debug.Stack()),
			)
			if !recorder.wroteHeader {
				writeError(recorder, requestID, apiError{
					Status:  http.StatusInternalServerError,
					Code:    "internal_error",
					Message: "an unexpected error occurred",
				})
			}
		}
		s.logger.InfoContext(ctx, "http request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}()
	if !s.applyCORS(recorder, r.WithContext(ctx)) {
		return
	}
	if requiresDemoAuthentication(r.URL.Path) {
		authenticator := s.userAuthenticator
		if r.URL.Path == "/api/v1/sync/operations" {
			authenticator = s.deviceAuthenticator
		}
		requestPrincipal, err := authenticator.Authenticate(ctx, r)
		if err != nil || requestPrincipal.TenantID == "" || requestPrincipal.ActorID == "" {
			writeError(recorder, requestID, apiError{
				Status:  http.StatusUnauthorized,
				Code:    "authentication_required",
				Message: "a valid user token or enrolled device certificate is required",
			})
			return
		}
		if !routeAuthorized(requestPrincipal, r) {
			writeError(recorder, requestID, apiError{Status: http.StatusForbidden, Code: "permission_denied", Message: "the authenticated role cannot perform this action"})
			return
		}
		ctx = context.WithValue(ctx, principalKey, requestPrincipal)
		recorder.Header().Set("X-FeastCloud-Auth-Mode", s.authMode)
	}
	s.mux.ServeHTTP(recorder, r.WithContext(ctx))
}

func (s *Server) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if origin != s.allowedOrigin {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: http.StatusForbidden, Code: "cors_origin_denied", Message: "browser origin is not allowed"})
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Add("Vary", "Origin")
	w.Header().Set("Access-Control-Expose-Headers", "Location, Idempotency-Replayed, X-Request-ID")
	if r.Method != http.MethodOptions {
		return true
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID, X-FeastCloud-Tenant-ID, X-FeastCloud-Actor-ID, X-FeastCloud-Platform-Admin")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
	return false
}

func routeAuthorized(value principal, request *http.Request) bool {
	if value.Kind == "device" {
		return request.Method == http.MethodPost && request.URL.Path == "/api/v1/sync/operations"
	}
	if value.IsPlatformAdmin() {
		return strings.HasPrefix(request.URL.Path, "/api/v1/platform/")
	}
	if value.HasRole("owner") || value.HasRole("manager") {
		return true
	}
	path := request.URL.Path
	if value.HasRole("cashier") {
		return request.Method == http.MethodPost && path == "/api/v1/orders" || request.Method == http.MethodGet && path == "/api/v1/orders" && value.AllowsOutlet(request.URL.Query().Get("outletId"))
	}
	if value.HasRole("chef") {
		return request.Method == http.MethodGet && (path == "/api/v1/orders" || path == "/api/v1/kitchen-tickets" || path == "/api/v1/production-batches") && value.AllowsOutlet(request.URL.Query().Get("outletId")) || request.Method == http.MethodPost && (strings.HasPrefix(path, "/api/v1/kitchen-tickets/") || strings.HasPrefix(path, "/api/v1/production-batches/")) && strings.HasSuffix(path, "/transitions")
	}
	return false
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

func (recorder *statusRecorder) Write(data []byte) (int, error) {
	if !recorder.wroteHeader {
		recorder.WriteHeader(http.StatusOK)
	}
	return recorder.ResponseWriter.Write(data)
}

func (recorder *statusRecorder) Flush() {
	if !recorder.wroteHeader { recorder.WriteHeader(http.StatusOK) }
	if flusher, ok := recorder.ResponseWriter.(http.Flusher); ok { flusher.Flush() }
}

type mutationEnvelope struct {
	domain.MutationMetadata
	Payload json.RawMessage `json:"payload"`
}

type responseMeta struct {
	RequestID         string `json:"requestId"`
	IdempotencyReplay bool   `json:"idempotencyReplay,omitempty"`
}

type responseEnvelope struct {
	Data any          `json:"data"`
	Meta responseMeta `json:"meta"`
}

type apiError struct {
	Status     int
	Code       string
	Message    string
	MessageKey string
	Retryable  bool
	Details    map[string]string
}

type problemDocument struct {
	Type          string           `json:"type"`
	Title         string           `json:"title"`
	Status        int              `json:"status"`
	Detail        string           `json:"detail"`
	Code          string           `json:"code"`
	MessageKey    string           `json:"messageKey"`
	CorrelationID string           `json:"correlationId"`
	Retryable     bool             `json:"retryable"`
	Violations    []fieldViolation `json:"violations,omitempty"`
}

type fieldViolation struct {
	Field      string `json:"field"`
	Code       string `json:"code"`
	MessageKey string `json:"messageKey"`
}

type operationValue struct {
	Data  any
	Error *apiError
}

type mutationOperation func(context.Context, domain.MutationMetadata, json.RawMessage) idempotency.Result

func (s *Server) executeMutation(w http.ResponseWriter, r *http.Request, operation mutationOperation) {
	requestID := requestIDFrom(r.Context())
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, requestID, apiError{
			Status:  http.StatusUnsupportedMediaType,
			Code:    "unsupported_media_type",
			Message: "Content-Type must be application/json",
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, requestID, apiError{
				Status:  http.StatusRequestEntityTooLarge,
				Code:    "request_too_large",
				Message: fmt.Sprintf("request body must not exceed %d bytes", s.maxBody),
			})
			return
		}
		writeError(w, requestID, apiError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "could not read request body",
		})
		return
	}

	var envelope mutationEnvelope
	if err := decodeStrict(body, &envelope); err != nil {
		writeError(w, requestID, apiError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_json",
			Message: err.Error(),
		})
		return
	}
	if len(envelope.Payload) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Payload), []byte("null")) {
		writeError(w, requestID, apiError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "payload is required",
		})
		return
	}
	if err := envelope.MutationMetadata.Validate(); err != nil {
		writeError(w, requestID, apiError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "invalid_metadata",
			Message: err.Error(),
		})
		return
	}
	requestPrincipal, ok := principalFrom(r.Context())
	if !ok {
		writeError(w, requestID, apiError{
			Status:  http.StatusUnauthorized,
			Code:    "authentication_required",
			Message: "an authenticated principal is required",
		})
		return
	}
	if envelope.ActorID != requestPrincipal.ActorID || (!requestPrincipal.IsPlatformAdmin() && envelope.TenantID != requestPrincipal.TenantID) {
		writeError(w, requestID, apiError{
			Status:  http.StatusForbidden,
			Code:    "principal_scope_mismatch",
			Message: "mutation tenantId and actorId must match the authenticated scope",
		})
		return
	}
	if !requestPrincipal.IsPlatformAdmin() && !requestPrincipal.AllowsOutlet(envelope.OutletID) {
		writeError(w, requestID, apiError{Status: http.StatusForbidden, Code: "outlet_permission_denied", Message: "the authenticated principal is not assigned to this outlet"})
		return
	}
	headerKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if headerKey == "" {
		writeError(w, requestID, apiError{
			Status:  http.StatusBadRequest,
			Code:    "missing_idempotency_key",
			Message: "Idempotency-Key header is required",
		})
		return
	}
	if headerKey != envelope.IdempotencyKey {
		writeError(w, requestID, apiError{
			Status:  http.StatusBadRequest,
			Code:    "idempotency_key_mismatch",
			Message: "Idempotency-Key header must match idempotencyKey",
		})
		return
	}
	correlationHeader := strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
	if correlationHeader != "" && envelope.CorrelationID != "" && correlationHeader != envelope.CorrelationID {
		writeError(w, requestID, apiError{
			Status:  http.StatusBadRequest,
			Code:    "correlation_id_mismatch",
			Message: "X-Correlation-ID header must match correlationId",
		})
		return
	}
	if envelope.CorrelationID == "" {
		envelope.CorrelationID = correlationHeader
	}

	canonicalBody, err := canonicalJSON(body)
	if err != nil {
		writeError(w, requestID, apiError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_json",
			Message: "request body could not be canonicalized",
		})
		return
	}
	fingerprintBytes := sha256.Sum256(append([]byte(r.Method+"\n"+r.URL.Path+"\n"), canonicalBody...))
	fingerprint := hex.EncodeToString(fingerprintBytes[:])
	scopeTenantID := envelope.TenantID
	if requestPrincipal.IsPlatformAdmin() {
		// The target tenant is new and therefore has no idempotency ledger yet.
		// Platform commands are deduplicated in the operator's pre-existing
		// control-plane tenant, never in the customer tenancy being created.
		scopeTenantID = requestPrincipal.TenantID
	}
	scope := idempotency.Scope{TenantID: scopeTenantID, ActorID: envelope.ActorID, Route: r.Method + " " + r.URL.Path, Key: headerKey}

	result, replayed, err := s.idempotency.Do(r.Context(), scope, fingerprint, func() idempotency.Result {
		return operation(r.Context(), envelope.MutationMetadata, envelope.Payload)
	})
	if errors.Is(err, idempotency.ErrFingerprintConflict) {
		writeError(w, requestID, apiError{
			Status:  http.StatusConflict,
			Code:    "idempotency_key_reused",
			Message: "this idempotency key was already used with a different request body",
		})
		return
	}
	if err != nil {
		writeError(w, requestID, apiError{
			Status:  http.StatusServiceUnavailable,
			Code:    "request_cancelled",
			Message: "the request ended before the original mutation completed",
		})
		return
	}

	var value operationValue
	if err := json.Unmarshal(result.Value, &value); err != nil {
		writeError(w, requestID, apiError{
			Status:  http.StatusInternalServerError,
			Code:    "internal_error",
			Message: "the operation returned an invalid result",
		})
		return
	}
	for key, value := range result.Headers {
		w.Header().Set(key, value)
	}
	w.Header().Set("Idempotency-Replayed", fmt.Sprintf("%t", replayed))
	if value.Error != nil {
		writeErrorWithReplay(w, requestID, *value.Error, replayed)
		return
	}
	writeJSON(w, result.Status, responseEnvelope{
		Data: value.Data,
		Meta: responseMeta{RequestID: requestID, IdempotencyReplay: replayed},
	})
}

func successResult(status int, data any, location string) idempotency.Result {
	headers := make(map[string]string)
	if location != "" {
		headers["Location"] = location
	}
	value, _ := json.Marshal(operationValue{Data: data})
	return idempotency.Result{
		Status:  status,
		Value:   value,
		Headers: headers,
	}
}

func errorResult(value apiError) idempotency.Result {
	encoded, _ := json.Marshal(operationValue{Error: &value})
	return idempotency.Result{
		Status: value.Status,
		Value:  encoded,
	}
}

func repositoryError(err error) idempotency.Result {
	switch {
	case errors.Is(err, store.ErrPlatformProvisioningUnavailable):
		return errorResult(apiError{Status: http.StatusServiceUnavailable, Code: "platform_control_plane_unavailable", Message: "a dedicated platform provisioning database role is required to create customer tenants", Retryable: false})
	case errors.Is(err, store.ErrInvalidReference):
		return errorResult(apiError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "invalid_reference",
			Message: err.Error(),
		})
	case errors.Is(err, store.ErrConflict):
		return errorResult(apiError{
			Status:  http.StatusConflict,
			Code:    "conflict",
			Message: err.Error(),
		})
	case errors.Is(err, store.ErrVersionConflict):
		return errorResult(apiError{Status: http.StatusConflict, Code: "version_conflict", Message: err.Error()})
	case errors.Is(err, store.ErrInvalidTransition):
		return errorResult(apiError{Status: http.StatusUnprocessableEntity, Code: "invalid_transition", Message: err.Error()})
	default:
		return errorResult(apiError{
			Status:  http.StatusInternalServerError,
			Code:    "persistence_error",
			Message: "the mutation could not be persisted",
		})
	}
}

func decodeStrict(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("request body must be valid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain exactly one JSON value")
	}
	return nil
}

func canonicalJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("JSON must contain exactly one value")
	}
	return json.Marshal(value)
}

func writeData(w http.ResponseWriter, requestID string, status int, data any) {
	writeJSON(w, status, responseEnvelope{
		Data: data,
		Meta: responseMeta{RequestID: requestID},
	})
}

func writeError(w http.ResponseWriter, requestID string, value apiError) {
	writeErrorWithReplay(w, requestID, value, false)
}

func writeErrorWithReplay(w http.ResponseWriter, requestID string, value apiError, replay bool) {
	messageKey := value.MessageKey
	if messageKey == "" {
		messageKey = "errors." + strings.ReplaceAll(value.Code, "_", ".")
	}
	violations := make([]fieldViolation, 0, len(value.Details))
	for field, code := range value.Details {
		violations = append(violations, fieldViolation{
			Field:      field,
			Code:       code,
			MessageKey: "validation." + strings.ReplaceAll(code, "_", "."),
		})
	}
	if replay {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	writeJSONWithContentType(w, value.Status, problemDocument{
		Type:          "https://docs.feastcloud.org/problems/" + strings.ReplaceAll(value.Code, "_", "-"),
		Title:         http.StatusText(value.Status),
		Status:        value.Status,
		Detail:        value.Message,
		Code:          value.Code,
		MessageKey:    messageKey,
		CorrelationID: requestID,
		Retryable:     value.Retryable,
		Violations:    violations,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	writeJSONWithContentType(w, status, value)
}

func writeJSONWithContentType(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func requestIDFrom(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey).(string)
	return requestID
}

func principalFrom(ctx context.Context) (principal, bool) {
	value, ok := ctx.Value(principalKey).(principal)
	return value, ok
}

func requiresDemoAuthentication(path string) bool {
	return strings.HasPrefix(path, "/api/v1/") && !strings.HasPrefix(path, "/api/v1/public/ordering/")
}

func cleanRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, character := range value {
		if character < 33 || character > 126 {
			return ""
		}
	}
	return value
}

var fallbackID atomic.Uint64

func mustRequestID() string {
	identifier, err := newID("req")
	if err == nil {
		return identifier
	}
	return fmt.Sprintf("req_fallback_%d_%d", time.Now().UnixNano(), fallbackID.Add(1))
}

func newID(prefix string) (string, error) {
	uuid, err := newUUIDv7(time.Now().UTC())
	if err != nil {
		return "", err
	}
	return prefix + "_" + uuid, nil
}

func newUUIDv7(now time.Time) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate identifier: %w", err)
	}
	milliseconds := uint64(now.UnixMilli())
	buffer[0] = byte(milliseconds >> 40)
	buffer[1] = byte(milliseconds >> 32)
	buffer[2] = byte(milliseconds >> 24)
	buffer[3] = byte(milliseconds >> 16)
	buffer[4] = byte(milliseconds >> 8)
	buffer[5] = byte(milliseconds)
	buffer[6] = (buffer[6] & 0x0f) | 0x70
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buffer)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		encoded[0:8],
		encoded[8:12],
		encoded[12:16],
		encoded[16:20],
		encoded[20:32],
	), nil
}
