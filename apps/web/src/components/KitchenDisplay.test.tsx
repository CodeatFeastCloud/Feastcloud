import { act, cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createInitialSnapshot, createOrder } from '../domain/kitchen';
import { createTranslator } from '../i18n';
import { KitchenDisplay } from './KitchenDisplay';

describe('kitchen station actions', () => {
  afterEach(() => cleanup());

  it('keeps the kitchen chime armed for newly queued orders', async () => {
    const audioInstances: Array<{ play: ReturnType<typeof vi.fn> }> = [];
    class TestAudio {
      preload = '';
      volume = 1;
      play = vi.fn().mockResolvedValue(undefined);
      addEventListener = vi.fn();
      constructor(readonly source: string) {
        audioInstances.push(this);
      }
      pause = vi.fn();
      removeAttribute = vi.fn();
      load = vi.fn();
    }
    Object.defineProperty(window, 'Audio', {
      configurable: true,
      value: TestAudio,
    });

    const snapshot = createInitialSnapshot();
    const onAdvanceOrder = vi.fn().mockResolvedValue(undefined);
    const onAdvanceTicket = vi.fn().mockResolvedValue(undefined);
    const { container, rerender } = render(
      <KitchenDisplay
        snapshot={snapshot}
        locale="en"
        t={createTranslator('en')}
        onAdvanceOrder={onAdvanceOrder}
        onAdvanceTicket={onAdvanceTicket}
      />,
    );

    const next = createOrder(snapshot, {
      type: 'takeaway',
      lines: [{ menuItemId: 'mango-lassi', quantity: 1 }],
    }).snapshot;
    await act(async () => rerender(
      <KitchenDisplay
        snapshot={next}
        locale="en"
        t={createTranslator('en')}
        onAdvanceOrder={onAdvanceOrder}
        onAdvanceTicket={onAdvanceTicket}
      />,
    ));

    expect(audioInstances).toHaveLength(1);
    expect(container).toHaveTextContent(/new order/i);
    expect(audioInstances.every((audio) => audio.play.mock.calls.length === 1)).toBe(true);
  });

  it('shows aggregator brand and platform number while reserving notes for special instructions', () => {
    const empty = { ...createInitialSnapshot(), orders: [], tickets: [], nextOrderNumber: 1 };
    const snapshot = createOrder(empty, {
      type: 'delivery',
      note: 'No onion. Pack cutlery separately.',
      aggregator: { provider: 'Swiggy', brandName: 'House Of Dragon, Anekal', externalOrderId: 'SW-887761', externalOutletId: '1366703' },
      lines: [{ menuItemId: 'connector-item', name: 'Veg Noodles', stationId: 'hot', quantity: 2, note: 'Add-ons: Extra sauce' }],
    }).snapshot;
    const view = render(<KitchenDisplay snapshot={snapshot} locale="en" t={createTranslator('en')} onAdvanceOrder={vi.fn()} onAdvanceTicket={vi.fn()} />);

    expect(view.getByText('House Of Dragon, Anekal')).toBeVisible();
    expect(view.getByText(/Swiggy/i)).toBeVisible();
    expect(view.getByText(/Order #SW-887761/i)).toBeVisible();
    expect(view.getByText('Veg Noodles')).toBeVisible();
    expect(view.getByText('2')).toBeVisible();
    expect(view.getByText(/Add-ons: Extra sauce/)).toBeVisible();
    expect(view.getByText(/Special instructions:/).parentElement).toHaveTextContent('No onion. Pack cutlery separately.');
    expect(view.container).not.toHaveTextContent('Partner order SW-887761');
  });

  it('shows a live elapsed timer, can pause kitchen alerts, and reads a ticket aloud', async () => {
    const snapshot = createInitialSnapshot();
    const speech = { cancel: vi.fn(), speak: vi.fn() } as unknown as SpeechSynthesis;
    class TestUtterance {
      lang = '';
      rate = 1;
      constructor(readonly text: string) {}
    }
    Object.defineProperty(window, 'speechSynthesis', { configurable: true, value: speech });
    Object.defineProperty(window, 'SpeechSynthesisUtterance', { configurable: true, value: TestUtterance });

    const { container, getByRole, getAllByRole } = render(
      <KitchenDisplay
        snapshot={snapshot}
        locale="en"
        t={createTranslator('en')}
        onAdvanceOrder={vi.fn().mockResolvedValue(undefined)}
        onAdvanceTicket={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    expect(container.querySelector('time')?.textContent).toMatch(/^\d{2}:\d{2}$/);
    await act(async () => getByRole('button', { name: /read order.*order #104/i }).click());
    expect(speech.cancel).toHaveBeenCalledOnce();
    expect(speech.speak).toHaveBeenCalledOnce();
    const snooze = getAllByRole('button', { name: 'Snooze alert 5m' })[0];
    await act(async () => snooze.click());
    expect(snooze).toHaveAttribute('aria-pressed', 'true');
    expect(container).toHaveTextContent(/kitchen alert paused for 5 minutes/i);
  });

  it('queues a spoken order readout when a ticket becomes accepted', async () => {
    const snapshot = createInitialSnapshot();
    const queuedTicket = snapshot.tickets?.find((ticket) => ticket.status === 'queued');
    if (!queuedTicket) throw new Error('test fixture is missing a queued ticket');
    const speech = { cancel: vi.fn(), speak: vi.fn() } as unknown as SpeechSynthesis;
    class TestUtterance {
      lang = '';
      rate = 1;
      constructor(readonly text: string) {}
    }
    Object.defineProperty(window, 'speechSynthesis', { configurable: true, value: speech });
    Object.defineProperty(window, 'SpeechSynthesisUtterance', { configurable: true, value: TestUtterance });
    const props = {
      locale: 'en',
      t: createTranslator('en'),
      onAdvanceOrder: vi.fn().mockResolvedValue(undefined),
      onAdvanceTicket: vi.fn().mockResolvedValue(undefined),
    } as const;
    const { rerender } = render(<KitchenDisplay snapshot={snapshot} {...props} />);
    const accepted = {
      ...snapshot,
      tickets: snapshot.tickets?.map((ticket) => ticket.id === queuedTicket.id
        ? { ...ticket, status: 'fired' as const, version: ticket.version + 1 }
        : ticket),
    };

    await act(async () => rerender(<KitchenDisplay snapshot={accepted} {...props} />));

    expect(speech.cancel).not.toHaveBeenCalled();
    expect(speech.speak).toHaveBeenCalledOnce();
  });

  it('advances only the selected station ticket', async () => {
    const snapshot = createInitialSnapshot();
    const onAdvanceOrder = vi.fn().mockResolvedValue(undefined);
    const onAdvanceTicket = vi.fn().mockResolvedValue(undefined);
    const { container } = render(
      <KitchenDisplay
        snapshot={snapshot}
        locale="en"
        t={createTranslator('en')}
        onAdvanceOrder={onAdvanceOrder}
        onAdvanceTicket={onAdvanceTicket}
      />,
    );
    const hotFilter = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent === 'Hot line',
    );

    await act(async () => hotFilter?.click());

    const action = container.querySelector<HTMLButtonElement>('.kds-ticket footer button');
    expect(action).not.toBeNull();
    expect(action?.disabled).toBe(false);
    await act(async () => action?.click());

    const hotTicketIds = new Set(
      snapshot.tickets?.filter((ticket) => ticket.stationId === 'hot').map((ticket) => ticket.id),
    );
    expect(hotTicketIds.has(onAdvanceTicket.mock.calls[0]?.[0] as string)).toBe(true);
    expect(onAdvanceOrder).not.toHaveBeenCalled();
    expect(container).toHaveTextContent(/actions update only this station ticket/i);
  });

  it('retains whole-order progression in the all-stations view', async () => {
    const snapshot = createInitialSnapshot();
    const onAdvanceOrder = vi.fn().mockResolvedValue(undefined);
    const onAdvanceTicket = vi.fn().mockResolvedValue(undefined);
    const { container } = render(
      <KitchenDisplay
        snapshot={snapshot}
        locale="en"
        t={createTranslator('en')}
        onAdvanceOrder={onAdvanceOrder}
        onAdvanceTicket={onAdvanceTicket}
      />,
    );
    const action = container.querySelector<HTMLButtonElement>('.kds-ticket footer button');

    await act(async () => action?.click());

    expect(onAdvanceOrder).toHaveBeenCalledWith(expect.any(String));
    expect(onAdvanceTicket).not.toHaveBeenCalled();
  });

  it('blocks an invalid whole-order jump when stations are at different steps', () => {
    const snapshot = createInitialSnapshot();
    const orderId = '01991f31-0001-7000-8000-000000000104';
    const mixed = {
      ...snapshot,
      tickets: snapshot.tickets?.map((ticket) =>
        ticket.orderId === orderId && ticket.stationId === 'beverage'
          ? { ...ticket, status: 'fired' as const }
          : ticket),
    };
    const { container } = render(
      <KitchenDisplay
        snapshot={mixed}
        locale="en"
        t={createTranslator('en')}
        onAdvanceOrder={vi.fn().mockResolvedValue(undefined)}
        onAdvanceTicket={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    const orderCard = Array.from(container.querySelectorAll<HTMLElement>('.kds-ticket')).find(
      (card) => card.textContent?.includes('#104'),
    );
    expect(orderCard?.querySelector<HTMLButtonElement>('footer button')).toBeDisabled();
    expect(orderCard).toHaveTextContent(/stations are at different steps/i);
  });

  it('shows and operates an outlet-defined station filter', async () => {
    const snapshot = createInitialSnapshot();
    const ticket = snapshot.tickets?.[0];
    if (!ticket) throw new Error('test fixture is missing a kitchen ticket');
    const stationId = 'dessert-pass';
    const custom = {
      ...snapshot,
      orders: snapshot.orders.map((order) => order.id === ticket.orderId
        ? {
            ...order,
            lines: order.lines.map((line) => ticket.lineIds.includes(line.id)
              ? { ...line, stationId }
              : line),
          }
        : order),
      tickets: snapshot.tickets?.map((candidate) => candidate.id === ticket.id
        ? { ...candidate, stationId }
        : candidate),
    };
    const onAdvanceTicket = vi.fn().mockResolvedValue(undefined);
    const { container } = render(
      <KitchenDisplay
        snapshot={custom}
        locale="en"
        t={createTranslator('en')}
        onAdvanceOrder={vi.fn().mockResolvedValue(undefined)}
        onAdvanceTicket={onAdvanceTicket}
      />,
    );
    const filter = Array.from(container.querySelectorAll<HTMLButtonElement>('.station-filters button')).find(
      (button) => button.textContent === 'Dessert pass',
    );

    expect(filter).toBeDefined();
    await act(async () => filter?.click());
    const card = container.querySelector<HTMLElement>('.kds-ticket');
    expect(card).toHaveTextContent('Dessert pass');
    await act(async () => card?.querySelector<HTMLButtonElement>('footer button')?.click());
    expect(onAdvanceTicket).toHaveBeenCalledWith(ticket.id);
  });
});
