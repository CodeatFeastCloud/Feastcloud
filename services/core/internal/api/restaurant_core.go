// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

func (s *Server) restaurantCore() (store.RestaurantCoreRepository, bool) {
	v, ok := s.repository.(store.RestaurantCoreRepository)
	return v, ok
}

func restaurantCoreUnavailable(w http.ResponseWriter, r *http.Request) {
	writeError(w, requestIDFrom(r.Context()), apiError{Status: http.StatusNotImplemented, Code: "restaurant_core_unavailable", Message: "Menu Studio and POS checkout require PostgreSQL"})
}

type menuStudioInput struct {
	ID       string                   `json:"id"`
	OutletID string                   `json:"outletId"`
	Name     string                   `json:"name"`
	Version  domain.MenuStudioVersion `json:"version"`
}

func validMenuVersion(value domain.MenuStudioVersion, initial bool) error {
	if !domain.ValidUUID(value.ID) || value.VersionNumber < 1 || value.EffectiveFrom.IsZero() {
		return fmt.Errorf("menu version id, version number and effectiveFrom are required")
	}
	if initial && value.VersionNumber != 1 {
		return fmt.Errorf("the first menu version must be number 1")
	}
	if value.Status != "draft" && value.Status != "published" {
		return fmt.Errorf("menu version status must be draft or published")
	}
	if len(value.Categories) > 80 || len(value.Modifiers) > 80 || len(value.Items) == 0 || len(value.Items) > 500 {
		return fmt.Errorf("menu version has too many or too few catalog records")
	}
	categories, groups, options, items, prices, publications := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, category := range value.Categories {
		if !domain.ValidUUID(category.ID) || categories[category.ID] || !validShortText(category.Name, 100) {
			return fmt.Errorf("menu category is invalid")
		}
		categories[category.ID] = true
	}
	for _, group := range value.Modifiers {
		if !domain.ValidUUID(group.ID) || groups[group.ID] || !validShortText(group.Name, 100) || group.SelectionMin < 0 || group.SelectionMax < group.SelectionMin || group.SelectionMax > 20 || (group.Required && group.SelectionMin < 1) {
			return fmt.Errorf("modifier group is invalid")
		}
		groups[group.ID] = true
		for _, option := range group.Options {
			if !domain.ValidUUID(option.ID) || options[option.ID] || !validShortText(option.Name, 100) {
				return fmt.Errorf("modifier option is invalid")
			}
			options[option.ID] = true
		}
	}
	for _, item := range value.Items {
		if !domain.ValidUUID(item.MenuItemID) || items[item.MenuItemID] || !domain.ValidUUID(item.PriceID) || prices[item.PriceID] || !validShortText(item.DisplayName, 160) || len(item.Description) > 500 || item.PriceMinor < 0 || !domain.ValidCurrency(item.Currency) {
			return fmt.Errorf("menu item is invalid")
		}
		if item.CategoryID != "" && !categories[item.CategoryID] {
			return fmt.Errorf("menu item category is invalid")
		}
		seenGroups := map[string]bool{}
		for _, groupID := range item.ModifierGroupIDs {
			if !groups[groupID] || seenGroups[groupID] {
				return fmt.Errorf("menu item modifier group is invalid")
			}
			seenGroups[groupID] = true
		}
		items[item.MenuItemID], prices[item.PriceID] = true, true
	}
	for _, publication := range value.Publications {
		if !domain.ValidUUID(publication.ID) || publications[publication.ID] || (publication.ChannelID != "" && !domain.ValidUUID(publication.ChannelID)) || (publication.Status != "scheduled" && publication.Status != "live" && publication.Status != "paused") || publication.EffectiveFrom.IsZero() || (publication.EffectiveTo != nil && !publication.EffectiveTo.After(publication.EffectiveFrom)) {
			return fmt.Errorf("menu publication is invalid")
		}
		publications[publication.ID] = true
	}
	return nil
}

func (s *Server) handleMenuStudios(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repo, ok := s.restaurantCore()
	if !ok {
		restaurantCoreUnavailable(w, r)
		return
	}
	values, err := repo.MenuStudios(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, values)
}

