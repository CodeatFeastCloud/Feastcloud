import { describe, expect, it } from 'vitest';
import {
  advanceOrder,
  advanceTicket,
  createInitialSnapshot,
  createOrder,
  createOutboxEvent,
  createTicketOutboxEvent,
  getOrderSubtotal,
  getOrderTax,
  reconcileExistingAggregatorOrder,
} from './kitchen';

describe('kitchen domain', () => {
  it('creates a reproducible ticket and advances the order sequence', () => {
    const snapshot = createInitialSnapshot(new Date('2026-09-01T12:00:00.000Z'));
    const result = createOrder(
      snapshot,
      {
        type: 'takeaway',
        guestName: '  Riya  ',
        lines: [
          { menuItemId: 'butter-chicken-bowl', quantity: 2 },
          { menuItemId: 'nimbu-soda', quantity: 1 },
        ],
      },
      new Date('2026-09-01T12:05:00.000Z'),
    );

    expect(result.order.number).toBe(108);
    expect(result.order.guestName).toBe('Riya');
    expect(result.order.dueAt).toBe('2026-09-01T12:19:00.000Z');
    expect(result.snapshot.nextOrderNumber).toBe(109);
    expect(result.snapshot.orders[0].id).toBe(result.order.id);
    const tickets = result.snapshot.tickets?.filter((ticket) => ticket.orderId === result.order.id) ?? [];
    expect(tickets).toHaveLength(2);
    expect(new Set(tickets.map((ticket) => ticket.stationId))).toEqual(new Set(['hot', 'beverage']));
    expect(tickets.every((ticket) => ticket.origin === 'local' && ticket.status === 'queued')).toBe(true);
  });

  it('keeps money in integer minor units', () => {
    const result = createOrder(createInitialSnapshot(), {
      type: 'delivery',
      lines: [
        { menuItemId: 'paneer-tikka-bowl', quantity: 2 },
        { menuItemId: 'mango-lassi', quantity: 1 },
      ],
    });

    expect(getOrderSubtotal(result.order)).toBe(71_700);
    expect(getOrderTax(result.order)).toBe(3_585);
    expect(Number.isInteger(getOrderTax(result.order))).toBe(true);
  });

  it('enforces the kitchen ticket state machine', () => {
    const initial = createOrder(createInitialSnapshot(), {
      type: 'dineIn',
      lines: [{ menuItemId: 'biryani', quantity: 1 }],
    });

    const preparing = advanceOrder(initial.snapshot, initial.order.id);
    const cooking = advanceOrder(preparing.snapshot, initial.order.id);
    const ready = advanceOrder(cooking.snapshot, initial.order.id);
    const completed = advanceOrder(ready.snapshot, initial.order.id);

    expect(preparing.order.status).toBe('fired');
    expect(cooking.order.status).toBe('preparing');
    expect(ready.order.status).toBe('ready');
    expect(completed.order.status).toBe('completed');
    expect(() => advanceOrder(completed.snapshot, initial.order.id)).toThrow(/already completed/);
  });

  it('creates a contract-compatible mutation envelope', () => {
    const result = createOrder(createInitialSnapshot(), {
      type: 'takeaway',
      lines: [{ menuItemId: 'kathi-roll', quantity: 1 }],
    });
    const event = createOutboxEvent(
      result.snapshot,
      'com.feastcloud.order.created.v1',
      result.order,
      { order: result.order },
    );
    const { attempts, ...envelope } = event;

    expect(attempts).toBe(0);
    expect(envelope.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(envelope.schemaVersion).toBe('1.0');
    expect(envelope.source).toBe('feastcloud.web');
    expect(envelope.idempotencyKey).toBe(envelope.id);
    expect(envelope.payload).toMatchObject({
      eventType: 'com.feastcloud.order.created.v1',
      aggregateType: 'order',
      aggregateId: result.order.id,
    });
  });

  it('identifies whole-order KDS advances as order aggregate events', () => {
    const created = createOrder(createInitialSnapshot(), {
      type: 'dineIn',
      lines: [{ menuItemId: 'biryani', quantity: 1 }],
    });
    const advanced = advanceOrder(created.snapshot, created.order.id);
    const event = createOutboxEvent(
      advanced.snapshot,
      'com.feastcloud.order.status-changed.v1',
      advanced.order,
      {
        orderId: advanced.order.id,
        toStatus: advanced.order.status,
        expectedVersion: advanced.order.version - 1,
      },
    );

    expect(event.payload).toMatchObject({
      eventType: 'com.feastcloud.order.status-changed.v1',
      aggregateType: 'order',
      aggregateId: advanced.order.id,
      orderId: advanced.order.id,
      toStatus: 'fired',
      expectedVersion: 1,
    });
  });

  it('repairs a legacy aggregator ticket without creating a duplicate order', () => {
    const created = createOrder(createInitialSnapshot(), {
      type: 'delivery',
      note: 'Swiggy · Partner order SW-101 · Keep gravy separate',
      lines: [{ menuItemId: 'biryani', quantity: 2 }],
    });
    const repaired = reconcileExistingAggregatorOrder(created.snapshot, {
      type: 'delivery',
      note: 'Keep gravy separate',
      aggregator: {
        provider: 'Swiggy',
        brandName: 'Dalchini Kitchen, Sarjapura',
        externalOrderId: 'SW-101',
        externalOutletId: '1211361',
      },
      lines: [{ menuItemId: 'biryani', quantity: 2 }],
    });

    expect(repaired?.changed).toBe(true);
    expect(repaired?.snapshot.orders).toHaveLength(created.snapshot.orders.length);
    expect(repaired?.order.id).toBe(created.order.id);
    expect(repaired?.order.note).toBe('Keep gravy separate');
    expect(repaired?.order.aggregator).toMatchObject({
      provider: 'Swiggy',
      brandName: 'Dalchini Kitchen, Sarjapura',
      externalOrderId: 'SW-101',
    });
  });

  it('keeps whole-order optimistic progress aligned with every station ticket', () => {
    const snapshot = createInitialSnapshot(new Date('2026-09-01T12:00:00.000Z'));
    const orderId = '01991f31-0001-7000-8000-000000000104';
    const advanced = advanceOrder(snapshot, orderId, new Date('2026-09-01T12:01:00.000Z'));
    const tickets = advanced.snapshot.tickets?.filter((ticket) => ticket.orderId === orderId) ?? [];

    expect(advanced.order.status).toBe('ready');
    expect(tickets).toHaveLength(2);
    expect(tickets.every((ticket) => ticket.status === 'ready' && ticket.origin === 'local')).toBe(true);
  });

  it('advances one station ticket and derives the order only from aggregate progress', () => {
    const snapshot = createInitialSnapshot(new Date('2026-09-01T12:00:00.000Z'));
    const orderId = '01991f31-0001-7000-8000-000000000104';
    const tickets = snapshot.tickets?.filter((ticket) => ticket.orderId === orderId) ?? [];
    const hot = tickets.find((ticket) => ticket.stationId === 'hot');
    const beverage = tickets.find((ticket) => ticket.stationId === 'beverage');
    if (!hot || !beverage) throw new Error('test fixture is missing station tickets');

    const hotReady = advanceTicket(snapshot, hot.id, new Date('2026-09-01T12:01:00.000Z'));
    expect(hotReady.ticket.status).toBe('ready');
    expect(hotReady.order.status).toBe('preparing');

    const beverageReady = advanceTicket(
      hotReady.snapshot,
      beverage.id,
      new Date('2026-09-01T12:02:00.000Z'),
    );
    expect(beverageReady.ticket.status).toBe('ready');
    expect(beverageReady.order.status).toBe('ready');

    const event = createTicketOutboxEvent(
      beverageReady.snapshot,
      beverageReady.ticket,
      beverageReady.order,
    );
    expect(event.payload).toMatchObject({
      eventType: 'com.feastcloud.kitchen-ticket.status-changed.v1',
      aggregateType: 'kitchenTicket',
      aggregateId: beverage.id,
      ticketId: beverage.id,
      orderId,
      toStatus: 'ready',
      expectedVersion: beverage.version,
    });
  });
});
