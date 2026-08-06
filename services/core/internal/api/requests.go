// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

var certificateFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type deviceInput struct {
	ID                     string `json:"id"`
	OutletID               string `json:"outletId"`
	EdgeID                 string `json:"edgeId"`
	Name                   string `json:"name"`
	CertificateFingerprint string `json:"certificateFingerprint"`
}

func (input deviceInput) validate() error {
	if !domain.ValidUUID(input.ID) {
		return fmt.Errorf("id must be a UUID string")
	}
	if !domain.ValidUUID(input.OutletID) {
		return fmt.Errorf("outlet_id must be a UUID string")
	}
	if err := requiredText("edge_id", input.EdgeID, 128); err != nil {
		return err
	}
	if err := requiredText("name", input.Name, 160); err != nil {
		return err
	}
	if !certificateFingerprintPattern.MatchString(input.CertificateFingerprint) {
		return fmt.Errorf("certificate_fingerprint must be a lowercase SHA-256 fingerprint")
	}
	return nil
}

type revokeDeviceInput struct {
	Reason string `json:"reason"`
}

func (input revokeDeviceInput) validate() error {
	return requiredText("reason", input.Reason, 500)
}

type organizationInput struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	LegalName       string `json:"legalName,omitempty"`
	DefaultLocale   string `json:"defaultLocale"`
	DefaultCurrency string `json:"defaultCurrency"`
	Active          *bool  `json:"active,omitempty"`
}

// tenantProvisionInput is intentionally smaller than the whole domain model:
// a platform operator supplies commercial facts and a kitchen template, while
// the service owns identifiers and the tenant topology it creates.
type tenantProvisionInput struct {
	OrganizationName string `json:"organizationName"`
	LegalName        string `json:"legalName,omitempty"`
	OwnerName        string `json:"ownerName"`
	OwnerEmail       string `json:"ownerEmail"`
	DefaultLocale    string `json:"defaultLocale"`
	DefaultCurrency  string `json:"defaultCurrency"`
	OutletName       string `json:"outletName"`
	OutletCode       string `json:"outletCode"`
	TimeZone         string `json:"timeZone"`
	BrandName        string `json:"brandName"`
	BrandCode        string `json:"brandCode"`
	Template         string `json:"template"`
}

func (input tenantProvisionInput) validate() error {
	if err := requiredText("organization_name", input.OrganizationName, 160); err != nil { return err }
	if err := requiredText("owner_name", input.OwnerName, 160); err != nil { return err }
	if err := requiredText("owner_email", input.OwnerEmail, 254); err != nil { return err }
	if err := requiredText("outlet_name", input.OutletName, 160); err != nil { return err }
	if err := requiredText("brand_name", input.BrandName, 160); err != nil { return err }
	if len(input.LegalName) > 240 { return fmt.Errorf("legal_name must be at most 240 characters") }
	if err := requiredText("default_locale", input.DefaultLocale, 35); err != nil { return err }
	if !domain.ValidCurrency(input.DefaultCurrency) { return fmt.Errorf("default_currency must be a three-letter uppercase currency code") }
	if !domain.ValidCode(input.OutletCode) || !domain.ValidCode(input.BrandCode) { return fmt.Errorf("outlet_code and brand_code must be valid codes") }
	if !domain.ValidTimeZone(input.TimeZone) { return fmt.Errorf("time_zone must be a valid IANA time zone") }
	if !strings.Contains(strings.TrimSpace(input.OwnerEmail), "@") { return fmt.Errorf("owner_email must be an email address") }
	if input.Template != "restaurant" && input.Template != "cloud" && input.Template != "central" { return fmt.Errorf("template must be restaurant, cloud, or central") }
	return nil
}

