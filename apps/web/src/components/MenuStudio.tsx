import { type ChangeEvent, type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { KitchenSnapshot } from '../domain/types';
import type { MessageKey } from '../i18n/messages';
import { fetchRecipes, type RecipeSummary } from '../domain/coreInventory';
import { fetchOrganizationControlData, organizationApiBase, type Station } from '../domain/coreOrganization';
import {
  createMenuItem,
  createMenuStudio,
  stageMenuImport,
  fetchMenuImportDrafts,
  fetchMenuItems,
  fetchMenuStudios,
  publishMenuStudioVersion,
  type MenuModifierGroup,
  type MenuImportDraft,
  type MenuStudio,
  type MenuStudioCategory,
  type MenuStudioItem,
} from '../domain/coreCommerce';
import { previewRestaurantMenu, type MenuImportPreview, type MenuImportWarning } from '../domain/menuImport';
import { createUuidV7 } from '../domain/kitchen';
import './menu-studio.css';

const api = organizationApiBase((import.meta.env.VITE_CORE_URL as string | undefined)?.trim());
const currency = 'INR';
type Tab = 'items' | 'categories' | 'addons' | 'channels' | 'import';
type Translate = (key: MessageKey, replacements?: Record<string, string | number>) => string;
type ImportedPreviewItem = MenuImportPreview['items'][number];

const money = (minor: number, valueCurrency = currency) => new Intl.NumberFormat(undefined, {
  style: 'currency', currency: valueCurrency, maximumFractionDigits: 2,
}).format(minor / 100);

const importCode = (value: string) => value
  .trim()
  .replace(/[^A-Za-z0-9._-]+/g, '-')
  .replace(/^[^A-Za-z0-9]+|[^A-Za-z0-9._-]+$/g, '')
  .slice(0, 64);

function normalizeStudio(studio: MenuStudio): MenuStudio {
  if (!studio.current) return studio;
  return {
    ...studio,
    current: {
      ...studio.current,
      categories: studio.current.categories ?? [],
      modifiers: studio.current.modifiers ?? [],
      items: (studio.current.items ?? []).map((item) => ({ ...item, modifierGroupIds: item.modifierGroupIds ?? [] })),
      publications: studio.current.publications ?? [],
    },
  };
}

export function MenuStudio({ snapshot, t }: { snapshot: KitchenSnapshot; t: Translate }) {
  const [studio, setStudio] = useState<MenuStudio>();
  const [studios, setStudios] = useState<MenuStudio[]>([]);
  const [recipes, setRecipes] = useState<RecipeSummary[]>([]);
  const [stations, setStations] = useState<Station[]>([]);
  const [library, setLibrary] = useState<Awaited<ReturnType<typeof fetchMenuItems>>>([]);
  const [tab, setTab] = useState<Tab>('items');
  const [categoryFilter, setCategoryFilter] = useState('all');
  const [query, setQuery] = useState('');
  const [selectedItemId, setSelectedItemId] = useState<string>();
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [itemExport, setItemExport] = useState('');
  const [addonExport, setAddonExport] = useState('');
  const [itemExportName, setItemExportName] = useState('');
  const [addonExportName, setAddonExportName] = useState('');
  const [importPreview, setImportPreview] = useState<MenuImportPreview>();
  const [stagedImportName, setStagedImportName] = useState('');
  const [importDrafts, setImportDrafts] = useState<MenuImportDraft[]>([]);
  const [selectedImportedItemLine, setSelectedImportedItemLine] = useState<number>();
  const actionSequence = useRef(0);

  const load = useCallback(async () => {
    if (!api) return;
    try {
      const [studios, menuItems, recipeRows, control, drafts] = await Promise.all([
        fetchMenuStudios(api, snapshot.organizationId, snapshot.outletId),
        fetchMenuItems(api, snapshot.organizationId, snapshot.outletId),
        fetchRecipes(api, snapshot.organizationId),
        fetchOrganizationControlData(api, snapshot.organizationId),
        fetchMenuImportDrafts(api, snapshot.organizationId, snapshot.outletId),
      ]);
      const normalizedStudios = studios.map(normalizeStudio);
      setStudios(normalizedStudios);
      setStudio((previous) => normalizedStudios.find((value) => value.id === previous?.id) ?? normalizedStudios[0]);
      setLibrary(menuItems);
      setRecipes(recipeRows);
      setStations(control.stations.filter((value) => value.outletId === snapshot.outletId && value.active));
      setImportDrafts(drafts);
      const latestImport = drafts.find((draft) => ['staged', 'mapping', 'applied'].includes(draft.status) && draft.draft?.items?.length);
      if (latestImport) {
        setImportPreview(latestImport.draft);
        setItemExportName(latestImport.itemFileName);
        setAddonExportName(latestImport.addonFileName ?? '');
        setStagedImportName(latestImport.name);
      }
      setError('');
    } catch {
      setError(t('menu.unavailable'));
    }
  }, [snapshot.organizationId, snapshot.outletId, t]);

  useEffect(() => { void load(); }, [load]);
  useEffect(() => { if (stagedImportName) setError(''); }, [stagedImportName]);

  const current = studio?.current;
  const categoryById = useMemo(() => new Map((current?.categories ?? []).map((value) => [value.id, value])), [current]);
  const itemById = useMemo(() => new Map(library.map((value) => [value.id, value])), [library]);
  const visibleItems = useMemo(() => (current?.items ?? []).filter((item) => {
    const matchesCategory = categoryFilter === 'all' || item.categoryId === categoryFilter;
    const haystack = `${item.displayName} ${item.description ?? ''}`.toLocaleLowerCase();
    return matchesCategory && haystack.includes(query.trim().toLocaleLowerCase());
  }), [categoryFilter, current, query]);
  const selectedItem = current?.items.find((item) => item.menuItemId === selectedItemId);
  const matchedImportCount = useMemo(() => new Set(importPreview?.items.map((item) => importCode(item.code)).filter(Boolean) ?? []).size, [importPreview]);
  const importedItems = importPreview?.items ?? [];
  const importedCategories = useMemo(() => [...new Set(importedItems.map((item) => item.category || 'Imported'))].sort((left, right) => left.localeCompare(right)), [importedItems]);
  const visibleImportedItems = useMemo(() => importedItems.filter((item) => {
    const matchesCategory = categoryFilter === 'all' || (item.category || 'Imported') === categoryFilter;
    const haystack = `${item.onlineName} ${item.name} ${item.description} ${item.code}`.toLocaleLowerCase();
    return matchesCategory && haystack.includes(query.trim().toLocaleLowerCase());
  }), [categoryFilter, importedItems, query]);
  const selectedImportedItem = importedItems.find((item) => item.sourceLine === selectedImportedItemLine);
  const showingImportedMenu = importedItems.length > 0;

  const run = async (name: string, action: () => Promise<unknown>, successMessage: MessageKey = 'menu.saved'): Promise<boolean> => {
    const sequence = ++actionSequence.current;
    setBusy(name);
    setError('');
    try {
      await action();
      await load();
      if (sequence === actionSequence.current) {
        setError('');
        setNotice(t(successMessage));
      }
      return true;
    } catch {
      if (sequence === actionSequence.current) setError(t('menu.failed'));
      return false;
    } finally {
      if (sequence === actionSequence.current) setBusy('');
    }
  };

  const saveItems = (items: MenuStudioItem[], action = 'items') => {
    if (!api || !studio) return;
    void run(action, () => publishMenuStudioVersion(api, snapshot.organizationId, snapshot.outletId, studio, { items }));
  };

  const saveImportedMenu = (preview: MenuImportPreview, itemsFileName: string, addonsFileName: string, successMessage: MessageKey) => {
    if (!api || !itemsFileName) return;
    void run('stage-import', async () => {
      const staged = await stageMenuImport(api, snapshot.organizationId, snapshot.outletId, itemsFileName, addonsFileName, preview);
      setStagedImportName(staged.name);
    }, successMessage);
  };

  const addCategory = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!api || !studio || !current) return;
    const form = new FormData(event.currentTarget);
    const name = String(form.get('name') ?? '').trim();
    if (!name) return;
    const categories = [...current.categories, { id: createUuidV7(), name, sortOrder: current.categories.length, active: true }];
    void run('category', () => publishMenuStudioVersion(api, snapshot.organizationId, snapshot.outletId, studio, { categories }))
      .then((saved) => { if (saved) event.currentTarget.reset(); });
  };

  const addAddon = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!api || !studio || !current) return;
    const form = new FormData(event.currentTarget);
    const name = String(form.get('name') ?? '').trim();
    const option = String(form.get('option') ?? '').trim();
    const min = Math.max(0, Number(form.get('min') ?? 0));
    const max = Math.max(min || 1, Number(form.get('max') ?? 1));
    const amount = Math.round(Number(form.get('amount') ?? 0) * 100);
    if (!name || !option || !Number.isFinite(amount) || amount < 0) return;
    const modifiers: MenuModifierGroup[] = [...current.modifiers, {
      id: createUuidV7(), name, selectionMin: min, selectionMax: max, required: min > 0, sortOrder: current.modifiers.length,
      options: [{ id: createUuidV7(), name: option, priceDeltaMinor: amount, active: true, sortOrder: 0 }],
    }];
    void run('addon', () => publishMenuStudioVersion(api, snapshot.organizationId, snapshot.outletId, studio, { modifiers }))
      .then((saved) => { if (saved) event.currentTarget.reset(); });
  };

  const addItem = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!api) return;
    const form = new FormData(event.currentTarget);
    const name = String(form.get('name') ?? '').trim();
    const code = String(form.get('code') ?? '').trim();
    const recipeId = String(form.get('recipeId') ?? '');
    const stationId = String(form.get('stationId') ?? '');
    const priceMinor = Math.round(Number(form.get('price') ?? 0) * 100);
    const categoryId = String(form.get('categoryId') ?? '');
    if (!name || !code || !Number.isFinite(priceMinor) || priceMinor < 0) return;
    void run('item', async () => {
      const created = await createMenuItem(api, snapshot.organizationId, snapshot.outletId, { recipeId: recipeId || undefined, name, code, priceMinor, currency, stationId: stationId || undefined });
      const menuItem: MenuStudioItem = {
        menuItemId: created.id, categoryId, displayName: name, description: '', sortOrder: current?.items.length ?? 0,
        active: true, modifierGroupIds: [], priceId: createUuidV7(), priceMinor, currency,
      };
      if (studio && current) {
        await publishMenuStudioVersion(api, snapshot.organizationId, snapshot.outletId, studio, { items: [...current.items, menuItem] });
      } else {
        const firstCategory: MenuStudioCategory = {
          id: createUuidV7(), name: String(form.get('categoryName') ?? '').trim() || t('menu.mainMenu'), sortOrder: 0, active: true,
        };
        await createMenuStudio(api, snapshot.organizationId, snapshot.outletId, t('menu.baseMenu'), firstCategory, { ...menuItem, categoryId: firstCategory.id });
      }
    }).then((saved) => { if (saved) event.currentTarget.reset(); });
  };

  const updateItem = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!current || !selectedItem) return;
    const form = new FormData(event.currentTarget);
    const priceMinor = Math.round(Number(form.get('price') ?? 0) * 100);
    const modifierGroupIds = current.modifiers.filter((group) => form.get(`modifier-${group.id}`) === 'on').map((group) => group.id);
    if (!Number.isFinite(priceMinor) || priceMinor < 0) return;
    saveItems(current.items.map((item) => item.menuItemId === selectedItem.menuItemId ? {
      ...item,
      displayName: String(form.get('displayName') ?? '').trim() || item.displayName,
      description: String(form.get('description') ?? '').trim(),
      categoryId: String(form.get('categoryId') ?? ''),
      priceMinor,
      modifierGroupIds,
    } : item), 'item-edit');
  };

  const readImportFile = async (event: ChangeEvent<HTMLInputElement>, kind: 'items' | 'addons') => {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      const text = await file.text();
      const nextItems = kind === 'items' ? text : itemExport;
      const nextAddons = kind === 'addons' ? text : addonExport;
      if (!nextItems) {
        setAddonExport(nextAddons);
        setNotice(t('menu.importNeedsItems'));
        return;
      }
      const nextItemExportName = kind === 'items' ? file.name : itemExportName;
      const nextAddonExportName = kind === 'addons' ? file.name : addonExportName;
      const preview = previewRestaurantMenu(nextItems, nextAddons);
      setItemExport(nextItems);
      setAddonExport(nextAddons);
      setItemExportName(nextItemExportName);
      setAddonExportName(nextAddonExportName);
      setImportPreview(preview);
      setStagedImportName('');
      setError('');
      setSelectedImportedItemLine(undefined);
      setTab('items');
      saveImportedMenu(preview, nextItemExportName, nextAddonExportName, 'menu.importApplied');
    } catch {
      setError(t('menu.importInvalid'));
    }
  };

  const updateImportedItem = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!importPreview || !selectedImportedItem || !itemExportName) return;
    const form = new FormData(event.currentTarget);
    const priceMinor = Math.round(Number(form.get('price') ?? 0) * 100);
    if (!Number.isFinite(priceMinor) || priceMinor < 0) return;
    const name = String(form.get('name') ?? '').trim();
    const onlineName = String(form.get('onlineName') ?? '').trim();
    const category = String(form.get('category') ?? '').trim() || 'Imported';
    const assignedAddons = new Set(form.getAll('addonGroup').map((value) => String(value)));
    const addonGroups = importPreview.addonGroups.filter((group) => assignedAddons.has(group.sourceId));
    const updatedItem: ImportedPreviewItem = {
      ...selectedImportedItem,
      name: name || selectedImportedItem.name,
      onlineName: onlineName || name || selectedImportedItem.onlineName,
      code: String(form.get('code') ?? '').trim(),
      category,
      onlineCategory: category,
      description: String(form.get('description') ?? '').trim(),
      dietaryLabel: String(form.get('dietaryLabel') ?? '').trim(),
      stationId: String(form.get('stationId') ?? '').trim() || undefined,
      prepMinutes: Math.max(1, Math.round(Number(form.get('prepMinutes') ?? 12))) || 12,
      priceMinor,
      addOnGroupNames: addonGroups.map((group) => group.sourceId),
      addonBindings: addonGroups.map((group) => selectedImportedItem.addonBindings.find((binding) => [group.sourceId, group.name, group.onlineName].some((candidate) => candidate.trim().toLocaleLowerCase() === binding.name.trim().toLocaleLowerCase())) ?? {
        name: group.sourceId, selection: group.selection, minimum: group.selectionMin, maximum: group.selectionMax,
      }),
    };
    const changed = selectedImportedItem.name !== updatedItem.name
      || selectedImportedItem.onlineName !== updatedItem.onlineName
      || selectedImportedItem.code !== updatedItem.code
      || selectedImportedItem.category !== updatedItem.category
      || selectedImportedItem.onlineCategory !== updatedItem.onlineCategory
      || selectedImportedItem.description !== updatedItem.description
      || selectedImportedItem.dietaryLabel !== updatedItem.dietaryLabel
      || selectedImportedItem.stationId !== updatedItem.stationId
      || selectedImportedItem.prepMinutes !== updatedItem.prepMinutes
      || selectedImportedItem.priceMinor !== updatedItem.priceMinor
      || JSON.stringify(selectedImportedItem.addOnGroupNames) !== JSON.stringify(updatedItem.addOnGroupNames);
    if (!changed) {
      setSelectedImportedItemLine(undefined);
      return;
    }
    const items = importPreview.items.map((item) => item.sourceLine === selectedImportedItem.sourceLine ? updatedItem : item);
    const preview: MenuImportPreview = {
      ...importPreview,
      items,
      categories: [...new Set(items.map((item) => item.category || 'Imported'))].sort((left, right) => left.localeCompare(right)),
    };
    setImportPreview(preview);
    setSelectedImportedItemLine(undefined);
    setError('');
    saveImportedMenu(preview, itemExportName, addonExportName, 'menu.importApplied');
  };

  if (!api) return <section className="menu-page">
    <header className="menu-heading"><div><p className="eyebrow">{t('menu.eyebrow')}</p><h1>{t('menu.title')}</h1><p>{t('menu.subtitle')}</p></div><span className="menu-draft">{t('menu.draft')}</span></header>
    <div className="menu-workspace menu-import-only"><main className="menu-main"><p className="menu-message error">{t('menu.notConfigured')}</p><MenuImportDesk t={t} preview={importPreview} itemExportName={itemExportName} addonExportName={addonExportName} matchedImportCount={matchedImportCount} itemCount={importPreview?.items.length ?? 0} stagedImportName={stagedImportName} importDrafts={[]} onItemsFile={(event) => void readImportFile(event, 'items')} onAddonsFile={(event) => void readImportFile(event, 'addons')} /></main></div>
  </section>;

  return <section className="menu-page">
    <header className="menu-heading">
      <div><p className="eyebrow">{t('menu.eyebrow')}</p><h1>{t('menu.title')}</h1><p>{t('menu.subtitle')}</p></div>
      {(studio || showingImportedMenu) && <div className="menu-heading-actions">{!showingImportedMenu && studio && studios.length > 1 && <label className="menu-studio-picker"><span>{t('menu.selectMenu')}</span><select value={studio.id} onChange={(event) => setStudio(studios.find((value) => value.id === event.target.value))}>{studios.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}</select></label>}<span className="menu-live">{showingImportedMenu || studio?.status === 'published' ? t('menu.liveNow') : t('menu.draft')}</span></div>}
    </header>
    {error && <p className="menu-message error" role="alert">{error}</p>}
    {notice && <p className="menu-message success" role="status">{notice}</p>}
    <nav className="menu-tabs" aria-label={t('menu.title')}>
      {(['items', 'categories', 'addons', 'channels', 'import'] as Tab[]).map((value) => <button type="button" key={value} onClick={() => setTab(value)} className={tab === value ? 'active' : ''}>{t(`menu.${value}` as MessageKey)}</button>)}
    </nav>
    <div className={`menu-workspace ${tab === 'import' ? 'is-importing' : ''}`}>
      {tab !== 'import' && <aside className="menu-sidebar">
        <label className="menu-search"><span>{t('menu.search')}</span><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t('menu.search')} /></label>
        <button type="button" className={categoryFilter === 'all' ? 'selected' : ''} onClick={() => setCategoryFilter('all')}><span>{t('menu.allItems')}</span><small>{showingImportedMenu ? importedItems.length : current?.items.length ?? 0}</small></button>
        {showingImportedMenu
          ? importedCategories.map((category) => <button type="button" key={category} className={categoryFilter === category ? 'selected' : ''} onClick={() => setCategoryFilter(category)}><span>{category}</span><small>{importedItems.filter((item) => (item.category || 'Imported') === category).length}</small></button>)
          : (current?.categories ?? []).map((value) => <button type="button" key={value.id} className={categoryFilter === value.id ? 'selected' : ''} onClick={() => setCategoryFilter(value.id)}><span>{value.name}</span><small>{current?.items.filter((item) => item.categoryId === value.id).length ?? 0}</small></button>)}
        <div className="menu-sidebar-tip"><b>{t('menu.kitchenLinked')}</b><p>{t('menu.kitchenLinkedHelp')}</p></div>
      </aside>}
      <main className="menu-main">
        {!studio && !showingImportedMenu && tab !== 'import' && <section className="menu-first"><div><p className="eyebrow">01</p><h2>{t('menu.firstTitle')}</h2><p>{t('menu.firstHelp')}</p></div><MenuItemForm recipes={recipes} stations={stations} t={t} onSubmit={addItem} busy={busy === 'item'} first /></section>}
        {showingImportedMenu && tab === 'items' && <>
          <div className="menu-toolbar"><div><h2>{categoryFilter === 'all' ? t('menu.allItems') : categoryFilter}</h2><small>{visibleImportedItems.length} {t('menu.items').toLocaleLowerCase()} · {stagedImportName || t('menu.import')}</small></div></div>
          <div className="menu-table"><header><span>{t('menu.itemName')}</span><span>{t('menu.itemCode')}</span><span>{t('menu.price')}</span><span>{t('menu.addons')}</span><span>{t('menu.itemEditor')}</span></header>
            {visibleImportedItems.map((item) => <article key={item.sourceLine}><button type="button" className="menu-item-name" onClick={() => setSelectedImportedItemLine(item.sourceLine)}><b>{item.onlineName || item.name}</b><small>{item.category || t('menu.unassigned')}</small></button><span>{item.code || '—'}</span><strong>{money(item.priceMinor)}</strong><button type="button" className="menu-link" onClick={() => setSelectedImportedItemLine(item.sourceLine)}>{item.addOnGroupNames.length} {t('menu.attached')}</button><button type="button" className="menu-secondary" disabled={Boolean(busy)} onClick={() => setSelectedImportedItemLine(item.sourceLine)}>{t('menu.itemEditor')}</button></article>)}
            {!visibleImportedItems.length && <p className="menu-none">{t('menu.noItems')}</p>}
          </div>
        </>}
        {studio && current && !showingImportedMenu && tab === 'items' && <>
          <div className="menu-toolbar"><div><h2>{categoryFilter === 'all' ? t('menu.allItems') : categoryById.get(categoryFilter)?.name}</h2><small>{visibleItems.length} {t('menu.items').toLocaleLowerCase()} · {t('menu.version')} {current?.versionNumber ?? 0}</small></div><button type="button" className="menu-primary" onClick={() => document.getElementById('menu-item-form')?.scrollIntoView({ behavior: 'smooth' })}>{t('menu.addItem')}</button></div>
          <div className="menu-table"><header><span>{t('menu.itemName')}</span><span>{t('menu.itemCode')}</span><span>{t('menu.price')}</span><span>{t('menu.addons')}</span><span>{t('menu.available')}</span></header>
            {visibleItems.map((item) => <article key={item.menuItemId}><button type="button" className="menu-item-name" onClick={() => setSelectedItemId(item.menuItemId)}><b>{item.displayName}</b><small>{categoryById.get(item.categoryId ?? '')?.name ?? t('menu.unassigned')}</small></button><span>{itemById.get(item.menuItemId)?.code ?? '—'}</span><strong>{money(item.priceMinor, item.currency)}</strong><button type="button" className="menu-link" onClick={() => setSelectedItemId(item.menuItemId)}>{item.modifierGroupIds.length} {t('menu.attached')}</button><button type="button" className={item.active ? 'toggle on' : 'toggle'} disabled={Boolean(busy)} onClick={() => saveItems((current?.items ?? []).map((value) => value.menuItemId === item.menuItemId ? { ...value, active: !value.active } : value), `toggle:${item.menuItemId}`)}>{item.active ? t('menu.available') : t('menu.hidden')}</button></article>)}
            {!visibleItems.length && <p className="menu-none">{t('menu.noItems')}</p>}
          </div>
          <section id="menu-item-form" className="menu-form-panel"><h2>{t('menu.addItem')}</h2><p>{t('menu.itemFormHelp')}</p><MenuItemForm recipes={recipes} stations={stations} categories={current?.categories ?? []} t={t} onSubmit={addItem} busy={busy === 'item'} /></section>
        </>}
        {studio && current && tab === 'categories' && <section className="menu-form-panel"><div className="menu-toolbar"><div><h2>{t('menu.categories')}</h2><small>{current.categories.length} {t('menu.categories').toLocaleLowerCase()}</small></div></div><div className="menu-category-list">{current.categories.map((value) => <article key={value.id}><span><b>{value.name}</b><small>{current.items.filter((item) => item.categoryId === value.id).length} {t('menu.items').toLocaleLowerCase()}</small></span><button type="button" className={value.active ? 'toggle on' : 'toggle'} disabled={Boolean(busy)} onClick={() => { if (!api || !studio || !current) return; void run(`category:${value.id}`, () => publishMenuStudioVersion(api, snapshot.organizationId, snapshot.outletId, studio, { categories: current.categories.map((category) => category.id === value.id ? { ...category, active: !category.active } : category) })); }}>{value.active ? t('menu.available') : t('menu.hidden')}</button></article>)}</div><form className="menu-inline-form" onSubmit={addCategory}><input name="name" required placeholder={t('menu.categoryName')} /><button className="menu-primary" disabled={Boolean(busy)}>{t('menu.addCategory')}</button></form></section>}
        {showingImportedMenu && tab === 'categories' && <section className="menu-form-panel"><div className="menu-toolbar"><div><h2>{t('menu.categories')}</h2><small>{importedCategories.length} {t('menu.categories').toLocaleLowerCase()}</small></div></div><div className="menu-category-list">{importedCategories.map((category) => <article key={category}><span><b>{category}</b><small>{importedItems.filter((item) => (item.category || 'Imported') === category).length} {t('menu.items').toLocaleLowerCase()}</small></span><span className="menu-live">{t('menu.available')}</span></article>)}</div><p className="menu-help">Imported categories are ready to use. Edit an item to move it to another category.</p></section>}
        {studio && current && tab === 'addons' && <section className="menu-form-panel"><div className="menu-toolbar"><div><h2>{t('menu.addons')}</h2><small>{current.modifiers.length} {t('menu.addons').toLocaleLowerCase()}</small></div></div><div className="menu-addon-list">{current.modifiers.map((group) => <article key={group.id}><div><b>{group.name}</b><small>{group.selectionMin}–{group.selectionMax} · {group.options.length} {t('menu.options')}</small></div><p>{group.options.map((option) => `${option.name} · ${money(option.priceDeltaMinor)}`).join('  •  ')}</p></article>)}</div><form className="menu-addon-form" onSubmit={addAddon}><input name="name" required placeholder={t('menu.addonName')} /><input name="option" required placeholder={t('menu.addonOption')} /><input name="amount" required type="number" min="0" step="0.01" placeholder={t('menu.addonPrice')} /><input name="min" type="number" min="0" defaultValue="0" aria-label={t('menu.minimum')} /><input name="max" type="number" min="1" defaultValue="1" aria-label={t('menu.maximum')} /><button className="menu-primary" disabled={Boolean(busy)}>{t('menu.addAddon')}</button></form></section>}
        {showingImportedMenu && tab === 'addons' && <section className="menu-form-panel"><div className="menu-toolbar"><div><h2>{t('menu.addons')}</h2><small>{importPreview?.addonGroups.length ?? 0} {t('menu.addons').toLocaleLowerCase()}</small></div></div><div className="menu-addon-list">{(importPreview?.addonGroups ?? []).map((group) => <article key={group.sourceId}><div><b>{group.onlineName || group.name}</b><small>{group.selectionMin}–{group.selectionMax} · {group.options.length} {t('menu.options')}</small></div><p>{group.options.map((option) => `${option.name} · ${money(option.priceMinor)}`).join('  •  ')}</p></article>)}</div>{!importPreview?.addonGroups.length && <p className="menu-none">{t('menu.noAddons')}</p>}<p className="menu-help">These add-ons came from the import. Open an item to choose which groups it can use.</p></section>}
        {studio && current && tab === 'channels' && <section className="menu-form-panel menu-channel-panel"><h2>{t('menu.channels')}</h2><p>{t('menu.channelHelp')}</p><div className="menu-channel-grid"><ChannelCard title={t('menu.posChannel')} status={t('menu.liveNow')} detail={t('menu.posChannelHelp')} /><ChannelCard title={t('menu.qrChannel')} status={t('menu.liveNow')} detail={t('menu.qrChannelHelp')} /><ChannelCard title={t('menu.aggregatorChannel')} status={t('menu.controlled')} detail={t('menu.aggregatorChannelHelp')} /></div><div className="menu-publications"><b>{t('menu.publicationHistory')}</b>{current.publications.map((value) => <span key={value.id}>{value.status} · {new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value.effectiveFrom))}</span>)}</div></section>}
        {showingImportedMenu && tab === 'channels' && <section className="menu-form-panel menu-channel-panel"><h2>{t('menu.channels')}</h2><p>{t('menu.channelHelp')}</p><div className="menu-channel-grid"><ChannelCard title={t('menu.posChannel')} status={t('menu.liveNow')} detail={t('menu.posChannelHelp')} /><ChannelCard title={t('menu.qrChannel')} status={t('menu.liveNow')} detail={t('menu.qrChannelHelp')} /><ChannelCard title={t('menu.aggregatorChannel')} status={t('menu.controlled')} detail={t('menu.aggregatorChannelHelp')} /></div><p className="menu-help">The imported menu is available in New Order. Publish a menu version when you are ready to control each channel.</p></section>}
        {tab === 'import' && <MenuImportDesk t={t} preview={importPreview} itemExportName={itemExportName} addonExportName={addonExportName} matchedImportCount={matchedImportCount} itemCount={importPreview?.items.length ?? 0} stagedImportName={stagedImportName} importDrafts={importDrafts} onItemsFile={(event) => void readImportFile(event, 'items')} onAddonsFile={(event) => void readImportFile(event, 'addons')} />}
      </main>
    </div>
    {studio && selectedItem && current && <ItemEditor item={selectedItem} categories={current.categories} modifiers={current.modifiers} recipeCode={itemById.get(selectedItem.menuItemId)?.code} t={t} busy={Boolean(busy)} onClose={() => setSelectedItemId(undefined)} onSave={updateItem} />}
    {showingImportedMenu && selectedImportedItem && <ImportedItemEditor item={selectedImportedItem} categories={importedCategories} addonGroups={importPreview?.addonGroups ?? []} stations={stations} t={t} busy={Boolean(busy)} onClose={() => setSelectedImportedItemLine(undefined)} onSave={updateImportedItem} />}
  </section>;
}

