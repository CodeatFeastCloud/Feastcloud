// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

func TestConnectedCommercePersistsTheSharedOrderFlow(t *testing.T) {
	url := os.Getenv("FEASTCLOUD_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set FEASTCLOUD_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, err := NewPostgresRepository(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	now := time.Now().UTC()
	meta := domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}
	stationID, channelID, connectorID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	if err := repository.CreateStation(ctx, domain.Station{ID: stationID, TenantID: integrationTenantA, OutletID: integrationOutletA, Name: "Connected hot line", Code: "connected-" + stationID[:6], Type: domain.StationTypeCooking, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "station", stationID, "station.created", now)); err != nil {
		t.Fatal(err)
	}
	channel := domain.SalesChannel{ID: channelID, TenantID: integrationTenantA, OutletID: integrationOutletA, Code: "WEB" + channelID[:6], Name: "Direct web", Type: "web", Active: true, Configuration: map[string]any{"serviceCharge": false}, RecordMetadata: meta}
	if err := repository.CreateSalesChannel(ctx, channel, integrationAudit(t, integrationTenantA, integrationOutletA, "sales_channel", channelID, "sales_channel.created", now)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.MenuSellability(ctx, integrationTenantA, integrationOutletA, ""); err != nil {
		t.Fatalf("all-channel sellability: %v", err)
	}
	if _, err := repository.MenuSellability(ctx, integrationTenantA, integrationOutletA, channelID); err != nil {
		t.Fatalf("channel sellability: %v", err)
	}
	connector := domain.ConnectorInstallation{ID: connectorID, TenantID: integrationTenantA, OutletID: integrationOutletA, ChannelID: channelID, Provider: "test-provider-" + connectorID[:6], ManifestVersion: "1.0.0", Capabilities: []string{"orders.read"}, Status: "healthy", RecordMetadata: meta}
	if err := repository.CreateConnectorInstallation(ctx, connector, integrationAudit(t, integrationTenantA, integrationOutletA, "connector_installation", connectorID, "connector.installed", now)); err != nil {
		t.Fatal(err)
	}
	inboxID := newIntegrationUUID(t)
	inbox := domain.ConnectorOrderInbox{ID: inboxID, TenantID: integrationTenantA, OutletID: integrationOutletA, ConnectorID: connectorID, ExternalOrderID: "AGG-" + inboxID[:8], Payload: map[string]any{"items": 1}, Status: "received", ReceivedAt: now}
	if err := repository.IngestConnectorOrder(ctx, inbox, integrationAudit(t, integrationTenantA, integrationOutletA, "connector_order_inbox", inboxID, "connector_order.received", now)); err != nil {
		t.Fatal(err)
	}
	if inboxes, err := repository.ConnectorOrderInbox(ctx, integrationTenantA, integrationOutletA); err != nil || len(inboxes) == 0 || inboxes[0].PayloadSHA256 == "" {
		t.Fatalf("connector inbox=%#v err=%v", inboxes, err)
	}
	limit, err := repository.SetStationCapacity(ctx, integrationTenantA, integrationOutletA, domain.StationCapacityLimit{StationID: stationID, MaxActiveTickets: 4}, integrationAudit(t, integrationTenantA, integrationOutletA, "station", stationID, "station_capacity.changed", now.Add(time.Second)))
	if err != nil || limit.Version != 1 {
		t.Fatalf("capacity=%#v err=%v", limit, err)
	}
	orderID, lineID, ticketID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	order := domain.Order{ID: orderID, TenantID: integrationTenantA, OutletID: integrationOutletA, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusReceived, Lines: []domain.OrderLine{{ID: lineID, Name: "Connected bowl", Quantity: 1, UnitPrice: domain.Money{MinorUnits: 10000, Currency: "INR"}, LineTotal: domain.Money{MinorUnits: 10000, Currency: "INR"}}}, Subtotal: domain.Money{MinorUnits: 10000, Currency: "INR"}, DiscountTotal: domain.Money{Currency: "INR"}, TaxTotal: domain.Money{Currency: "INR"}, ServiceCharge: domain.Money{Currency: "INR"}, Total: domain.Money{MinorUnits: 10000, Currency: "INR"}, PlacedAt: now, RecordMetadata: meta}
	if err := repository.CreateOrder(ctx, order, integrationAudit(t, integrationTenantA, integrationOutletA, "order", orderID, "order.created", now)); err != nil {
		t.Fatal(err)
	}
	resolved, err := repository.DecideConnectorOrder(ctx, integrationTenantA, integrationOutletA, domain.ConnectorOrderDecision{ID: newIntegrationUUID(t), InboxID: inboxID, Decision: "accepted", NormalizedOrderID: orderID, OccurredAt: now.Add(time.Second), ActorID: "manager", DeviceID: "counter"}, integrationAudit(t, integrationTenantA, integrationOutletA, "connector_order_inbox", inboxID, "connector_order.accepted", now.Add(time.Second)))
	if err != nil || resolved.Status != "accepted" || resolved.NormalizedOrderID != orderID {
		t.Fatalf("connector decision=%#v err=%v", resolved, err)
	}
	if inboxes, err := repository.ConnectorOrderInbox(ctx, integrationTenantA, integrationOutletA); err != nil || len(inboxes) == 0 || inboxes[0].Status != "accepted" {
		t.Fatalf("resolved connector inbox=%#v err=%v", inboxes, err)
	}
	ticket := domain.KitchenTicket{ID: ticketID, TenantID: integrationTenantA, OutletID: integrationOutletA, OrderID: orderID, StationID: stationID, LineIDs: []string{lineID}, Status: domain.TicketStatusQueued, RecordMetadata: meta}
	if err := repository.CreateKitchenTicket(ctx, ticket, integrationAudit(t, integrationTenantA, integrationOutletA, "kitchen_ticket", ticketID, "kitchen_ticket.created", now)); err != nil {
		t.Fatal(err)
	}
	printID := newIntegrationUUID(t)
	if err := repository.CreateKitchenPrintJob(ctx, domain.KitchenPrintJob{ID: printID, TenantID: integrationTenantA, OutletID: integrationOutletA, TicketID: ticketID, PrinterRoute: "hot-printer", CopyType: "kot", Payload: map[string]any{"orderId": orderID}, Status: "queued", CreatedAt: now}, integrationAudit(t, integrationTenantA, integrationOutletA, "kitchen_print_job", printID, "kitchen_print.queued", now)); err != nil {
		t.Fatal(err)
	}
	tokenID := newIntegrationUUID(t)
	if _, err := repository.IssuePickupToken(ctx, domain.PickupToken{ID: tokenID, TenantID: integrationTenantA, OutletID: integrationOutletA, OrderID: orderID, Token: strings.ToUpper("T" + tokenID[:7]), Status: "issued", IssuedAt: now}, integrationAudit(t, integrationTenantA, integrationOutletA, "pickup_token", tokenID, "pickup_token.issued", now)); err != nil {
		t.Fatal(err)
	}
	qrID := newIntegrationUUID(t)
	if err := repository.CreateQROrderingLink(ctx, domain.QROrderingLink{ID: qrID, TenantID: integrationTenantA, OutletID: integrationOutletA, ChannelID: channelID, Slug: "direct-" + qrID[:12], Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "qr_ordering_link", qrID, "qr_ordering_link.created", now)); err != nil {
		t.Fatal(err)
	}
	if jobs, err := repository.KitchenPrintJobs(ctx, integrationTenantA, integrationOutletA); err != nil || len(jobs) == 0 {
		t.Fatalf("print jobs=%#v err=%v", jobs, err)
	}
	if tokens, err := repository.PickupTokens(ctx, integrationTenantA, integrationOutletA); err != nil || len(tokens) == 0 {
		t.Fatalf("tokens=%#v err=%v", tokens, err)
	}
	if links, err := repository.QROrderingLinks(ctx, integrationTenantA, integrationOutletA); err != nil || len(links) == 0 {
		t.Fatalf("QR links=%#v err=%v", links, err)
	}

	destinationID, unitID, ingredientID, transferID, lineTransferID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	if err := repository.CreateOutlet(ctx, domain.Outlet{ID: destinationID, TenantID: integrationTenantA, OrganizationID: integrationTenantA, Name: "Central store destination", Code: "dest-" + destinationID[:6], TimeZone: "Asia/Kolkata", Currency: "INR", Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "outlet", destinationID, "outlet.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateUnit(ctx, domain.Unit{ID: unitID, TenantID: integrationTenantA, Name: "Transfer unit", Symbol: "tu" + unitID[:6], Dimension: "count", BaseNumerator: 1, BaseDenominator: 1, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "unit", unitID, "unit.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateIngredient(ctx, domain.Ingredient{ID: ingredientID, TenantID: integrationTenantA, Name: "Transfer ingredient", Code: "transfer-" + ingredientID[:6], BaseUnitID: unitID, Allergens: []string{}, DietaryLabels: []string{}, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "ingredient", ingredientID, "ingredient.created", now)); err != nil {
		t.Fatal(err)
	}
	transfer := domain.StockTransfer{ID: transferID, TenantID: integrationTenantA, SourceOutletID: integrationOutletA, DestinationOutletID: destinationID, Status: "requested", RequestedBy: "Chef", RequestedAt: now, Version: 1, Lines: []domain.StockTransferLine{{ID: lineTransferID, IngredientID: ingredientID, QuantityBase: 4}}}
	if err := repository.CreateStockTransfer(ctx, transfer, integrationAudit(t, integrationTenantA, integrationOutletA, "stock_transfer", transferID, "stock_transfer.requested", now)); err != nil {
		t.Fatal(err)
	}
	if transfers, err := repository.StockTransfers(ctx, integrationTenantA, integrationOutletA); err != nil || len(transfers) == 0 || len(transfers[0].Lines) == 0 {
		t.Fatalf("transfers=%#v err=%v", transfers, err)
	}
	profile, err := repository.SetOutletControlProfile(ctx, integrationTenantA, integrationOutletA, domain.OutletControlProfile{OutletID: integrationOutletA, ProfileName: "Chain standard", ApprovalPolicy: map[string]any{"refund": "manager"}, FeatureProfile: map[string]any{"qr": true}}, integrationAudit(t, integrationTenantA, integrationOutletA, "outlet_control_profile", integrationOutletA, "outlet_controls.changed", now))
	if err != nil || profile.Version < 1 || profile.OutletID != integrationOutletA {
		t.Fatalf("profile=%#v err=%v", profile, err)
	}
	hardwareID := newIntegrationUUID(t)
	if err := repository.RegisterHardwareDevice(ctx, domain.HardwareDevice{ID: hardwareID, TenantID: integrationTenantA, OutletID: integrationOutletA, DeviceType: "printer", Manufacturer: "Test", Model: "KOT", SerialNumber: "S" + hardwareID[:8], CertificationStatus: "certified", RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "hardware_device", hardwareID, "hardware_device.registered", now)); err != nil {
		t.Fatal(err)
	}
	runbookID := newIntegrationUUID(t)
	if err := repository.CreateImplementationRunbook(ctx, domain.ImplementationRunbook{ID: runbookID, TenantID: integrationTenantA, OutletID: integrationOutletA, TemplateCode: "qsr-india", Status: "in_progress", Checklist: []map[string]any{{"id": "menu", "done": true}}, Owner: "Implementation", RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "implementation_runbook", runbookID, "implementation_runbook.created", now)); err != nil {
		t.Fatal(err)
	}
	if devices, err := repository.HardwareDevices(ctx, integrationTenantA, integrationOutletA); err != nil || len(devices) == 0 {
		t.Fatalf("hardware=%#v err=%v", devices, err)
	}
	if runbooks, err := repository.ImplementationRunbooks(ctx, integrationTenantA, integrationOutletA); err != nil || len(runbooks) == 0 {
		t.Fatalf("runbooks=%#v err=%v", runbooks, err)
	}
}
