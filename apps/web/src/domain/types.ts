export type Locale = string;

export const roles = ['cashier', 'chef', 'manager'] as const;
export type Role = (typeof roles)[number];

export type View = 'overview' | 'orders' | 'kds' | 'production' | 'inventory' | 'planning' | 'daily' | 'commerce' | 'growth' | 'operations' | 'organization' | 'platform' | 'menu';
export type OrderType = 'dineIn' | 'takeaway' | 'delivery' | 'roomService';
export type TicketStatus = 'queued' | 'fired' | 'preparing' | 'ready' | 'completed' | 'cancelled';
/**
 * Outlet-defined station identifier. The edge contract deliberately permits
 * custom station IDs; translated labels for FeastCloud's defaults are a UI
 * convenience, not a closed domain enum.
 */
export type StationId = string;

export type LocalizedText = Record<string, string>;

export interface Money {
  minorUnits: number;
  currency: 'INR';
}

export interface MenuItem {
  id: string;
  name: LocalizedText;
  description: LocalizedText;
  category: 'mains' | 'snacks' | 'drinks';
  station: StationId;
  price: Money;
  prepMinutes: number;
  vegetarian: boolean;
  available: boolean;
  accent: string;
  glyph: string;
}

export interface OrderLine {
  id: string;
  menuItemId: string;
  quantity: number;
  note?: string;
  name?: string;
  stationId?: StationId;
}

export interface AggregatorOrderSource {
  provider: string;
  brandName: string;
  externalOrderId: string;
  externalOutletId?: string;
}

export interface KitchenOrder {
  id: string;
  number: number;
  type: OrderType;
  guestName?: string;
  tableLabel?: string;
  note?: string;
  aggregator?: AggregatorOrderSource;
  lines: OrderLine[];
  status: TicketStatus;
  createdAt: string;
  updatedAt: string;
  dueAt: string;
  version: number;
  origin?: 'demo' | 'local' | 'edge';
}

export interface KitchenTicket {
  id: string;
  orderId: string;
  stationId: StationId;
  lineIds: string[];
  status: TicketStatus;
  priority: number;
  targetAt?: string;
  createdAt: string;
  updatedAt: string;
  version: number;
  origin?: 'demo' | 'local' | 'edge';
}

export interface KitchenSnapshot {
  schemaVersion: 1;
  organizationId: string;
  outletId: string;
  /** Present after this installation has discovered and bound to an outlet edge. */
  edgeId?: string;
  orders: KitchenOrder[];
  /** Station projections are optional only for snapshots written before ticket-level KDS support. */
  tickets?: KitchenTicket[];
  nextOrderNumber: number;
}

export interface UserPreferences {
  locale: Locale;
  role: Role;
  view: View;
  compactMode: boolean;
}

export interface OutboxEvent {
  id: string;
  tenantId: string;
  outletId: string;
  deviceId: string;
  actorId: string;
  occurredAt: string;
  source: string;
  sourceId?: string;
  schemaVersion: '1.0';
  idempotencyKey: string;
  payload: Record<string, unknown>;
  /** Local persistence metadata; removed before transmitting the mutation envelope. */
  attempts: number;
  /** Durable device-local ordering; allocated atomically when the event is committed. */
  localSequence?: number;
  /** Permanent failures remain available for operator reconciliation. */
  disposition?: 'pending' | 'quarantined';
  lastError?: string;
}

export interface SyncState {
  pending: number;
  quarantined: number;
  syncing: boolean;
  lastSyncedAt?: string;
  error?: string;
}
