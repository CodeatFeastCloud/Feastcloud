// SPDX-License-Identifier: AGPL-3.0-only

package domain

import "time"

// DailyDashboard is a tenant- and outlet-scoped read projection for one outlet
// business date. Monetary values are exact minor units in Currency.
type DailyDashboard struct {
	OutletID          string                  `json:"outletId"`
	BusinessDate      string                  `json:"businessDate"`
	Currency          string                  `json:"currency"`
	TimeZone          string                  `json:"timeZone"`
	Period            DashboardPeriod         `json:"period"`
	AsOf              time.Time               `json:"asOf"`
	Sales             DashboardSales          `json:"sales"`
	PaymentFlow       DashboardPaymentFlow    `json:"paymentFlow"`
	Orders            DashboardOrders         `json:"orders"`
	TenderMix         []DashboardTenderMix    `json:"tenderMix"`
	FulfillmentMix    []DashboardOrderTypeMix `json:"fulfillmentMix"`
	Hourly            []DashboardHourly       `json:"hourly"`
	Leakage           DashboardLeakage        `json:"leakage"`
	TopItems          []DashboardTopItem      `json:"topItems"`
	DataQuality       DashboardDataQuality    `json:"dataQuality"`
	Provenance        DashboardProvenance     `json:"provenance"`
	UnavailableFields []string                `json:"unavailableFields"`
}

type DashboardPeriod struct {
	StartsAt     time.Time `json:"startsAt"`
	EndsAt       time.Time `json:"endsAt"`
	BoundaryKind string    `json:"boundaryKind"`
}

// DashboardSales uses issued fiscal receipts as sales evidence. Tender
// reversals are deliberately reported in PaymentFlow rather than subtracted
// from this receipt cohort.
type DashboardSales struct {
	ReceiptedOrderCount int64 `json:"receiptedOrderCount"`
	SubtotalMinor       int64 `json:"subtotalMinor"`
	DiscountMinor       int64 `json:"discountMinor"`
	TaxMinor            int64 `json:"taxMinor"`
	ServiceChargeMinor  int64 `json:"serviceChargeMinor"`
	TotalMinor          int64 `json:"totalMinor"`
}

// DashboardPaymentFlow is event-time tender movement. It must not be treated
// as the net value of the orders or receipts originating on this date.
type DashboardPaymentFlow struct {
	CapturedCount int64 `json:"capturedCount"`
	CapturedMinor int64 `json:"capturedMinor"`
	RefundCount   int64 `json:"refundCount"`
	RefundMinor   int64 `json:"refundMinor"`
	NetMinor      int64 `json:"netMinor"`
}

type DashboardOrders struct {
	Total                  int64  `json:"total"`
	Included               int64  `json:"included"`
	Completed              int64  `json:"completed"`
	Cancelled              int64  `json:"cancelled"`
	Active                 int64  `json:"active"`
	Unpriced               int64  `json:"unpriced"`
	SubtotalMinor          int64  `json:"subtotalMinor"`
	DiscountMinor          int64  `json:"discountMinor"`
	TaxMinor               int64  `json:"taxMinor"`
	ServiceChargeMinor     int64  `json:"serviceChargeMinor"`
	OrderValueMinor        int64  `json:"orderValueMinor"`
	AverageOrderValueMinor *int64 `json:"averageOrderValueMinor"`
}

type DashboardTenderMix struct {
	TenderType    string `json:"tenderType"`
	CapturedCount int64  `json:"capturedCount"`
	CapturedMinor int64  `json:"capturedMinor"`
	RefundCount   int64  `json:"refundCount"`
	RefundMinor   int64  `json:"refundMinor"`
	NetMinor      int64  `json:"netMinor"`
}

type DashboardOrderTypeMix struct {
	OrderType       OrderType `json:"orderType"`
	OrderCount      int64     `json:"orderCount"`
	OrderValueMinor int64     `json:"orderValueMinor"`
}

// DashboardHourly contains observed UTC hour buckets only. LocalHour is the
// outlet-local clock hour; StartsAt keeps repeated DST hours distinguishable.
type DashboardHourly struct {
	LocalHour       int       `json:"localHour"`
	StartsAt        time.Time `json:"startsAt"`
	OrderCount      int64     `json:"orderCount"`
	OrderValueMinor int64     `json:"orderValueMinor"`
}

type DashboardLeakage struct {
	CancelledOrderCount      int64 `json:"cancelledOrderCount"`
	CancelledOrderValueMinor int64 `json:"cancelledOrderValueMinor"`
	RefundCount              int64 `json:"refundCount"`
	RefundMinor              int64 `json:"refundMinor"`
	PromotionRedemptionCount int64 `json:"promotionRedemptionCount"`
	PromotionDiscountMinor   int64 `json:"promotionDiscountMinor"`
}

type DashboardTopItem struct {
	MenuItemID     string `json:"menuItemId,omitempty"`
	Name           string `json:"name"`
	Quantity       int64  `json:"quantity"`
	LineValueMinor int64  `json:"lineValueMinor"`
}

type DashboardDataQuality struct {
	FutureDatedOrderCount          int64 `json:"futureDatedOrderCount"`
	OrderCurrencyMismatchCount     int64 `json:"orderCurrencyMismatchCount"`
	ReceiptCurrencyMismatchCount   int64 `json:"receiptCurrencyMismatchCount"`
	TenderCurrencyMismatchCount    int64 `json:"tenderCurrencyMismatchCount"`
	OrderLineCurrencyMismatchCount int64 `json:"orderLineCurrencyMismatchCount"`
	UnlinkedMenuItemLineCount      int64 `json:"unlinkedMenuItemLineCount"`
}

type DashboardProvenance struct {
	Sales      string `json:"sales"`
	Orders     string `json:"orders"`
	Payments   string `json:"payments"`
	Promotions string `json:"promotions"`
	TopItems   string `json:"topItems"`
}
