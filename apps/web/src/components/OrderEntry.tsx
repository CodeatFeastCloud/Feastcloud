import { useEffect, useMemo, useRef, useState, type CSSProperties, type FormEvent } from 'react';
import { localize, menuCatalog } from '../domain/catalog';
import type { KitchenOrder, Locale, OrderLine, OrderType } from '../domain/types';
import {
  commerceApiBase,
  createConnectorCanonicalOrder,
  decideConnectorInbox,
  fetchConnectorInbox,
  fetchConnectorInstallations,
  fetchOrders,
  fetchMenuImportDrafts,
  ingestConnectorOrder,
  MENU_IMPORT_UPDATED_EVENT,
  streamConnectorInbox,
  type ConnectorInboxOrder,
  type ConnectorInstallation,
} from '../domain/coreCommerce';
import { createUuidV7 } from '../domain/kitchen';
import { formatMoney } from '../i18n';
import type { MessageKey } from '../i18n/messages';
import { Icon } from './Icon';

type CatalogAddonOption = { id: string; name: string; priceMinor: number };
type CatalogAddonGroup = { id: string; name: string; selectionMin: number; selectionMax: number; options: CatalogAddonOption[] };
type SelectedAddon = CatalogAddonOption & { groupId: string; groupName: string; quantity: number };
type CartLine = Pick<OrderLine, 'menuItemId' | 'quantity' | 'name' | 'stationId' | 'note'> & {
  prepMinutes?: number;
  unitPriceMinor: number;
  addons: SelectedAddon[];
};
type SubmittedOrderLine = Pick<OrderLine, 'menuItemId' | 'quantity' | 'name' | 'stationId' | 'note'> & { prepMinutes?: number };
type CategoryFilter = string;
type OrderCatalogItem = { id: string; name: string; description: string; category: string; priceMinor: number; vegetarian: boolean; station: string; prepMinutes: number; accent: string; glyph: string; addonGroups: CatalogAddonGroup[] };

const api = commerceApiBase((import.meta.env.VITE_CORE_URL as string | undefined)?.trim());
const accents = ['#bf573e', '#527b5d', '#a77633', '#657ca6', '#805c91', '#cf7963'];
const coreUUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const connectorCatalog = [
  { key: 'swiggy', name: 'Swiggy', color: '#f47b20' },
  { key: 'zomato', name: 'Zomato', color: '#e23744' },
  { key: 'magicpin', name: 'Magicpin', color: '#ef3d7d' },
  { key: 'ondc', name: 'ONDC', color: '#5b52a3' },
  { key: 'dotpe', name: 'DotPe', color: '#246bce' },
  { key: 'thrive', name: 'Thrive', color: '#21845b' },
  { key: 'ownly', name: 'Ownly', color: '#294146' },
  { key: 'other', name: 'More adapters', color: '#64748b' },
] as const;

type IncomingLine = { name: string; quantity: number; note?: string; unitPriceMinor?: number; addons: string[] };
type IncomingSummary = {
  provider: string; brand?: string; externalOutletId?: string; customer: string; phone?: string; address?: string; payment?: string;
  totalMinor?: number; lines: IncomingLine[]; note?: string; placedAt?: string;
  deliveryEta?: string; deliveryPartner?: string; simulated: boolean;
};

function nested(value: Record<string, unknown>, path: string): unknown {
  return path.split('.').reduce<unknown>((current, key) => current && typeof current === 'object' ? (current as Record<string, unknown>)[key] : undefined, value);
}

function firstText(payload: Record<string, unknown>, paths: string[]): string {
  for (const path of paths) {
    const value = nested(payload, path);
    if (typeof value === 'string' && value.trim()) return value.trim();
    if (typeof value === 'number') return String(value);
  }
  return '';
}

function monetaryMinor(payload: Record<string, unknown>, minorPaths: string[], majorPaths: string[]): number | undefined {
  const minor = Number(firstText(payload, minorPaths));
  if (Number.isFinite(minor) && minor > 0) return Math.round(minor);
  const major = Number(firstText(payload, majorPaths));
  return Number.isFinite(major) && major > 0 ? Math.round(major * 100) : undefined;
}

function addressText(payload: Record<string, unknown>): string | undefined {
  const direct = firstText(payload, ['delivery.address', 'delivery_address', 'customer.address', 'order.delivery_address']);
  if (direct) return direct;
  const value = ['delivery.address', 'delivery_address', 'customer.address', 'order.delivery_address']
    .map((path) => nested(payload, path)).find((candidate) => candidate && typeof candidate === 'object');
  if (!value || typeof value !== 'object') return undefined;
  const address = value as Record<string, unknown>;
  const parts = ['address1', 'address2', 'line1', 'line2', 'landmark', 'area', 'city', 'pincode']
    .map((key) => address[key]).filter((part): part is string | number => typeof part === 'string' || typeof part === 'number');
  return parts.length ? parts.join(', ') : undefined;
}

