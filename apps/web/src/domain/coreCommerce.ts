import { createUuidV7, getDeviceId } from './kitchen';
import { coreApiBase } from './coreInventory';
import { fileSHA256 } from './corePlanning';
import type { MenuImportPreview } from './menuImport';

export interface Availability { menuItemId: string; menuItemName: string; available: boolean; reason: string; version: number }
export interface Sellability { menuItemId: string; available: boolean; reasonCode?: string; reason?: string; stockReady: boolean; capacityReady: boolean; activeTickets: number; capacityLimit: number }
export interface DiningTable { id: string; label: string; section: string; capacity: number; status: string; version: number }
export interface DiningSession { id: string; tableId: string; tableLabel: string; status: string; guestCount: number; version: number }
export interface CashShift { id: string; registerLabel: string; status: string; openingFloatMinor: number; expectedCashMinor: number; closingCountMinor?: number; varianceMinor?: number; version: number }
export interface CoreOrder { id: string; status: string; total: { minorUnits: number; currency: string }; placedAt: string; lines: Array<{ name: string; quantity: number }> }
export interface CanonicalConnectorLine { id: string; menuItemId?: string; name: string; quantity: number; unitPriceMinor: number; preparationNote?: string }

export interface MenuStudioCategory { id: string; name: string; sortOrder: number; active: boolean }
export interface MenuPublication { id: string; channelId?: string; status: 'scheduled' | 'live' | 'paused'; effectiveFrom: string; effectiveTo?: string }
export interface MenuStudioItem { menuItemId: string; categoryId?: string; displayName: string; description?: string; sortOrder: number; active: boolean; modifierGroupIds: string[]; priceId: string; priceMinor: number; currency: string }
export interface MenuModifierOption { id: string; name: string; priceDeltaMinor: number; active: boolean; sortOrder: number }
export interface MenuModifierGroup { id: string; name: string; selectionMin: number; selectionMax: number; required: boolean; sortOrder: number; options: MenuModifierOption[] }
export interface MenuStudioVersion { id: string; versionNumber: number; status: 'draft' | 'published'; effectiveFrom: string; categories: MenuStudioCategory[]; items: MenuStudioItem[]; modifiers: MenuModifierGroup[]; publications: MenuPublication[] }
export interface MenuStudio { id: string; name: string; status: string; currentVersionId?: string; version: number; current?: MenuStudioVersion }
export interface MenuItemReference { id: string; outletId: string; recipeId?: string; name: string; code: string; priceMinor: number; currency: string; stationId?: string; active: boolean }
export interface MenuImportDraft { id: string; outletId: string; name: string; itemFileName: string; addonFileName?: string; sourceSha256: string; status: 'staged' | 'mapping' | 'applied' | 'rejected'; itemCount: number; categoryCount: number; addonGroupCount: number; variationCount: number; draft: MenuImportPreview; importedAt: string }
export interface KitchenPrintJob { id: string; ticketId: string; printerRoute: string; status: string; attempts: number; createdAt: string; acknowledgedAt?: string }
export interface PickupToken { id: string; orderId: string; token: string; status: string; issuedAt: string; collectedAt?: string; version: number }
export interface POSCheckoutLine { menuItemId: string; quantity: number; modifierOptionIds?: string[]; preparationNote?: string }
export interface POSCheckoutResult { order: CoreOrder; tickets: Array<{ id: string; stationId: string; status: string }>; printJobs: KitchenPrintJob[]; pickupToken?: PickupToken; receipt?: { receiptNumber: string; totalMinor: number }; tenders: Array<{ tenderType: string; amountMinor: number }> }
export interface ConnectorInboxOrder { id: string; connectorId: string; externalOrderId: string; payload: Record<string, unknown>; status: 'received' | 'accepted' | 'rejected' | 'duplicate' | 'needs_review'; normalizedOrderId?: string; receivedAt: string; resolvedAt?: string; errorCode?: string }
export interface ConnectorExternalOutlet { externalOutletId: string; brandName: string; active: boolean }
export interface ConnectorInstallation { id: string; provider: string; manifestVersion: string; capabilities: string[]; configuration?: { externalOutlets?: ConnectorExternalOutlet[] }; status: 'draft' | 'healthy' | 'degraded' | 'disabled'; lastHealthAt?: string }
export interface StockTransferLine { id: string; ingredientId: string; quantityBase: number; dispatchedQuantityBase?: number; receivedQuantityBase?: number }
export interface StockTransfer { id: string; sourceOutletId: string; destinationOutletId: string; status: 'requested' | 'approved' | 'dispatched' | 'received' | 'cancelled'; requestedBy: string; notes?: string; requestedAt: string; dispatchedAt?: string; receivedAt?: string; lines: StockTransferLine[]; version: number }
export interface OutletReference { id: string; name: string; active: boolean }
export interface IngredientReference { id: string; name: string; active: boolean }
export interface ReplenishmentRule { outletId: string; ingredientId: string; sourceOutletId: string; reorderPointBase: number; targetLevelBase: number; active: boolean; version: number; updatedAt: string }
export interface ReplenishmentSuggestion { outletId: string; ingredientId: string; ingredientName: string; unitSymbol: string; sourceOutletId: string; onHandBase: number; reorderPointBase: number; targetLevelBase: number; sourceAvailableBase: number; suggestedQuantityBase: number; status: 'ready' | 'source_short' | 'source_empty' }

