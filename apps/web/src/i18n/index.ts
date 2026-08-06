import type { Locale } from '../domain/types';
import { IntlMessageFormat } from 'intl-messageformat';
import { bn, en, hi, messages, type MessageKey } from './messages';

export type LanguageDirection = 'ltr' | 'rtl';
export type CertificationStatus = 'unsupported' | 'draft' | 'reviewed' | 'certified';

export interface LanguageCertification {
  ui: CertificationStatus;
  operations: CertificationStatus;
  speechInput: CertificationStatus;
  speechOutput: CertificationStatus;
}

export interface InstallableLanguagePack {
  locale: string;
  name: string;
  direction: LanguageDirection;
  version: string;
  fallbackLocale?: string;
  certification: LanguageCertification;
  messages: Record<string, string>;
}

export interface LanguageOption {
  locale: string;
  name: string;
  direction: LanguageDirection;
  version: string;
  intlLocale: string;
  certification: LanguageCertification;
}

interface LanguagePackIndex {
  schemaVersion: '1.0';
  packs: Array<{ locale: string; url: string; sha256: string }>;
}

interface ValidatedLanguagePack {
  option: LanguageOption;
  messages: Record<MessageKey, string>;
}

interface VerifiedLanguagePack {
  cacheKey: string;
  source: string;
  validated: ValidatedLanguagePack;
}

interface VerifiedLanguageBundle {
  indexSource: string;
  packs: VerifiedLanguagePack[];
}

const installedMessages = new Map<string, Record<MessageKey, string>>([
  ['en', en],
  ['hi', hi],
  ['bn', bn],
]);

const languageOptions = new Map<string, LanguageOption>([
  ['en', { locale: 'en', name: 'English', direction: 'ltr', version: '0.1.0', intlLocale: 'en-IN', certification: { ui: 'reviewed', operations: 'draft', speechInput: 'draft', speechOutput: 'unsupported' } }],
  ['hi', { locale: 'hi', name: 'हिन्दी', direction: 'ltr', version: '0.1.0', intlLocale: 'hi-IN', certification: { ui: 'reviewed', operations: 'draft', speechInput: 'draft', speechOutput: 'unsupported' } }],
  ['bn', { locale: 'bn', name: 'বাংলা', direction: 'ltr', version: '0.1.0', intlLocale: 'bn-IN', certification: { ui: 'reviewed', operations: 'draft', speechInput: 'draft', speechOutput: 'unsupported' } }],
]);

const englishKeys = Object.keys(en) as MessageKey[];
const maximumIndexBytes = 128 * 1024;
const maximumPackBytes = 512 * 1024;
const formatterCache = new Map<string, IntlMessageFormat>();
export const LANGUAGE_PACK_CACHE_NAME = 'feastcloud-language-packs-v1';
const verifiedChecksumParameter = '__feastcloud_verified_sha256';

function canonicalLocale(locale: string): string {
  try {
    return Intl.getCanonicalLocales(locale)[0] ?? '';
  } catch {
    return '';
  }
}

function placeholders(message: string): string {
  return [...message.matchAll(/\{([A-Za-z][A-Za-z0-9_]*)(?=\s*[,}])/g)]
    .map((match) => match[1])
    .filter((name, index, names) => names.indexOf(name) === index)
    .sort()
    .join('|');
}

function validateMessages(locale: string, candidate: Record<string, string>): Record<MessageKey, string> {
  const validated = {} as Record<MessageKey, string>;
  for (const key of englishKeys) {
    const value = candidate[key];
    if (typeof value !== 'string' || value.trim().length === 0 || value.length > 2_000) {
      throw new Error(`language pack message ${key} is missing or invalid`);
    }
    if (placeholders(value) !== placeholders(en[key])) {
      throw new Error(`language pack message ${key} changes required placeholders`);
    }
    try {
      new IntlMessageFormat(value, locale);
    } catch {
      throw new Error(`language pack message ${key} is not valid ICU MessageFormat`);
    }
    validated[key] = value;
  }
  return validated;
}

