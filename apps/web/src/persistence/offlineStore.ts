import type { KitchenSnapshot, OutboxEvent, UserPreferences } from '../domain/types';

const DATABASE_NAME = 'feastcloud-edge';
const DATABASE_VERSION = 2;
const SNAPSHOT_STORE = 'snapshots';
const OUTBOX_STORE = 'outbox';
const META_STORE = 'meta';
const SNAPSHOT_KEY = 'active-kitchen';
const LOCAL_STATE_KEY = 'feastcloud.offline-state.v2';
const LEGACY_LOCAL_SNAPSHOT_KEY = 'feastcloud.snapshot.v1';
const LEGACY_LOCAL_OUTBOX_KEY = 'feastcloud.outbox.v1';
const PREFERENCES_KEY = 'feastcloud.preferences.v1';
const LAST_SYNCED_KEY = 'last-synced-at';
const LAST_OUTBOX_SEQUENCE_KEY = 'last-outbox-sequence';
const DATABASE_OPEN_TIMEOUT_MS = 2_000;

type PersistenceBackend = 'indexeddb' | 'localstorage';

interface LocalDurableState {
  version: 2;
  snapshot?: KitchenSnapshot;
  outbox: OutboxEvent[];
  lastOutboxSequence: number;
  lastSyncedAt?: string;
}

let databasePromise: Promise<IDBDatabase> | undefined;
let backendPromise: Promise<PersistenceBackend> | undefined;

function isValidSequence(value: number | undefined): value is number {
  return Number.isSafeInteger(value) && (value ?? 0) > 0;
}

function normalizeLegacyEvents(
  events: OutboxEvent[],
  sequenceFloor = 0,
): { events: OutboxEvent[]; lastSequence: number } {
  let lastSequence = Math.max(
    Number.isSafeInteger(sequenceFloor) && sequenceFloor > 0 ? sequenceFloor : 0,
    ...events.map((event) => isValidSequence(event.localSequence) ? event.localSequence : 0),
  );
  const assignedSequences = new Map<string, number>();

  [...events]
    .filter((event) => !isValidSequence(event.localSequence))
    .sort((left, right) =>
      left.occurredAt.localeCompare(right.occurredAt) || left.id.localeCompare(right.id),
    )
    .forEach((event) => {
      lastSequence += 1;
      assignedSequences.set(event.id, lastSequence);
    });

  return {
    events: events.map((event) => ({
      ...event,
      localSequence: isValidSequence(event.localSequence)
        ? event.localSequence
        : assignedSequences.get(event.id),
      disposition: event.disposition ?? 'pending',
    })),
    lastSequence,
  };
}

function migrateLegacyOutboxDuringUpgrade(transaction: IDBTransaction): void {
  const outbox = transaction.objectStore(OUTBOX_STORE);
  const meta = transaction.objectStore(META_STORE);
  const eventsRequest = outbox.getAll() as IDBRequest<OutboxEvent[]>;
  const sequenceRequest = meta.get(LAST_OUTBOX_SEQUENCE_KEY) as IDBRequest<number | undefined>;
  let events: OutboxEvent[] | undefined;
  let sequence: number | undefined;
  let eventsLoaded = false;
  let sequenceLoaded = false;

  const finish = () => {
    if (!eventsLoaded || !sequenceLoaded) return;
    try {
      const normalized = normalizeLegacyEvents(events ?? [], sequence ?? 0);
      normalized.events.forEach((event) => outbox.put(event));
      meta.put(normalized.lastSequence, LAST_OUTBOX_SEQUENCE_KEY);
    } catch {
      transaction.abort();
    }
  };

  eventsRequest.onsuccess = () => {
    events = eventsRequest.result;
    eventsLoaded = true;
    finish();
  };
  sequenceRequest.onsuccess = () => {
    sequence = sequenceRequest.result;
    sequenceLoaded = true;
    finish();
  };
}