export const MENU_IMPORT_UPDATED_EVENT = 'feastcloud:menu-import-updated';

function auth(tenantId: string): { actorId: string; headers: Record<string, string> } {
  const token = sessionStorage.getItem('feastcloud.oidc-access-token');
  return token ? { actorId: 'manager-dashboard', headers: { Authorization: `Bearer ${token}` } } : { actorId: 'manager-dashboard', headers: { 'X-FeastCloud-Tenant-ID': tenantId, 'X-FeastCloud-Actor-ID': 'manager-dashboard' } };
}

async function mutate<T>(base: string, path: string, tenantId: string, outletId: string, payload: Record<string, unknown>): Promise<T> {
  const id = createUuidV7();
  const identity = auth(tenantId);
  const body = { id, tenantId, outletId, deviceId: getDeviceId(), actorId: identity.actorId, occurredAt: new Date().toISOString(), source: 'feastcloud.web', sourceId: id, schemaVersion: '1.0', idempotencyKey: id, payload };
  const response = await fetch(base + path, { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': id, ...identity.headers }, body: JSON.stringify(body) });
  if (!response.ok) throw new Error(`${path}: ${response.status}`);
  return (await response.json() as { data: T }).data;
}

async function list<T>(base: string, path: string, tenantId: string, outletId: string, limit = 100): Promise<T[]> {
  const response = await fetch(`${base}${path}?outletId=${encodeURIComponent(outletId)}&limit=${limit}`, { headers: { Accept: 'application/json', ...auth(tenantId).headers }, cache: 'no-store' });
  if (!response.ok) throw new Error(`${path}: ${response.status}`);
  return (await response.json() as { data: T[] }).data;
}

async function tenantList<T>(base: string, path: string, tenantId: string): Promise<T[]> {
  const response = await fetch(`${base}${path}`, { headers: { Accept: 'application/json', ...auth(tenantId).headers }, cache: 'no-store' });
  if (!response.ok) throw new Error(`${path}: ${response.status}`);
  const body = await response.json() as { data: T[] };
  return body.data;
}

export const commerceApiBase = coreApiBase;
export const fetchAvailability = (base: string, tenant: string, outlet: string) => list<Availability>(base, '/menu-availability', tenant, outlet);
export const fetchSellability = (base: string, tenant: string, outlet: string) => list<Sellability>(base, '/menu-sellability', tenant, outlet);
export const setAvailability = (base: string, tenant: string, outlet: string, value: Availability) => mutate<Availability>(base, '/menu-availability', tenant, outlet, { menuItemId: value.menuItemId, available: !value.available, reason: value.available ? 'Manager 86 override' : '' });
export const fetchTables = (base: string, tenant: string, outlet: string) => list<DiningTable>(base, '/dining-tables', tenant, outlet);
export const createTable = (base: string, tenant: string, outlet: string, label: string) => mutate<DiningTable>(base, '/dining-tables', tenant, outlet, { id: createUuidV7(), outletId: outlet, label, section: 'Main', capacity: 4 });
export const transitionTable = (base: string, tenant: string, outlet: string, value: DiningTable, status: 'available' | 'disabled') => mutate<DiningTable>(base, `/dining-tables/${value.id}/transitions`, tenant, outlet, { status, expectedVersion: value.version });
export const fetchSessions = (base: string, tenant: string, outlet: string) => list<DiningSession>(base, '/dining-sessions', tenant, outlet);
export const openSession = (base: string, tenant: string, outlet: string, tableId: string, guests: number) => mutate<DiningSession>(base, '/dining-sessions', tenant, outlet, { id: createUuidV7(), outletId: outlet, tableId, guestCount: guests });
export const closeSession = (base: string, tenant: string, outlet: string, value: DiningSession) => mutate<DiningSession>(base, `/dining-sessions/${value.id}/close`, tenant, outlet, { expectedVersion: value.version });
export const fetchCashShifts = (base: string, tenant: string, outlet: string) => list<CashShift>(base, '/cash-shifts', tenant, outlet);
export const openCash = (base: string, tenant: string, outlet: string, label: string, openingFloatMinor: number) => mutate<CashShift>(base, '/cash-shifts', tenant, outlet, { id: createUuidV7(), outletId: outlet, registerLabel: label, openingFloatMinor });
export const closeCash = (base: string, tenant: string, outlet: string, value: CashShift, closingCountMinor: number) => mutate<CashShift>(base, `/cash-shifts/${value.id}/close`, tenant, outlet, { expectedVersion: value.version, closingCountMinor });
export const fetchOrders = (base: string, tenant: string, outlet: string) => list<CoreOrder>(base, '/orders', tenant, outlet);
export const createConnectorCanonicalOrder = (base: string, tenant: string, outlet: string, id: string, externalRef: string, placedAt: string, lines: CanonicalConnectorLine[]) => {
  const subtotal = lines.reduce((sum, line) => sum + line.unitPriceMinor * line.quantity, 0);
  const money = (minorUnits: number) => ({ minorUnits, currency: 'INR' });
  return mutate<CoreOrder>(base, '/orders', tenant, outlet, {
    id, outletId: outlet, externalRef, type: 'delivery', status: 'received', placedAt,
    lines: lines.map((line) => ({
      id: line.id, menuItemId: line.menuItemId ?? '', name: line.name, quantity: line.quantity,
      unitPrice: money(line.unitPriceMinor), lineTotal: money(line.unitPriceMinor * line.quantity),
      preparationNote: line.preparationNote ?? '',
    })),
    subtotal: money(subtotal), discountTotal: money(0), taxTotal: money(0),
    serviceCharge: money(0), total: money(subtotal),
  });
};
export const captureTender = (base: string, tenant: string, outlet: string, order: CoreOrder, tenderType: string, cashShiftId: string, amountMinor: number) => mutate(base, '/tenders', tenant, outlet, { id: createUuidV7(), outletId: outlet, orderId: order.id, cashShiftId: tenderType === 'cash' ? cashShiftId : '', tenderType, amountMinor, providerReference: '', receiptId: createUuidV7(), receiptNumber: `R-${Date.now()}` });
export const settleToday = (base: string, tenant: string, outlet: string) => mutate(base, '/tender-settlements', tenant, outlet, { businessDate: new Date().toISOString().slice(0, 10) });

export const fetchMenuStudios = (base: string, tenant: string, outlet: string) => list<MenuStudio>(base, '/menu-studios', tenant, outlet);
export const fetchMenuItems = (base: string, tenant: string, outlet: string) => list<MenuItemReference>(base, '/menu-items', tenant, outlet);
export const fetchMenuImportDrafts = (base: string, tenant: string, outlet: string) => list<MenuImportDraft>(base, '/menu-imports', tenant, outlet, 5);
export async function stageMenuImport(base: string, tenant: string, outlet: string, itemFileName: string, addonFileName: string, preview: MenuImportPreview): Promise<MenuImportDraft> {
  const sourceSha256 = await fileSHA256(JSON.stringify(preview));
  const existing = (await fetchMenuImportDrafts(base, tenant, outlet)).find((draft) => draft.sourceSha256 === sourceSha256);
  if (existing) {
    window.dispatchEvent(new CustomEvent(MENU_IMPORT_UPDATED_EVENT, { detail: { tenant, outlet, importId: existing.id } }));
    return existing;
  }
  try {
    const imported = await mutate<MenuImportDraft>(base, '/menu-imports', tenant, outlet, {
      id: createUuidV7(), outletId: outlet, name: itemFileName.replace(/\.[^.]+$/, '') || 'Imported menu', itemFileName, addonFileName, sourceSha256,
      itemCount: preview.items.length, categoryCount: preview.categories.length, addonGroupCount: preview.addonGroups.length, variationCount: preview.variationCount, draft: preview,
    });
    window.dispatchEvent(new CustomEvent(MENU_IMPORT_UPDATED_EVENT, { detail: { tenant, outlet, importId: imported.id } }));
    return imported;
  } catch (error) {
    // A concurrent upload of the same canonical menu is a successful no-op.
    const duplicate = (await fetchMenuImportDrafts(base, tenant, outlet)).find((draft) => draft.sourceSha256 === sourceSha256);
    if (duplicate) {
      window.dispatchEvent(new CustomEvent(MENU_IMPORT_UPDATED_EVENT, { detail: { tenant, outlet, importId: duplicate.id } }));
      return duplicate;
    }
    throw error;
  }
}
export const createMenuItem = (base: string, tenant: string, outlet: string, input: { recipeId?: string; name: string; code: string; priceMinor: number; currency: string; stationId?: string }) => mutate<MenuItemReference>(base, '/menu-items', tenant, outlet, { id: createUuidV7(), outletId: outlet, ...input });
const livePublication = (): MenuPublication => ({ id: createUuidV7(), status: 'live', effectiveFrom: new Date().toISOString() });
export const createMenuStudio = (base: string, tenant: string, outlet: string, name: string, category: MenuStudioCategory, item: MenuStudioItem) => {
  const studioId = createUuidV7();
  const version: MenuStudioVersion = { id: createUuidV7(), versionNumber: 1, status: 'published', effectiveFrom: new Date().toISOString(), categories: [category], modifiers: [], items: [item], publications: [livePublication()] };
  return mutate<MenuStudio>(base, '/menu-studios', tenant, outlet, { id: studioId, outletId: outlet, name, version });
};
export const publishMenuStudioVersion = (base: string, tenant: string, outlet: string, studio: MenuStudio, update: { categories?: MenuStudioCategory[]; modifiers?: MenuModifierGroup[]; items?: MenuStudioItem[] }) => {
  const current = studio.current;
  if (!current) throw new Error('menu studio has no current version');
  const version: MenuStudioVersion = { id: createUuidV7(), versionNumber: current.versionNumber + 1, status: 'published', effectiveFrom: new Date().toISOString(), categories: update.categories ?? current.categories, modifiers: update.modifiers ?? current.modifiers, items: update.items ?? current.items, publications: [livePublication()] };
  return mutate<MenuStudio>(base, `/menu-studios/${studio.id}/versions`, tenant, outlet, { expectedVersion: studio.version, version });
};
export const fetchPrintJobs = (base: string, tenant: string, outlet: string) => list<KitchenPrintJob>(base, '/kitchen-print-jobs', tenant, outlet);
export const fetchPickupTokens = (base: string, tenant: string, outlet: string) => list<PickupToken>(base, '/pickup-tokens', tenant, outlet);
export const fetchConnectorInbox = (base: string, tenant: string, outlet: string) => list<ConnectorInboxOrder>(base, '/connector-order-inbox', tenant, outlet);
export const fetchConnectorInstallations = (base: string, tenant: string, outlet: string) => list<ConnectorInstallation>(base, '/connector-installations', tenant, outlet);
export const ingestConnectorOrder = (base: string, tenant: string, outlet: string, connectorId: string, externalOrderId: string, payload: Record<string, unknown>) => mutate<ConnectorInboxOrder>(base, '/connector-order-inbox', tenant, outlet, { id: createUuidV7(), outletId: outlet, connectorId, externalOrderId, payload });
export async function streamConnectorInbox(base: string, tenant: string, outlet: string, onOrders: (orders: ConnectorInboxOrder[]) => void, signal: AbortSignal): Promise<void> {
  const response = await fetch(`${base}/connector-order-inbox/stream?outletId=${encodeURIComponent(outlet)}`, {
    headers: { Accept: 'text/event-stream', ...auth(tenant).headers },
    cache: 'no-store', signal,
  });
  if (!response.ok || !response.body) throw new Error(`connector stream: ${response.status}`);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  while (!signal.aborted) {
    const { value, done } = await reader.read();
    if (done) return;
    buffer += decoder.decode(value, { stream: true });
    let boundary = buffer.indexOf('\n\n');
    while (boundary >= 0) {
      const event = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      const data = event.split('\n').filter((line) => line.startsWith('data:')).map((line) => line.slice(5).trim()).join('\n');
      if (data) onOrders(JSON.parse(data) as ConnectorInboxOrder[]);
      boundary = buffer.indexOf('\n\n');
    }
  }
}
export const decideConnectorInbox = (base: string, tenant: string, outlet: string, inboxId: string, decision: 'accepted' | 'rejected' | 'duplicate' | 'needs_review', reason = '', normalizedOrderId = '') => mutate<ConnectorInboxOrder>(base, `/connector-order-inbox/${inboxId}/decisions`, tenant, outlet, { id: createUuidV7(), decision, reason, normalizedOrderId });
export const checkoutPOS = (base: string, tenant: string, outlet: string, menuVersionId: string, lines: POSCheckoutLine[], totalMinor: number, tenderType: 'cash' | 'upi' | 'card_terminal', cashShiftId = '') => {
  const checkoutId = createUuidV7();
  const orderId = createUuidV7();
  const token = `F${checkoutId.replaceAll('-', '').slice(0, 7).toUpperCase()}`;
  return mutate<POSCheckoutResult>(base, '/pos-checkouts', tenant, outlet, { id: checkoutId, outletId: outlet, menuVersionId, orderId, orderType: 'takeaway', lines: lines.map((line) => ({ id: createUuidV7(), ...line })), tenders: [{ id: createUuidV7(), tenderType, cashShiftId: tenderType === 'cash' ? cashShiftId : '', amountMinor: totalMinor }], receiptId: createUuidV7(), receiptNumber: `FC-${Date.now()}`, pickupTokenId: createUuidV7(), pickupToken: token, printerRoute: 'default-kot', placedAt: new Date().toISOString() });
};
export const actOnPrintJob = (base: string, tenant: string, outlet: string, jobId: string, action: 'acknowledged' | 'failed' | 'requeued' | 'reprinted' | 'cancelled') => mutate<KitchenPrintJob>(base, `/kitchen-print-jobs/${jobId}/actions`, tenant, outlet, { action });
export const transitionPickupToken = (base: string, tenant: string, outlet: string, token: PickupToken, status: 'called' | 'collected' | 'cancelled') => mutate<PickupToken>(base, `/pickup-tokens/${token.id}/transitions`, tenant, outlet, { status, expectedVersion: token.version });
export const fetchStockTransfers = (base: string, tenant: string, outlet: string) => list<StockTransfer>(base, '/stock-transfers', tenant, outlet);
export const fetchOutletReferences = (base: string, tenant: string) => tenantList<OutletReference>(base, '/outlets', tenant);
export const fetchIngredientReferences = (base: string, tenant: string) => tenantList<IngredientReference>(base, '/ingredients', tenant);
export const createStockTransfer = (base: string, tenant: string, requestingOutletId: string, sourceOutletId: string, destinationOutletId: string, ingredientId: string, quantityBase: number, notes: string) => mutate<StockTransfer>(base, '/stock-transfers', tenant, requestingOutletId, { id: createUuidV7(), sourceOutletId, destinationOutletId, requestedBy: 'Stock movement desk', notes, lines: [{ id: createUuidV7(), ingredientId, quantityBase }] });
export const transitionStockTransfer = (base: string, tenant: string, outlet: string, transfer: StockTransfer, action: 'approved' | 'dispatched' | 'received' | 'cancelled', lines: Array<{ ingredientId: string; quantityBase: number }> = []) => mutate<StockTransfer>(base, `/stock-transfers/${transfer.id}/transitions`, tenant, outlet, { action, expectedVersion: transfer.version, lines });
export const fetchReplenishmentSuggestions = (base: string, tenant: string, outlet: string) => list<ReplenishmentSuggestion>(base, '/replenishment-suggestions', tenant, outlet);
export const saveReplenishmentRule = (base: string, tenant: string, outlet: string, ingredientId: string, sourceOutletId: string, reorderPointBase: number, targetLevelBase: number) => mutate<ReplenishmentRule>(base, '/replenishment-rules', tenant, outlet, { ingredientId, sourceOutletId, reorderPointBase, targetLevelBase, active: true });