function MenuItemForm({ recipes, stations, categories = [], t, onSubmit, busy, first = false }: { recipes: RecipeSummary[]; stations: Station[]; categories?: MenuStudioCategory[]; t: Translate; onSubmit: (event: FormEvent<HTMLFormElement>) => void; busy: boolean; first?: boolean }) {
  return <form className="menu-item-form" onSubmit={onSubmit}>
    <input name="name" required placeholder={t('menu.itemName')} disabled={busy} />
    <input name="code" required placeholder={t('menu.itemCode')} disabled={busy} />
    <input name="price" required type="number" min="0" step="0.01" placeholder={t('menu.price')} disabled={busy} />
    <select name="recipeId" disabled={busy}><option value="">{t('menu.recipe')}</option>{recipes.map((recipe) => <option key={recipe.id} value={recipe.id}>{recipe.name}</option>)}</select>
    <select name="stationId" disabled={busy}><option value="">{t('menu.unassigned')}</option>{stations.map((station) => <option key={station.id} value={station.id}>{station.name}</option>)}</select>
    {first ? <input name="categoryName" placeholder={t('menu.categoryName')} /> : <select name="categoryId"><option value="">{t('menu.unassigned')}</option>{categories.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}</select>}
    <button className="menu-primary" disabled={busy}>{t('menu.addItem')}</button>
  </form>;
}

