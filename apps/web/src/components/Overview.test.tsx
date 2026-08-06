import { render } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createInitialSnapshot } from '../domain/kitchen';
import type { KitchenSnapshot } from '../domain/types';
import { createTranslator } from '../i18n';
import { Overview } from './Overview';

const preferences = {
  locale: 'en',
  role: 'manager' as const,
  view: 'overview' as const,
  compactMode: false,
};

function stationCount(container: HTMLElement, label: string): string | undefined {
  const row = Array.from(container.querySelectorAll<HTMLElement>('.station-row')).find(
    (candidate) => candidate.querySelector('span')?.textContent === label,
  );
  return row?.querySelector('strong')?.textContent ?? undefined;
}

function focusedSnapshot(): KitchenSnapshot {
  const snapshot = createInitialSnapshot();
  const orderId = '01991f31-0001-7000-8000-000000000104';
  return {
    ...snapshot,
    orders: snapshot.orders.filter((order) => order.id === orderId),
    tickets: snapshot.tickets?.filter((ticket) => ticket.orderId === orderId),
  };
}

describe('overview station load', () => {
  it('uses active station-ticket evidence and includes outlet-defined stations', () => {
    const snapshot = focusedSnapshot();
    const tickets = snapshot.tickets?.map((ticket) => ticket.stationId === 'hot'
      ? { ...ticket, status: 'completed' as const }
      : { ...ticket, stationId: 'dessert-pass' });
    const { container } = render(
      <Overview
        snapshot={{ ...snapshot, tickets }}
        preferences={preferences}
        t={createTranslator('en')}
        onNavigate={vi.fn()}
      />,
    );

    expect(stationCount(container, 'Hot line')).toBe('0');
    expect(stationCount(container, 'Beverage')).toBe('0');
    expect(stationCount(container, 'Dessert pass')).toBe('1');
  });

  it('falls back to active order-line routing for a legacy snapshot without tickets', () => {
    const snapshot = focusedSnapshot();
    const { container } = render(
      <Overview
        snapshot={{ ...snapshot, tickets: undefined }}
        preferences={preferences}
        t={createTranslator('en')}
        onNavigate={vi.fn()}
      />,
    );

    expect(stationCount(container, 'Hot line')).toBe('1');
    expect(stationCount(container, 'Beverage')).toBe('1');
  });
});
