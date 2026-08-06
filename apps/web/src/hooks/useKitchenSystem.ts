import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { catalogById, localize } from '../domain/catalog';
import {
  advanceOrder,
  advanceTicket,
  createInitialSnapshot,
  createOrder,
  createOutboxEvent,
  createTicketOutboxEvent,
  reconcileExistingAggregatorOrder,
} from '../domain/kitchen';
import {
  edgeApiBase,
  fetchEdgeDiscovery,
  fetchEdgeOrders,
  fetchEdgeTickets,
  mergeEdgeProjection,
} from '../domain/edgeProjection';
import type {
  KitchenOrder,
  KitchenSnapshot,
  Locale,
  OrderLine,
  OrderType,
  Role,
  SyncState,
  UserPreferences,
  View,
} from '../domain/types';
import {
  commitSnapshot,
  getLastSyncedAt,
  listOutbox,
  loadPreferences,
  loadSnapshot,
  quarantineOutboxEvent,
  savePreferences,
} from '../persistence/offlineStore';
import { getLanguageDirection, resolveLocale } from '../i18n';
import { drainPendingOutbox, transmitOutboxEvent } from '../sync/outbox';
import { loadEdgeSession, pairWithEdge as exchangeEdgePairingCode } from '../security/edgeSession';

const roleViews: Record<Role, View[]> = {
  cashier: ['orders','commerce'],
  chef: ['kds', 'production'],
  manager: ['overview', 'orders', 'kds', 'production', 'inventory', 'planning', 'daily', 'commerce', 'growth', 'operations', 'organization', 'menu', 'platform'],
};

const configuredEdgeApi = edgeApiBase(
  (import.meta.env.VITE_EDGE_URL as string | undefined)?.trim(),
);

export function shouldAttemptOutletSync(edgeApi: string | undefined, internetOnline: boolean): boolean {
  return Boolean(edgeApi) || internetOnline;
}

function startingSnapshot(): KitchenSnapshot {
  const snapshot = createInitialSnapshot();
  if (!configuredEdgeApi) return snapshot;
  return { ...snapshot, orders: [], tickets: [], nextOrderNumber: 1 };
}

function removeDemoOrders(snapshot: KitchenSnapshot): KitchenSnapshot {
  if (!configuredEdgeApi) return snapshot;
  const orders = snapshot.orders.filter((order) => order.origin !== 'demo');
  return {
    ...snapshot,
    orders,
    tickets: (snapshot.tickets ?? []).filter((ticket) =>
      orders.some((order) => order.id === ticket.orderId)),
    nextOrderNumber: Math.max(0, ...orders.map((order) => order.number)) + 1,
  };
}

function initialLocale(): Locale {
  const language = globalThis.navigator?.language?.toLowerCase() ?? 'en';
  return resolveLocale(language);
}

const supportedViews = new Set<View>([
  'overview', 'orders', 'kds', 'production', 'inventory', 'planning', 'daily',
  'commerce', 'growth', 'operations', 'organization', 'platform', 'menu',
]);

export function viewFromSearch(search: string | undefined): View | undefined {
  try {
    const requested = new URLSearchParams(search).get('view')?.trim().toLowerCase();
    const view = requested === 'order' || requested === 'new-order' || requested === 'new_order'
      ? 'orders'
      : requested;
    return view && supportedViews.has(view as View) ? view as View : undefined;
  } catch {
    return undefined;
  }
}

function initialView(): View {
  return viewFromSearch(globalThis.location?.search) ?? 'overview';
}

const defaultPreferences: UserPreferences = {
  locale: initialLocale(),
  role: 'manager',
  view: initialView(),
  compactMode: false,
};

function restoredPreferences(): UserPreferences {
  const restored = loadPreferences(defaultPreferences);
  restored.locale = resolveLocale(restored.locale);
  const requestedView = viewFromSearch(globalThis.location?.search);
  if (requestedView && roleViews[restored.role].includes(requestedView)) {
    restored.view = requestedView;
  }
  if (!roleViews[restored.role].includes(restored.view)) {
    restored.view = roleViews[restored.role][0];
  }
  return restored;
}

