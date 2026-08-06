import type {
  KitchenOrder,
  KitchenSnapshot,
  KitchenTicket,
  OrderLine,
  OrderType,
  StationId,
  TicketStatus,
} from './types';
import { edgeAuthorizationHeaders } from '../security/edgeSession';

const orderTypes = new Set<OrderType>(['dineIn', 'takeaway', 'delivery', 'roomService']);
const statusMap: Record<string, TicketStatus> = {
  received: 'queued',
  accepted: 'fired',
  preparing: 'preparing',
  ready: 'ready',
  completed: 'completed',
  cancelled: 'cancelled',
};
const activeStatuses = new Set<TicketStatus>(['queued', 'fired', 'preparing', 'ready']);
const edgeOrderStatuses = ['received', 'accepted', 'preparing', 'ready', 'completed', 'cancelled'] as const;
const edgeTicketStatuses = ['queued', 'fired', 'preparing', 'ready', 'completed', 'cancelled'] as const;

export interface EdgeDiscovery {
  edgeId: string;
  tenantId: string;
  outletId: string;
}

export function edgeApiBase(configured: string | undefined): string | undefined {
  const value = configured?.trim();
  if (!value) return undefined;
  const root = value.replace(/\/$/, '');
  return root.endsWith('/api/v1') ? root : `${root}/api/v1`;
}

function requiredString(record: Record<string, unknown>, field: string): string {
  const value = record[field];
  if (typeof value !== 'string' || value.length === 0) throw new Error(`edge order ${field} is invalid`);
  return value;
}

function optionalString(value: unknown): string | undefined {
  return typeof value === 'string' && value.length > 0 ? value : undefined;
}

export async function fetchEdgeDiscovery(apiBase: string): Promise<EdgeDiscovery> {
  const response = await fetch(apiBase, {
    headers: { Accept: 'application/json', ...edgeAuthorizationHeaders() },
    cache: 'no-store',
  });
  if (!response.ok) throw new Error(`Edge discovery returned ${response.status}`);
  const body = (await response.json()) as { data?: unknown };
  if (!body.data || typeof body.data !== 'object') throw new Error('Edge discovery response is invalid');
  const data = body.data as Record<string, unknown>;
  return {
    edgeId: requiredString(data, 'edgeId'),
    tenantId: requiredString(data, 'tenantId'),
    outletId: requiredString(data, 'outletId'),
  };
}

function mapLine(value: unknown): OrderLine {
  if (!value || typeof value !== 'object') throw new Error('edge order line is invalid');
  const line = value as Record<string, unknown>;
  const id = requiredString(line, 'id');
  const quantity = line.quantity;
  if (!Number.isInteger(quantity) || Number(quantity) <= 0) throw new Error('edge order line quantity is invalid');
  const station = optionalString(line.stationId);
  return {
    id,
    menuItemId: optionalString(line.menuItemId) ?? `edge:${id}`,
    quantity: Number(quantity),
    note: optionalString(line.preparationNote),
    name: optionalString(line.name),
    stationId: station ?? 'hot',
  };
}

function mapAggregator(value: unknown): KitchenOrder['aggregator'] {
  if (!value || typeof value !== 'object') return undefined;
  const source = value as Record<string, unknown>;
  const provider = optionalString(source.provider);
  const brandName = optionalString(source.brandName);
  const externalOrderId = optionalString(source.externalOrderId);
  if (!provider || !brandName || !externalOrderId) return undefined;
  return { provider, brandName, externalOrderId, externalOutletId: optionalString(source.externalOutletId) };
}

