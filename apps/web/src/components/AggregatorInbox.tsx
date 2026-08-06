import { useState } from 'react';
import { decideConnectorInbox, type ConnectorInboxOrder } from '../domain/coreCommerce';
import type { MessageKey } from '../i18n/messages';

type Translate = (key: MessageKey, replacements?: Record<string, string | number>) => string;

export function AggregatorInbox({ api, tenantId, outletId, orders, t, onRefresh, onError }: { api: string; tenantId: string; outletId: string; orders: ConnectorInboxOrder[]; t: Translate; onRefresh: () => Promise<void>; onError: (value: string) => void }) {
  const [busy, setBusy] = useState('');
  const [canonicalIDs, setCanonicalIDs] = useState<Record<string, string>>({});
  const act = async (order: ConnectorInboxOrder, decision: 'accepted' | 'rejected' | 'duplicate' | 'needs_review') => {
    setBusy(`${order.id}:${decision}`);
    try {
      await decideConnectorInbox(api, tenantId, outletId, order.id, decision, decision === 'needs_review' ? t('commerce.inbox.reviewReason') : '', canonicalIDs[order.id] ?? '');
      await onRefresh();
    } catch { onError(t('commerce.inbox.failed')); }
    finally { setBusy(''); }
  };
  const open = orders.filter((order) => order.status === 'received' || order.status === 'needs_review');
  return <section className="aggregator-inbox">
    <header><div><span>{t('commerce.inbox.eyebrow')}</span><h2>{t('commerce.inbox.title')}</h2><p>{t('commerce.inbox.help')}</p></div><strong>{t('commerce.inbox.reviewCount', { count: open.length })}</strong></header>
    {orders.length === 0 ? <div className="inbox-empty">{t('commerce.inbox.empty')}</div> : <div className="inbox-list">{orders.slice(0, 30).map((order) => <article className={`inbox-order status-${order.status}`} key={order.id}><div className="inbox-order-title"><span><b>{order.externalOrderId}</b><small>{new Date(order.receivedAt).toLocaleString()}</small></span><i>{t(`commerce.inbox.status.${order.status}` as MessageKey)}</i></div><details><summary>{t('commerce.inbox.payload')}</summary><pre>{JSON.stringify(order.payload, null, 2)}</pre></details>{(order.status === 'received' || order.status === 'needs_review') && <div className="inbox-actions"><input aria-label={t('commerce.inbox.canonicalId')} placeholder={t('commerce.inbox.canonicalPlaceholder')} value={canonicalIDs[order.id] ?? ''} onChange={(event) => setCanonicalIDs((current) => ({ ...current, [order.id]: event.target.value }))} /><button type="button" disabled={!!busy} onClick={() => void act(order, 'accepted')}>{t('commerce.inbox.accept')}</button><button type="button" disabled={!!busy} className="quiet" onClick={() => void act(order, 'needs_review')}>{t('commerce.inbox.review')}</button><button type="button" disabled={!!busy} className="danger" onClick={() => void act(order, 'rejected')}>{t('commerce.inbox.reject')}</button></div>}{order.normalizedOrderId && <p className="inbox-link">{t('commerce.inbox.canonicalOrder')}: <code>{order.normalizedOrderId}</code></p>}</article>)}</div>}
  </section>;
}
