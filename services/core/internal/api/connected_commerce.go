// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

func (s *Server) connectedCommerce() (store.ConnectedCommerceRepository, bool) {
	v, ok := s.repository.(store.ConnectedCommerceRepository)
	return v, ok
}

func connectedUnavailable(w http.ResponseWriter, r *http.Request) {
	writeError(w, requestIDFrom(r.Context()), apiError{Status: http.StatusNotImplemented, Code: "connected_commerce_unavailable", Message: "connected commerce requires PostgreSQL"})
}

func validOutletScoped(inlet, outlet string) bool { return domain.ValidUUID(inlet) && inlet == outlet }
func validShortText(value string, max int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= max
}
func validChannelType(value string) bool {
	return value == "pos" || value == "qr" || value == "web" || value == "aggregator" || value == "call_center"
}
func validDate(value string) bool { _, err := time.Parse("2006-01-02", value); return err == nil }

func (s *Server) handleSalesChannels(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.SalesChannels(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}

func (s *Server) handleCreateSalesChannel(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.SalesChannel
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.ID) || !validOutletScoped(in.OutletID, meta.OutletID) || !domain.ValidCode(in.Code) || !validShortText(in.Name, 160) || !validChannelType(in.Type) {
				return fmt.Errorf("sales channel is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.TenantID = meta.TenantID
		in.OutletID = meta.OutletID
		in.Code = strings.TrimSpace(in.Code)
		in.Name = strings.TrimSpace(in.Name)
		in.RecordMetadata = newRecordMetadata(now)
		audit, err := newAuditEvent(meta, "sales_channel.created", "sales_channel", in.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err := repo.CreateSalesChannel(ctx, in, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, in, "/api/v1/sales-channels/"+in.ID)
	})
}

func (s *Server) handleConnectors(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.ConnectorInstallations(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}
func (s *Server) handleCreateConnector(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.ConnectorInstallation
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.ID) || !validOutletScoped(in.OutletID, meta.OutletID) || (in.ChannelID != "" && !domain.ValidUUID(in.ChannelID)) || !validShortText(in.Provider, 80) || !validShortText(in.ManifestVersion, 64) || (in.Status != "draft" && in.Status != "healthy" && in.Status != "degraded" && in.Status != "disabled") {
				return fmt.Errorf("connector installation is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.TenantID = meta.TenantID
		in.OutletID = meta.OutletID
		in.Provider = strings.TrimSpace(in.Provider)
		in.ManifestVersion = strings.TrimSpace(in.ManifestVersion)
		if in.Configuration == nil {
			in.Configuration = map[string]any{}
		}
		in.RecordMetadata = newRecordMetadata(now)
		audit, err := newAuditEvent(meta, "connector.installed", "connector_installation", in.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err = repo.CreateConnectorInstallation(ctx, in, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, in, "/api/v1/connector-installations/"+in.ID)
	})
}

func (s *Server) handleConnectorInbox(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.ConnectorOrderInbox(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}

// handleConnectorInboxStream keeps the order desk current without requiring
// provider adapters to know about browsers. Adapters write immutable inbox
// evidence; this outlet-scoped stream publishes a fresh projection whenever it
// changes. A periodic query is intentional so it works with every repository
// implementation and across multiple Core replicas.
func (s *Server) handleConnectorInboxStream(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: http.StatusNotImplemented, Code: "streaming_unavailable", Message: "response streaming is unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	last := ""
	send := func() bool {
		values, err := repo.ConnectorOrderInbox(r.Context(), tenant, outlet)
		if err != nil {
			return false
		}
		body, err := json.Marshal(values)
		if err != nil {
			return false
		}
		current := string(body)
		if current == last {
			return true
		}
		last = current
		_, _ = fmt.Fprintf(w, "event: orders\ndata: %s\n\n", body)
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}
func (s *Server) handleIngestConnectorOrder(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.ConnectorOrderInbox
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.ID) || !validOutletScoped(in.OutletID, meta.OutletID) || !domain.ValidUUID(in.ConnectorID) || !validShortText(in.ExternalOrderID, 160) || in.Payload == nil {
				return fmt.Errorf("connector order is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.TenantID = meta.TenantID
		in.OutletID = meta.OutletID
		in.ExternalOrderID = strings.TrimSpace(in.ExternalOrderID)
		in.Status = "received"
		in.ReceivedAt = now
		audit, err := newAuditEvent(meta, "connector_order.received", "connector_order_inbox", in.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err = repo.IngestConnectorOrder(ctx, in, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, in, "/api/v1/connector-order-inbox/"+in.ID)
	})
}

type connectorDecisionInput struct {
	ID                string `json:"id"`
	Decision          string `json:"decision"`
	Reason            string `json:"reason"`
	NormalizedOrderID string `json:"normalizedOrderId,omitempty"`
}

func (s *Server) handleConnectorInboxDecision(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in connectorDecisionInput
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.ID) || (in.Decision != "accepted" && in.Decision != "rejected" && in.Decision != "needs_review" && in.Decision != "duplicate") || len(in.Reason) > 500 {
				return fmt.Errorf("connector inbox decision is invalid")
			}
			if (in.Decision == "accepted" && !domain.ValidUUID(in.NormalizedOrderID)) || (in.Decision != "accepted" && in.NormalizedOrderID != "") {
				return fmt.Errorf("accepted connector orders require a canonical order id")
			}
			return nil
		}); result != nil {
			return *result
		}
		inboxID := r.PathValue("id")
		if !domain.ValidUUID(inboxID) {
			return errorResult(apiError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "connector inbox id is invalid"})
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		decision := domain.ConnectorOrderDecision{ID: in.ID, InboxID: inboxID, Decision: in.Decision, Reason: strings.TrimSpace(in.Reason), NormalizedOrderID: in.NormalizedOrderID, OccurredAt: now, ActorID: meta.ActorID, DeviceID: meta.DeviceID}
		audit, err := newAuditEvent(meta, "connector_order."+in.Decision, "connector_order_inbox", inboxID, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.DecideConnectorOrder(ctx, meta.TenantID, meta.OutletID, decision, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusOK, value, "")
	})
}

