import { coreApiBase } from './coreInventory';
import type { OrderType } from './types';

export interface DailyDashboardSales {
  receiptedOrderCount: number;
  subtotalMinor: number;
  discountMinor: number;
  taxMinor: number;
  serviceChargeMinor: number;
  totalMinor: number;
}

export interface DailyDashboardOrders {
  total: number;
  included: number;
  completed: number;
  cancelled: number;
  active: number;
  unpriced: number;
  subtotalMinor: number;
  discountMinor: number;
  taxMinor: number;
  serviceChargeMinor: number;
  orderValueMinor: number;
  averageOrderValueMinor: number | null;
}

export interface DailyDashboardTender {
  tenderType: string;
  capturedCount: number;
  capturedMinor: number;
  refundCount: number;
  refundMinor: number;
  netMinor: number;
}

export interface DailyDashboardPaymentFlow {
  capturedCount: number;
  capturedMinor: number;
  refundCount: number;
  refundMinor: number;
  netMinor: number;
}

export interface DailyDashboardFulfillment {
  orderType: OrderType;
  orderCount: number;
  orderValueMinor: number;
}

export interface DailyDashboardHour {
  localHour: number;
  startsAt: string;
  orderCount: number;
  orderValueMinor: number;
}

export interface DailyDashboardLeakage {
  cancelledOrderCount: number;
  cancelledOrderValueMinor: number;
  refundCount: number;
  refundMinor: number;
  promotionRedemptionCount: number;
  promotionDiscountMinor: number;
}

export interface DailyDashboardItem {
  menuItemId?: string;
  name: string;
  quantity: number;
  lineValueMinor: number;
}

export interface DailyDashboardProvenance {
  sales: string;
  orders: string;
  payments: string;
  promotions: string;
  topItems: string;
}

export interface DailyDashboardDataQuality {
  futureDatedOrderCount: number;
  orderCurrencyMismatchCount: number;
  receiptCurrencyMismatchCount: number;
  tenderCurrencyMismatchCount: number;
  orderLineCurrencyMismatchCount: number;
  unlinkedMenuItemLineCount: number;
}

export interface DailyDashboardData {
  outletId: string;
  businessDate: string;
  currency: string;
  timeZone: string;
  period: { startsAt: string; endsAt: string; boundaryKind: 'outlet_local_calendar_day' };
  asOf: string;
  sales: DailyDashboardSales;
  orders: DailyDashboardOrders;
  paymentFlow: DailyDashboardPaymentFlow;
  tenderMix: DailyDashboardTender[];
  fulfillmentMix: DailyDashboardFulfillment[];
  hourly: DailyDashboardHour[];
  leakage: DailyDashboardLeakage;
  topItems: DailyDashboardItem[];
  dataQuality: DailyDashboardDataQuality;
  provenance: DailyDashboardProvenance;
  unavailableFields: string[];
}

export class DailyDashboardRequestError extends Error {
  constructor(readonly status: number, message: string) {
    super(message);
    this.name = 'DailyDashboardRequestError';
  }
}

const fulfillmentTypes = new Set<OrderType>(['dineIn', 'takeaway', 'delivery', 'roomService']);