function ItemEditor({ item, categories, modifiers, recipeCode, t, busy, onClose, onSave }: { item: MenuStudioItem; categories: MenuStudioCategory[]; modifiers: MenuModifierGroup[]; recipeCode?: string; t: Translate; busy: boolean; onClose: () => void; onSave: (event: FormEvent<HTMLFormElement>) => void }) {
  return <aside className="menu-editor" aria-label={t('menu.itemEditor')}><div className="menu-editor-heading"><div><p className="eyebrow">{t('menu.itemEditor')}</p><h2>{item.displayName}</h2><small>{recipeCode ?? '—'}</small></div><button type="button" className="menu-close" onClick={onClose} aria-label={t('menu.closeEditor')}>×</button></div><form onSubmit={onSave}><label>{t('menu.itemName')}<input name="displayName" defaultValue={item.displayName} required /></label><label>{t('menu.price')}<input name="price" type="number" min="0" step="0.01" defaultValue={(item.priceMinor / 100).toFixed(2)} required /></label><label>{t('menu.categoryName')}<select name="categoryId" defaultValue={item.categoryId ?? ''}><option value="">{t('menu.unassigned')}</option>{categories.map((category) => <option key={category.id} value={category.id}>{category.name}</option>)}</select></label><label>{t('menu.description')}<textarea name="description" defaultValue={item.description ?? ''} maxLength={500} /></label><fieldset><legend>{t('menu.addons')}</legend>{modifiers.length ? modifiers.map((group) => <label className="menu-check" key={group.id}><input name={`modifier-${group.id}`} type="checkbox" defaultChecked={item.modifierGroupIds.includes(group.id)} /><span>{group.name}</span><small>{group.options.length} {t('menu.options')}</small></label>) : <p>{t('menu.noAddons')}</p>}</fieldset><button className="menu-primary" disabled={busy}>{t('menu.saveItem')}</button></form></aside>;
}

