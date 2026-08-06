import {
  labelForStation,
  nameForLine,
  stationForLine,
  stationIdsInDisplayOrder,
} from '../domain/catalog';
import { getOrderSubtotal } from '../domain/kitchen';
import type { KitchenSnapshot, Locale, StationId, TicketStatus, UserPreferences } from '../domain/types';
import { formatMoney } from '../i18n';
import type { MessageKey } from '../i18n/messages';
import { Icon } from './Icon';

interface OverviewProps {
  snapshot: KitchenSnapshot;
  preferences: UserPreferences;
  t: (key: MessageKey, replacements?: Record<string, string | number>) => string;
  onNavigate: (view: 'orders' | 'kds') => void;
}

const activeTicketStatuses = new Set<TicketStatus>(['queued', 'fired', 'preparing', 'ready']);

function stationLabel(stationId: StationId, t: OverviewProps['t']): string {
  return labelForStation(
    stationId,
    (defaultStationId) => t(`kds.station.${defaultStationId}` as MessageKey),
  );
}

function stationCount(snapshot: KitchenSnapshot, station: StationId) {
  if (snapshot.tickets !== undefined) {
    return snapshot.tickets.filter(
      (ticket) => ticket.stationId === station && activeTicketStatuses.has(ticket.status),
    ).length;
  }
  return snapshot.orders.filter(
    (order) =>
      order.status !== 'completed' && order.status !== 'cancelled' &&
      order.lines.some((line) => stationForLine(line) === station),
  ).length;
}

function TicketStrip({
  snapshot,
  locale,
  t,
}: {
  snapshot: KitchenSnapshot;
  locale: Locale;
  t: OverviewProps['t'];
}) {
  const active = snapshot.orders
    .filter((order) => order.status !== 'completed' && order.status !== 'cancelled')
    .slice(0, 4);
  if (active.length === 0) return <p className="empty-inline">{t('overview.noOrders')}</p>;

  return (
    <div className="ticket-strip">
      {active.map((order) => {
        const firstLine = order.lines[0];
        return (
          <article key={order.id} className={`mini-ticket status-${order.status}`}>
            <div>
              <strong>{t('kds.order', { number: order.number })}</strong>
              <span>{firstLine ? nameForLine(firstLine, locale) : ''}</span>
            </div>
            <span className="status-label">{t(`kds.status.${order.status}` as MessageKey)}</span>
          </article>
        );
      })}
    </div>
  );
}

export function Overview({ snapshot, preferences, t, onNavigate }: OverviewProps) {
  const active = snapshot.orders.filter(
    (order) => order.status !== 'completed' && order.status !== 'cancelled',
  );
  const preparing = active.filter(
    (order) => order.status === 'fired' || order.status === 'preparing',
  ).length;
  const ready = active.filter((order) => order.status === 'ready').length;
  const late = active.filter(
    (order) => order.status !== 'ready' && new Date(order.dueAt).getTime() < Date.now(),
  ).length;
  const openValue = active.reduce((total, order) => total + getOrderSubtotal(order), 0);
  const stationEvidence = snapshot.tickets === undefined
    ? active.flatMap((order) => order.lines.map(stationForLine))
    : snapshot.tickets
        .filter((ticket) => activeTicketStatuses.has(ticket.status))
        .map((ticket) => ticket.stationId);
  const stations = stationIdsInDisplayOrder(stationEvidence);

  return (
    <section className="page overview-page" aria-labelledby="overview-title">
      <div className="page-heading overview-heading">
        <div>
          <span className="eyebrow">{t('overview.eyebrow')}</span>
          <h1 id="overview-title">{t('overview.title')}</h1>
          <p>{t('overview.subtitle')}</p>
        </div>
        <div className="heading-actions">
          <button type="button" className="button secondary" onClick={() => onNavigate('orders')}>
            <Icon name="plus" />
            {t('overview.takeOrder')}
          </button>
          <button type="button" className="button primary" onClick={() => onNavigate('kds')}>
            <Icon name="kitchen" />
            {t('overview.viewKitchen')}
          </button>
        </div>
      </div>

      <div className="metric-grid">
        <article className="metric-card metric-strong">
          <span>{t('overview.openOrders')}</span>
          <strong>{active.length}</strong>
          <small>#{snapshot.orders[0]?.number ?? '—'}</small>
        </article>
        <article className="metric-card">
          <span>{t('overview.inKitchen')}</span>
          <strong>{preparing}</strong>
          <span className="metric-icon amber"><Icon name="flame" /></span>
        </article>
        <article className="metric-card">
          <span>{t('overview.ready')}</span>
          <strong>{ready}</strong>
          <span className="metric-icon green"><Icon name="check" /></span>
        </article>
        <article className="metric-card">
          <span>{t('overview.sales')}</span>
          <strong className="money-metric">{formatMoney(preferences.locale, openValue)}</strong>
          <span className="metric-icon sage"><Icon name="sparkles" /></span>
        </article>
      </div>

      <div className="overview-grid">
        <article className={`pulse-card ${late > 0 ? 'needs-attention' : ''}`}>
          <div className="pulse-visual" aria-hidden="true">
            <span className="pulse-ring ring-one" />
            <span className="pulse-ring ring-two" />
            <span className="pulse-center"><Icon name={late > 0 ? 'clock' : 'check'} /></span>
          </div>
          <div>
            <span className="eyebrow">{t('overview.pulse')}</span>
            <h2>{late > 0 ? t('overview.pulseBody', { count: late }) : t('overview.onTrack')}</h2>
          </div>
        </article>

        <article className="station-card">
          <div className="section-title">
            <h2>{t('overview.stationLoad')}</h2>
            <span>{active.length}</span>
          </div>
          <div className="station-bars">
            {stations.map((station) => {
              const count = stationCount(snapshot, station);
              return (
                <div className="station-row" key={station}>
                  <span>{stationLabel(station, t)}</span>
                  <div className="bar-track">
                    <span style={{ width: `${Math.min(100, (count / Math.max(active.length, 1)) * 100)}%` }} />
                  </div>
                  <strong>{count}</strong>
                </div>
              );
            })}
          </div>
        </article>
      </div>

      <article className="recent-card">
        <div className="section-title">
          <h2>{t('overview.recent')}</h2>
          <button type="button" className="text-button" onClick={() => onNavigate('kds')}>
            {t('nav.kds')} <Icon name="arrow" />
          </button>
        </div>
        <TicketStrip snapshot={snapshot} locale={preferences.locale} t={t} />
      </article>
    </section>
  );
}
