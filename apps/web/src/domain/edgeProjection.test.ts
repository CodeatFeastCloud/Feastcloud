import { afterEach, describe, expect, it, vi } from 'vitest';
import { advanceOrder, createInitialSnapshot, createOrder } from './kitchen';
import {
  edgeApiBase,
  fetchEdgeDiscovery,
  fetchEdgeOrders,
  fetchEdgeTickets,
  mapEdgeOrder,
  mapEdgeTicket,
  mergeEdgeOrders,
  mergeEdgeProjection,
} from './edgeProjection';

const edgeOrder = {
  id: '019cfeb0-0001-7000-8000-000000000010',
  tenantId: '11111111-1111-4111-8111-111111111111',
  outletId: '33333333-3333-4333-8333-333333333333',
  number: 7,
  type: 'roomService',
  guestName: 'Room 412',
  status: 'accepted',
  lines: [
    {
      id: '019cfeb0-0001-7000-8000-000000000011',
      menuItemId: 'biryani',
      name: 'Biryani',
      quantity: 1,
      stationId: 'hot',
      preparationNote: 'No chilli',
    },
  ],
  placedAt: '2026-08-03T03:36:00Z',
  targetAt: '2026-08-03T03:51:00Z',
  createdAt: '2026-08-03T03:36:01Z',
  updatedAt: '2026-08-03T03:37:00Z',
  version: 2,
};

const edgeTicket = {
  id: '019cfeb0-0001-7000-8000-000000000020',
  tenantId: '11111111-1111-4111-8111-111111111111',
  outletId: '33333333-3333-4333-8333-333333333333',
  orderId: edgeOrder.id,
  stationId: 'hot',
  lineIds: [edgeOrder.lines[0].id],
  status: 'fired',
  priority: 2,
  targetAt: edgeOrder.targetAt,
  createdAt: edgeOrder.createdAt,
  updatedAt: edgeOrder.updatedAt,
  version: 2,
};