function record(value: unknown, field: string): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${field} must be an object`);
  }
  return value as Record<string, unknown>;
}

function text(value: unknown, field: string, allowEmpty = false): string {
  if (typeof value !== 'string' || (!allowEmpty && value.trim().length === 0)) {
    throw new Error(`${field} must be a string`);
  }
  return value;
}

function integer(value: unknown, field: string, allowNegative = false): number {
  if (!Number.isSafeInteger(value) || (!allowNegative && Number(value) < 0)) {
    throw new Error(`${field} must be a safe integer`);
  }
  return Number(value);
}

function list(value: unknown, field: string): unknown[] {
  if (!Array.isArray(value)) throw new Error(`${field} must be an array`);
  return value;
}

function instant(value: unknown, field: string): string {
  const parsed = text(value, field);
  if (!Number.isFinite(Date.parse(parsed))) throw new Error(`${field} must be an ISO timestamp`);
  return parsed;
}

function date(value: unknown, field: string): string {
  const parsed = text(value, field);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(parsed) || !Number.isFinite(Date.parse(`${parsed}T00:00:00Z`))) {
    throw new Error(`${field} must be YYYY-MM-DD`);
  }
  return parsed;
}

/** Validates the frozen daily-dashboard boundary while tolerating additive fields. */
export function parseDailyDashboard(value: unknown): DailyDashboardData {
  const source = record(value, 'dashboard');
  const periodSource = record(source.period, 'period');
  const salesSource = record(source.sales, 'sales');
  const ordersSource = record(source.orders, 'orders');
  const paymentFlowSource = record(source.paymentFlow, 'paymentFlow');
  const leakageSource = record(source.leakage, 'leakage');
  const dataQualitySource = record(source.dataQuality, 'dataQuality');
  const provenanceSource = record(source.provenance, 'provenance');
  if (periodSource.boundaryKind !== 'outlet_local_calendar_day') {
    throw new Error('period.boundaryKind must be outlet_local_calendar_day');
  }
  const currency = text(source.currency, 'currency');
  if (!/^[A-Z]{3}$/.test(currency)) throw new Error('currency must be an ISO 4217 code');

  const tenderMix = list(source.tenderMix, 'tenderMix').map((entry, index) => {
    const tender = record(entry, `tenderMix[${index}]`);
    return {
      tenderType: text(tender.tenderType, `tenderMix[${index}].tenderType`),
      capturedCount: integer(tender.capturedCount, `tenderMix[${index}].capturedCount`),
      capturedMinor: integer(tender.capturedMinor, `tenderMix[${index}].capturedMinor`),
      refundCount: integer(tender.refundCount, `tenderMix[${index}].refundCount`),
      refundMinor: integer(tender.refundMinor, `tenderMix[${index}].refundMinor`),
      netMinor: integer(tender.netMinor, `tenderMix[${index}].netMinor`, true),
    };
  });
  const fulfillmentMix = list(source.fulfillmentMix, 'fulfillmentMix').map((entry, index) => {
    const fulfillment = record(entry, `fulfillmentMix[${index}]`);
    const orderType = text(fulfillment.orderType, `fulfillmentMix[${index}].orderType`) as OrderType;
    if (!fulfillmentTypes.has(orderType)) throw new Error(`fulfillmentMix[${index}].orderType is invalid`);
    return {
      orderType,
      orderCount: integer(fulfillment.orderCount, `fulfillmentMix[${index}].orderCount`),
      orderValueMinor: integer(fulfillment.orderValueMinor, `fulfillmentMix[${index}].orderValueMinor`),
    };
  });
  const hourly = list(source.hourly, 'hourly').map((entry, index) => {
    const hour = record(entry, `hourly[${index}]`);
    const localHour = integer(hour.localHour, `hourly[${index}].localHour`);
    if (localHour > 23) throw new Error(`hourly[${index}].localHour is invalid`);
    return {
      localHour,
      startsAt: instant(hour.startsAt, `hourly[${index}].startsAt`),
      orderCount: integer(hour.orderCount, `hourly[${index}].orderCount`),
      orderValueMinor: integer(hour.orderValueMinor, `hourly[${index}].orderValueMinor`),
    };
  });
  const topItems = list(source.topItems, 'topItems').map((entry, index) => {
    const item = record(entry, `topItems[${index}]`);
    const quantity = integer(item.quantity, `topItems[${index}].quantity`);
    if (quantity < 1) throw new Error(`topItems[${index}].quantity must be positive`);
    return {
      ...(item.menuItemId === undefined || item.menuItemId === null
        ? {}
        : { menuItemId: text(item.menuItemId, `topItems[${index}].menuItemId`) }),
      name: text(item.name, `topItems[${index}].name`),
      quantity,
      lineValueMinor: integer(item.lineValueMinor, `topItems[${index}].lineValueMinor`),
    };
  });

  return {
    outletId: text(source.outletId, 'outletId'),
    businessDate: date(source.businessDate, 'businessDate'),
    currency,
    timeZone: text(source.timeZone, 'timeZone'),
    period: {
      startsAt: instant(periodSource.startsAt, 'period.startsAt'),
      endsAt: instant(periodSource.endsAt, 'period.endsAt'),
      boundaryKind: 'outlet_local_calendar_day',
    },
    asOf: instant(source.asOf, 'asOf'),
    sales: {
      receiptedOrderCount: integer(salesSource.receiptedOrderCount, 'sales.receiptedOrderCount'),
      subtotalMinor: integer(salesSource.subtotalMinor, 'sales.subtotalMinor'),
      discountMinor: integer(salesSource.discountMinor, 'sales.discountMinor'),
      taxMinor: integer(salesSource.taxMinor, 'sales.taxMinor'),
      serviceChargeMinor: integer(salesSource.serviceChargeMinor, 'sales.serviceChargeMinor'),
      totalMinor: integer(salesSource.totalMinor, 'sales.totalMinor'),
    },
    orders: {
      total: integer(ordersSource.total, 'orders.total'),
      included: integer(ordersSource.included, 'orders.included'),
      completed: integer(ordersSource.completed, 'orders.completed'),
      cancelled: integer(ordersSource.cancelled, 'orders.cancelled'),
      active: integer(ordersSource.active, 'orders.active'),
      unpriced: integer(ordersSource.unpriced, 'orders.unpriced'),
      subtotalMinor: integer(ordersSource.subtotalMinor, 'orders.subtotalMinor'),
      discountMinor: integer(ordersSource.discountMinor, 'orders.discountMinor'),
      taxMinor: integer(ordersSource.taxMinor, 'orders.taxMinor'),
      serviceChargeMinor: integer(ordersSource.serviceChargeMinor, 'orders.serviceChargeMinor'),
      orderValueMinor: integer(ordersSource.orderValueMinor, 'orders.orderValueMinor'),
      averageOrderValueMinor: ordersSource.averageOrderValueMinor === null
        ? null
        : integer(ordersSource.averageOrderValueMinor, 'orders.averageOrderValueMinor'),
    },
    paymentFlow: {
      capturedCount: integer(paymentFlowSource.capturedCount, 'paymentFlow.capturedCount'),
      capturedMinor: integer(paymentFlowSource.capturedMinor, 'paymentFlow.capturedMinor'),
      refundCount: integer(paymentFlowSource.refundCount, 'paymentFlow.refundCount'),
      refundMinor: integer(paymentFlowSource.refundMinor, 'paymentFlow.refundMinor'),
      netMinor: integer(paymentFlowSource.netMinor, 'paymentFlow.netMinor', true),
    },
    tenderMix,
    fulfillmentMix,
    hourly,
    leakage: {
      cancelledOrderCount: integer(leakageSource.cancelledOrderCount, 'leakage.cancelledOrderCount'),
      cancelledOrderValueMinor: integer(leakageSource.cancelledOrderValueMinor, 'leakage.cancelledOrderValueMinor'),
      refundCount: integer(leakageSource.refundCount, 'leakage.refundCount'),
      refundMinor: integer(leakageSource.refundMinor, 'leakage.refundMinor'),
      promotionRedemptionCount: integer(leakageSource.promotionRedemptionCount, 'leakage.promotionRedemptionCount'),
      promotionDiscountMinor: integer(leakageSource.promotionDiscountMinor, 'leakage.promotionDiscountMinor'),
    },
    topItems,
    dataQuality: {
      futureDatedOrderCount: integer(dataQualitySource.futureDatedOrderCount, 'dataQuality.futureDatedOrderCount'),
      orderCurrencyMismatchCount: integer(dataQualitySource.orderCurrencyMismatchCount, 'dataQuality.orderCurrencyMismatchCount'),
      receiptCurrencyMismatchCount: integer(dataQualitySource.receiptCurrencyMismatchCount, 'dataQuality.receiptCurrencyMismatchCount'),
      tenderCurrencyMismatchCount: integer(dataQualitySource.tenderCurrencyMismatchCount, 'dataQuality.tenderCurrencyMismatchCount'),
      orderLineCurrencyMismatchCount: integer(dataQualitySource.orderLineCurrencyMismatchCount, 'dataQuality.orderLineCurrencyMismatchCount'),
      unlinkedMenuItemLineCount: integer(dataQualitySource.unlinkedMenuItemLineCount, 'dataQuality.unlinkedMenuItemLineCount'),
    },
    provenance: {
      sales: text(provenanceSource.sales, 'provenance.sales'),
      orders: text(provenanceSource.orders, 'provenance.orders'),
      payments: text(provenanceSource.payments, 'provenance.payments'),
      promotions: text(provenanceSource.promotions, 'provenance.promotions'),
      topItems: text(provenanceSource.topItems, 'provenance.topItems'),
    },
    unavailableFields: list(source.unavailableFields, 'unavailableFields').map((entry, index) =>
      text(entry, `unavailableFields[${index}]`),
    ),
  };
}

function authHeaders(tenantId: string): Record<string, string> {
  const token = sessionStorage.getItem('feastcloud.oidc-access-token');
  return token
    ? { Authorization: `Bearer ${token}` }
    : {
        'X-FeastCloud-Tenant-ID': tenantId,
        'X-FeastCloud-Actor-ID': 'manager-dashboard',
      };
}

export const dailyDashboardApiBase = coreApiBase;

export async function fetchDailyDashboard(
  apiBase: string,
  tenantId: string,
  outletId: string,
  businessDate: string,
  signal?: AbortSignal,
): Promise<DailyDashboardData> {
  const query = new URLSearchParams({ outletId, date: businessDate });
  const response = await fetch(`${apiBase}/dashboard/daily?${query}`, {
    headers: { Accept: 'application/json', ...authHeaders(tenantId) },
    cache: 'no-store',
    signal,
  });
  if (!response.ok) {
    throw new DailyDashboardRequestError(response.status, `Daily dashboard returned ${response.status}`);
  }
  const envelope = record(await response.json(), 'response');
  return parseDailyDashboard(envelope.data);
}