function openDatabase(): Promise<IDBDatabase> {
  if (!globalThis.indexedDB) return Promise.reject(new Error('IndexedDB is unavailable'));

  databasePromise ??= new Promise((resolve, reject) => {
    const request = indexedDB.open(DATABASE_NAME, DATABASE_VERSION);
    let settled = false;
    const timeout = globalThis.setTimeout(() => {
      if (settled) return;
      settled = true;
      reject(new Error('Offline database did not open within 2 seconds'));
    }, DATABASE_OPEN_TIMEOUT_MS);

    const rejectOpen = (error: Error) => {
      if (settled) return;
      settled = true;
      globalThis.clearTimeout(timeout);
      reject(error);
    };

    request.onupgradeneeded = (event) => {
      const database = request.result;
      if (!database.objectStoreNames.contains(SNAPSHOT_STORE)) database.createObjectStore(SNAPSHOT_STORE);
      if (!database.objectStoreNames.contains(OUTBOX_STORE)) {
        database.createObjectStore(OUTBOX_STORE, { keyPath: 'id' });
      }
      if (!database.objectStoreNames.contains(META_STORE)) database.createObjectStore(META_STORE);
      if (event.oldVersion < 2 && request.transaction) {
        migrateLegacyOutboxDuringUpgrade(request.transaction);
      }
    };

    request.onsuccess = () => {
      if (settled) {
        // A browser may finish an open after the deadline. The backend is already
        // pinned to the local journal for this page load, so do not retain a second
        // writable authority or leave a connection that can block a future upgrade.
        request.result.close();
        return;
      }
      settled = true;
      globalThis.clearTimeout(timeout);
      request.result.onversionchange = () => request.result.close();
      resolve(request.result);
    };
    request.onerror = () => rejectOpen(request.error ?? new Error('Could not open offline database'));
    request.onblocked = () => rejectOpen(new Error('Offline database upgrade is blocked by another tab'));
  });

  return databasePromise;
}

function requestValue<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error('Offline database request failed'));
  });
}

function transactionComplete(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error ?? new Error('Offline transaction failed'));
    transaction.onabort = () => reject(transaction.error ?? new Error('Offline transaction was aborted'));
  });
}

function readOptionalJson<T>(key: string): T | undefined {
  try {
    const value = localStorage.getItem(key);
    return value ? (JSON.parse(value) as T) : undefined;
  } catch {
    return undefined;
  }
}

function verifyLocalStorage(): void {
  const probe = 'feastcloud.storage-probe';
  localStorage.setItem(probe, '1');
  localStorage.removeItem(probe);
}

function parseLocalState(raw: string): LocalDurableState {
  const parsed = JSON.parse(raw) as Partial<LocalDurableState>;
  if (parsed.version !== 2 || !Array.isArray(parsed.outbox)) {
    throw new Error('The local kitchen journal has an unsupported format');
  }
  const normalized = normalizeLegacyEvents(parsed.outbox, parsed.lastOutboxSequence ?? 0);
  return {
    version: 2,
    snapshot: parsed.snapshot,
    outbox: normalized.events,
    lastOutboxSequence: normalized.lastSequence,
    lastSyncedAt: parsed.lastSyncedAt,
  };
}

function newerTimestamp(left: string | undefined, right: string | undefined): string | undefined {
  if (!left) return right;
  if (!right) return left;
  return left >= right ? left : right;
}

function reconcileSnapshots(
  stored: KitchenSnapshot | undefined,
  local: KitchenSnapshot | undefined,
): KitchenSnapshot | undefined {
  if (!stored) return local;
  if (!local) return stored;
  if (stored.organizationId !== local.organizationId || stored.outletId !== local.outletId) {
    throw new Error('The local kitchen journal belongs to a different outlet');
  }

  const orders = new Map(stored.orders.map((order) => [order.id, order]));
  local.orders.forEach((localOrder) => {
    const storedOrder = orders.get(localOrder.id);
    if (
      !storedOrder
      || localOrder.version > storedOrder.version
      || (
        localOrder.version === storedOrder.version
        && localOrder.updatedAt >= storedOrder.updatedAt
      )
    ) {
      orders.set(localOrder.id, localOrder);
    }
  });
  const tickets = new Map((stored.tickets ?? []).map((ticket) => [ticket.id, ticket]));
  (local.tickets ?? []).forEach((localTicket) => {
    const storedTicket = tickets.get(localTicket.id);
    if (
      !storedTicket
      || localTicket.version > storedTicket.version
      || (
        localTicket.version === storedTicket.version
        && localTicket.updatedAt >= storedTicket.updatedAt
      )
    ) {
      tickets.set(localTicket.id, localTicket);
    }
  });

  return {
    ...stored,
    ...local,
    edgeId: local.edgeId ?? stored.edgeId,
    orders: [...orders.values()].sort(
      (left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id),
    ),
    tickets: stored.tickets || local.tickets
      ? [...tickets.values()].sort(
          (left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id),
        )
      : undefined,
    nextOrderNumber: Math.max(stored.nextOrderNumber, local.nextOrderNumber),
  };
}

