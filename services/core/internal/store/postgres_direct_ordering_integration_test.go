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

func TestDirectOrderingIntegration(t *testing.T) {
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

	now := time.Now().UTC().Truncate(time.Second)
	meta := domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}
	unitID, ingredientID, recipeID, recipeVersionID, componentID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	stationID, menuItemID, studioID, menuVersionID, categoryID, priceID, publicationID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	channelID, qrID, requestID, clientRequestID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	if err := repository.CreateUnit(ctx, domain.Unit{ID: unitID, TenantID: integrationTenantA, Name: "Direct portion", Symbol: "dir-" + unitID[:6], Dimension: "count", BaseNumerator: 1, BaseDenominator: 1, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "unit", unitID, "unit.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateIngredient(ctx, domain.Ingredient{ID: ingredientID, TenantID: integrationTenantA, Name: "Direct ingredient", Code: "direct-" + ingredientID[:6], BaseUnitID: unitID, Allergens: []string{}, DietaryLabels: []string{}, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "ingredient", ingredientID, "ingredient.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateRecipe(ctx, domain.Recipe{ID: recipeID, TenantID: integrationTenantA, Name: "Direct recipe", Code: "direct-" + recipeID[:6], Active: true, RecordMetadata: meta}, domain.RecipeVersion{ID: recipeVersionID, TenantID: integrationTenantA, RecipeID: recipeID, VersionNumber: 1, YieldQuantity: 1, YieldUnitID: unitID, EffectiveFrom: now.Add(-time.Minute), CreatedAt: now, Components: []domain.RecipeComponent{{ID: componentID, IngredientID: ingredientID, Quantity: 1, UnitID: unitID}}}, integrationAudit(t, integrationTenantA, integrationOutletA, "recipe", recipeID, "recipe.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateStation(ctx, domain.Station{ID: stationID, TenantID: integrationTenantA, OutletID: integrationOutletA, Name: "Direct hot line", Code: "direct-" + stationID[:6], Type: domain.StationTypeCooking, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "station", stationID, "station.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateMenuItem(ctx, domain.MenuItem{ID: menuItemID, TenantID: integrationTenantA, OutletID: integrationOutletA, RecipeID: recipeID, Name: "Direct bowl", Code: "direct-" + menuItemID[:6], PriceMinor: 14900, Currency: "INR", StationID: stationID, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "menu_item", menuItemID, "menu_item.created", now)); err != nil {
		t.Fatal(err)
	}
	receiptID := newIntegrationUUID(t)
	receiptAudit := integrationAudit(t, integrationTenantA, integrationOutletA, "inventory_event", receiptID, "inventory.receipt", now)
	if _, err := repository.RecordInventoryEvent(ctx, StockMovement{Event: domain.InventoryEvent{ID: receiptID, TenantID: integrationTenantA, OutletID: integrationOutletA, IngredientID: ingredientID, EventType: "receipt", TotalCostMinor: 80000, Currency: "INR", ReferenceType: "goods_receipt", ReferenceID: newIntegrationUUID(t), ActorID: receiptAudit.ActorID, DeviceID: receiptAudit.DeviceID, OperationID: receiptAudit.OperationID, OccurredAt: now, RecordedAt: now}, Quantity: 8, UnitID: unitID}, receiptAudit); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateSalesChannel(ctx, domain.SalesChannel{ID: channelID, TenantID: integrationTenantA, OutletID: integrationOutletA, Code: "QR" + channelID[:6], Name: "Table QR", Type: "qr", Active: true, Configuration: map[string]any{}, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "sales_channel", channelID, "sales_channel.created", now)); err != nil {
		t.Fatal(err)
	}
	version := domain.MenuStudioVersion{ID: menuVersionID, MenuStudioID: studioID, VersionNumber: 1, Status: "published", EffectiveFrom: now, CreatedAt: now, PublishedAt: &now, PublishedBy: "integration", Categories: []domain.MenuStudioCategory{{ID: categoryID, Name: "Direct", Active: true}}, Items: []domain.MenuStudioItem{{MenuItemID: menuItemID, CategoryID: categoryID, DisplayName: "Direct bowl", Active: true, PriceID: priceID, PriceMinor: 14900, Currency: "INR"}}, Publications: []domain.MenuPublication{{ID: publicationID, ChannelID: channelID, Status: "live", EffectiveFrom: now}}}
	studio := domain.MenuStudio{ID: studioID, TenantID: integrationTenantA, OutletID: integrationOutletA, Name: "Direct menu " + studioID[:6], Status: "published", CurrentVersionID: menuVersionID, Version: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := repository.CreateMenuStudio(ctx, studio, version, integrationAudit(t, integrationTenantA, integrationOutletA, "menu_studio", studioID, "menu_studio.created", now)); err != nil {
		t.Fatal(err)
	}
	live, err := repository.LiveMenuStudio(ctx, integrationTenantA, integrationOutletA, channelID, now.Add(time.Second))
	if err != nil || live.Current == nil || live.Current.ID != menuVersionID {
		t.Fatalf("live QR menu=%#v err=%v", live, err)
	}
	availability, err := repository.MenuSellability(ctx, integrationTenantA, integrationOutletA, channelID)
	if err != nil {
		t.Fatal(err)
	}
	foundSellability := false
	for _, value := range availability {
		if value.MenuItemID == menuItemID {
			foundSellability = true
		}
	}
	if !foundSellability {
		t.Fatal("channel without explicit overrides should inherit its menu items")
	}
	if err := repository.CreateQROrderingLink(ctx, domain.QROrderingLink{ID: qrID, TenantID: integrationTenantA, OutletID: integrationOutletA, ChannelID: channelID, Slug: "feast-direct-" + qrID[:12], Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "qr_ordering_link", qrID, "qr_ordering_link.created", now)); err != nil {
		t.Fatal(err)
	}
	request := domain.GuestOrderRequest{ID: requestID, TenantID: integrationTenantA, OutletID: integrationOutletA, QRLinkID: qrID, ChannelID: channelID, MenuVersionID: menuVersionID, TrackingCode: "DIRECT" + strings.ToUpper(requestID[:8]), GuestName: "Kitchen guest", Lines: []domain.GuestOrderRequestLine{{MenuItemID: menuItemID, Quantity: 2}}, PaymentState: "pay_at_counter", Status: "submitted", ClientRequestID: clientRequestID, SubmittedAt: now}
	created, err := repository.SubmitGuestOrderRequest(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if created.TotalMinor != 29800 || created.Currency != "INR" || len(created.Lines) != 1 || created.Lines[0].Name != "Direct bowl" {
		t.Fatalf("quoted guest request=%#v", created)
	}
	retry := request
	retry.ID = newIntegrationUUID(t)
	retry.TrackingCode = "RETRY" + strings.ToUpper(retry.ID[:9])
	replayed, err := repository.SubmitGuestOrderRequest(ctx, retry)
	if err != nil || replayed.ID != requestID || replayed.TotalMinor != created.TotalMinor {
		t.Fatalf("idempotency replay=%#v err=%v", replayed, err)
	}
	requests, err := repository.GuestOrderRequests(ctx, integrationTenantA, integrationOutletA)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, value := range requests {
		if value.ID == requestID {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("wanted exactly one immutable guest request, found %d", found)
	}
}
