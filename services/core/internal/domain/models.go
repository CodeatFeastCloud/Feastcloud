// SPDX-License-Identifier: AGPL-3.0-only

// Package domain contains FeastCloud's canonical, transport-independent models.
package domain

import (
	"encoding/json"
	"time"
)

const (
	// CurrentSchemaVersion is the mutation schema accepted by this service.
	CurrentSchemaVersion = "1.0"
)

// MutationMetadata identifies who produced a write and makes it safe to replay.
// OutletID is omitted only for organization-scoped mutations.
type MutationMetadata struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenantId"`
	OutletID       string    `json:"outletId"`
	DeviceID       string    `json:"deviceId"`
	ActorID        string    `json:"actorId"`
	OccurredAt     time.Time `json:"occurredAt"`
	Source         string    `json:"source"`
	SourceID       string    `json:"sourceId,omitempty"`
	SchemaVersion  string    `json:"schemaVersion"`
	IdempotencyKey string    `json:"idempotencyKey"`
	CorrelationID  string    `json:"correlationId,omitempty"`
	CausationID    string    `json:"causationId,omitempty"`
}

// RecordMetadata is shared by mutable aggregate roots. Version starts at one.
type RecordMetadata struct {
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Version   uint64    `json:"version"`
}

// Organization is the top-level operational and tenant governance boundary.
type Organization struct {
	ID              string `json:"id"`
	TenantID        string `json:"tenantId"`
	Name            string `json:"name"`
	LegalName       string `json:"legalName,omitempty"`
	DefaultLocale   string `json:"defaultLocale"`
	DefaultCurrency string `json:"defaultCurrency"`
	Active          bool   `json:"active"`
	RecordMetadata
}

// Outlet is a physical or virtual place that fulfills orders.
type Outlet struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenantId"`
	OrganizationID string `json:"organizationId"`
	Name           string `json:"name"`
	Code           string `json:"code"`
	TimeZone       string `json:"timeZone"`
	Currency       string `json:"currency"`
	Active         bool   `json:"active"`
	RecordMetadata
}

// Brand groups a customer-facing menu identity within an organization.
type Brand struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenantId"`
	OrganizationID string `json:"organizationId"`
	Name           string `json:"name"`
	Code           string `json:"code"`
	Active         bool   `json:"active"`
	RecordMetadata
}

// BrandOutletAssignment makes a customer-facing brand explicitly available at
// an outlet. A brand is organization-owned; this record is the controlled
// rollout boundary that prevents a newly created virtual brand from appearing
// at every physical kitchen by accident.
type BrandOutletAssignment struct {
	TenantID string `json:"tenantId"`
	BrandID  string `json:"brandId"`
	OutletID string `json:"outletId"`
	Active   bool   `json:"active"`
	RecordMetadata
}

// StationType classifies a kitchen execution station.
type StationType string

const (
	StationTypePreparation StationType = "preparation"
	StationTypeCooking     StationType = "cooking"
	StationTypeBeverage    StationType = "beverage"
	StationTypeAssembly    StationType = "assembly"
	StationTypeExpo        StationType = "expo"
	StationTypePacking     StationType = "packing"
)

// Station is a routable work queue inside an outlet.
type Station struct {
	ID       string      `json:"id"`
	TenantID string      `json:"tenantId"`
	OutletID string      `json:"outletId"`
	Name     string      `json:"name"`
	Code     string      `json:"code"`
	Type     StationType `json:"type"`
	Active   bool        `json:"active"`
	RecordMetadata
}

// Money represents an exact amount in ISO 4217 minor units.
type Money struct {
	MinorUnits int64  `json:"minorUnits"`
	Currency   string `json:"currency"`
}

// OrderType identifies the fulfillment flow for an order.
type OrderType string

const (
	OrderTypeDineIn      OrderType = "dineIn"
	OrderTypeTakeaway    OrderType = "takeaway"
	OrderTypeDelivery    OrderType = "delivery"
	OrderTypeRoomService OrderType = "roomService"
)

// OrderStatus is the deterministic lifecycle state of an order.
type OrderStatus string

const (
	OrderStatusReceived  OrderStatus = "received"
	OrderStatusAccepted  OrderStatus = "accepted"
	OrderStatusPreparing OrderStatus = "preparing"
	OrderStatusReady     OrderStatus = "ready"
	OrderStatusCompleted OrderStatus = "completed"
	OrderStatusCancelled OrderStatus = "cancelled"
)

// OrderLine captures the commercial facts needed to reproduce an order total.
type OrderLine struct {
	ID              string `json:"id"`
	MenuItemID      string `json:"menuItemId,omitempty"`
	Name            string `json:"name"`
	Quantity        int32  `json:"quantity"`
	UnitPrice       Money  `json:"unitPrice"`
	LineTotal       Money  `json:"lineTotal"`
	PreparationNote string `json:"preparationNote,omitempty"`
}

// Order is a canonical order regardless of its originating channel.
type Order struct {
	ID            string      `json:"id"`
	TenantID      string      `json:"tenantId"`
	OutletID      string      `json:"outletId"`
	BrandID       string      `json:"brandId,omitempty"`
	ExternalRef   string      `json:"externalRef,omitempty"`
	Type          OrderType   `json:"type"`
	Status        OrderStatus `json:"status"`
	Lines         []OrderLine `json:"lines"`
	Subtotal      Money       `json:"subtotal"`
	DiscountTotal Money       `json:"discountTotal"`
	TaxTotal      Money       `json:"taxTotal"`
	ServiceCharge Money       `json:"serviceCharge"`
	Total         Money       `json:"total"`
	PlacedAt      time.Time   `json:"placedAt"`
	RecordMetadata
}

// TicketStatus is the deterministic lifecycle state of a kitchen ticket.
type TicketStatus string

const (
	TicketStatusQueued    TicketStatus = "queued"
	TicketStatusFired     TicketStatus = "fired"
	TicketStatusPreparing TicketStatus = "preparing"
	TicketStatusReady     TicketStatus = "ready"
	TicketStatusCompleted TicketStatus = "completed"
	TicketStatusCancelled TicketStatus = "cancelled"
)