export function mapEdgeOrder(value: unknown): KitchenOrder {
  if (!value || typeof value !== 'object') throw new Error('edge order is invalid');
  const order = value as Record<string, unknown>;
  const type = requiredString(order, 'type') as OrderType;
  const edgeStatus = requiredString(order, 'status');
  const number = order.number;
  const version = order.version;
  if (!orderTypes.has(type) || !statusMap[edgeStatus]) throw new Error('edge order type or status is invalid');
  if (!Number.isInteger(number) || Number(number) < 1 || !Number.isInteger(version) || Number(version) < 1) {
    throw new Error('edge order number or version is invalid');
  }
  if (!Array.isArray(order.lines) || order.lines.length === 0) throw new Error('edge order lines are invalid');

  const placedAt = requiredString(order, 'placedAt');
  const createdAt = requiredString(order, 'createdAt');
  const updatedAt = requiredString(order, 'updatedAt');
  const targetAt = optionalString(order.targetAt);
  const dueAt = targetAt ?? new Date(new Date(placedAt).getTime() + 15 * 60_000).toISOString();
  return {
    id: requiredString(order, 'id'),
    number: Number(number),
    type,
    guestName: optionalString(order.guestName),
    tableLabel: optionalString(order.tableLabel),
    note: optionalString(order.note),
    aggregator: mapAggregator(order.aggregator),
    lines: order.lines.map(mapLine),
    status: statusMap[edgeStatus],
    createdAt,
    updatedAt,
    dueAt,
    version: Number(version),
    origin: 'edge',
  };
}

export function mapEdgeTicket(value: unknown): KitchenTicket {
  if (!value || typeof value !== 'object') throw new Error('edge kitchen ticket is invalid');
  const ticket = value as Record<string, unknown>;
  const status = requiredString(ticket, 'status') as TicketStatus;
  const stationId: StationId = requiredString(ticket, 'stationId');
  const version = ticket.version;
  const priority = ticket.priority;
  if (!activeStatuses.has(status) && status !== 'completed' && status !== 'cancelled') {
    throw new Error('edge kitchen ticket status is invalid');
  }
  if (!Number.isInteger(version) || Number(version) < 1 || !Number.isInteger(priority)) {
    throw new Error('edge kitchen ticket version or priority is invalid');
  }
  if (!Array.isArray(ticket.lineIds) || ticket.lineIds.some((id) => typeof id !== 'string' || id.length === 0)) {
    throw new Error('edge kitchen ticket lineIds are invalid');
  }
  return {
    id: requiredString(ticket, 'id'),
    orderId: requiredString(ticket, 'orderId'),
    stationId,
    lineIds: [...ticket.lineIds] as string[],
    status,
    priority: Number(priority),
    targetAt: optionalString(ticket.targetAt),
    createdAt: requiredString(ticket, 'createdAt'),
    updatedAt: requiredString(ticket, 'updatedAt'),
    version: Number(version),
    origin: 'edge',
  };
}

async function fetchCollection(apiBase: string, resource: string, status: string): Promise<unknown[]> {
  const response = await fetch(`${apiBase}/${resource}?status=${encodeURIComponent(status)}&limit=500`, {
    headers: { Accept: 'application/json', ...edgeAuthorizationHeaders() },
    cache: 'no-store',
  });
  if (!response.ok) throw new Error(`Edge ${resource} projection returned ${response.status}`);
  const body = (await response.json()) as { data?: unknown };
  if (!Array.isArray(body.data)) throw new Error(`Edge ${resource} projection response is invalid`);
  return body.data;
}

interface VersionedProjection {
  id: string;
  version: number;
  updatedAt: string;
}

function compareProjectionTime(left: string, right: string): number {
  const leftMilliseconds = Date.parse(left);
  const rightMilliseconds = Date.parse(right);
  if (Number.isFinite(leftMilliseconds) && Number.isFinite(rightMilliseconds)) {
    if (leftMilliseconds !== rightMilliseconds) return leftMilliseconds - rightMilliseconds;
    // Date.parse truncates sub-millisecond evidence. Edge timestamps are UTC
    // RFC3339Nano, so normalize their fractional seconds before the tie-break.
    const normalized = (value: string) => value.replace(
      /(?:\.(\d{1,9}))?Z$/,
      (_match, fraction: string | undefined) => `.${(fraction ?? '').padEnd(9, '0')}Z`,
    );
    return normalized(left).localeCompare(normalized(right));
  }
  return left.localeCompare(right);
}