interface LocalJournalMigration {
  state: LocalDurableState;
  sources: Array<{ key: string; value: string }>;
}

function readLocalJournalForMigration(): LocalJournalMigration | undefined {
  // IndexedDB is independently usable in workers, hardened browser contexts and tests
  // where localStorage is not exposed.
  if (!globalThis.localStorage) return undefined;
  const current = localStorage.getItem(LOCAL_STATE_KEY);
  if (current !== null) {
    const legacySources = [
      LEGACY_LOCAL_SNAPSHOT_KEY,
      LEGACY_LOCAL_OUTBOX_KEY,
      LAST_SYNCED_KEY,
    ].flatMap((key) => {
      const value = localStorage.getItem(key);
      return value === null ? [] : [{ key, value }];
    });
    return {
      state: parseLocalState(current),
      // Clearing stale legacy keys prevents them from being resurrected on a later restart.
      sources: [{ key: LOCAL_STATE_KEY, value: current }, ...legacySources],
    };
  }

  const legacySnapshot = localStorage.getItem(LEGACY_LOCAL_SNAPSHOT_KEY);
  const legacyOutbox = localStorage.getItem(LEGACY_LOCAL_OUTBOX_KEY);
  const legacyLastSyncedAt = localStorage.getItem(LAST_SYNCED_KEY);
  if (legacySnapshot === null && legacyOutbox === null && legacyLastSyncedAt === null) return undefined;

  const normalized = normalizeLegacyEvents(
    legacyOutbox ? JSON.parse(legacyOutbox) as OutboxEvent[] : [],
  );
  return {
    state: {
      version: 2,
      snapshot: legacySnapshot ? JSON.parse(legacySnapshot) as KitchenSnapshot : undefined,
      outbox: normalized.events,
      lastOutboxSequence: normalized.lastSequence,
      lastSyncedAt: legacyLastSyncedAt ?? undefined,
    },
    sources: [
      { key: LEGACY_LOCAL_SNAPSHOT_KEY, value: legacySnapshot },
      { key: LEGACY_LOCAL_OUTBOX_KEY, value: legacyOutbox },
      { key: LAST_SYNCED_KEY, value: legacyLastSyncedAt },
    ].filter((source): source is { key: string; value: string } => source.value !== null),
  };
}

function mergeDuplicateEvent(stored: OutboxEvent, local: OutboxEvent): OutboxEvent {
  const quarantined = stored.disposition === 'quarantined' || local.disposition === 'quarantined';
  return {
    ...stored,
    ...local,
    attempts: Math.max(stored.attempts, local.attempts),
    localSequence: stored.localSequence,
    disposition: quarantined ? 'quarantined' : (local.disposition ?? stored.disposition ?? 'pending'),
    lastError: local.lastError ?? stored.lastError,
  };
}