function summarizeIncoming(order: ConnectorInboxOrder, connector?: ConnectorInstallation): IncomingSummary {
  const payload = order.payload;
  const rawItems = ['items', 'order.items', 'order_items', 'orderItems', 'order_details.items', 'order_details.order_items', 'cart.items', 'line_items']
    .map((path) => nested(payload, path)).find(Array.isArray) as unknown[] | undefined;
  const lines = (rawItems ?? []).flatMap((raw) => {
    if (!raw || typeof raw !== 'object') return [];
    const item = raw as Record<string, unknown>;
    const name = firstText(item, ['name', 'item_name', 'itemName', 'title', 'displayName']);
    if (!name) return [];
    const quantity = Number(firstText(item, ['quantity', 'qty', 'count'])) || 1;
    const rawAddons = ['addons', 'add_ons', 'modifiers', 'options'].map((path) => nested(item, path)).find(Array.isArray) as unknown[] | undefined;
    const addons = (rawAddons ?? []).flatMap((addon) => {
      if (typeof addon === 'string') return [addon];
      if (!addon || typeof addon !== 'object') return [];
      const label = firstText(addon as Record<string, unknown>, ['name', 'title', 'option_name', 'itemName']);
      return label ? [label] : [];
    });
    return [{
      name, quantity: Math.max(1, quantity),
      note: firstText(item, ['note', 'instructions', 'preparation_note', 'food_instruction']) || undefined,
      unitPriceMinor: monetaryMinor(item, ['priceMinor', 'price_minor', 'unit_price_minor'], ['price', 'unit_price', 'item_price']),
      addons,
    }];
  });
  const provider = connector?.provider || firstText(payload, ['provider', 'platform', 'channel', 'source']) || 'Online partner';
  const externalOutletId = firstText(payload, ['externalOutletId', 'external_outlet_id', 'restaurantId', 'restaurant_id', 'restaurant.id', 'storeId', 'store_id', 'outlet.id', 'order.restaurant_id']) || undefined;
  const externalOutlet = connector?.configuration?.externalOutlets?.find((candidate) => candidate.active && candidate.externalOutletId === externalOutletId);
  const customer = firstText(payload, ['customer.name', 'customer_name', 'guest.name', 'name']) || 'Online guest';
  const totalMinor = monetaryMinor(payload, ['totalMinor', 'total_minor', 'order.totalMinor'], ['total', 'totalAmount', 'order_total', 'order.total', 'bill.total']);
  return {
    provider, brand: externalOutlet?.brandName, externalOutletId, customer, totalMinor, lines,
    simulated: payload.simulation === true,
    phone: firstText(payload, ['customer.phone', 'customer.mobile', 'customer_phone', 'customer_mobile', 'guest.phone']) || undefined,
    address: addressText(payload),
    payment: firstText(payload, ['payment.status', 'payment.mode', 'payment_method', 'paymentMode', 'order.payment_status']) || undefined,
    note: firstText(payload, ['note', 'instructions', 'order.note', 'special_instructions']) || undefined,
    placedAt: firstText(payload, ['placedAt', 'placed_at', 'order.created_at', 'order.order_time', 'created_at']) || undefined,
    deliveryEta: firstText(payload, ['delivery.eta', 'delivery_eta', 'expected_delivery_time', 'order.delivery_time']) || undefined,
    deliveryPartner: firstText(payload, ['delivery.partner', 'delivery.executive.name', 'rider.name', 'delivery_partner']) || undefined,
  };
}

function realConnectorOrders(orders: ConnectorInboxOrder[]): ConnectorInboxOrder[] {
  return orders.filter((order) => {
    const keys = Object.keys(order.payload);
    return !(keys.length === 1 && keys[0] === 'items' && typeof order.payload.items === 'number');
  });
}

function realConnectors(connectors: ConnectorInstallation[]): ConnectorInstallation[] {
  return connectors.filter((connector) => !connector.provider.toLocaleLowerCase().startsWith('test-provider-'));
}

function initials(value: string): string {
  return value.split(/\s+/).filter(Boolean).slice(0, 2).map((word) => word[0]).join('').toUpperCase() || 'FC';
}

function initialOrderWorkspace(): 'create' | 'incoming' {
  try { return new URLSearchParams(globalThis.location?.search).get('flow') === 'incoming' ? 'incoming' : 'create'; }
  catch { return 'create'; }
}

interface OrderEntryProps {
  locale: Locale;
  tenantId?: string;
  outletId?: string;
  apiBase?: string;
  t: (key: MessageKey, replacements?: Record<string, string | number>) => string;
  onSubmit: (input: {
    type: OrderType;
    guestName?: string;
    tableLabel?: string;
    note?: string;
    aggregator?: KitchenOrder['aggregator'];
    lines: SubmittedOrderLine[];
  }) => Promise<KitchenOrder>;
}

const orderTypes: OrderType[] = ['dineIn', 'takeaway', 'delivery'];

