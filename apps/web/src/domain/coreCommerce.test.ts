import { webcrypto } from 'node:crypto';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MENU_IMPORT_UPDATED_EVENT, stageMenuImport, type MenuImportDraft } from './coreCommerce';
import type { MenuImportPreview } from './menuImport';

const preview: MenuImportPreview = {
  items: [{
    sourceLine: 2,
    name: 'Dal Tadka',
    onlineName: 'Dal Tadka',
    description: '',
    code: 'DAL-1',
    category: 'Mains',
    onlineCategory: 'Mains',
    priceMinor: 24000,
    dietaryLabel: 'veg',
    rank: 1,
    addOnGroupNames: [],
    addonBindings: [],
    variations: [],
  }],
  addonGroups: [],
  categories: ['Mains'],
  variationCount: 0,
  warnings: [],
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('menu import activation', () => {
  it('treats an exact repeat upload as a successful no-op and announces refreshes', async () => {
    vi.stubGlobal('crypto', webcrypto);
    const activated: MenuImportDraft = {
      id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb1',
      outletId: '33333333-3333-4333-8333-333333333333',
      name: 'items',
      itemFileName: 'items.csv',
      sourceSha256: '',
      status: 'applied',
      itemCount: 1,
      categoryCount: 1,
      addonGroupCount: 0,
      variationCount: 0,
      draft: preview,
      importedAt: new Date().toISOString(),
    };
    const calls: string[] = [];
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      calls.push(init?.method ?? 'GET');
      if (init?.method === 'POST') {
        const payload = JSON.parse(String(init.body)) as { payload: { sourceSha256: string } };
        activated.sourceSha256 = payload.payload.sourceSha256;
        return new Response(JSON.stringify({ data: activated }), { status: 201, headers: { 'Content-Type': 'application/json' } });
      }
      const data = activated.sourceSha256 ? [activated] : [];
      return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    });
    vi.stubGlobal('fetch', fetchMock);
    const events = vi.fn();
    window.addEventListener(MENU_IMPORT_UPDATED_EVENT, events);

    const first = await stageMenuImport('http://core', 'tenant', activated.outletId, 'items.csv', '', preview);
    const second = await stageMenuImport('http://core', 'tenant', activated.outletId, 'items.csv', '', preview);

    window.removeEventListener(MENU_IMPORT_UPDATED_EVENT, events);
    expect(first.id).toBe(activated.id);
    expect(second.id).toBe(activated.id);
    expect(calls).toEqual(['GET', 'POST', 'GET']);
    expect(events).toHaveBeenCalledTimes(2);
  });
});
