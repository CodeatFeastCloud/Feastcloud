import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchDailyDashboard, parseDailyDashboard } from './coreDashboard';

const outletId = '33333333-3333-4333-8333-333333333333';
const tenantId = '11111111-1111-4111-8111-111111111111';

function report() {
  return {
    outletId,
    businessDate: '2026-08-03',
    currency: 'INR',
    timeZone: 'Asia/Kolkata',
    period: {
      startsAt: '2026-08-02T18:30:00.000Z',
      endsAt: '2026-08-03T18:30:00.000Z',
      boundaryKind: 'outlet_local_calendar_day',
    },
    asOf: '2026-08-03T18:31:00.000Z',
    sales: { receiptedOrderCount: 1, subtotalMinor: 10000, discountMinor: 0, taxMinor: 500, serviceChargeMinor: 0, totalMinor: 10500 },
    paymentFlow: { capturedCount: 1, capturedMinor: 10500, refundCount: 0, refundMinor: 0, netMinor: 10500 },
    orders: { total: 1, included: 1, completed: 1, cancelled: 0, active: 0, unpriced: 0, subtotalMinor: 10000, discountMinor: 0, taxMinor: 500, serviceChargeMinor: 0, orderValueMinor: 10500, averageOrderValueMinor: 10500 },
    tenderMix: [{ tenderType: 'upi', capturedCount: 1, capturedMinor: 10500, refundCount: 0, refundMinor: 0, netMinor: 10500 }],
    fulfillmentMix: [{ orderType: 'takeaway', orderCount: 1, orderValueMinor: 10500 }],
    hourly: [{ localHour: 13, startsAt: '2026-08-03T07:30:00.000Z', orderCount: 1, orderValueMinor: 10500 }],
    leakage: { cancelledOrderCount: 0, cancelledOrderValueMinor: 0, refundCount: 0, refundMinor: 0, promotionRedemptionCount: 0, promotionDiscountMinor: 0 },
    topItems: [{ name: 'Lunch bowl', quantity: 1, lineValueMinor: 10000 }],
    dataQuality: { futureDatedOrderCount: 0, orderCurrencyMismatchCount: 0, receiptCurrencyMismatchCount: 0, tenderCurrencyMismatchCount: 0, orderLineCurrencyMismatchCount: 0, unlinkedMenuItemLineCount: 0 },
    provenance: { sales: 'fiscal_receipts', orders: 'orders', payments: 'tender_events', promotions: 'promotion_redemptions', topItems: 'order_lines' },
    unavailableFields: ['onlineOrderReconciliation'],
    additiveFutureField: { safe: true },
  };
}

afterEach(() => {
  sessionStorage.clear();
  vi.unstubAllGlobals();
});

describe('daily dashboard contract', () => {
  it('validates the frozen contract and tolerates additive response fields', () => {
    const parsed = parseDailyDashboard(report());

    expect(parsed.period.boundaryKind).toBe('outlet_local_calendar_day');
    expect(parsed.orders.orderValueMinor).toBe(10500);
    expect(parsed.paymentFlow.netMinor).toBe(10500);
    expect(parsed.fulfillmentMix[0]?.orderType).toBe('takeaway');
    expect(parsed.dataQuality.futureDatedOrderCount).toBe(0);
  });

  it('rejects a report that is not bounded by the outlet-local calendar day', () => {
    const invalid = report();
    invalid.period.boundaryKind = 'browser_local_day';

    expect(() => parseDailyDashboard(invalid)).toThrow(/boundaryKind/);
  });

  it('requests the selected outlet and business date with tenant-scoped headers', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      data: report(),
      meta: { requestId: 'req-1' },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    const parsed = await fetchDailyDashboard(
      'http://core.test/api/v1',
      tenantId,
      outletId,
      '2026-08-03',
    );

    expect(parsed.businessDate).toBe('2026-08-03');
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(`http://core.test/api/v1/dashboard/daily?outletId=${outletId}&date=2026-08-03`);
    expect(request.headers).toMatchObject({
      'X-FeastCloud-Tenant-ID': tenantId,
      'X-FeastCloud-Actor-ID': 'manager-dashboard',
    });
  });
});