export function OrderEntry({ locale, tenantId, outletId, apiBase, t, onSubmit }: OrderEntryProps) {
  const connectorApi = apiBase ?? api;
  const [workspace, setWorkspace] = useState<'create' | 'incoming'>(initialOrderWorkspace);
  const [incomingFilter, setIncomingFilter] = useState<'all' | 'awaiting' | 'accepted' | 'needs_review' | 'rejected'>('all');
  const [cart, setCart] = useState<CartLine[]>([]);
  const [category, setCategory] = useState<CategoryFilter>('all');
  const [categoryQuery, setCategoryQuery] = useState('');
  const [query, setQuery] = useState('');
  const [orderType, setOrderType] = useState<OrderType>('takeaway');
  const [guestName, setGuestName] = useState('');
  const [tableLabel, setTableLabel] = useState('');
  const [note, setNote] = useState('');
  const [discountPercent, setDiscountPercent] = useState(0);
  const [customizingItemId, setCustomizingItemId] = useState<string>();
  const [addonSelections, setAddonSelections] = useState<Record<string, Record<string, number>>>({});
  const [submitting, setSubmitting] = useState(false);
  const [confirmation, setConfirmation] = useState('');
  const [submissionFailed, setSubmissionFailed] = useState(false);
  const [importedMenu, setImportedMenu] = useState<OrderCatalogItem[]>([]);
  const [incomingOrders, setIncomingOrders] = useState<ConnectorInboxOrder[]>([]);
  const [connectors, setConnectors] = useState<ConnectorInstallation[]>([]);
  const [incomingBusy, setIncomingBusy] = useState('');
  const [simulatorBusy, setSimulatorBusy] = useState(false);
  const [incomingError, setIncomingError] = useState('');
  const [canonicalOrderIds, setCanonicalOrderIds] = useState<Record<string, string>>({});
  const confirmationTimer = useRef<number>();
  const changeWorkspace = (next: 'create' | 'incoming') => {
    setWorkspace(next);
    const url = new URL(globalThis.location.href);
    if (next === 'incoming') url.searchParams.set('flow', 'incoming');
    else url.searchParams.delete('flow');
    globalThis.history.replaceState(globalThis.history.state, '', url);
  };

  useEffect(() => {
    if (!connectorApi || !tenantId || !outletId) return;
    let active = true;
    const refreshImportedMenu = () => {
      void fetchMenuImportDrafts(connectorApi, tenantId, outletId).then((drafts) => {
        const draft = drafts
          .filter((value) => ['applied', 'staged', 'mapping'].includes(value.status))
          .sort((left, right) => Date.parse(right.importedAt) - Date.parse(left.importedAt))[0];
        if (!active) return;
        if (!draft?.draft?.items.length) {
          setImportedMenu([]);
          return;
        }
        setImportedMenu(draft.draft.items.map((item, index) => ({
          id: `import:${draft.id}:${item.code || item.sourceLine}`,
          name: item.onlineName || item.name,
          description: item.description || '',
          category: item.category || 'Imported',
          priceMinor: item.priceMinor || item.variations[0]?.priceMinor || 0,
          vegetarian: item.dietaryLabel.toLocaleLowerCase() !== 'non-veg',
          station: item.stationId || 'unassigned', prepMinutes: item.prepMinutes ?? 12,
          accent: accents[index % accents.length], glyph: initials(item.onlineName || item.name),
          addonGroups: [...new Set([...item.addOnGroupNames, ...item.addonBindings.map((binding) => binding.name)])]
            .map((groupName) => draft.draft.addonGroups.find((group) => [group.sourceId, group.name, group.onlineName].some((candidate) => candidate.trim().toLocaleLowerCase() === groupName.trim().toLocaleLowerCase())))
            .filter((group) => Boolean(group)).map((group) => ({
            id: group!.sourceId,
            name: group!.onlineName || group!.name,
            selectionMin: group!.selectionMin,
            selectionMax: Math.max(group!.selectionMin, group!.selectionMax || 1),
            options: group!.options.filter((option) => option.active).map((option, optionIndex) => ({ id: `${group!.sourceId}:${optionIndex}`, name: option.name, priceMinor: option.priceMinor })),
          })).filter((group) => group.options.length > 0),
        })));
      }).catch(() => { /* The local order screen remains usable offline. */ });
    };
    const onImportUpdated = (event: Event) => {
      const detail = (event as CustomEvent<{ tenant?: string; outlet?: string }>).detail;
      if ((!detail?.tenant || detail.tenant === tenantId) && (!detail?.outlet || detail.outlet === outletId)) refreshImportedMenu();
    };
    refreshImportedMenu();
    window.addEventListener(MENU_IMPORT_UPDATED_EVENT, onImportUpdated);
    const interval = window.setInterval(refreshImportedMenu, 30_000);
    return () => {
      active = false;
      window.removeEventListener(MENU_IMPORT_UPDATED_EVENT, onImportUpdated);
      window.clearInterval(interval);
    };
  }, [connectorApi, outletId, tenantId]);

  useEffect(() => {
    if (!connectorApi || !tenantId || !outletId) return;
    let active = true;
    const refresh = async () => {
      try {
        const [orders, installed] = await Promise.all([
          fetchConnectorInbox(connectorApi, tenantId, outletId),
          fetchConnectorInstallations(connectorApi, tenantId, outletId),
        ]);
        if (!active) return;
        setIncomingOrders(realConnectorOrders(orders));
        setConnectors(realConnectors(installed));
        setIncomingError('');
      } catch {
        if (active) setIncomingError(t('commerce.unavailable'));
      }
    };
    void refresh();
    const controller = new AbortController();
    let reconnectTimer: number | undefined;
    const connectLive = async () => {
      try {
        await streamConnectorInbox(connectorApi, tenantId, outletId, (orders) => {
          if (active) { setIncomingOrders(realConnectorOrders(orders)); setIncomingError(''); }
        }, controller.signal);
      } catch {
        if (active && !controller.signal.aborted) reconnectTimer = window.setTimeout(() => void connectLive(), 2_000);
      }
    };
    void connectLive();
    const interval = window.setInterval(() => void refresh(), 30_000);
    const refreshWhenVisible = () => { if (document.visibilityState === 'visible') void refresh(); };
    window.addEventListener('focus', refreshWhenVisible);
    document.addEventListener('visibilitychange', refreshWhenVisible);
    return () => {
      active = false;
      controller.abort();
      if (reconnectTimer) window.clearTimeout(reconnectTimer);
      window.clearInterval(interval);
      window.removeEventListener('focus', refreshWhenVisible);
      document.removeEventListener('visibilitychange', refreshWhenVisible);
    };
  }, [connectorApi, outletId, t, tenantId]);

  useEffect(() => {
    if (!customizingItemId) return;
    const previousOverflow = document.body.style.overflow;
    const closeOnEscape = (event: KeyboardEvent) => { if (event.key === 'Escape') setCustomizingItemId(undefined); };
    document.body.style.overflow = 'hidden';
    window.addEventListener('keydown', closeOnEscape);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener('keydown', closeOnEscape);
    };
  }, [customizingItemId]);

  const fallbackMenu = useMemo<OrderCatalogItem[]>(() => menuCatalog.map((item, index) => ({
    id: item.id, name: localize(item.name, locale), description: localize(item.description, locale), category: item.category,
    priceMinor: item.price.minorUnits, vegetarian: item.vegetarian, station: item.station, prepMinutes: item.prepMinutes,
    accent: item.accent, glyph: item.glyph || initials(localize(item.name, locale)), addonGroups: [],
  })), [locale]);
  const sourceMenu = importedMenu.length ? importedMenu : fallbackMenu;
  const menuByID = useMemo(() => new Map(sourceMenu.map((item) => [item.id, item])), [sourceMenu]);
  const categoryFilters = useMemo(() => ['all', ...new Set(sourceMenu.map((item) => item.category))], [sourceMenu]);
  const categoryCounts = useMemo(() => new Map(categoryFilters.map((filter) => [filter, filter === 'all' ? sourceMenu.length : sourceMenu.filter((item) => item.category === filter).length])), [categoryFilters, sourceMenu]);
  const visibleCategoryFilters = useMemo(() => {
    const normalized = categoryQuery.trim().toLocaleLowerCase(locale);
    return normalized ? categoryFilters.filter((filter) => filter === 'all' || filter.toLocaleLowerCase(locale).includes(normalized)) : categoryFilters;
  }, [categoryFilters, categoryQuery, locale]);

  useEffect(() => {
    if (!categoryFilters.includes(category)) setCategory('all');
  }, [category, categoryFilters]);

  const menu = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase(locale);
    return sourceMenu.filter((item) => {
      const inCategory = category === 'all' || item.category === category;
      const searchable = `${item.name} ${item.description}`.toLocaleLowerCase(locale);
      return inCategory && (!normalized || searchable.includes(normalized));
    });
  }, [category, locale, query, sourceMenu]);

  const itemCount = cart.reduce((total, line) => total + line.quantity, 0);
  const subtotal = cart.reduce((total, line) => total + line.unitPriceMinor * line.quantity, 0);
  const discount = Math.round(subtotal * discountPercent / 100);
  const taxableSubtotal = Math.max(0, subtotal - discount);
  const tax = Math.round(taxableSubtotal * 0.05);
  const customizingItem = customizingItemId ? menuByID.get(customizingItemId) : undefined;
  const selectedAddonOptions = customizingItem?.addonGroups.flatMap((group) => {
    const selected = addonSelections[group.id] ?? {};
    return group.options.filter((option) => (selected[option.id] ?? 0) > 0).map((option) => ({ ...option, groupId: group.id, groupName: group.name, quantity: selected[option.id] }));
  }) ?? [];
  const customizationValid = customizingItem?.addonGroups.every((group) => {
    const count = Object.values(addonSelections[group.id] ?? {}).reduce((total, quantity) => total + quantity, 0);
    return count >= group.selectionMin && count <= group.selectionMax;
  }) ?? false;

  const categoryLabel = (filter: string) => filter === 'all' || ['mains', 'snacks', 'drinks'].includes(filter) ? t(`order.category.${filter}` as MessageKey) : filter;

  const changeQuantity = (menuItemId: string, delta: number) => {
    setCart((current) => {
      const existing = current.find((line) => line.menuItemId === menuItemId);
      const item = menuByID.get(menuItemId);
      if (!existing && delta > 0 && item) return [...current, { menuItemId, quantity: 1, name: item.name, stationId: item.station, prepMinutes: item.prepMinutes, unitPriceMinor: item.priceMinor, addons: [] }];
      return current
        .map((line) =>
          line.menuItemId === menuItemId ? { ...line, quantity: line.quantity + delta } : line,
        )
        .filter((line) => line.quantity > 0);
    });
  };

  const openCustomization = (item: OrderCatalogItem) => {
    const existing = cart.find((line) => line.menuItemId === item.id);
    setAddonSelections(Object.fromEntries(item.addonGroups.map((group) => [group.id, Object.fromEntries(existing?.addons.filter((addon) => addon.groupId === group.id).map((addon) => [addon.id, addon.quantity]) ?? [])])));
    setCustomizingItemId(item.id);
  };

  const chooseAddon = (group: CatalogAddonGroup, optionId: string) => {
    setAddonSelections((current) => {
      const selected = current[group.id] ?? {};
      if ((selected[optionId] ?? 0) > 0) return { ...current, [group.id]: { ...selected, [optionId]: 0 } };
      if (group.selectionMax === 1) return { ...current, [group.id]: { [optionId]: 1 } };
      const total = Object.values(selected).reduce((sum, quantity) => sum + quantity, 0);
      if (total >= group.selectionMax) return current;
      return { ...current, [group.id]: { ...selected, [optionId]: 1 } };
    });
  };

  const changeAddonQuantity = (group: CatalogAddonGroup, optionId: string, delta: number) => {
    setAddonSelections((current) => {
      const selected = current[group.id] ?? {};
      const currentQuantity = selected[optionId] ?? 0;
      const total = Object.values(selected).reduce((sum, quantity) => sum + quantity, 0);
      if (delta > 0 && total >= group.selectionMax) return current;
      return { ...current, [group.id]: { ...selected, [optionId]: Math.max(0, currentQuantity + delta) } };
    });
  };

  const saveCustomization = () => {
    if (!customizingItem || !customizationValid) return;
    const addons = selectedAddonOptions;
    const unitPriceMinor = customizingItem.priceMinor + addons.reduce((sum, option) => sum + option.priceMinor * option.quantity, 0);
    const addonNote = addons.length ? `${t('order.addons')}: ${addons.map((option) => `${option.name}${option.quantity > 1 ? ` ×${option.quantity}` : ''}`).join(', ')}` : undefined;
    setCart((current) => {
      const existing = current.find((line) => line.menuItemId === customizingItem.id);
      if (existing) return current.map((line) => line.menuItemId === customizingItem.id ? { ...line, addons, unitPriceMinor, note: addonNote } : line);
      return [...current, { menuItemId: customizingItem.id, quantity: 1, name: customizingItem.name, stationId: customizingItem.station, prepMinutes: customizingItem.prepMinutes, addons, unitPriceMinor, note: addonNote }];
    });
    setCustomizingItemId(undefined);
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (cart.length === 0 || submitting) return;
    setSubmitting(true);
    setSubmissionFailed(false);
    try {
      const discountNote = discountPercent > 0 ? t('order.discountNote', { percent: discountPercent, amount: formatMoney(locale, discount) }) : '';
      const order = await onSubmit({
        type: orderType,
        guestName,
        tableLabel: orderType === 'dineIn' ? tableLabel : undefined,
        note: [note.trim(), discountNote].filter(Boolean).join(' · ') || undefined,
        lines: importedMenu.length ? cart.map(({ menuItemId, quantity, name, stationId, prepMinutes, note: lineNote }) => ({ menuItemId, quantity, name, stationId, prepMinutes, note: lineNote })) : cart.map(({ menuItemId, quantity }) => ({ menuItemId, quantity })),
      });
      setCart([]);
      setGuestName('');
      setTableLabel('');
      setNote('');
      setDiscountPercent(0);
      setConfirmation(t('order.sentOffline', { number: order.number }));
      if (confirmationTimer.current) window.clearTimeout(confirmationTimer.current);
      confirmationTimer.current = window.setTimeout(() => setConfirmation(''), 5000);
    } catch {
      setSubmissionFailed(true);
      setConfirmation(t('order.saveFailed'));
      if (confirmationTimer.current) window.clearTimeout(confirmationTimer.current);
      confirmationTimer.current = window.setTimeout(() => setConfirmation(''), 8000);
    } finally {
      setSubmitting(false);
    }
  };

  const openIncoming = incomingOrders.filter((order) => order.status === 'received' || order.status === 'needs_review');
  const displayedIncoming = incomingOrders.filter((order) => {
    if (incomingFilter === 'all') return true;
    if (incomingFilter === 'awaiting') return order.status === 'received';
    return order.status === incomingFilter;
  });
  const connectorById = new Map(connectors.map((connector) => [connector.id, connector]));
  const refreshIncoming = async () => {
    if (!connectorApi || !tenantId || !outletId) return;
    const [orders, installed] = await Promise.all([
      fetchConnectorInbox(connectorApi, tenantId, outletId),
      fetchConnectorInstallations(connectorApi, tenantId, outletId),
    ]);
    setIncomingOrders(realConnectorOrders(orders));
    setConnectors(realConnectors(installed));
    setIncomingError('');
  };
  const simulateMappedSwiggyOrder = async () => {
    if (!connectorApi || !tenantId || !outletId || simulatorBusy) return;
    const connector = connectors.find((candidate) => candidate.provider.toLocaleLowerCase().includes('swiggy'));
    // Older connector drafts did not persist `active`; treat those mappings as
    // enabled so the local test flow keeps working after a configuration reload.
    const mappings = connector?.configuration?.externalOutlets?.filter((mapping) => mapping.active !== false) ?? [];
    if (!connector || mappings.length === 0) {
      setIncomingError('Add the Swiggy restaurant mappings before running the simulator.');
      return;
    }
    setSimulatorBusy(true);
    setIncomingError('');
    try {
      const now = Date.now();
      const index = Math.floor(Math.random() * mappings.length);
      const mapping = mappings[index];
      if (!mapping) throw new Error('No active Swiggy restaurant mapping is available.');
      const first = sourceMenu[(index * 2) % sourceMenu.length];
      const second = sourceMenu[(index * 2 + 1) % sourceMenu.length];
      const items = [first, second].filter((item): item is OrderCatalogItem => Boolean(item)).map((item, itemIndex) => ({
          itemId: item.id,
          name: item.name,
          quantity: itemIndex === 0 ? 1 : 2,
          unitPriceMinor: item.priceMinor,
          addons: item.addonGroups[0]?.options.slice(0, 1).map((option) => ({ name: option.name, priceMinor: option.priceMinor })) ?? [],
          instructions: itemIndex === 0 ? 'Simulation: standard preparation' : '',
      }));
      const totalMinor = items.reduce((total, item) => total + item.unitPriceMinor * item.quantity, 0);
      await ingestConnectorOrder(connectorApi, tenantId, outletId, connector.id, `SIM-${mapping.externalOutletId}-${createUuidV7()}`, {
          simulation: true,
          provider: 'Swiggy',
          externalOutletId: mapping.externalOutletId,
          restaurant: { id: mapping.externalOutletId, name: mapping.brandName },
          customer: { name: 'Test guest', phone: '9000000000' },
          delivery: { address: { line1: '1, Simulator Road', area: 'Bengaluru', city: 'Bengaluru', pincode: '560001' }, eta: new Date(now + 25 * 60_000).toISOString(), partner: 'Awaiting assignment' },
          payment: { status: 'PAID', mode: 'ONLINE' },
          order: { created_at: new Date(now + index * 1000).toISOString() },
          items,
          totalMinor,
          instructions: 'LOCAL TEST ORDER — do not prepare',
      });
      await refreshIncoming();
      setIncomingFilter('awaiting');
    } catch (error) {
      setIncomingError(error instanceof Error && error.message ? `The simulated Swiggy order could not be created: ${error.message}` : 'The simulated Swiggy order could not be created.');
    } finally {
      setSimulatorBusy(false);
    }
  };
  const decideIncoming = async (order: ConnectorInboxOrder, decision: 'rejected' | 'needs_review') => {
    if (!connectorApi || !tenantId || !outletId || incomingBusy) return;
    setIncomingBusy(order.id);
    setIncomingError('');
    try {
      await decideConnectorInbox(connectorApi, tenantId, outletId, order.id, decision, decision === 'needs_review' ? t('commerce.inbox.reviewReason') : 'Rejected by outlet');
      await refreshIncoming();
    } catch { setIncomingError(t('commerce.inbox.failed')); }
    finally { setIncomingBusy(''); }
  };
  const acceptIncoming = async (order: ConnectorInboxOrder) => {
    if (!connectorApi || !tenantId || !outletId || incomingBusy) return;
    const summary = summarizeIncoming(order, connectorById.get(order.connectorId));
    if (summary.lines.length === 0) {
      setIncomingError('This partner order has no readable item lines. Send it to review and check its menu mapping.');
      return;
    }
    setIncomingBusy(order.id);
    setIncomingError('');
    try {
      const lines = summary.lines.map((line, index) => {
        const normalizedName = line.name.trim().toLocaleLowerCase(locale);
        const item = sourceMenu.find((candidate) => candidate.name.trim().toLocaleLowerCase(locale) === normalizedName);
        return {
          menuItemId: item?.id ?? `connector:${order.id}:${index}`,
          quantity: line.quantity,
          name: item?.name ?? line.name,
          stationId: item?.station ?? 'unassigned',
          prepMinutes: item?.prepMinutes ?? 12,
          note: [line.note, line.addons.length ? `Add-ons: ${line.addons.join(', ')}` : ''].filter(Boolean).join(' · ') || undefined,
        };
      });
      const existingCanonicalId = canonicalOrderIds[order.id];
      const canonical = existingCanonicalId ? undefined : await onSubmit({
        type: 'delivery',
        guestName: summary.customer,
        note: summary.note,
        aggregator: {
          provider: summary.provider,
          brandName: summary.brand || summary.provider,
          externalOrderId: order.externalOrderId,
          externalOutletId: summary.externalOutletId,
        },
        lines,
      });
      const canonicalId = existingCanonicalId ?? canonical!.id;
      if (!existingCanonicalId) setCanonicalOrderIds((current) => ({ ...current, [order.id]: canonicalId }));
      const coreOrders = await fetchOrders(connectorApi, tenantId, outletId);
      if (!coreOrders.some((coreOrder) => coreOrder.id === canonicalId)) {
        await createConnectorCanonicalOrder(
          connectorApi, tenantId, outletId, canonicalId,
          `${summary.provider}:${order.externalOrderId}`.slice(0, 128),
          summary.placedAt && !Number.isNaN(Date.parse(summary.placedAt)) ? new Date(summary.placedAt).toISOString() : order.receivedAt,
          summary.lines.map((line) => {
            const normalizedName = line.name.trim().toLocaleLowerCase(locale);
            const item = sourceMenu.find((candidate) => candidate.name.trim().toLocaleLowerCase(locale) === normalizedName);
            return { id: createUuidV7(), menuItemId: item && coreUUID.test(item.id) ? item.id : undefined, name: item?.name ?? line.name, quantity: line.quantity, unitPriceMinor: line.unitPriceMinor ?? item?.priceMinor ?? 0, preparationNote: [line.note, line.addons.length ? `Add-ons: ${line.addons.join(', ')}` : ''].filter(Boolean).join(' · ') || undefined };
          }),
        );
      }
      await decideConnectorInbox(connectorApi, tenantId, outletId, order.id, 'accepted', '', canonicalId);
      await refreshIncoming();
      setIncomingFilter('all');
      setConfirmation(canonical ? `Order #${canonical.number} accepted and sent to the kitchen.` : 'Order acceptance synced with the partner inbox.');
      if (confirmationTimer.current) window.clearTimeout(confirmationTimer.current);
      confirmationTimer.current = window.setTimeout(() => setConfirmation(''), 5000);
    } catch { setIncomingError(t('commerce.inbox.failed')); }
    finally { setIncomingBusy(''); }
  };

  return (
    <section className="page order-page" aria-labelledby="order-title">
      <header className="order-workspace-header">
        <div className="order-workspace-title">
          <span className="eyebrow">Order desk</span>
          <h1 id="order-title">{workspace === 'create' ? 'Create an order' : 'Incoming online orders'}</h1>
          <p>{workspace === 'create' ? 'Counter, phone and direct orders.' : 'Review partner orders before they reach the kitchen.'}</p>
        </div>
        <nav className="order-workspace-tabs" aria-label="Order workflow">
          <button type="button" className={workspace === 'create' ? 'active' : ''} aria-pressed={workspace === 'create'} onClick={() => changeWorkspace('create')}><Icon name="plus" /><span>Create order</span></button>
          <button type="button" className={workspace === 'incoming' ? 'active' : ''} aria-pressed={workspace === 'incoming'} onClick={() => changeWorkspace('incoming')}><Icon name="truck" /><span>Online orders</span>{openIncoming.length > 0 && <b>{openIncoming.length}</b>}</button>
        </nav>
        <div className="order-workspace-facts">
          <span><i className="is-live" />{importedMenu.length || sourceMenu.length} menu items</span>
          <span><i />{connectors.filter((connector) => connector.status === 'healthy').length} integrations live</span>
        </div>
      </header>

      {workspace === 'create' ? <>
        {importedMenu.length > 0 && <p className="order-menu-source"><b>{t('menu.liveNow')}</b><span>{importedMenu.length} {t('menu.items').toLocaleLowerCase()} available</span></p>}

        <div className="order-layout">
        <div className="menu-panel">
          <div className="order-browser-toolbar">
            <label className="search-field">
              <span className="sr-only">{t('order.search')}</span>
              <Icon name="search" />
              <input
                type="search"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={t('order.searchPlaceholder')}
              />
            </label>
            <label className="order-category-select"><span>{t('order.categories')}</span><select value={category} onChange={(event) => setCategory(event.target.value)}>{categoryFilters.map((filter) => <option value={filter} key={filter}>{categoryLabel(filter)} ({categoryCounts.get(filter)})</option>)}</select></label>
          </div>

          <div className="order-browser">
            <aside className="order-category-rail" aria-label={t('order.categories')}>
              <header><span>{t('order.categories')}</span><b>{categoryFilters.length - 1}</b></header>
              {categoryFilters.length > 8 && <label><span className="sr-only">{t('order.searchCategories')}</span><Icon name="search" /><input type="search" value={categoryQuery} onChange={(event) => setCategoryQuery(event.target.value)} placeholder={t('order.searchCategories')} /></label>}
              <nav>{visibleCategoryFilters.map((filter) => <button type="button" key={filter} className={category === filter ? 'active' : ''} aria-pressed={category === filter} onClick={() => setCategory(filter)}><span>{categoryLabel(filter)}</span><b>{categoryCounts.get(filter)}</b></button>)}</nav>
            </aside>
            <section className="order-menu-results">
              <header className="order-result-heading"><div><span>{t('order.showing')}</span><h2>{categoryLabel(category)}</h2></div><b>{menu.length} {t('menu.items').toLocaleLowerCase()}</b></header>
          {menu.length > 0 ? (
            <div className="menu-grid">
              {menu.map((item) => {
                const cartLine = cart.find((line) => line.menuItemId === item.id);
                return (
                  <article className="menu-card" key={item.id}>
                    <div className="menu-art" style={{ '--item-accent': item.accent } as CSSProperties}>
                      <span>{item.glyph}</span>
                      {item.vegetarian && (
                        <i role="img" title={t('order.vegetarian')} aria-label={t('order.vegetarian')} />
                      )}
                    </div>
                    <div className="menu-copy">
                      <div>
                        <h2>{item.name}</h2>
                        <p>{item.description}</p>
                      </div>
                      <div className="menu-meta">
                        <strong>{formatMoney(locale, item.priceMinor)}</strong>
                        <span><Icon name="clock" /> {t('order.minutes', { count: item.prepMinutes })}</span>
                      </div>
                    </div>
                    {cartLine ? (
                      <div className="menu-card-actions"><div className="inline-stepper">
                        <button
                          type="button"
                          onClick={() => changeQuantity(item.id, -1)}
                          aria-label={t('a11y.decrease', { item: item.name })}
                        >
                          <Icon name="minus" />
                        </button>
                        <strong>{cartLine.quantity}</strong>
                        <button
                          type="button"
                          onClick={() => changeQuantity(item.id, 1)}
                          aria-label={t('a11y.increase', { item: item.name })}
                        >
                          <Icon name="plus" />
                        </button>
                      </div>{item.addonGroups.length > 0 && <button type="button" className="customize-item" onClick={() => openCustomization(item)}>{t('order.customize')}</button>}</div>
                    ) : (
                      <button type="button" className="add-item" onClick={() => item.addonGroups.length ? openCustomization(item) : changeQuantity(item.id, 1)}>
                        <Icon name="plus" /> {item.addonGroups.length ? t('order.chooseOptions') : t('order.add')}
                      </button>
                    )}
                  </article>
                );
              })}
            </div>
          ) : (
            <div className="empty-menu"><Icon name="search" /><p>{t('order.noResults')}</p></div>
          )}
            </section>
          </div>
        </div>

        <form className="cart-panel" onSubmit={submit}>
          <div className="cart-heading">
            <div>
              <span>{t('order.cart')}</span>
              <strong>{t(itemCount === 1 ? 'order.item' : 'order.items', { count: itemCount })}</strong>
            </div>
            <Icon name="bag" />
          </div>

          <div className="cart-lines">
            {cart.length === 0 ? (
              <div className="empty-cart">
                <span><Icon name="bag" /></span>
                <strong>{t('order.emptyTitle')}</strong>
                <p>{t('order.emptyBody')}</p>
              </div>
            ) : (
              cart.map((line) => {
                const item = menuByID.get(line.menuItemId);
                if (!item) return null;
                return (
                  <div className="cart-line" key={line.menuItemId}>
                    <div>
                      <strong>{item.name}</strong>
                      <span>{formatMoney(locale, line.unitPriceMinor * line.quantity)}</span>
                      {line.addons.length > 0 && <small>{line.addons.map((addon) => `${addon.name}${addon.quantity > 1 ? ` ×${addon.quantity}` : ''}`).join(', ')}</small>}
                      {item.addonGroups.length > 0 && <button type="button" className="cart-customize" onClick={() => openCustomization(item)}>{t('order.editOptions')}</button>}
                    </div>
                    <div className="cart-stepper">
                      <button type="button" onClick={() => changeQuantity(item.id, -1)} aria-label={t('a11y.decrease', { item: item.name })}>
                        <Icon name="minus" />
                      </button>
                      <b>{line.quantity}</b>
                      <button type="button" onClick={() => changeQuantity(item.id, 1)} aria-label={t('a11y.increase', { item: item.name })}>
                        <Icon name="plus" />
                      </button>
                    </div>
                  </div>
                );
              })
            )}
          </div>

          <fieldset className="order-type-fieldset">
            <legend>{t('order.type')}</legend>
            <div className="order-type-options">
              {orderTypes.map((type) => (
                <label key={type} className={orderType === type ? 'active' : ''}>
                  <input
                    type="radio"
                    name="orderType"
                    value={type}
                    checked={orderType === type}
                    onChange={() => setOrderType(type)}
                  />
                  <Icon name={type === 'dineIn' ? 'table' : type === 'delivery' ? 'truck' : 'bag'} />
                  <span>{t(`order.type.${type}` as MessageKey)}</span>
                </label>
              ))}
            </div>
          </fieldset>

          <div className="customer-fields">
            <label>
              <span>{t('order.guest')}</span>
              <input value={guestName} onChange={(event) => setGuestName(event.target.value)} placeholder={t('order.guestPlaceholder')} />
            </label>
            {orderType === 'dineIn' && (
              <label>
                <span>{t('order.table')}</span>
                <input value={tableLabel} onChange={(event) => setTableLabel(event.target.value)} placeholder={t('order.tablePlaceholder')} />
              </label>
            )}
            <label>
              <span>{t('order.note')}</span>
              <input value={note} onChange={(event) => setNote(event.target.value)} placeholder={t('order.notePlaceholder')} />
            </label>
          </div>

          <fieldset className="order-discount-fieldset">
            <legend>{t('order.discount')}</legend>
            <div>{[0, 5, 10, 15, 20].map((percent) => <button type="button" key={percent} className={discountPercent === percent ? 'active' : ''} aria-pressed={discountPercent === percent} onClick={() => setDiscountPercent(percent)}>{percent === 0 ? t('order.noDiscount') : `${percent}%`}</button>)}</div>
          </fieldset>

          <div className="totals">
            <span>{t('order.subtotal')} <b>{formatMoney(locale, subtotal)}</b></span>
            {discount > 0 && <span className="discount-total">{t('order.discount')} <b>−{formatMoney(locale, discount)}</b></span>}
            <span>{t('order.tax')} <b>{formatMoney(locale, tax)}</b></span>
            <strong>{t('order.total')} <b>{formatMoney(locale, taxableSubtotal + tax)}</b></strong>
          </div>

          <button type="submit" className="button primary send-order" disabled={cart.length === 0 || submitting}>
            <span>{submitting ? t('order.sending') : t('order.send')}</span>
            <Icon name="arrow" />
          </button>
          </form>
        </div>
      </> : <section className="incoming-workspace" aria-label="Incoming online orders">
        <div className="connector-strip">
          <header><div><span>Integration points</span><strong>One acceptance queue</strong></div><small>Provider webhooks → FeastCloud normalizer → outlet acceptance → kitchen</small></header>
          <div>{connectorCatalog.map((provider) => {
            const installed = connectors.find((connector) => connector.provider.toLocaleLowerCase().includes(provider.key));
            const hasTraffic = incomingOrders.some((order) => summarizeIncoming(order, connectorById.get(order.connectorId)).provider.toLocaleLowerCase().includes(provider.key));
            const state = installed?.status === 'healthy' ? 'Live' : installed?.status === 'degraded' ? 'Needs attention' : hasTraffic ? 'Receiving' : 'Ready to connect';
            const mappedBrands = installed?.configuration?.externalOutlets?.filter((mapping) => mapping.active) ?? [];
            return <article key={provider.key} style={{ '--connector-color': provider.color } as CSSProperties} className={installed?.status === 'healthy' || hasTraffic ? 'is-connected' : ''}><i>{provider.name.slice(0, 1)}</i><span><b>{provider.name}</b><small>{mappedBrands.length ? `${mappedBrands.length} brands mapped · ${state}` : state}</small></span></article>;
          })}</div>
        </div>

        {import.meta.env.DEV && <aside className="incoming-simulator" aria-label="Local order simulator"><span><b>Local test simulator</b><small>Creates one test order for a randomly selected mapped Swiggy virtual brand.</small></span><button type="button" disabled={simulatorBusy} onClick={() => void simulateMappedSwiggyOrder()}>{simulatorBusy ? 'Creating test order…' : 'Simulate one random Swiggy order'}</button></aside>}

        <div className="incoming-commandbar">
          <div className="incoming-command-summary"><strong>{incomingOrders.length} total orders</strong><span>{openIncoming.length} awaiting action · {incomingOrders.filter((order) => order.status === 'accepted').length} accepted</span></div>
          <nav aria-label="Filter online orders">{(['all', 'awaiting', 'accepted', 'needs_review', 'rejected'] as const).map((filter) => {
            const count = filter === 'all' ? incomingOrders.length : filter === 'awaiting' ? incomingOrders.filter((order) => order.status === 'received').length : incomingOrders.filter((order) => order.status === filter).length;
            const label = filter === 'all' ? 'All orders' : filter === 'needs_review' ? 'Review' : filter[0].toUpperCase() + filter.slice(1);
            return <button type="button" key={filter} className={incomingFilter === filter ? 'active' : ''} aria-pressed={incomingFilter === filter} onClick={() => setIncomingFilter(filter)}>{label}<b>{count}</b></button>;
          })}</nav>
          <button type="button" onClick={() => void refreshIncoming()} disabled={!connectorApi || !!incomingBusy}>Refresh orders</button>
        </div>
        {incomingError && <p className="incoming-error" role="alert">{incomingError}</p>}
        {!connectorApi ? <div className="incoming-empty"><Icon name="wifi" /><strong>Connect Core to receive partner orders</strong><p>The common connector inbox is ready for signed webhooks and provider normalizers.</p></div> : displayedIncoming.length === 0 ? <div className="incoming-empty"><Icon name="check" /><strong>No orders in this view</strong><p>Choose All orders to see the complete partner-order history.</p></div> : <div className="incoming-grid">{displayedIncoming.map((order) => {
          const summary = summarizeIncoming(order, connectorById.get(order.connectorId));
          const provider = connectorCatalog.find((value) => summary.provider.toLocaleLowerCase().includes(value.key));
          const ageMinutes = Math.max(0, Math.floor((Date.now() - Date.parse(order.receivedAt)) / 60_000));
          return <article className="incoming-order-card" key={order.id} style={{ '--connector-color': provider?.color ?? '#294146' } as CSSProperties}>
            <header><span className="incoming-provider"><i>{summary.provider.slice(0, 1).toUpperCase()}</i><span><b>{summary.brand || summary.provider}{summary.simulated && <em className="simulation-badge">Test</em>}</b><small>{summary.brand ? `${summary.provider} · ` : ''}#{order.externalOrderId}{summary.externalOutletId ? ` · Store ${summary.externalOutletId}` : ''}</small></span></span><span className="incoming-card-state"><em className={`status-${order.status}`}>{order.status === 'received' ? 'Awaiting' : order.status === 'needs_review' ? 'Review' : order.status}</em><strong className={ageMinutes >= 5 && order.status === 'received' ? 'is-late' : ''}>{ageMinutes < 1 ? 'Just now' : `${ageMinutes} min`}</strong></span></header>
            <div className="incoming-customer"><span><small>Customer</small><b>{summary.customer}</b></span>{summary.totalMinor !== undefined && <strong>{formatMoney(locale, summary.totalMinor)}</strong>}</div>
            {(summary.phone || summary.address || summary.payment || summary.deliveryEta || summary.deliveryPartner) && <dl className="incoming-delivery-meta">
              {summary.phone && <div><dt>Phone</dt><dd>{summary.phone}</dd></div>}
              {summary.payment && <div><dt>Payment</dt><dd>{summary.payment}</dd></div>}
              {summary.deliveryEta && <div><dt>Delivery ETA</dt><dd>{summary.deliveryEta}</dd></div>}
              {summary.deliveryPartner && <div><dt>Rider</dt><dd>{summary.deliveryPartner}</dd></div>}
              {summary.address && <div className="span-two"><dt>Deliver to</dt><dd>{summary.address}</dd></div>}
            </dl>}
            <ul>{summary.lines.length > 0 ? summary.lines.map((line, index) => <li key={`${line.name}:${index}`}><b>{line.quantity}×</b><span>{line.name}{line.addons.length > 0 && <small>{line.addons.join(', ')}</small>}{line.note && <small>{line.note}</small>}</span>{line.unitPriceMinor !== undefined && <strong>{formatMoney(locale, line.unitPriceMinor * line.quantity)}</strong>}</li>) : <li className="unmapped"><span>Item mapping could not be read from this provider payload.</span></li>}</ul>
            {summary.note && <p className="incoming-note">{summary.note}</p>}
            <details className="incoming-provider-details"><summary>Provider details</summary><pre>{JSON.stringify(order.payload, null, 2)}</pre></details>
            {(order.status === 'received' || order.status === 'needs_review') ? <footer><button type="button" className="incoming-reject" disabled={!!incomingBusy} onClick={() => void decideIncoming(order, 'rejected')}>{t('commerce.inbox.reject')}</button><button type="button" className="incoming-review" disabled={!!incomingBusy} onClick={() => void decideIncoming(order, 'needs_review')}>{t('commerce.inbox.review')}</button><button type="button" className="incoming-accept" disabled={!!incomingBusy || summary.lines.length === 0} onClick={() => void acceptIncoming(order)}>{incomingBusy === order.id ? 'Accepting…' : t('commerce.inbox.accept')}</button></footer> : <footer className="incoming-resolution"><span>{order.status === 'accepted' ? 'Sent to kitchen' : order.status === 'rejected' ? 'Rejected by outlet' : 'Marked duplicate'}</span>{order.normalizedOrderId && <code>{order.normalizedOrderId}</code>}</footer>}
          </article>;
        })}</div>}
      </section>}

      {customizingItem && (
        <div className="order-customizer-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setCustomizingItemId(undefined); }}>
          <section className="order-customizer" role="dialog" aria-modal="true" aria-labelledby="order-customizer-title">
            <header>
              <div><span>{t('order.customize')}</span><h2 id="order-customizer-title">{customizingItem.name}</h2><p>{t('order.customizeHelp')}</p></div>
              <button type="button" onClick={() => setCustomizingItemId(undefined)} aria-label={t('menu.closeEditor')}>×</button>
            </header>
            <div className="order-customizer-groups">
              {customizingItem.addonGroups.map((group) => (
                <fieldset key={group.id}>
                  <legend><span>{group.name}</span><small>{group.selectionMin > 0 ? t('order.chooseRange', { min: group.selectionMin, max: group.selectionMax }) : t('order.chooseUpTo', { max: group.selectionMax })}</small></legend>
                  {group.options.map((option) => {
                    const quantity = addonSelections[group.id]?.[option.id] ?? 0;
                    const checked = quantity > 0;
                    return (
                      <div className={`order-addon-option ${checked ? 'selected' : ''}`} key={option.id}>
                        <label>
                          <input type={group.selectionMax === 1 ? 'radio' : 'checkbox'} name={`addon-${group.id}`} checked={checked} onChange={() => chooseAddon(group, option.id)} />
                          <span>{option.name}</span>
                          <b>{option.priceMinor ? `+${formatMoney(locale, option.priceMinor)}` : t('order.included')}</b>
                        </label>
                        {checked && group.selectionMax > 1 && (
                          <div className="addon-quantity">
                            <button type="button" onClick={() => changeAddonQuantity(group, option.id, -1)} aria-label={t('a11y.decrease', { item: option.name })}><Icon name="minus" /></button>
                            <strong>{quantity}</strong>
                            <button type="button" onClick={() => changeAddonQuantity(group, option.id, 1)} aria-label={t('a11y.increase', { item: option.name })}><Icon name="plus" /></button>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </fieldset>
              ))}
            </div>
            <footer>
              <div><span>{t('order.itemTotal')}</span><strong>{formatMoney(locale, customizingItem.priceMinor + selectedAddonOptions.reduce((sum, option) => sum + option.priceMinor * option.quantity, 0))}</strong></div>
              <button type="button" className="button primary" disabled={!customizationValid} onClick={saveCustomization}>{cart.some((line) => line.menuItemId === customizingItem.id) ? t('order.updateItem') : t('order.addToOrder')}</button>
            </footer>
          </section>
        </div>
      )}

      <div
        className={`toast ${confirmation ? 'visible' : ''} ${submissionFailed ? 'is-error' : ''}`}
        role={submissionFailed ? 'alert' : 'status'}
        aria-live={submissionFailed ? 'assertive' : 'polite'}
      >
        <Icon name="check" />
        <span>{confirmation}</span>
      </div>
    </section>
  );
}