// KitchenTicket routes selected order lines to one station.
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
	RecordMetadata
}

// AuditEvent is an immutable record appended atomically with a domain mutation.
type AuditEvent struct {
	ID             string    `json:"id"`
	OperationID    string    `json:"operationId"`
	TenantID       string    `json:"tenantId"`
	OutletID       string    `json:"outletId,omitempty"`
	ActorID        string    `json:"actorId"`
	DeviceID       string    `json:"deviceId"`
	Source         string    `json:"source"`
	SourceID       string    `json:"sourceId,omitempty"`
	IdempotencyKey string    `json:"idempotencyKey"`
	CorrelationID  string    `json:"correlationId,omitempty"`
	SchemaVersion  string    `json:"schemaVersion"`
	Action         string    `json:"action"`
	EntityType     string    `json:"entityType"`
	EntityID       string    `json:"entityId"`
	OccurredAt     time.Time `json:"occurredAt"`
	RecordedAt     time.Time `json:"recordedAt"`
}

type Unit struct {
	ID              string `json:"id"`
	TenantID        string `json:"tenantId"`
	Name            string `json:"name"`
	Symbol          string `json:"symbol"`
	Dimension       string `json:"dimension"`
	BaseNumerator   int64  `json:"baseNumerator"`
	BaseDenominator int64  `json:"baseDenominator"`
	Active          bool   `json:"active"`
	RecordMetadata
}

type Ingredient struct {
	ID            string   `json:"id"`
	TenantID      string   `json:"tenantId"`
	Name          string   `json:"name"`
	Code          string   `json:"code"`
	BaseUnitID    string   `json:"baseUnitId"`
	Allergens     []string `json:"allergens"`
	DietaryLabels []string `json:"dietaryLabels"`
	Active        bool     `json:"active"`
	RecordMetadata
}

type RecipeComponent struct {
	ID                   string  `json:"id"`
	IngredientID         string  `json:"ingredientId,omitempty"`
	ChildRecipeVersionID string  `json:"childRecipeVersionId,omitempty"`
	UnitID               string  `json:"unitId"`
	PreparationNote      string  `json:"preparationNote,omitempty"`
	Quantity             float64 `json:"quantity"`
}

type Recipe struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenantId"`
	Name           string         `json:"name"`
	Code           string         `json:"code"`
	Active         bool           `json:"active"`
	CurrentVersion *RecipeVersion `json:"currentVersion,omitempty"`
	RecordMetadata
}

type RecipeVersion struct {
	ID                     string            `json:"id"`
	TenantID               string            `json:"tenantId"`
	RecipeID               string            `json:"recipeId"`
	YieldUnitID            string            `json:"yieldUnitId"`
	Instructions           string            `json:"instructions"`
	VersionNumber          uint64            `json:"versionNumber"`
	YieldQuantity          float64           `json:"yieldQuantity"`
	PreparationLossPercent float64           `json:"preparationLossPercent"`
	CookingLossPercent     float64           `json:"cookingLossPercent"`
	EffectiveFrom          time.Time         `json:"effectiveFrom"`
	EffectiveTo            *time.Time        `json:"effectiveTo,omitempty"`
	Components             []RecipeComponent `json:"components"`
	CreatedAt              time.Time         `json:"createdAt"`
}

type MenuItem struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenantId"`
	OutletID   string `json:"outletId"`
	BrandID    string `json:"brandId,omitempty"`
	RecipeID   string `json:"recipeId"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	Currency   string `json:"currency"`
	StationID  string `json:"stationId,omitempty"`
	PriceMinor int64  `json:"priceMinor"`
	Active     bool   `json:"active"`
	RecordMetadata
}

type InventoryEvent struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenantId"`
	OutletID        string     `json:"outletId"`
	IngredientID    string     `json:"ingredientId"`
	EventType       string     `json:"eventType"`
	Currency        string     `json:"currency"`
	QuantityBase    float64    `json:"quantityBase"`
	TotalCostMinor  int64      `json:"totalCostMinor"`
	ReferenceType   string     `json:"referenceType"`
	ReferenceID     string     `json:"referenceId"`
	LotCode         string     `json:"lotCode,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	ActorID         string     `json:"actorId"`
	DeviceID        string     `json:"deviceId"`
	OperationID     string     `json:"operationId"`
	ReversesEventID string     `json:"reversesEventId,omitempty"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	OccurredAt      time.Time  `json:"occurredAt"`
	RecordedAt      time.Time  `json:"recordedAt"`
}

type InventorySummary struct {
	IngredientID            string  `json:"ingredientId"`
	BaseUnitID              string  `json:"baseUnitId"`
	IngredientName          string  `json:"ingredientName"`
	UnitSymbol              string  `json:"unitSymbol"`
	Currency                string  `json:"currency"`
	QuantityBase            float64 `json:"quantityBase"`
	ReceivedQuantity        float64 `json:"receivedQuantity"`
	ConsumedQuantity        float64 `json:"consumedQuantity"`
	WasteQuantity           float64 `json:"wasteQuantity"`
	CountVarianceQuantity   float64 `json:"countVarianceQuantity"`
	StockValueMinor         int64   `json:"stockValueMinor"`
	WasteValueMinor         int64   `json:"wasteValueMinor"`
	TheoreticalCostMinor    int64   `json:"theoreticalCostMinor"`
	CountVarianceValueMinor int64   `json:"countVarianceValueMinor"`
}

type InventoryCountLine struct {
	ID                   string  `json:"id"`
	IngredientID         string  `json:"ingredientId"`
	UnitID               string  `json:"unitId"`
	CountedQuantity      float64 `json:"countedQuantity"`
	CountedQuantityBase  float64 `json:"countedQuantityBase"`
	ExpectedQuantityBase float64 `json:"expectedQuantityBase"`
	VarianceQuantityBase float64 `json:"varianceQuantityBase"`
	VarianceCostMinor    int64   `json:"varianceCostMinor"`
}

