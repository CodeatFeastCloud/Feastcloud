export type ImportedAddonBinding = {
  name: string;
  selection: 'single' | 'multiple';
  minimum: number;
  maximum: number;
};

export type ImportedVariation = { groupName: string; name: string; priceMinor: number };

export type ImportedMenuItem = {
  sourceRow: number;
  name: string;
  onlineName: string;
  description: string;
  code: string;
  category: string;
  onlineCategory: string;
  priceMinor: number;
  dietaryLabel: string;
  rank: number;
  addonBindings: ImportedAddonBinding[];
  variations: ImportedVariation[];
  issues: string[];
};

export type ImportedAddonGroup = {
  sourceId: string;
  name: string;
  onlineName: string;
  minimum: number;
  maximum: number;
  selection: 'single' | 'multiple';
  showInOnline: boolean;
  showInPos: boolean;
  options: Array<{ name: string; priceMinor: number; dietaryLabel: string; rank: number; active: boolean }>;
};

export type MenuImportReview = {
  items: ImportedMenuItem[];
  addonGroups: ImportedAddonGroup[];
  categories: Array<{ name: string; itemCount: number }>;
  variationCount: number;
  unresolvedAddonBindings: number;
  warnings: MenuImportWarning[];
};

export type MenuImportWarning =
  | { code: 'no_items' }
  | { code: 'no_addon_groups' }
  | { code: 'unresolved_addon_bindings'; count: number }
  | { code: 'missing_item_codes' };

/** The UI-facing, deliberately lossy view of a source export. Core retains
 * this canonical projection as the active menu; recipe and station links stay
 * optional and can be completed later in Menu Studio. */
export type MenuImportPreview = {
  items: Array<{
    sourceLine: number;
    name: string;
    onlineName: string;
    description: string;
    code: string;
    category: string;
    onlineCategory: string;
    priceMinor: number;
    dietaryLabel: string;
    rank: number;
    stationId?: string;
    prepMinutes?: number;
    addOnGroupNames: string[];
    addonBindings: ImportedAddonBinding[];
    variations: ImportedVariation[];
  }>;
  addonGroups: Array<{
    sourceId: string;
    name: string;
    onlineName: string;
    selectionMin: number;
    selectionMax: number;
    selection: 'single' | 'multiple';
    showInOnline: boolean;
    showInPos: boolean;
    options: Array<{ name: string; priceMinor: number; dietaryLabel: string; rank: number; active: boolean }>;
  }>;
  categories: string[];
  variationCount: number;
  warnings: MenuImportWarning[];
};

type CsvTable = { headers: string[]; rows: string[][] };

const text = (value: string | undefined) => (value ?? '').replace(/^\uFEFF/, '').trim();
const key = (value: string) => text(value).toLocaleLowerCase();
const majorToMinor = (value: string | undefined) => {
  const parsed = Number(text(value).replace(/[^0-9.-]/g, ''));
  return Number.isFinite(parsed) && parsed >= 0 ? Math.round(parsed * 100) : 0;
};
const whole = (value: string | undefined) => {
  const parsed = Number(text(value));
  return Number.isFinite(parsed) && parsed >= 0 ? Math.round(parsed) : 0;
};
const selection = (value: string | undefined): 'single' | 'multiple' => text(value).toLocaleUpperCase() === 'M' ? 'multiple' : 'single';
const enabled = (value: string | undefined) => !['no', 'false', '0', 'inactive'].includes(key(value ?? ''));

/** RFC 4180-style CSV parser. It deliberately keeps repeated headers because Petpooja uses them for repeated add-on columns. */
export function parseMenuCsv(source: string): CsvTable {
  const rows: string[][] = [];
  let row: string[] = [];
  let value = '';
  let quoted = false;
  for (let index = 0; index < source.length; index += 1) {
    const character = source[index];
    if (character === '"') {
      if (quoted && source[index + 1] === '"') { value += '"'; index += 1; } else quoted = !quoted;
    } else if (character === ',' && !quoted) {
      row.push(value); value = '';
    } else if ((character === '\n' || character === '\r') && !quoted) {
      if (character === '\r' && source[index + 1] === '\n') index += 1;
      row.push(value); value = '';
      if (row.some((cell) => text(cell) !== '')) rows.push(row);
      row = [];
    } else value += character;
  }
  if (value !== '' || row.length > 0) { row.push(value); if (row.some((cell) => text(cell) !== '')) rows.push(row); }
  if (!rows.length) return { headers: [], rows: [] };
  // Some live add-on exports begin with a harmless blank `,` row. Find the
  // real schema rather than treating that marker as the header row.
  const schemaIndex = rows.findIndex((candidate) => candidate.some((cell) => {
    const header = key(cell);
    return header === 'name' || header === 'addon_group_name';
  }));
  const headerIndex = schemaIndex >= 0 ? schemaIndex : 0;
  return { headers: rows[headerIndex].map(text), rows: rows.slice(headerIndex + 1) };
}

