import { act, cleanup, render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createInitialSnapshot, createOrder } from '../domain/kitchen';
import { MENU_IMPORT_UPDATED_EVENT } from '../domain/coreCommerce';
import { createTranslator } from '../i18n';
import { OrderEntry } from './OrderEntry';

describe('order entry', () => {
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
  it('builds and sends a ticket using touch controls', async () => {
    const created = createOrder(createInitialSnapshot(), {
      type: 'takeaway',
      lines: [{ menuItemId: 'butter-chicken-bowl', quantity: 1 }],
    }).order;
    const onSubmit = vi.fn().mockResolvedValue(created);

    const { container } = render(
      <OrderEntry locale="en" t={createTranslator('en')} onSubmit={onSubmit} />,
    );
    const element = container as HTMLElement;

    const buttons = Array.from(element.querySelectorAll<HTMLButtonElement>('button'));
    const add = buttons.find((button) => button.textContent?.includes('Add'));
    expect(add).toBeDefined();
    await act(async () => add?.click());

    const send = Array.from(element.querySelectorAll<HTMLButtonElement>('button')).find((button) =>
      button.textContent?.includes('Send to kitchen'),
    );
    expect(send).toBeDefined();
    await act(async () => send?.click());

    expect(element).toHaveTextContent(/saved and queued for sync/i);
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'takeaway',
        lines: [{ menuItemId: 'butter-chicken-bowl', quantity: 1 }],
      }),
    );
  });

  it('keeps the cart open and reports a durability failure', async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error('Quota exceeded'));
    const { container } = render(
      <OrderEntry locale="en" t={createTranslator('en')} onSubmit={onSubmit} />,
    );
    const element = container as HTMLElement;
    const add = Array.from(element.querySelectorAll<HTMLButtonElement>('button')).find((button) =>
      button.textContent?.includes('Add'),
    );
    await act(async () => add?.click());
    const send = Array.from(element.querySelectorAll<HTMLButtonElement>('button')).find((button) =>
      button.textContent?.includes('Send to kitchen'),
    );

    await act(async () => send?.click());

    expect(element.querySelector('[role="alert"]')).toHaveTextContent(/order not saved/i);
    expect(element).toHaveTextContent('1 item');
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it('refreshes an open order screen when an active import changes', async () => {
    let itemName = 'Imported Dal';
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ data: [{
      id: 'import-1',
      outletId: 'outlet-1',
      name: 'Live import',
      itemFileName: 'items.csv',
      sourceSha256: 'a'.repeat(64),
      status: 'applied',
      itemCount: 1,
      categoryCount: 1,
      addonGroupCount: 0,
      variationCount: 0,
      importedAt: new Date().toISOString(),
      draft: {
        items: [{ sourceLine: 2, name: itemName, onlineName: itemName, description: '', code: 'DAL', category: 'Mains', onlineCategory: 'Mains', priceMinor: 20000, dietaryLabel: 'veg', rank: 1, stationId: 'unassigned', prepMinutes: 9, addOnGroupNames: [], addonBindings: [], variations: [] }],
        addonGroups: [], categories: ['Mains'], variationCount: 0, warnings: [],
      },
    }] }), { status: 200, headers: { 'Content-Type': 'application/json' } })));
    const onSubmit = vi.fn();
    const { container } = render(<OrderEntry locale="en" tenantId="tenant-1" outletId="outlet-1" t={createTranslator('en')} onSubmit={onSubmit} />);

    await waitFor(() => expect(container).toHaveTextContent('Imported Dal'));
    itemName = 'Edited Dal';
    await act(async () => window.dispatchEvent(new CustomEvent(MENU_IMPORT_UPDATED_EVENT, { detail: { tenant: 'tenant-1', outlet: 'outlet-1' } })));
    await waitFor(() => expect(container).toHaveTextContent('Edited Dal'));
  });

  it('selects imported add-ons and applies a discount before submitting', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ data: [{
      id: 'import-options', outletId: 'outlet-1', name: 'Menu with options', itemFileName: 'items.csv', addonFileName: 'addons.csv',
      sourceSha256: 'b'.repeat(64), status: 'applied', itemCount: 1, categoryCount: 1, addonGroupCount: 2, variationCount: 0,
      importedAt: new Date().toISOString(),
      draft: {
        items: [{ sourceLine: 2, name: 'Paneer Roll', onlineName: 'Paneer Roll', description: 'Fresh roll', code: 'ROLL', category: 'Rolls', onlineCategory: 'Rolls', priceMinor: 20000, dietaryLabel: 'veg', rank: 1, stationId: 'hot', prepMinutes: 8, addOnGroupNames: ['EXTRAS_SOURCE', 'SIDES_SOURCE'], addonBindings: [], variations: [] }],
        addonGroups: [
          { sourceId: 'EXTRAS_SOURCE', name: 'Extras', onlineName: 'Extras', selectionMin: 1, selectionMax: 2, selection: 'multiple', showInOnline: true, showInPos: true, options: [{ name: 'Cheese', priceMinor: 5000, dietaryLabel: 'veg', rank: 1, active: true }] },
          { sourceId: 'SIDES_SOURCE', name: 'Sides', onlineName: 'Choose a side', selectionMin: 0, selectionMax: 1, selection: 'single', showInOnline: true, showInPos: true, options: [{ name: 'Fries', priceMinor: 4000, dietaryLabel: 'veg', rank: 1, active: true }] },
        ],
        categories: ['Rolls'], variationCount: 0, warnings: [],
      },
    }] }), { status: 200, headers: { 'Content-Type': 'application/json' } })));
    const created = createOrder(createInitialSnapshot(), { type: 'takeaway', lines: [{ menuItemId: 'butter-chicken-bowl', quantity: 1 }] }).order;
    const onSubmit = vi.fn().mockResolvedValue(created);
    const view = render(<OrderEntry locale="en" tenantId="tenant-1" outletId="outlet-1" t={createTranslator('en')} onSubmit={onSubmit} />);

    await waitFor(() => expect(view.getByText('Paneer Roll')).toBeInTheDocument());
    await act(async () => view.getByRole('button', { name: /choose options/i }).click());
    expect(view.getByRole('dialog')).toBeInTheDocument();
    expect(view.getByRole('group', { name: /Choose a side/i })).toBeInTheDocument();
    await act(async () => view.getByRole('checkbox', { name: /Cheese/i }).click());
    await act(async () => view.getByRole('button', { name: /increase.*Cheese/i }).click());
    expect(view.getByRole('dialog')).toHaveTextContent('2');
    await act(async () => view.getByRole('button', { name: /add to order/i }).click());
    expect(view.getByText('Cheese ×2')).toBeInTheDocument();
    await act(async () => view.getByRole('button', { name: '10%' }).click());
    await act(async () => view.getByRole('button', { name: /send to kitchen/i }).click());

    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      note: expect.stringContaining('10% discount'),
      lines: [expect.objectContaining({ menuItemId: 'import:import-options:ROLL', quantity: 1, note: 'Add-ons: Cheese ×2' })],
    }));
  });

  it('simulates one order for a random mapped Swiggy brand and accepts it into the kitchen flow', async () => {
    const mappings = [
      ['1211361', 'Dalchini Kitchen, Sarjapura'],
      ['1313330', 'Daily Plates, Anekal'],
      ['1366703', 'House Of Dragon, Anekal'],
      ['1392334', 'Grain And Gravy, Anekal'],
    ].map(([externalOutletId, brandName]) => ({ externalOutletId, brandName, active: true }));
    const inbox: Array<Record<string, unknown>> = [];
    const canonicalOrders: Array<Record<string, unknown>> = [];
    const calls: Array<{ url: string; method: string; payload?: Record<string, unknown> }> = [];
    const importedMenu = [{
      id: 'import-swiggy', outletId: 'outlet-1', name: 'Swiggy test menu', itemFileName: 'items.csv', sourceSha256: 'c'.repeat(64),
      status: 'applied', itemCount: 1, categoryCount: 1, addonGroupCount: 1, variationCount: 0, importedAt: new Date().toISOString(),
      draft: {
        items: [{ sourceLine: 2, name: 'Paneer Roll', onlineName: 'Paneer Roll', description: 'Fresh roll', code: 'ROLL', category: 'Rolls', onlineCategory: 'Rolls', priceMinor: 20000, dietaryLabel: 'veg', rank: 1, stationId: 'hot', prepMinutes: 8, addOnGroupNames: ['EXTRAS'], addonBindings: [], variations: [] }],
        addonGroups: [{ sourceId: 'EXTRAS', name: 'Extras', onlineName: 'Extras', selectionMin: 0, selectionMax: 2, selection: 'multiple', showInOnline: true, showInPos: true, options: [{ name: 'Cheese', priceMinor: 5000, dietaryLabel: 'veg', rank: 1, active: true }] }],
        categories: ['Rolls'], variationCount: 0, warnings: [],
      },
    }];
    const connector = { id: 'connector-swiggy', provider: 'Swiggy', manifestVersion: '1.0.0', capabilities: ['orders.read', 'orders.accept'], configuration: { externalOutlets: mappings }, status: 'draft' };
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? 'GET';
      const envelope = init?.body ? JSON.parse(String(init.body)) as { payload: Record<string, unknown> } : undefined;
      calls.push({ url, method, payload: envelope?.payload });
      if (url.includes('/connector-order-inbox/stream')) return new Response('', { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
      if (method === 'GET' && url.includes('/menu-imports')) return Response.json({ data: importedMenu });
      if (method === 'GET' && url.includes('/connector-installations')) return Response.json({ data: [connector] });
      if (method === 'GET' && url.includes('/connector-order-inbox')) return Response.json({ data: inbox });
      if (method === 'POST' && url.endsWith('/connector-order-inbox')) {
        const payload = envelope!.payload;
        const order = { id: payload.id, connectorId: payload.connectorId, externalOrderId: payload.externalOrderId, payload: payload.payload, status: 'received', receivedAt: new Date().toISOString() };
        inbox.unshift(order);
        return Response.json({ data: order }, { status: 201 });
      }
      if (method === 'GET' && /\/orders\?/.test(url)) return Response.json({ data: canonicalOrders });
      if (method === 'POST' && url.endsWith('/orders')) {
        canonicalOrders.push(envelope!.payload);
        return Response.json({ data: envelope!.payload }, { status: 201 });
      }
      if (method === 'POST' && url.includes('/decisions')) {
        const inboxId = url.split('/').at(-2);
        const order = inbox.find((candidate) => candidate.id === inboxId)!;
        order.status = envelope!.payload.decision;
        order.normalizedOrderId = envelope!.payload.normalizedOrderId;
        return Response.json({ data: order });
      }
      return Response.json({ data: [] });
    }));
    let kitchenSnapshot = createInitialSnapshot();
    let acceptedOrderId = '';
    const onSubmit = vi.fn(async (input: Parameters<typeof createOrder>[1]) => {
      const result = createOrder(kitchenSnapshot, input);
      kitchenSnapshot = result.snapshot;
      acceptedOrderId = result.order.id;
      return result.order;
    });
    const view = render(<OrderEntry locale="en" tenantId="tenant-1" outletId="outlet-1" apiBase="http://core.test/api/v1" t={createTranslator('en')} onSubmit={onSubmit} />);

    vi.spyOn(Math, 'random').mockReturnValue(0.5);
    await act(async () => view.getByRole('button', { name: /online orders/i }).click());
    await waitFor(() => expect(view.getByRole('button', { name: /simulate random swiggy order/i })).toBeEnabled());
    await act(async () => view.getByRole('button', { name: /simulate random swiggy order/i }).click());

    await waitFor(() => expect(view.getAllByText('Test')).toHaveLength(1));
    expect(view.getByText(mappings[2]!.brandName)).toBeVisible();
    expect(calls.filter((call) => call.method === 'POST' && call.url.endsWith('/connector-order-inbox'))).toHaveLength(1);
    expect(calls.some((call) => call.method === 'POST' && call.url.endsWith('/connector-order-inbox') && (call.payload?.payload as Record<string, unknown>)?.externalOutletId === mappings[2]!.externalOutletId)).toBe(true);
    await act(async () => view.getAllByRole('button', { name: /accept to kitchen/i })[0].click());

    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce());
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      type: 'delivery',
      note: 'LOCAL TEST ORDER — do not prepare',
      aggregator: expect.objectContaining({ provider: 'Swiggy', brandName: expect.any(String), externalOrderId: expect.stringMatching(/^SIM-/) }),
      lines: expect.arrayContaining([expect.objectContaining({ note: expect.stringContaining('Add-ons: Cheese') })]),
    }));
    expect(onSubmit.mock.calls[0]?.[0].note).not.toContain('Partner order');
    expect(kitchenSnapshot.orders.find((order) => order.id === acceptedOrderId)?.lines.some((line) => line.note?.includes('Add-ons: Cheese'))).toBe(true);
    expect(kitchenSnapshot.tickets?.some((ticket) => ticket.orderId === acceptedOrderId && ticket.status === 'queued')).toBe(true);
    expect(canonicalOrders).toHaveLength(1);
    expect((canonicalOrders[0]?.lines as Array<{ menuItemId?: string }>).every((line) => !line.menuItemId)).toBe(true);
    expect(calls.some((call) => call.method === 'POST' && call.url.includes('/decisions') && call.payload?.decision === 'accepted' && call.payload.normalizedOrderId === acceptedOrderId)).toBe(true);
    await waitFor(() => expect(view.getByText('Sent to kitchen')).toBeVisible());

  });
});
