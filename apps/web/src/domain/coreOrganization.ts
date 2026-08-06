import { coreApiBase } from './coreInventory';
import { createUuidV7, getDeviceId } from './kitchen';

export type StationType = 'preparation' | 'cooking' | 'beverage' | 'assembly' | 'expo' | 'packing';

interface RecordMetadata {
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface Organization extends RecordMetadata {
  id: string;
  tenantId: string;
  name: string;
  legalName?: string;
  defaultLocale: string;
  defaultCurrency: string;
  active: boolean;
}

export interface Outlet extends RecordMetadata {
  id: string;
  tenantId: string;
  organizationId: string;
  name: string;
  code: string;
  timeZone: string;
  currency: string;
  active: boolean;
}

export interface Brand extends RecordMetadata {
  id: string;
  tenantId: string;
  organizationId: string;
  name: string;
  code: string;
  active: boolean;
}

export interface BrandOutletAssignment extends RecordMetadata {
  tenantId: string;
  brandId: string;
  outletId: string;
  active: boolean;
}

export interface Station extends RecordMetadata {
  id: string;
  tenantId: string;
  outletId: string;
  name: string;
  code: string;
  type: StationType;
  active: boolean;
}

export interface StationCapacityLimit {
  stationId: string;
  maxActiveTickets: number;
  version: number;
  updatedAt: string;
}

export interface OutletControlProfile {
  outletId: string;
  profileName: string;
  approvalPolicy: Record<string, unknown>;
  featureProfile: Record<string, unknown>;
  version: number;
  updatedAt: string;
}

export interface OrganizationControlData {
  organizations: Organization[];
  outlets: Outlet[];
  brands: Brand[];
  brandAssignments: BrandOutletAssignment[];
  stations: Station[];
}

export interface WorkspaceIdentity {
  organizationName: string;
  outletName: string;
}

export interface ProvisionedTenant {
  organization: Organization;
  outlet: Outlet;
  brand: Brand;
  stations: Station[];
  ownerHandoff: { name: string; email: string; status: string };
}

type CollectionEnvelope<T> = {
  data?: unknown;
  meta?: { page?: { nextCursor?: string } };
};

function auth(tenantId: string): { actorId: string; headers: Record<string, string> } {
  const token = sessionStorage.getItem('feastcloud.oidc-access-token');
  if (token) return { actorId: 'manager-dashboard', headers: { Authorization: `Bearer ${token}` } };
  return {
    actorId: 'manager-dashboard',
    headers: {
      'X-FeastCloud-Tenant-ID': tenantId,
      'X-FeastCloud-Actor-ID': 'manager-dashboard',
    },
  };
}

function pathWithQuery(path: string, query: Record<string, string | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value);
  }
  const encoded = params.toString();
  return encoded ? `${path}?${encoded}` : path;
}

async function errorFor(response: Response, path: string): Promise<Error> {
  let detail = '';
  try {
    const body = await response.json() as { detail?: unknown; message?: unknown; code?: unknown };
    detail = typeof body.detail === 'string' ? body.detail : typeof body.message === 'string' ? body.message : typeof body.code === 'string' ? body.code : '';
  } catch {
    // A network gateway can return a non-JSON error. The status remains useful.
  }
  return new Error(`${path}: ${response.status}${detail ? ` · ${detail}` : ''}`);
}

async function listAll<T>(api: string, path: string, tenantId: string, query: Record<string, string | undefined> = {}): Promise<T[]> {
  const values: T[] = [];
  let cursor: string | undefined;
  do {
    const response = await fetch(`${api}${pathWithQuery(path, { ...query, limit: '200', cursor })}`, {
      headers: { Accept: 'application/json', ...auth(tenantId).headers },
      cache: 'no-store',
    });
    if (!response.ok) throw await errorFor(response, path);
    const body = await response.json() as CollectionEnvelope<T>;
    if (!Array.isArray(body.data)) throw new Error(`${path}: invalid collection response`);
    values.push(...body.data as T[]);
    cursor = body.meta?.page?.nextCursor;
  } while (cursor);
  return values;
}

async function read<T>(api: string, path: string, tenantId: string, query: Record<string, string | undefined> = {}): Promise<T> {
  const response = await fetch(`${api}${pathWithQuery(path, query)}`, {
    headers: { Accept: 'application/json', ...auth(tenantId).headers },
    cache: 'no-store',
  });
  if (!response.ok) throw await errorFor(response, path);
  const body = await response.json() as { data?: unknown };
  if (body.data === undefined || body.data === null) throw new Error(`${path}: invalid response`);
  return body.data as T;
}