async function migrateLocalJournalToDatabase(database: IDBDatabase): Promise<void> {
  const migration = readLocalJournalForMigration();
  if (!migration) return;

  const transaction = database.transaction(
    [SNAPSHOT_STORE, OUTBOX_STORE, META_STORE],
    'readwrite',
  );
  const snapshotStore = transaction.objectStore(SNAPSHOT_STORE);
  const outboxStore = transaction.objectStore(OUTBOX_STORE);
  const metaStore = transaction.objectStore(META_STORE);
  const storedSnapshotRequest = snapshotStore.get(SNAPSHOT_KEY) as IDBRequest<KitchenSnapshot | undefined>;
  const storedOutboxRequest = outboxStore.getAll() as IDBRequest<OutboxEvent[]>;
  const storedSequenceRequest = metaStore.get(LAST_OUTBOX_SEQUENCE_KEY) as IDBRequest<number | undefined>;
  const storedLastSyncedRequest = metaStore.get(LAST_SYNCED_KEY) as IDBRequest<string | undefined>;
  const [storedSnapshot, storedOutbox, storedSequence, storedLastSyncedAt] = await Promise.all([
    requestValue(storedSnapshotRequest),
    requestValue(storedOutboxRequest),
    requestValue(storedSequenceRequest),
    requestValue(storedLastSyncedRequest),
  ]);

  try {
    const normalizedStored = normalizeLegacyEvents(storedOutbox, storedSequence ?? 0);
    const storedById = new Map(normalizedStored.events.map((event) => [event.id, event]));
    let lastSequence = normalizedStored.lastSequence;
    const usedSequences = new Set(
      normalizedStored.events
        .map((event) => event.localSequence)
        .filter(isValidSequence),
    );

    sortOutbox(migration.state.outbox).forEach((localEvent) => {
      const storedEvent = storedById.get(localEvent.id);
      if (storedEvent) {
        storedById.set(localEvent.id, mergeDuplicateEvent(storedEvent, localEvent));
        return;
      }

      const localSequence = localEvent.localSequence;
      const nextSequence = isValidSequence(localSequence)
        && localSequence > lastSequence
        && !usedSequences.has(localSequence)
        ? localSequence
        : lastSequence + 1;
      lastSequence = Math.max(lastSequence, nextSequence);
      usedSequences.add(nextSequence);
      storedById.set(localEvent.id, {
        ...localEvent,
        localSequence: nextSequence,
        disposition: localEvent.disposition ?? 'pending',
      });
    });

    lastSequence = Math.max(lastSequence, migration.state.lastOutboxSequence);
    const reconciledSnapshot = reconcileSnapshots(storedSnapshot, migration.state.snapshot);
    if (reconciledSnapshot) snapshotStore.put(reconciledSnapshot, SNAPSHOT_KEY);
    storedById.forEach((event) => outboxStore.put(event));
    metaStore.put(lastSequence, LAST_OUTBOX_SEQUENCE_KEY);
    const lastSyncedAt = newerTimestamp(storedLastSyncedAt, migration.state.lastSyncedAt);
    if (lastSyncedAt) metaStore.put(lastSyncedAt, LAST_SYNCED_KEY);
  } catch (error) {
    try {
      transaction.abort();
    } catch {
      // The transaction may already have aborted because of the original failure.
    }
    throw error;
  }

  // The source journal remains intact if any read, reconciliation or write aborts.
  await transactionComplete(transaction);
  migration.sources.forEach(({ key, value }) => {
    try {
      // Do not remove a journal another tab changed while this transaction was committing.
      if (localStorage.getItem(key) === value) localStorage.removeItem(key);
    } catch {
      // The committed IndexedDB copy is authoritative. A retained source is reconciled idempotently.
    }
  });
}

async function selectBackend(): Promise<PersistenceBackend> {
  try {
    const database = await openDatabase();
    await migrateLocalJournalToDatabase(database);
    return 'indexeddb';
  } catch (indexedDBError) {
    try {
      verifyLocalStorage();
      return 'localstorage';
    } catch (localStorageError) {
      throw new AggregateError(
        [indexedDBError, localStorageError],
        'This browser cannot durably save kitchen operations',
      );
    }
  }
}

function backend(): Promise<PersistenceBackend> {
  backendPromise ??= selectBackend();
  return backendPromise;
}

