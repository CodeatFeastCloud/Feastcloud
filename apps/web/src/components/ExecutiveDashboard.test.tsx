import { cleanup, fireEvent, render, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi, type Mock } from 'vitest';
import type { DailyDashboardData, DailyDashboardDataQuality } from '../domain/coreDashboard';
import type { KitchenOrder, KitchenSnapshot, TicketStatus, View } from '../domain/types';
import { createTranslator } from '../i18n';
import { ExecutiveDashboard } from './ExecutiveDashboard';

const tenantId = '11111111-1111-4111-8111-111111111111';
const outletId = '33333333-3333-4333-8333-333333333333';
afterEach(cleanup);

function emptySnapshot(patch: Partial<KitchenSnapshot> = {}): KitchenSnapshot {
  return { schemaVersion: 1, organizationId: tenantId, outletId, nextOrderNumber: 1, orders: [], tickets: [], ...patch };
}

function order(id: string, number: number, status: TicketStatus, menuItemId: string): KitchenOrder {
  return { id, number, type: 'dineIn', lines: [{ id: `${id}-line`, menuItemId, quantity: 1 }], status, createdAt: '2026-08-03T08:00:00.000Z', updatedAt: '2026-08-03T08:10:00.000Z', dueAt: status === 'preparing' ? '2020-01-01T00:00:00.000Z' : '2026-08-03T08:20:00.000Z', version: 1, origin: 'edge' };
}

const noDataQualityIssues: DailyDashboardDataQuality = { futureDatedOrderCount: 0, orderCurrencyMismatchCount: 0, receiptCurrencyMismatchCount: 0, tenderCurrencyMismatchCount: 0, orderLineCurrencyMismatchCount: 0, unlinkedMenuItemLineCount: 0 };

function verifiedDailyReport(): DailyDashboardData {
  return {
    outletId, businessDate: '2026-08-03', currency: 'USD', timeZone: 'America/New_York',
    period: { startsAt: '2026-08-03T04:00:00.000Z', endsAt: '2026-08-04T04:00:00.000Z', boundaryKind: 'outlet_local_calendar_day' }, asOf: '2026-08-04T04:01:00.000Z',
    sales: { receiptedOrderCount: 2, subtotalMinor: 115000, discountMinor: 5000, taxMinor: 8000, serviceChargeMinor: 2000, totalMinor: 120000 },
    paymentFlow: { capturedCount: 3, capturedMinor: 125000, refundCount: 1, refundMinor: 20000, netMinor: 105000 },
    orders: { total: 4, included: 3, completed: 2, cancelled: 1, active: 1, unpriced: 0, subtotalMinor: 145000, discountMinor: 5000, taxMinor: 8000, serviceChargeMinor: 2000, orderValueMinor: 150000, averageOrderValueMinor: 50000 },
    tenderMix: [{ tenderType: 'card_terminal', capturedCount: 2, capturedMinor: 100000, refundCount: 1, refundMinor: 20000, netMinor: 80000 }, { tenderType: 'external', capturedCount: 1, capturedMinor: 25000, refundCount: 0, refundMinor: 0, netMinor: 25000 }],
    fulfillmentMix: [{ orderType: 'dineIn', orderCount: 2, orderValueMinor: 100000 }, { orderType: 'delivery', orderCount: 1, orderValueMinor: 50000 }],
    hourly: [{ localHour: 12, startsAt: '2026-08-03T16:00:00.000Z', orderCount: 3, orderValueMinor: 150000 }],
    leakage: { cancelledOrderCount: 1, cancelledOrderValueMinor: 30000, refundCount: 1, refundMinor: 20000, promotionRedemptionCount: 1, promotionDiscountMinor: 5000 },
    topItems: [{ menuItemId: 'butter-chicken-bowl', name: 'Butter chicken bowl', quantity: 2, lineValueMinor: 65800 }],
    dataQuality: { ...noDataQualityIssues, orderCurrencyMismatchCount: 1 },
    provenance: { sales: 'fiscal_receipts', orders: 'orders', payments: 'tender_events', promotions: 'promotion_redemptions', topItems: 'order_lines' }, unavailableFields: ['onlineOrderReconciliation', 'marketplaceSettlement'],
  };
}

