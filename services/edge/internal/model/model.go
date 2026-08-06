// SPDX-License-Identifier: AGPL-3.0-only

// Package model contains the outlet edge's transport-independent data types.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

const CurrentSchemaVersion = "1.0"

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

var uuidV7State struct {
	sync.Mutex
	milliseconds int64
	sequence     uint16
}

// MutationEnvelope is the canonical metadata carried by every edge command.
// Its JSON shape intentionally matches packages/contracts/schemas/mutation-envelope.json.
type MutationEnvelope struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenantId"`
	OutletID       string          `json:"outletId"`
	DeviceID       string          `json:"deviceId"`
	ActorID        string          `json:"actorId"`
	OccurredAt     time.Time       `json:"occurredAt"`
	Source         string          `json:"source"`
	SourceID       string          `json:"sourceId,omitempty"`
	SchemaVersion  string          `json:"schemaVersion"`
	IdempotencyKey string          `json:"idempotencyKey"`
	CausationID    string          `json:"causationId,omitempty"`
	CorrelationID  string          `json:"correlationId,omitempty"`
	Payload        json.RawMessage `json:"payload"`
}

// Validate checks the stable mutation metadata independently of command payloads.
func (envelope MutationEnvelope) Validate() error {
	if !IsUUIDv7(envelope.ID) {
		return errors.New("id must be a UUIDv7")
	}
	if err := bounded("tenantId", envelope.TenantID, 1, 128); err != nil {
		return err
	}
	if err := bounded("outletId", envelope.OutletID, 1, 128); err != nil {
		return err
	}
	if err := bounded("deviceId", envelope.DeviceID, 1, 128); err != nil {
		return err
	}
	if err := bounded("actorId", envelope.ActorID, 1, 128); err != nil {
		return err
	}
	if envelope.OccurredAt.IsZero() {
		return errors.New("occurredAt must be an RFC 3339 timestamp")
	}
	if err := bounded("source", envelope.Source, 1, 128); err != nil {
		return err
	}
	if len(envelope.SourceID) > 256 {
		return errors.New("sourceId must not exceed 256 characters")
	}
	if envelope.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("schemaVersion must be %q", CurrentSchemaVersion)
	}
	if err := bounded("idempotencyKey", envelope.IdempotencyKey, 8, 256); err != nil {
		return err
	}
	if envelope.CausationID != "" && !IsUUID(envelope.CausationID) {
		return errors.New("causationId must be a UUID")
	}
	if envelope.CorrelationID != "" && !IsUUID(envelope.CorrelationID) {
		return errors.New("correlationId must be a UUID")
	}
	if len(envelope.Payload) == 0 || string(envelope.Payload) == "null" {
		return errors.New("payload must be an object")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload == nil {
		return errors.New("payload must be an object")
	}
	return nil
}

func bounded(field, value string, minimum, maximum int) error {
	length := len(strings.TrimSpace(value))
	if length < minimum || length > maximum {
		return fmt.Errorf("%s must contain between %d and %d characters", field, minimum, maximum)
	}
	return nil
}

// IsUUID accepts RFC 4122-compatible UUID versions, including UUIDv7 operation IDs.
func IsUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

// IsUUIDv7 verifies the RFC 9562 version and RFC 4122 variant bits.
func IsUUIDv7(value string) bool {
	return uuidPattern.MatchString(value) && (value[14] == '7')
}

// NewUUID creates an RFC 4122 version 4 UUID for edge-owned resource identifiers.
func NewUUID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("model: secure random source unavailable: %v", err))
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded)
}

