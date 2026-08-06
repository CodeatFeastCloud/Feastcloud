import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { coreApiBase, fetchInventorySummary, fetchRecipes, recordInventoryCount, recordReceipt, recordWaste, type InventorySummary, type RecipeSummary } from '../domain/coreInventory';
import { createUuidV7, getDeviceId } from '../domain/kitchen';
import type { KitchenSnapshot } from '../domain/types';
import type { MessageKey } from '../i18n/messages';

const configuredCoreApi = coreApiBase((import.meta.env.VITE_CORE_URL as string | undefined)?.trim());
type Action = 'receive' | 'count' | 'waste';

export function InventoryDashboard({ snapshot, t }: { snapshot: KitchenSnapshot; t: (key: MessageKey, replacements?: Record<string,string|number>) => string }) {
  const [rows,setRows]=useState<InventorySummary[]>([]);
  const [recipes,setRecipes]=useState<RecipeSummary[]>([]);
  const [error,setError]=useState<string>();
  const [notice,setNotice]=useState<string>();
  const [action,setAction]=useState<Action>('receive');
  const [ingredientId,setIngredientId]=useState('');
  const [quantity,setQuantity]=useState('');
  const [cost,setCost]=useState('');
  const [lotCode,setLotCode]=useState('');
  const [expiry,setExpiry]=useState('');
  const [reason,setReason]=useState('');
  const [counts,setCounts]=useState<Record<string,string>>({});
  const [saving,setSaving]=useState(false);
  const [focus,setFocus]=useState<'stock'|'receive'|'count'|'waste'>('stock');
  const load=useCallback(async()=>{if(!configuredCoreApi){setError(t('inventory.notConfigured'));return}try{const [inventory,recipeValues]=await Promise.all([fetchInventorySummary(configuredCoreApi,snapshot.organizationId,snapshot.outletId),fetchRecipes(configuredCoreApi,snapshot.organizationId)]);setRows(inventory);setRecipes(recipeValues);setIngredientId(current=>current||inventory[0]?.ingredientId||'');setError(undefined)}catch{setError(t('inventory.unavailable'))}},[snapshot.organizationId,snapshot.outletId,t]);
  useEffect(()=>{void load();const timer=window.setInterval(()=>void load(),10_000);return()=>window.clearInterval(timer)},[load]);
  const totals=useMemo(()=>rows.reduce((value,row)=>({stock:value.stock+row.stockValueMinor,waste:value.waste+row.wasteValueMinor,cost:value.cost+row.theoreticalCostMinor,variance:value.variance+Math.abs(row.countVarianceValueMinor)}),{stock:0,waste:0,cost:0,variance:0}),[rows]);
  const money=(minor:number,currency='INR')=>new Intl.NumberFormat(undefined,{style:'currency',currency}).format(minor/100);
  const selected=rows.find(row=>row.ingredientId===ingredientId);

  const submitMovement=async(event:FormEvent)=>{event.preventDefault();const parsed=Number(quantity);if(!configuredCoreApi||!selected||!Number.isFinite(parsed)||parsed<=0)return;setSaving(true);setError(undefined);setNotice(undefined);try{const eventId=createUuidV7();if(action==='receive'){await recordReceipt(configuredCoreApi,snapshot.organizationId,snapshot.outletId,{ingredientId,unitId:selected.baseUnitId,quantity:parsed,totalCostMinor:Math.round(Number(cost)*100),currency:selected.currency,lotCode,expiresAt:expiry?new Date(`${expiry}T23:59:59`).toISOString():undefined,eventId,deviceId:getDeviceId()});setNotice(t('inventory.receiptSaved'))}else{await recordWaste(configuredCoreApi,snapshot.organizationId,snapshot.outletId,{ingredientId,unitId:selected.baseUnitId,quantity:parsed,currency:selected.currency,reason,eventId,deviceId:getDeviceId()});setNotice(t('inventory.wasteSaved'))}setQuantity('');setCost('');setLotCode('');setExpiry('');setReason('');await load()}catch{setError(t(action==='receive'?'inventory.receiptFailed':'inventory.wasteFailed'))}finally{setSaving(false)}};
  const submitCount=async(event:FormEvent)=>{event.preventDefault();if(!configuredCoreApi)return;const lines=rows.flatMap(row=>{const value=counts[row.ingredientId];if(value===undefined||value==='')return[];const countedQuantity=Number(value);return Number.isFinite(countedQuantity)&&countedQuantity>=0?[{id:createUuidV7(),ingredientId:row.ingredientId,unitId:row.baseUnitId,countedQuantity}]:[]});if(!lines.length)return;setSaving(true);setError(undefined);setNotice(undefined);try{const result=await recordInventoryCount(configuredCoreApi,snapshot.organizationId,snapshot.outletId,{countId:createUuidV7(),deviceId:getDeviceId(),notes:reason,lines});const changed=result.lines.filter(line=>Math.abs(line.varianceQuantityBase)>.0000005).length;setNotice(t('inventory.countSaved',{count:result.lines.length,changed}));setCounts({});setReason('');await load()}catch{setError(t('inventory.countFailed'))}finally{setSaving(false)}};

  return <section className="inventory-page">
    <header className="page-heading"><div><span className="eyebrow">{t('inventory.eyebrow')}</span><h1>{t('inventory.title')}</h1><p>{t('inventory.subtitle')}</p></div></header>
    <div className="inventory-metrics"><article><span>{t('inventory.stockValue')}</span><strong>{money(totals.stock,rows[0]?.currency)}</strong></article><article><span>{t('inventory.theoreticalCost')}</span><strong>{money(totals.cost,rows[0]?.currency)}</strong></article><article className={totals.waste+totals.variance>0?'warning':''}><span>{t('inventory.wasteVariance')}</span><strong>{money(totals.waste+totals.variance,rows[0]?.currency)}</strong></article></div>
    {error&&<p className="inventory-alert" role="alert">{error}</p>}{notice&&<p className="inventory-notice" role="status">{notice}</p>}
    <nav className="inventory-focus" aria-label={t('inventory.focusLabel')}>{(['stock','receive','count','waste'] as const).map(value=><button key={value} type="button" className={focus===value?'active':''} aria-pressed={focus===value} onClick={()=>{setFocus(value);if(value!=='stock')setAction(value)}}>{t(`inventory.focus.${value}` as MessageKey)}</button>)}</nav>
    <div className="inventory-operations" data-focus={focus}>
      <article className="inventory-table-card" data-area="stock"><div className="section-title"><h2>{t('inventory.onHand')}</h2><span>{rows.length}</span></div><div className="inventory-table-wrap"><table><thead><tr><th>{t('inventory.ingredient')}</th><th>{t('inventory.onHand')}</th><th>{t('inventory.consumed')}</th><th>{t('inventory.waste')}</th><th>{t('inventory.value')}</th></tr></thead><tbody>{rows.map(row=><tr key={row.ingredientId}><td><strong>{row.ingredientName}</strong><small>{row.unitSymbol}</small></td><td>{row.quantityBase.toFixed(3)}</td><td>{row.consumedQuantity.toFixed(3)}</td><td>{row.wasteQuantity.toFixed(3)}</td><td>{money(row.stockValueMinor,row.currency)}</td></tr>)}</tbody></table></div></article>
      <aside className="inventory-side">
        <article className="stock-action-card" data-area="receive count waste"><div className="inventory-tabs" role="tablist">{(['receive','count','waste'] as Action[]).map(value=><button key={value} type="button" role="tab" aria-selected={action===value} onClick={()=>{setAction(value);setFocus(value)}}>{t(`inventory.action.${value}` as MessageKey)}</button>)}</div>
          {action==='count'?<form className="stock-form count-form" onSubmit={(event)=>void submitCount(event)}><p>{t('inventory.countHelp')}</p>{rows.map(row=><label key={row.ingredientId}><span>{row.ingredientName}<small>{row.unitSymbol} · {t('inventory.expected',{quantity:row.quantityBase.toFixed(3)})}</small></span><input aria-label={row.ingredientName} type="number" min="0" step="0.001" value={counts[row.ingredientId]??''} onChange={event=>setCounts(current=>({...current,[row.ingredientId]:event.target.value}))} /></label>)}<label><span>{t('inventory.reason')}</span><input value={reason} maxLength={500} onChange={event=>setReason(event.target.value)} /></label><button disabled={saving||!Object.values(counts).some(value=>value!=='')}>{saving?t('inventory.saving'):t('inventory.saveCount')}</button></form>:
          <form className="stock-form" onSubmit={(event)=>void submitMovement(event)}><label><span>{t('inventory.ingredient')}</span><select value={ingredientId} onChange={event=>setIngredientId(event.target.value)}>{rows.map(row=><option key={row.ingredientId} value={row.ingredientId}>{row.ingredientName} · {row.unitSymbol}</option>)}</select></label><label><span>{t('inventory.quantity')}</span><input type="number" min="0.001" step="0.001" value={quantity} onChange={event=>setQuantity(event.target.value)} /></label>{action==='receive'?<><label><span>{t('inventory.totalCost')}</span><input type="number" min="0" step="0.01" value={cost} onChange={event=>setCost(event.target.value)} /></label><label><span>{t('inventory.lotCode')}</span><input value={lotCode} onChange={event=>setLotCode(event.target.value)} /></label><label><span>{t('inventory.expiry')}</span><input type="date" value={expiry} onChange={event=>setExpiry(event.target.value)} /></label></>:<label><span>{t('inventory.reason')}</span><input value={reason} maxLength={500} onChange={event=>setReason(event.target.value)} /></label>}<button disabled={saving||!ingredientId||Number(quantity)<=0||(action==='receive'&&(!Number.isFinite(Number(cost))||Number(cost)<0))}>{saving?t('inventory.saving'):t(action==='receive'?'inventory.saveReceipt':'inventory.saveWaste')}</button></form>}
        </article>
        <article className="recipe-list" data-area="stock"><div className="section-title"><h2>{t('inventory.recipes')}</h2><span>{recipes.length}</span></div>{recipes.slice(0,6).map(recipe=><div key={recipe.id}><strong>{recipe.name}</strong><small>{t('inventory.recipeVersion',{version:recipe.currentVersion?.versionNumber??1,components:recipe.currentVersion?.components.length??0})}</small></div>)}</article>
      </aside>
    </div>
  </section>;
}
