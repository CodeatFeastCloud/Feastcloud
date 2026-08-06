import { useCallback, useEffect, useMemo, useState, type CSSProperties, type ReactNode } from 'react';
import { catalogById, localize } from '../domain/catalog';
import { dailyDashboardApiBase, fetchDailyDashboard, type DailyDashboardData } from '../domain/coreDashboard';
import type { KitchenOrder, KitchenSnapshot, OrderType, TicketStatus, UserPreferences, View } from '../domain/types';
import { formatMoney } from '../i18n';
import type { MessageKey } from '../i18n/messages';
import './executive-dashboard.css';

type DashboardMode = 'live' | 'report' | 'system';
type IconName = 'alert' | 'arrow' | 'bag' | 'calendar' | 'check' | 'clock' | 'cloud' | 'food' | 'money' | 'refresh' | 'trend';

interface Props {
  snapshot: KitchenSnapshot;
  preferences: Pick<UserPreferences, 'locale'>;
  t: (key: MessageKey, replacements?: Record<string, string | number>) => string;
  onNavigate?: (view: View) => void;
  trustedDailyData?: DailyDashboardData;
  apiBase?: string | null;
}

const paths: Record<IconName, ReactNode> = {
  alert: <path d="M12 3 2.8 20h18.4L12 3Zm0 6v5m0 3v.01" />,
  arrow: <path d="M5 12h14m-5-5 5 5-5 5" />,
  bag: <path d="M6 8h12l1 13H5L6 8Zm3 0V6a3 3 0 0 1 6 0v2" />,
  calendar: <path d="M6 3v3m12-3v3M4 9h16M5 5h14a1 1 0 0 1 1 1v14H4V6a1 1 0 0 1 1-1Z" />,
  check: <path d="m5 12 4 4L19 6" />,
  clock: <path d="M12 7v5l3.5 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />,
  cloud: <path d="M7 18h10a4 4 0 0 0 .4-8 6 6 0 0 0-11.6 1.5A3.3 3.3 0 0 0 7 18Zm2-4 2 2 4-4" />,
  food: <path d="M7 4v7a3 3 0 0 0 3 3V4m-6 0v7a3 3 0 0 0 3 3v6m8-16v16m0-9c3 0 5-2 5-7" />,
  money: <><path d="M4 6h16v12H4z" /><path d="M8 9h5a2 2 0 0 1 0 4H9m3-6v10m-4-2h5" /></>,
  refresh: <path d="M20 7v5h-5M4 17v-5h5m10.3 0a7.5 7.5 0 0 0-13-4M4.7 12a7.5 7.5 0 0 0 13 4" />,
  trend: <path d="m4 16 5-5 4 3 7-8m-5 0h5v5" />,
};

function Icon({ name }: { name: IconName }) {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">{paths[name]}</svg>;
}

const activeStatuses = new Set<TicketStatus>(['queued', 'fired', 'preparing', 'ready']);
const flow: Array<{ status: TicketStatus; label: MessageKey }> = [
  { status: 'queued', label: 'kds.status.queued' },
  { status: 'fired', label: 'kds.status.fired' },
  { status: 'preparing', label: 'kds.status.preparing' },
  { status: 'ready', label: 'kds.status.ready' },
];
const orderTypes: OrderType[] = ['dineIn', 'takeaway', 'delivery', 'roomService'];

function timestamp(value?: string): number { const parsed = value ? new Date(value).getTime() : 0; return Number.isFinite(parsed) ? parsed : 0; }
function businessDate(now = new Date()): string { const offset = now.getTimezoneOffset() * 60_000; return new Date(now.getTime() - offset).toISOString().slice(0, 10); }
function displayDate(locale: string, value: string): string { const date = new Date(`${value}T12:00:00`); return Number.isFinite(date.getTime()) ? new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', year: 'numeric' }).format(date) : value; }
function displayTime(locale: string, value?: string): string { const date = timestamp(value); return date ? new Intl.DateTimeFormat(locale, { hour: 'numeric', minute: '2-digit' }).format(date) : '—'; }
function readable(value: string): string { return value.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/[._-]+/g, ' ').replace(/^./, (letter) => letter.toUpperCase()); }
function currency(locale: string, minor: number, code: string): string { try { const digits = new Intl.NumberFormat('en', { style: 'currency', currency: code }).resolvedOptions().maximumFractionDigits ?? 2; return new Intl.NumberFormat(locale, { style: 'currency', currency: code, maximumFractionDigits: digits }).format(minor / (10 ** digits)); } catch { return `${code} ${minor}`; } }
function tenderLabel(type: string, t: Props['t']): string { const keys: Partial<Record<string, MessageKey>> = { cash: 'dashboard.tender.cash', card: 'dashboard.tender.card', card_terminal: 'dashboard.tender.card', upi: 'dashboard.tender.upi', wallet: 'dashboard.tender.wallet', gift_card: 'dashboard.tender.giftCard', room_folio: 'dashboard.tender.roomFolio', external: 'dashboard.tender.external' }; const key = keys[type.toLowerCase()]; return key ? t(key) : t('dashboard.tender.other', { type: type.replace(/[_-]+/g, ' ') }); }

