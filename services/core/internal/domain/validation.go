// SPDX-License-Identifier: AGPL-3.0-only

package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	codePattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	uuidPattern     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
)

// Validate checks the transport-independent mutation metadata contract.
func (m MutationMetadata) Validate() error {
	required := map[string]string{
		"id":       m.ID,
		"tenantId": m.TenantID,
		"outletId": m.OutletID,
		"deviceId": m.DeviceID,
		"actorId":  m.ActorID,
		"source":   m.Source,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
		if len(value) > 128 {
			return fmt.Errorf("%s must be at most 128 characters", field)
		}
	}
	if m.OccurredAt.IsZero() {
		return fmt.Errorf("occurredAt is required and must be RFC3339")
	}
	if m.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("schemaVersion must be %q", CurrentSchemaVersion)
	}
	if len(m.SourceID) > 256 || len(m.CorrelationID) > 128 || len(m.CausationID) > 128 {
		return fmt.Errorf("sourceId must be at most 256 characters and correlation identifiers at most 128")
	}
	if len(m.IdempotencyKey) < 8 || len(m.IdempotencyKey) > 256 {
		return fmt.Errorf("idempotencyKey must be between 8 and 256 characters")
	}
	if !ValidUUID(m.ID) {
		return fmt.Errorf("id must be a UUID string")
	}
	if m.CorrelationID != "" && !ValidUUID(m.CorrelationID) {
		return fmt.Errorf("correlationId must be a UUID string")
	}
	if m.CausationID != "" && !ValidUUID(m.CausationID) {
		return fmt.Errorf("causationId must be a UUID string")
	}
	return nil
}

// ValidUUID reports whether value is an RFC 4122-compatible UUID string.
func ValidUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

// ValidCurrency reports whether currency is an uppercase ISO 4217-style code.
func ValidCurrency(currency string) bool {
	return currencyPattern.MatchString(currency)
}

// ValidCode reports whether code is suitable for stable operator-facing lookup.
func ValidCode(code string) bool {
	return codePattern.MatchString(code)
}

// ValidStationType reports whether a station type is currently supported.
func ValidStationType(stationType StationType) bool {
	switch stationType {
	case StationTypePreparation, StationTypeCooking, StationTypeBeverage,
		StationTypeAssembly, StationTypeExpo, StationTypePacking:
		return true
	default:
		return false
	}
}

// ValidOrderType reports whether an order fulfillment type is supported.
func ValidOrderType(orderType OrderType) bool {
	switch orderType {
	case OrderTypeDineIn, OrderTypeTakeaway, OrderTypeDelivery, OrderTypeRoomService:
		return true
	default:
		return false
	}
}

// ValidOrderStatus reports whether an order status is supported.
func ValidOrderStatus(status OrderStatus) bool {
	switch status {
	case OrderStatusReceived, OrderStatusAccepted, OrderStatusPreparing,
		OrderStatusReady, OrderStatusCompleted, OrderStatusCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionOrderStatus enforces the deterministic order lifecycle.
func CanTransitionOrderStatus(from, to OrderStatus) bool {
	allowed := map[OrderStatus]map[OrderStatus]bool{
		OrderStatusReceived:  {OrderStatusAccepted: true, OrderStatusCancelled: true},
		OrderStatusAccepted:  {OrderStatusPreparing: true, OrderStatusCancelled: true},
		OrderStatusPreparing: {OrderStatusReady: true, OrderStatusCancelled: true},
		OrderStatusReady:     {OrderStatusCompleted: true, OrderStatusCancelled: true},
	}
	return allowed[from][to]
}

// ValidTicketStatus reports whether a kitchen-ticket status is supported.
func ValidTicketStatus(status TicketStatus) bool {
	switch status {
	case TicketStatusQueued, TicketStatusFired, TicketStatusPreparing,
		TicketStatusReady, TicketStatusCompleted, TicketStatusCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionTicketStatus enforces the deterministic station lifecycle.
func CanTransitionTicketStatus(from, to TicketStatus) bool {
	allowed := map[TicketStatus]map[TicketStatus]bool{
		TicketStatusQueued:    {TicketStatusFired: true, TicketStatusCancelled: true},
		TicketStatusFired:     {TicketStatusPreparing: true, TicketStatusCancelled: true},
		TicketStatusPreparing: {TicketStatusReady: true, TicketStatusCancelled: true},
		TicketStatusReady:     {TicketStatusCompleted: true, TicketStatusCancelled: true},
	}
	return allowed[from][to]
}

func ValidProductionBatchStatus(status ProductionBatchStatus) bool {
	switch status {
	case ProductionBatchPlanned, ProductionBatchInProgress, ProductionBatchCompleted, ProductionBatchCancelled:
		return true
	default:
		return false
	}
}

func CanTransitionProductionBatchStatus(from, to ProductionBatchStatus) bool {
	allowed := map[ProductionBatchStatus]map[ProductionBatchStatus]bool{
		ProductionBatchPlanned:    {ProductionBatchInProgress: true, ProductionBatchCancelled: true},
		ProductionBatchInProgress: {ProductionBatchCompleted: true, ProductionBatchCancelled: true},
	}
	return allowed[from][to]
}

// ValidTimeZone verifies an IANA time-zone name.
func ValidTimeZone(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}
