import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  formatMoney,
  getLanguageDirection,
  getLanguageOptions,
  installLanguagePack,
  LANGUAGE_PACK_CACHE_NAME,
  loadConfiguredLanguagePacks,
  resolveLocale,
  translate,
} from '.';
import { en, messages } from './messages';

function requestUrl(request: RequestInfo | URL): string {
  const raw = typeof request === 'string' ? request : request instanceof URL ? request.href : request.url;
  return new URL(raw, globalThis.location.href).href;
}

function memoryCache(initial: Map<string, Response> = new Map()) {
  const entries = new Map(initial);
  const cache = {
    match: vi.fn(async (request: RequestInfo | URL) => entries.get(requestUrl(request))?.clone()),
    put: vi.fn(async (request: RequestInfo | URL, response: Response) => {
      entries.set(requestUrl(request), response.clone());
    }),
    keys: vi.fn(async () => [...entries.keys()].map((url) => ({ url }) as Request)),
    delete: vi.fn(async (request: RequestInfo | URL) => entries.delete(requestUrl(request))),
  } as unknown as Cache;
  return { cache, entries };
}

async function checksum(source: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(source));
  return [...new Uint8Array(digest)]
    .map((value) => value.toString(16).padStart(2, '0'))
    .join('');
}

function packSource(locale: string, name: string, productName: string): string {
  return JSON.stringify({
    locale,
    name,
    direction: 'ltr',
    version: '1.0.0',
    certification: {
      ui: 'reviewed',
      operations: 'draft',
      speechInput: 'unsupported',
      speechOutput: 'unsupported',
    },
    messages: { ...en, 'app.product': productName },
  });
}