const indexes = (headers: string[], header: string) => headers.flatMap((value, index) => key(value) === key(header) ? [index] : []);
const firstIndex = (headers: string[], header: string) => indexes(headers, header)[0] ?? -1;
const firstIndexOf = (headers: string[], alternatives: string[]) => alternatives.map((header) => firstIndex(headers, header)).find((index) => index >= 0) ?? -1;
const at = (row: string[], index: number) => index < 0 ? '' : text(row[index]);

export function reviewPetpoojaMenu(itemsCsv: string, addonsCsv: string): MenuImportReview {
  const itemTable = parseMenuCsv(itemsCsv);
  const addonTable = parseMenuCsv(addonsCsv);
  const addonGroups = parseAddonGroups(addonTable);
  const knownAddonGroups = new Set(addonGroups.map((group) => key(group.name)));
  const name = firstIndex(itemTable.headers, 'Name');
  const onlineName = firstIndex(itemTable.headers, 'Online_Name');
  const description = firstIndex(itemTable.headers, 'Description');
  const code = firstIndex(itemTable.headers, 'Short_Code');
  const parentCategory = firstIndex(itemTable.headers, 'Parent_Category');
  const category = firstIndex(itemTable.headers, 'Category');
  const onlineCategory = firstIndex(itemTable.headers, 'Category_online_display');
  const price = firstIndex(itemTable.headers, 'Price');
  const attributes = firstIndex(itemTable.headers, 'Attributes');
  const rank = firstIndex(itemTable.headers, 'Rank');
  const addonNames = indexes(itemTable.headers, 'Addon_Group_Name');
  const variationGroups = indexes(itemTable.headers, 'Variation_group_name');
  const variations = indexes(itemTable.headers, 'Variation');
  const items = itemTable.rows.flatMap((row, rowIndex) => {
    const itemName = at(row, name);
    if (!itemName) return [];
    const addonBindings = addonNames.flatMap((index) => {
      const addonName = at(row, index);
      return addonName ? [{ name: addonName, selection: selection(row[index + 1]), minimum: whole(row[index + 2]), maximum: whole(row[index + 3]) }] : [];
    }).filter((binding, index, values) => values.findIndex((value) => key(value.name) === key(binding.name)) === index);
    const itemVariations = variations.flatMap((index) => {
      const variationName = at(row, index);
      if (!variationName) return [];
      const groupIndex = [...variationGroups].reverse().find((candidate) => candidate < index);
      const groupName = at(row, groupIndex ?? -1);
      return groupName ? [{ groupName, name: variationName, priceMinor: majorToMinor(row[index + 1]) }] : [];
    });
    const issues: string[] = [];
    if (!at(row, code)) issues.push('Missing item code');
    if (!at(row, category) && !at(row, parentCategory)) issues.push('Missing category');
    if (majorToMinor(row[price]) === 0 && !itemVariations.length) issues.push('No base price or price variation');
    addonBindings.forEach((binding) => { if (!knownAddonGroups.has(key(binding.name))) issues.push(`Add-on group not found: ${binding.name}`); });
    return [{
      sourceRow: rowIndex + 2,
      name: itemName,
      onlineName: at(row, onlineName) || itemName,
      description: at(row, description),
      code: at(row, code),
      category: at(row, category) || at(row, parentCategory),
      onlineCategory: at(row, onlineCategory) || at(row, category) || at(row, parentCategory),
      priceMinor: majorToMinor(row[price]),
      dietaryLabel: at(row, attributes),
      rank: whole(row[rank]),
      addonBindings,
      variations: itemVariations,
      issues,
    }];
  });
  const categoryCounts = new Map<string, number>();
  items.forEach((item) => categoryCounts.set(item.category || 'Uncategorised', (categoryCounts.get(item.category || 'Uncategorised') ?? 0) + 1));
  const unresolvedAddonBindings = items.flatMap((item) => item.addonBindings).filter((binding) => !knownAddonGroups.has(key(binding.name))).length;
  const warnings: MenuImportWarning[] = [];
  if (!items.length) warnings.push({ code: 'no_items' });
  if (!addonGroups.length) warnings.push({ code: 'no_addon_groups' });
  if (unresolvedAddonBindings) warnings.push({ code: 'unresolved_addon_bindings', count: unresolvedAddonBindings });
  if (items.some((item) => item.issues.some((issue) => issue === 'Missing item code'))) warnings.push({ code: 'missing_item_codes' });
  return { items, addonGroups, categories: [...categoryCounts].map(([categoryName, itemCount]) => ({ name: categoryName, itemCount })).sort((a, b) => a.name.localeCompare(b.name)), variationCount: items.reduce((sum, item) => sum + item.variations.length, 0), unresolvedAddonBindings, warnings };
}