func (input organizationInput) validate() error {
	if !domain.ValidUUID(input.ID) {
		return fmt.Errorf("id must be a UUID string")
	}
	if err := requiredText("name", input.Name, 160); err != nil {
		return err
	}
	if len(input.LegalName) > 240 {
		return fmt.Errorf("legal_name must be at most 240 characters")
	}
	if err := requiredText("default_locale", input.DefaultLocale, 35); err != nil {
		return err
	}
	if !domain.ValidCurrency(input.DefaultCurrency) {
		return fmt.Errorf("default_currency must be a three-letter uppercase currency code")
	}
	return nil
}

type outletInput struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId"`
	Name           string `json:"name"`
	Code           string `json:"code"`
	TimeZone       string `json:"timeZone"`
	Currency       string `json:"currency"`
	Active         *bool  `json:"active,omitempty"`
}

func (input outletInput) validate() error {
	if !domain.ValidUUID(input.ID) {
		return fmt.Errorf("id must be a UUID string")
	}
	if err := requiredText("organization_id", input.OrganizationID, 128); err != nil {
		return err
	}
	if err := requiredText("name", input.Name, 160); err != nil {
		return err
	}
	if !domain.ValidCode(input.Code) {
		return fmt.Errorf("code must start with a letter or number and contain only letters, numbers, '.', '_' or '-'")
	}
	if !domain.ValidTimeZone(input.TimeZone) {
		return fmt.Errorf("time_zone must be a valid IANA time zone")
	}
	if !domain.ValidCurrency(input.Currency) {
		return fmt.Errorf("currency must be a three-letter uppercase currency code")
	}
	return nil
}

type brandInput struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId"`
	Name           string `json:"name"`
	Code           string `json:"code"`
	Active         *bool  `json:"active,omitempty"`
}

func (input brandInput) validate() error {
	if !domain.ValidUUID(input.ID) {
		return fmt.Errorf("id must be a UUID string")
	}
	if err := requiredText("organization_id", input.OrganizationID, 128); err != nil {
		return err
	}
	if err := requiredText("name", input.Name, 160); err != nil {
		return err
	}
	if !domain.ValidCode(input.Code) {
		return fmt.Errorf("code must start with a letter or number and contain only letters, numbers, '.', '_' or '-'")
	}
	return nil
}

// brandOutletAssignmentInput creates an explicit rollout relationship. An
// expectedVersion of zero means "first assignment"; later changes must carry
// the version returned by the service so a stale manager cannot silently undo
// another manager's outlet rollout decision.
type brandOutletAssignmentInput struct {
	BrandID         string `json:"brandId"`
	OutletID        string `json:"outletId"`
	Active          bool   `json:"active"`
	ExpectedVersion uint64 `json:"expectedVersion,omitempty"`
}

func (input brandOutletAssignmentInput) validate() error {
	if !domain.ValidUUID(input.BrandID) {
		return fmt.Errorf("brand_id must be a UUID string")
	}
	if !domain.ValidUUID(input.OutletID) {
		return fmt.Errorf("outlet_id must be a UUID string")
	}
	return nil
}

type stationInput struct {
	ID       string             `json:"id"`
	OutletID string             `json:"outletId"`
	Name     string             `json:"name"`
	Code     string             `json:"code"`
	Type     domain.StationType `json:"type"`
	Active   *bool              `json:"active,omitempty"`
}

func (input stationInput) validate() error {
	if !domain.ValidUUID(input.ID) {
		return fmt.Errorf("id must be a UUID string")
	}
	if err := requiredText("outlet_id", input.OutletID, 128); err != nil {
		return err
	}
	if err := requiredText("name", input.Name, 160); err != nil {
		return err
	}
	if !domain.ValidCode(input.Code) {
		return fmt.Errorf("code must start with a letter or number and contain only letters, numbers, '.', '_' or '-'")
	}
	if !domain.ValidStationType(input.Type) {
		return fmt.Errorf("type is not a supported station type")
	}
	return nil
}

