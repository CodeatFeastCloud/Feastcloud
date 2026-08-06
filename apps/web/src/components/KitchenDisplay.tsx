import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  isDefaultStationId,
  labelForStation,
  nameForLine,
  stationForLine,
  stationIdsInDisplayOrder,
} from '../domain/catalog';
import { canAdvanceOrder } from '../domain/kitchen';
import type {
  KitchenOrder,
  KitchenSnapshot,
  KitchenTicket,
  Locale,
  StationId,
  TicketStatus,
} from '../domain/types';
import type { MessageKey } from '../i18n/messages';
import { Icon } from './Icon';

type StationFilter = StationId | null;

interface KitchenDisplayProps {
  snapshot: KitchenSnapshot;
  locale: Locale;
  t: (key: MessageKey, replacements?: Record<string, string | number>) => string;
  onAdvanceOrder: (orderId: string) => Promise<unknown>;
  onAdvanceTicket: (ticketId: string) => Promise<unknown>;
}

interface CardProjection {
  key: string;
  order: KitchenOrder;
  ticket?: KitchenTicket;
  status: TicketStatus;
  advanceDisabled?: boolean;
}

const activeStatuses: TicketStatus[] = ['queued', 'fired', 'preparing', 'ready'];
type KitchenSound = 'chime' | 'urgent';

const kitchenSoundUrls = new Map<KitchenSound, string>();

/**
 * Builds a tiny PCM WAV data URL at runtime. Native <audio> playback is more
 * dependable across kiosk browsers than relying only on an AudioContext, and
 * it keeps the sound pack completely local/offline.
 */
function kitchenSoundUrl(kind: KitchenSound): string {
  const cached = kitchenSoundUrls.get(kind);
  if (cached) return cached;
  const sampleRate = 22_050;
  const durationSeconds = kind === 'chime' ? 0.64 : 0.9;
  const sampleCount = Math.floor(sampleRate * durationSeconds);
  const bytes = new Uint8Array(44 + sampleCount * 2);
  const view = new DataView(bytes.buffer);
  const writeText = (offset: number, value: string) => {
    for (let index = 0; index < value.length; index += 1) bytes[offset + index] = value.charCodeAt(index);
  };
  writeText(0, 'RIFF');
  view.setUint32(4, 36 + sampleCount * 2, true);
  writeText(8, 'WAVEfmt ');
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  writeText(36, 'data');
  view.setUint32(40, sampleCount * 2, true);

  for (let sample = 0; sample < sampleCount; sample += 1) {
    const time = sample / sampleRate;
    let signal = 0;
    if (kind === 'chime') {
      const notes = [[0, 740], [0.15, 988], [0.3, 1_318]] as const;
      for (const [start, frequency] of notes) {
        const elapsed = time - start;
        if (elapsed < 0 || elapsed > 0.14) continue;
        const envelope = Math.sin(Math.PI * elapsed / 0.14);
        signal += Math.sin(2 * Math.PI * frequency * elapsed) * envelope * 0.27;
      }
    } else {
      for (const start of [0, 0.28, 0.56]) {
        const elapsed = time - start;
        if (elapsed < 0 || elapsed > 0.22) continue;
        const envelope = Math.sin(Math.PI * elapsed / 0.22);
        signal += Math.sign(Math.sin(2 * Math.PI * 186 * elapsed)) * envelope * 0.23;
      }
    }
    view.setInt16(44 + sample * 2, Math.max(-1, Math.min(1, signal)) * 0x7fff, true);
  }
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  const url = `data:audio/wav;base64,${btoa(binary)}`;
  kitchenSoundUrls.set(kind, url);
  return url;
}

function stationLabel(stationId: StationId, t: KitchenDisplayProps['t']): string {
  return labelForStation(
    stationId,
    (defaultStationId) => t(`kds.station.${defaultStationId}` as MessageKey),
  );
}

function stationAppearance(stationId: StationId): string {
  return isDefaultStationId(stationId) ? `station-${stationId}` : 'station-custom';
}

function useClock() {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    // A kitchen needs a visible second-by-second counter, not a dashboard-style
    // timestamp. The state remains local and has no impact on order durability.
    const timer = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(timer);
  }, []);
  return now;
}