type InventoryCount struct {
	ID          string               `json:"id"`
	TenantID    string               `json:"tenantId"`
	OutletID    string               `json:"outletId"`
	Notes       string               `json:"notes,omitempty"`
	CountedAt   time.Time            `json:"countedAt"`
	RecordedAt  time.Time            `json:"recordedAt"`
	ActorID     string               `json:"actorId"`
	DeviceID    string               `json:"deviceId"`
	OperationID string               `json:"operationId"`
	Lines       []InventoryCountLine `json:"lines"`
}

type ProductionBatchStatus string

const (
	ProductionBatchPlanned    ProductionBatchStatus = "planned"
	ProductionBatchInProgress ProductionBatchStatus = "in_progress"
	ProductionBatchCompleted  ProductionBatchStatus = "completed"
	ProductionBatchCancelled  ProductionBatchStatus = "cancelled"
)

type ProductionBatch struct {
	ID                 string                `json:"id"`
	TenantID           string                `json:"tenantId"`
	OutletID           string                `json:"outletId"`
	StationID          string                `json:"stationId,omitempty"`
	RecipeVersionID    string                `json:"recipeVersionId"`
	RecipeName         string                `json:"recipeName,omitempty"`
	OutputIngredientID string                `json:"outputIngredientId"`
	OutputIngredient   string                `json:"outputIngredient,omitempty"`
	OutputUnitID       string                `json:"outputUnitId"`
	OutputUnitSymbol   string                `json:"outputUnitSymbol,omitempty"`
	Status             ProductionBatchStatus `json:"status"`
	PlannedQuantity    float64               `json:"plannedQuantity"`
	ActualQuantity     *float64              `json:"actualQuantity,omitempty"`
	PlannedFor         time.Time             `json:"plannedFor"`
	StartedAt          *time.Time            `json:"startedAt,omitempty"`
	CompletedAt        *time.Time            `json:"completedAt,omitempty"`
	ExpiresAt          *time.Time            `json:"expiresAt,omitempty"`
	LotCode            string                `json:"lotCode,omitempty"`
	Notes              string                `json:"notes,omitempty"`
	RecordMetadata
}

type OrderImportRow struct {
	ID           string         `json:"id"`
	RowNumber    int            `json:"rowNumber"`
	ExternalRef  string         `json:"externalRef"`
	Status       string         `json:"status"`
	ErrorCode    string         `json:"errorCode,omitempty"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
	OrderID      string         `json:"orderId,omitempty"`
	RawData      map[string]any `json:"rawData"`
}

type OrderImport struct {
	ID           string           `json:"id"`
	TenantID     string           `json:"tenantId"`
	OutletID     string           `json:"outletId"`
	FileName     string           `json:"fileName"`
	FileSHA256   string           `json:"fileSha256"`
	TotalRows    int              `json:"totalRows"`
	AcceptedRows int              `json:"acceptedRows"`
	RejectedRows int              `json:"rejectedRows"`
	Status       string           `json:"status"`
	ImportedAt   time.Time        `json:"importedAt"`
	Rows         []OrderImportRow `json:"rows"`
}

// MenuImportDraft preserves the canonical result of a third-party menu import.
// Applied imports are immediately sellable; recipe and station mappings remain
// optional metadata that an operator can complete later in Menu Studio.
type MenuImportDraft struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenantId"`
	OutletID        string          `json:"outletId"`
	Name            string          `json:"name"`
	ItemFileName    string          `json:"itemFileName"`
	AddonFileName   string          `json:"addonFileName,omitempty"`
	SourceSHA256    string          `json:"sourceSha256"`
	Status          string          `json:"status"`
	ItemCount       int             `json:"itemCount"`
	CategoryCount   int             `json:"categoryCount"`
	AddonGroupCount int             `json:"addonGroupCount"`
	VariationCount  int             `json:"variationCount"`
	Draft           json.RawMessage `json:"draft"`
	ImportedAt      time.Time       `json:"importedAt"`
}

type PlanningRecommendation struct {
	ID                    string         `json:"id"`
	Type                  string         `json:"type"`
	MenuItemID            string         `json:"menuItemId,omitempty"`
	MenuItemName          string         `json:"menuItemName,omitempty"`
	RecipeVersionID       string         `json:"recipeVersionId,omitempty"`
	IngredientID          string         `json:"ingredientId,omitempty"`
	IngredientName        string         `json:"ingredientName,omitempty"`
	UnitSymbol            string         `json:"unitSymbol,omitempty"`
	ForecastQuantity      float64        `json:"forecastQuantity"`
	RequiredQuantityBase  float64        `json:"requiredQuantityBase"`
	AvailableQuantityBase float64        `json:"availableQuantityBase"`
	Confidence            float64        `json:"confidence"`
	Explanation           string         `json:"explanation"`
	Evidence              map[string]any `json:"evidence"`
}

type PlanningRun struct {
	ID              string                   `json:"id"`
	TenantID        string                   `json:"tenantId"`
	OutletID        string                   `json:"outletId"`
	HorizonStart    time.Time                `json:"horizonStart"`
	HorizonEnd      time.Time                `json:"horizonEnd"`
	ModelVersion    string                   `json:"modelVersion"`
	Status          string                   `json:"status"`
	EvidenceFrom    time.Time                `json:"evidenceFrom"`
	EvidenceTo      time.Time                `json:"evidenceTo"`
	GeneratedAt     time.Time                `json:"generatedAt"`
	Recommendations []PlanningRecommendation `json:"recommendations"`
}

