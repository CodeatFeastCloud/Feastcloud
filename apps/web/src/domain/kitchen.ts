import { catalogById } from './catalog';
import type {
  KitchenOrder,
  KitchenSnapshot,
  KitchenTicket,
  OrderLine,
  OrderType,
  OutboxEvent,
  StationId,
  TicketStatus,
} from './types';

const transitionMap: Record<TicketStatus, TicketStatus | undefined> = {
  queued: 'fired',
  fired: 'preparing',
  preparing: 'ready',
  ready: 'completed',
  completed: undefined,
  cancelled: undefined,
};

export function canAdvanceOrder(snapshot: KitchenSnapshot, orderId: string): boolean {
  const order = snapshot.orders.find((candidate) => candidate.id === orderId);
  const nextStatus = order ? transitionMap[order.status] : undefined;
  if (!order || !nextStatus) return false;
  const tickets = (snapshot.tickets ?? []).filter((ticket) => ticket.orderId === orderId);
  if (tickets.length === 0) return true;
  let changed = false;
  for (const ticket of tickets) {
    if (ticket.status === nextStatus) continue;
    if (transitionMap[ticket.status] !== nextStatus) return false;
    changed = true;
  }
  return changed;
}

/** Creates a lowercase RFC 9562 UUIDv7 with the current Unix-millisecond prefix. */
export function createUuidV7(timestamp = Date.now()): string {
  const bytes = new Uint8Array(16);
  if (globalThis.crypto?.getRandomValues) {
    globalThis.crypto.getRandomValues(bytes);
  } else {
    for (let index = 0; index < bytes.length; index += 1) {
      bytes[index] = Math.floor(Math.random() * 256);
    }
  }

  bytes[0] = Math.floor(timestamp / 2 ** 40) & 0xff;
  bytes[1] = Math.floor(timestamp / 2 ** 32) & 0xff;
  bytes[2] = Math.floor(timestamp / 2 ** 24) & 0xff;
  bytes[3] = Math.floor(timestamp / 2 ** 16) & 0xff;
  bytes[4] = Math.floor(timestamp / 2 ** 8) & 0xff;
  bytes[5] = timestamp & 0xff;
  bytes[6] = (bytes[6] & 0x0f) | 0x70;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;

  const hex = Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function createDeviceId(): string {
  return `device_${createUuidV7()}`;
}

export function createInitialSnapshot(now = new Date()): KitchenSnapshot {
  const ago = (minutes: number) => new Date(now.getTime() - minutes * 60_000).toISOString();
  const ahead = (minutes: number) => new Date(now.getTime() + minutes * 60_000).toISOString();

  return {
    schemaVersion: 1,
    organizationId: '11111111-1111-4111-8111-111111111111',
    outletId: '33333333-3333-4333-8333-333333333333',
    nextOrderNumber: 108,
    orders: [
      {
        id: '01991f31-0001-7000-8000-000000000104',
        number: 104,
        type: 'delivery',
        guestName: 'Aarav',
        lines: [
          { id: '01991f31-0001-7000-8000-000000000201', menuItemId: 'butter-chicken-bowl', quantity: 2 },
          { id: '01991f31-0001-7000-8000-000000000202', menuItemId: 'nimbu-soda', quantity: 1, note: 'Less ice' },
        ],
        status: 'preparing',
        createdAt: ago(11),
        updatedAt: ago(7),
        dueAt: ahead(3),
        version: 3,
        origin: 'demo',
      },
      {
        id: '01991f31-0001-7000-8000-000000000105',
        number: 105,
        type: 'takeaway',
        guestName: 'Meera',
        lines: [
          { id: '01991f31-0001-7000-8000-000000000203', menuItemId: 'paneer-tikka-bowl', quantity: 1 },
          { id: '01991f31-0001-7000-8000-000000000204', menuItemId: 'masala-fries', quantity: 1 },
        ],
        status: 'queued',
        createdAt: ago(5),
        updatedAt: ago(5),
        dueAt: ahead(7),
        version: 1,
        origin: 'demo',
      },
      {
        id: '01991f31-0001-7000-8000-000000000106',
        number: 106,
        type: 'dineIn',
        tableLabel: 'T4',
        lines: [
          { id: '01991f31-0001-7000-8000-000000000205', menuItemId: 'biryani', quantity: 1 },
          { id: '01991f31-0001-7000-8000-000000000206', menuItemId: 'mango-lassi', quantity: 2 },
        ],
        status: 'ready',
        createdAt: ago(18),
        updatedAt: ago(2),
        dueAt: ago(2),
        version: 4,
        origin: 'demo',
      },
    ],
    tickets: [
      {
        id: '01991f31-0001-7000-8000-000000000301',
        orderId: '01991f31-0001-7000-8000-000000000104',
        stationId: 'hot',
        lineIds: ['01991f31-0001-7000-8000-000000000201'],
        status: 'preparing',
        priority: 0,
        targetAt: ahead(3),
        createdAt: ago(11),
        updatedAt: ago(7),
        version: 3,
        origin: 'demo',
      },
      {
        id: '01991f31-0001-7000-8000-000000000302',
        orderId: '01991f31-0001-7000-8000-000000000104',
        stationId: 'beverage',
        lineIds: ['01991f31-0001-7000-8000-000000000202'],
        status: 'preparing',
        priority: 0,
        targetAt: ahead(3),
        createdAt: ago(11),
        updatedAt: ago(7),
        version: 3,
        origin: 'demo',
      },
      {
        id: '01991f31-0001-7000-8000-000000000303',
        orderId: '01991f31-0001-7000-8000-000000000105',
        stationId: 'hot',
        lineIds: [
          '01991f31-0001-7000-8000-000000000203',
          '01991f31-0001-7000-8000-000000000204',
        ],
        status: 'queued',
        priority: 0,
        targetAt: ahead(7),
        createdAt: ago(5),
        updatedAt: ago(5),
        version: 1,
        origin: 'demo',
      },
      {
        id: '01991f31-0001-7000-8000-000000000304',
        orderId: '01991f31-0001-7000-8000-000000000106',
        stationId: 'hot',
        lineIds: ['01991f31-0001-7000-8000-000000000205'],
        status: 'ready',
        priority: 0,
        targetAt: ago(2),
        createdAt: ago(18),
        updatedAt: ago(2),
        version: 4,
        origin: 'demo',
      },
      {
        id: '01991f31-0001-7000-8000-000000000305',
        orderId: '01991f31-0001-7000-8000-000000000106',
        stationId: 'beverage',
        lineIds: ['01991f31-0001-7000-8000-000000000206'],
        status: 'ready',
        priority: 0,
        targetAt: ago(2),
        createdAt: ago(18),
        updatedAt: ago(2),
        version: 4,
        origin: 'demo',
      },
    ],
  };
}

export interface NewOrderInput {
  type: OrderType;
  guestName?: string;
  tableLabel?: string;
  note?: string;
  aggregator?: KitchenOrder['aggregator'];
  lines: Array<Pick<OrderLine, 'menuItemId' | 'quantity' | 'note' | 'name' | 'stationId'> & { prepMinutes?: number }>;
}

/** Reuse an aggregator order and repair notes written by older clients. */
export function reconcileExistingAggregatorOrder(
  snapshot: KitchenSnapshot,
  input: NewOrderInput,
): { snapshot: KitchenSnapshot; order: KitchenOrder; changed: boolean } | undefined {
  const source = input.aggregator;
  if (!source) return undefined;

  const provider = source.provider.trim().toLowerCase();
  const existing = snapshot.orders.find((order) =>
    (order.aggregator?.provider.trim().toLowerCase() === provider &&
      order.aggregator.externalOrderId === source.externalOrderId) ||
    (!order.aggregator && order.note?.includes(`Partner order ${source.externalOrderId}`)),
  );
  if (!existing) return undefined;

  const note = input.note?.trim() || undefined;
  const changed = existing.aggregator?.provider !== source.provider ||
    existing.aggregator?.brandName !== source.brandName ||
    existing.aggregator?.externalOrderId !== source.externalOrderId ||
    existing.aggregator?.externalOutletId !== source.externalOutletId ||
    existing.note !== note;
  if (!changed) return { snapshot, order: existing, changed: false };

  const repaired: KitchenOrder = { ...existing, note, aggregator: source };
  return {
    changed: true,
    order: repaired,
    snapshot: {
      ...snapshot,
      orders: snapshot.orders.map((order) => order.id === repaired.id ? repaired : order),
    },
  };
}

export function createOrder(
  snapshot: KitchenSnapshot,
  input: NewOrderInput,
  now = new Date(),
): { snapshot: KitchenSnapshot; order: KitchenOrder } {
  const lines = input.lines
    .filter((line) => line.quantity > 0 && (catalogById.has(line.menuItemId) || Boolean(line.name && line.stationId)))
    .map((line) => ({ ...line, id: createUuidV7(now.getTime()) }));

  if (lines.length === 0) throw new Error('Order must include at least one available item');

  const prepMinutes = Math.max(
    ...lines.map((line) => line.prepMinutes ?? catalogById.get(line.menuItemId)?.prepMinutes ?? 12),
  );
  const createdAt = now.toISOString();
  const order: KitchenOrder = {
    id: createUuidV7(now.getTime()),
    number: snapshot.nextOrderNumber,
    type: input.type,
    guestName: input.guestName?.trim() || undefined,
    tableLabel: input.tableLabel?.trim() || undefined,
    note: input.note?.trim() || undefined,
    aggregator: input.aggregator,
    lines,
    status: 'queued',
    createdAt,
    updatedAt: createdAt,
    dueAt: new Date(now.getTime() + prepMinutes * 60_000).toISOString(),
    version: 1,
    origin: 'local',
  };
  const stationLines = new Map<StationId, string[]>();
  for (const line of lines) {
    const stationId = line.stationId ?? catalogById.get(line.menuItemId)?.station ?? 'hot';
    stationLines.set(stationId, [...(stationLines.get(stationId) ?? []), line.id]);
  }
  const tickets: KitchenTicket[] = [...stationLines].map(([stationId, lineIds]) => ({
    id: createUuidV7(now.getTime()),
    orderId: order.id,
    stationId,
    lineIds,
    status: 'queued',
    priority: 0,
    targetAt: order.dueAt,
    createdAt,
    updatedAt: createdAt,
    version: 1,
    origin: 'local',
  }));

  return {
    order,
    snapshot: {
      ...snapshot,
      nextOrderNumber: snapshot.nextOrderNumber + 1,
      orders: [order, ...snapshot.orders],
      tickets: [...tickets, ...(snapshot.tickets ?? [])],
    },
  };
}

export function advanceOrder(
  snapshot: KitchenSnapshot,
  orderId: string,
  now = new Date(),
): { snapshot: KitchenSnapshot; order: KitchenOrder } {
  const existing = snapshot.orders.find((order) => order.id === orderId);
  if (!existing) throw new Error(`Unknown order: ${orderId}`);

  const nextStatus = transitionMap[existing.status];
  if (!nextStatus) throw new Error(`Order ${existing.number} is already completed`);
  if (!canAdvanceOrder(snapshot, orderId)) {
    throw new Error(`Order ${existing.number} has stations at different steps; advance each station ticket first`);
  }

  const updatedAt = now.toISOString();
  const tickets = (snapshot.tickets ?? []).map((ticket) => {
    if (ticket.orderId !== orderId || ticket.status === nextStatus) return ticket;
    if (transitionMap[ticket.status] !== nextStatus) {
      throw new Error(`Order ${existing.number} has stations at different steps; advance each station ticket first`);
    }
    return {
      ...ticket,
      status: nextStatus,
      updatedAt,
      version: ticket.version + 1,
      origin: 'local' as const,
    };
  });

  const updated: KitchenOrder = {
    ...existing,
    status: nextStatus,
    updatedAt,
    version: existing.version + 1,
    origin: 'local',
  };

  return {
    order: updated,
    snapshot: {
      ...snapshot,
      tickets: snapshot.tickets ? tickets : undefined,
      orders: snapshot.orders.map((order) => (order.id === orderId ? updated : order)),
    },
  };
}

function deriveOrderStatus(tickets: KitchenTicket[]): TicketStatus {
  if (tickets.length === 0) return 'queued';
  const active = tickets.filter((ticket) => ticket.status !== 'cancelled');
  if (active.length === 0) return 'cancelled';
  if (active.every((ticket) => ticket.status === 'completed')) return 'completed';
  if (active.every((ticket) => ticket.status === 'ready' || ticket.status === 'completed')) return 'ready';
  if (active.some((ticket) => ['preparing', 'ready', 'completed'].includes(ticket.status))) return 'preparing';
  if (active.some((ticket) => ticket.status === 'fired')) return 'fired';
  return 'queued';
}

export function advanceTicket(
  snapshot: KitchenSnapshot,
  ticketId: string,
  now = new Date(),
): { snapshot: KitchenSnapshot; ticket: KitchenTicket; order: KitchenOrder } {
  const tickets = snapshot.tickets ?? [];
  const existing = tickets.find((ticket) => ticket.id === ticketId);
  if (!existing) throw new Error(`Unknown kitchen ticket: ${ticketId}`);
  const order = snapshot.orders.find((candidate) => candidate.id === existing.orderId);
  if (!order) throw new Error(`Unknown order for kitchen ticket: ${ticketId}`);

  const nextStatus = transitionMap[existing.status];
  if (!nextStatus) throw new Error(`Kitchen ticket ${ticketId} is already completed`);
  const updatedAt = now.toISOString();
  const ticket: KitchenTicket = {
    ...existing,
    status: nextStatus,
    updatedAt,
    version: existing.version + 1,
    origin: 'local',
  };
  const nextTickets = tickets.map((candidate) => (candidate.id === ticketId ? ticket : candidate));
  const aggregateTickets = nextTickets.filter((candidate) => candidate.orderId === order.id);
  const derivedStatus = deriveOrderStatus(aggregateTickets);
  const updatedOrder: KitchenOrder = derivedStatus === order.status
    ? order
    : {
        ...order,
        status: derivedStatus,
        updatedAt,
        version: order.version + 1,
        origin: 'local',
      };

  return {
    ticket,
    order: updatedOrder,
    snapshot: {
      ...snapshot,
      tickets: nextTickets,
      orders: snapshot.orders.map((candidate) =>
        candidate.id === updatedOrder.id ? updatedOrder : candidate),
    },
  };
}

export function getOrderSubtotal(order: Pick<KitchenOrder, 'lines'>): number {
  return order.lines.reduce((total, line) => {
    const price = catalogById.get(line.menuItemId)?.price.minorUnits ?? 0;
    return total + price * line.quantity;
  }, 0);
}

export function getOrderTax(order: Pick<KitchenOrder, 'lines'>): number {
  return Math.round(getOrderSubtotal(order) * 0.05);
}

export function createOutboxEvent(
  snapshot: KitchenSnapshot,
  eventType:
    | 'com.feastcloud.order.created.v1'
    | 'com.feastcloud.order.status-changed.v1',
  order: KitchenOrder,
  payload: Record<string, unknown>,
): OutboxEvent {
  const id = createUuidV7(new Date(order.updatedAt).getTime());
  return {
    id,
    tenantId: snapshot.organizationId,
    outletId: snapshot.outletId,
    deviceId: getDeviceId(),
    actorId: 'demo-user',
    occurredAt: order.updatedAt,
    source: 'feastcloud.web',
    sourceId: order.id,
    schemaVersion: '1.0',
    idempotencyKey: id,
    payload: {
      eventType,
      aggregateType: 'order',
      aggregateId: order.id,
      ...payload,
    },
    attempts: 0,
  };
}

export function createTicketOutboxEvent(
  snapshot: KitchenSnapshot,
  ticket: KitchenTicket,
  order: KitchenOrder,
): OutboxEvent {
  const id = createUuidV7(new Date(ticket.updatedAt).getTime());
  return {
    id,
    tenantId: snapshot.organizationId,
    outletId: snapshot.outletId,
    deviceId: getDeviceId(),
    actorId: 'demo-user',
    occurredAt: ticket.updatedAt,
    source: 'feastcloud.web',
    sourceId: ticket.id,
    schemaVersion: '1.0',
    idempotencyKey: id,
    payload: {
      eventType: 'com.feastcloud.kitchen-ticket.status-changed.v1',
      aggregateType: 'kitchenTicket',
      aggregateId: ticket.id,
      ticketId: ticket.id,
      orderId: order.id,
      toStatus: ticket.status,
      expectedVersion: ticket.version - 1,
    },
    attempts: 0,
  };
}

export function getDeviceId(): string {
  const key = 'feastcloud.device-id';
  try {
    const existing = globalThis.localStorage?.getItem(key);
    if (existing) return existing;
    const id = createDeviceId();
    globalThis.localStorage?.setItem(key, id);
    return id;
  } catch {
    return 'device_ephemeral';
  }
}
