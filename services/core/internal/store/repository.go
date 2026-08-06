// SPDX-License-Identifier: AGPL-3.0-only

// Package store defines persistence ports plus PostgreSQL and in-memory adapters.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/auth"
	"github.com/feastcloud/feastcloud/services/core/internal/domain"
)

var (
	// ErrNotFound is returned when a tenant-scoped entity does not exist.
	ErrNotFound = errors.New("entity not found")
	// ErrConflict is returned when an identifier already exists.
	ErrConflict = errors.New("entity already exists")
	// ErrInvalidReference is returned when a relationship crosses a tenant or outlet boundary.
	ErrInvalidReference = errors.New("invalid entity reference")
	// ErrSyncUnavailable means no durable cloud transaction can currently be committed.
	ErrSyncUnavailable = errors.New("durable sync repository unavailable")
	// ErrVersionConflict means the caller's expected aggregate version is stale.
	ErrVersionConflict = errors.New("aggregate version conflict")
	// ErrInvalidTransition means the requested lifecycle move is not allowed.
	ErrInvalidTransition = errors.New("invalid status transition")
	// ErrCausalPredecessor means a later offline operation arrived before its required predecessor.
	ErrCausalPredecessor = errors.New("causal predecessor is missing")
	// ErrPlatformProvisioningUnavailable means the restricted operational DB
	// role has not been paired with a dedicated platform provisioning writer.
	ErrPlatformProvisioningUnavailable = errors.New("platform provisioning writer is unavailable")
)

// SyncOutcome is terminal only after the durable inbox transaction commits.
type SyncOutcome string

const (
	SyncAccepted  SyncOutcome = "ACCEPTED"
	SyncDuplicate SyncOutcome = "DUPLICATE"
	SyncRejected  SyncOutcome = "REJECTED"
	SyncConflict  SyncOutcome = "CONFLICT"
)

// SyncOperation is the persistence-neutral edge operation accepted by core.
type SyncOperation struct {
	TenantID         string
	OperationID      string
	EdgeID           string
	OutletID         string
	BatchID          string
	AggregateType    string
	AggregateID      string
	AggregateVersion uint64
	CommandType      string
	RequestHash      []byte
	Mutation         json.RawMessage
	RecordedAt       time.Time
	ReceivedAt       time.Time
}

// SyncRepository atomically records transport evidence and its domain effect.
// Implementations must never return Accepted before both writes are committed.
type SyncRepository interface {
	ApplySyncOperation(context.Context, SyncOperation) (SyncOutcome, string, error)
	Ready(context.Context) error
}

type unavailableSyncRepository struct{}

// NewUnavailableSyncRepository keeps edge operations pending in installations
// that intentionally run core without PostgreSQL.
func NewUnavailableSyncRepository() SyncRepository { return unavailableSyncRepository{} }

func (unavailableSyncRepository) ApplySyncOperation(context.Context, SyncOperation) (SyncOutcome, string, error) {
	return "", "", ErrSyncUnavailable
}

func (unavailableSyncRepository) Ready(context.Context) error { return ErrSyncUnavailable }

// AuditFilter constrains append-only audit-event queries.
type AuditFilter struct {
	TenantID   string
	OutletID   string
	EntityType string
	EntityID   string
}

// PageCursor is a stable keyset position independent of row retention.
type PageCursor struct {
	CreatedAt time.Time
	ID        string
}
type OrderPageRequest struct {
	TenantID, OutletID string
	Limit              int
	After              *PageCursor
}
type TicketPageRequest struct {
	TenantID, OutletID, OrderID, StationID string
	Limit                                  int
	After                                  *PageCursor
}
type OrderPage struct {
	Values []domain.Order
	Next   *PageCursor
}
type TicketPage struct {
	Values []domain.KitchenTicket
	Next   *PageCursor
}

// OperationalPager avoids loading an outlet's complete shift into memory.
type OperationalPager interface {
	PageOrders(context.Context, OrderPageRequest) (OrderPage, error)
	PageKitchenTickets(context.Context, TicketPageRequest) (TicketPage, error)
}

