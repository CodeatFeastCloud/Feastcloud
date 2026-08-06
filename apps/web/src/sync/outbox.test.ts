import { afterEach, describe, expect, it, vi } from 'vitest';
import { createInitialSnapshot, createOrder, createOutboxEvent } from '../domain/kitchen';
import type { KitchenSnapshot, OutboxEvent } from '../domain/types';
import {
  commitSnapshot,
  listOutbox,
  resetOfflineStoreForTests,
} from '../persistence/offlineStore';
import {
  drainPendingOutbox,
  OutboxTransmissionError,
  transmitOutboxEvent,
} from './outbox';

function addOrder(
  snapshot: KitchenSnapshot,
  menuItemId: string,
): { snapshot: KitchenSnapshot; event: OutboxEvent } {
  const result = createOrder(snapshot, {
    type: 'takeaway',
    lines: [{ menuItemId, quantity: 1 }],
  });
  return {
    snapshot: result.snapshot,
    event: createOutboxEvent(
      result.snapshot,
      'com.feastcloud.order.created.v1',
      result.order,
      { order: result.order },
    ),
  };
}

describe('outbox drain', () => {
  afterEach(async () => {
    vi.unstubAllGlobals();
    await resetOfflineStoreForTests();
  });

  it('quarantines a permanent failure and continues with independent later work', async () => {
    const first = addOrder(createInitialSnapshot(), 'biryani');
    const second = addOrder(first.snapshot, 'masala-fries');
    await commitSnapshot(first.snapshot, first.event);
    await commitSnapshot(second.snapshot, second.event);
    const transmitted: string[] = [];

    const result = await drainPendingOutbox(async (event) => {
      transmitted.push(event.id);
      if (event.id === first.event.id) {
        throw new OutboxTransmissionError('version conflict', { permanent: true, status: 409 });
      }
    });

    expect(transmitted).toEqual([first.event.id, second.event.id]);
    expect(result).toMatchObject({ acknowledged: 1, quarantined: 1 });
    expect(await listOutbox()).toEqual([]);
    expect(await listOutbox({ includeQuarantined: true })).toEqual([
      expect.objectContaining({ id: first.event.id, disposition: 'quarantined' }),
    ]);
  });

  it('retains sequence and stops after a transient failure', async () => {
    const first = addOrder(createInitialSnapshot(), 'biryani');
    const second = addOrder(first.snapshot, 'masala-fries');
    await commitSnapshot(first.snapshot, first.event);
    await commitSnapshot(second.snapshot, second.event);
    const transmitted: string[] = [];

    const result = await drainPendingOutbox(async (event) => {
      transmitted.push(event.id);
      throw new OutboxTransmissionError('edge unavailable', { permanent: false });
    });

    expect(transmitted).toEqual([first.event.id]);
    expect(result.transientError?.message).toBe('edge unavailable');
    expect(await listOutbox()).toEqual([
      expect.objectContaining({ id: first.event.id, attempts: 1 }),
      expect.objectContaining({ id: second.event.id, attempts: 0 }),
    ]);
  });

  it('continues draining an event appended during an active transmission', async () => {
    const first = addOrder(createInitialSnapshot(), 'biryani');
    const second = addOrder(first.snapshot, 'masala-fries');
    await commitSnapshot(first.snapshot, first.event);
    const transmitted: string[] = [];

    await drainPendingOutbox(async (event) => {
      transmitted.push(event.id);
      if (event.id === first.event.id) await commitSnapshot(second.snapshot, second.event);
    });

    expect(transmitted).toEqual([first.event.id, second.event.id]);
    expect(await listOutbox()).toEqual([]);
  });

  it('classifies a non-retryable edge 4xx response as permanent', async () => {
    const created = addOrder(createInitialSnapshot(), 'biryani');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ code: 'invalid_payload', detail: 'Invalid payload', retryable: false }),
      { status: 422, headers: { 'Content-Type': 'application/problem+json' } },
    )));

    await expect(transmitOutboxEvent(created.event, 'http://edge.test/api/v1')).rejects.toMatchObject({
      message: 'Invalid payload',
      permanent: true,
      status: 422,
    });
  });
});
