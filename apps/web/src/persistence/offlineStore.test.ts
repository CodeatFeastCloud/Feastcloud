import { afterEach, describe, expect, it, vi } from 'vitest';
import { createInitialSnapshot, createOrder, createOutboxEvent } from '../domain/kitchen';
import {
  acknowledgeOutboxEvent,
  commitSnapshot,
  getLastSyncedAt,
  listOutbox,
  loadSnapshot,
  quarantineOutboxEvent,
  resetOfflineStoreForTests,
  restartOfflineStoreForTests,
} from './offlineStore';

function memoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    removeItem: (key) => { values.delete(key); },
    setItem: (key, value) => { values.set(key, String(value)); },
  };
}

async function seedVersionOneDatabase(input: {
  snapshot: ReturnType<typeof createInitialSnapshot>;
  events: ReturnType<typeof createOutboxEvent>[];
  lastOutboxSequence: number;
  lastSyncedAt: string;
}): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const request = indexedDB.open('feastcloud-edge', 1);
    request.onupgradeneeded = () => {
      const database = request.result;
      database.createObjectStore('snapshots');
      database.createObjectStore('outbox', { keyPath: 'id' });
      database.createObjectStore('meta');
    };
    request.onerror = () => reject(request.error ?? new Error('Could not seed legacy database'));
    request.onsuccess = () => {
      const database = request.result;
      const transaction = database.transaction(['snapshots', 'outbox', 'meta'], 'readwrite');
      transaction.objectStore('snapshots').put(input.snapshot, 'active-kitchen');
      input.events.forEach((event) => transaction.objectStore('outbox').put(event));
      transaction.objectStore('meta').put(input.lastOutboxSequence, 'last-outbox-sequence');
      transaction.objectStore('meta').put(input.lastSyncedAt, 'last-synced-at');
      transaction.oncomplete = () => {
        database.close();
        resolve();
      };
      transaction.onerror = () => reject(
        transaction.error ?? new Error('Could not write legacy database'),
      );
      transaction.onabort = () => reject(
        transaction.error ?? new Error('Legacy database transaction was aborted'),
      );
    };
  });
}