interface SubmitOrderInput {
  type: OrderType;
  guestName?: string;
  tableLabel?: string;
  note?: string;
  aggregator?: KitchenOrder['aggregator'];
  lines: Array<Pick<OrderLine, 'menuItemId' | 'quantity' | 'note' | 'name' | 'stationId'> & { prepMinutes?: number }>;
}

async function requestBackgroundSync(): Promise<void> {
  if (!('serviceWorker' in navigator)) return;
  try {
    const registration = await navigator.serviceWorker.ready;
    const syncManager = (registration as ServiceWorkerRegistration & {
      sync?: { register: (tag: string) => Promise<void> };
    }).sync;
    await syncManager?.register('feastcloud-outbox');
  } catch {
    // Background Sync is optional; online and manual retries still flush the outbox.
  }
}

export function useKitchenSystem() {
  const [snapshot, setSnapshot] = useState<KitchenSnapshot>(startingSnapshot);
  const snapshotRef = useRef(snapshot);
  const [hydrated, setHydrated] = useState(false);
  const [preferences, setPreferences] = useState<UserPreferences>(restoredPreferences);
  const [online, setOnline] = useState(() => globalThis.navigator?.onLine !== false);
  const [syncState, setSyncState] = useState<SyncState>({ pending: 0, quarantined: 0, syncing: false });
  const [pairingRequired, setPairingRequired] = useState(false);
  const flushing = useRef(false);
  const flushRequested = useRef(false);
  const pulling = useRef(false);
  const edgeScopeReady = useRef(!configuredEdgeApi);
  const stateQueue = useRef<Promise<void>>(Promise.resolve());

  const enqueueState = useCallback(<Result,>(task: () => Promise<Result>): Promise<Result> => {
    const result = stateQueue.current.then(task);
    stateQueue.current = result.then(() => undefined, () => undefined);
    return result;
  }, []);

  const refreshSyncState = useCallback(async () => {
    const [events, lastSyncedAt] = await Promise.all([
      listOutbox({ includeQuarantined: true }),
      getLastSyncedAt(),
    ]);
    setSyncState((current) => ({
      ...current,
      pending: events.filter((event) => event.disposition !== 'quarantined').length,
      quarantined: events.filter((event) => event.disposition === 'quarantined').length,
      lastSyncedAt,
    }));
  }, []);

  const discoverEdgeScope = useCallback(async () => {
    if (!configuredEdgeApi) {
      edgeScopeReady.current = true;
      return;
    }
    const discovery = await fetchEdgeDiscovery(configuredEdgeApi);
    await enqueueState(async () => {
      const current = snapshotRef.current;
      if (current.edgeId && current.edgeId !== discovery.edgeId) {
        edgeScopeReady.current = false;
        throw new Error('This browser is paired with a different outlet edge. Re-pairing is required.');
      }
      const events = await listOutbox({ includeQuarantined: true });
      for (const event of events) {
        if (
          event.disposition !== 'quarantined' &&
          (event.tenantId !== discovery.tenantId || event.outletId !== discovery.outletId)
        ) {
          await quarantineOutboxEvent(
            event,
            'The saved operation belongs to a different tenant or outlet and requires reconciliation.',
          );
        }
      }
      const scoped: KitchenSnapshot = {
        ...current,
        organizationId: discovery.tenantId,
        outletId: discovery.outletId,
        edgeId: discovery.edgeId,
      };
      await commitSnapshot(scoped);
      snapshotRef.current = scoped;
      setSnapshot(scoped);
      edgeScopeReady.current = true;
    });
  }, [enqueueState]);

  const pullEdgeProjection = useCallback(async () => {
    if (!configuredEdgeApi || pulling.current) return;
    pulling.current = true;
    try {
      const [remoteOrders, remoteTickets] = await Promise.all([
        fetchEdgeOrders(configuredEdgeApi),
        fetchEdgeTickets(configuredEdgeApi),
      ]);
      await enqueueState(async () => {
        // Read pending intent only after entering the same state queue used by
        // mutations, so a pull cannot overwrite an operation committed mid-fetch.
        const events = await listOutbox();
        const pendingOrderIDs = new Set(
          events
            .flatMap((event) => [event.payload.aggregateType === 'order' ? event.payload.aggregateId : undefined, event.payload.orderId])
            .filter((id): id is string => typeof id === 'string'),
        );
        const pendingTicketIDs = new Set(
          events
            .filter((event) => event.payload.aggregateType === 'kitchenTicket')
            .map((event) => event.payload.aggregateId)
            .filter((id): id is string => typeof id === 'string'),
        );
        for (const ticket of snapshotRef.current.tickets ?? []) {
          if (pendingOrderIDs.has(ticket.orderId)) pendingTicketIDs.add(ticket.id);
        }
        const next = mergeEdgeProjection(
          snapshotRef.current,
          remoteOrders,
          remoteTickets,
          pendingOrderIDs,
          pendingTicketIDs,
        );
        await commitSnapshot(next);
        snapshotRef.current = next;
        setSnapshot(next);
        setSyncState((current) => ({ ...current, error: undefined }));
      });
    } finally {
      pulling.current = false;
    }
  }, [enqueueState]);

  const flushOutbox = useCallback(async () => {
    flushRequested.current = true;
    if (flushing.current) return;
    // A configured outlet edge is a LAN authority and may remain reachable
    // while the browser reports that the public internet is offline.
    if (!shouldAttemptOutletSync(configuredEdgeApi, globalThis.navigator?.onLine !== false)) {
      void requestBackgroundSync();
      return;
    }
    if (configuredEdgeApi && !edgeScopeReady.current) {
      setSyncState((current) => ({
        ...current,
        error: 'Outlet identity is unavailable. Reconnect to the paired edge before creating or syncing operations.',
      }));
      return;
    }

    flushing.current = true;
    setSyncState((current) => ({ ...current, syncing: true, error: undefined }));

    try {
      let transientError: Error | undefined;
      do {
        flushRequested.current = false;
        const result = await drainPendingOutbox((event) =>
          transmitOutboxEvent(event, configuredEdgeApi),
        );
        transientError = result.transientError;
        await refreshSyncState();
        if (transientError) break;
      } while (flushRequested.current || (await listOutbox()).length > 0);

      if (transientError) {
        setSyncState((current) => ({ ...current, error: transientError?.message }));
        void requestBackgroundSync();
        return;
      }

      await refreshSyncState();
      await pullEdgeProjection();
    } catch (error) {
      setSyncState((current) => ({
        ...current,
        error: error instanceof Error ? error.message : 'Sync failed',
      }));
    } finally {
      flushing.current = false;
      setSyncState((current) => ({ ...current, syncing: false }));
    }
  }, [pullEdgeProjection, refreshSyncState]);

  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const stored = await loadSnapshot();
        if (!active) return;
        const initial = removeDemoOrders(
          stored?.schemaVersion === 1 ? stored : startingSnapshot(),
        );
        snapshotRef.current = initial;
        setSnapshot(initial);
        edgeScopeReady.current = !configuredEdgeApi || Boolean(initial.edgeId);
        if (!stored) await commitSnapshot(initial);

        // Rendering must depend only on local durability. Edge discovery and cloud
        // synchronization are background concerns and must never hold the shift UI
        // on its restore screen when a network request is slow or unavailable.
        if (active) setHydrated(true);

        if (configuredEdgeApi) {
          try {
            await discoverEdgeScope();
          } catch (error) {
			if (error instanceof Error && error.message.includes('401')) setPairingRequired(true);
            setSyncState((current) => ({
              ...current,
              error: error instanceof Error ? error.message : 'Edge discovery unavailable',
            }));
          }
        }

        await refreshSyncState();
        if (shouldAttemptOutletSync(configuredEdgeApi, globalThis.navigator?.onLine !== false) && edgeScopeReady.current) {
          await flushOutbox();
        } else {
          void requestBackgroundSync();
        }
      } catch (error) {
        setSyncState((current) => ({
          ...current,
          error: error instanceof Error ? error.message : 'Offline persistence unavailable',
        }));
      } finally {
        if (active) setHydrated(true);
      }
    })();
    return () => {
      active = false;
    };
  }, [discoverEdgeScope, flushOutbox, refreshSyncState]);

  useEffect(() => {
    if (!hydrated || !configuredEdgeApi || !edgeScopeReady.current) return undefined;
    const refresh = () => {
      void pullEdgeProjection().catch((error) =>
        setSyncState((current) => ({
          ...current,
          error: error instanceof Error ? error.message : 'Edge projection unavailable',
        })),
      );
    };
    const timer = window.setInterval(refresh, 2_000);
    return () => window.clearInterval(timer);
  }, [hydrated, pullEdgeProjection]);

  useEffect(() => {
    if (
      !hydrated ||
      !shouldAttemptOutletSync(configuredEdgeApi, online) ||
      syncState.pending === 0 ||
      syncState.syncing ||
      (configuredEdgeApi && !edgeScopeReady.current)
    ) return undefined;

    let cancelled = false;
    let timer: number | undefined;
    void listOutbox().then((events) => {
      if (cancelled || events.length === 0) return;
      const attempts = Math.min(events[0]?.attempts ?? 0, 5);
      const retryDelay = Math.min(30_000, 1_000 * (2 ** attempts)) + Math.floor(Math.random() * 250);
      timer = window.setTimeout(() => void flushOutbox(), retryDelay);
    }).catch((error) => {
      if (!cancelled) {
        setSyncState((current) => ({
          ...current,
          error: error instanceof Error ? error.message : 'Sync retry could not be scheduled',
        }));
      }
    });
    return () => {
      cancelled = true;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [hydrated, online, syncState.pending, syncState.syncing, syncState.error, flushOutbox]);

  useEffect(() => {
    savePreferences(preferences);
    document.documentElement.lang = preferences.locale;
    document.documentElement.dir = getLanguageDirection(preferences.locale);
  }, [preferences]);

  useEffect(() => {
    const handleOnline = () => {
      setOnline(true);
      void (async () => {
        try {
          if (configuredEdgeApi && !edgeScopeReady.current) await discoverEdgeScope();
          await flushOutbox();
        } catch (error) {
          setSyncState((current) => ({
            ...current,
            error: error instanceof Error ? error.message : 'Edge discovery unavailable',
          }));
        }
      })();
    };
    const handleOffline = () => setOnline(false);
    const handleWorkerMessage = (event: MessageEvent<{ type?: string }>) => {
      if (event.data?.type === 'FLUSH_OUTBOX') void flushOutbox();
    };

    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);
    navigator.serviceWorker?.addEventListener('message', handleWorkerMessage);
    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
      navigator.serviceWorker?.removeEventListener('message', handleWorkerMessage);
    };
  }, [discoverEdgeScope, flushOutbox]);

  const commit = useCallback(
    async (next: KitchenSnapshot, event: Parameters<typeof commitSnapshot>[1]) => {
      if (configuredEdgeApi && !edgeScopeReady.current) {
        throw new Error('This device must discover its paired outlet edge before saving operations.');
      }
      try {
        await commitSnapshot(next, event);
      } catch (error) {
        const message = error instanceof Error ? error.message : 'The operation could not be saved';
        setSyncState((current) => ({ ...current, error: message }));
        throw error;
      }
      snapshotRef.current = next;
      setSnapshot(next);
      try {
        await refreshSyncState();
      } catch (error) {
        setSyncState((current) => ({
          ...current,
          error: error instanceof Error ? error.message : 'Saved locally; sync status is unavailable',
        }));
      }
      if (shouldAttemptOutletSync(configuredEdgeApi, globalThis.navigator?.onLine !== false)) void flushOutbox();
      else void requestBackgroundSync();
    },
    [flushOutbox, refreshSyncState],
  );

  const submitOrder = useCallback(
    (input: SubmitOrderInput) => enqueueState(async () => {
      const existing = reconcileExistingAggregatorOrder(snapshotRef.current, input);
      if (existing) {
        if (existing.changed) await commit(existing.snapshot, undefined);
        return existing.order;
      }
      const result = createOrder(snapshotRef.current, input);
      const event = createOutboxEvent(
        result.snapshot,
        'com.feastcloud.order.created.v1',
        result.order,
        {
          order: {
            id: result.order.id,
            type: result.order.type,
            guestName: result.order.guestName,
            tableLabel: result.order.tableLabel,
            note: result.order.note,
            aggregator: result.order.aggregator,
            placedAt: result.order.createdAt,
            targetAt: result.order.dueAt,
            priority: 0,
            stationTicketIds: Object.fromEntries(
              (result.snapshot.tickets ?? [])
                .filter((ticket) => ticket.orderId === result.order.id)
                .map((ticket) => [ticket.stationId, ticket.id]),
            ),
            lines: result.order.lines.map((line) => {
              const item = catalogById.get(line.menuItemId);
              return {
                id: line.id,
                menuItemId: line.menuItemId,
                name: item ? localize(item.name, preferences.locale) : line.name ?? line.menuItemId,
                quantity: line.quantity,
                stationId: item?.station ?? line.stationId ?? 'hot',
                preparationNote: line.note ?? '',
              };
            }),
          },
        },
      );
      await commit(result.snapshot, event);
      return result.order;
    }),
    [commit, enqueueState, preferences.locale],
  );

  const moveOrderForward = useCallback(
    (orderId: string) => enqueueState(async () => {
      const result = advanceOrder(snapshotRef.current, orderId);
      const event = createOutboxEvent(
        result.snapshot,
        'com.feastcloud.order.status-changed.v1',
        result.order,
        {
          orderId,
          toStatus: result.order.status,
          expectedVersion: result.order.version - 1,
        },
      );
      await commit(result.snapshot, event);
      return result.order;
    }),
    [commit, enqueueState],
  );

  const moveTicketForward = useCallback(
    (ticketId: string) => enqueueState(async () => {
      const result = advanceTicket(snapshotRef.current, ticketId);
      const event = createTicketOutboxEvent(
        result.snapshot,
        result.ticket,
        result.order,
      );
      await commit(result.snapshot, event);
      return result.ticket;
    }),
    [commit, enqueueState],
  );

  const updatePreferences = useCallback((patch: Partial<UserPreferences>) => {
    if (patch.view && globalThis.location && globalThis.history) {
      const url = new URL(globalThis.location.href);
      url.searchParams.set('view', patch.view);
      globalThis.history.replaceState(globalThis.history.state, '', url);
    }
    setPreferences((current) => {
      const next = { ...current, ...patch };
      if (!roleViews[next.role].includes(next.view)) next.view = roleViews[next.role][0];
      return next;
    });
  }, []);

  const pairWithEdge = useCallback(async (code: string) => {
    if (!configuredEdgeApi) return;
    const session = await exchangeEdgePairingCode(configuredEdgeApi, code);
    setPreferences((current) => ({
      ...current,
      role: session.role,
      view: roleViews[session.role][0],
    }));
    setPairingRequired(false);
    await discoverEdgeScope();
    await flushOutbox();
  }, [discoverEdgeScope, flushOutbox]);

  return useMemo(
    () => ({
      snapshot,
      hydrated,
      preferences,
      allowedViews: roleViews[preferences.role],
      online,
      syncState,
      submitOrder,
      moveOrderForward,
      moveTicketForward,
      flushOutbox,
      updatePreferences,
      pairingRequired,
      pairWithEdge,
      roleLocked: Boolean(configuredEdgeApi && loadEdgeSession()),
    }),
    [
      snapshot,
      hydrated,
      preferences,
      online,
      syncState,
      submitOrder,
      moveOrderForward,
      moveTicketForward,
      flushOutbox,
      updatePreferences,
      pairingRequired,
      pairWithEdge,
    ],
  );
}