function validateLanguagePack(pack: InstallableLanguagePack): ValidatedLanguagePack {
  if (!pack || typeof pack !== 'object') throw new Error('language pack must be an object');
  const allowedFields = new Set(['locale', 'name', 'direction', 'version', 'fallbackLocale', 'certification', 'messages']);
  if (Object.keys(pack).some((field) => !allowedFields.has(field))) {
    throw new Error('language pack contains an unsupported field');
  }
  const locale = canonicalLocale(pack.locale);
  if (!/^[a-z]{2,3}(?:-[A-Z][a-z]{3})?(?:-(?:[A-Z]{2}|[0-9]{3}))?$/.test(locale) || pack.name.trim().length === 0 || pack.name.length > 80) {
    throw new Error('language pack locale or name is invalid');
  }
  if (pack.direction !== 'ltr' && pack.direction !== 'rtl') {
    throw new Error('language pack direction must be ltr or rtl');
  }
  if (!/^\d+\.\d+\.\d+$/.test(pack.version)) {
    throw new Error('language pack version must use semantic versioning');
  }
  if (pack.fallbackLocale !== undefined && !canonicalLocale(pack.fallbackLocale)) {
    throw new Error('language pack fallbackLocale is invalid');
  }
  const certificationFields = ['ui', 'operations', 'speechInput', 'speechOutput'] as const;
  const statuses = new Set<CertificationStatus>(['unsupported', 'draft', 'reviewed', 'certified']);
  if (
    !pack.certification ||
    typeof pack.certification !== 'object' ||
    Object.keys(pack.certification).length !== certificationFields.length ||
    certificationFields.some((field) => !statuses.has(pack.certification[field]))
  ) {
    throw new Error('language pack certification is invalid');
  }
  const validated = validateMessages(locale, pack.messages);
  const option: LanguageOption = {
    locale,
    name: pack.name.trim(),
    direction: pack.direction,
    version: pack.version,
    intlLocale: locale,
    certification: { ...pack.certification },
  };
  return { option, messages: validated };
}

function commitLanguagePack(validated: ValidatedLanguagePack): LanguageOption {
  installedMessages.set(validated.option.locale, validated.messages);
  languageOptions.set(validated.option.locale, validated.option);
  return validated.option;
}

export function installLanguagePack(pack: InstallableLanguagePack): LanguageOption {
  return commitLanguagePack(validateLanguagePack(pack));
}

export function getLanguageOptions(): LanguageOption[] {
  return [...languageOptions.values()];
}

export function resolveLocale(requested: string): Locale {
  const normalized = canonicalLocale(requested) || requested;
  const exact = [...languageOptions.keys()].find(
    (locale) => locale.toLowerCase() === normalized.toLowerCase(),
  );
  if (exact) return exact;

  const base = normalized.split('-')[0].toLowerCase();
  const compatible = [...languageOptions.values()].find(
    (option) =>
      option.locale.split('-')[0].toLowerCase() === base ||
      option.intlLocale.split('-')[0].toLowerCase() === base,
  );
  return compatible?.locale ?? 'en';
}

export function getLanguageDirection(locale: Locale): LanguageDirection {
  return languageOptions.get(resolveLocale(locale))?.direction ?? 'ltr';
}

export function translate(
  locale: Locale,
  key: MessageKey,
  replacements: Record<string, string | number> = {},
): string {
  const pack = installedMessages.get(resolveLocale(locale)) ?? messages.en;
  const message = pack[key] ?? messages.en[key];
  const cacheKey = `${locale}\u0000${key}\u0000${message}`;
  let formatter = formatterCache.get(cacheKey);
  if (!formatter) {
    formatter = new IntlMessageFormat(message, intlLocale(locale));
    formatterCache.set(cacheKey, formatter);
  }
  const formatted = formatter.format(replacements);
  return Array.isArray(formatted) ? formatted.join('') : String(formatted);
}

export function createTranslator(locale: Locale) {
  return (key: MessageKey, replacements?: Record<string, string | number>) =>
    translate(locale, key, replacements);
}

