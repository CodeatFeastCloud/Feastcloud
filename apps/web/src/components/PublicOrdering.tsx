import { FormEvent, useEffect, useMemo, useState } from 'react';
import { createUuidV7 } from '../domain/kitchen';
import type { Locale } from '../domain/types';
import type { MessageKey } from '../i18n/messages';
import './public-ordering.css';

type MenuItem = { menuItemId: string; displayName: string; description?: string; active: boolean; priceMinor: number; currency: string };
type Sellability = { menuItemId: string; available: boolean; reason?: string };
type MenuResponse = { menu: { name: string; currentVersionId: string; current?: { items: MenuItem[] } }; sellability: Sellability[]; paymentState: string };

const core = (import.meta.env.VITE_CORE_URL as string | undefined)?.trim() ?? '';
type Translate = (key: MessageKey, replacements?: Record<string, string | number>) => string;

export function PublicOrdering({ slug, locale, t }: { slug: string; locale: Locale; t: Translate }) {
  const query = useMemo(() => new URLSearchParams(window.location.search), []);
  const tenantId = query.get('tenant') ?? '';
  const outletId = query.get('outlet') ?? '';
  const [menu, setMenu] = useState<MenuResponse>();
  const [cart, setCart] = useState<Record<string, number>>({});
  const [guestName, setGuestName] = useState('');
  const [guestPhone, setGuestPhone] = useState('');
  const [note, setNote] = useState('');
  const [status, setStatus] = useState<'loading' | 'ready' | 'failed' | 'sending' | 'sent'>('loading');
  const [tracking, setTracking] = useState('');
  const [requestId] = useState(() => createUuidV7());
  const [clientRequestId] = useState(() => createUuidV7());
  const [orderError, setOrderError] = useState('');
  const base = `${core}/api/v1/public/ordering/${encodeURIComponent(slug)}?tenantId=${encodeURIComponent(tenantId)}&outletId=${encodeURIComponent(outletId)}`;

  useEffect(() => { void (async () => { try { const response = await fetch(base, { cache: 'no-store' }); if (!response.ok) throw new Error(); setMenu((await response.json() as { data: MenuResponse }).data); setStatus('ready'); } catch { setStatus('failed'); } })(); }, [base]);
  const items = menu?.menu.current?.items.filter((item) => item.active) ?? [];
  const available = new Map(menu?.sellability.map((item) => [item.menuItemId, item]) ?? []);
  const total = items.reduce((sum, item) => sum + item.priceMinor * (cart[item.menuItemId] ?? 0), 0);
  const change = (id: string, delta: number) => setCart((current) => ({ ...current, [id]: Math.max(0, (current[id] ?? 0) + delta) }));
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!menu || total < 1 || status === 'sending') return;
    setStatus('sending');
    setOrderError('');
    try {
      const response = await fetch(`${core}/api/v1/public/ordering/${encodeURIComponent(slug)}/requests?tenantId=${encodeURIComponent(tenantId)}&outletId=${encodeURIComponent(outletId)}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id: requestId, clientRequestId, menuVersionId: menu.menu.currentVersionId, guestName, guestPhone, note, lines: Object.entries(cart).filter(([, quantity]) => quantity > 0).map(([menuItemId, quantity]) => ({ menuItemId, quantity })) }) });
      if (!response.ok) throw new Error(); const body = await response.json() as { data: { trackingCode: string } }; setTracking(body.data.trackingCode); setStatus('sent');
    } catch { setOrderError(t('publicOrder.sendFailed')); setStatus('ready'); }
  };

  const money = (amount: number, currency = 'INR') => new Intl.NumberFormat(locale, { style: 'currency', currency }).format(amount / 100);
  if (status === 'loading') return <main className="guest-order-shell"><p>{t('publicOrder.loading')}</p></main>;
  if (status === 'failed') return <main className="guest-order-shell"><section className="guest-error"><b>{t('publicOrder.unavailable')}</b><p>{t('publicOrder.unavailableHelp')}</p></section></main>;
  if (status === 'sent') return <main className="guest-order-shell"><section className="guest-confirm"><span>✓</span><p>{t('publicOrder.received')}</p><h1>{t('publicOrder.show')} <b>#{tracking}</b> {t('publicOrder.atCounter')}</h1><small>{t('publicOrder.receivedHelp')}</small></section></main>;
  const selectedCount = Object.values(cart).reduce((sum, quantity) => sum + quantity, 0);
  return <main className="guest-order-shell"><header className="guest-order-header"><span>{t('publicOrder.eyebrow')}</span><h1>{menu?.menu.name}</h1><p>{t('publicOrder.subtitle')}</p></header><form className="guest-order-layout" onSubmit={submit}><section className="guest-menu"><h2>{t('publicOrder.craving')}</h2>{items.map((item) => { const quantity = cart[item.menuItemId] ?? 0; const state = available.get(item.menuItemId); return <article key={item.menuItemId} className={!state?.available ? 'sold-out' : ''}><span>{item.displayName.slice(0, 1)}</span><div><b>{item.displayName}</b><small>{state?.available ? item.description || t('publicOrder.fresh') : state?.reason || t('publicOrder.unavailableNow')}</small><strong>{money(item.priceMinor, item.currency)}</strong></div>{quantity ? <div className="guest-stepper"><button type="button" aria-label={t('a11y.decrease', { item: item.displayName })} onClick={() => change(item.menuItemId, -1)}>−</button><b>{quantity}</b><button type="button" aria-label={t('a11y.increase', { item: item.displayName })} onClick={() => change(item.menuItemId, 1)}>+</button></div> : <button className="guest-add" type="button" disabled={!state?.available} onClick={() => change(item.menuItemId, 1)}>{state?.available ? t('publicOrder.add') : t('publicOrder.soldOut')}</button>}</article>; })}</section><aside className="guest-cart"><h2>{t('publicOrder.yourOrder')}</h2><p>{t('publicOrder.selectedCount', { count: selectedCount })}</p><label>{t('publicOrder.name')}<input value={guestName} maxLength={160} onChange={(event) => setGuestName(event.target.value)} /></label><label>{t('publicOrder.mobile')}<input value={guestPhone} maxLength={40} inputMode="tel" onChange={(event) => setGuestPhone(event.target.value)} /></label><label>{t('publicOrder.note')}<input value={note} maxLength={500} onChange={(event) => setNote(event.target.value)} placeholder={t('publicOrder.noteExample')} /></label><div className="guest-total"><span>{t('publicOrder.total')}</span><strong>{money(total)}</strong></div>{orderError && <p className="guest-order-error" role="alert">{orderError}</p>}<button disabled={total < 1 || status === 'sending'}>{status === 'sending' ? t('publicOrder.sending') : t('publicOrder.place')}</button><small>{t('publicOrder.paymentHelp')}</small></aside></form></main>;
}
