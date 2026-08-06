import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const serviceWorker = readFileSync(resolve(process.cwd(), 'public/sw.js'), 'utf8');

describe('service worker cache policy', () => {
  it('versions caches and bypasses authenticated, no-store and API requests', () => {
    expect(serviceWorker).toContain("feastcloud-shell-v5");
    expect(serviceWorker).toContain("url.pathname.startsWith('/api/')");
    expect(serviceWorker).toContain("request.headers.has('Authorization')");
    expect(serviceWorker).toContain("request.cache === 'no-store'");
    expect(serviceWorker).toContain("cacheControl.includes('no-store')");
  });

  it('keeps unverified language resources out of the generic runtime cache', () => {
    expect(serviceWorker).toContain("url.pathname.startsWith('/assets/')");
    expect(serviceWorker).not.toContain("url.pathname.startsWith('/language-packs/')");
    expect(serviceWorker).not.toContain("'/language-packs/index.json'");
    expect(serviceWorker).toContain('if (!isCacheableAsset(url)) return');
  });

  it('preserves the dedicated cache containing application-verified language packs', () => {
    expect(serviceWorker).toContain("const LANGUAGE_PACK_CACHE = 'feastcloud-language-packs-v1'");
    expect(serviceWorker).toContain('name !== LANGUAGE_PACK_CACHE');
  });
});
