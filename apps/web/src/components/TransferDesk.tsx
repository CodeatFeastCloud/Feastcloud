import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import {
  createStockTransfer, fetchIngredientReferences, fetchOutletReferences, fetchStockTransfers,
  fetchReplenishmentSuggestions, saveReplenishmentRule, transitionStockTransfer, type IngredientReference, type OutletReference, type ReplenishmentSuggestion, type StockTransfer,
} from '../domain/coreCommerce';
import type { MessageKey } from '../i18n/messages';
import './transfer-desk.css';
import './replenishment.css';

type Action = 'approved' | 'dispatched' | 'received' | 'cancelled';

type Translate = (key: MessageKey, replacements?: Record<string, string | number>) => string;

export function TransferDesk({ api, tenantId, outletId, t, onError }: { api: string; tenantId: string; outletId: string; t: Translate; onError: (value: string) => void }) {
  const [transfers, setTransfers] = useState<StockTransfer[]>([]);
  const [outlets, setOutlets] = useState<OutletReference[]>([]);
  const [ingredients, setIngredients] = useState<IngredientReference[]>([]);
  const [suggestions, setSuggestions] = useState<ReplenishmentSuggestion[]>([]);
  const [busy, setBusy] = useState('');
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [nextTransfers, nextOutlets, nextIngredients, nextSuggestions] = await Promise.all([
        fetchStockTransfers(api, tenantId, outletId), fetchOutletReferences(api, tenantId), fetchIngredientReferences(api, tenantId), fetchReplenishmentSuggestions(api, tenantId, outletId),
      ]);
      setTransfers(nextTransfers);
      setOutlets(nextOutlets.filter((outlet) => outlet.active));
      setIngredients(nextIngredients.filter((ingredient) => ingredient.active));
      setSuggestions(nextSuggestions);
    } catch {
      onError(t('commerce.transfer.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [api, onError, outletId, t, tenantId]);

  useEffect(() => { void load(); }, [load]);
  const outletName = useMemo(() => new Map(outlets.map((outlet) => [outlet.id, outlet.name])), [outlets]);
  const ingredientName = useMemo(() => new Map(ingredients.map((ingredient) => [ingredient.id, ingredient.name])), [ingredients]);
  const destinations = outlets.filter((outlet) => outlet.id !== outletId);

  const request = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const destination = String(data.get('destination') ?? '');
    const ingredient = String(data.get('ingredient') ?? '');
    const quantity = Number(data.get('quantity'));
    const notes = String(data.get('notes') ?? '').trim();
    if (!destination || !ingredient || !Number.isFinite(quantity) || quantity <= 0) return;
    setBusy('request');
    try {
      await createStockTransfer(api, tenantId, outletId, outletId, destination, ingredient, quantity, notes);
      event.currentTarget.reset();
      await load();
    } catch {
      onError(t('commerce.transfer.requestFailed'));
    } finally {
      setBusy('');
    }
  };

  const saveRule = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const source = String(data.get('source') ?? '');
    const ingredient = String(data.get('ingredient') ?? '');
    const reorder = Number(data.get('reorder'));
    const target = Number(data.get('target'));
    if (!source || !ingredient || !Number.isFinite(reorder) || !Number.isFinite(target) || reorder < 0 || target <= reorder) return;
    setBusy('rule');
    try {
      await saveReplenishmentRule(api, tenantId, outletId, ingredient, source, reorder, target);
      event.currentTarget.reset();
      await load();
    } catch {
      onError(t('commerce.transfer.ruleFailed'));
    } finally {
      setBusy('');
    }
  };

  const draftSuggestion = async (suggestion: ReplenishmentSuggestion) => {
    if (suggestion.suggestedQuantityBase <= 0) return;
    setBusy(`suggest:${suggestion.ingredientId}`);
    try {
      await createStockTransfer(api, tenantId, outletId, suggestion.sourceOutletId, outletId, suggestion.ingredientId, suggestion.suggestedQuantityBase, t('commerce.transfer.signalNote'));
      await load();
    } catch {
      onError(t('commerce.transfer.suggestionFailed'));
    } finally {
      setBusy('');
    }
  };

  const act = async (transfer: StockTransfer, action: Action) => {
    const quantities = action === 'dispatched'
      ? transfer.lines.map((line) => ({ ingredientId: line.ingredientId, quantityBase: line.quantityBase }))
      : action === 'received'
        ? transfer.lines.map((line) => ({ ingredientId: line.ingredientId, quantityBase: line.dispatchedQuantityBase ?? 0 }))
        : [];
    if (quantities.some((line) => line.quantityBase <= 0)) return;
    setBusy(`${transfer.id}:${action}`);
    try {
      await transitionStockTransfer(api, tenantId, outletId, transfer, action, quantities);
      await load();
    } catch {
      onError(t('commerce.transfer.actionFailed'));
    } finally {
      setBusy('');
    }
  };

  return <section className="transfer-desk" aria-label={t('commerce.transfer.label')}>
    <header className="transfer-title"><div><span>{t('commerce.transfer.eyebrow')}</span><h2>{t('commerce.transfer.title')}</h2><p>{t('commerce.transfer.help')}</p></div><strong>{t('commerce.transfer.activeCount', { count: transfers.filter((transfer) => transfer.status === 'requested' || transfer.status === 'approved' || transfer.status === 'dispatched').length })}</strong></header>
    <div className="transfer-layout">
      <form className="transfer-request" onSubmit={(event) => void request(event)}>
        <div><span className="transfer-step">1</span><b>{t('commerce.transfer.ask')}</b><small>{t('commerce.transfer.askHelp')}</small></div>
        <label>{t('commerce.transfer.sendTo')}<select name="destination" required defaultValue="" disabled={!destinations.length || !!busy}><option value="" disabled>{t('commerce.transfer.selectOutlet')}</option>{destinations.map((outlet) => <option key={outlet.id} value={outlet.id}>{outlet.name}</option>)}</select></label>
        <label>{t('commerce.transfer.ingredient')}<select name="ingredient" required defaultValue="" disabled={!ingredients.length || !!busy}><option value="" disabled>{t('commerce.transfer.selectIngredient')}</option>{ingredients.map((ingredient) => <option key={ingredient.id} value={ingredient.id}>{ingredient.name}</option>)}</select></label>
        <label>{t('commerce.transfer.quantity')}<input name="quantity" type="number" min="0.001" step="0.001" required placeholder={t('commerce.transfer.quantityExample')} disabled={!!busy} /></label>
        <label>{t('commerce.transfer.packNote')}<input name="notes" maxLength={500} placeholder={t('commerce.transfer.packNoteExample')} disabled={!!busy} /></label>
        <button disabled={!!busy || !destinations.length || !ingredients.length}>{busy === 'request' ? t('commerce.transfer.saving') : t('commerce.transfer.create')}</button>
        {!destinations.length && <small className="transfer-hint">{t('commerce.transfer.noDestination')}</small>}
      </form>
      <div className="transfer-queue">
        <section className="transfer-replenishment"><header><div><span>{t('commerce.transfer.replenishment')}</span><b>{t('commerce.transfer.lowStock')}</b></div><small>{t('commerce.transfer.lowStockHelp')}</small></header>{suggestions.length === 0 ? <p>{t('commerce.transfer.noSignals')}</p> : suggestions.map((suggestion) => <article key={suggestion.ingredientId}><div><b>{suggestion.ingredientName}</b><small>{t('commerce.transfer.stockLevels', { onHand: suggestion.onHandBase, unit: suggestion.unitSymbol, alert: suggestion.reorderPointBase, target: suggestion.targetLevelBase })}</small></div><span><strong>{suggestion.status === 'ready' ? t('commerce.transfer.moveQuantity', { quantity: suggestion.suggestedQuantityBase }) : suggestion.status === 'source_short' ? t('commerce.transfer.onlyAvailable', { quantity: suggestion.suggestedQuantityBase }) : t('commerce.transfer.sourceEmpty')}</strong>{suggestion.suggestedQuantityBase > 0 && <button type="button" disabled={!!busy} onClick={() => void draftSuggestion(suggestion)}>{busy === `suggest:${suggestion.ingredientId}` ? t('commerce.transfer.drafting') : t('commerce.transfer.draftRequest')}</button>}</span></article>)}</section>
        <div className="transfer-queue-heading"><div><b>{t('commerce.transfer.queue')}</b><small>{loading ? t('commerce.transfer.checking') : t('commerce.transfer.auditable')}</small></div><button type="button" className="transfer-refresh" onClick={() => void load()} disabled={!!busy}>{t('common.refresh')}</button></div>
        {!loading && transfers.length === 0 && <div className="transfer-empty">{t('commerce.transfer.empty')}</div>}
        {transfers.slice(0, 30).map((transfer) => {
          const origin = transfer.sourceOutletId === outletId;
          const destination = transfer.destinationOutletId === outletId;
          const action = transfer.status === 'requested' && origin ? 'approved' : transfer.status === 'approved' && origin ? 'dispatched' : transfer.status === 'dispatched' && destination ? 'received' : undefined;
          const actionText = action ? t(`commerce.transfer.action.${action}` as MessageKey) : '';
          return <article className={`transfer-row status-${transfer.status}`} key={transfer.id}>
            <div className="transfer-row-head"><span><b>{outletName.get(transfer.sourceOutletId) ?? t('commerce.transfer.origin')} <i>→</i> {outletName.get(transfer.destinationOutletId) ?? t('commerce.transfer.destination')}</b><small>{t('commerce.transfer.requestedBy', { date: new Date(transfer.requestedAt).toLocaleString(), name: transfer.requestedBy })}</small></span><em>{t(`commerce.transfer.status.${transfer.status}` as MessageKey)}</em></div>
            <div className="transfer-lines">{transfer.lines.map((line) => <p key={line.id}><b>{ingredientName.get(line.ingredientId) ?? line.ingredientId.slice(0, 8)}</b><span>{t('commerce.transfer.quantities', { requested: line.quantityBase, packed: line.dispatchedQuantityBase ?? '—', received: line.receivedQuantityBase ?? '—' })}</span></p>)}</div>
            {transfer.notes && <p className="transfer-note">{transfer.notes}</p>}
            <footer><span>{t(origin ? 'commerce.transfer.originView' : destination ? 'commerce.transfer.receivingView' : 'commerce.transfer.readOnly')}</span><div>{action && <button type="button" disabled={!!busy} onClick={() => void act(transfer, action)}>{busy === `${transfer.id}:${action}` ? t('commerce.transfer.saving') : actionText}</button>}{(transfer.status === 'requested' || transfer.status === 'approved') && origin && <button type="button" className="transfer-cancel" disabled={!!busy} onClick={() => void act(transfer, 'cancelled')}>{t('menu.cancel')}</button>}</div></footer>
          </article>;
        })}
      </div>
    </div>
    <form className="transfer-rule" onSubmit={(event) => void saveRule(event)}><div><span className="transfer-step">2</span><b>{t('commerce.transfer.parTitle')}</b><small>{t('commerce.transfer.parHelp')}</small></div><label>{t('commerce.transfer.ingredient')}<select name="ingredient" required defaultValue="" disabled={!ingredients.length || !!busy}><option value="" disabled>{t('commerce.transfer.selectIngredient')}</option>{ingredients.map((ingredient) => <option key={ingredient.id} value={ingredient.id}>{ingredient.name}</option>)}</select></label><label>{t('commerce.transfer.preferredSource')}<select name="source" required defaultValue="" disabled={!destinations.length || !!busy}><option value="" disabled>{t('commerce.transfer.selectSource')}</option>{destinations.map((outlet) => <option key={outlet.id} value={outlet.id}>{outlet.name}</option>)}</select></label><label>{t('commerce.transfer.alertAt')}<input name="reorder" type="number" min="0" step="0.001" required placeholder={t('commerce.transfer.alertExample')} disabled={!!busy} /></label><label>{t('commerce.transfer.refillTo')}<input name="target" type="number" min="0.001" step="0.001" required placeholder={t('commerce.transfer.refillExample')} disabled={!!busy} /></label><button disabled={!!busy || !destinations.length || !ingredients.length}>{busy === 'rule' ? t('commerce.transfer.saving') : t('commerce.transfer.savePar')}</button></form>
  </section>;
}