/** Normalizes a simple item CSV into the same reviewed shape as a Petpooja
 * export. This keeps imports from other POS systems on the same safe path,
 * while retaining Petpooja's richer repeated add-on and variation columns. */
function reviewGenericMenu(itemsCsv: string, addonsCsv: string): MenuImportReview {
  const itemTable = parseMenuCsv(itemsCsv);
  const addonGroups = parseAddonGroups(parseMenuCsv(addonsCsv));
  const name = firstIndexOf(itemTable.headers, ['Name', 'Item Name', 'Item_Name', 'Product Name', 'Product_Name', 'Item']);
  const onlineName = firstIndexOf(itemTable.headers, ['Online Name', 'Online_Name', 'Display Name', 'Display_Name']);
  const description = firstIndexOf(itemTable.headers, ['Description', 'Item Description', 'Item_Description']);
  const code = firstIndexOf(itemTable.headers, ['Code', 'Item Code', 'Item_Code', 'SKU', 'Short Code', 'Short_Code']);
  const category = firstIndexOf(itemTable.headers, ['Category', 'Category Name', 'Category_Name', 'Department']);
  const price = firstIndexOf(itemTable.headers, ['Price', 'Selling Price', 'Selling_Price', 'Amount']);
  const dietaryLabel = firstIndexOf(itemTable.headers, ['Attributes', 'Dietary', 'Dietary Label', 'Dietary_Label']);
  const rank = firstIndexOf(itemTable.headers, ['Rank', 'Sort Order', 'Sort_Order', 'Position']);
  const items = itemTable.rows.flatMap((row, rowIndex) => {
    const itemName = at(row, name);
    if (!itemName) return [];
    const issues: string[] = [];
    if (!at(row, code)) issues.push('Missing item code');
    if (!at(row, category)) issues.push('Missing category');
    return [{
      sourceRow: rowIndex + 2, name: itemName, onlineName: at(row, onlineName) || itemName,
      description: at(row, description), code: at(row, code), category: at(row, category), onlineCategory: at(row, category),
      priceMinor: majorToMinor(row[price]), dietaryLabel: at(row, dietaryLabel), rank: whole(row[rank]), addonBindings: [], variations: [], issues,
    }];
  });
  const categoryCounts = new Map<string, number>();
  items.forEach((item) => categoryCounts.set(item.category || 'Uncategorised', (categoryCounts.get(item.category || 'Uncategorised') ?? 0) + 1));
  const warnings: MenuImportWarning[] = [];
  if (!items.length) warnings.push({ code: 'no_items' });
  if (!addonGroups.length) warnings.push({ code: 'no_addon_groups' });
  if (items.some((item) => item.issues.includes('Missing item code'))) warnings.push({ code: 'missing_item_codes' });
  return { items, addonGroups, categories: [...categoryCounts].map(([name, itemCount]) => ({ name, itemCount })).sort((left, right) => left.name.localeCompare(right.name)), variationCount: 0, unresolvedAddonBindings: 0, warnings };
}