function ImportedItemEditor({ item, categories, addonGroups, stations, t, busy, onClose, onSave }: { item: ImportedPreviewItem; categories: string[]; addonGroups: MenuImportPreview['addonGroups']; stations: Station[]; t: Translate; busy: boolean; onClose: () => void; onSave: (event: FormEvent<HTMLFormElement>) => void }) {
  return <aside className="menu-item-workbench" aria-label={t('menu.itemEditor')}>
    <header className="menu-item-workbench-header">
      <div><p className="menu-item-breadcrumb">{t('menu.title')} <span>›</span> {t('menu.items')} <span>›</span> <b>{item.onlineName || item.name}</b></p><h1>{t('menu.itemEditor')}</h1></div>
      <button type="button" className="menu-workbench-back" onClick={onClose}>‹ {t('menu.back')}</button>
    </header>
    <form className="menu-item-workbench-form" onSubmit={onSave}>
      <main className="menu-item-workbench-body">
        <section className="menu-item-workbench-section">
          <header><h2>{t('menu.configuration')}</h2><p>{t('menu.configurationHelp')}</p></header>
          <div className="menu-item-config-grid">
            <label><span>{t('menu.categoryName')} <em>*</em></span><select name="category" defaultValue={item.category || 'Imported'}>{categories.map((category) => <option key={category} value={category}>{category}</option>)}</select></label>
            <label><span>{t('menu.itemName')} <em>*</em></span><input name="name" defaultValue={item.name} required /></label>
            <label><span>{t('menu.onlineDisplayName')}</span><input name="onlineName" defaultValue={item.onlineName || item.name} /></label>
            <label><span>{t('menu.shortCode')}</span><input name="code" defaultValue={item.code} /></label>
            <label><span>{t('menu.price')} <em>*</em></span><input name="price" type="number" min="0" step="0.01" defaultValue={(item.priceMinor / 100).toFixed(2)} required /></label>
            <label><span>{t('menu.dietary')}</span><select name="dietaryLabel" defaultValue={item.dietaryLabel.toLocaleLowerCase()}><option value="veg">{t('menu.veg')}</option><option value="non-veg">{t('menu.nonVeg')}</option><option value="egg">{t('menu.egg')}</option><option value="">{t('menu.notSpecified')}</option></select></label>
            <label><span>{t('menu.station')}</span><select name="stationId" defaultValue={item.stationId ?? ''}><option value="">{t('menu.unassigned')}</option>{stations.map((station) => <option key={station.id} value={station.id}>{station.name}</option>)}</select></label>
            <label><span>{t('menu.prepMinutes')}</span><input name="prepMinutes" type="number" min="1" max="240" defaultValue={item.prepMinutes ?? 12} /></label>
            <label className="menu-item-config-wide"><span>{t('menu.description')}</span><textarea name="description" defaultValue={item.description} maxLength={500} /></label>
          </div>
        </section>
        <section className="menu-item-workbench-section">
          <header><h2>{t('menu.addons')} &amp; {t('menu.variations')}</h2><p>{t('menu.addonEditorHelp')}</p></header>
          {addonGroups.length ? <div className="menu-item-addon-grid">{addonGroups.map((group) => {
            const assigned = new Set([...item.addOnGroupNames, ...item.addonBindings.map((binding) => binding.name)].map((name) => name.trim().toLocaleLowerCase()));
            return <label className="menu-item-addon-choice" key={group.sourceId}><input type="checkbox" name="addonGroup" value={group.sourceId} defaultChecked={[group.sourceId, group.name, group.onlineName].some((name) => assigned.has(name.trim().toLocaleLowerCase()))} /><span><b>{group.onlineName || group.name}</b><small>{group.selectionMin}–{group.selectionMax} {t('menu.selections')} · {group.options.length} {t('menu.options')}</small></span></label>;
          })}</div> : <p className="menu-item-empty-note">{t('menu.noAddons')}</p>}
          {item.variations.length > 0 && <div className="menu-item-variations">{item.variations.map((variation) => <article key={`${variation.groupName}:${variation.name}`}><span><b>{variation.name}</b><small>{variation.groupName}</small></span><strong>{money(variation.priceMinor)}</strong></article>)}</div>}
        </section>
        <section className="menu-item-workbench-section menu-item-recipe-note"><div><h2>{t('menu.recipe')}</h2><p>{t('menu.recipeOptionalHelp')}</p></div><span>{t('menu.import')}</span></section>
      </main>
      <footer className="menu-item-workbench-footer"><button type="button" className="menu-secondary" onClick={onClose}>{t('menu.cancel')}</button><button className="menu-primary" disabled={busy}>{busy ? t('menu.saving') : t('menu.saveChanges')}</button></footer>
    </form>
  </aside>;
}