describe('edge projection', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('normalizes configured edge API roots', () => {
    expect(edgeApiBase('http://localhost:8081')).toBe('http://localhost:8081/api/v1');
    expect(edgeApiBase('http://localhost:8081/api/v1/')).toBe('http://localhost:8081/api/v1');
    expect(edgeApiBase(undefined)).toBeUndefined();
  });

  it('maps durable edge order fields into the offline UI projection', () => {
    expect(mapEdgeOrder(edgeOrder)).toMatchObject({
      id: edgeOrder.id,
      number: 7,
      type: 'roomService',
      guestName: 'Room 412',
      status: 'fired',
      dueAt: edgeOrder.targetAt,
      version: 2,
      origin: 'edge',
      lines: [expect.objectContaining({ name: 'Biryani', stationId: 'hot', note: 'No chilli' })],
    });
  });

  it('maps station tickets without losing edge version or routing evidence', () => {
    expect(mapEdgeTicket(edgeTicket)).toMatchObject({
      id: edgeTicket.id,
      orderId: edgeOrder.id,
      stationId: 'hot',
      lineIds: edgeTicket.lineIds,
      status: 'fired',
      priority: 2,
      version: 2,
      origin: 'edge',
    });
  });

  it('preserves outlet-defined station IDs in order and ticket projections', () => {
    const stationId = 'dessert-pass';
    const order = mapEdgeOrder({
      ...edgeOrder,
      lines: [{ ...edgeOrder.lines[0], stationId }],
    });
    const ticket = mapEdgeTicket({ ...edgeTicket, stationId });

    expect(order.lines[0].stationId).toBe(stationId);
    expect(ticket.stationId).toBe(stationId);
  });

  it('discovers the edge-owned tenant and outlet scope', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      data: {
        service: 'feastcloud-edge',
        edgeId: 'edge-indiranagar-1',
        tenantId: 'tenant-live',
        outletId: 'outlet-live',
      },
    }), { status: 200 })));

    await expect(fetchEdgeDiscovery('http://edge.test/api/v1')).resolves.toEqual({
      edgeId: 'edge-indiranagar-1',
      tenantId: 'tenant-live',
      outletId: 'outlet-live',
    });
    expect(fetch).toHaveBeenCalledWith(
      'http://edge.test/api/v1',
      expect.objectContaining({ cache: 'no-store' }),
    );
  });

  it('removes demo data while preserving a newer unsynchronized local order', () => {
    const created = createOrder(createInitialSnapshot(), {
      type: 'takeaway',
      lines: [{ menuItemId: 'biryani', quantity: 1 }],
    });
    const merged = mergeEdgeOrders(
      created.snapshot,
      [mapEdgeOrder(edgeOrder)],
      new Set([created.order.id]),
    );

    expect(new Set(merged.orders.map((order) => order.id))).toEqual(
      new Set([created.order.id, edgeOrder.id]),
    );
    expect(merged.orders.some((order) => order.origin === 'demo')).toBe(false);
  });

  it('rolls a quarantined local projection back to confirmed edge state', () => {
    const remote = mapEdgeOrder({ ...edgeOrder, status: 'received', version: 1 });
    const current = {
      ...createInitialSnapshot(),
      orders: [remote],
      tickets: [mapEdgeTicket({ ...edgeTicket, status: 'queued', version: 1 })],
    };
    const advanced = advanceOrder(current, remote.id);
    const merged = mergeEdgeProjection(
      advanced.snapshot,
      [remote],
      [mapEdgeTicket({ ...edgeTicket, status: 'queued', version: 1 })],
      new Set(),
      new Set(),
    );

    expect(merged.orders[0].status).toBe('queued');
    expect(merged.orders[0].origin).toBe('edge');
  });

  it('does not infer deletion when an active projection is absent from a limited page', () => {
    const current = {
      ...createInitialSnapshot(),
      orders: [mapEdgeOrder(edgeOrder)],
      tickets: [mapEdgeTicket(edgeTicket)],
    };
    const merged = mergeEdgeProjection(current, [], [], new Set(), new Set());

    expect(merged.orders.map((order) => order.id)).toContain(edgeOrder.id);
    expect(merged.tickets?.map((ticket) => ticket.id)).toContain(edgeTicket.id);
  });

  it('pulls terminal projections so another device can clear completed work', async () => {
    const fetchMock = vi.fn().mockImplementation(async () =>
      new Response(JSON.stringify({ data: [] }), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);

    await Promise.all([
      fetchEdgeOrders('http://edge.test/api/v1'),
      fetchEdgeTickets('http://edge.test/api/v1'),
    ]);

    const requested = fetchMock.mock.calls.map(([url]) => String(url));
    expect(requested).toContain('http://edge.test/api/v1/orders?status=completed&limit=500');
    expect(requested).toContain('http://edge.test/api/v1/orders?status=cancelled&limit=500');
    expect(requested).toContain('http://edge.test/api/v1/kitchen-tickets?status=completed&limit=500');
    expect(requested).toContain('http://edge.test/api/v1/kitchen-tickets?status=cancelled&limit=500');
  });

  it('deduplicates status-query races using version and update evidence', async () => {
    const fetchMock = vi.fn().mockImplementation(async (request: RequestInfo | URL) => {
      const status = new URL(String(request)).searchParams.get('status');
      const data = status === 'received'
        ? [{ ...edgeOrder, status: 'received', version: 2, updatedAt: '2026-08-03T03:39:00Z' }]
        : status === 'accepted'
          ? [{ ...edgeOrder, status: 'accepted', version: 3, updatedAt: '2026-08-03T03:38:00Z' }]
          : status === 'preparing'
            ? [{ ...edgeOrder, status: 'preparing', version: 3, updatedAt: '2026-08-03T03:40:00Z' }]
            : [];
      return new Response(JSON.stringify({ data }), { status: 200 });
    });
    vi.stubGlobal('fetch', fetchMock);

    const orders = await fetchEdgeOrders('http://edge.test/api/v1');

    expect(orders).toHaveLength(1);
    expect(orders[0]).toMatchObject({
      id: edgeOrder.id,
      status: 'preparing',
      version: 3,
      updatedAt: '2026-08-03T03:40:00Z',
    });
  });

  it('applies the status-query consistency guard to station tickets', async () => {
    const fetchMock = vi.fn().mockImplementation(async (request: RequestInfo | URL) => {
      const status = new URL(String(request)).searchParams.get('status');
      const data = status === 'fired'
        ? [{ ...edgeTicket, status: 'fired', version: 2, updatedAt: '2026-08-03T03:41:00Z' }]
        : status === 'preparing'
          ? [{ ...edgeTicket, status: 'preparing', version: 3, updatedAt: '2026-08-03T03:40:00Z' }]
          : status === 'ready'
            ? [{ ...edgeTicket, status: 'ready', version: 3, updatedAt: '2026-08-03T03:42:00Z' }]
            : [];
      return new Response(JSON.stringify({ data }), { status: 200 });
    });
    vi.stubGlobal('fetch', fetchMock);

    const tickets = await fetchEdgeTickets('http://edge.test/api/v1');

    expect(tickets).toHaveLength(1);
    expect(tickets[0]).toMatchObject({
      id: edgeTicket.id,
      status: 'ready',
      version: 3,
      updatedAt: '2026-08-03T03:42:00Z',
    });
  });

  it('replaces stale active cards with terminal state from another device', () => {
    const current = {
      ...createInitialSnapshot(),
      orders: [mapEdgeOrder(edgeOrder)],
      tickets: [mapEdgeTicket(edgeTicket)],
    };
    const merged = mergeEdgeProjection(
      current,
      [mapEdgeOrder({ ...edgeOrder, status: 'completed', version: 5 })],
      [mapEdgeTicket({ ...edgeTicket, status: 'completed', version: 5 })],
      new Set(),
      new Set(),
    );

    expect(merged.orders[0].status).toBe('completed');
    expect(merged.tickets?.[0].status).toBe('completed');
  });
});