async function mutate<T>(api: string, path: string, tenantId: string, outletId: string, payload: Record<string, unknown>): Promise<T> {
  const operationId = createUuidV7();
  const identity = auth(tenantId);
  const body = {
    id: operationId,
    tenantId,
    outletId,
    deviceId: getDeviceId(),
    actorId: identity.actorId,
    occurredAt: new Date().toISOString(),
    source: 'feastcloud.web',
    sourceId: operationId,
    schemaVersion: '1.0',
    idempotencyKey: operationId,
    payload,
  };
  const response = await fetch(`${api}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': operationId, ...identity.headers },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw await errorFor(response, path);
  const result = await response.json() as { data?: unknown };
  if (result.data === undefined || result.data === null) throw new Error(`${path}: invalid mutation response`);
  return result.data as T;
}

export const organizationApiBase = coreApiBase;

export async function fetchOrganizationControlData(api: string, tenantId: string): Promise<OrganizationControlData> {
  const [organizations, outlets, brands, brandAssignments, stations] = await Promise.all([
    listAll<Organization>(api, '/organizations', tenantId),
    listAll<Outlet>(api, '/outlets', tenantId),
    listAll<Brand>(api, '/brands', tenantId),
    listAll<BrandOutletAssignment>(api, '/brand-outlet-assignments', tenantId).catch((error: unknown) => {
      // An older Core can still be used read-only while it is being upgraded.
      // The page exposes the rollout action only after the new API is present.
      if (error instanceof Error && error.message.startsWith('/brand-outlet-assignments: 404')) return [];
      throw error;
    }),
    listAll<Station>(api, '/stations', tenantId),
  ]);
  return { organizations, outlets, brands, brandAssignments, stations };
}

export async function fetchWorkspaceIdentity(api: string, tenantId: string, outletId: string): Promise<WorkspaceIdentity> {
  const [organization, outlet] = await Promise.all([
    read<Organization>(api, `/organizations/${encodeURIComponent(tenantId)}`, tenantId),
    read<Outlet>(api, `/outlets/${encodeURIComponent(outletId)}`, tenantId),
  ]);
  return { organizationName: organization.name, outletName: outlet.name };
}

export const fetchOutletProfile = (api: string, tenantId: string, outletId: string) =>
  read<OutletControlProfile>(api, '/outlet-control-profile', tenantId, { outletId });

export const fetchStationCapacity = (api: string, tenantId: string, outletId: string) =>
  read<StationCapacityLimit[]>(api, '/station-capacity-limits', tenantId, { outletId });

export function createOrganization(api: string, tenantId: string, outletId: string, input: { name: string; legalName?: string; defaultLocale: string; defaultCurrency: string }) {
  return mutate<Organization>(api, '/organizations', tenantId, outletId, {
    id: tenantId,
    name: input.name,
    legalName: input.legalName?.trim() || undefined,
    defaultLocale: input.defaultLocale,
    defaultCurrency: input.defaultCurrency.toUpperCase(),
  });
}

export function createOutlet(api: string, tenantId: string, anchorOutletId: string, organizationId: string, input: { name: string; code: string; timeZone: string; currency: string }) {
  return mutate<Outlet>(api, '/outlets', tenantId, anchorOutletId, {
    id: createUuidV7(),
    organizationId,
    name: input.name,
    code: input.code.trim().toUpperCase(),
    timeZone: input.timeZone,
    currency: input.currency.toUpperCase(),
  });
}

export function createBrand(api: string, tenantId: string, anchorOutletId: string, organizationId: string, input: { name: string; code: string }) {
  return mutate<Brand>(api, '/brands', tenantId, anchorOutletId, {
    id: createUuidV7(),
    organizationId,
    name: input.name,
    code: input.code.trim().toUpperCase(),
  });
}

export function createStation(api: string, tenantId: string, outletId: string, input: { name: string; code: string; type: StationType }) {
  return mutate<Station>(api, '/stations', tenantId, outletId, {
    id: createUuidV7(),
    outletId,
    name: input.name,
    code: input.code.trim().toUpperCase(),
    type: input.type,
  });
}

export function setBrandOutletAssignment(api: string, tenantId: string, outletId: string, input: { brandId: string; active: boolean; expectedVersion?: number }) {
  return mutate<BrandOutletAssignment>(api, '/brand-outlet-assignments', tenantId, outletId, {
    brandId: input.brandId,
    outletId,
    active: input.active,
    expectedVersion: input.expectedVersion ?? 0,
  });
}

export async function provisionTenant(api: string, input: {
  organizationName: string; legalName?: string; ownerName: string; ownerEmail: string; defaultLocale: string; defaultCurrency: string;
  outletName: string; outletCode: string; timeZone: string; brandName: string; brandCode: string; template: 'restaurant' | 'cloud' | 'central';
}): Promise<ProvisionedTenant> {
  const tenantId = createUuidV7();
  const outletId = createUuidV7();
  const operationId = createUuidV7();
  const body = { id: operationId, tenantId, outletId, deviceId: getDeviceId(), actorId: 'platform-admin', occurredAt: new Date().toISOString(), source: 'feastcloud.platform', sourceId: operationId, schemaVersion: '1.0', idempotencyKey: operationId, payload: { ...input, legalName: input.legalName?.trim() || undefined, defaultCurrency: input.defaultCurrency.toUpperCase(), outletCode: input.outletCode.toUpperCase(), brandCode: input.brandCode.toUpperCase() } };
  const token = sessionStorage.getItem('feastcloud.oidc-access-token');
  const headers: Record<string, string> = { 'Content-Type': 'application/json', 'Idempotency-Key': operationId };
  if (token) headers.Authorization = `Bearer ${token}`;
  else Object.assign(headers, { 'X-FeastCloud-Tenant-ID': (import.meta.env.VITE_PLATFORM_TENANT_ID as string | undefined)?.trim() || '11111111-1111-4111-8111-111111111111', 'X-FeastCloud-Actor-ID': 'platform-admin', 'X-FeastCloud-Platform-Admin': 'true' });
  const response = await fetch(`${api}/platform/tenants`, { method: 'POST', headers, body: JSON.stringify(body) });
  if (!response.ok) throw await errorFor(response, '/platform/tenants');
  const result = await response.json() as { data?: ProvisionedTenant };
  if (!result.data) throw new Error('/platform/tenants: invalid provisioning response');
  return result.data;
}

export function saveOutletProfile(api: string, tenantId: string, outletId: string, input: { profileName: string; approvalPolicy: Record<string, unknown>; featureProfile: Record<string, unknown> }) {
  return mutate<OutletControlProfile>(api, '/outlet-control-profile', tenantId, outletId, input);
}

export function saveStationCapacity(api: string, tenantId: string, outletId: string, stationId: string, maxActiveTickets: number) {
  return mutate<StationCapacityLimit>(api, '/station-capacity-limits', tenantId, outletId, { stationId, maxActiveTickets });
}