// DailyDashboardRequest identifies an immutable read cutoff for one outlet's
// local business date. AsOf is supplied by the transport so the response can
// disclose and consistently enforce its observation time.
type DailyDashboardRequest struct {
	TenantID     string
	OutletID     string
	BusinessDate string
	AsOf         time.Time
}

// DailyDashboardRepository exposes the PostgreSQL-backed operational read
// projection without making the infrastructure-free repository invent facts.
type DailyDashboardRepository interface {
	DailyDashboard(context.Context, DailyDashboardRequest) (domain.DailyDashboard, error)
}

type DeviceAdministration interface {
	RegisterDevice(context.Context, auth.Device, string, domain.AuditEvent) error
	RevokeDevice(context.Context, string, string, string, domain.AuditEvent) error
}

type StockMovement struct {
	Event    domain.InventoryEvent
	Quantity float64
	UnitID   string
}

type StockCountLine struct {
	ID, IngredientID, UnitID string
	CountedQuantity          float64
}

type KitchenGraphRepository interface {
	CreateUnit(context.Context, domain.Unit, domain.AuditEvent) error
	Units(context.Context, string) ([]domain.Unit, error)
	CreateIngredient(context.Context, domain.Ingredient, domain.AuditEvent) error
	Ingredients(context.Context, string) ([]domain.Ingredient, error)
	CreateRecipe(context.Context, domain.Recipe, domain.RecipeVersion, domain.AuditEvent) error
	AddRecipeVersion(context.Context, domain.RecipeVersion, domain.AuditEvent) error
	Recipes(context.Context, string) ([]domain.Recipe, error)
	CreateMenuItem(context.Context, domain.MenuItem, domain.AuditEvent) error
	MenuItems(context.Context, string, string) ([]domain.MenuItem, error)
	RecordInventoryEvent(context.Context, StockMovement, domain.AuditEvent) (domain.InventoryEvent, error)
	RecordInventoryCount(context.Context, domain.InventoryCount, []StockCountLine, domain.AuditEvent) (domain.InventoryCount, error)
	InventorySummary(context.Context, string, string) ([]domain.InventorySummary, error)
}

type ProductionRepository interface {
	CreateProductionBatch(context.Context, domain.ProductionBatch, domain.AuditEvent) error
	ProductionBatches(context.Context, string, string) ([]domain.ProductionBatch, error)
	TransitionProductionBatch(context.Context, string, string, string, domain.ProductionBatchStatus, uint64, *float64, *time.Time, string, string, domain.AuditEvent) (domain.ProductionBatch, error)
}

type ImportedOrderRow struct {
	ID, ExternalRef, OrderType, ItemCode string
	ErrorCode, ErrorMessage              string
	RowNumber                            int
	Quantity                             int32
	PlacedAt                             time.Time
	RawData                              map[string]any
}

type IntelligenceRepository interface {
	ImportOrders(context.Context, domain.OrderImport, []ImportedOrderRow, domain.AuditEvent) (domain.OrderImport, error)
	OrderImports(context.Context, string, string) ([]domain.OrderImport, error)
	CreateMenuImportDraft(context.Context, domain.MenuImportDraft, domain.AuditEvent) (domain.MenuImportDraft, error)
	MenuImportDrafts(context.Context, string, string) ([]domain.MenuImportDraft, error)
	GeneratePlanningRun(context.Context, domain.PlanningRun, domain.AuditEvent) (domain.PlanningRun, error)
	PlanningRuns(context.Context, string, string) ([]domain.PlanningRun, error)
}

type OperationalControlRepository interface {
	PublishSnapshot(context.Context, domain.ConfigurationSnapshot, domain.AuditEvent) (domain.ConfigurationSnapshot, error)
	Snapshots(context.Context, string, string) ([]domain.ConfigurationSnapshot, error)
	EdgeCheckpoints(context.Context, string, string) ([]domain.EdgeCheckpoint, error)
	ReconciliationCases(context.Context, string, string) ([]domain.ReconciliationCase, error)
	ActOnReconciliationCase(context.Context, string, string, string, domain.ReconciliationAction, string, domain.AuditEvent) (domain.ReconciliationCase, error)
	CreateIncident(context.Context, domain.OperationalIncident, domain.AuditEvent) error
	Incidents(context.Context, string, string) ([]domain.OperationalIncident, error)
	TransitionIncident(context.Context, string, string, string, string, uint64, string, domain.AuditEvent) (domain.OperationalIncident, error)
	RecordBackupManifest(context.Context, domain.BackupManifest, domain.AuditEvent) error
	RecordRestoreDrill(context.Context, domain.RestoreDrill, domain.AuditEvent) error
	BackupEvidence(context.Context, string) ([]domain.BackupManifest, []domain.RestoreDrill, error)
}

