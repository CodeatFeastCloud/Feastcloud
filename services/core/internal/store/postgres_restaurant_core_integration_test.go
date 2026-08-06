// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

func TestRestaurantCoreIntegration(t *testing.T) {
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
	unitID, ingredientID, recipeID, recipeVersionID, componentID, stationID, menuItemID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	if err := repository.CreateUnit(ctx, domain.Unit{ID: unitID, TenantID: integrationTenantA, Name: "POS portion", Symbol: "pos-" + unitID[:6], Dimension: "count", BaseNumerator: 1, BaseDenominator: 1, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "unit", unitID, "unit.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateIngredient(ctx, domain.Ingredient{ID: ingredientID, TenantID: integrationTenantA, Name: "POS ingredient", Code: "pos-ing-" + ingredientID[:6], BaseUnitID: unitID, Allergens: []string{}, DietaryLabels: []string{}, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "ingredient", ingredientID, "ingredient.created", now)); err != nil {
		t.Fatal(err)
	}
	recipe := domain.Recipe{ID: recipeID, TenantID: integrationTenantA, Name: "POS recipe", Code: "pos-rec-" + recipeID[:6], Active: true, RecordMetadata: meta}
	version := domain.RecipeVersion{ID: recipeVersionID, TenantID: integrationTenantA, RecipeID: recipeID, VersionNumber: 1, YieldQuantity: 1, YieldUnitID: unitID, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, Components: []domain.RecipeComponent{{ID: componentID, IngredientID: ingredientID, Quantity: 1, UnitID: unitID}}}
	if err := repository.CreateRecipe(ctx, recipe, version, integrationAudit(t, integrationTenantA, integrationOutletA, "recipe", recipeID, "recipe.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateStation(ctx, domain.Station{ID: stationID, TenantID: integrationTenantA, OutletID: integrationOutletA, Name: "POS hot line", Code: "pos-hot-" + stationID[:6], Type: domain.StationTypeCooking, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "station", stationID, "station.created", now)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateMenuItem(ctx, domain.MenuItem{ID: menuItemID, TenantID: integrationTenantA, OutletID: integrationOutletA, RecipeID: recipeID, Name: "POS bowl", Code: "pos-bowl-" + menuItemID[:6], PriceMinor: 10000, Currency: "INR", StationID: stationID, Active: true, RecordMetadata: meta}, integrationAudit(t, integrationTenantA, integrationOutletA, "menu_item", menuItemID, "menu_item.created", now)); err != nil {
		t.Fatal(err)
	}

	studioID, menuVersionID, categoryID, groupID, optionID, priceID, publicationID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	menuVersion := domain.MenuStudioVersion{ID: menuVersionID, MenuStudioID: studioID, VersionNumber: 1, Status: "published", EffectiveFrom: now, CreatedAt: now, PublishedAt: &now, PublishedBy: "integration", Categories: []domain.MenuStudioCategory{{ID: categoryID, Name: "Bowls", Active: true}}, Modifiers: []domain.MenuModifierGroup{{ID: groupID, Name: "Spice", SelectionMin: 0, SelectionMax: 1, Options: []domain.MenuModifierOption{{ID: optionID, Name: "Extra heat", PriceDeltaMinor: 2500, Active: true}}}}, Items: []domain.MenuStudioItem{{MenuItemID: menuItemID, CategoryID: categoryID, DisplayName: "POS Bowl", Active: true, ModifierGroupIDs: []string{groupID}, PriceID: priceID, PriceMinor: 10000, Currency: "INR"}}, Publications: []domain.MenuPublication{{ID: publicationID, Status: "scheduled", EffectiveFrom: now}}}
	studio := domain.MenuStudio{ID: studioID, TenantID: integrationTenantA, OutletID: integrationOutletA, Name: "Counter menu " + studioID[:6], Status: "published", CurrentVersionID: menuVersionID, Version: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := repository.CreateMenuStudio(ctx, studio, menuVersion, integrationAudit(t, integrationTenantA, integrationOutletA, "menu_studio", studioID, "menu_studio.created", now)); err != nil {
		t.Fatal(err)
	}
	studios, err := repository.MenuStudios(ctx, integrationTenantA, integrationOutletA)
	if err != nil {
		t.Fatal(err)
	}
	var loaded *domain.MenuStudio
	for index := range studios {
		if studios[index].ID == studioID {
			loaded = &studios[index]
		}
	}
	if loaded == nil || loaded.Current == nil || len(loaded.Current.Items) != 1 || len(loaded.Current.Modifiers) != 1 {
		t.Fatalf("menu studio did not round trip: %#v", loaded)
	}

	checkoutID, orderID, lineID, tenderID, receiptID, tokenID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	checkout := domain.POSCheckout{ID: checkoutID, TenantID: integrationTenantA, OutletID: integrationOutletA, MenuVersionID: menuVersionID, OrderID: orderID, OrderType: domain.OrderTypeTakeaway, Lines: []domain.POSCheckoutLine{{ID: lineID, MenuItemID: menuItemID, Quantity: 2, ModifierOptionIDs: []string{optionID}}}, Tenders: []domain.POSCheckoutTender{{ID: tenderID, TenderType: "upi", AmountMinor: 25000, ProviderReference: "upi-integration"}}, ReceiptID: receiptID, ReceiptNumber: "POS-" + receiptID[:8], PickupTokenID: tokenID, PickupToken: strings.ToUpper("T" + tokenID[:7]), PrinterRoute: "hot-line", PlacedAt: now}
	result, err := repository.CheckoutPOS(ctx, checkout, integrationAudit(t, integrationTenantA, integrationOutletA, "pos_checkout", checkoutID, "pos.checkout_completed", now))
	if err != nil {
		t.Fatal(err)
	}
	if result.Order.Total.MinorUnits != 25000 || len(result.Tickets) != 1 || len(result.PrintJobs) != 1 || result.PickupToken == nil || len(result.Tenders) != 1 || result.Receipt == nil {
		t.Fatalf("incomplete atomic checkout: %#v", result)
	}
	if _, err := repository.CheckoutPOS(ctx, checkout, integrationAudit(t, integrationTenantA, integrationOutletA, "pos_checkout", checkoutID, "pos.checkout_completed", now)); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate checkout error=%v want conflict", err)
	}
	ack, err := repository.AcknowledgeKitchenPrintJob(ctx, integrationTenantA, integrationOutletA, result.PrintJobs[0].ID, "acknowledged", integrationAudit(t, integrationTenantA, integrationOutletA, "kitchen_print_job", result.PrintJobs[0].ID, "kitchen_print.acknowledged", now.Add(time.Second)))
	if err != nil || ack.Status != "acknowledged" {
		t.Fatalf("print acknowledgement: %#v / %v", ack, err)
	}
	called, err := repository.TransitionPickupToken(ctx, integrationTenantA, integrationOutletA, tokenID, "called", 1, integrationAudit(t, integrationTenantA, integrationOutletA, "pickup_token", tokenID, "pickup_token.called", now.Add(time.Second)))
	if err != nil || called.Version != 2 {
		t.Fatalf("token called: %#v / %v", called, err)
	}
	collected, err := repository.TransitionPickupToken(ctx, integrationTenantA, integrationOutletA, tokenID, "collected", 2, integrationAudit(t, integrationTenantA, integrationOutletA, "pickup_token", tokenID, "pickup_token.collected", now.Add(2*time.Second)))
	if err != nil || collected.Status != "collected" || collected.Version != 3 {
		t.Fatalf("token collected: %#v / %v", collected, err)
	}
}
