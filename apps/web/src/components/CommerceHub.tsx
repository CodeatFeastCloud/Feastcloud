import { FormEvent, useCallback, useEffect, useState } from 'react';
import type { KitchenSnapshot } from '../domain/types';
import type { MessageKey } from '../i18n/messages';
import {
  captureTender, closeCash, closeSession, commerceApiBase, createTable,
  fetchAvailability, fetchCashShifts, fetchConnectorInbox, fetchMenuStudios, fetchOrders, fetchPickupTokens, fetchPrintJobs, fetchSellability, fetchSessions, fetchTables,
  openCash, openSession, setAvailability, settleToday, transitionTable,
  type Availability, type CashShift, type ConnectorInboxOrder, type CoreOrder, type DiningSession, type DiningTable, type KitchenPrintJob, type MenuStudio, type PickupToken, type Sellability,
} from '../domain/coreCommerce';
import { RestaurantPOSDesk } from './RestaurantPOSDesk';
import { AggregatorInbox } from './AggregatorInbox';
import { TransferDesk } from './TransferDesk';

const api = commerceApiBase((import.meta.env.VITE_CORE_URL as string | undefined)?.trim());

export function CommerceHub({ snapshot, t }: { snapshot: KitchenSnapshot; t: (k: MessageKey, replacements?: Record<string, string | number>) => string }) {
  const [availability, setAvailabilityRows] = useState<Availability[]>([]);
  const [sellability, setSellability] = useState<Sellability[]>([]);
  const [tables, setTables] = useState<DiningTable[]>([]);
  const [sessions, setSessions] = useState<DiningSession[]>([]);
  const [shifts, setShifts] = useState<CashShift[]>([]);
  const [orders, setOrders] = useState<CoreOrder[]>([]);
  const [studios, setStudios] = useState<MenuStudio[]>([]);
  const [printJobs, setPrintJobs] = useState<KitchenPrintJob[]>([]);
  const [pickupTokens, setPickupTokens] = useState<PickupToken[]>([]);
  const [connectorInbox, setConnectorInbox] = useState<ConnectorInboxOrder[]>([]);
  const [amounts, setAmounts] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [focus, setFocus] = useState<'pos' | 'inbox' | 'stock' | 'sell' | 'floor' | 'cash'>('pos');

  const load = useCallback(async () => {
    if (!api) { setError(t('commerce.notConfigured')); return; }
    try {
      const [v, effective, ta, se, sh, or, menu, print, tokens, inbox] = await Promise.all([
        fetchAvailability(api, snapshot.organizationId, snapshot.outletId),
        fetchSellability(api, snapshot.organizationId, snapshot.outletId),
        fetchTables(api, snapshot.organizationId, snapshot.outletId),
        fetchSessions(api, snapshot.organizationId, snapshot.outletId),
        fetchCashShifts(api, snapshot.organizationId, snapshot.outletId),
        fetchOrders(api, snapshot.organizationId, snapshot.outletId),
        fetchMenuStudios(api, snapshot.organizationId, snapshot.outletId),
        fetchPrintJobs(api, snapshot.organizationId, snapshot.outletId),
        fetchPickupTokens(api, snapshot.organizationId, snapshot.outletId),
        fetchConnectorInbox(api, snapshot.organizationId, snapshot.outletId),
      ]);
      setAvailabilityRows(v);
      setSellability(effective);
      setTables(ta);
      setSessions(se);
      setShifts(sh);
      setOrders(or);
      setStudios(menu);
      setPrintJobs(print);
      setPickupTokens(tokens);
      setConnectorInbox(inbox);
      setError('');
    } catch { setError(t('commerce.unavailable')); }
  }, [snapshot.organizationId, snapshot.outletId, t]);

  useEffect(() => { void load(); }, [load]);
  const run = async (name: string, action: () => Promise<unknown>) => {
    setBusy(name);
    try { await action(); await load(); } catch { setError(t('commerce.failed')); } finally { setBusy(''); }
  };
  const tableForm = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = event.currentTarget;
    const label = String(new FormData(form).get('label'));
    void run('table', () => createTable(api!, snapshot.organizationId, snapshot.outletId, label));
    form.reset();
  };
  const cashForm = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    void run('cash', () => openCash(api!, snapshot.organizationId, snapshot.outletId, String(data.get('label')), Math.round(Number(data.get('float')) * 100)));
    form.reset();
  };
  const openShift = shifts.find((x) => x.status === 'open');
  const openSessions = sessions.filter((x) => x.status === 'open');
  const tenderAmount = (order: CoreOrder) => Math.round(Number(amounts[order.id] ?? order.total.minorUnits / 100) * 100);
  const tableAction = (table: DiningTable, session?: DiningSession) => {
    if (session) return closeSession(api!, snapshot.organizationId, snapshot.outletId, session);
    if (table.status === 'cleaning' || table.status === 'disabled') return transitionTable(api!, snapshot.organizationId, snapshot.outletId, table, 'available');
    return openSession(api!, snapshot.organizationId, snapshot.outletId, table.id, 2);
  };

  return <section className="commerce-page">
    <header className="page-heading"><div><span className="eyebrow">{t('commerce.eyebrow')}</span><h1>{t('commerce.title')}</h1><p>{t('commerce.subtitle')}</p></div><button className="planning-generate" onClick={() => void run('settle', () => settleToday(api!, snapshot.organizationId, snapshot.outletId))}>{t('commerce.settle')}</button></header>
    {error && <p className="inventory-alert">{error}</p>}
    <nav className="commerce-focus" aria-label={t('commerce.focusLabel')}>{(['pos','inbox','stock','sell','floor','cash'] as const).map(value=><button key={value} type="button" className={focus===value?'active':''} aria-pressed={focus===value} onClick={()=>setFocus(value)}>{value === 'inbox' ? `${t('commerce.focus.inbox')}${connectorInbox.filter((order) => order.status === 'received' || order.status === 'needs_review').length ? ` · ${connectorInbox.filter((order) => order.status === 'received' || order.status === 'needs_review').length}` : ''}` : t(`commerce.focus.${value}` as MessageKey)}</button>)}</nav>
    <div className="commerce-grid" data-focus={focus}>
      <div className="commerce-pos" data-area="pos"><RestaurantPOSDesk api={api!} tenantId={snapshot.organizationId} outletId={snapshot.outletId} studios={studios} sellability={sellability} cashShift={openShift} printJobs={printJobs} pickupTokens={pickupTokens} t={t} onRefresh={load} onError={setError} /></div>
      <div className="commerce-pos" data-area="inbox"><AggregatorInbox api={api!} tenantId={snapshot.organizationId} outletId={snapshot.outletId} orders={connectorInbox} t={t} onRefresh={load} onError={setError} /></div>
      <div className="commerce-pos" data-area="stock"><TransferDesk api={api!} tenantId={snapshot.organizationId} outletId={snapshot.outletId} t={t} onError={setError} /></div>
      <article className="commerce-card" data-area="sell"><h2>{t('commerce.availability')}</h2>{availability.map((x) => { const effective = sellability.find((value) => value.menuItemId === x.menuItemId); const availableNow = effective?.available ?? x.available; return <button className="commerce-switch" key={x.menuItemId} onClick={() => void run(x.menuItemId, () => setAvailability(api!, snapshot.organizationId, snapshot.outletId, x))}><span><b>{x.menuItemName}</b><small>{effective?.reason ?? x.reason}</small></span><i className={availableNow ? 'on' : ''}>{availableNow ? t('commerce.available') : t('commerce.86')}</i></button>; })}</article>
      <article className="commerce-card" data-area="floor"><h2>{t('commerce.tables')}</h2><form className="commerce-form" onSubmit={tableForm}><input name="label" required placeholder={t('commerce.tableLabel')} /><button>{t('commerce.add')}</button></form><div className="table-grid">{tables.map((x) => { const session = openSessions.find((s) => s.tableId === x.id); return <button key={x.id} className={x.status} disabled={!!busy} onClick={() => void run(x.id, () => tableAction(x, session))}><b>{x.label}</b><small>{x.status}</small></button>; })}</div></article>
      <article className="commerce-card" data-area="cash"><h2>{t('commerce.cash')}</h2>{!openShift ? <form className="commerce-form" onSubmit={cashForm}><input name="label" required placeholder="POS 1" /><input name="float" required type="number" min="0" placeholder={t('commerce.float')} /><button>{t('commerce.open')}</button></form> : <div className="cash-summary"><b>{openShift.registerLabel}</b><strong>₹{(openShift.expectedCashMinor / 100).toFixed(2)}</strong><button onClick={() => void run('close', () => closeCash(api!, snapshot.organizationId, snapshot.outletId, openShift, openShift.expectedCashMinor))}>{t('commerce.close')}</button></div>}</article>
      <article className="commerce-card span-two" data-area="sell cash"><h2>{t('commerce.checkout')}</h2>{orders.slice(0, 12).map((order) => <div className="checkout-row" key={order.id}><div><b>{order.lines.map((x) => `${x.quantity}× ${x.name}`).join(', ')}</b><small>{new Date(order.placedAt).toLocaleString()}</small></div><strong>₹{(order.total.minorUnits / 100).toFixed(2)}</strong><label className="tender-amount"><span>{t('commerce.amount')}</span><input type="number" min="0.01" step="0.01" value={amounts[order.id] ?? (order.total.minorUnits / 100).toFixed(2)} onChange={(event) => setAmounts((current) => ({ ...current, [order.id]: event.target.value }))} /></label><button disabled={!openShift || !!busy || tenderAmount(order) < 1} onClick={() => void run(order.id, () => captureTender(api!, snapshot.organizationId, snapshot.outletId, order, 'cash', openShift!.id, tenderAmount(order)))}>{t('commerce.payCash')}</button><button disabled={!!busy || tenderAmount(order) < 1} onClick={() => void run(order.id, () => captureTender(api!, snapshot.organizationId, snapshot.outletId, order, 'upi', '', tenderAmount(order)))}>{t('commerce.payUpi')}</button></div>)}</article>
    </div>
  </section>;
}