type DailyOperationsRepository interface {
	CreateSupplier(context.Context, domain.Supplier, domain.AuditEvent) error
	Suppliers(context.Context, string) ([]domain.Supplier, error)
	CreatePurchaseOrder(context.Context, domain.PurchaseOrder, domain.AuditEvent) error
	PurchaseOrders(context.Context, string, string) ([]domain.PurchaseOrder, error)
	TransitionPurchaseOrder(context.Context, string, string, string, string, uint64, domain.AuditEvent) (domain.PurchaseOrder, error)
	ReceivePurchaseOrder(context.Context, domain.GoodsReceipt, uint64, domain.AuditEvent) (domain.PurchaseOrder, error)
	RecordTemperature(context.Context, domain.TemperatureLog, domain.AuditEvent) error
	TemperatureLogs(context.Context, string, string) ([]domain.TemperatureLog, error)
	CreateChecklist(context.Context, domain.OperationalChecklist, domain.AuditEvent) error
	Checklists(context.Context, string, string) ([]domain.OperationalChecklist, error)
	CompleteChecklistItem(context.Context, string, string, string, string, uint64, domain.AuditEvent) (domain.OperationalChecklist, error)
	CreateStaffMember(context.Context, domain.StaffMember, domain.AuditEvent) error
	StaffMembers(context.Context, string) ([]domain.StaffMember, error)
	CreateShift(context.Context, domain.StaffShift, domain.AuditEvent) error
	Shifts(context.Context, string, string) ([]domain.StaffShift, error)
	TransitionShift(context.Context, string, string, string, string, uint64, domain.AuditEvent) (domain.StaffShift, error)
	CreateTask(context.Context, domain.OperationalTask, domain.AuditEvent) error
	Tasks(context.Context, string, string) ([]domain.OperationalTask, error)
	TransitionTask(context.Context, string, string, string, string, uint64, domain.AuditEvent) (domain.OperationalTask, error)
}
type CommerceRepository interface {
	SetMenuAvailability(context.Context, string, string, domain.MenuAvailability, domain.AuditEvent) (domain.MenuAvailability, error)
	MenuAvailability(context.Context, string, string) ([]domain.MenuAvailability, error)
	CreateDiningTable(context.Context, domain.DiningTable, domain.AuditEvent) error
	DiningTables(context.Context, string, string) ([]domain.DiningTable, error)
	TransitionDiningTable(context.Context, string, string, string, string, uint64, domain.AuditEvent) (domain.DiningTable, error)
	OpenDiningSession(context.Context, domain.DiningSession, domain.AuditEvent) (domain.DiningSession, error)
	DiningSessions(context.Context, string, string) ([]domain.DiningSession, error)
	CloseDiningSession(context.Context, string, string, string, uint64, domain.AuditEvent) (domain.DiningSession, error)
	OpenCashShift(context.Context, domain.CashShift, domain.AuditEvent) error
	CashShifts(context.Context, string, string) ([]domain.CashShift, error)
	CloseCashShift(context.Context, string, string, string, uint64, int64, domain.AuditEvent) (domain.CashShift, error)
	CaptureTender(context.Context, domain.Tender, domain.FiscalReceipt, domain.AuditEvent) (domain.Tender, *domain.FiscalReceipt, error)
	Tenders(context.Context, string, string) ([]domain.Tender, error)
	ReverseTender(context.Context, domain.Tender, domain.AuditEvent) (domain.Tender, error)
	GenerateSettlements(context.Context, string, string, string, domain.AuditEvent) ([]domain.TenderSettlement, error)
}

