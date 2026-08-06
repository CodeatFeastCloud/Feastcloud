package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
	"net/http"
	"strings"
	"time"
)

func (s *Server) guestGrowth() (store.GuestGrowthRepository, bool) {
	v, ok := s.repository.(store.GuestGrowthRepository)
	return v, ok
}

type guestInput struct {
	ID               string   `json:"id"`
	FullName         string   `json:"fullName"`
	Phone            string   `json:"phone"`
	Email            string   `json:"email"`
	Locale           string   `json:"locale"`
	DietaryLabels    []string `json:"dietaryLabels"`
	Notes            string   `json:"notes"`
	MarketingConsent bool     `json:"marketingConsent"`
}

func (s *Server) handleCreateGuest(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in guestInput
		if x := decodeAndValidate(p, &in, func() error {
			if !domain.ValidUUID(in.ID) || strings.TrimSpace(in.FullName) == "" || len(in.FullName) > 160 || len(in.Phone) > 32 || len(in.Email) > 254 {
				return fmt.Errorf("guest is invalid")
			}
			return nil
		}); x != nil {
			return *x
		}
		now := s.now().UTC()
		var consent *time.Time
		if in.MarketingConsent {
			consent = &now
		}
		v := domain.GuestProfile{ID: in.ID, TenantID: m.TenantID, FullName: strings.TrimSpace(in.FullName), Phone: strings.TrimSpace(in.Phone), Email: strings.ToLower(strings.TrimSpace(in.Email)), Locale: strings.TrimSpace(in.Locale), DietaryLabels: in.DietaryLabels, Notes: strings.TrimSpace(in.Notes), MarketingConsent: in.MarketingConsent, ConsentUpdatedAt: consent, Version: 1, CreatedAt: now, UpdatedAt: now}
		if v.Locale == "" {
			v.Locale = "en-IN"
		}
		a, e := newAuditEvent(m, "guest.created", "guest", v.ID, now)
		if e != nil {
			return internalOperationError()
		}
		repo, ok := s.guestGrowth()
		if !ok {
			return internalOperationError()
		}
		if e = repo.CreateGuest(ctx, v, a); e != nil {
			return repositoryError(e)
		}
		return successResult(201, v, "/api/v1/guests/"+v.ID)
	})
}
func (s *Server) handleGuests(w http.ResponseWriter, r *http.Request) {
	t, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repo, ok := s.guestGrowth()
	if !ok {
		return
	}
	v, e := repo.Guests(r.Context(), t)
	if e != nil {
		writeReadRepositoryError(w, r, e)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, v)
}

type consentInput struct {
	Granted         bool   `json:"granted"`
	ExpectedVersion uint64 `json:"expectedVersion"`
	Source          string `json:"source"`
}

func (s *Server) handleGuestConsent(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in consentInput
		if x := decodeAndValidate(p, &in, func() error {
			if in.ExpectedVersion < 1 {
				return fmt.Errorf("expectedVersion is invalid")
			}
			return nil
		}); x != nil {
			return *x
		}
		id := r.PathValue("id")
		a, e := newAuditEvent(m, "guest.consent_changed", "guest", id, s.now().UTC())
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.guestGrowth()
		v, e := repo.SetGuestConsent(ctx, m.TenantID, id, in.Granted, in.ExpectedVersion, strings.TrimSpace(in.Source), a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(200, v, "")
	})
}

type reservationInput struct {
	ID              string `json:"id"`
	OutletID        string `json:"outletId"`
	GuestID         string `json:"guestId"`
	GuestName       string `json:"guestName"`
	Phone           string `json:"phone"`
	PartySize       int    `json:"partySize"`
	ScheduledFor    string `json:"scheduledFor"`
	DurationMinutes int    `json:"durationMinutes"`
	Status          string `json:"status"`
	Source          string `json:"source"`
	Notes           string `json:"notes"`
}