type ConfigurationSnapshot struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenantId"`
	OutletID      string         `json:"outletId"`
	Sequence      uint64         `json:"sequence"`
	Content       map[string]any `json:"content"`
	ContentSHA256 string         `json:"contentSha256"`
	Signature     string         `json:"signature"`
	PublicKey     string         `json:"publicKey"`
	Algorithm     string         `json:"algorithm"`
	Status        string         `json:"status"`
	SignedAt      time.Time      `json:"signedAt"`
}
type EdgeCheckpoint struct {
	TenantID             string     `json:"tenantId"`
	EdgeID               string     `json:"edgeId"`
	OutletID             string     `json:"outletId"`
	LastOperationID      string     `json:"lastOperationId,omitempty"`
	LastReceivedAt       *time.Time `json:"lastReceivedAt,omitempty"`
	LastAcceptedAt       *time.Time `json:"lastAcceptedAt,omitempty"`
	LastSnapshotSequence uint64     `json:"lastSnapshotSequence"`
	BacklogCount         int        `json:"backlogCount"`
	Degraded             bool       `json:"degraded"`
	LastProblemCode      string     `json:"lastProblemCode,omitempty"`
	Version              uint64     `json:"version"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}
type ReconciliationAction struct {
	ID              string    `json:"id"`
	Action          string    `json:"action"`
	Notes           string    `json:"notes"`
	ActorID         string    `json:"actorId"`
	PreviousStatus  string    `json:"previousStatus"`
	ResultingStatus string    `json:"resultingStatus"`
	ExpectedVersion uint64    `json:"expectedVersion"`
	OccurredAt      time.Time `json:"occurredAt"`
}
type ReconciliationCase struct {
	ID         string                 `json:"id"`
	TenantID   string                 `json:"tenantId"`
	OutletID   string                 `json:"outletId"`
	SourceType string                 `json:"sourceType"`
	SourceID   string                 `json:"sourceId"`
	Category   string                 `json:"category"`
	Severity   string                 `json:"severity"`
	Status     string                 `json:"status"`
	Title      string                 `json:"title"`
	AssignedTo string                 `json:"assignedTo"`
	Resolution string                 `json:"resolution"`
	Details    map[string]any         `json:"details"`
	Version    uint64                 `json:"version"`
	OpenedAt   time.Time              `json:"openedAt"`
	UpdatedAt  time.Time              `json:"updatedAt"`
	ResolvedAt *time.Time             `json:"resolvedAt,omitempty"`
	Actions    []ReconciliationAction `json:"actions"`
}
type OperationalIncident struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenantId"`
	OutletID     string         `json:"outletId"`
	IncidentType string         `json:"incidentType"`
	Severity     string         `json:"severity"`
	Status       string         `json:"status"`
	Title        string         `json:"title"`
	Details      map[string]any `json:"details"`
	Version      uint64         `json:"version"`
	StartedAt    time.Time      `json:"startedAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
	ResolvedAt   *time.Time     `json:"resolvedAt,omitempty"`
}
type BackupManifest struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenantId"`
	OutletID         string    `json:"outletId,omitempty"`
	BackupType       string    `json:"backupType"`
	StorageReference string    `json:"storageReference"`
	ContentSHA256    string    `json:"contentSha256"`
	SizeBytes        int64     `json:"sizeBytes"`
	Encrypted        bool      `json:"encrypted"`
	Verified         bool      `json:"verified"`
	StartedAt        time.Time `json:"startedAt"`
	CompletedAt      time.Time `json:"completedAt"`
	RecoveryPointAt  time.Time `json:"recoveryPointAt"`
	RecordedAt       time.Time `json:"recordedAt"`
}
type RestoreDrill struct {
	ID                  string    `json:"id"`
	TenantID            string    `json:"tenantId"`
	BackupManifestID    string    `json:"backupManifestId"`
	Status              string    `json:"status"`
	TargetEnvironment   string    `json:"targetEnvironment"`
	Notes               string    `json:"notes"`
	StartedAt           time.Time `json:"startedAt"`
	CompletedAt         time.Time `json:"completedAt"`
	RecoveryTimeSeconds int       `json:"recoveryTimeSeconds"`
	IntegrityVerified   bool      `json:"integrityVerified"`
	RecordedAt          time.Time `json:"recordedAt"`
}