// NewUUIDv7 creates a time-ordered RFC 9562 UUIDv7. A process-local sequence
// preserves lexical order for calls within one millisecond and across small
// wall-clock regressions; the remaining 62 bits come from crypto/rand.
func NewUUIDv7() string {
	var value [16]byte
	if _, err := rand.Read(value[8:]); err != nil {
		panic(fmt.Sprintf("model: secure random source unavailable: %v", err))
	}

	nowMilliseconds := time.Now().UnixMilli()
	uuidV7State.Lock()
	if nowMilliseconds > uuidV7State.milliseconds {
		var seed [2]byte
		if _, err := rand.Read(seed[:]); err != nil {
			uuidV7State.Unlock()
			panic(fmt.Sprintf("model: secure random source unavailable: %v", err))
		}
		uuidV7State.milliseconds = nowMilliseconds
		uuidV7State.sequence = (uint16(seed[0])<<8 | uint16(seed[1])) & 0x0fff
	} else {
		nowMilliseconds = uuidV7State.milliseconds
		if uuidV7State.sequence == 0x0fff {
			uuidV7State.milliseconds++
			nowMilliseconds = uuidV7State.milliseconds
			uuidV7State.sequence = 0
		} else {
			uuidV7State.sequence++
		}
	}
	sequence := uuidV7State.sequence
	uuidV7State.Unlock()

	timestamp := uint64(nowMilliseconds)
	value[0] = byte(timestamp >> 40)
	value[1] = byte(timestamp >> 32)
	value[2] = byte(timestamp >> 24)
	value[3] = byte(timestamp >> 16)
	value[4] = byte(timestamp >> 8)
	value[5] = byte(timestamp)
	value[6] = 0x70 | byte(sequence>>8)
	value[7] = byte(sequence)
	value[8] = (value[8] & 0x3f) | 0x80

	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded)
}

type OrderType string

const (
	OrderTypeDineIn      OrderType = "dineIn"
	OrderTypeTakeaway    OrderType = "takeaway"
	OrderTypeDelivery    OrderType = "delivery"
	OrderTypeRoomService OrderType = "roomService"
)

func (kind OrderType) Valid() bool {
	switch kind {
	case OrderTypeDineIn, OrderTypeTakeaway, OrderTypeDelivery, OrderTypeRoomService:
		return true
	default:
		return false
	}
}

type OrderStatus string

const (
	OrderStatusReceived  OrderStatus = "received"
	OrderStatusAccepted  OrderStatus = "accepted"
	OrderStatusPreparing OrderStatus = "preparing"
	OrderStatusReady     OrderStatus = "ready"
	OrderStatusCompleted OrderStatus = "completed"
	OrderStatusCancelled OrderStatus = "cancelled"
)