function ChannelCard({ title, status, detail }: { title: string; status: string; detail: string }) {
  return <article><div><b>{title}</b><span>{status}</span></div><p>{detail}</p></article>;
}

function MenuImportDesk({ t, preview, itemExportName, addonExportName, matchedImportCount, itemCount, stagedImportName, importDrafts, onItemsFile, onAddonsFile }: { t: Translate; preview?: MenuImportPreview; itemExportName: string; addonExportName: string; matchedImportCount: number; itemCount: number; stagedImportName: string; importDrafts: MenuImportDraft[]; onItemsFile: (event: ChangeEvent<HTMLInputElement>) => void; onAddonsFile: (event: ChangeEvent<HTMLInputElement>) => void }) {
  return <section className="menu-import-desk">
    <header className="menu-import-header"><div><p className="eyebrow">{t('menu.import')}</p><h2>{t('menu.importTitle')}</h2><p>{t('menu.importHelp')}</p></div><div className="menu-import-status"><span className={itemExportName ? 'ready' : ''}>{itemExportName ? '01' : '01'}</span><i /><span className={addonExportName ? 'ready' : ''}>02</span><i /><span>03</span></div></header>
    <div className="menu-file-grid"><label className={itemExportName ? 'has-file' : ''}><span>01 · {t('menu.items')}</span><b>{t('menu.uploadItems')}</b><small>{itemExportName || t('menu.importWaiting')}</small><input type="file" accept=".csv,text/csv" onChange={onItemsFile} /></label><label className={addonExportName ? 'has-file' : ''}><span>02 · {t('menu.addons')}</span><b>{t('menu.uploadAddons')}</b><small>{addonExportName || t('menu.importPrivacy')}</small><input type="file" accept=".csv,text/csv" onChange={onAddonsFile} /></label></div>
    {!preview && <div className="menu-import-empty"><b>{t('menu.importWaiting')}</b><p>{t('menu.importPrivacy')}</p></div>}
    {importDrafts.length > 0 && <section className="menu-import-saved"><div><b>{t('menu.importDrafts')}</b><small>{t('menu.importDraftsHelp')}</small></div>{importDrafts.slice(0, 3).map((draft) => <article key={draft.id}><span><b>{draft.name}</b><small>{draft.itemCount} {t('menu.items').toLocaleLowerCase()} · {draft.categoryCount} {t('menu.categories').toLocaleLowerCase()} · {draft.addonGroupCount} {t('menu.addons').toLocaleLowerCase()}</small></span><em>{t(`menu.importStatus.${draft.status}` as MessageKey)}</em></article>)}</section>}
    {preview && <>
      <div className="menu-import-metrics"><Metric label={t('menu.items')} value={preview.items.length} /><Metric label={t('menu.categories')} value={preview.categories.length} /><Metric label={t('menu.addons')} value={preview.addonGroups.length} /><Metric label={t('menu.variations')} value={preview.variationCount} /></div>
      <div className="menu-import-safety"><b>{t('menu.importMappingTitle')}</b><p>{t('menu.importMappingHelp')}</p><strong>{matchedImportCount} / {itemCount} {t('menu.recipeMatched')}</strong></div>
      {preview.warnings.map((warning) => <p className="menu-message error" key={warning.code}>{importWarningText(warning, t)}</p>)}
      {stagedImportName && <p className="menu-message success">{t('menu.importStagedNamed', { name: stagedImportName })}</p>}
      <div className="menu-import-list"><header><span>{t('menu.items')}</span><span>{t('menu.price')}</span><span>{t('menu.addons')} · {t('menu.variations')}</span></header>{preview.items.slice(0, 8).map((item) => <article key={item.sourceLine}><div><b>{item.onlineName}</b><small>{item.category} · {item.code}</small></div><strong>{money(item.priceMinor)}</strong><span>{item.addOnGroupNames.length} {t('menu.addons').toLocaleLowerCase()} · {item.variations.length} {t('menu.variations').toLocaleLowerCase()}</span></article>)}{preview.items.length > 8 && <p>{preview.items.length - 8} {t('menu.moreImportedItems')}</p>}</div>
    </>}
  </section>;
}

function importWarningText(warning: MenuImportWarning, t: Translate): string {
  if (warning.code === 'no_items') return t('menu.importWarningNoItems');
  if (warning.code === 'no_addon_groups') return t('menu.importWarningNoAddons');
  if (warning.code === 'missing_item_codes') return t('menu.importWarningMissingCodes');
  return t('menu.importWarningUnresolvedLinks', { count: warning.count });
}

function Metric({ label, value }: { label: string; value: number }) { return <article><strong>{value}</strong><span>{label}</span></article>; }