type Supplier struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	ContactName string `json:"contactName"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	TaxID       string `json:"taxId"`
	Active      bool   `json:"active"`
	RecordMetadata
}
type PurchaseOrderLine struct {
	ID               string  `json:"id"`
	IngredientID     string  `json:"ingredientId"`
	IngredientName   string  `json:"ingredientName"`
	UnitID           string  `json:"unitId"`
	UnitSymbol       string  `json:"unitSymbol"`
	OrderedQuantity  float64 `json:"orderedQuantity"`
	ReceivedQuantity float64 `json:"receivedQuantity"`
	UnitCostMinor    int64   `json:"unitCostMinor"`
}
type PurchaseOrder struct {
	ID           string              `json:"id"`
	TenantID     string              `json:"tenantId"`
	OutletID     string              `json:"outletId"`
	SupplierID   string              `json:"supplierId"`
	SupplierName string              `json:"supplierName"`
	PONumber     string              `json:"poNumber"`
	Status       string              `json:"status"`
	ExpectedAt   *time.Time          `json:"expectedAt,omitempty"`
	Currency     string              `json:"currency"`
	Notes        string              `json:"notes"`
	TotalMinor   int64               `json:"totalMinor"`
	Lines        []PurchaseOrderLine `json:"lines"`
	RecordMetadata
}
type GoodsReceiptLine struct {
	ID                  string     `json:"id"`
	PurchaseOrderLineID string     `json:"purchaseOrderLineId"`
	IngredientID        string     `json:"ingredientId"`
	UnitID              string     `json:"unitId"`
	Quantity            float64    `json:"quantity"`
	UnitCostMinor       int64      `json:"unitCostMinor"`
	LotCode             string     `json:"lotCode"`
	ExpiresAt           *time.Time `json:"expiresAt,omitempty"`
	InventoryEventID    string     `json:"inventoryEventId"`
}
type GoodsReceipt struct {
	ID               string             `json:"id"`
	TenantID         string             `json:"tenantId"`
	OutletID         string             `json:"outletId"`
	PurchaseOrderID  string             `json:"purchaseOrderId"`
	ReceivedAt       time.Time          `json:"receivedAt"`
	SupplierDocument string             `json:"supplierDocument"`
	Notes            string             `json:"notes"`
	Lines            []GoodsReceiptLine `json:"lines"`
}
type TemperatureLog struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenantId"`
	OutletID         string    `json:"outletId"`
	Location         string    `json:"location"`
	TemperatureC     float64   `json:"temperatureC"`
	SafeMinC         float64   `json:"safeMinC"`
	SafeMaxC         float64   `json:"safeMaxC"`
	Compliant        bool      `json:"compliant"`
	CorrectiveAction string    `json:"correctiveAction"`
	MeasuredAt       time.Time `json:"measuredAt"`
	ActorID          string    `json:"actorId"`
}
type ChecklistItem struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Required    bool       `json:"required"`
	Completed   bool       `json:"completed"`
	CompletedBy string     `json:"completedBy"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Position    int        `json:"position"`
}
type OperationalChecklist struct {
	ID            string          `json:"id"`
	TenantID      string          `json:"tenantId"`
	OutletID      string          `json:"outletId"`
	ChecklistType string          `json:"checklistType"`
	BusinessDate  string          `json:"businessDate"`
	Status        string          `json:"status"`
	Version       uint64          `json:"version"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
	CompletedAt   *time.Time      `json:"completedAt,omitempty"`
	Items         []ChecklistItem `json:"items"`
}
type StaffMember struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenantId"`
	EmployeeCode string `json:"employeeCode"`
	DisplayName  string `json:"displayName"`
	Role         string `json:"role"`
	Phone        string `json:"phone"`
	Active       bool   `json:"active"`
	RecordMetadata
}
type StaffShift struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenantId"`
	OutletID      string    `json:"outletId"`
	StaffMemberID string    `json:"staffMemberId"`
	StaffName     string    `json:"staffName"`
	StartsAt      time.Time `json:"startsAt"`
	EndsAt        time.Time `json:"endsAt"`
	StationID     string    `json:"stationId,omitempty"`
	Status        string    `json:"status"`
	RecordMetadata
}
type OperationalTask struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenantId"`
	OutletID      string     `json:"outletId"`
	StaffMemberID string     `json:"staffMemberId,omitempty"`
	StaffName     string     `json:"staffName,omitempty"`
	Title         string     `json:"title"`
	DueAt         *time.Time `json:"dueAt,omitempty"`
	Priority      string     `json:"priority"`
	Status        string     `json:"status"`
	RecordMetadata
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}
type MenuAvailability struct {
	MenuItemID   string    `json:"menuItemId"`
	MenuItemName string    `json:"menuItemName"`
	Available    bool      `json:"available"`
	Reason       string    `json:"reason"`
	Version      uint64    `json:"version"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
type DiningTable struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	OutletID  string    `json:"outletId"`
	Label     string    `json:"label"`
	Section   string    `json:"section"`
	Capacity  int       `json:"capacity"`
	Status    string    `json:"status"`
	Version   uint64    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type DiningSession struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenantId"`
	OutletID   string     `json:"outletId"`
	TableID    string     `json:"tableId"`
	TableLabel string     `json:"tableLabel"`
	Status     string     `json:"status"`
	GuestCount int        `json:"guestCount"`
	GuestName  string     `json:"guestName"`
	OpenedAt   time.Time  `json:"openedAt"`
	ClosedAt   *time.Time `json:"closedAt,omitempty"`
	Version    uint64     `json:"version"`
}
type CashShift struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"tenantId"`
	OutletID          string     `json:"outletId"`
	RegisterLabel     string     `json:"registerLabel"`
	Status            string     `json:"status"`
	OpeningFloatMinor int64      `json:"openingFloatMinor"`
	ExpectedCashMinor int64      `json:"expectedCashMinor"`
	ClosingCountMinor *int64     `json:"closingCountMinor,omitempty"`
	VarianceMinor     *int64     `json:"varianceMinor,omitempty"`
	OpenedAt          time.Time  `json:"openedAt"`
	ClosedAt          *time.Time `json:"closedAt,omitempty"`
	Version           uint64     `json:"version"`
}
type Tender struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"tenantId"`
	OutletID          string    `json:"outletId"`
	OrderID           string    `json:"orderId"`
	CashShiftID       string    `json:"cashShiftId,omitempty"`
	TenderType        string    `json:"tenderType"`
	AmountMinor       int64     `json:"amountMinor"`
	Currency          string    `json:"currency"`
	ProviderReference string    `json:"providerReference"`
	Status            string    `json:"status"`
	ReversesTenderID  string    `json:"reversesTenderId,omitempty"`
	OccurredAt        time.Time `json:"occurredAt"`
}
type FiscalReceipt struct {
	ID                 string    `json:"id"`
	OrderID            string    `json:"orderId"`
	ReceiptNumber      string    `json:"receiptNumber"`
	Currency           string    `json:"currency"`
	SubtotalMinor      int64     `json:"subtotalMinor"`
	DiscountMinor      int64     `json:"discountMinor"`
	TaxMinor           int64     `json:"taxMinor"`
	ServiceChargeMinor int64     `json:"serviceChargeMinor"`
	TotalMinor         int64     `json:"totalMinor"`
	IssuedAt           time.Time `json:"issuedAt"`
}
type TenderSettlement struct {
	ID               string    `json:"id"`
	BusinessDate     string    `json:"businessDate"`
	TenderType       string    `json:"tenderType"`
	GrossMinor       int64     `json:"grossMinor"`
	ReversedMinor    int64     `json:"reversedMinor"`
	NetMinor         int64     `json:"netMinor"`
	TransactionCount int       `json:"transactionCount"`
	GeneratedAt      time.Time `json:"generatedAt"`
}

// SalesChannel is a single publication target for the canonical menu. Orders
// from every channel still use the same order, ticket, inventory and tender ledgers.
type SalesChannel struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenantId"`
	OutletID      string         `json:"outletId"`
	Code          string         `json:"code"`
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	Active        bool           `json:"active"`
	Configuration map[string]any `json:"configuration"`
	RecordMetadata
}

