import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { resetOfflineStoreForTests } from '../persistence/offlineStore';
import { shouldAttemptOutletSync, useKitchenSystem, viewFromSearch } from './useKitchenSystem';

describe('kitchen system state serialization', () => {
  afterEach(async () => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    await resetOfflineStoreForTests();
  });

  it('still targets a configured LAN edge when public internet is unavailable', () => {
    expect(shouldAttemptOutletSync('http://edge.local/api/v1', false)).toBe(true);
    expect(shouldAttemptOutletSync(undefined, false)).toBe(false);
  });

  it('opens New Order from both the public and canonical URL names', () => {
    expect(viewFromSearch('?view=order')).toBe('orders');
    expect(viewFromSearch('?view=orders')).toBe('orders');
    expect(viewFromSearch('?view=new-order')).toBe('orders');
    expect(viewFromSearch('?view=unknown')).toBeUndefined();
  });

  it('hydrates into the durable local journal when IndexedDB open never settles', async () => {
    vi.useFakeTimers();
    const values = new Map<string, string>();
    const localStorageFallback: Storage = {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (key) => values.get(key) ?? null,
      key: (index) => [...values.keys()][index] ?? null,
      removeItem: (key) => { values.delete(key); },
      setItem: (key, value) => { values.set(key, String(value)); },
    };
    const open = vi.fn(() => ({} as IDBOpenDBRequest));
    vi.stubGlobal('localStorage', localStorageFallback);
    vi.stubGlobal('indexedDB', { open });

    const { result, unmount } = renderHook(() => useKitchenSystem());
    expect(result.current.hydrated).toBe(false);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_999);
    });
    expect(result.current.hydrated).toBe(false);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(result.current.hydrated).toBe(true);
    expect(open).toHaveBeenCalledTimes(1);
    const journal = JSON.parse(
      localStorageFallback.getItem('feastcloud.offline-state.v2') ?? '{}',
    ) as { version?: number; snapshot?: { organizationId?: string; outletId?: string } };
    expect(journal).toEqual(expect.objectContaining({
      version: 2,
      snapshot: expect.objectContaining({
        organizationId: result.current.snapshot.organizationId,
        outletId: result.current.snapshot.outletId,
      }),
    }));
    unmount();
  });

  it('does not lose one station update when two cards advance concurrently', async () => {
    vi.spyOn(window.navigator, 'onLine', 'get').mockReturnValue(false);
    const { result, unmount } = renderHook(() => useKitchenSystem());
    await waitFor(() => expect(result.current.hydrated).toBe(true));

    const orderId = '01991f31-0001-7000-8000-000000000104';
    const tickets = result.current.snapshot.tickets?.filter(
      (ticket) => ticket.orderId === orderId,
    ) ?? [];
    expect(tickets).toHaveLength(2);

    await act(async () => {
      await Promise.all(tickets.map((ticket) => result.current.moveTicketForward(ticket.id)));
    });

    const advanced = result.current.snapshot.tickets?.filter(
      (ticket) => ticket.orderId === orderId,
    ) ?? [];
    expect(advanced.every((ticket) => ticket.status === 'ready')).toBe(true);
    expect(result.current.snapshot.orders.find((order) => order.id === orderId)?.status).toBe('ready');
    unmount();
  });
});