/**
 * Converts a restaurant export into a local review model. The caller applies
 * the canonical result atomically; recipe and station mapping remain optional.
 */
export function previewRestaurantMenu(itemsCsv: string, addonsCsv = ''): MenuImportPreview {
  const headers = parseMenuCsv(itemsCsv).headers;
  const isPetpooja = firstIndex(headers, 'Short_Code') >= 0 || firstIndex(headers, 'Addon_Group_Name') >= 0 || firstIndex(headers, 'Variation_group_name') >= 0;
  const review = isPetpooja ? reviewPetpoojaMenu(itemsCsv, addonsCsv) : reviewGenericMenu(itemsCsv, addonsCsv);
  return {
    items: review.items.map((item) => ({
      sourceLine: item.sourceRow,
      name: item.name,
      onlineName: item.onlineName,
      description: item.description,
      code: item.code,
      category: item.onlineCategory || item.category,
      onlineCategory: item.onlineCategory,
      priceMinor: item.priceMinor,
      dietaryLabel: item.dietaryLabel,
      rank: item.rank,
      addOnGroupNames: item.addonBindings.map((binding) => binding.name),
      addonBindings: item.addonBindings,
      variations: item.variations,
    })),
    addonGroups: review.addonGroups.map((group) => ({
      sourceId: group.sourceId,
      // Keep the canonical export name for item-to-group bindings. The online
      // name is presentation-only and may differ (for example "Add a bread"
      // versus "Add bread"), which previously broke multi-group mappings.
      name: group.name,
      onlineName: group.onlineName,
      selectionMin: group.minimum,
      selectionMax: group.maximum,
      selection: group.selection,
      showInOnline: group.showInOnline,
      showInPos: group.showInPos,
      options: group.options.map((option) => ({ name: option.name, priceMinor: option.priceMinor, dietaryLabel: option.dietaryLabel, rank: option.rank, active: option.active })),
    })),
    categories: review.categories.map((category) => category.name),
    variationCount: review.variationCount,
    warnings: review.warnings,
  };
}

function parseAddonGroups(table: CsvTable): ImportedAddonGroup[] {
  const id = firstIndex(table.headers, 'Addon_Group_Id');
  const name = firstIndex(table.headers, 'Addon_Group_Name');
  const onlineName = firstIndex(table.headers, 'Addon_Group_online_display');
  const minimum = firstIndex(table.headers, 'Addon_Min');
  const maximum = firstIndex(table.headers, 'Addon_Max');
  const selectionIndex = firstIndex(table.headers, 'Addon_Item_Selection');
  const showInOnline = firstIndex(table.headers, 'Show_In_Online');
  const showInPos = firstIndex(table.headers, 'Show_In_Pos');
  const itemName = firstIndex(table.headers, 'Addon_Item_Name');
  const itemPrice = firstIndex(table.headers, 'Addon_Item_Price');
  const itemStatus = firstIndex(table.headers, 'Addon_item_status');
  const attribute = firstIndex(table.headers, 'Attribute');
  const rank = firstIndex(table.headers, 'Rank');
  const groups = new Map<string, ImportedAddonGroup>();
  table.rows.forEach((row) => {
    const groupName = at(row, name);
    if (!groupName) return;
    const groupKey = key(at(row, id) || groupName);
    const existing = groups.get(groupKey) ?? {
      sourceId: at(row, id) || groupName,
      name: groupName,
      onlineName: at(row, onlineName) || groupName,
      minimum: whole(row[minimum]),
      maximum: whole(row[maximum]),
      selection: selection(row[selectionIndex]),
      showInOnline: enabled(row[showInOnline]),
      showInPos: enabled(row[showInPos]),
      options: [],
    };
    const optionName = at(row, itemName);
    if (optionName) existing.options.push({ name: optionName, priceMinor: majorToMinor(row[itemPrice]), dietaryLabel: at(row, attribute), rank: whole(row[rank]), active: enabled(row[itemStatus]) });
    groups.set(groupKey, existing);
  });
  return [...groups.values()].map((group) => ({ ...group, options: group.options.sort((a, b) => a.rank - b.rank || a.name.localeCompare(b.name)) })).sort((a, b) => a.name.localeCompare(b.name));
}