function readLocalState(): LocalDurableState {
  const raw = localStorage.getItem(LOCAL_STATE_KEY);
  if (raw) return parseLocalState(raw);

  const snapshot = readOptionalJson<KitchenSnapshot>(LEGACY_LOCAL_SNAPSHOT_KEY);
  const normalized = normalizeLegacyEvents(
    readOptionalJson<OutboxEvent[]>(LEGACY_LOCAL_OUTBOX_KEY) ?? [],
  );
  return {
    version: 2,
    snapshot,
    outbox: normalized.events,
    lastOutboxSequence: normalized.lastSequence,
    lastSyncedAt: localStorage.getItem(LAST_SYNCED_KEY) ?? undefined,
  };
}

function writeLocalState(state: LocalDurableState): void {
  // One durable write keeps the snapshot, outbox and ordering cursor indivisible.
  localStorage.setItem(LOCAL_STATE_KEY, JSON.stringify(state));
}

function sortOutbox(events: OutboxEvent[]): OutboxEvent[] {
  return [...events].sort((left, right) => {
    const leftSequence = left.localSequence ?? Number.MAX_SAFE_INTEGER;
    const rightSequence = right.localSequence ?? Number.MAX_SAFE_INTEGER;
    return leftSequence - rightSequence || left.id.localeCompare(right.id);
  });
}

export async function loadSnapshot(): Promise<KitchenSnapshot | undefined> {
  if ((await backend()) === 'localstorage') return readLocalState().snapshot;

  const database = await openDatabase();
  const transaction = database.transaction(SNAPSHOT_STORE, 'readonly');
  return requestValue<KitchenSnapshot | undefined>(
    transaction.objectStore(SNAPSHOT_STORE).get(SNAPSHOT_KEY),
  );
}

export async function commitSnapshot(
  snapshot: KitchenSnapshot,
  event?: OutboxEvent,
): Promise<void> {
  if ((await backend()) === 'localstorage') {
    const state = readLocalState();
    const alreadyStored = event && state.outbox.some((candidate) => candidate.id === event.id);
    const lastOutboxSequence = alreadyStored || !event
      ? state.lastOutboxSequence
      : state.lastOutboxSequence + 1;
    const persistedEvent = event && !alreadyStored
      ? {
          ...event,
          localSequence: lastOutboxSequence,
          disposition: event.disposition ?? 'pending' as const,
        }
      : undefined;
    writeLocalState({
      ...state,
      snapshot,
      lastOutboxSequence,
      outbox: persistedEvent ? [...state.outbox, persistedEvent] : state.outbox,
    });
    return;
  }

  const database = await openDatabase();
  const stores = event
    ? [SNAPSHOT_STORE, OUTBOX_STORE, META_STORE]
    : [SNAPSHOT_STORE];
  const transaction = database.transaction(stores, 'readwrite');
  transaction.objectStore(SNAPSHOT_STORE).put(snapshot, SNAPSHOT_KEY);
  if (event) {
    const outbox = transaction.objectStore(OUTBOX_STORE);
    const existing = await requestValue<OutboxEvent | undefined>(outbox.get(event.id));
    if (!existing) {
      const meta = transaction.objectStore(META_STORE);
      const storedSequence = await requestValue<number | undefined>(meta.get(LAST_OUTBOX_SEQUENCE_KEY));
      const existingEvents = await requestValue<OutboxEvent[]>(outbox.getAll());
      const lastSequence = Math.max(
        storedSequence ?? 0,
        ...existingEvents.map((candidate) => candidate.localSequence ?? 0),
      );
      const nextSequence = lastSequence + 1;
      outbox.put({
        ...event,
        localSequence: nextSequence,
        disposition: event.disposition ?? 'pending',
      });
      meta.put(nextSequence, LAST_OUTBOX_SEQUENCE_KEY);
    }
  }
  await transactionComplete(transaction);
}

export async function listOutbox(options: { includeQuarantined?: boolean } = {}): Promise<OutboxEvent[]> {
  let events: OutboxEvent[];
  if ((await backend()) === 'localstorage') {
    events = readLocalState().outbox;
  } else {
    const database = await openDatabase();
    const transaction = database.transaction(OUTBOX_STORE, 'readonly');
    events = await requestValue<OutboxEvent[]>(transaction.objectStore(OUTBOX_STORE).getAll());
  }
  return sortOutbox(
    options.includeQuarantined
      ? events
      : events.filter((event) => event.disposition !== 'quarantined'),
  );
}