type orderLineInput struct {
	ID              string       `json:"id"`
	MenuItemID      string       `json:"menuItemId,omitempty"`
	Name            string       `json:"name"`
	Quantity        int32        `json:"quantity"`
	UnitPrice       domain.Money `json:"unitPrice"`
	LineTotal       domain.Money `json:"lineTotal"`
	PreparationNote string       `json:"preparationNote,omitempty"`
}

type orderInput struct {
	ID            string             `json:"id"`
	OutletID      string             `json:"outletId"`
	BrandID       string             `json:"brandId,omitempty"`
	ExternalRef   string             `json:"externalRef,omitempty"`
	Type          domain.OrderType   `json:"type"`
	Status        domain.OrderStatus `json:"status,omitempty"`
	Lines         []orderLineInput   `json:"lines"`
	Subtotal      domain.Money       `json:"subtotal"`
	DiscountTotal domain.Money       `json:"discountTotal"`
	TaxTotal      domain.Money       `json:"taxTotal"`
	ServiceCharge domain.Money       `json:"serviceCharge"`
	Total         domain.Money       `json:"total"`
	PlacedAt      time.Time          `json:"placedAt"`
}

func (input orderInput) validate() error {
	if !domain.ValidUUID(input.ID) {
		return fmt.Errorf("id must be a UUID string")
	}
	if err := requiredText("outlet_id", input.OutletID, 128); err != nil {
		return err
	}
	if len(input.BrandID) > 128 || len(input.ExternalRef) > 128 {
		return fmt.Errorf("brand_id and external_ref must be at most 128 characters")
	}
	if !domain.ValidOrderType(input.Type) {
		return fmt.Errorf("type is not a supported order type")
	}
	if input.Status != "" && !domain.ValidOrderStatus(input.Status) {
		return fmt.Errorf("status is not a supported order status")
	}
	if input.PlacedAt.IsZero() {
		return fmt.Errorf("placed_at is required and must be RFC3339")
	}
	if len(input.Lines) == 0 || len(input.Lines) > 500 {
		return fmt.Errorf("lines must contain between 1 and 500 items")
	}

	currency := input.Total.Currency
	amounts := map[string]domain.Money{
		"subtotal":       input.Subtotal,
		"discount_total": input.DiscountTotal,
		"tax_total":      input.TaxTotal,
		"service_charge": input.ServiceCharge,
		"total":          input.Total,
	}
	if !domain.ValidCurrency(currency) {
		return fmt.Errorf("total.currency must be a three-letter uppercase currency code")
	}
	for name, amount := range amounts {
		if amount.Currency != currency {
			return fmt.Errorf("%s.currency must equal total.currency", name)
		}
		if amount.MinorUnits < 0 {
			return fmt.Errorf("%s.minor_units cannot be negative", name)
		}
	}

	var computedSubtotal int64
	for index, line := range input.Lines {
		if !domain.ValidUUID(line.ID) {
			return fmt.Errorf("lines[%d].id must be a UUID string", index)
		}
		if err := requiredText(fmt.Sprintf("lines[%d].name", index), line.Name, 200); err != nil {
			return err
		}
		if len(line.MenuItemID) > 128 || len(line.PreparationNote) > 500 {
			return fmt.Errorf("lines[%d] contains a field longer than its allowed limit", index)
		}
		if line.Quantity <= 0 {
			return fmt.Errorf("lines[%d].quantity must be greater than zero", index)
		}
		if line.UnitPrice.Currency != currency || line.LineTotal.Currency != currency {
			return fmt.Errorf("lines[%d] money must use order currency", index)
		}
		if line.UnitPrice.MinorUnits < 0 || line.LineTotal.MinorUnits < 0 {
			return fmt.Errorf("lines[%d] money cannot be negative", index)
		}
		if line.UnitPrice.MinorUnits > math.MaxInt64/int64(line.Quantity) {
			return fmt.Errorf("lines[%d] amount exceeds supported range", index)
		}
		expected := line.UnitPrice.MinorUnits * int64(line.Quantity)
		if line.LineTotal.MinorUnits != expected {
			return fmt.Errorf("lines[%d].line_total must equal unit_price multiplied by quantity", index)
		}
		if computedSubtotal > math.MaxInt64-line.LineTotal.MinorUnits {
			return fmt.Errorf("subtotal exceeds supported range")
		}
		computedSubtotal += line.LineTotal.MinorUnits
	}
	if input.Subtotal.MinorUnits != computedSubtotal {
		return fmt.Errorf("subtotal must equal the sum of line totals")
	}
	expectedTotal := input.Subtotal.MinorUnits - input.DiscountTotal.MinorUnits
	if expectedTotal < 0 || expectedTotal > math.MaxInt64-input.TaxTotal.MinorUnits {
		return fmt.Errorf("order total calculation exceeds supported range")
	}
	expectedTotal += input.TaxTotal.MinorUnits
	if expectedTotal > math.MaxInt64-input.ServiceCharge.MinorUnits {
		return fmt.Errorf("order total calculation exceeds supported range")
	}
	expectedTotal += input.ServiceCharge.MinorUnits
	if input.Total.MinorUnits != expectedTotal {
		return fmt.Errorf("total must equal subtotal minus discounts plus tax and service charge")
	}
	return nil
}