// ConnectorInstallation carries no secrets; credential references are resolved
// only by the approved connector runtime.
type ConnectorInstallation struct {
	ID                  string         `json:"id"`
	TenantID            string         `json:"tenantId"`
	OutletID            string         `json:"outletId"`
	ChannelID           string         `json:"channelId,omitempty"`
	Provider            string         `json:"provider"`
	ManifestVersion     string         `json:"manifestVersion"`
	CredentialReference string         `json:"credentialReference,omitempty"`
	Capabilities        []string       `json:"capabilities"`
	Configuration       map[string]any `json:"configuration"`
	Status              string         `json:"status"`
	LastHealthAt        *time.Time     `json:"lastHealthAt,omitempty"`
	RecordMetadata
}

// ConnectorOrderInbox is immutable ingress evidence. A normalizer may create a
// canonical order from it, but must retain the original payload and its hash.
type ConnectorOrderInbox struct {
	ID                string         `json:"id"`
	TenantID          string         `json:"tenantId"`
	OutletID          string         `json:"outletId"`
	ConnectorID       string         `json:"connectorId"`
	ExternalOrderID   string         `json:"externalOrderId"`
	Payload           map[string]any `json:"payload"`
	PayloadSHA256     string         `json:"payloadSha256"`
	Status            string         `json:"status"`
	NormalizedOrderID string         `json:"normalizedOrderId,omitempty"`
	ReceivedAt        time.Time      `json:"receivedAt"`
	ResolvedAt        *time.Time     `json:"resolvedAt,omitempty"`
	ErrorCode         string         `json:"errorCode,omitempty"`
}

// ConnectorOrderDecision is immutable staff evidence for an external order.
// Provider payloads are never overwritten; the current inbox status is derived
// from the latest decision.
type ConnectorOrderDecision struct {
	ID                string    `json:"id"`
	InboxID           string    `json:"inboxId"`
	Decision          string    `json:"decision"`
	Reason            string    `json:"reason,omitempty"`
	NormalizedOrderID string    `json:"normalizedOrderId,omitempty"`
	OccurredAt        time.Time `json:"occurredAt"`
	ActorID           string    `json:"actorId"`
	DeviceID          string    `json:"deviceId"`
}

// MenuSellability exposes the explainable effective menu decision. It never
// overwrites a manager's availability override or inventory history.
type MenuSellability struct {
	MenuItemID      string `json:"menuItemId"`
	MenuItemName    string `json:"menuItemName"`
	ChannelID       string `json:"channelId,omitempty"`
	PriceMinor      int64  `json:"priceMinor"`
	Currency        string `json:"currency"`
	Available       bool   `json:"available"`
	ReasonCode      string `json:"reasonCode,omitempty"`
	Reason          string `json:"reason,omitempty"`
	StockReady      bool   `json:"stockReady"`
	CapacityReady   bool   `json:"capacityReady"`
	ManualAvailable bool   `json:"manualAvailable"`
	ActiveTickets   int    `json:"activeTickets"`
	CapacityLimit   int    `json:"capacityLimit"`
}

type StationCapacityLimit struct {
	StationID        string    `json:"stationId"`
	MaxActiveTickets int       `json:"maxActiveTickets"`
	Version          uint64    `json:"version"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type KitchenPrintJob struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenantId"`
	OutletID       string         `json:"outletId"`
	TicketID       string         `json:"ticketId"`
	PrinterRoute   string         `json:"printerRoute"`
	CopyType       string         `json:"copyType"`
	Payload        map[string]any `json:"payload"`
	Status         string         `json:"status"`
	Attempts       int            `json:"attempts"`
	LastError      string         `json:"lastError,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	AcknowledgedAt *time.Time     `json:"acknowledgedAt,omitempty"`
}

type PickupToken struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenantId"`
	OutletID    string     `json:"outletId"`
	OrderID     string     `json:"orderId"`
	Token       string     `json:"token"`
	Status      string     `json:"status"`
	IssuedAt    time.Time  `json:"issuedAt"`
	CollectedAt *time.Time `json:"collectedAt,omitempty"`
	Version     uint64     `json:"version"`
}

type QROrderingLink struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenantId"`
	OutletID  string     `json:"outletId"`
	ChannelID string     `json:"channelId,omitempty"`
	TableID   string     `json:"tableId,omitempty"`
	Slug      string     `json:"slug"`
	Active    bool       `json:"active"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	RecordMetadata
}

type GuestOrderRequestLine struct {
	MenuItemID string `json:"menuItemId"`
	Name       string `json:"name"`
	Quantity   int32  `json:"quantity"`
	UnitMinor  int64  `json:"unitMinor"`
	LineMinor  int64  `json:"lineMinor"`
}

// GuestOrderRequest is an idempotent QR/web order intent. A cashier or payment
// adapter must create the paid canonical order before it is sent to the KDS.
type GuestOrderRequest struct {
	ID              string                  `json:"id"`
	TenantID        string                  `json:"tenantId"`
	OutletID        string                  `json:"outletId"`
	QRLinkID        string                  `json:"qrLinkId"`
	ChannelID       string                  `json:"channelId,omitempty"`
	MenuVersionID   string                  `json:"menuVersionId"`
	TrackingCode    string                  `json:"trackingCode"`
	GuestName       string                  `json:"guestName,omitempty"`
	GuestPhone      string                  `json:"guestPhone,omitempty"`
	Note            string                  `json:"note,omitempty"`
	Lines           []GuestOrderRequestLine `json:"lines"`
	TotalMinor      int64                   `json:"totalMinor"`
	Currency        string                  `json:"currency"`
	PaymentState    string                  `json:"paymentState"`
	Status          string                  `json:"status"`
	ClientRequestID string                  `json:"clientRequestId"`
	SubmittedAt     time.Time               `json:"submittedAt"`
}

type StockTransferLine struct {
	ID                     string   `json:"id"`
	IngredientID           string   `json:"ingredientId"`
	QuantityBase           float64  `json:"quantityBase"`
	DispatchedQuantityBase *float64 `json:"dispatchedQuantityBase,omitempty"`
	ReceivedQuantityBase   *float64 `json:"receivedQuantityBase,omitempty"`
}

// StockTransferExecutionLine carries the physically packed or received base
// quantity. The requested line remains immutable evidence; actual quantities
// are recorded only when a transfer changes custody.
type StockTransferExecutionLine struct {
	IngredientID string  `json:"ingredientId"`
	QuantityBase float64 `json:"quantityBase"`
}

type StockTransfer struct {
	ID                  string              `json:"id"`
	TenantID            string              `json:"tenantId"`
	SourceOutletID      string              `json:"sourceOutletId"`
	DestinationOutletID string              `json:"destinationOutletId"`
	Status              string              `json:"status"`
	RequestedBy         string              `json:"requestedBy"`
	Notes               string              `json:"notes,omitempty"`
	RequestedAt         time.Time           `json:"requestedAt"`
	DispatchedAt        *time.Time          `json:"dispatchedAt,omitempty"`
	ReceivedAt          *time.Time          `json:"receivedAt,omitempty"`
	Lines               []StockTransferLine `json:"lines"`
	Version             uint64              `json:"version"`
}

// ReplenishmentRule is per ingredient and destination outlet. It provides a
// deterministic par-level policy, independent of whichever forecast model is
// temporarily available.
type ReplenishmentRule struct {
	OutletID         string    `json:"outletId"`
	IngredientID     string    `json:"ingredientId"`
	SourceOutletID   string    `json:"sourceOutletId"`
	ReorderPointBase float64   `json:"reorderPointBase"`
	TargetLevelBase  float64   `json:"targetLevelBase"`
	Active           bool      `json:"active"`
	Version          uint64    `json:"version"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type ReplenishmentSuggestion struct {
	OutletID              string  `json:"outletId"`
	IngredientID          string  `json:"ingredientId"`
	IngredientName        string  `json:"ingredientName"`
	UnitSymbol            string  `json:"unitSymbol"`
	SourceOutletID        string  `json:"sourceOutletId"`
	OnHandBase            float64 `json:"onHandBase"`
	ReorderPointBase      float64 `json:"reorderPointBase"`
	TargetLevelBase       float64 `json:"targetLevelBase"`
	SourceAvailableBase   float64 `json:"sourceAvailableBase"`
	SuggestedQuantityBase float64 `json:"suggestedQuantityBase"`
	Status                string  `json:"status"`
}