func (s *Server) handleCreateReservation(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in reservationInput
		var scheduled time.Time
		if x := decodeAndValidate(p, &in, func() error {
			var e error
			scheduled, e = time.Parse(time.RFC3339, in.ScheduledFor)
			if e != nil || !domain.ValidUUID(in.ID) || in.OutletID != m.OutletID || strings.TrimSpace(in.GuestName) == "" || in.PartySize < 1 || in.PartySize > 100 || (in.GuestID != "" && !domain.ValidUUID(in.GuestID)) || (in.Status != "" && in.Status != "booked" && in.Status != "waiting") {
				return fmt.Errorf("reservation is invalid")
			}
			return nil
		}); x != nil {
			return *x
		}
		if in.DurationMinutes == 0 {
			in.DurationMinutes = 90
		}
		if in.Status == "" {
			in.Status = "booked"
		}
		now := s.now().UTC()
		v := domain.Reservation{ID: in.ID, TenantID: m.TenantID, OutletID: m.OutletID, GuestID: in.GuestID, GuestName: strings.TrimSpace(in.GuestName), Phone: strings.TrimSpace(in.Phone), PartySize: in.PartySize, ScheduledFor: scheduled, DurationMinutes: in.DurationMinutes, Status: in.Status, Source: strings.TrimSpace(in.Source), Notes: strings.TrimSpace(in.Notes), Version: 1, CreatedAt: now, UpdatedAt: now}
		if v.Source == "" {
			v.Source = "staff"
		}
		a, e := newAuditEvent(m, "reservation.created", "reservation", v.ID, now)
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.guestGrowth()
		if e = repo.CreateReservation(ctx, v, a); e != nil {
			return repositoryError(e)
		}
		return successResult(201, v, "/api/v1/reservations/"+v.ID)
	})
}
func (s *Server) handleReservations(w http.ResponseWriter, r *http.Request) {
	t, o, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, _ := s.guestGrowth()
	v, e := repo.Reservations(r.Context(), t, o)
	if e != nil {
		writeReadRepositoryError(w, r, e)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, v)
}

type statusVersionInput struct {
	Status          string `json:"status"`
	ExpectedVersion uint64 `json:"expectedVersion"`
}

func (s *Server) handleReservationTransition(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in statusVersionInput
		if x := decodeAndValidate(p, &in, func() error {
			if in.ExpectedVersion < 1 {
				return fmt.Errorf("expectedVersion is invalid")
			}
			return nil
		}); x != nil {
			return *x
		}
		id := r.PathValue("id")
		a, e := newAuditEvent(m, "reservation.status_changed", "reservation", id, s.now().UTC())
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.guestGrowth()
		v, e := repo.TransitionReservation(ctx, m.TenantID, m.OutletID, id, in.Status, in.ExpectedVersion, a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(200, v, "")
	})
}

