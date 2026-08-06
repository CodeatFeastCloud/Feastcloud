import { edgeApiBase } from './edgeProjection';

export interface InventorySummary {
  ingredientId: string;
  baseUnitId: string;
  ingredientName: string;
  unitSymbol: string;
  currency: string;
  quantityBase: number;
  receivedQuantity: number;
  consumedQuantity: number;
  wasteQuantity: number;
  countVarianceQuantity: number;
  stockValueMinor: number;
  wasteValueMinor: number;
  theoreticalCostMinor: number;
  countVarianceValueMinor: number;
}

export interface RecipeSummary {
  id: string;
  name: string;
  code: string;
  currentVersion?: { id: string; versionNumber: number; yieldQuantity: number; yieldUnitId: string; components: unknown[] };
}

function authContext(tenantId: string): { headers: Record<string, string>; actorId: string } {
  const token = sessionStorage.getItem('feastcloud.oidc-access-token');
  const headers: Record<string, string> = {};
  let actorId = 'manager-dashboard';
  if (token) {
    headers.Authorization = `Bearer ${token}`;
    try {
      const encoded = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
      const payload = JSON.parse(atob(encoded)) as { sub?: string };
      if (payload.sub) actorId = payload.sub;
    } catch { /* Core remains authoritative. */ }
  } else {
    headers['X-FeastCloud-Tenant-ID'] = tenantId;
    headers['X-FeastCloud-Actor-ID'] = actorId;
  }
  return { headers, actorId };
}

export function coreApiBase(configured: string | undefined): string | undefined {
  return edgeApiBase(configured);
}

export async function fetchInventorySummary(apiBase: string, tenantId: string, outletId: string): Promise<InventorySummary[]> {
  const headers: Record<string, string> = { Accept: 'application/json', ...authContext(tenantId).headers };
  const response = await fetch(`${apiBase}/inventory-summary?outletId=${encodeURIComponent(outletId)}`, { headers, cache: 'no-store' });
  if (!response.ok) throw new Error(`Inventory service returned ${response.status}`);
  const body = await response.json() as { data?: unknown };
  if (!Array.isArray(body.data)) throw new Error('Inventory response is invalid');
  return body.data as InventorySummary[];
}

export async function fetchRecipes(apiBase: string, tenantId: string): Promise<RecipeSummary[]> {
  const response = await fetch(`${apiBase}/recipes`, { headers: { Accept: 'application/json', ...authContext(tenantId).headers }, cache: 'no-store' });
  if (!response.ok) throw new Error(`Recipe service returned ${response.status}`);
  const body = await response.json() as { data?: unknown };
  if (!Array.isArray(body.data)) throw new Error('Recipe response is invalid');
  return body.data as RecipeSummary[];
}

export async function recordWaste(apiBase: string, tenantId: string, outletId: string, input: { ingredientId: string; unitId: string; quantity: number; currency: string; reason: string; eventId: string; deviceId: string }): Promise<void> {
  const context = authContext(tenantId);
  const occurredAt = new Date().toISOString();
  const envelope = { id: input.eventId, tenantId, outletId, deviceId: input.deviceId, actorId: context.actorId, occurredAt, source: 'feastcloud.web', sourceId: input.eventId, schemaVersion: '1.0', idempotencyKey: input.eventId, payload: { id: input.eventId, outletId, ingredientId: input.ingredientId, eventType: 'waste', quantity: input.quantity, unitId: input.unitId, totalCostMinor: 0, currency: input.currency, referenceType: 'waste_log', referenceId: input.eventId, reason: input.reason } };
  const response = await fetch(`${apiBase}/inventory-events`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': input.eventId, ...context.headers }, body: JSON.stringify(envelope) });
  if (!response.ok) throw new Error(`Waste service returned ${response.status}`);
}

export async function recordReceipt(apiBase: string, tenantId: string, outletId: string, input: { ingredientId: string; unitId: string; quantity: number; totalCostMinor: number; currency: string; lotCode: string; expiresAt?: string; eventId: string; deviceId: string }): Promise<void> {
  const context = authContext(tenantId);
  const occurredAt = new Date().toISOString();
  const envelope = { id: input.eventId, tenantId, outletId, deviceId: input.deviceId, actorId: context.actorId, occurredAt, source: 'feastcloud.web', sourceId: input.eventId, schemaVersion: '1.0', idempotencyKey: input.eventId, payload: { id: input.eventId, outletId, ingredientId: input.ingredientId, eventType: 'receipt', quantity: input.quantity, unitId: input.unitId, totalCostMinor: input.totalCostMinor, currency: input.currency, referenceType: 'direct_receipt', referenceId: input.eventId, lotCode: input.lotCode, expiresAt: input.expiresAt || undefined } };
  const response = await fetch(`${apiBase}/inventory-events`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': input.eventId, ...context.headers }, body: JSON.stringify(envelope) });
  if (!response.ok) throw new Error(`Receiving service returned ${response.status}`);
}

export interface RecordedCount { lines: Array<{ ingredientId: string; expectedQuantityBase: number; countedQuantityBase: number; varianceQuantityBase: number; varianceCostMinor: number }> }

export async function recordInventoryCount(apiBase: string, tenantId: string, outletId: string, input: { notes: string; lines: Array<{ id: string; ingredientId: string; unitId: string; countedQuantity: number }>; countId: string; deviceId: string }): Promise<RecordedCount> {
  const context = authContext(tenantId);
  const occurredAt = new Date().toISOString();
  const envelope = { id: input.countId, tenantId, outletId, deviceId: input.deviceId, actorId: context.actorId, occurredAt, source: 'feastcloud.web', sourceId: input.countId, schemaVersion: '1.0', idempotencyKey: input.countId, payload: { id: input.countId, outletId, notes: input.notes, lines: input.lines } };
  const response = await fetch(`${apiBase}/inventory-counts`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': input.countId, ...context.headers }, body: JSON.stringify(envelope) });
  if (!response.ok) throw new Error(`Stock count service returned ${response.status}`);
  const body = await response.json() as { data?: RecordedCount };
  if (!body.data?.lines) throw new Error('Stock count response is invalid');
  return body.data;
}
