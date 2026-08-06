// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

type publicOrderingMenu struct {
	Slug         string                   `json:"slug"`
	LinkID       string                   `json:"linkId"`
	ChannelID    string                   `json:"channelId,omitempty"`
	Menu         domain.MenuStudio        `json:"menu"`
	Sellability  []domain.MenuSellability `json:"sellability"`
	PaymentState string                   `json:"paymentState"`
}

func publicScope(r *http.Request) (string, string, bool) {
	tenant, outlet := r.URL.Query().Get("tenantId"), r.URL.Query().Get("outletId")
	return tenant, outlet, domain.ValidUUID(tenant) && domain.ValidUUID(outlet)
}

func (s *Server) publicMenu(r *http.Request) (publicOrderingMenu, error) {
	tenant, outlet, ok := publicScope(r)
	if !ok {
		return publicOrderingMenu{}, fmt.Errorf("invalid public ordering scope")
	}
	connected, ok := s.repository.(store.ConnectedCommerceRepository)
	if !ok {
		return publicOrderingMenu{}, store.ErrSyncUnavailable
	}
	restaurant, ok := s.repository.(store.RestaurantCoreRepository)
	if !ok {
		return publicOrderingMenu{}, store.ErrSyncUnavailable
	}
	slug := r.PathValue("slug")
	if len(slug) < 16 || len(slug) > 96 {
		return publicOrderingMenu{}, store.ErrNotFound
	}
	links, err := connected.QROrderingLinks(r.Context(), tenant, outlet)
	if err != nil {
		return publicOrderingMenu{}, err
	}
	var link *domain.QROrderingLink
	for index := range links {
		if links[index].Slug == slug && links[index].Active && (links[index].ExpiresAt == nil || links[index].ExpiresAt.After(s.now())) {
			link = &links[index]
			break
		}
	}
	if link == nil {
		return publicOrderingMenu{}, store.ErrNotFound
	}
	studio, err := restaurant.LiveMenuStudio(r.Context(), tenant, outlet, link.ChannelID, s.now())
	if err != nil {
		return publicOrderingMenu{}, err
	}
	sellability, err := connected.MenuSellability(r.Context(), tenant, outlet, link.ChannelID)
	if err != nil {
		return publicOrderingMenu{}, err
	}
	// The sellability engine sees the outlet-wide catalog. A public QR response
	// must expose only the items in this channel's live menu, and its effective
	// channel price becomes the guest-visible price.
	byMenuItem := make(map[string]domain.MenuSellability, len(sellability))
	for _, value := range sellability {
		byMenuItem[value.MenuItemID] = value
	}
	filtered := make([]domain.MenuSellability, 0, len(studio.Current.Items))
	for index := range studio.Current.Items {
		item := &studio.Current.Items[index]
		if state, found := byMenuItem[item.MenuItemID]; found {
			item.PriceMinor = state.PriceMinor
			item.Currency = state.Currency
			filtered = append(filtered, state)
		} else {
			filtered = append(filtered, domain.MenuSellability{MenuItemID: item.MenuItemID, MenuItemName: item.DisplayName, ChannelID: link.ChannelID, PriceMinor: item.PriceMinor, Currency: item.Currency, Available: false, ManualAvailable: false, ReasonCode: "not_published", Reason: "This item is unavailable"})
		}
	}
	return publicOrderingMenu{Slug: slug, LinkID: link.ID, ChannelID: link.ChannelID, Menu: studio, Sellability: filtered, PaymentState: "pay_at_counter"}, nil
}

// handleGuestOrderRequests is the authenticated counter-facing queue. A guest
// request remains a request until staff take payment and run canonical POS
// checkout, preventing an unpaid web intent from reaching KDS by accident.
func (s *Server) handleGuestOrderRequests(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.repository.(store.DirectOrderingRepository)
	if !ok {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: http.StatusNotImplemented, Code: "direct_ordering_unavailable", Message: "direct ordering requires PostgreSQL"})
		return
	}
	values, err := repo.GuestOrderRequests(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}

func (s *Server) handlePublicOrderingMenu(w http.ResponseWriter, r *http.Request) {
	value, err := s.publicMenu(r)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}

type publicOrderLineInput struct {
	MenuItemID string `json:"menuItemId"`
	Quantity   int32  `json:"quantity"`
}
type publicOrderRequestInput struct {
	ID              string                 `json:"id"`
	ClientRequestID string                 `json:"clientRequestId"`
	MenuVersionID   string                 `json:"menuVersionId"`
	GuestName       string                 `json:"guestName,omitempty"`
	GuestPhone      string                 `json:"guestPhone,omitempty"`
	Note            string                 `json:"note,omitempty"`
	Lines           []publicOrderLineInput `json:"lines"`
}

func (s *Server) handleSubmitPublicOrder(w http.ResponseWriter, r *http.Request) {
	menu, err := s.publicMenu(r)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	var in publicOrderRequestInput
	body, bodyErr := io.ReadAll(http.MaxBytesReader(w, r.Body, s.maxBody))
	if bodyErr != nil || decodeStrict(body, &in) != nil {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "invalid_payload", Message: "request body must be valid JSON"})
		return
	}
	if !domain.ValidUUID(in.ID) || !domain.ValidUUID(in.ClientRequestID) || in.MenuVersionID != menu.Menu.CurrentVersionID || len(in.Lines) == 0 || len(in.Lines) > 80 || len(in.GuestName) > 160 || len(in.GuestPhone) > 40 || len(in.Note) > 500 {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "validation_failed", Message: "guest order request is invalid"})
		return
	}
	seen := map[string]bool{}
	lines := make([]domain.GuestOrderRequestLine, 0, len(in.Lines))
	available := map[string]bool{}
	for _, item := range menu.Sellability {
		available[item.MenuItemID] = item.Available
	}
	for _, line := range in.Lines {
		if !domain.ValidUUID(line.MenuItemID) || line.Quantity < 1 || line.Quantity > 99 || seen[line.MenuItemID] || !available[line.MenuItemID] {
			writeError(w, requestIDFrom(r.Context()), apiError{Status: 422, Code: "validation_failed", Message: "guest order item is unavailable"})
			return
		}
		seen[line.MenuItemID] = true
		lines = append(lines, domain.GuestOrderRequestLine{MenuItemID: line.MenuItemID, Quantity: line.Quantity})
	}
	tenant, outlet, _ := publicScope(r)
	repo, ok := s.repository.(store.DirectOrderingRepository)
	if !ok {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: 501, Code: "direct_ordering_unavailable", Message: "direct ordering requires PostgreSQL"})
		return
	}
	now := s.now().UTC()
	tracking := strings.ToUpper(strings.ReplaceAll(in.ID, "-", "")[:10])
	value, err := repo.SubmitGuestOrderRequest(r.Context(), domain.GuestOrderRequest{ID: in.ID, ClientRequestID: in.ClientRequestID, TenantID: tenant, OutletID: outlet, QRLinkID: menu.LinkID, ChannelID: menu.ChannelID, MenuVersionID: in.MenuVersionID, TrackingCode: tracking, GuestName: strings.TrimSpace(in.GuestName), GuestPhone: strings.TrimSpace(in.GuestPhone), Note: strings.TrimSpace(in.Note), Lines: lines, PaymentState: "pay_at_counter", Status: "submitted", SubmittedAt: now})
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusCreated, value)
}