type OutletControlProfile struct {
	OutletID       string         `json:"outletId"`
	ProfileName    string         `json:"profileName"`
	ApprovalPolicy map[string]any `json:"approvalPolicy"`
	FeatureProfile map[string]any `json:"featureProfile"`
	Version        uint64         `json:"version"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type HardwareDevice struct {
	ID                  string     `json:"id"`
	TenantID            string     `json:"tenantId"`
	OutletID            string     `json:"outletId"`
	DeviceType          string     `json:"deviceType"`
	Manufacturer        string     `json:"manufacturer"`
	Model               string     `json:"model"`
	SerialNumber        string     `json:"serialNumber"`
	CertificationStatus string     `json:"certificationStatus"`
	GatewayReference    string     `json:"gatewayReference,omitempty"`
	LastSeenAt          *time.Time `json:"lastSeenAt,omitempty"`
	RecordMetadata
}

type ImplementationRunbook struct {
	ID           string           `json:"id"`
	TenantID     string           `json:"tenantId"`
	OutletID     string           `json:"outletId"`
	TemplateCode string           `json:"templateCode"`
	Status       string           `json:"status"`
	Checklist    []map[string]any `json:"checklist"`
	Owner        string           `json:"owner"`
	DueAt        *time.Time       `json:"dueAt,omitempty"`
	RecordMetadata
}

type GSTReport struct {
	BusinessDate string `json:"businessDate"`
	Currency     string `json:"currency"`
	InvoiceCount int    `json:"invoiceCount"`
	TaxableMinor int64  `json:"taxableMinor"`
	GSTMinor     int64  `json:"gstMinor"`
	GrossMinor   int64  `json:"grossMinor"`
}

type DayEndReport struct {
	BusinessDate       string             `json:"businessDate"`
	Currency           string             `json:"currency"`
	ReceiptCount       int                `json:"receiptCount"`
	GrossMinor         int64              `json:"grossMinor"`
	DiscountMinor      int64              `json:"discountMinor"`
	TaxMinor           int64              `json:"taxMinor"`
	ServiceChargeMinor int64              `json:"serviceChargeMinor"`
	Settlements        []TenderSettlement `json:"settlements"`
}

// MenuStudio is the versioned definition of a sellable menu. Recipes and menu
// items remain canonical; a studio controls presentation, modifiers, prices,
// and channel publication without rewriting historical orders.
type MenuStudio struct {
	ID               string             `json:"id"`
	TenantID         string             `json:"tenantId"`
	OutletID         string             `json:"outletId"`
	Name             string             `json:"name"`
	Status           string             `json:"status"`
	CurrentVersionID string             `json:"currentVersionId,omitempty"`
	Version          uint64             `json:"version"`
	CreatedAt        time.Time          `json:"createdAt"`
	UpdatedAt        time.Time          `json:"updatedAt"`
	Current          *MenuStudioVersion `json:"current,omitempty"`
}

type MenuStudioCategory struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
	Active    bool   `json:"active"`
}
type MenuModifierOption struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PriceDeltaMinor int64  `json:"priceDeltaMinor"`
	Active          bool   `json:"active"`
	SortOrder       int    `json:"sortOrder"`
}
type MenuModifierGroup struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	SelectionMin int                  `json:"selectionMin"`
	SelectionMax int                  `json:"selectionMax"`
	Required     bool                 `json:"required"`
	SortOrder    int                  `json:"sortOrder"`
	Options      []MenuModifierOption `json:"options"`
}
type MenuStudioItem struct {
	MenuItemID       string   `json:"menuItemId"`
	CategoryID       string   `json:"categoryId,omitempty"`
	DisplayName      string   `json:"displayName"`
	Description      string   `json:"description,omitempty"`
	SortOrder        int      `json:"sortOrder"`
	Active           bool     `json:"active"`
	ModifierGroupIDs []string `json:"modifierGroupIds"`
	PriceID          string   `json:"priceId"`
	PriceMinor       int64    `json:"priceMinor"`
	Currency         string   `json:"currency"`
}
type MenuPublication struct {
	ID            string     `json:"id"`
	ChannelID     string     `json:"channelId,omitempty"`
	Status        string     `json:"status"`
	EffectiveFrom time.Time  `json:"effectiveFrom"`
	EffectiveTo   *time.Time `json:"effectiveTo,omitempty"`
}
type MenuStudioVersion struct {
	ID            string               `json:"id"`
	MenuStudioID  string               `json:"menuStudioId"`
	VersionNumber int                  `json:"versionNumber"`
	Status        string               `json:"status"`
	EffectiveFrom time.Time            `json:"effectiveFrom"`
	CreatedAt     time.Time            `json:"createdAt"`
	PublishedAt   *time.Time           `json:"publishedAt,omitempty"`
	PublishedBy   string               `json:"publishedBy,omitempty"`
	Categories    []MenuStudioCategory `json:"categories"`
	Modifiers     []MenuModifierGroup  `json:"modifiers"`
	Items         []MenuStudioItem     `json:"items"`
	Publications  []MenuPublication    `json:"publications"`
}

// POSCheckoutLine accepts only canonical identifiers. The server resolves the
// display text, current price, modifiers, tax totals, and kitchen station.
type POSCheckoutLine struct {
	ID                string   `json:"id"`
	MenuItemID        string   `json:"menuItemId"`
	Quantity          int32    `json:"quantity"`
	PreparationNote   string   `json:"preparationNote,omitempty"`
	ModifierOptionIDs []string `json:"modifierOptionIds,omitempty"`
}
type POSCheckoutTender struct {
	ID                string `json:"id"`
	TenderType        string `json:"tenderType"`
	CashShiftID       string `json:"cashShiftId,omitempty"`
	AmountMinor       int64  `json:"amountMinor"`
	ProviderReference string `json:"providerReference,omitempty"`
}
type POSCheckout struct {
	ID                 string              `json:"id"`
	TenantID           string              `json:"tenantId"`
	OutletID           string              `json:"outletId"`
	MenuVersionID      string              `json:"menuVersionId,omitempty"`
	OrderID            string              `json:"orderId"`
	OrderType          OrderType           `json:"orderType"`
	BrandID            string              `json:"brandId,omitempty"`
	ExternalRef        string              `json:"externalRef,omitempty"`
	DiscountMinor      int64               `json:"discountMinor"`
	TaxMinor           int64               `json:"taxMinor"`
	ServiceChargeMinor int64               `json:"serviceChargeMinor"`
	Lines              []POSCheckoutLine   `json:"lines"`
	Tenders            []POSCheckoutTender `json:"tenders"`
	ReceiptID          string              `json:"receiptId"`
	ReceiptNumber      string              `json:"receiptNumber"`
	PickupTokenID      string              `json:"pickupTokenId,omitempty"`
	PickupToken        string              `json:"pickupToken,omitempty"`
	PrinterRoute       string              `json:"printerRoute"`
	PlacedAt           time.Time           `json:"placedAt"`
}
type POSCheckoutResult struct {
	Order       Order             `json:"order"`
	Tickets     []KitchenTicket   `json:"tickets"`
	PrintJobs   []KitchenPrintJob `json:"printJobs"`
	PickupToken *PickupToken      `json:"pickupToken,omitempty"`
	Tenders     []Tender          `json:"tenders"`
	Receipt     *FiscalReceipt    `json:"receipt,omitempty"`
}

type GuestProfile struct {
	ID               string     `json:"id"`
	TenantID         string     `json:"tenantId"`
	FullName         string     `json:"fullName"`
	Phone            string     `json:"phone"`
	Email            string     `json:"email"`
	Locale           string     `json:"locale"`
	DietaryLabels    []string   `json:"dietaryLabels"`
	Notes            string     `json:"notes"`
	MarketingConsent bool       `json:"marketingConsent"`
	ConsentUpdatedAt *time.Time `json:"consentUpdatedAt,omitempty"`
	Version          uint64     `json:"version"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}