describe('offline store', () => {
  afterEach(async () => {
    vi.unstubAllGlobals();
    await resetOfflineStoreForTests();
  });

  it('commits local business state and its outbox event together', async () => {
    const result = createOrder(createInitialSnapshot(), {
      type: 'delivery',
      lines: [{ menuItemId: 'biryani', quantity: 1 }],
    });
    const event = createOutboxEvent(
      result.snapshot,
      'com.feastcloud.order.created.v1',
      result.order,
      { order: result.order },
    );

    await commitSnapshot(result.snapshot, event);

    expect((await loadSnapshot())?.orders[0].id).toBe(result.order.id);
    expect(await listOutbox()).toEqual([
      expect.objectContaining({ ...event, localSequence: 1, disposition: 'pending' }),
    ]);
  });

  it('only removes an outbox event after acknowledgement', async () => {
    const result = createOrder(createInitialSnapshot(), {
      type: 'takeaway',
      lines: [{ menuItemId: 'masala-fries', quantity: 1 }],
    });
    const event = createOutboxEvent(
      result.snapshot,
      'com.feastcloud.order.created.v1',
      result.order,
      { order: result.order },
    );
    await commitSnapshot(result.snapshot, event);

    await acknowledgeOutboxEvent(event.id, '2026-09-01T12:00:00.000Z');

    expect(await listOutbox()).toEqual([]);
    expect((await loadSnapshot())?.orders[0].id).toBe(result.order.id);
  });

  it('orders events by an atomic monotonic sequence instead of the device clock', async () => {
    const first = createOrder(
      createInitialSnapshot(),
      { type: 'takeaway', lines: [{ menuItemId: 'biryani', quantity: 1 }] },
      new Date('2026-09-01T12:05:00.000Z'),
    );
    const second = createOrder(
      first.snapshot,
      { type: 'delivery', lines: [{ menuItemId: 'masala-fries', quantity: 1 }] },
      new Date('2026-09-01T12:00:00.000Z'),
    );
    const firstEvent = createOutboxEvent(
      first.snapshot,
      'com.feastcloud.order.created.v1',
      first.order,
      { order: first.order },
    );
    const secondEvent = createOutboxEvent(
      second.snapshot,
      'com.feastcloud.order.created.v1',
      second.order,
      { order: second.order },
    );

    await commitSnapshot(first.snapshot, firstEvent);
    await commitSnapshot(second.snapshot, secondEvent);

    const events = await listOutbox();
    expect(events.map((event) => event.id)).toEqual([firstEvent.id, secondEvent.id]);
    expect(events.map((event) => event.localSequence)).toEqual([1, 2]);
  });

  it('retains quarantined commands without retrying them as pending work', async () => {
    const result = createOrder(createInitialSnapshot(), {
      type: 'delivery',
      lines: [{ menuItemId: 'biryani', quantity: 1 }],
    });
    const event = createOutboxEvent(
      result.snapshot,
      'com.feastcloud.order.created.v1',
      result.order,
      { order: result.order },
    );
    await commitSnapshot(result.snapshot, event);

    await quarantineOutboxEvent(event, 'Invalid command');

    expect(await listOutbox()).toEqual([]);
    expect(await listOutbox({ includeQuarantined: true })).toEqual([
      expect.objectContaining({
        id: event.id,
        disposition: 'quarantined',
        attempts: 1,
        lastError: 'Invalid command',
      }),
    ]);
  });

  it('pins the localStorage fallback and journals snapshot, outbox and sequence together', async () => {
    const indexedDBImplementation = globalThis.indexedDB;
    vi.stubGlobal('indexedDB', undefined);
    vi.stubGlobal('localStorage', memoryStorage());
    const result = createOrder(createInitialSnapshot(), {
      type: 'takeaway',
      lines: [{ menuItemId: 'masala-fries', quantity: 1 }],
    });
    const event = createOutboxEvent(
      result.snapshot,
      'com.feastcloud.order.created.v1',
      result.order,
      { order: result.order },
    );

    await commitSnapshot(result.snapshot, event);
    vi.stubGlobal('indexedDB', indexedDBImplementation);

    const localJournal = JSON.parse(localStorage.getItem('feastcloud.offline-state.v2') ?? '{}') as {
      snapshot?: { orders?: Array<{ id: string }> };
      outbox?: Array<{ id: string; localSequence: number }>;
      lastOutboxSequence?: number;
    };
    expect(localJournal.snapshot?.orders?.[0]?.id).toBe(result.order.id);
    expect(localJournal.outbox).toEqual([
      expect.objectContaining({ id: event.id, localSequence: 1 }),
    ]);
    expect(localJournal.lastOutboxSequence).toBe(1);
    expect((await listOutbox())[0]?.id).toBe(event.id);
  });

  it('migrates a fallback journal into IndexedDB before selecting it after restart', async () => {
    const indexedDBImplementation = globalThis.indexedDB;
    vi.stubGlobal('localStorage', memoryStorage());
    vi.stubGlobal('indexedDB', undefined);
    const first = createOrder(createInitialSnapshot(), {
      type: 'takeaway',
      lines: [{ menuItemId: 'biryani', quantity: 1 }],
    });
    const firstEvent = createOutboxEvent(
      first.snapshot,
      'com.feastcloud.order.created.v1',
      first.order,
      { order: first.order },
    );
    await commitSnapshot(first.snapshot, firstEvent);
    await acknowledgeOutboxEvent(firstEvent.id, '2026-09-01T12:00:00.000Z');

    const second = createOrder(first.snapshot, {
      type: 'delivery',
      lines: [{ menuItemId: 'masala-fries', quantity: 1 }],
    });
    const secondEvent = createOutboxEvent(
      second.snapshot,
      'com.feastcloud.order.created.v1',
      second.order,
      { order: second.order },
    );
    await commitSnapshot(second.snapshot, secondEvent);
    expect(localStorage.getItem('feastcloud.offline-state.v2')).not.toBeNull();

    vi.stubGlobal('indexedDB', indexedDBImplementation);
    await restartOfflineStoreForTests();

    expect(await loadSnapshot()).toEqual(second.snapshot);
    expect(await listOutbox()).toEqual([
      expect.objectContaining({ id: secondEvent.id, localSequence: 2 }),
    ]);
    expect(await getLastSyncedAt()).toBe('2026-09-01T12:00:00.000Z');
    expect(localStorage.getItem('feastcloud.offline-state.v2')).toBeNull();

    const third = createOrder(second.snapshot, {
      type: 'dineIn',
      lines: [{ menuItemId: 'mango-lassi', quantity: 1 }],
    });
    const thirdEvent = createOutboxEvent(
      third.snapshot,
      'com.feastcloud.order.created.v1',
      third.order,
      { order: third.order },
    );
    await commitSnapshot(third.snapshot, thirdEvent);

    expect((await listOutbox()).map((event) => [event.id, event.localSequence])).toEqual([
      [secondEvent.id, 2],
      [thirdEvent.id, 3],
    ]);
  });

  it('retains the fallback journal and leaves IndexedDB untouched when reconciliation is unsafe', async () => {
    vi.stubGlobal('localStorage', memoryStorage());
    const storedSnapshot = createInitialSnapshot(new Date('2026-09-01T12:00:00.000Z'));
    await commitSnapshot(storedSnapshot);
    await restartOfflineStoreForTests();

    const localResult = createOrder(storedSnapshot, {
      type: 'delivery',
      lines: [{ menuItemId: 'biryani', quantity: 1 }],
    });
    const unsafeSnapshot = { ...localResult.snapshot, outletId: 'outlet_unrelated' };
    const unsafeEvent = {
      ...createOutboxEvent(
        unsafeSnapshot,
        'com.feastcloud.order.created.v1',
        localResult.order,
        { order: localResult.order },
      ),
      outletId: 'outlet_unrelated',
      localSequence: 1,
      disposition: 'pending' as const,
    };
    localStorage.setItem('feastcloud.offline-state.v2', JSON.stringify({
      version: 2,
      snapshot: unsafeSnapshot,
      outbox: [unsafeEvent],
      lastOutboxSequence: 1,
      lastSyncedAt: '2026-09-01T12:01:00.000Z',
    }));

    // Unsafe cross-outlet reconciliation falls back to the intact local journal.
    expect((await loadSnapshot())?.outletId).toBe('outlet_unrelated');
    expect(localStorage.getItem('feastcloud.offline-state.v2')).not.toBeNull();

    // Removing the test journal reveals that the aborted transaction never changed IndexedDB.
    localStorage.removeItem('feastcloud.offline-state.v2');
    await restartOfflineStoreForTests();
    expect(await loadSnapshot()).toEqual(storedSnapshot);
  });

  it('upgrades legacy IndexedDB events in deterministic order before allocating a new sequence', async () => {
    const first = createOrder(
      createInitialSnapshot(),
      { type: 'takeaway', lines: [{ menuItemId: 'biryani', quantity: 1 }] },
      new Date('2026-09-01T12:05:00.000Z'),
    );
    const second = createOrder(
      first.snapshot,
      { type: 'delivery', lines: [{ menuItemId: 'masala-fries', quantity: 1 }] },
      new Date('2026-09-01T12:00:00.000Z'),
    );
    const third = createOrder(
      second.snapshot,
      { type: 'dineIn', lines: [{ menuItemId: 'mango-lassi', quantity: 1 }] },
      new Date('2026-09-01T11:00:00.000Z'),
    );
    const lateLegacyEvent = {
      ...createOutboxEvent(
        first.snapshot,
        'com.feastcloud.order.created.v1',
        first.order,
        { order: first.order },
      ),
      occurredAt: '2026-09-01T12:05:00.000Z',
    };
    const earlyLegacyEvent = {
      ...createOutboxEvent(
        second.snapshot,
        'com.feastcloud.order.created.v1',
        second.order,
        { order: second.order },
      ),
      occurredAt: '2026-09-01T12:00:00.000Z',
    };
    const sequencedLegacyEvent = {
      ...createOutboxEvent(
        third.snapshot,
        'com.feastcloud.order.created.v1',
        third.order,
        { order: third.order },
      ),
      occurredAt: '2026-09-01T11:00:00.000Z',
      localSequence: 7,
    };
    await seedVersionOneDatabase({
      snapshot: third.snapshot,
      // Reverse chronological insertion proves assignment does not depend on object-store order.
      events: [lateLegacyEvent, sequencedLegacyEvent, earlyLegacyEvent],
      lastOutboxSequence: 10,
      lastSyncedAt: '2026-08-31T18:30:00.000Z',
    });

    const fourth = createOrder(
      third.snapshot,
      { type: 'roomService', lines: [{ menuItemId: 'biryani', quantity: 1 }] },
      new Date('2026-09-01T12:10:00.000Z'),
    );
    const newEvent = createOutboxEvent(
      fourth.snapshot,
      'com.feastcloud.order.created.v1',
      fourth.order,
      { order: fourth.order },
    );
    await commitSnapshot(fourth.snapshot, newEvent);

    expect((await listOutbox()).map((event) => [event.id, event.localSequence])).toEqual([
      [sequencedLegacyEvent.id, 7],
      [earlyLegacyEvent.id, 11],
      [lateLegacyEvent.id, 12],
      [newEvent.id, 13],
    ]);
    expect(await getLastSyncedAt()).toBe('2026-08-31T18:30:00.000Z');
    expect(await loadSnapshot()).toEqual(fourth.snapshot);
  });

  it('propagates a durability failure after the fallback backend is selected', async () => {
    vi.stubGlobal('indexedDB', undefined);
    vi.stubGlobal('localStorage', memoryStorage());
    await loadSnapshot();
    const result = createOrder(createInitialSnapshot(), {
      type: 'delivery',
      lines: [{ menuItemId: 'biryani', quantity: 1 }],
    });
    const event = createOutboxEvent(
      result.snapshot,
      'com.feastcloud.order.created.v1',
      result.order,
      { order: result.order },
    );
    const failure = vi.spyOn(globalThis.localStorage, 'setItem').mockImplementation(() => {
      throw new DOMException('Quota exceeded', 'QuotaExceededError');
    });

    await expect(commitSnapshot(result.snapshot, event)).rejects.toThrow('Quota exceeded');
    failure.mockRestore();
  });
});