// ConnectedCommerceRepository is the durable channel and fulfilment boundary.
// It deliberately sits beside the immutable orders, tickets and stock ledgers:
// a POS, QR page or aggregator may never invent a second source of truth.
type ConnectedCommerceRepository interface {
	CreateSalesChannel(context.Context, domain.SalesChannel, domain.AuditEvent) error
	SalesChannels(context.Context, string, string) ([]domain.SalesChannel, error)
	CreateConnectorInstallation(context.Context, domain.ConnectorInstallation, domain.AuditEvent) error
	ConnectorInstallations(context.Context, string, string) ([]domain.ConnectorInstallation, error)
	IngestConnectorOrder(context.Context, domain.ConnectorOrderInbox, domain.AuditEvent) error
	ConnectorOrderInbox(context.Context, string, string) ([]domain.ConnectorOrderInbox, error)
	DecideConnectorOrder(context.Context, string, string, domain.ConnectorOrderDecision, domain.AuditEvent) (domain.ConnectorOrderInbox, error)
	MenuSellability(context.Context, string, string, string) ([]domain.MenuSellability, error)
	SetStationCapacity(context.Context, string, string, domain.StationCapacityLimit, domain.AuditEvent) (domain.StationCapacityLimit, error)
	StationCapacityLimits(context.Context, string, string) ([]domain.StationCapacityLimit, error)
	CreateKitchenPrintJob(context.Context, domain.KitchenPrintJob, domain.AuditEvent) error
	KitchenPrintJobs(context.Context, string, string) ([]domain.KitchenPrintJob, error)
	IssuePickupToken(context.Context, domain.PickupToken, domain.AuditEvent) (domain.PickupToken, error)
	PickupTokens(context.Context, string, string) ([]domain.PickupToken, error)
	CreateQROrderingLink(context.Context, domain.QROrderingLink, domain.AuditEvent) error
	QROrderingLinks(context.Context, string, string) ([]domain.QROrderingLink, error)
	CreateStockTransfer(context.Context, domain.StockTransfer, domain.AuditEvent) error
	StockTransfers(context.Context, string, string) ([]domain.StockTransfer, error)
	TransitionStockTransfer(context.Context, string, string, string, string, uint64, []domain.StockTransferExecutionLine, domain.AuditEvent) (domain.StockTransfer, error)
	SetReplenishmentRule(context.Context, string, string, domain.ReplenishmentRule, domain.AuditEvent) (domain.ReplenishmentRule, error)
	ReplenishmentRules(context.Context, string, string) ([]domain.ReplenishmentRule, error)
	ReplenishmentSuggestions(context.Context, string, string) ([]domain.ReplenishmentSuggestion, error)
	SetOutletControlProfile(context.Context, string, string, domain.OutletControlProfile, domain.AuditEvent) (domain.OutletControlProfile, error)
	OutletControlProfile(context.Context, string, string) (domain.OutletControlProfile, error)
	RegisterHardwareDevice(context.Context, domain.HardwareDevice, domain.AuditEvent) error
	HardwareDevices(context.Context, string, string) ([]domain.HardwareDevice, error)
	CreateImplementationRunbook(context.Context, domain.ImplementationRunbook, domain.AuditEvent) error
	ImplementationRunbooks(context.Context, string, string) ([]domain.ImplementationRunbook, error)
	GSTReport(context.Context, string, string, string) (domain.GSTReport, error)
	DayEndReport(context.Context, string, string, string) (domain.DayEndReport, error)
}

// RestaurantCoreRepository owns the versioned Menu Studio and the atomic
// counter checkout. The checkout is intentionally a single transaction: no
// tender, KOT, token, or receipt can survive without its canonical order.
type RestaurantCoreRepository interface {
	CreateMenuStudio(context.Context, domain.MenuStudio, domain.MenuStudioVersion, domain.AuditEvent) (domain.MenuStudio, error)
	AddMenuStudioVersion(context.Context, string, string, domain.MenuStudioVersion, uint64, domain.AuditEvent) (domain.MenuStudio, error)
	MenuStudios(context.Context, string, string) ([]domain.MenuStudio, error)
	LiveMenuStudio(context.Context, string, string, string, time.Time) (domain.MenuStudio, error)
	CheckoutPOS(context.Context, domain.POSCheckout, domain.AuditEvent) (domain.POSCheckoutResult, error)
	AcknowledgeKitchenPrintJob(context.Context, string, string, string, string, domain.AuditEvent) (domain.KitchenPrintJob, error)
	TransitionPickupToken(context.Context, string, string, string, string, uint64, domain.AuditEvent) (domain.PickupToken, error)
}