func (s *Server) handleCreateMenuStudio(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in menuStudioInput
		if result := decodeAndValidate(payload, &in, func() error {
			if !domain.ValidUUID(in.ID) || !validOutletScoped(in.OutletID, meta.OutletID) || !validShortText(in.Name, 160) {
				return fmt.Errorf("menu studio is invalid")
			}
			return validMenuVersion(in.Version, true)
		}); result != nil {
			return *result
		}
		repo, ok := s.restaurantCore()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.Version.MenuStudioID, in.Version.CreatedAt = in.ID, now
		if in.Version.Status == "published" {
			in.Version.PublishedAt = &now
			in.Version.PublishedBy = meta.ActorID
		}
		studio := domain.MenuStudio{ID: in.ID, TenantID: meta.TenantID, OutletID: meta.OutletID, Name: strings.TrimSpace(in.Name), Status: "draft", Version: 1, CreatedAt: now, UpdatedAt: now}
		if in.Version.Status == "published" {
			studio.Status, studio.CurrentVersionID = "published", in.Version.ID
		}
		audit, err := newAuditEvent(meta, "menu_studio.created", "menu_studio", studio.ID, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.CreateMenuStudio(ctx, studio, in.Version, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/menu-studios/"+value.ID)
	})
}

type menuVersionInput struct {
	ExpectedVersion uint64                   `json:"expectedVersion"`
	Version         domain.MenuStudioVersion `json:"version"`
}

func (s *Server) handleAddMenuStudioVersion(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in menuVersionInput
		if result := decodeAndValidate(payload, &in, func() error {
			if in.ExpectedVersion < 1 {
				return fmt.Errorf("expectedVersion is required")
			}
			return validMenuVersion(in.Version, false)
		}); result != nil {
			return *result
		}
		id := r.PathValue("id")
		if !domain.ValidUUID(id) {
			return errorResult(apiError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "menu studio id is invalid"})
		}
		repo, ok := s.restaurantCore()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.Version.MenuStudioID, in.Version.CreatedAt = id, now
		if in.Version.Status == "published" {
			in.Version.PublishedAt = &now
			in.Version.PublishedBy = meta.ActorID
		}
		audit, err := newAuditEvent(meta, "menu_studio.versioned", "menu_studio", id, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.AddMenuStudioVersion(ctx, meta.TenantID, meta.OutletID, in.Version, in.ExpectedVersion, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/menu-studios/"+id)
	})
}

func validCheckout(in domain.POSCheckout) error {
	if !domain.ValidUUID(in.ID) || !domain.ValidUUID(in.OutletID) || !domain.ValidUUID(in.OrderID) || !domain.ValidUUID(in.ReceiptID) || !domain.ValidOrderType(in.OrderType) || len(in.Lines) == 0 || len(in.Lines) > 500 || len(in.Tenders) == 0 || len(in.Tenders) > 12 || !validShortText(in.ReceiptNumber, 100) || !validShortText(in.PrinterRoute, 120) || in.DiscountMinor < 0 || in.TaxMinor < 0 || in.ServiceChargeMinor < 0 {
		return fmt.Errorf("checkout is invalid")
	}
	if in.MenuVersionID != "" && !domain.ValidUUID(in.MenuVersionID) {
		return fmt.Errorf("menuVersionId is invalid")
	}
	if in.BrandID != "" && !domain.ValidUUID(in.BrandID) {
		return fmt.Errorf("brandId is invalid")
	}
	if (in.PickupTokenID == "") != (in.PickupToken == "") || (in.PickupTokenID != "" && (!domain.ValidUUID(in.PickupTokenID) || !validPickupToken(in.PickupToken))) {
		return fmt.Errorf("pickup token is invalid")
	}
	lineIDs, tenderIDs := map[string]bool{}, map[string]bool{}
	for _, line := range in.Lines {
		if !domain.ValidUUID(line.ID) || lineIDs[line.ID] || !domain.ValidUUID(line.MenuItemID) || line.Quantity < 1 || line.Quantity > 999 || len(line.PreparationNote) > 500 {
			return fmt.Errorf("checkout line is invalid")
		}
		lineIDs[line.ID] = true
		selected := map[string]bool{}
		for _, optionID := range line.ModifierOptionIDs {
			if !domain.ValidUUID(optionID) || selected[optionID] {
				return fmt.Errorf("checkout modifier is invalid")
			}
			selected[optionID] = true
		}
	}
	for _, tender := range in.Tenders {
		if !domain.ValidUUID(tender.ID) || tenderIDs[tender.ID] || tender.AmountMinor < 1 || (tender.TenderType != "cash" && tender.TenderType != "upi" && tender.TenderType != "card_terminal" && tender.TenderType != "external") || len(tender.ProviderReference) > 160 {
			return fmt.Errorf("checkout tender is invalid")
		}
		if (tender.TenderType == "cash" && !domain.ValidUUID(tender.CashShiftID)) || (tender.TenderType != "cash" && tender.CashShiftID != "") {
			return fmt.Errorf("checkout cash shift is invalid")
		}
		tenderIDs[tender.ID] = true
	}
	return nil
}