type Reservation struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenantId"`
	OutletID        string    `json:"outletId"`
	GuestID         string    `json:"guestId,omitempty"`
	GuestName       string    `json:"guestName"`
	Phone           string    `json:"phone"`
	PartySize       int       `json:"partySize"`
	ScheduledFor    time.Time `json:"scheduledFor"`
	DurationMinutes int       `json:"durationMinutes"`
	Status          string    `json:"status"`
	Source          string    `json:"source"`
	Notes           string    `json:"notes"`
	Version         uint64    `json:"version"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
type Promotion struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenantId"`
	OutletID        string    `json:"outletId"`
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	DiscountType    string    `json:"discountType"`
	DiscountValue   int64     `json:"discountValue"`
	MinOrderMinor   int64     `json:"minOrderMinor"`
	StartsAt        time.Time `json:"startsAt"`
	EndsAt          time.Time `json:"endsAt"`
	RedemptionLimit *int      `json:"redemptionLimit,omitempty"`
	RedemptionCount int       `json:"redemptionCount"`
	Active          bool      `json:"active"`
	Version         uint64    `json:"version"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
type PromotionRedemption struct {
	ID            string    `json:"id"`
	PromotionID   string    `json:"promotionId"`
	GuestID       string    `json:"guestId,omitempty"`
	OrderID       string    `json:"orderId,omitempty"`
	BasketMinor   int64     `json:"basketMinor"`
	DiscountMinor int64     `json:"discountMinor"`
	OccurredAt    time.Time `json:"occurredAt"`
}
type LoyaltyAccount struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenantId"`
	GuestID        string    `json:"guestId"`
	GuestName      string    `json:"guestName"`
	PointsBalance  int64     `json:"pointsBalance"`
	LifetimeEarned int64     `json:"lifetimeEarned"`
	Version        uint64    `json:"version"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
type LoyaltyEvent struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"accountId"`
	EventType   string    `json:"eventType"`
	PointsDelta int64     `json:"pointsDelta"`
	Reason      string    `json:"reason"`
	OrderID     string    `json:"orderId,omitempty"`
	OccurredAt  time.Time `json:"occurredAt"`
}
