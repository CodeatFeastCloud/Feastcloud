package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

func TestGuestGrowthIntegration(t *testing.T) {
	url := os.Getenv("FEASTCLOUD_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set FEASTCLOUD_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := NewPostgresRepository(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	now := time.Now().UTC()
	guestID := newIntegrationUUID(t)
	guest := domain.GuestProfile{ID: guestID, TenantID: integrationTenantA, FullName: "Guest " + guestID[:6], Phone: "+91" + guestID[:8], Locale: "en-IN", DietaryLabels: []string{"vegetarian"}, MarketingConsent: true, ConsentUpdatedAt: &now, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err = r.CreateGuest(ctx, guest, integrationAudit(t, integrationTenantA, integrationOutletA, "guest", guestID, "guest.created", now)); err != nil {
		t.Fatal(err)
	}
	guests, err := r.Guests(ctx, integrationTenantA)
	if err != nil || len(guests) == 0 {
		t.Fatalf("guests: %v", err)
	}
	changed, err := r.SetGuestConsent(ctx, integrationTenantA, guestID, false, 1, "integration", integrationAudit(t, integrationTenantA, integrationOutletA, "guest", guestID, "guest.consent_changed", now.Add(time.Second)))
	if err != nil || changed.MarketingConsent || changed.Version != 2 {
		t.Fatalf("consent: %#v %v", changed, err)
	}
	reservationID := newIntegrationUUID(t)
	reservation := domain.Reservation{ID: reservationID, TenantID: integrationTenantA, OutletID: integrationOutletA, GuestID: guestID, GuestName: guest.FullName, Phone: guest.Phone, PartySize: 3, ScheduledFor: now.Add(time.Hour), DurationMinutes: 90, Status: "waiting", Source: "integration", Version: 1, CreatedAt: now, UpdatedAt: now}
	if err = r.CreateReservation(ctx, reservation, integrationAudit(t, integrationTenantA, integrationOutletA, "reservation", reservationID, "reservation.created", now)); err != nil {
		t.Fatal(err)
	}
	seated, err := r.TransitionReservation(ctx, integrationTenantA, integrationOutletA, reservationID, "seated", 1, integrationAudit(t, integrationTenantA, integrationOutletA, "reservation", reservationID, "reservation.status_changed", now.Add(2*time.Second)))
	if err != nil || seated.Status != "seated" {
		t.Fatalf("seat: %#v %v", seated, err)
	}
	if _, err = r.TransitionReservation(ctx, integrationTenantA, integrationOutletA, reservationID, "completed", 1, integrationAudit(t, integrationTenantA, integrationOutletA, "reservation", reservationID, "reservation.status_changed", now.Add(3*time.Second))); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	promotionID := newIntegrationUUID(t)
	limit := 2
	promotion := domain.Promotion{ID: promotionID, TenantID: integrationTenantA, OutletID: integrationOutletA, Code: "SAVE" + promotionID[:5], Name: "Integration offer", DiscountType: "percentage", DiscountValue: 1000, MinOrderMinor: 10000, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), RedemptionLimit: &limit, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err = r.CreatePromotion(ctx, promotion, integrationAudit(t, integrationTenantA, integrationOutletA, "promotion", promotionID, "promotion.created", now)); err != nil {
		t.Fatal(err)
	}
	redemptionID := newIntegrationUUID(t)
	redemption, err := r.RedeemPromotion(ctx, integrationTenantA, integrationOutletA, promotion.Code, domain.PromotionRedemption{ID: redemptionID, GuestID: guestID, BasketMinor: 25000}, integrationAudit(t, integrationTenantA, integrationOutletA, "promotion_redemption", redemptionID, "promotion.redeemed", now))
	if err != nil || redemption.DiscountMinor != 2500 {
		t.Fatalf("redemption: %#v %v", redemption, err)
	}
	accounts, err := r.LoyaltyAccounts(ctx, integrationTenantA)
	if err != nil {
		t.Fatal(err)
	}
	var account domain.LoyaltyAccount
	for _, x := range accounts {
		if x.GuestID == guestID {
			account = x
		}
	}
	if account.ID == "" {
		t.Fatal("loyalty account not created")
	}
	eventID := newIntegrationUUID(t)
	account, err = r.AdjustLoyalty(ctx, integrationTenantA, account.ID, "earn", account.Version, domain.LoyaltyEvent{ID: eventID, PointsDelta: 100, Reason: "integration earn"}, integrationAudit(t, integrationTenantA, integrationOutletA, "loyalty_account", account.ID, "loyalty.points_changed", now))
	if err != nil || account.PointsBalance != 100 {
		t.Fatalf("loyalty: %#v %v", account, err)
	}
	orderID, lineID, tenderID, receiptID, refundID := newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t), newIntegrationUUID(t)
	meta := domain.RecordMetadata{CreatedAt: now, UpdatedAt: now, Version: 1}
	order := domain.Order{ID: orderID, TenantID: integrationTenantA, OutletID: integrationOutletA, Type: domain.OrderTypeTakeaway, Status: domain.OrderStatusReceived, Lines: []domain.OrderLine{{ID: lineID, Name: "Refund test", Quantity: 1, UnitPrice: domain.Money{MinorUnits: 12000, Currency: "INR"}, LineTotal: domain.Money{MinorUnits: 12000, Currency: "INR"}}}, Subtotal: domain.Money{MinorUnits: 12000, Currency: "INR"}, DiscountTotal: domain.Money{Currency: "INR"}, TaxTotal: domain.Money{Currency: "INR"}, ServiceCharge: domain.Money{Currency: "INR"}, Total: domain.Money{MinorUnits: 12000, Currency: "INR"}, PlacedAt: now, RecordMetadata: meta}
	if err = r.CreateOrder(ctx, order, integrationAudit(t, integrationTenantA, integrationOutletA, "order", orderID, "order.created", now)); err != nil {
		t.Fatal(err)
	}
	tender := domain.Tender{ID: tenderID, TenantID: integrationTenantA, OutletID: integrationOutletA, OrderID: orderID, TenderType: "upi", AmountMinor: 12000, Currency: "INR", Status: "captured", OccurredAt: now}
	if _, _, err = r.CaptureTender(ctx, tender, domain.FiscalReceipt{ID: receiptID, OrderID: orderID, ReceiptNumber: "G-" + receiptID[:8], IssuedAt: now}, integrationAudit(t, integrationTenantA, integrationOutletA, "tender", tenderID, "tender.captured", now)); err != nil {
		t.Fatal(err)
	}
	refund := domain.Tender{ID: refundID, TenantID: integrationTenantA, OutletID: integrationOutletA, AmountMinor: 5000, Status: "reversed", ReversesTenderID: tenderID, OccurredAt: now.Add(time.Second)}
	refund, err = r.ReverseTender(ctx, refund, integrationAudit(t, integrationTenantA, integrationOutletA, "tender", refundID, "tender.reversed", now.Add(time.Second)))
	if err != nil || refund.OrderID != orderID || refund.TenderType != "upi" {
		t.Fatalf("refund: %#v %v", refund, err)
	}
	if _, err = r.ReverseTender(ctx, domain.Tender{ID: newIntegrationUUID(t), TenantID: integrationTenantA, OutletID: integrationOutletA, AmountMinor: 8000, Status: "reversed", ReversesTenderID: tenderID, OccurredAt: now.Add(2 * time.Second)}, integrationAudit(t, integrationTenantA, integrationOutletA, "tender", newIntegrationUUID(t), "tender.reversed", now.Add(2*time.Second))); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("expected over-refund rejection, got %v", err)
	}
}