function renderDashboard(snapshot: KitchenSnapshot, report?: DailyDashboardData, onNavigate?: Mock<(view: View) => void>) {
  return { onNavigate: onNavigate ?? vi.fn(), ...render(<ExecutiveDashboard snapshot={snapshot} preferences={{ locale: 'en' }} t={createTranslator('en')} onNavigate={onNavigate} trustedDailyData={report} apiBase={null} />) };
}

describe('Service board', () => {
  it('opens in a live shift and does not pretend local data is connected KDS coverage', () => {
    const { container } = renderDashboard(emptySnapshot());
    const view = within(container);
    expect(view.getByRole('heading', { level: 1, name: 'Kitchen Pulse' })).toBeVisible();
    expect(view.getByRole('tab', { name: 'Live shift' })).toHaveAttribute('aria-selected', 'true');
    expect(view.getAllByText('Limited coverage')[0]).toBeVisible();
    expect(view.getAllByText('Ticket projection is not available')[0]).toBeVisible();
    expect(view.getByRole('button', { name: /01 New/ })).toBeVisible();
    expect(view.getByRole('button', { name: /02 Accepted/ })).toBeVisible();
    expect(view.queryByText('Billed sales')).not.toBeInTheDocument();
  });

  it('uses an explicit business-report mode for verified financial facts', () => {
    const { container } = renderDashboard(emptySnapshot(), verifiedDailyReport());
    const view = within(container);
    fireEvent.click(view.getByRole('tab', { name: 'Business report' }));
    expect(view.getByRole('heading', { level: 1, name: 'Business report' })).toBeVisible();
    expect(container).toHaveTextContent('Billed sales');
    expect(container).toHaveTextContent('$1,200.00');
    expect(container).toHaveTextContent('$1,050.00');
    expect(container).toHaveTextContent('Card');
    expect(view.queryByRole('heading', { name: 'Order status' })).not.toBeInTheDocument();
  });

  it('moves to the business report when the operator selects a date', () => {
    const { container } = renderDashboard(emptySnapshot(), verifiedDailyReport());
    const view = within(container);
    fireEvent.click(view.getByRole('tab', { name: 'Business report' }));
    fireEvent.change(view.getByLabelText('Business date'), { target: { value: '2026-08-02' } });
    expect(view.getByRole('tab', { name: 'Business report' })).toHaveAttribute('aria-selected', 'true');
    expect(view.getByRole('heading', { level: 1, name: 'Business report' })).toBeVisible();
    expect(view.queryByRole('heading', { name: 'Order status' })).not.toBeInTheDocument();
  });

  it('groups data-quality issues as one action instead of a misleading review total', () => {
    const report = verifiedDailyReport();
    report.dataQuality = { ...noDataQualityIssues, unlinkedMenuItemLineCount: 61 };
    const { container } = renderDashboard(emptySnapshot(), report);
    const view = within(container);
    fireEvent.click(view.getByRole('tab', { name: /System/ }));
    expect(view.getByText('61 data-quality exceptions')).toBeVisible();
    expect(view.queryByText('Review 61')).not.toBeInTheDocument();
    expect(view.getByText('Unlinked Menu Item Line Count')).toBeVisible();
    expect(within(view.getByText('Unlinked Menu Item Line Count').parentElement!).getByText('61')).toBeVisible();
  });

  it('only creates late-KDS actions once the outlet edge is paired', () => {
    const late = order('01991f31-0001-7000-8000-000000000402', 402, 'preparing', 'biryani');
    const local = renderDashboard(emptySnapshot({ orders: [late] }));
    expect(local.getAllByText('Limited coverage')[0]).toBeVisible();
    expect(within(local.container.querySelector('.shift-board__docket')!).queryByText('Late kitchen tickets')).not.toBeInTheDocument();
    cleanup();
    const paired = renderDashboard(emptySnapshot({ orders: [late], edgeId: 'edge-test' }));
    expect(paired.getByText('Act now')).toBeVisible();
    expect(within(paired.container.querySelector('.shift-board__docket')!).getByText('Late kitchen tickets')).toBeVisible();
  });

  it('keeps operational navigation tied to clear work destinations', () => {
    const go = vi.fn<(view: View) => void>();
    const { container } = renderDashboard(emptySnapshot({ edgeId: 'edge-test' }), undefined, go);
    fireEvent.click(container.querySelector<HTMLButtonElement>('.shift-board__primary')!);
    expect(go).toHaveBeenCalledWith('kds');
  });
});