func (status OrderStatus) Valid() bool {
	switch status {
	case OrderStatusReceived, OrderStatusAccepted, OrderStatusPreparing, OrderStatusReady, OrderStatusCompleted, OrderStatusCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionOrder is the only allowed public order state machine.
func CanTransitionOrder(from, to OrderStatus) bool {
	if to == OrderStatusCancelled {
		return from != OrderStatusCompleted && from != OrderStatusCancelled
	}
	switch from {
	case OrderStatusReceived:
		return to == OrderStatusAccepted
	case OrderStatusAccepted:
		return to == OrderStatusPreparing
	case OrderStatusPreparing:
		return to == OrderStatusReady
	case OrderStatusReady:
		return to == OrderStatusCompleted
	default:
		return false
	}
}

type TicketStatus string

const (
	TicketStatusQueued    TicketStatus = "queued"
	TicketStatusFired     TicketStatus = "fired"
	TicketStatusPreparing TicketStatus = "preparing"
	TicketStatusReady     TicketStatus = "ready"
	TicketStatusCompleted TicketStatus = "completed"
	TicketStatusCancelled TicketStatus = "cancelled"
)

func (status TicketStatus) Valid() bool {
	switch status {
	case TicketStatusQueued, TicketStatusFired, TicketStatusPreparing, TicketStatusReady, TicketStatusCompleted, TicketStatusCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionTicket is the only allowed public KDS ticket state machine.
func CanTransitionTicket(from, to TicketStatus) bool {
	if to == TicketStatusCancelled {
		return from != TicketStatusCompleted && from != TicketStatusCancelled
	}
	switch from {
	case TicketStatusQueued:
		return to == TicketStatusFired
	case TicketStatusFired:
		return to == TicketStatusPreparing
	case TicketStatusPreparing:
		return to == TicketStatusReady
	case TicketStatusReady:
		return to == TicketStatusCompleted
	default:
		return false
	}
}

type OrderLine struct {
	ID              string `json:"id"`
	MenuItemID      string `json:"menuItemId,omitempty"`
	Name            string `json:"name"`
	Quantity        int32  `json:"quantity"`
	StationID       string `json:"stationId"`
	PreparationNote string `json:"preparationNote,omitempty"`
}

type Order struct {
	ID          string      `json:"id"`
	TenantID    string      `json:"tenantId"`
	OutletID    string      `json:"outletId"`
	BrandID     string      `json:"brandId,omitempty"`
	ExternalRef string      `json:"externalRef,omitempty"`
	GuestName   string      `json:"guestName,omitempty"`
	TableLabel  string      `json:"tableLabel,omitempty"`
	Note        string      `json:"note,omitempty"`
	Number      int64       `json:"number"`
	Type        OrderType   `json:"type"`
	Status      OrderStatus `json:"status"`
	Lines       []OrderLine `json:"lines"`
	PlacedAt    time.Time   `json:"placedAt"`
	TargetAt    *time.Time  `json:"targetAt,omitempty"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
	Version     uint64      `json:"version"`
}

type KitchenTicket struct {
	ID        string       `json:"id"`
	TenantID  string       `json:"tenantId"`
	OutletID  string       `json:"outletId"`
	OrderID   string       `json:"orderId"`
	StationID string       `json:"stationId"`
	LineIDs   []string     `json:"lineIds"`
	Status    TicketStatus `json:"status"`
	Priority  int          `json:"priority"`
	TargetAt  *time.Time   `json:"targetAt,omitempty"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
	Version   uint64       `json:"version"`
}

type CreateOrderPayload struct {
	Order NewOrder `json:"order"`
}

type NewOrder struct {
	ID          string      `json:"id"`
	BrandID     string      `json:"brandId,omitempty"`
	ExternalRef string      `json:"externalRef,omitempty"`
	GuestName   string      `json:"guestName,omitempty"`
	TableLabel  string      `json:"tableLabel,omitempty"`
	Note        string      `json:"note,omitempty"`
	Type        OrderType   `json:"type"`
	Lines       []OrderLine `json:"lines"`
	Priority    int         `json:"priority,omitempty"`
	// StationTicketIDs lets an offline PWA allocate stable ticket identities so
	// station work can begin before the order reaches the outlet edge.
	StationTicketIDs map[string]string `json:"stationTicketIds,omitempty"`
	TargetAt         *time.Time        `json:"targetAt,omitempty"`
	PlacedAt         time.Time         `json:"placedAt"`
}

func (input NewOrder) Validate() error {
	if !IsUUIDv7(input.ID) {
		return errors.New("order.id must be a UUIDv7")
	}
	if len(input.BrandID) > 128 || len(input.ExternalRef) > 256 {
		return errors.New("brandId or externalRef exceeds its maximum length")
	}
	if len(input.GuestName) > 256 || len(input.TableLabel) > 64 || len(input.Note) > 1_000 {
		return errors.New("order guestName, tableLabel, or note exceeds its maximum length")
	}
	if !input.Type.Valid() {
		return errors.New("order.type is not supported")
	}
	if len(input.Lines) == 0 || len(input.Lines) > 500 {
		return errors.New("order.lines must contain between 1 and 500 items")
	}
	seen := map[string]struct{}{input.ID: {}}
	stations := make(map[string]struct{})
	for index, line := range input.Lines {
		if !IsUUIDv7(line.ID) {
			return fmt.Errorf("order.lines[%d].id must be a UUIDv7", index)
		}
		if _, exists := seen[line.ID]; exists {
			return fmt.Errorf("order.lines[%d].id is duplicated", index)
		}
		seen[line.ID] = struct{}{}
		stations[line.StationID] = struct{}{}
		if err := bounded(fmt.Sprintf("order.lines[%d].name", index), line.Name, 1, 256); err != nil {
			return err
		}
		if line.Quantity <= 0 || line.Quantity > 10_000 {
			return fmt.Errorf("order.lines[%d].quantity must be between 1 and 10000", index)
		}
		if err := bounded(fmt.Sprintf("order.lines[%d].stationId", index), line.StationID, 1, 128); err != nil {
			return err
		}
		if len(line.MenuItemID) > 128 || len(line.PreparationNote) > 1_000 {
			return fmt.Errorf("order.lines[%d] exceeds a field length limit", index)
		}
	}
	if len(input.StationTicketIDs) > len(stations) {
		return errors.New("stationTicketIds contains more entries than the routed stations")
	}
	for stationID, ticketID := range input.StationTicketIDs {
		if _, exists := stations[stationID]; !exists {
			return fmt.Errorf("stationTicketIds contains unknown station %q", stationID)
		}
		if !IsUUIDv7(ticketID) {
			return fmt.Errorf("stationTicketIds[%q] must be a UUIDv7", stationID)
		}
		if _, exists := seen[ticketID]; exists {
			return fmt.Errorf("stationTicketIds[%q] is duplicated", stationID)
		}
		seen[ticketID] = struct{}{}
	}
	if input.Priority < -100 || input.Priority > 100 {
		return errors.New("order.priority must be between -100 and 100")
	}
	return nil
}

type TransitionOrderPayload struct {
	ToStatus        OrderStatus `json:"toStatus"`
	ExpectedVersion uint64      `json:"expectedVersion"`
}

func (input TransitionOrderPayload) Validate() error {
	if !input.ToStatus.Valid() {
		return errors.New("toStatus is not a valid order status")
	}
	if input.ExpectedVersion == 0 {
		return errors.New("expectedVersion must be greater than zero")
	}
	return nil
}

type TransitionTicketPayload struct {
	ToStatus        TicketStatus `json:"toStatus"`
	ExpectedVersion uint64       `json:"expectedVersion"`
	// ExpectedOrderID is set only by the browser compatibility ingress so the
	// ticket/parent relationship is verified inside the transition transaction.
	ExpectedOrderID string `json:"-"`
}

// BrowserMutationPayload is the PWA-to-edge compatibility command. The event
// remains a normal canonical mutation and is converted into edge projections.
type BrowserMutationPayload struct {
	EventType       string       `json:"eventType"`
	AggregateType   string       `json:"aggregateType"`
	AggregateID     string       `json:"aggregateId"`
	Order           *NewOrder    `json:"order,omitempty"`
	OrderID         string       `json:"orderId,omitempty"`
	TicketID        string       `json:"ticketId,omitempty"`
	ToStatus        TicketStatus `json:"toStatus,omitempty"`
	ExpectedVersion uint64       `json:"expectedVersion,omitempty"`
}

func (input TransitionTicketPayload) Validate() error {
	if !input.ToStatus.Valid() {
		return errors.New("toStatus is not a valid ticket status")
	}
	if input.ExpectedVersion == 0 {
		return errors.New("expectedVersion must be greater than zero")
	}
	return nil
}

type CreateOrderResult struct {
	Order   Order           `json:"order"`
	Tickets []KitchenTicket `json:"tickets"`
}

type TransitionOrderResult struct {
	Order   Order           `json:"order"`
	Tickets []KitchenTicket `json:"tickets"`
}

type TransitionTicketResult struct {
	Ticket KitchenTicket `json:"ticket"`
	Order  Order         `json:"order"`
}

type ResponseMeta struct {
	OperationID string `json:"operationId,omitempty"`
	Count       int    `json:"count,omitempty"`
}

type ResponseEnvelope struct {
	Data any          `json:"data"`
	Meta ResponseMeta `json:"meta"`
}

// Operation is the durable edge-to-cloud unit.
type Operation struct {
	OperationID      string           `json:"operationId"`
	AggregateType    string           `json:"aggregateType"`
	AggregateID      string           `json:"aggregateId"`
	AggregateVersion uint64           `json:"aggregateVersion"`
	CommandType      string           `json:"commandType"`
	Mutation         MutationEnvelope `json:"mutation"`
	RecordedAt       time.Time        `json:"recordedAt"`
}

type PushOperationsRequest struct {
	BatchID    string      `json:"batchId"`
	EdgeID     string      `json:"edgeId"`
	OutletID   string      `json:"outletId"`
	Operations []Operation `json:"operations"`
}

type PushResultStatus string

const (
	PushAccepted  PushResultStatus = "ACCEPTED"
	PushDuplicate PushResultStatus = "DUPLICATE"
	PushRejected  PushResultStatus = "REJECTED"
	PushConflict  PushResultStatus = "CONFLICT"
	PushRetry     PushResultStatus = "RETRY"
)

func (status PushResultStatus) Valid() bool {
	switch status {
	case PushAccepted, PushDuplicate, PushRejected, PushConflict, PushRetry:
		return true
	default:
		return false
	}
}

type PushResult struct {
	OperationID       string           `json:"operationId"`
	Status            PushResultStatus `json:"status"`
	ProblemCode       string           `json:"problemCode,omitempty"`
	RetryAfterSeconds int              `json:"retryAfterSeconds,omitempty"`
}

type PushOperationsResponse struct {
	BatchID string       `json:"batchId"`
	Results []PushResult `json:"results"`
}