export async function acknowledgeOutboxEvent(
  id: string,
  syncedAt = new Date().toISOString(),
): Promise<void> {
  if ((await backend()) === 'localstorage') {
    const state = readLocalState();
    writeLocalState({
      ...state,
      outbox: state.outbox.filter((event) => event.id !== id),
      lastSyncedAt: syncedAt,
    });
    return;
  }

  const database = await openDatabase();
  const transaction = database.transaction([OUTBOX_STORE, META_STORE], 'readwrite');
  transaction.objectStore(OUTBOX_STORE).delete(id);
  transaction.objectStore(META_STORE).put(syncedAt, LAST_SYNCED_KEY);
  await transactionComplete(transaction);
}

async function replaceOutboxEvent(
  event: OutboxEvent,
  update: (current: OutboxEvent) => OutboxEvent,
): Promise<void> {
  if ((await backend()) === 'localstorage') {
    const state = readLocalState();
    writeLocalState({
      ...state,
      outbox: state.outbox.map((candidate) =>
        candidate.id === event.id ? update(candidate) : candidate,
      ),
    });
    return;
  }

  const database = await openDatabase();
  const transaction = database.transaction(OUTBOX_STORE, 'readwrite');
  const store = transaction.objectStore(OUTBOX_STORE);
  const current = await requestValue<OutboxEvent | undefined>(store.get(event.id));
  if (current) store.put(update(current));
  await transactionComplete(transaction);
}

export async function incrementOutboxAttempt(event: OutboxEvent, message?: string): Promise<void> {
  await replaceOutboxEvent(event, (current) => ({
    ...current,
    attempts: current.attempts + 1,
    lastError: message ?? current.lastError,
  }));
}

export async function quarantineOutboxEvent(event: OutboxEvent, message: string): Promise<void> {
  await replaceOutboxEvent(event, (current) => ({
    ...current,
    attempts: current.attempts + 1,
    disposition: 'quarantined',
    lastError: message,
  }));
}

export async function getLastSyncedAt(): Promise<string | undefined> {
  if ((await backend()) === 'localstorage') return readLocalState().lastSyncedAt;

  const database = await openDatabase();
  const transaction = database.transaction(META_STORE, 'readonly');
  return requestValue<string | undefined>(transaction.objectStore(META_STORE).get(LAST_SYNCED_KEY));
}

export function loadPreferences(defaults: UserPreferences): UserPreferences {
  const stored = readOptionalJson<Partial<UserPreferences>>(PREFERENCES_KEY);
  return { ...defaults, ...stored };
}

export function savePreferences(preferences: UserPreferences): void {
  try {
    localStorage.setItem(PREFERENCES_KEY, JSON.stringify(preferences));
  } catch {
    // Preferences are non-operational; a blocked store must not interrupt a shift.
  }
}

export async function resetOfflineStoreForTests(): Promise<void> {
  await restartOfflineStoreForTests();
  if (globalThis.indexedDB) {
    await new Promise<void>((resolve) => {
      const request = indexedDB.deleteDatabase(DATABASE_NAME);
      request.onsuccess = () => resolve();
      request.onerror = () => resolve();
      request.onblocked = () => resolve();
    });
  }
  try {
    localStorage.removeItem(LOCAL_STATE_KEY);
    localStorage.removeItem(LEGACY_LOCAL_SNAPSHOT_KEY);
    localStorage.removeItem(LEGACY_LOCAL_OUTBOX_KEY);
    localStorage.removeItem(PREFERENCES_KEY);
    localStorage.removeItem(LAST_SYNCED_KEY);
  } catch {
    // Test cleanup only.
  }
}

export async function restartOfflineStoreForTests(): Promise<void> {
  const activeDatabase = databasePromise;
  databasePromise = undefined;
  backendPromise = undefined;
  if (activeDatabase) {
    const database = await activeDatabase.catch(() => undefined);
    database?.close();
  }
}
