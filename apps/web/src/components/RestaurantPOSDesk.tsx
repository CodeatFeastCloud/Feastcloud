import { useMemo, useState } from 'react';
import {
  actOnPrintJob, checkoutPOS, type CashShift, type KitchenPrintJob, type MenuModifierOption,
  type MenuStudio, type MenuStudioItem, type PickupToken, transitionPickupToken, type Sellability,
} from '../domain/coreCommerce';
import type { MessageKey } from '../i18n/messages';

type BasketLine = { item: MenuStudioItem; quantity: number; optionIds: string[] };

function money(value: number, currency = 'INR') {
  return new Intl.NumberFormat('en-IN', { style: 'currency', currency, maximumFractionDigits: 2 }).format(value / 100);
}

export function RestaurantPOSDesk({
  api, tenantId, outletId, studios, sellability, cashShift, printJobs, pickupTokens, t, onRefresh, onError,
}: {
  api: string; tenantId: string; outletId: string; studios: MenuStudio[]; sellability: Sellability[];
  cashShift?: CashShift; printJobs: KitchenPrintJob[]; pickupTokens: PickupToken[];
  t: (key: MessageKey, replacements?: Record<string, string | number>) => string;
  onRefresh: () => Promise<void>; onError: (value: string) => void;
}) {
  const [basket, setBasket] = useState<BasketLine[]>([]);
  const [selectedID, setSelectedID] = useState('');
  const [paying, setPaying] = useState(false);
  const studio = studios.find((candidate) => candidate.status === 'published' && candidate.current) ?? studios.find((candidate) => candidate.current);
  const version = studio?.current;
  const items = version?.items.filter((item) => item.active) ?? [];
  const modifierGroups = version?.modifiers ?? [];
  const selected = items.find((item) => item.menuItemId === selectedID) ?? items[0];
  const sellabilityByItem = useMemo(() => new Map(sellability.map((item) => [item.menuItemId, item])), [sellability]);
  const optionByID = useMemo(() => new Map(modifierGroups.flatMap((group) => group.options.map((option) => [option.id, option] as const))), [modifierGroups]);
  const lineTotal = (line: BasketLine) => (line.item.priceMinor + line.optionIds.reduce((total, id) => total + (optionByID.get(id)?.priceDeltaMinor ?? 0), 0)) * line.quantity;
  const total = basket.reduce((sum, line) => sum + lineTotal(line), 0);

  const add = (item: MenuStudioItem) => {
    if (!sellabilityByItem.get(item.menuItemId)?.available) return;
    setBasket((current) => {
      const existing = current.find((line) => line.item.menuItemId === item.menuItemId && line.optionIds.length === 0);
      return existing ? current.map((line) => line === existing ? { ...line, quantity: line.quantity + 1 } : line) : [...current, { item, quantity: 1, optionIds: [] }];
    });
    setSelectedID(item.menuItemId);
  };
  const setOption = (option: MenuModifierOption, groupID: string) => {
    if (!selected) return;
    setBasket((current) => current.map((line) => {
      if (line.item.menuItemId !== selected.menuItemId) return line;
      const group = modifierGroups.find((candidate) => candidate.id === groupID);
      const withoutGroup = line.optionIds.filter((id) => !group?.options.some((candidate) => candidate.id === id));
      return { ...line, optionIds: line.optionIds.includes(option.id) ? withoutGroup : [...withoutGroup, option.id] };
    }));
  };
  const pay = async (tenderType: 'cash' | 'upi' | 'card_terminal') => {
    if (!version || basket.length === 0 || paying || (tenderType === 'cash' && !cashShift)) return;
    setPaying(true);
    try {
      await checkoutPOS(api, tenantId, outletId, version.id, basket.map((line) => ({ menuItemId: line.item.menuItemId, quantity: line.quantity, modifierOptionIds: line.optionIds })), total, tenderType, cashShift?.id ?? '');
      setBasket([]);
      await onRefresh();
    } catch { onError(t('commerce.pos.checkoutFailed')); }
    finally { setPaying(false); }
  };
  const mutateJob = async (job: KitchenPrintJob, action: 'acknowledged' | 'reprinted') => {
    try { await actOnPrintJob(api, tenantId, outletId, job.id, action); await onRefresh(); } catch { onError(t('commerce.pos.printFailed')); }
  };
  const mutateToken = async (token: PickupToken, status: 'called' | 'collected') => {
    try { await transitionPickupToken(api, tenantId, outletId, token, status); await onRefresh(); } catch { onError(t('commerce.pos.tokenFailed')); }
  };

  if (!studio || !version) return <article className="pos-empty"><span>01</span><div><b>{t('commerce.pos.noMenuTitle')}</b><p>{t('commerce.pos.noMenuHelp')}</p></div></article>;

  return <section className="restaurant-pos" aria-label={t('commerce.pos.counter')}>
    <header className="pos-title"><div><span>{t('commerce.pos.liveCounter')}</span><h2>{studio.name}</h2><p>{t('commerce.pos.version', { version: version.versionNumber })}</p></div><strong>{t('commerce.pos.itemCount', { count: basket.reduce((sum, line) => sum + line.quantity, 0) })}</strong></header>
    <div className="pos-layout">
      <section className="pos-menu" aria-label={t('menu.items')}>
        {items.map((item) => {
          const availability = sellabilityByItem.get(item.menuItemId);
          return <button key={item.menuItemId} type="button" className={`pos-menu-item ${selected?.menuItemId === item.menuItemId ? 'selected' : ''}`} disabled={!availability?.available} onClick={() => add(item)}>
            <span className="pos-item-initial">{item.displayName.slice(0, 1)}</span><span><b>{item.displayName}</b><small>{availability?.available ? t('commerce.pos.ready') : availability?.reason ?? t('commerce.pos.notSellable')}</small></span><strong>{money(item.priceMinor, item.currency)}</strong>
          </button>;
        })}
      </section>
      <aside className="pos-cart" aria-live="polite">
        <div className="pos-cart-head"><h3>{t('commerce.pos.currentOrder')}</h3><button type="button" onClick={() => setBasket([])} disabled={!basket.length}>{t('commerce.pos.clear')}</button></div>
        {basket.length === 0 ? <p className="pos-empty-cart">{t('commerce.pos.emptyCart')}</p> : basket.map((line, index) => <div className="pos-line" key={`${line.item.menuItemId}-${index}`}><span><b>{line.quantity}× {line.item.displayName}</b><small>{line.optionIds.map((id) => optionByID.get(id)?.name).filter(Boolean).join(', ') || t('commerce.pos.standard')}</small></span><strong>{money(lineTotal(line), line.item.currency)}</strong><button type="button" aria-label={t('commerce.pos.remove', { item: line.item.displayName })} onClick={() => setBasket((current) => current.filter((_, lineIndex) => lineIndex !== index))}>×</button></div>)}
        <div className="pos-total"><span>{t('commerce.pos.total')}</span><strong>{money(total)}</strong></div>
        <div className="pos-pay"><button type="button" disabled={!basket.length || paying || !cashShift} onClick={() => void pay('cash')}>{cashShift ? t('commerce.pos.cash') : t('commerce.pos.openShift')}</button><button type="button" disabled={!basket.length || paying} onClick={() => void pay('upi')}>{paying ? t('commerce.pos.finishing') : t('commerce.pos.upi')}</button><button type="button" disabled={!basket.length || paying} onClick={() => void pay('card_terminal')}>{t('commerce.pos.card')}</button></div>
      </aside>
      <section className="pos-modifiers" aria-label={t('menu.addons')}>
        <h3>{selected ? t('commerce.pos.customize', { item: selected.displayName }) : t('commerce.pos.selectItem')}</h3>
        {selected && modifierGroups.filter((group) => selected.modifierGroupIds.includes(group.id)).map((group) => <div className="pos-modifier-group" key={group.id}><span><b>{group.name}</b><small>{t(group.required ? 'commerce.pos.requiredOptions' : 'commerce.pos.optionalOptions', { count: group.selectionMax })}</small></span><div>{group.options.filter((option) => option.active).map((option) => <button type="button" key={option.id} className={basket.find((line) => line.item.menuItemId === selected.menuItemId)?.optionIds.includes(option.id) ? 'selected' : ''} onClick={() => setOption(option, group.id)}>{option.name}{option.priceDeltaMinor ? ` +${money(option.priceDeltaMinor)}` : ''}</button>)}</div></div>)}
      </section>
    </div>
    <section className="pos-fulfilment"><div><span>{t('commerce.pos.printQueue')}</span>{printJobs.filter((job) => job.status === 'queued').slice(0, 3).map((job) => <p key={job.id}><b>{job.printerRoute}</b><button type="button" onClick={() => void mutateJob(job, 'acknowledged')}>{t('commerce.pos.acknowledge')}</button></p>)}{!printJobs.some((job) => job.status === 'queued') && <p>{t('commerce.pos.printerClear')}</p>}</div><div><span>{t('commerce.pos.pickupDesk')}</span>{pickupTokens.filter((token) => token.status === 'issued' || token.status === 'called').slice(0, 3).map((token) => <p key={token.id}><b>#{token.token}</b><button type="button" onClick={() => void mutateToken(token, token.status === 'issued' ? 'called' : 'collected')}>{t(token.status === 'issued' ? 'commerce.pos.call' : 'commerce.pos.collected')}</button></p>)}{!pickupTokens.some((token) => token.status === 'issued' || token.status === 'called') && <p>{t('commerce.pos.noGuests')}</p>}</div></section>
  </section>;
}