// DirectOrderingRepository persists guest QR/web requests only after resolving
// prices from a published menu version inside the tenant transaction.
type DirectOrderingRepository interface {
	SubmitGuestOrderRequest(context.Context, domain.GuestOrderRequest) (domain.GuestOrderRequest, error)
	GuestOrderRequests(context.Context, string, string) ([]domain.GuestOrderRequest, error)
}
type GuestGrowthRepository interface {
	CreateGuest(context.Context, domain.GuestProfile, domain.AuditEvent) error
	Guests(context.Context, string) ([]domain.GuestProfile, error)
	SetGuestConsent(context.Context, string, string, bool, uint64, string, domain.AuditEvent) (domain.GuestProfile, error)
	CreateReservation(context.Context, domain.Reservation, domain.AuditEvent) error
	Reservations(context.Context, string, string) ([]domain.Reservation, error)
	TransitionReservation(context.Context, string, string, string, string, uint64, domain.AuditEvent) (domain.Reservation, error)
	CreatePromotion(context.Context, domain.Promotion, domain.AuditEvent) error
	Promotions(context.Context, string, string) ([]domain.Promotion, error)
	RedeemPromotion(context.Context, string, string, string, domain.PromotionRedemption, domain.AuditEvent) (domain.PromotionRedemption, error)
	LoyaltyAccounts(context.Context, string) ([]domain.LoyaltyAccount, error)
	AdjustLoyalty(context.Context, string, string, string, uint64, domain.LoyaltyEvent, domain.AuditEvent) (domain.LoyaltyAccount, error)
}

// Repository is the persistence boundary used by the HTTP application.
// Implementations can change without changing the domain or transport packages.
type Repository interface {
	CreateOrganization(context.Context, domain.Organization, domain.AuditEvent) error
	Organization(context.Context, string, string) (domain.Organization, error)
	Organizations(context.Context, string) ([]domain.Organization, error)

	CreateOutlet(context.Context, domain.Outlet, domain.AuditEvent) error
	Outlet(context.Context, string, string) (domain.Outlet, error)
	Outlets(context.Context, string) ([]domain.Outlet, error)

	CreateBrand(context.Context, domain.Brand, domain.AuditEvent) error
	Brand(context.Context, string, string) (domain.Brand, error)
	Brands(context.Context, string) ([]domain.Brand, error)
	SetBrandOutletAssignment(context.Context, domain.BrandOutletAssignment, uint64, domain.AuditEvent) (domain.BrandOutletAssignment, error)
	BrandOutletAssignments(context.Context, string) ([]domain.BrandOutletAssignment, error)

	CreateStation(context.Context, domain.Station, domain.AuditEvent) error
	Station(context.Context, string, string) (domain.Station, error)
	Stations(context.Context, string) ([]domain.Station, error)

	CreateOrder(context.Context, domain.Order, domain.AuditEvent) error
	Order(context.Context, string, string) (domain.Order, error)
	Orders(context.Context, string) ([]domain.Order, error)
	TransitionOrder(context.Context, string, string, string, domain.OrderStatus, uint64, domain.AuditEvent) (domain.Order, error)

	CreateKitchenTicket(context.Context, domain.KitchenTicket, domain.AuditEvent) error
	KitchenTicket(context.Context, string, string) (domain.KitchenTicket, error)
	KitchenTickets(context.Context, string) ([]domain.KitchenTicket, error)
	TransitionKitchenTicket(context.Context, string, string, string, domain.TicketStatus, uint64, domain.AuditEvent) (domain.KitchenTicket, error)

	AuditEvents(context.Context, AuditFilter) ([]domain.AuditEvent, error)
}

// PlatformProvisioner creates a new customer tenancy atomically. It is a
// narrow control-plane port, intentionally unavailable to restaurant roles.
type PlatformProvisioner interface {
	ProvisionTenant(context.Context, domain.Organization, domain.Outlet, domain.Brand, domain.BrandOutletAssignment, []domain.Station, domain.AuditEvent) error
}
