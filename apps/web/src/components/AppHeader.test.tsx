import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createTranslator } from '../i18n';
import { AppHeader } from './AppHeader';

describe('application sync status', () => {
  afterEach(cleanup);

  it('shows retained reconciliation work instead of claiming everything is synced', () => {
    const { container } = render(
      <AppHeader
        preferences={{ locale: 'en', role: 'manager', view: 'overview', compactMode: false }}
        allowedViews={['overview', 'orders', 'kds']}
        online
        syncState={{ pending: 0, quarantined: 2, syncing: false }}
        t={createTranslator('en')}
        onPreferencesChange={vi.fn()}
        onSync={vi.fn()}
      />,
    );

    expect(container).toHaveTextContent('2 changes need review');
    expect(container).not.toHaveTextContent('All changes synced');
  });

  it('makes transient edge failure visible in the status label', () => {
    const { container } = render(
      <AppHeader
        preferences={{ locale: 'en', role: 'manager', view: 'overview', compactMode: false }}
        allowedViews={['overview']}
        online
        syncState={{
          pending: 1,
          quarantined: 0,
          syncing: false,
          error: 'The outlet edge could not be reached',
        }}
        t={createTranslator('en')}
        onPreferencesChange={vi.fn()}
        onSync={vi.fn()}
      />,
    );

    expect(container).toHaveTextContent('Sync unavailable · saved locally');
  });

  it('groups modules around operator jobs and keeps primary work in the mobile dock', () => {
    const onPreferencesChange = vi.fn();
    render(
      <AppHeader
        preferences={{ locale: 'en', role: 'manager', view: 'overview', compactMode: false }}
        allowedViews={['overview', 'orders', 'kds', 'daily', 'inventory']}
        online
        syncState={{ pending: 0, quarantined: 0, syncing: false }}
        t={createTranslator('en')}
        onPreferencesChange={onPreferencesChange}
        onSync={vi.fn()}
      />,
    );

    expect(screen.getByRole('heading', { name: 'Today' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Sell' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Make' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Run' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Quick links' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'More' })).toBeInTheDocument();

    const dockOrder = screen.getAllByRole('button', { name: 'New order' }).at(-1);
    expect(dockOrder).toBeDefined();
    fireEvent.click(dockOrder!);
    expect(onPreferencesChange).toHaveBeenCalledWith({ view: 'orders' });
  });
});