type promotionInput struct {
	ID              string `json:"id"`
	OutletID        string `json:"outletId"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	DiscountType    string `json:"discountType"`
	DiscountValue   int64  `json:"discountValue"`
	MinOrderMinor   int64  `json:"minOrderMinor"`
	StartsAt        string `json:"startsAt"`
	EndsAt          string `json:"endsAt"`
	RedemptionLimit *int   `json:"redemptionLimit"`
}

func (s *Server) handleCreatePromotion(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in promotionInput
		var starts, ends time.Time
		if x := decodeAndValidate(p, &in, func() error {
			var e error
			starts, e = time.Parse(time.RFC3339, in.StartsAt)
			if e != nil {
				return fmt.Errorf("startsAt is invalid")
			}
			ends, e = time.Parse(time.RFC3339, in.EndsAt)
			if e != nil || !ends.After(starts) || !domain.ValidUUID(in.ID) || in.OutletID != m.OutletID || strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" || in.DiscountValue < 1 || (in.DiscountType != "percentage" && in.DiscountType != "fixed") || (in.DiscountType == "percentage" && in.DiscountValue > 10000) {
				return fmt.Errorf("promotion is invalid")
			}
			return nil
		}); x != nil {
			return *x
		}
		now := s.now().UTC()
		v := domain.Promotion{ID: in.ID, TenantID: m.TenantID, OutletID: m.OutletID, Code: strings.ToUpper(strings.TrimSpace(in.Code)), Name: strings.TrimSpace(in.Name), DiscountType: in.DiscountType, DiscountValue: in.DiscountValue, MinOrderMinor: in.MinOrderMinor, StartsAt: starts, EndsAt: ends, RedemptionLimit: in.RedemptionLimit, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
		a, e := newAuditEvent(m, "promotion.created", "promotion", v.ID, now)
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.guestGrowth()
		if e = repo.CreatePromotion(ctx, v, a); e != nil {
			return repositoryError(e)
		}
		return successResult(201, v, "/api/v1/promotions/"+v.ID)
	})
}
func (s *Server) handlePromotions(w http.ResponseWriter, r *http.Request) {
	t, o, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, _ := s.guestGrowth()
	v, e := repo.Promotions(r.Context(), t, o)
	if e != nil {
		writeReadRepositoryError(w, r, e)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, v)
}

type redeemInput struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	GuestID     string `json:"guestId"`
	OrderID     string `json:"orderId"`
	BasketMinor int64  `json:"basketMinor"`
}

func (s *Server) handleRedeemPromotion(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in redeemInput
		if x := decodeAndValidate(p, &in, func() error {
			if !domain.ValidUUID(in.ID) || strings.TrimSpace(in.Code) == "" || in.BasketMinor < 1 || (in.GuestID != "" && !domain.ValidUUID(in.GuestID)) || (in.OrderID != "" && !domain.ValidUUID(in.OrderID)) {
				return fmt.Errorf("redemption is invalid")
			}
			return nil
		}); x != nil {
			return *x
		}
		a, e := newAuditEvent(m, "promotion.redeemed", "promotion_redemption", in.ID, s.now().UTC())
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.guestGrowth()
		v, e := repo.RedeemPromotion(ctx, m.TenantID, m.OutletID, in.Code, domain.PromotionRedemption{ID: in.ID, GuestID: in.GuestID, OrderID: in.OrderID, BasketMinor: in.BasketMinor}, a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(201, v, "")
	})
}

func (s *Server) handleLoyaltyAccounts(w http.ResponseWriter, r *http.Request) {
	t, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	repo, _ := s.guestGrowth()
	v, e := repo.LoyaltyAccounts(r.Context(), t)
	if e != nil {
		writeReadRepositoryError(w, r, e)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, v)
}

type loyaltyInput struct {
	ID              string `json:"id"`
	EventType       string `json:"eventType"`
	PointsDelta     int64  `json:"pointsDelta"`
	Reason          string `json:"reason"`
	OrderID         string `json:"orderId"`
	ExpectedVersion uint64 `json:"expectedVersion"`
}

func (s *Server) handleLoyaltyEvent(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in loyaltyInput
		if x := decodeAndValidate(p, &in, func() error {
			if !domain.ValidUUID(in.ID) || in.ExpectedVersion < 1 || in.PointsDelta == 0 || strings.TrimSpace(in.Reason) == "" || (in.EventType != "earn" && in.EventType != "redeem" && in.EventType != "adjustment" && in.EventType != "expiry") || (in.EventType == "earn" && in.PointsDelta < 0) || (in.EventType == "redeem" && in.PointsDelta > 0) {
				return fmt.Errorf("loyalty event is invalid")
			}
			return nil
		}); x != nil {
			return *x
		}
		id := r.PathValue("id")
		a, e := newAuditEvent(m, "loyalty.points_changed", "loyalty_account", id, s.now().UTC())
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.guestGrowth()
		v, e := repo.AdjustLoyalty(ctx, m.TenantID, id, in.EventType, in.ExpectedVersion, domain.LoyaltyEvent{ID: in.ID, PointsDelta: in.PointsDelta, Reason: strings.TrimSpace(in.Reason), OrderID: in.OrderID}, a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(201, v, "")
	})
}

func (s *Server) handleTenders(w http.ResponseWriter, r *http.Request) {
	t, o, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, _ := s.commerce()
	v, e := repo.Tenders(r.Context(), t, o)
	if e != nil {
		writeReadRepositoryError(w, r, e)
		return
	}
	writeData(w, requestIDFrom(r.Context()), 200, v)
}

type reverseTenderInput struct {
	ID                string `json:"id"`
	AmountMinor       int64  `json:"amountMinor"`
	CashShiftID       string `json:"cashShiftId"`
	ProviderReference string `json:"providerReference"`
}

func (s *Server) handleReverseTender(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, m domain.MutationMetadata, p json.RawMessage) idempotency.Result {
		var in reverseTenderInput
		if x := decodeAndValidate(p, &in, func() error {
			if !domain.ValidUUID(in.ID) || in.AmountMinor < 1 {
				return fmt.Errorf("refund is invalid")
			}
			return nil
		}); x != nil {
			return *x
		}
		original := r.PathValue("id")
		now := s.now().UTC()
		v := domain.Tender{ID: in.ID, TenantID: m.TenantID, OutletID: m.OutletID, CashShiftID: in.CashShiftID, AmountMinor: in.AmountMinor, ProviderReference: strings.TrimSpace(in.ProviderReference), Status: "reversed", ReversesTenderID: original, OccurredAt: now}
		a, e := newAuditEvent(m, "tender.reversed", "tender", v.ID, now)
		if e != nil {
			return internalOperationError()
		}
		repo, _ := s.commerce()
		v, e = repo.ReverseTender(ctx, v, a)
		if e != nil {
			return repositoryError(e)
		}
		return successResult(201, v, "")
	})
}