function intlLocale(locale: Locale): string {
  return languageOptions.get(resolveLocale(locale))?.intlLocale ?? 'en-IN';
}

export function formatMoney(locale: Locale, amountInMinorUnits: number): string {
  return new Intl.NumberFormat(intlLocale(locale), {
    style: 'currency',
    currency: 'INR',
    maximumFractionDigits: 0,
  }).format(amountInMinorUnits / 100);
}

export function formatTime(locale: Locale, iso: string): string {
  return new Intl.DateTimeFormat(intlLocale(locale), {
    hour: 'numeric',
    minute: '2-digit',
  }).format(new Date(iso));
}

async function sha256Hex(content: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(content));
  return [...new Uint8Array(digest)].map((value) => value.toString(16).padStart(2, '0')).join('');
}

function parseIndex(value: unknown): LanguagePackIndex {
  if (!value || typeof value !== 'object') throw new Error('language pack index must be an object');
  const candidate = value as Partial<LanguagePackIndex>;
  if (
    Object.keys(value).some((field) => field !== 'schemaVersion' && field !== 'packs') ||
    candidate.schemaVersion !== '1.0' ||
    !Array.isArray(candidate.packs) ||
    candidate.packs.length > 50
  ) {
    throw new Error('language pack index is incompatible or too large');
  }
  for (const entry of candidate.packs) {
    if (
      !entry ||
      Object.keys(entry).some((field) => !['locale', 'url', 'sha256'].includes(field)) ||
      typeof entry.locale !== 'string' ||
      !canonicalLocale(entry.locale) ||
      typeof entry.url !== 'string' ||
      entry.url.length === 0 ||
      !/^[a-f0-9]{64}$/.test(entry.sha256)
    ) {
      throw new Error('language pack index contains an invalid entry');
    }
  }
  return candidate as LanguagePackIndex;
}

function trustedPackUrl(
  entry: LanguagePackIndex['packs'][number],
  indexUrl: URL,
): URL {
  const packUrl = new URL(entry.url, indexUrl);
  if (packUrl.origin !== indexUrl.origin) {
    throw new Error(`language pack ${entry.locale} must share the trusted index origin`);
  }
  return packUrl;
}

function verifiedPackCacheKey(packUrl: URL, checksum: string): string {
  const cacheUrl = new URL(packUrl);
  cacheUrl.hash = '';
  cacheUrl.searchParams.set(verifiedChecksumParameter, checksum);
  return cacheUrl.href;
}

function assertResponseOrigin(response: Response, expectedUrl: URL, resource: string): void {
  if (response.url && new URL(response.url).origin !== expectedUrl.origin) {
    throw new Error(`${resource} redirected outside its trusted origin`);
  }
}

async function boundedText(response: Response, maximumBytes: number, resource: string): Promise<string> {
  const source = await response.text();
  if (new TextEncoder().encode(source).byteLength > maximumBytes) {
    throw new Error(`${resource} is too large`);
  }
  return source;
}

async function verifyLanguagePack(
  entry: LanguagePackIndex['packs'][number],
  indexUrl: URL,
  response: Response,
): Promise<VerifiedLanguagePack> {
  const packUrl = trustedPackUrl(entry, indexUrl);
  if (!response.ok) throw new Error(`language pack ${entry.locale} is unavailable`);
  assertResponseOrigin(response, packUrl, `language pack ${entry.locale}`);
  const source = await boundedText(response, maximumPackBytes, `language pack ${entry.locale}`);
  if ((await sha256Hex(source)) !== entry.sha256) {
    throw new Error(`language pack ${entry.locale} checksum does not match the index`);
  }
  const validated = validateLanguagePack(JSON.parse(source) as InstallableLanguagePack);
  if (validated.option.locale !== canonicalLocale(entry.locale)) {
    throw new Error(`language pack ${entry.locale} has a mismatched locale`);
  }
  return {
    cacheKey: verifiedPackCacheKey(packUrl, entry.sha256),
    source,
    validated,
  };
}

function languagePackCacheAvailable(): boolean {
  return typeof globalThis.caches?.open === 'function';
}