describe('language packs', () => {
  afterEach(() => vi.unstubAllGlobals());
  it('keeps every bundled language structurally complete', () => {
    const englishKeys = Object.keys(en).sort();
    for (const pack of Object.values(messages)) {
      expect(Object.keys(pack).sort()).toEqual(englishKeys);
      expect(Object.values(pack).every((message) => message.trim().length > 0)).toBe(true);
    }
  });

  it('interpolates values without evaluating content', () => {
    expect(translate('hi', 'order.sent', { number: 109 })).toContain('#109');
    expect(translate('bn', 'sync.pendingMany', { count: 3 })).toContain('3');
  });

  it('formats integer minor units in the selected locale', () => {
    expect(formatMoney('en', 32_900)).toMatch(/329/);
    expect(formatMoney('hi', 32_900)).toContain('329');
  });

  it('installs a checksum-delivered language pack without application code changes', () => {
    installLanguagePack({
      locale: 'ta-IN',
      name: 'தமிழ்',
      direction: 'ltr',
      version: '1.0.0',
      certification: { ui: 'reviewed', operations: 'draft', speechInput: 'unsupported', speechOutput: 'unsupported' },
      messages: {
        ...en,
        'app.product': 'சமையலறை OS',
        'sync.pendingMany': '{count, plural, one {# மாற்றம்} other {# மாற்றங்கள்}}',
      },
    });

    expect(resolveLocale('ta-IN')).toBe('ta-IN');
    expect(resolveLocale('ta-LK')).toBe('ta-IN');
    expect(translate('ta-IN', 'app.product')).toBe('சமையலறை OS');
    expect(translate('ta-IN', 'sync.pendingMany', { count: 3 })).toBe('3 மாற்றங்கள்');
    expect(getLanguageDirection('ta-IN')).toBe('ltr');
    expect(getLanguageOptions()).toContainEqual(
      expect.objectContaining({ locale: 'ta-IN', name: 'தமிழ்' }),
    );
  });

  it('rejects packs that remove required interpolation placeholders', () => {
    expect(() =>
      installLanguagePack({
        locale: 'ar',
        name: 'العربية',
        direction: 'rtl',
        version: '1.0.0',
        certification: { ui: 'reviewed', operations: 'draft', speechInput: 'unsupported', speechOutput: 'unsupported' },
        messages: { ...en, 'order.sent': 'تم إرسال الطلب' },
      }),
    ).toThrow(/placeholders/);
  });

  it('loads a same-origin pack only when its index checksum matches', async () => {
    const packSource = JSON.stringify({
      locale: 'ar',
      name: 'العربية',
      direction: 'rtl',
      version: '1.0.0',
      certification: { ui: 'reviewed', operations: 'draft', speechInput: 'unsupported', speechOutput: 'unsupported' },
      messages: { ...en, 'app.product': 'نظام المطبخ' },
    });
    const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(packSource));
    const checksum = [...new Uint8Array(digest)]
      .map((value) => value.toString(16).padStart(2, '0'))
      .join('');
    const indexSource = JSON.stringify({
      schemaVersion: '1.0',
      packs: [
        {
          locale: 'ar',
          url: './ar.json',
          sha256: checksum,
        },
      ],
    });
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: URL | RequestInfo) => {
        const url = String(input);
        return {
          ok: true,
          text: async () => (url.endsWith('/index.json') ? indexSource : packSource),
        } as Response;
      }),
    );

    await loadConfiguredLanguagePacks();

    expect(translate('ar', 'app.product')).toBe('نظام المطبخ');
    expect(getLanguageDirection('ar')).toBe('rtl');
  });

  it('durably caches the index and content-addressed pack only after verification', async () => {
    const source = packSource('mr-IN', 'मराठी', 'किचन प्रणाली');
    const sha256 = await checksum(source);
    const indexSource = JSON.stringify({
      schemaVersion: '1.0',
      packs: [{ locale: 'mr-IN', url: './mr.json', sha256 }],
    });
    const stored = memoryCache();
    const open = vi.fn(async () => stored.cache);
    vi.stubGlobal('caches', { open });
    const networkFetch = vi.fn(async (input: URL | RequestInfo) =>
      new Response(String(input).endsWith('/index.json') ? indexSource : source, {
        headers: { 'Content-Type': 'application/json' },
      }));
    vi.stubGlobal('fetch', networkFetch);

    await loadConfiguredLanguagePacks();

    const indexUrl = new URL('/language-packs/index.json', globalThis.location.href);
    const packCacheUrl = new URL('./mr.json', indexUrl);
    packCacheUrl.searchParams.set('__feastcloud_verified_sha256', sha256);
    expect(open).toHaveBeenCalledWith(LANGUAGE_PACK_CACHE_NAME);
    expect(stored.entries.has(indexUrl.href)).toBe(true);
    expect(stored.entries.has(packCacheUrl.href)).toBe(true);
    expect(translate('mr-IN', 'app.product')).toBe('किचन प्रणाली');
    expect(networkFetch).toHaveBeenCalledWith(
      expect.any(URL),
      expect.objectContaining({ cache: 'no-store', credentials: 'omit' }),
    );
  });

  it('restores and re-verifies a cached pack when the language network is offline', async () => {
    const source = packSource('gu-IN', 'ગુજરાતી', 'રસોડું સિસ્ટમ');
    const sha256 = await checksum(source);
    const indexSource = JSON.stringify({
      schemaVersion: '1.0',
      packs: [{ locale: 'gu-IN', url: './gu.json', sha256 }],
    });
    const indexUrl = new URL('/language-packs/index.json', globalThis.location.href);
    const packCacheUrl = new URL('./gu.json', indexUrl);
    packCacheUrl.searchParams.set('__feastcloud_verified_sha256', sha256);
    const stored = memoryCache(
      new Map([
        [indexUrl.href, new Response(indexSource)],
        [packCacheUrl.href, new Response(source)],
      ]),
    );
    vi.stubGlobal('caches', { open: vi.fn(async () => stored.cache) });
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('offline'))));

    await expect(loadConfiguredLanguagePacks()).resolves.toBeUndefined();

    expect(translate('gu-IN', 'app.product')).toBe('રસોડું સિસ્ટમ');
    expect(stored.cache.match).toHaveBeenCalledWith(indexUrl.href);
    expect(stored.cache.match).toHaveBeenCalledWith(packCacheUrl.href);
  });

  it('does not persist a pack whose response fails its indexed checksum', async () => {
    const expectedSource = packSource('te-IN', 'తెలుగు', 'వంటగది వ్యవస్థ');
    const tamperedSource = packSource('te-IN', 'తెలుగు', 'changed after indexing');
    const sha256 = await checksum(expectedSource);
    const indexSource = JSON.stringify({
      schemaVersion: '1.0',
      packs: [{ locale: 'te-IN', url: './te.json', sha256 }],
    });
    const stored = memoryCache();
    vi.stubGlobal('caches', { open: vi.fn(async () => stored.cache) });
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: URL | RequestInfo) =>
        new Response(String(input).endsWith('/index.json') ? indexSource : tamperedSource)),
    );

    await expect(loadConfiguredLanguagePacks()).rejects.toThrow(/checksum/);
    expect(stored.entries.size).toBe(0);
    expect(stored.cache.put).not.toHaveBeenCalled();
  });

  it('rejects pack URLs outside the trusted index origin before fetching them', async () => {
    const indexSource = JSON.stringify({
      schemaVersion: '1.0',
      packs: [{ locale: 'pa-IN', url: 'https://untrusted.example/pa.json', sha256: 'a'.repeat(64) }],
    });
    const networkFetch = vi.fn(async () => new Response(indexSource));
    vi.stubGlobal('fetch', networkFetch);

    await expect(loadConfiguredLanguagePacks()).rejects.toThrow(/trusted index origin/);
    expect(networkFetch).toHaveBeenCalledOnce();
  });
});
