import type { LocalizedText, MenuItem, OrderLine, StationId } from './types';

export function localize(content: LocalizedText, locale: string): string {
  const baseLocale = locale.split('-')[0];
  return content[locale] ?? content[baseLocale] ?? content.en ?? Object.values(content)[0] ?? '';
}

export const menuCatalog: MenuItem[] = [
  {
    id: 'butter-chicken-bowl',
    name: { en: 'Butter chicken bowl', hi: 'बटर चिकन बाउल', bn: 'বাটার চিকেন বোল' },
    description: {
      en: 'Charred chicken, makhani gravy and cumin rice',
      hi: 'भुना चिकन, मखनी ग्रेवी और जीरा चावल',
      bn: 'ঝলসানো চিকেন, মাখনি গ্রেভি ও জিরা ভাত',
    },
    category: 'mains',
    station: 'hot',
    price: { minorUnits: 32900, currency: 'INR' },
    prepMinutes: 14,
    vegetarian: false,
    available: true,
    accent: '#d96a34',
    glyph: 'BC',
  },
  {
    id: 'paneer-tikka-bowl',
    name: { en: 'Paneer tikka bowl', hi: 'पनीर टिक्का बाउल', bn: 'পনির টিক্কা বোল' },
    description: {
      en: 'Tandoori paneer, mint slaw and cumin rice',
      hi: 'तंदूरी पनीर, पुदीना स्लॉ और जीरा चावल',
      bn: 'তন্দুরি পনির, পুদিনা স্ল ও জিরা ভাত',
    },
    category: 'mains',
    station: 'hot',
    price: { minorUnits: 28900, currency: 'INR' },
    prepMinutes: 12,
    vegetarian: true,
    available: true,
    accent: '#c88b2b',
    glyph: 'PT',
  },
  {
    id: 'biryani',
    name: { en: 'Dum chicken biryani', hi: 'दम चिकन बिरयानी', bn: 'দম চিকেন বিরিয়ানি' },
    description: {
      en: 'Saffron rice, slow-cooked chicken and raita',
      hi: 'केसर चावल, धीमी आँच का चिकन और रायता',
      bn: 'জাফরানি ভাত, ধীরে রান্না চিকেন ও রায়তা',
    },
    category: 'mains',
    station: 'hot',
    price: { minorUnits: 34900, currency: 'INR' },
    prepMinutes: 16,
    vegetarian: false,
    available: true,
    accent: '#9d5035',
    glyph: 'DB',
  },
  {
    id: 'kathi-roll',
    name: { en: 'Kolkata kathi roll', hi: 'कोलकाता काठी रोल', bn: 'কলকাতা কাঠি রোল' },
    description: {
      en: 'Flaky paratha, spiced filling and onion relish',
      hi: 'परतदार पराठा, मसालेदार भरावन और प्याज़',
      bn: 'পরোটা, মশলাদার পুর ও পেঁয়াজের রেলিশ',
    },
    category: 'snacks',
    station: 'hot',
    price: { minorUnits: 21900, currency: 'INR' },
    prepMinutes: 9,
    vegetarian: false,
    available: true,
    accent: '#547a42',
    glyph: 'KR',
  },
  {
    id: 'dahi-kebab',
    name: { en: 'Dahi kebab', hi: 'दही कबाब', bn: 'দই কাবাব' },
    description: {
      en: 'Crisp yoghurt patties with tamarind chutney',
      hi: 'कुरकुरे दही कबाब और इमली की चटनी',
      bn: 'মুচমুচে দই কাবাব ও তেঁতুলের চাটনি',
    },
    category: 'snacks',
    station: 'cold',
    price: { minorUnits: 24900, currency: 'INR' },
    prepMinutes: 8,
    vegetarian: true,
    available: true,
    accent: '#6e7452',
    glyph: 'DK',
  },
  {
    id: 'masala-fries',
    name: { en: 'Masala fries', hi: 'मसाला फ्राइज़', bn: 'মশলা ফ্রাই' },
    description: {
      en: 'Crisp fries, house masala and lime',
      hi: 'कुरकुरे फ्राइज़, हाउस मसाला और नींबू',
      bn: 'মুচমুচে ফ্রাই, নিজস্ব মশলা ও লেবু',
    },
    category: 'snacks',
    station: 'hot',
    price: { minorUnits: 15900, currency: 'INR' },
    prepMinutes: 7,
    vegetarian: true,
    available: true,
    accent: '#dd9d2e',
    glyph: 'MF',
  },
  {
    id: 'mango-lassi',
    name: { en: 'Mango lassi', hi: 'मैंगो लस्सी', bn: 'আমের লস্যি' },
    description: {
      en: 'Mango, cultured yoghurt and cardamom',
      hi: 'आम, दही और इलायची',
      bn: 'আম, টক দই ও এলাচ',
    },
    category: 'drinks',
    station: 'beverage',
    price: { minorUnits: 13900, currency: 'INR' },
    prepMinutes: 4,
    vegetarian: true,
    available: true,
    accent: '#efad37',
    glyph: 'ML',
  },
  {
    id: 'nimbu-soda',
    name: { en: 'Nimbu soda', hi: 'नींबू सोडा', bn: 'লেবু সোডা' },
    description: {
      en: 'Fresh lime, soda and black salt',
      hi: 'ताज़ा नींबू, सोडा और काला नमक',
      bn: 'তাজা লেবু, সোডা ও বিট লবণ',
    },
    category: 'drinks',
    station: 'beverage',
    price: { minorUnits: 9900, currency: 'INR' },
    prepMinutes: 3,
    vegetarian: true,
    available: true,
    accent: '#63a65d',
    glyph: 'NS',
  },
];

export const catalogById = new Map(menuCatalog.map((item) => [item.id, item]));

export const stationOrder = ['hot', 'cold', 'beverage'] as const satisfies readonly StationId[];
export type DefaultStationId = (typeof stationOrder)[number];

export function isDefaultStationId(stationId: StationId): stationId is DefaultStationId {
  return (stationOrder as readonly StationId[]).includes(stationId);
}

export function labelForStation(
  stationId: StationId,
  translateDefault: (stationId: DefaultStationId) => string,
): string {
  if (isDefaultStationId(stationId)) return translateDefault(stationId);
  const readable = stationId
    .trim()
    .replace(/([\p{Ll}\d])(\p{Lu})/gu, '$1 $2')
    .replace(/[-_.]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
  if (!readable) return stationId;
  return readable.replace(/^./u, (first) => first.toLocaleUpperCase());
}

export function stationIdsInDisplayOrder(
  discovered: Iterable<StationId>,
  includeDefaults = true,
): StationId[] {
  const unique = new Set(discovered);
  const defaults = stationOrder.filter((stationId) => includeDefaults || unique.has(stationId));
  const custom = [...unique]
    .filter((stationId) => !isDefaultStationId(stationId))
    .sort((left, right) => left.localeCompare(right));
  return [...defaults, ...custom];
}

export function stationForLine(line: Pick<OrderLine, 'menuItemId' | 'stationId'>): StationId {
  return line.stationId ?? catalogById.get(line.menuItemId)?.station ?? 'hot';
}

export function nameForLine(
  line: Pick<OrderLine, 'menuItemId' | 'name'>,
  locale: string,
): string {
  const item = catalogById.get(line.menuItemId);
  return item ? localize(item.name, locale) : line.name ?? line.menuItemId;
}