function elapsedClock(now: number, iso: string): string {
  const elapsedSeconds = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1_000));
  const hours = Math.floor(elapsedSeconds / 3_600);
  const minutes = Math.floor((elapsedSeconds % 3_600) / 60);
  const seconds = elapsedSeconds % 60;
  const clock = `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
  return hours > 0 ? `${String(hours).padStart(2, '0')}:${clock}` : clock;
}

function minutesFrom(now: number, iso: string): number {
  return Math.max(0, Math.floor((now - new Date(iso).getTime()) / 60_000));
}

function dueLabel(status: TicketStatus, dueAt: string, now: number, t: KitchenDisplayProps['t']) {
  if (status === 'ready') return t('kds.status.ready');
  const difference = Math.ceil((new Date(dueAt).getTime() - now) / 60_000);
  return difference >= 0
    ? t('kds.due', { count: difference })
    : t('kds.overdue', { count: Math.abs(difference) });
}

function TicketCard({
  order,
  ticket,
  status,
  advanceDisabled,
  locale,
  now,
  t,
  onAdvanceOrder,
  onAdvanceTicket,
  isNew,
  isAlertSnoozed,
  onSnoozeAlert,
  onReadOrder,
}: CardProjection & {
  locale: Locale;
  now: number;
  t: KitchenDisplayProps['t'];
  onAdvanceOrder: KitchenDisplayProps['onAdvanceOrder'];
  onAdvanceTicket: KitchenDisplayProps['onAdvanceTicket'];
  isNew: boolean;
  isAlertSnoozed: boolean;
  onSnoozeAlert: () => void;
  onReadOrder: (order: KitchenOrder, lines: KitchenOrder['lines']) => void;
}) {
  const [working, setWorking] = useState(false);
  const lineIds = ticket ? new Set(ticket.lineIds) : undefined;
  const visibleLines = order.lines.filter((line) => !lineIds || lineIds.has(line.id));
  const createdAt = ticket?.createdAt ?? order.createdAt;
  const dueAt = ticket?.targetAt ?? order.dueAt;
  const elapsed = minutesFrom(now, createdAt);
  const elapsedTime = elapsedClock(now, createdAt);
  const isLate = status !== 'ready' && new Date(dueAt).getTime() < now;
  const actionKey: MessageKey =
    status === 'queued'
      ? 'kds.fire'
      : status === 'fired'
        ? 'kds.start'
        : status === 'preparing'
          ? 'kds.ready'
          : 'kds.serve';

  const advance = async () => {
    if (working) return;
    setWorking(true);
    try {
      if (ticket) await onAdvanceTicket(ticket.id);
      else await onAdvanceOrder(order.id);
    } finally {
      setWorking(false);
    }
  };

  return (
    <article className={`kds-ticket status-${status} ${isLate ? 'is-late' : ''} ${isNew ? 'is-new' : ''}`}>
      <header>
        <div>
          <div className="ticket-identifiers">
            {!order.aggregator && <span className="ticket-number">#{order.number}</span>}
            {isNew && <span className="ticket-new-badge">{t('kds.newOrder')}</span>}
          </div>
          <span className="order-channel">
            <Icon name={order.type === 'dineIn' ? 'table' : order.type === 'delivery' ? 'truck' : 'bag'} />
            {t(`order.type.${order.type}` as MessageKey)}
          </span>
        </div>
        <div className="ticket-meta">
          <div className="ticket-time">
            <strong aria-label={t('kds.elapsed', { time: elapsedTime })}>
              <Icon name="clock" /> <time dateTime={`PT${Math.max(0, Math.floor((now - new Date(createdAt).getTime()) / 1_000))}S`}>{elapsedTime}</time>
            </strong>
            <span>{elapsed === 0 ? t('kds.now') : t('kds.ago', { count: elapsed })}</span>
            {ticket && (
              <span className={`ticket-station-label ${stationAppearance(ticket.stationId)}`}>
                {stationLabel(ticket.stationId, t)}
              </span>
            )}
          </div>
          <div className="ticket-utility-actions">
            <button
              type="button"
              className="ticket-icon-action"
              onClick={() => onReadOrder(order, visibleLines)}
              title={t('kds.readOrder')}
              aria-label={`${t('kds.readOrder')} · ${t('kds.order', { number: order.number })}`}
            >
              <Icon name="volume" />
            </button>
            <button
              type="button"
              className="ticket-icon-action"
              onClick={onSnoozeAlert}
              aria-pressed={isAlertSnoozed}
              title={isAlertSnoozed ? t('kds.alertSnoozed') : t('kds.snoozeAlert')}
              aria-label={isAlertSnoozed ? t('kds.resumeAlert') : t('kds.snoozeAlert')}
            >
              <Icon name="bell" />
            </button>
          </div>
        </div>
      </header>

      {order.aggregator && (
        <div className="ticket-aggregator-source">
          <strong>{order.aggregator.brandName}</strong>
          <span><b>{order.aggregator.provider}</b><i aria-hidden="true">·</i> Order #{order.aggregator.externalOrderId}</span>
        </div>
      )}

      {!order.aggregator && (order.guestName || order.tableLabel) && (
        <div className="ticket-customer">
          {order.guestName && <span><b>{t('kds.guest')}:</b> {order.guestName}</span>}
          {order.tableLabel && <span><b>{t('kds.table')}:</b> {order.tableLabel}</span>}
        </div>
      )}

      <div className="ticket-lines">
        {visibleLines.map((line) => {
          const stationId = stationForLine(line);
          return (
            <div key={line.id}>
              <strong>{line.quantity}</strong>
              <span>{nameForLine(line, locale)}</span>
              <i
                className={`station-dot ${stationAppearance(stationId)}`}
                title={stationLabel(stationId, t)}
              />
              {line.note && <small>{line.note}</small>}
            </div>
          );
        })}
      </div>

      {order.note && <p className="ticket-note"><b>{t('kds.note')}:</b> {order.note}</p>}

      <footer>
        <span className={isLate ? 'late' : ''}>{dueLabel(status, dueAt, now, t)}</span>
        <button
          type="button"
          className="ticket-primary-action"
          onClick={advance}
          disabled={working || advanceDisabled}
          title={advanceDisabled ? t('kds.stationMixed') : undefined}
          aria-label={`${t(actionKey)} · ${t('kds.order', { number: order.number })}`}
        >
          {working ? '…' : t(actionKey)} <Icon name="arrow" />
        </button>
      </footer>
      {advanceDisabled && <p className="ticket-action-note">{t('kds.stationMixed')}</p>}
    </article>
  );
}

export function KitchenDisplay({
  snapshot,
  locale,
  t,
  onAdvanceOrder,
  onAdvanceTicket,
}: KitchenDisplayProps) {
  const [station, setStation] = useState<StationFilter>(null);
  // Kitchen alerts are always armed. Operators can still snooze an alert for
  // five minutes on a ticket, but a new KDS session must never start muted.
  const buzzerEnabled = true;
  const [autoReadEnabled, setAutoReadEnabled] = useState(true);
  const [newOrderIds, setNewOrderIds] = useState<Set<string>>(() => new Set());
  const [alertsSnoozedUntil, setAlertsSnoozedUntil] = useState(0);
  const [announcement, setAnnouncement] = useState('');
  const knownQueuedOrderIds = useRef<Set<string> | null>(null);
  const alertedLateOrderIds = useRef<Set<string> | null>(null);
  const observedTicketStates = useRef<Map<string, { status: TicketStatus; orderId: string }> | null>(null);
  const activeAudio = useRef(new Set<HTMLAudioElement>());
  const now = useClock();
  const stations = useMemo(
    () => stationIdsInDisplayOrder((snapshot.tickets ?? []).map((ticket) => ticket.stationId)),
    [snapshot.tickets],
  );
  const cards = useMemo<CardProjection[]>(() => {
    if (station === null) {
      return snapshot.orders
        .filter((order) => activeStatuses.includes(order.status))
        .map((order) => ({
          key: order.id,
          order,
          status: order.status,
          advanceDisabled: !canAdvanceOrder(snapshot, order.id),
        }));
    }
    const orderById = new Map(snapshot.orders.map((order) => [order.id, order]));
    return (snapshot.tickets ?? [])
      .filter((ticket) => ticket.stationId === station && activeStatuses.includes(ticket.status))
      .flatMap((ticket) => {
        const order = orderById.get(ticket.orderId);
        return order ? [{ key: ticket.id, order, ticket, status: ticket.status }] : [];
      });
  }, [snapshot.orders, snapshot.tickets, station]);

  const queuedOrderIds = useMemo(() => {
    const tickets = snapshot.tickets ?? [];
    if (tickets.length > 0) {
      return new Set(tickets.filter((ticket) => ticket.status === 'queued').map((ticket) => ticket.orderId));
    }
    return new Set(snapshot.orders.filter((order) => order.status === 'queued').map((order) => order.id));
  }, [snapshot.orders, snapshot.tickets]);

  const ticketStates = useMemo(() => {
    const states = new Map<string, { status: TicketStatus; orderId: string }>();
    const tickets = snapshot.tickets ?? [];
    if (tickets.length > 0) {
      for (const ticket of tickets) states.set(ticket.id, { status: ticket.status, orderId: ticket.orderId });
    } else {
      for (const order of snapshot.orders) states.set(order.id, { status: order.status, orderId: order.id });
    }
    return states;
  }, [snapshot.orders, snapshot.tickets]);

  const playSound = useCallback(async (kind: KitchenSound): Promise<boolean> => {
    let audio: HTMLAudioElement | undefined;
    try {
      const nextAudio = new Audio(kitchenSoundUrl(kind));
      audio = nextAudio;
      nextAudio.preload = 'auto';
      nextAudio.volume = 0.95;
      activeAudio.current.add(nextAudio);
      const release = () => activeAudio.current.delete(nextAudio);
      nextAudio.addEventListener('ended', release, { once: true });
      nextAudio.addEventListener('error', release, { once: true });
      await nextAudio.play();
      return true;
    } catch {
      if (audio) {
        activeAudio.current.delete(audio);
        audio.pause();
        audio.removeAttribute('src');
      }
      return false;
    }
  }, []);

  const playNewOrderChime = useCallback(() => {
    void playSound('chime').then((played) => {
      if (!played) setAnnouncement(t('kds.buzzerUnavailable'));
    });
  }, [playSound, t]);

  const playUrgencyBuzzer = useCallback(() => {
    void playSound('urgent').then((played) => {
      if (!played) setAnnouncement(t('kds.buzzerUnavailable'));
    });
  }, [playSound, t]);

  useEffect(() => () => {
    for (const audio of activeAudio.current) {
      audio.pause();
      audio.removeAttribute('src');
      audio.load();
    }
    activeAudio.current.clear();
  }, []);

  const speakOrder = useCallback((order: KitchenOrder, lines: KitchenOrder['lines'], append = false) => {
    if (!('speechSynthesis' in window) || !('SpeechSynthesisUtterance' in window)) {
      setAnnouncement(t('kds.readoutUnavailable'));
      return;
    }
    const details = [
      append ? t('kds.status.fired') : '',
      t('kds.order', { number: order.number }),
      t(`order.type.${order.type}` as MessageKey),
      ...lines.map((line) => `${line.quantity} ${nameForLine(line, locale)}${line.note ? `. ${t('kds.note')}: ${line.note}` : ''}`),
      order.note ? `${t('kds.note')}: ${order.note}` : '',
      order.tableLabel ? `${t('kds.table')}: ${order.tableLabel}` : '',
    ].filter(Boolean);
    if (!append) window.speechSynthesis.cancel();
    const utterance = new SpeechSynthesisUtterance(details.join('. '));
    utterance.lang = locale === 'hi' ? 'hi-IN' : locale === 'bn' ? 'bn-IN' : locale;
    utterance.rate = 0.92;
    window.speechSynthesis.speak(utterance);
    setAnnouncement(`${t('kds.readOrder')}: ${t('kds.order', { number: order.number })}`);
  }, [locale, t]);

  useEffect(() => {
    const previous = knownQueuedOrderIds.current;
    if (previous === null) {
      knownQueuedOrderIds.current = queuedOrderIds;
      return;
    }
    const arrivals = [...queuedOrderIds].filter((orderId) => !previous.has(orderId));
    knownQueuedOrderIds.current = queuedOrderIds;
    if (arrivals.length === 0) return;
    setNewOrderIds((current) => new Set([...current, ...arrivals]));
    setAnnouncement(arrivals.length === 1 ? t('kds.newOrder') : t('kds.newOrders', { count: arrivals.length }));
    if (buzzerEnabled && alertsSnoozedUntil <= Date.now()) {
      playNewOrderChime();
    }
  }, [alertsSnoozedUntil, buzzerEnabled, playNewOrderChime, queuedOrderIds, t]);

  useEffect(() => {
    const previous = observedTicketStates.current;
    if (previous === null) {
      observedTicketStates.current = ticketStates;
      return;
    }
    observedTicketStates.current = ticketStates;
    if (!autoReadEnabled) return;
    const newlyAcceptedOrderIds = new Set<string>();
    for (const [ticketId, current] of ticketStates) {
      const prior = previous.get(ticketId);
      if (current.status === 'fired' && (prior?.status === 'queued' || !prior)) {
        newlyAcceptedOrderIds.add(current.orderId);
      }
    }
    for (const orderId of newlyAcceptedOrderIds) {
      const order = snapshot.orders.find((candidate) => candidate.id === orderId);
      if (order) speakOrder(order, order.lines, true);
    }
  }, [autoReadEnabled, snapshot.orders, speakOrder, ticketStates]);

  const overdueOrderIds = useMemo(() => new Set(
    cards
      .filter((card) => {
        const targetAt = card.ticket?.targetAt ?? card.order.dueAt;
        return card.status !== 'ready' && new Date(targetAt).getTime() < now;
      })
      .map((card) => card.order.id),
  ), [cards, now]);
  const boardCounts = useMemo(() => ({
    queued: cards.filter((card) => card.status === 'queued').length,
    preparing: cards.filter((card) => card.status === 'preparing').length,
    ready: cards.filter((card) => card.status === 'ready').length,
    late: overdueOrderIds.size,
  }), [cards, overdueOrderIds]);

  useEffect(() => {
    const previous = alertedLateOrderIds.current;
    if (previous === null) {
      alertedLateOrderIds.current = overdueOrderIds;
      return;
    }
    const newlyLate = [...overdueOrderIds].filter((orderId) => !previous.has(orderId));
    alertedLateOrderIds.current = overdueOrderIds;
    if (newlyLate.length === 0) return;
    setAnnouncement(t('kds.urgentAlert'));
    if (buzzerEnabled && alertsSnoozedUntil <= Date.now()) playUrgencyBuzzer();
  }, [alertsSnoozedUntil, buzzerEnabled, overdueOrderIds, playUrgencyBuzzer, t]);

  useEffect(() => {
    if (newOrderIds.size === 0) return undefined;
    const timer = window.setTimeout(() => setNewOrderIds(new Set()), 12_000);
    return () => window.clearTimeout(timer);
  }, [newOrderIds]);

  const testSound = async (kind: 'chime' | 'urgent') => {
    if (!await playSound(kind)) {
      setAnnouncement(t('kds.buzzerUnavailable'));
      return;
    }
    setAnnouncement(kind === 'chime' ? t('kds.chimeTested') : t('kds.urgentTested'));
  };

  const snoozeAlert = () => {
    if (alertsSnoozedUntil > Date.now()) {
      setAlertsSnoozedUntil(0);
      setAnnouncement(t('kds.alertsOn'));
      return;
    }
    setAlertsSnoozedUntil(Date.now() + 5 * 60_000);
    setAnnouncement(t('kds.alertSnoozed'));
  };

  const readOrder = (order: KitchenOrder, lines: KitchenOrder['lines']) => speakOrder(order, lines);

  const stopReading = () => {
    if ('speechSynthesis' in window) window.speechSynthesis.cancel();
    setAnnouncement(t('kds.readingStopped'));
  };

  return (
    <section className="page kds-page" aria-labelledby="kds-title">
      <div className="kds-command-row">
        <div className="kds-title-block">
          <span className="eyebrow">{t('kds.eyebrow')}</span>
          <h1 id="kds-title">{t('kds.title')}</h1>
        </div>
        <div className="kds-audio-controls">
          <span className={`kds-audio-state ${buzzerEnabled ? 'is-enabled' : ''}`} aria-live="polite" role="status" title={announcement || t('kds.liveTimers')}>
            <i aria-hidden="true" />
            {alertsSnoozedUntil > now ? t('kds.alertsSnoozed') : buzzerEnabled ? t('kds.alertsOn') : t('kds.alertsMuted')}
          </span>
          <details className="kds-sound-menu">
            <summary aria-label={t('kds.soundControls')} title={t('kds.soundControls')}>
              <Icon name="volume" />
            </summary>
            <div>
              <p>{announcement || t('kds.liveTimers')}</p>
              <button type="button" onClick={() => void testSound('chime')}>
                <Icon name="volume" /> {t('kds.testChime')}
              </button>
              <button type="button" className="kds-urgent-test" onClick={() => void testSound('urgent')}>
                <Icon name="bell" /> {t('kds.testUrgent')}
              </button>
              <button type="button" aria-pressed={autoReadEnabled} onClick={() => setAutoReadEnabled((current) => !current)}>
                <Icon name="volume" /> {autoReadEnabled ? t('kds.autoReadOn') : t('kds.autoReadOff')}
              </button>
              <button type="button" onClick={stopReading}>
                <Icon name="pause" /> {t('kds.stopReading')}
              </button>
            </div>
          </details>
        </div>
      </div>
      <div className="kds-workbar">
        <div className="station-filters" aria-label={t('kds.station.all')}>
          <button
            key="all-stations"
            type="button"
            className={station === null ? 'active' : ''}
            aria-pressed={station === null}
            onClick={() => setStation(null)}
          >
            {t('kds.station.all')}
          </button>
          {stations.map((candidate) => (
            <button
              key={`station:${candidate}`}
              type="button"
              className={station === candidate ? 'active' : ''}
              aria-pressed={station === candidate}
              onClick={() => setStation(candidate)}
            >
              {stationLabel(candidate, t)}
            </button>
          ))}
        </div>
        <div className="kds-glance" aria-label={t('kds.title')}>
          <span><b>{boardCounts.queued}</b> {t('kds.status.queued')}</span>
          <span><b>{boardCounts.preparing}</b> {t('kds.status.preparing')}</span>
          <span><b>{boardCounts.ready}</b> {t('kds.status.ready')}</span>
          {boardCounts.late > 0 && <span className="is-urgent"><b>{boardCounts.late}</b> {t('kds.overdue', { count: boardCounts.late })}</span>}
        </div>
      </div>
      {station !== null && <p className="station-safety-note">{t('kds.stationScoped')}</p>}

      <div className="kds-board">
        {activeStatuses.map((status) => {
          const tickets = cards.filter((card) => card.status === status);
          return (
            <section className={`kds-column column-${status}`} key={status} aria-labelledby={`column-${status}`}>
              <header>
                <div>
                  <i aria-hidden="true" />
                  <h2 id={`column-${status}`}>{t(`kds.status.${status}` as MessageKey)}</h2>
                </div>
                <span>{tickets.length}</span>
              </header>
              <div className="column-tickets">
                {tickets.length > 0 ? (
                  tickets.map((card) => (
                    <TicketCard
                      key={card.key}
                      order={card.order}
                      ticket={card.ticket}
                      status={card.status}
                      advanceDisabled={card.advanceDisabled}
                      locale={locale}
                      now={now}
                      t={t}
                      onAdvanceOrder={onAdvanceOrder}
                      onAdvanceTicket={onAdvanceTicket}
                      isNew={newOrderIds.has(card.order.id)}
                      isAlertSnoozed={alertsSnoozedUntil > now}
                      onSnoozeAlert={snoozeAlert}
                      onReadOrder={readOrder}
                    />
                  ))
                ) : (
                  <p className="empty-column"><Icon name="check" /> {t('kds.empty')}</p>
                )}
              </div>
            </section>
          );
        })}
      </div>
    </section>
  );
}