func (s *Server) handleSellability(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	channel := r.URL.Query().Get("channelId")
	if channel != "" && !domain.ValidUUID(channel) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "invalid_channel_id", Message: "channelId must be a UUID string"})
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.MenuSellability(r.Context(), tenant, outlet, channel)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}

type stationCapacityInput struct {
	StationID        string `json:"stationId"`
	MaxActiveTickets int    `json:"maxActiveTickets"`
}

func (s *Server) handleStationCapacity(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.StationCapacityLimits(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}
func (s *Server) handleSetStationCapacity(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in stationCapacityInput
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.StationID) || in.MaxActiveTickets < 0 {
				return fmt.Errorf("station capacity is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		audit, err := newAuditEvent(meta, "station_capacity.changed", "station", in.StationID, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.SetStationCapacity(ctx, meta.TenantID, meta.OutletID, domain.StationCapacityLimit{StationID: in.StationID, MaxActiveTickets: in.MaxActiveTickets}, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusOK, value, "")
	})
}

func (s *Server) handlePrintJobs(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.KitchenPrintJobs(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}
func (s *Server) handleCreatePrintJob(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.KitchenPrintJob
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.ID) || !validOutletScoped(in.OutletID, meta.OutletID) || !domain.ValidUUID(in.TicketID) || !validShortText(in.PrinterRoute, 120) || (in.CopyType != "kot" && in.CopyType != "expeditor" && in.CopyType != "packing" && in.CopyType != "receipt") {
				return fmt.Errorf("print job is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.TenantID = meta.TenantID
		in.OutletID = meta.OutletID
		in.PrinterRoute = strings.TrimSpace(in.PrinterRoute)
		in.Status = "queued"
		in.Attempts = 0
		in.CreatedAt = now
		audit, err := newAuditEvent(meta, "kitchen_print.queued", "kitchen_print_job", in.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err = repo.CreateKitchenPrintJob(ctx, in, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, in, "/api/v1/kitchen-print-jobs/"+in.ID)
	})
}

func (s *Server) handlePickupTokens(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.PickupTokens(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}
func (s *Server) handleIssuePickupToken(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.PickupToken
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.ID) || !validOutletScoped(in.OutletID, meta.OutletID) || !domain.ValidUUID(in.OrderID) || len(in.Token) < 3 || len(in.Token) > 12 {
				return fmt.Errorf("pickup token is invalid")
			}
			for _, char := range in.Token {
				if !(char >= 'A' && char <= 'Z' || char >= '0' && char <= '9') {
					return fmt.Errorf("pickup token is invalid")
				}
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.TenantID = meta.TenantID
		in.OutletID = meta.OutletID
		in.Status = "issued"
		in.IssuedAt = now
		audit, err := newAuditEvent(meta, "pickup_token.issued", "pickup_token", in.ID, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.IssuePickupToken(ctx, in, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/pickup-tokens/"+value.ID)
	})
}

func (s *Server) handleQROrderingLinks(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.QROrderingLinks(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}
func (s *Server) handleCreateQROrderingLink(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.QROrderingLink
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.ID) || !validOutletScoped(in.OutletID, meta.OutletID) || (in.ChannelID != "" && !domain.ValidUUID(in.ChannelID)) || (in.TableID != "" && !domain.ValidUUID(in.TableID)) || len(in.Slug) < 6 || len(in.Slug) > 96 {
				return fmt.Errorf("QR ordering link is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.TenantID = meta.TenantID
		in.OutletID = meta.OutletID
		in.Active = true
		in.RecordMetadata = newRecordMetadata(now)
		audit, err := newAuditEvent(meta, "qr_ordering_link.created", "qr_ordering_link", in.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err = repo.CreateQROrderingLink(ctx, in, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, in, "/api/v1/qr-ordering-links/"+in.ID)
	})
}

func (s *Server) handleStockTransfers(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.StockTransfers(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}
func (s *Server) handleCreateStockTransfer(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.StockTransfer
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.ID) || !domain.ValidUUID(in.SourceOutletID) || !domain.ValidUUID(in.DestinationOutletID) || in.SourceOutletID == in.DestinationOutletID || (in.SourceOutletID != meta.OutletID && in.DestinationOutletID != meta.OutletID) || len(in.Lines) == 0 || !validShortText(in.RequestedBy, 160) {
				return fmt.Errorf("stock transfer is invalid")
			}
			seen := map[string]bool{}
			for _, line := range in.Lines {
				if !domain.ValidUUID(line.ID) || !domain.ValidUUID(line.IngredientID) || line.QuantityBase <= 0 || seen[line.IngredientID] {
					return fmt.Errorf("stock transfer line is invalid")
				}
				seen[line.IngredientID] = true
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.TenantID = meta.TenantID
		in.Status = "requested"
		in.RequestedBy = strings.TrimSpace(in.RequestedBy)
		in.Notes = strings.TrimSpace(in.Notes)
		in.RequestedAt = now
		in.Version = 1
		audit, err := newAuditEvent(meta, "stock_transfer.requested", "stock_transfer", in.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err = repo.CreateStockTransfer(ctx, in, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, in, "/api/v1/stock-transfers/"+in.ID)
	})
}

type stockTransferTransitionInput struct {
	Action          string                              `json:"action"`
	ExpectedVersion uint64                              `json:"expectedVersion"`
	Lines           []domain.StockTransferExecutionLine `json:"lines,omitempty"`
}

func validStockTransferAction(value string) bool {
	return value == "approved" || value == "dispatched" || value == "received" || value == "cancelled"
}

func (s *Server) handleStockTransferTransition(w http.ResponseWriter, r *http.Request) {
	transferID := r.PathValue("id")
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in stockTransferTransitionInput
		if result := decodeAndValidate(payload, &in, func() error {
			in.Action = strings.TrimSpace(in.Action)
			if !domain.ValidUUID(transferID) || !validStockTransferAction(in.Action) || in.ExpectedVersion == 0 {
				return fmt.Errorf("stock transfer transition is invalid")
			}
			if (in.Action == "dispatched" || in.Action == "received") && len(in.Lines) == 0 {
				return fmt.Errorf("transfer actual quantities are required")
			}
			if in.Action != "dispatched" && in.Action != "received" && len(in.Lines) > 0 {
				return fmt.Errorf("this transfer action does not accept quantities")
			}
			seen := map[string]bool{}
			for _, line := range in.Lines {
				if !domain.ValidUUID(line.IngredientID) || line.QuantityBase <= 0 || seen[line.IngredientID] {
					return fmt.Errorf("transfer actual quantity is invalid")
				}
				seen[line.IngredientID] = true
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		audit, err := newAuditEvent(meta, "stock_transfer."+in.Action, "stock_transfer", transferID, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.TransitionStockTransfer(ctx, meta.TenantID, meta.OutletID, transferID, in.Action, in.ExpectedVersion, in.Lines, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusOK, value, "/api/v1/stock-transfers/"+value.ID)
	})
}

func (s *Server) handleReplenishmentRules(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.ReplenishmentRules(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}

func (s *Server) handleReplenishmentSuggestions(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.ReplenishmentSuggestions(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}

func (s *Server) handleSetReplenishmentRule(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.ReplenishmentRule
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.IngredientID) || !domain.ValidUUID(in.SourceOutletID) || in.SourceOutletID == meta.OutletID || in.ReorderPointBase < 0 || in.TargetLevelBase <= in.ReorderPointBase {
				return fmt.Errorf("replenishment rule is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.OutletID, in.UpdatedAt = meta.OutletID, now
		audit, err := newAuditEvent(meta, "replenishment_rule.saved", "replenishment_rule", in.IngredientID, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.SetReplenishmentRule(ctx, meta.TenantID, meta.OutletID, in, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusOK, value, "/api/v1/replenishment-rules")
	})
}

func (s *Server) handleOutletControlProfile(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	value, err := repo.OutletControlProfile(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}
func (s *Server) handleSetOutletControlProfile(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.OutletControlProfile
		if result := decodeAndValidate(payload, &in, func() error {
			if !validShortText(in.ProfileName, 120) {
				return fmt.Errorf("outlet control profile is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.OutletID = meta.OutletID
		in.ProfileName = strings.TrimSpace(in.ProfileName)
		audit, err := newAuditEvent(meta, "outlet_controls.changed", "outlet_control_profile", meta.OutletID, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.SetOutletControlProfile(ctx, meta.TenantID, meta.OutletID, in, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusOK, value, "")
	})
}

func (s *Server) handleHardwareDevices(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.HardwareDevices(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}
func (s *Server) handleRegisterHardwareDevice(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.HardwareDevice
		if result := decodeAndValidate(payload, &in, func() error {
			validType := in.DeviceType == "printer" || in.DeviceType == "cash_drawer" || in.DeviceType == "scanner" || in.DeviceType == "scale" || in.DeviceType == "customer_display" || in.DeviceType == "payment_terminal"
			validStatus := in.CertificationStatus == "candidate" || in.CertificationStatus == "certified" || in.CertificationStatus == "deprecated" || in.CertificationStatus == "blocked"
			if !domain.ValidUUID(in.ID) || !validOutletScoped(in.OutletID, meta.OutletID) || !validType || !validStatus || !validShortText(in.Manufacturer, 120) || !validShortText(in.Model, 120) || !validShortText(in.SerialNumber, 160) {
				return fmt.Errorf("hardware device is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.TenantID = meta.TenantID
		in.OutletID = meta.OutletID
		in.RecordMetadata = newRecordMetadata(now)
		audit, err := newAuditEvent(meta, "hardware_device.registered", "hardware_device", in.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err = repo.RegisterHardwareDevice(ctx, in, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, in, "/api/v1/hardware-devices/"+in.ID)
	})
}

func (s *Server) handleRunbooks(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	values, err := repo.ImplementationRunbooks(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}
func (s *Server) handleCreateRunbook(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.ImplementationRunbook
		if result := decodeAndValidate(payload, &in, func() error {
			validStatus := in.Status == "draft" || in.Status == "in_progress" || in.Status == "ready" || in.Status == "blocked"
			if !domain.ValidUUID(in.ID) || !validOutletScoped(in.OutletID, meta.OutletID) || !validShortText(in.TemplateCode, 80) || !validStatus || !validShortText(in.Owner, 160) {
				return fmt.Errorf("implementation runbook is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		repo, ok := s.connectedCommerce()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.TenantID = meta.TenantID
		in.OutletID = meta.OutletID
		in.RecordMetadata = newRecordMetadata(now)
		audit, err := newAuditEvent(meta, "implementation_runbook.created", "implementation_runbook", in.ID, now)
		if err != nil {
			return internalOperationError()
		}
		if err = repo.CreateImplementationRunbook(ctx, in, audit); err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, in, "/api/v1/implementation-runbooks/"+in.ID)
	})
}

func (s *Server) handleGSTReport(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	date := r.URL.Query().Get("businessDate")
	if !validDate(date) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "invalid_business_date", Message: "businessDate must be YYYY-MM-DD"})
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	value, err := repo.GSTReport(r.Context(), tenant, outlet, date)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}
func (s *Server) handleDayEndReport(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	date := r.URL.Query().Get("businessDate")
	if !validDate(date) {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "invalid_business_date", Message: "businessDate must be YYYY-MM-DD"})
		return
	}
	repo, ok := s.connectedCommerce()
	if !ok {
		connectedUnavailable(w, r)
		return
	}
	value, err := repo.DayEndReport(r.Context(), tenant, outlet, date)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}