async function restoreVerifiedLanguageBundle(indexUrl: URL): Promise<boolean> {
  if (!languagePackCacheAvailable()) return false;

  try {
    const cache = await globalThis.caches.open(LANGUAGE_PACK_CACHE_NAME);
    const cachedIndex = await cache.match(indexUrl.href);
    if (!cachedIndex?.ok) return false;
    const indexSource = await boundedText(cachedIndex, maximumIndexBytes, 'language pack index');
    const index = parseIndex(JSON.parse(indexSource) as unknown);
    const packs = await Promise.all(
      index.packs.map(async (entry) => {
        const packUrl = trustedPackUrl(entry, indexUrl);
        const cachedPack = await cache.match(verifiedPackCacheKey(packUrl, entry.sha256));
        if (!cachedPack) throw new Error(`verified language pack ${entry.locale} is missing`);
        return verifyLanguagePack(entry, indexUrl, cachedPack);
      }),
    );
    packs.forEach((pack) => commitLanguagePack(pack.validated));
    return true;
  } catch {
    // Treat a partial, corrupt or obsolete cache as absent and retry from the
    // trusted network origin. Cached bytes never bypass the normal validators.
    return false;
  }
}

async function persistVerifiedLanguageBundle(
  indexUrl: URL,
  bundle: VerifiedLanguageBundle,
): Promise<void> {
  if (!languagePackCacheAvailable()) return;

  const cache = await globalThis.caches.open(LANGUAGE_PACK_CACHE_NAME);
  const responseInit: ResponseInit = {
    headers: {
      'Cache-Control': 'public, max-age=31536000, immutable',
      'Content-Type': 'application/json; charset=utf-8',
    },
  };

  // Packs are content-addressed by their verified checksum. Write the index last:
  // interrupted updates therefore leave the previous index and pack set usable.
  await Promise.all(
    bundle.packs.map((pack) => cache.put(pack.cacheKey, new Response(pack.source, responseInit))),
  );
  await cache.put(
    indexUrl.href,
    new Response(bundle.indexSource, {
      headers: {
        'Cache-Control': 'public, max-age=86400',
        'Content-Type': 'application/json; charset=utf-8',
      },
    }),
  );

  const retainedKeys = new Set([indexUrl.href, ...bundle.packs.map((pack) => pack.cacheKey)]);
  const cachedRequests = await cache.keys();
  await Promise.all(
    cachedRequests
      .filter((request) => !retainedKeys.has(request.url))
      .map((request) => cache.delete(request)),
  );
}

async function fetchVerifiedLanguageBundle(indexUrl: URL): Promise<VerifiedLanguageBundle | null> {
  const indexResponse = await fetch(indexUrl, { credentials: 'omit', cache: 'no-store' });
  if (!indexResponse.ok) return null;
  assertResponseOrigin(indexResponse, indexUrl, 'language pack index');
  const indexSource = await boundedText(indexResponse, maximumIndexBytes, 'language pack index');
  const index = parseIndex(JSON.parse(indexSource) as unknown);
  const packs = await Promise.all(
    index.packs.map(async (entry) => {
      const packUrl = trustedPackUrl(entry, indexUrl);
      const response = await fetch(packUrl, { credentials: 'omit', cache: 'no-store' });
      return verifyLanguagePack(entry, indexUrl, response);
    }),
  );
  return { indexSource, packs };
}

export async function loadConfiguredLanguagePacks(): Promise<void> {
  const configured = (import.meta.env.VITE_LANGUAGE_PACK_INDEX_URL as string | undefined)?.trim();
  const indexUrl = new URL(configured || '/language-packs/index.json', globalThis.location?.href);
  const restoredFromCache = await restoreVerifiedLanguageBundle(indexUrl);

  try {
    const bundle = await fetchVerifiedLanguageBundle(indexUrl);
    if (!bundle) return;
    bundle.packs.forEach((pack) => commitLanguagePack(pack.validated));
    await persistVerifiedLanguageBundle(indexUrl, bundle);
  } catch (error) {
    if (!restoredFromCache) throw error;
  }
}