func validPickupToken(value string) bool {
	if len(value) < 3 || len(value) > 12 {
		return false
	}
	for _, character := range value {
		if !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

func (s *Server) handleCheckoutPOS(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in domain.POSCheckout
		if result := decodeAndValidate(payload, &in, func() error {
			if !validOutletScoped(in.OutletID, meta.OutletID) {
				return fmt.Errorf("outletId must match the mutation outlet")
			}
			return validCheckout(in)
		}); result != nil {
			return *result
		}
		repo, ok := s.restaurantCore()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		in.TenantID, in.OutletID = meta.TenantID, meta.OutletID
		if in.PlacedAt.IsZero() {
			in.PlacedAt = now
		}
		audit, err := newAuditEvent(meta, "pos.checkout_completed", "pos_checkout", in.ID, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.CheckoutPOS(ctx, in, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, value, "/api/v1/orders/"+value.Order.ID)
	})
}

type printActionInput struct {
	Action string `json:"action"`
}

func (s *Server) handlePrintJobAction(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in printActionInput
		if result := decodeAndValidate(payload, &in, func() error {
			if in.Action != "acknowledged" && in.Action != "failed" && in.Action != "cancelled" && in.Action != "requeued" && in.Action != "reprinted" {
				return fmt.Errorf("print action is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		id := r.PathValue("id")
		if !domain.ValidUUID(id) {
			return errorResult(apiError{Status: 422, Code: "validation_failed", Message: "print job id is invalid"})
		}
		repo, ok := s.restaurantCore()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		audit, err := newAuditEvent(meta, "kitchen_print."+in.Action, "kitchen_print_job", id, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.AcknowledgeKitchenPrintJob(ctx, meta.TenantID, meta.OutletID, id, in.Action, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusOK, value, "")
	})
}

type pickupTokenTransitionInput struct {
	Status          string `json:"status"`
	ExpectedVersion uint64 `json:"expectedVersion"`
}

func (s *Server) handlePickupTokenTransition(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var in pickupTokenTransitionInput
		if result := decodeAndValidate(payload, &in, func() error {
			if (in.Status != "called" && in.Status != "collected" && in.Status != "cancelled") || in.ExpectedVersion < 1 {
				return fmt.Errorf("pickup token transition is invalid")
			}
			return nil
		}); result != nil {
			return *result
		}
		id := r.PathValue("id")
		if !domain.ValidUUID(id) {
			return errorResult(apiError{Status: 422, Code: "validation_failed", Message: "pickup token id is invalid"})
		}
		repo, ok := s.restaurantCore()
		if !ok {
			return internalOperationError()
		}
		now := s.now().UTC()
		audit, err := newAuditEvent(meta, "pickup_token."+in.Status, "pickup_token", id, now)
		if err != nil {
			return internalOperationError()
		}
		value, err := repo.TransitionPickupToken(ctx, meta.TenantID, meta.OutletID, id, in.Status, in.ExpectedVersion, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusOK, value, "")
	})
}