type ticketInput struct {
	ID        string              `json:"id"`
	OutletID  string              `json:"outletId"`
	OrderID   string              `json:"orderId"`
	StationID string              `json:"stationId"`
	LineIDs   []string            `json:"lineIds"`
	Status    domain.TicketStatus `json:"status,omitempty"`
	Priority  int                 `json:"priority"`
	TargetAt  *time.Time          `json:"targetAt,omitempty"`
}

type orderTransitionInput struct {
	ToStatus        domain.OrderStatus `json:"toStatus"`
	ExpectedVersion uint64             `json:"expectedVersion"`
}

func (input orderTransitionInput) validate() error {
	if !domain.ValidOrderStatus(input.ToStatus) {
		return fmt.Errorf("to_status is not supported")
	}
	if input.ExpectedVersion < 1 {
		return fmt.Errorf("expected_version must be at least one")
	}
	return nil
}

type ticketTransitionInput struct {
	ToStatus        domain.TicketStatus `json:"toStatus"`
	ExpectedVersion uint64              `json:"expectedVersion"`
}

func (input ticketTransitionInput) validate() error {
	if !domain.ValidTicketStatus(input.ToStatus) {
		return fmt.Errorf("to_status is not supported")
	}
	if input.ExpectedVersion < 1 {
		return fmt.Errorf("expected_version must be at least one")
	}
	return nil
}

func (input ticketInput) validate() error {
	if !domain.ValidUUID(input.ID) {
		return fmt.Errorf("id must be a UUID string")
	}
	for field, value := range map[string]string{
		"outlet_id":  input.OutletID,
		"order_id":   input.OrderID,
		"station_id": input.StationID,
	} {
		if err := requiredText(field, value, 128); err != nil {
			return err
		}
	}
	if len(input.LineIDs) == 0 || len(input.LineIDs) > 500 {
		return fmt.Errorf("line_ids must contain between 1 and 500 identifiers")
	}
	for index, id := range input.LineIDs {
		if err := requiredText(fmt.Sprintf("line_ids[%d]", index), id, 128); err != nil {
			return err
		}
	}
	if input.Status != "" && !domain.ValidTicketStatus(input.Status) {
		return fmt.Errorf("status is not a supported kitchen-ticket status")
	}
	if input.Priority < 0 || input.Priority > 100 {
		return fmt.Errorf("priority must be between 0 and 100")
	}
	return nil
}

func requiredText(field, value string, maxLength int) error {
	length := len(strings.TrimSpace(value))
	if length == 0 {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > maxLength {
		return fmt.Errorf("%s must be at most %d characters", field, maxLength)
	}
	return nil
}

func boolDefaultTrue(value *bool) bool {
	return value == nil || *value
}