export function deduplicateProjections<T extends VersionedProjection>(entities: T[]): T[] {
  const byID = new Map<string, T>();
  for (const entity of entities) {
    const existing = byID.get(entity.id);
    if (
      !existing
      || entity.version > existing.version
      || (
        entity.version === existing.version
        && compareProjectionTime(entity.updatedAt, existing.updatedAt) > 0
      )
    ) {
      byID.set(entity.id, entity);
    }
  }
  return [...byID.values()];
}

export async function fetchEdgeOrders(apiBase: string): Promise<KitchenOrder[]> {
  const collections = await Promise.all(
    edgeOrderStatuses.map((status) => fetchCollection(apiBase, 'orders', status)),
  );
  return deduplicateProjections(collections.flat().map(mapEdgeOrder));
}

export async function fetchEdgeTickets(apiBase: string): Promise<KitchenTicket[]> {
  const collections = await Promise.all(
    edgeTicketStatuses.map((status) => fetchCollection(apiBase, 'kitchen-tickets', status)),
  );
  return deduplicateProjections(collections.flat().map(mapEdgeTicket));
}

export function mergeEdgeOrders(
  current: KitchenSnapshot,
  remote: KitchenOrder[],
  pendingOrderIds: ReadonlySet<string>,
): KitchenSnapshot {
  const currentOrders = deduplicateProjections(current.orders);
  const remoteOrders = deduplicateProjections(remote);
  const currentByID = new Map(currentOrders.map((order) => [order.id, order]));
  const merged = remoteOrders.map((order) => {
    const local = currentByID.get(order.id);
    return local && pendingOrderIds.has(order.id) && local.origin === 'local' && local.version > order.version
      ? local
      : order;
  });
  const remoteIDs = new Set(remoteOrders.map((order) => order.id));
  for (const order of currentOrders) {
    if (
      !remoteIDs.has(order.id) &&
      (pendingOrderIds.has(order.id) || (order.origin === 'edge' && activeStatuses.has(order.status)))
    ) {
      merged.push(order);
    }
  }
  merged.sort((left, right) => right.createdAt.localeCompare(left.createdAt));
  return {
    ...current,
    orders: merged,
    nextOrderNumber: Math.max(0, ...merged.map((order) => order.number)) + 1,
  };
}

export function mergeEdgeTickets(
  current: KitchenSnapshot,
  remote: KitchenTicket[],
  pendingTicketIds: ReadonlySet<string>,
): KitchenTicket[] {
  const currentTickets = deduplicateProjections(current.tickets ?? []);
  const remoteTickets = deduplicateProjections(remote);
  const currentByID = new Map(currentTickets.map((ticket) => [ticket.id, ticket]));
  const merged = remoteTickets.map((ticket) => {
    const local = currentByID.get(ticket.id);
    return local && pendingTicketIds.has(ticket.id) && local.origin === 'local' && local.version > ticket.version
      ? local
      : ticket;
  });
  const remoteIDs = new Set(remoteTickets.map((ticket) => ticket.id));
  for (const ticket of currentTickets) {
    if (
      !remoteIDs.has(ticket.id) &&
      (pendingTicketIds.has(ticket.id) || (ticket.origin === 'edge' && activeStatuses.has(ticket.status)))
    ) {
      merged.push(ticket);
    }
  }
  return merged.sort((left, right) =>
    right.priority - left.priority || left.createdAt.localeCompare(right.createdAt));
}

export function mergeEdgeProjection(
  current: KitchenSnapshot,
  remoteOrders: KitchenOrder[],
  remoteTickets: KitchenTicket[],
  pendingOrderIds: ReadonlySet<string>,
  pendingTicketIds: ReadonlySet<string>,
): KitchenSnapshot {
  const orders = mergeEdgeOrders(current, remoteOrders, pendingOrderIds);
  return {
    ...orders,
    tickets: mergeEdgeTickets(current, remoteTickets, pendingTicketIds),
  };
}