export function ExecutiveDashboard({ snapshot, preferences, t, onNavigate, trustedDailyData, apiBase }: Props) {
  const today = businessDate();
  const [mode, setMode] = useState<DashboardMode>('live');
  const [selectedDate, setSelectedDate] = useState(() => trustedDailyData?.businessDate ?? today);
  const [refresh, setRefresh] = useState(0);
  const [remoteReport, setRemoteReport] = useState<DailyDashboardData>();
  const [loadState, setLoadState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
  const api = useMemo(() => apiBase === null ? undefined : dailyDashboardApiBase(apiBase ?? import.meta.env.VITE_CORE_URL), [apiBase]);
  const injected = trustedDailyData?.businessDate === selectedDate ? trustedDailyData : undefined;

  useEffect(() => {
    if (injected) { setRemoteReport(undefined); setLoadState('ready'); return; }
    if (!api) { setRemoteReport(undefined); setLoadState('idle'); return; }
    const controller = new AbortController();
    setLoadState('loading'); setRemoteReport(undefined);
    void fetchDailyDashboard(api, snapshot.organizationId, snapshot.outletId, selectedDate, controller.signal)
      .then((next) => { if (next.outletId !== snapshot.outletId || next.businessDate !== selectedDate) throw new Error('Dashboard scope mismatch'); setRemoteReport(next); setLoadState('ready'); })
      .catch((error: unknown) => { if (!(error instanceof DOMException && error.name === 'AbortError')) setLoadState('error'); });
    return () => controller.abort();
  }, [api, injected, refresh, selectedDate, snapshot.organizationId, snapshot.outletId]);

  const report = injected ?? (remoteReport?.businessDate === selectedDate ? remoteReport : undefined);
  const live = useMemo(() => {
    const now = Date.now();
    const orders = snapshot.orders.filter((order) => order.status !== 'cancelled');
    const statuses = Object.fromEntries(flow.map(({ status }) => [status, orders.filter((order) => order.status === status).length])) as Record<TicketStatus, number>;
    const active = orders.filter((order) => activeStatuses.has(order.status));
    const coverage = Boolean(snapshot.edgeId && snapshot.tickets !== undefined);
    const late = active.filter((order) => order.status !== 'ready' && timestamp(order.dueAt) > 0 && timestamp(order.dueAt) < now);
    const handoff = active.filter((order) => order.status === 'ready' && timestamp(order.updatedAt) > 0 && now - timestamp(order.updatedAt) > 300_000);
    const unrouted = coverage ? active.filter((order) => !snapshot.tickets?.some((ticket) => ticket.orderId === order.id)) : [];
    const stations = new Map<string, { total: number; ready: number; nextDue: number }>();
    if (coverage) (snapshot.tickets ?? []).filter((ticket) => activeStatuses.has(ticket.status)).forEach((ticket) => {
      const row = stations.get(ticket.stationId) ?? { total: 0, ready: 0, nextDue: 0 };
      row.total += 1; if (ticket.status === 'ready') row.ready += 1;
      const due = timestamp(ticket.targetAt); if (due && (!row.nextDue || due < row.nextDue)) row.nextDue = due;
      stations.set(ticket.stationId, row);
    });
    return { coverage, orders, active, statuses, late, handoff, unrouted, stations: [...stations.entries()].sort((a, b) => b[1].total - a[1].total) };
  }, [snapshot]);

  const qualityCount = report ? Object.values(report.dataQuality).reduce((sum, count) => sum + count, 0) : 0;
  const todayReport = selectedDate === today ? report : undefined;
  const money = (minor: number) => report ? currency(preferences.locale, minor, report.currency) : formatMoney(preferences.locale, minor);
  const serviceTone = !live.coverage ? 'limited' : live.late.length || live.unrouted.length ? 'urgent' : live.handoff.length ? 'watch' : 'clear';
  const serviceLabel = serviceTone === 'limited' ? t('dashboard.simple.limitedCoverage') : serviceTone === 'urgent' ? t('dashboard.simple.actNow') : serviceTone === 'watch' ? t('dashboard.simple.watch') : t('dashboard.simple.serviceClear');
  const serviceHelp = serviceTone === 'limited' ? t('dashboard.simple.limitedCoverageHelp') : serviceTone === 'urgent' ? t('dashboard.simple.actNowHelp') : serviceTone === 'watch' ? t('dashboard.simple.watchHelp') : t('dashboard.simple.serviceClearHelp');
  const modeTabs: Array<{ id: DashboardMode; label: MessageKey }> = [
    { id: 'live', label: 'dashboard.simple.live' }, { id: 'report', label: 'dashboard.simple.overview' }, { id: 'system', label: 'dashboard.simple.controls' },
  ];
  const actions = [
    !live.coverage ? { icon: 'cloud' as IconName, title: t('dashboard.exception.unrouted.unavailable'), help: t('dashboard.simple.limitedCoverageHelp'), target: 'operations' as View, tone: 'limited' } : null,
    live.coverage && live.late.length ? { icon: 'alert' as IconName, title: t('dashboard.exception.late.label'), help: t('dashboard.exception.late.help'), target: 'kds' as View, tone: 'urgent' } : null,
    live.coverage && live.unrouted.length ? { icon: 'alert' as IconName, title: t('dashboard.exception.unrouted.label'), help: t('dashboard.exception.unrouted.help'), target: 'kds' as View, tone: 'urgent' } : null,
    live.coverage && live.handoff.length ? { icon: 'clock' as IconName, title: t('dashboard.exception.handoff.label'), help: t('dashboard.exception.handoff.help'), target: 'kds' as View, tone: 'watch' } : null,
    todayReport && qualityCount ? { icon: 'trend' as IconName, title: t('dashboard.dataQuality.warning', { count: qualityCount }), help: t('dashboard.dataQuality.warningHelp'), target: undefined, tone: 'watch' } : null,
  ].filter(Boolean) as Array<{ icon: IconName; title: string; help: string; target?: View; tone: string }>;
  const buckets = report ? Array.from({ length: 6 }, (_, index) => { const start = index * 4; const matching = report.hourly.filter((hour) => hour.localHour >= start && hour.localHour < start + 4); return { start, orders: matching.reduce((sum, hour) => sum + hour.orderCount, 0), value: matching.reduce((sum, hour) => sum + hour.orderValueMinor, 0) }; }) : [];
  const peak = Math.max(1, ...buckets.map((bucket) => bucket.orders));
  const fulfillment = Object.fromEntries(orderTypes.map((type) => [type, report?.fulfillmentMix.find((row) => row.orderType === type)?.orderCount ?? 0])) as Record<OrderType, number>;
  const fulfillmentTotal = orderTypes.reduce((sum, type) => sum + fulfillment[type], 0);

  const changeDate = (value: string) => { setSelectedDate(value); setMode('report'); };
  const refreshReport = useCallback(() => setRefresh((value) => value + 1), []);

  return <section className="shift-board" aria-labelledby="shift-board-title">
    <header className="shift-board__header">
      <div>
        <span className="shift-board__eyebrow"><i className={`shift-board__live-dot ${serviceTone}`} />{mode === 'live' ? t('dashboard.simple.live') : mode === 'report' ? t('dashboard.simple.overview') : t('dashboard.simple.controls')}</span>
        <h1 id="shift-board-title">{mode === 'live' ? t('dashboard.simple.kitchenPulse') : mode === 'report' ? t('dashboard.simple.overview') : t('dashboard.simple.controls')}</h1>
        <p>{mode === 'live' ? `${t('app.outlet')} · ${live.coverage ? t('dashboard.sync.edgePaired') : t('dashboard.sync.localSnapshot')}` : `${t('app.outlet')} · ${displayDate(preferences.locale, selectedDate)}`}</p>
      </div>
      <div className="shift-board__header-actions">
        {mode !== 'live' && <label className="shift-board__date"><Icon name="calendar" /><input type="date" aria-label={t('dashboard.daily.date')} value={selectedDate} onChange={(event) => changeDate(event.target.value)} /></label>}
        {mode !== 'live' && <button type="button" className="shift-board__icon-button" onClick={refreshReport} disabled={Boolean(injected) || !api || loadState === 'loading'} aria-label={t('dashboard.daily.refresh')}><Icon name="refresh" /></button>}
        <button type="button" className="shift-board__primary" onClick={() => onNavigate?.(mode === 'live' ? 'kds' : 'orders')}><Icon name={mode === 'live' ? 'food' : 'bag'} />{mode === 'live' ? t('dashboard.status.openKds') : t('nav.orders')}</button>
      </div>
    </header>

    <div className="shift-board__modes" role="tablist" aria-label={t('nav.overview')}>
      {modeTabs.map((tab) => <button key={tab.id} type="button" role="tab" aria-selected={mode === tab.id} className={mode === tab.id ? 'active' : ''} onClick={() => setMode(tab.id)}>{t(tab.label)}{tab.id === 'system' && qualityCount > 0 && <b>{qualityCount}</b>}</button>)}
    </div>

    {mode === 'live' && <div className="shift-board__live" role="tabpanel">
      <section className={`shift-board__service-state ${serviceTone}`}>
        <div><span>{serviceLabel}</span><strong>{live.coverage ? t('dashboard.simple.kitchenPulse') : t('dashboard.simple.limitedCoverage')}</strong><p>{serviceHelp}</p></div>
        <div className="shift-board__state-numbers"><span><b>{live.active.length}</b>{t('dashboard.metric.activeCompleted', { active: live.active.length, completed: 0 })}</span><span><b>{live.coverage ? live.late.length : '—'}</b>{t('dashboard.exception.late.label')}</span></div>
      </section>
      <div className="shift-board__live-grid">
        <article className="shift-board__flow-card">
          <div className="shift-board__section-heading"><div><span>{t('dashboard.status.kicker')}</span><h2>{t('dashboard.status.title')}</h2><p>{live.coverage ? t('dashboard.source.liveSnapshot') : t('dashboard.snapshot.fallback')}</p></div><button type="button" onClick={() => onNavigate?.('kds')}>{t('dashboard.status.openKds')} <Icon name="arrow" /></button></div>
          <div className="shift-board__feastline">{flow.map(({ status, label }, index) => <button key={status} type="button" onClick={() => onNavigate?.('kds')}><span className="shift-board__flow-index">0{index + 1}</span><strong>{t(label)}</strong><b>{live.statuses[status]}</b><small>{t('dashboard.orders.count', { count: live.statuses[status] })}</small></button>)}</div>
          <div className="shift-board__stations"><div className="shift-board__stations-title"><span>{t('kds.station.all')}</span><small>{live.coverage ? t('dashboard.sync.edgePaired') : t('dashboard.exception.unrouted.unavailable')}</small></div>{live.coverage ? live.stations.length ? live.stations.map(([station, data]) => <button type="button" key={station} onClick={() => onNavigate?.('kds')}><span><strong>{station}</strong><small>{data.ready} {t('kds.status.ready').toLowerCase()}</small></span><b>{data.total}</b><em>{data.nextDue ? displayTime(preferences.locale, new Date(data.nextDue).toISOString()) : '—'}</em><Icon name="arrow" /></button>) : <p>{t('kds.empty')}</p> : <button type="button" className="shift-board__connect-lane" onClick={() => onNavigate?.('operations')}><Icon name="cloud" /><span><strong>{t('dashboard.exception.unrouted.unavailable')}</strong><small>{t('dashboard.simple.limitedCoverageHelp')}</small></span><Icon name="arrow" /></button>}</div>
        </article>
        <aside className="shift-board__docket"><div className="shift-board__section-heading"><div><span>{t('dashboard.exceptions.kicker')}</span><h2>{t('dashboard.exceptions.title')}</h2></div>{actions.length > 0 && <b>{actions.length}</b>}</div>{actions.length ? <div>{actions.slice(0, 5).map((action, index) => <button type="button" key={`${action.title}-${index}`} className={action.tone} onClick={() => action.target ? onNavigate?.(action.target) : setMode('system')}><Icon name={action.icon} /><span><strong>{action.title}</strong><small>{action.help}</small></span><Icon name="arrow" /></button>)}</div> : <div className="shift-board__all-clear"><Icon name="check" /><span><strong>{t('dashboard.exceptions.clear')}</strong><small>{t('dashboard.exceptions.clearHelp')}</small></span></div>}</aside>
      </div>
      <section className="shift-board__graph"><div className="shift-board__section-heading"><div><span>{t('inventory.eyebrow')}</span><h2>{t('dashboard.simple.performance')}</h2><p>{t('dashboard.simple.limitedCoverageHelp')}</p></div></div><div className="shift-board__graph-line"><div className={todayReport ? 'connected' : 'unknown'}><span>01</span><strong>{t('dashboard.metric.totalOrders')}</strong><small>{todayReport ? t('dashboard.orders.count', { count: todayReport.orders.total }) : t('dashboard.daily.detailsUnavailable')}</small></div><i /><div className={todayReport && qualityCount === 0 ? 'connected' : 'watch'}><span>02</span><strong>{t('inventory.recipes')}</strong><small>{todayReport && qualityCount ? t('dashboard.dataQuality.warning', { count: qualityCount }) : t('dashboard.simple.serviceClear')}</small></div><i /><button type="button" onClick={() => onNavigate?.('production')}><span>03</span><strong>{t('production.title')}</strong><small>{t('production.queue')}</small></button><i /><button type="button" onClick={() => onNavigate?.('inventory')}><span>04</span><strong>{t('inventory.title')}</strong><small>{t('inventory.onHand')}</small></button><i /><div className={live.coverage ? 'connected' : 'unknown'}><span>05</span><strong>{t('kds.eyebrow')}</strong><small>{live.coverage ? t('dashboard.sync.edgePaired') : t('dashboard.exception.unrouted.unavailable')}</small></div></div></section>
    </div>}

    {mode === 'report' && <div className="shift-board__report" role="tabpanel">
      {!report ? <div className="shift-board__unavailable"><Icon name="cloud" /><strong>{loadState === 'loading' ? t('dashboard.daily.loading') : t('dashboard.daily.detailsUnavailable')}</strong><span>{loadState === 'loading' ? t('dashboard.daily.loadingHelp') : t('dashboard.daily.detailsUnavailableHelp')}</span></div> : <>
        <section className="shift-board__ribbon"><div><span>{t('dashboard.simple.billedSales')}</span><strong>{money(report.sales.totalMinor)}</strong><small>{t('dashboard.simple.bills', { count: report.sales.receiptedOrderCount })}</small></div><div><span>{t('dashboard.metric.totalOrders')}</span><strong>{report.orders.total}</strong><small>{t('dashboard.metric.activeCompleted', { active: report.orders.active, completed: report.orders.completed })}</small></div><div><span>{t('dashboard.metric.averageValue')}</span><strong>{report.orders.averageOrderValueMinor === null ? '—' : money(report.orders.averageOrderValueMinor)}</strong><small>{t('dashboard.metric.averageOrderHelp', { count: report.orders.included })}</small></div><div><span>{t('dashboard.simple.netPayments')}</span><strong>{money(report.paymentFlow.netMinor)}</strong><small>{t('dashboard.simple.netPaymentsHelp', { captured: money(report.paymentFlow.capturedMinor), refunds: money(report.paymentFlow.refundMinor) })}</small></div><div className={report.sales.receiptedOrderCount < report.orders.total ? 'watch' : ''}><span>{t('dashboard.reconciliation.title')}</span><strong>{report.sales.receiptedOrderCount}/{report.orders.total}</strong><small>{t('dashboard.reconciliation.receipts')}</small></div></section>
        <div className="shift-board__report-grid"><article className="shift-board__runway"><div className="shift-board__section-heading"><div><span>{t('dashboard.simple.performance')}</span><h2>{t('dashboard.simple.ordersByHour')}</h2></div></div><div>{buckets.map((bucket) => <span key={bucket.start} style={{ '--load': `${Math.max(4, bucket.orders / peak * 100)}%` } as CSSProperties}><small>{bucket.start}:00</small><b>{bucket.orders}</b><i /><em>{t('dashboard.orders.count', { count: bucket.orders })}</em></span>)}</div></article><article className="shift-board__payments"><div className="shift-board__section-heading"><div><span>{t('dashboard.tender.kicker')}</span><h2>{t('dashboard.simple.paymentMix')}</h2></div></div>{report.tenderMix.map((tender) => <div key={tender.tenderType}><span><i /><strong>{tenderLabel(tender.tenderType, t)}</strong><small>{t('dashboard.tender.transactions', { count: tender.capturedCount })}</small></span><b>{money(tender.netMinor)}</b></div>)}</article></div>
        <div className="shift-board__business-grid"><article><div className="shift-board__section-heading"><div><span>{t('dashboard.simple.orderType')}</span><h2>{t('dashboard.simple.orderType')}</h2></div></div>{orderTypes.map((type) => { const count = fulfillment[type]; const share = fulfillmentTotal ? Math.round(count / fulfillmentTotal * 100) : 0; return <div className="shift-board__type" key={type}><span>{t(`order.type.${type}` as MessageKey)}</span><b>{t('dashboard.orders.count', { count })}</b><i><em style={{ width: `${share}%` }} /></i></div>; })}</article><article><div className="shift-board__section-heading"><div><span>{t('dashboard.leakage.kicker')}</span><h2>{t('dashboard.simple.leakage')}</h2></div></div><div className="shift-board__facts"><span>{t('dashboard.leakage.cancelled')}<b>{money(report.leakage.cancelledOrderValueMinor)}</b></span><span>{t('dashboard.leakage.refunds')}<b>{money(report.leakage.refundMinor)}</b></span><span>{t('dashboard.leakage.promotions')}<b>{money(report.leakage.promotionDiscountMinor)}</b></span></div></article><article className="shift-board__drivers"><div className="shift-board__section-heading"><div><span>{t('dashboard.items.kicker')}</span><h2>{t('dashboard.simple.topItems')}</h2></div></div>{report.topItems.slice(0, 5).map((item, index) => <div key={`${item.menuItemId}-${item.name}`}><b>{index + 1}</b><span><strong>{catalogById.get(item.menuItemId ?? '') ? localize(catalogById.get(item.menuItemId ?? '')!.name, preferences.locale) : item.name}</strong><small>{t('dashboard.items.sold', { count: item.quantity, value: money(item.lineValueMinor) })}</small></span></div>)}</article></div>
      </>}
    </div>}

    {mode === 'system' && <div className="shift-board__system" role="tabpanel">
      {!report ? <div className="shift-board__unavailable"><Icon name="cloud" /><strong>{t('dashboard.daily.detailsUnavailable')}</strong><span>{t('dashboard.daily.detailsUnavailableHelp')}</span></div> : <div className="shift-board__system-grid"><article><div className="shift-board__section-heading"><div><span>{t('dashboard.reconciliation.kicker')}</span><h2>{t('dashboard.reconciliation.title')}</h2><p>{t('dashboard.reconciliation.subtitle')}</p></div></div><div className="shift-board__system-metrics"><span>{t('dashboard.reconciliation.orders')}<b>{report.orders.total}</b></span><span>{t('dashboard.reconciliation.receipts')}<b>{report.sales.receiptedOrderCount}</b></span><span>{t('dashboard.reconciliation.zeroValue')}<b>{report.orders.unpriced}</b></span><span>{t('dashboard.reconciliation.cancelled')}<b>{report.orders.cancelled}</b></span></div></article><article><div className="shift-board__section-heading"><div><span>{t('dashboard.source.derived')}</span><h2>{t('dashboard.dataQuality.warning', { count: qualityCount })}</h2><p>{t('dashboard.dataQuality.warningHelp')}</p></div></div><div className="shift-board__system-list">{Object.entries(report.dataQuality).filter(([, count]) => count > 0).map(([field, count]) => <span className="watch" key={field}><strong>{readable(field)}</strong><b>{count}</b></span>)}{qualityCount === 0 && <span className="good"><strong>{t('dashboard.exceptions.clear')}</strong><Icon name="check" /></span>}</div></article><article><div className="shift-board__section-heading"><div><span>{t('dashboard.sync.aria')}</span><h2>{t('dashboard.reconciliation.otherUnavailable', { count: report.unavailableFields.length })}</h2></div></div><div className="shift-board__system-list">{report.unavailableFields.length ? report.unavailableFields.map((field) => <span key={field}><strong>{readable(field)}</strong><small>{t('dashboard.reconciliation.otherUnavailableHelp')}</small></span>) : <span className="good"><strong>{t('dashboard.exceptions.clear')}</strong><Icon name="check" /></span>}</div></article></div>}
    </div>}
  </section>;
}
