import { useMemo, useState } from 'react';
import type { SyncState, UserPreferences, View } from '../domain/types';
import { roles } from '../domain/types';
import { getLanguageOptions } from '../i18n';
import type { MessageKey } from '../i18n/messages';
import { Icon } from './Icon';
import './app-shell.css';

interface AppHeaderProps {
  preferences: UserPreferences;
  allowedViews: View[];
  online: boolean;
  syncState: SyncState;
  t: (key: MessageKey, replacements?: Record<string, string | number>) => string;
  onPreferencesChange: (patch: Partial<UserPreferences>) => void;
  onSync: () => void;
  roleLocked?: boolean;
  workspace?: { outletName: string; organizationName: string };
}

const navIcons = {
  overview: 'overview',
  orders: 'plus',
  kds: 'kitchen',
  production: 'flame',
  inventory: 'bag',
  planning: 'sparkles',
  daily: 'clock',
  commerce: 'table',
  growth: 'sparkles',
  operations: 'check',
  organization: 'overview',
  menu: 'bag',
  platform: 'sparkles',
} as const;

const navGroups: Array<{ id: string; label: MessageKey; views: View[] }> = [
  { id: 'today', label: 'shell.group.today', views: ['overview'] },
  { id: 'sell', label: 'shell.group.sell', views: ['orders', 'commerce'] },
  { id: 'make', label: 'shell.group.make', views: ['kds', 'production'] },
  { id: 'stock', label: 'shell.group.stock', views: ['inventory', 'planning'] },
  { id: 'run', label: 'shell.group.run', views: ['daily', 'operations'] },
  { id: 'grow', label: 'shell.group.grow', views: ['growth'] },
  { id: 'control', label: 'shell.group.control', views: ['organization'] },
  { id: 'menu', label: 'shell.group.menu', views: ['menu'] },
  { id: 'platform', label: 'shell.group.platform', views: ['platform'] },
];

export function AppHeader({
  preferences,
  allowedViews,
  online,
  syncState,
  t,
  onPreferencesChange,
  onSync,
  roleLocked = false,
  workspace,
}: AppHeaderProps) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [navQuery, setNavQuery] = useState('');
  const languageOptions = getLanguageOptions();
  const pendingKey = syncState.pending === 1 ? 'sync.pending' : 'sync.pendingMany';
  const reviewKey = syncState.quarantined === 1 ? 'sync.review' : 'sync.reviewMany';
  const hasSyncError = Boolean(syncState.error);
  const syncLabel = syncState.quarantined > 0
    ? t(reviewKey, { count: syncState.quarantined })
    : hasSyncError
      ? t('sync.unavailable')
      : !online
        ? t('sync.offline')
        : syncState.syncing
          ? t('sync.syncing')
          : syncState.pending > 0
            ? t(pendingKey, { count: syncState.pending })
            : t('sync.synced');
  const attentionCount = syncState.pending + syncState.quarantined;
  const currentViewLabel = t(`nav.${preferences.view}` as MessageKey);
  const currentRoleLabel = t(`role.${preferences.role}` as MessageKey);

  const visibleGroups = useMemo(() => {
    const query = navQuery.trim().toLocaleLowerCase(preferences.locale);

    return navGroups.flatMap((group) => {
      const views = group.views.filter((view) => {
        if (!allowedViews.includes(view)) return false;
        if (!query) return true;
        return t(`nav.${view}` as MessageKey).toLocaleLowerCase(preferences.locale).includes(query);
      });

      return views.length > 0 ? [{ ...group, views }] : [];
    });
  }, [allowedViews, navQuery, preferences.locale, t]);

  const selectView = (view: View) => {
    onPreferencesChange({ view });
    setDrawerOpen(false);
  };

  return (
    <>
      <a className="skip-link" href="#main-content">
        {t('a11y.skip')}
      </a>

      <header className="app-header">
        <button
          type="button"
          className="shell-menu-button"
          aria-label={t('shell.openNavigation')}
          aria-controls="feastcloud-navigation"
          aria-expanded={drawerOpen}
          onClick={() => setDrawerOpen(true)}
        >
          <span />
          <span />
          <span />
        </button>

        <div className="outlet-selector">
          <span className={`outlet-live ${online ? '' : 'is-offline'}`} aria-hidden="true" />
          <label>
            <span className="sr-only">{t('shell.outlet')}</span>
            <select aria-label={t('shell.outlet')} value="current" onChange={(event) => {
              if (event.target.value === 'organization') onPreferencesChange({ view: 'organization' });
            }}>
              <option value="current">{workspace?.outletName ?? t('app.outlet')}</option>
              {allowedViews.includes('organization') && <option value="organization">{t('shell.manageOutlets')}</option>}
            </select>
            <small>{workspace?.organizationName ?? t('app.brand')}</small>
          </label>
        </div>

        <label className="module-search">
          <span className="sr-only">{t('shell.searchModules')}</span>
          <Icon name="search" />
          <input
            type="search"
            value={navQuery}
            placeholder={t('shell.searchPlaceholder')}
            aria-label={t('shell.searchModules')}
            onChange={(event) => setNavQuery(event.target.value)}
          />
        </label>

        <div className="header-actions">
          <button
            type="button"
            className={`sync-pill ${online ? 'is-online' : 'is-offline'} ${hasSyncError || syncState.quarantined > 0 ? 'is-error' : ''}`}
            onClick={onSync}
            title={syncState.error ?? (syncState.quarantined > 0 ? syncLabel : t('sync.saved'))}
            aria-label={syncLabel}
            aria-live="polite"
          >
            <Icon name={online && !hasSyncError ? 'wifi' : 'offline'} />
            <span>{syncLabel}</span>
            {attentionCount > 0 && <b>{attentionCount}</b>}
          </button>

          <label className="compact-select language-select">
            <span className="sr-only">{t('a11y.language')}</span>
            <select
              aria-label={t('a11y.language')}
              value={preferences.locale}
              onChange={(event) =>
                onPreferencesChange({ locale: event.target.value as UserPreferences['locale'] })
              }
            >
              {languageOptions.map((language) => (
                <option key={language.locale} value={language.locale}>
                  {language.name}
                </option>
              ))}
            </select>
          </label>

          <div className="user-control">
            <span className="user-avatar" aria-hidden="true">{currentRoleLabel.slice(0, 1)}</span>
            {roleLocked ? (
              <span className="compact-select locked-role" aria-label={t('a11y.role')}>
                <small>{t('a11y.role')}</small>
                <strong>{currentRoleLabel}</strong>
              </span>
            ) : (
              <label className="compact-select role-select">
                <span className="sr-only">{t('a11y.role')}</span>
                <small>{t('a11y.role')}</small>
                <select
                  aria-label={t('a11y.role')}
                  value={preferences.role}
                  onChange={(event) =>
                    onPreferencesChange({ role: event.target.value as UserPreferences['role'] })
                  }
                >
                  {roles.map((role) => (
                    <option key={role} value={role}>
                      {t(`role.${role}` as MessageKey)}
                    </option>
                  ))}
                </select>
              </label>
            )}
          </div>
        </div>
      </header>

      <button
        type="button"
        className={`shell-drawer-scrim ${drawerOpen ? 'is-visible' : ''}`}
        aria-label={t('shell.closeNavigation')}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={() => setDrawerOpen(false)}
      />

      <aside
        id="feastcloud-navigation"
        className={`primary-nav ${drawerOpen ? 'is-open' : ''} ${preferences.compactMode ? 'is-compact' : ''}`}
        aria-label={t('a11y.navigation')}
      >
        <div className="sidebar-brand">
          <div className="brand-lockup" aria-label={`${t('app.name')} ${t('app.product')}`}>
            <span className="brand-mark">
              <img src="/icons/feastcloud.svg" alt="" aria-hidden="true" />
            </span>
            <span>
              <strong>{t('app.name')}</strong>
              <small>{t('app.product')}</small>
            </span>
          </div>
          <button type="button" className="drawer-close" aria-label={t('shell.closeNavigation')} onClick={() => setDrawerOpen(false)}>
            ×
          </button>
        </div>

        <div className="sidebar-context">
          <small>{t('app.outlet')}</small>
          <strong>{currentViewLabel}</strong>
        </div>

        <nav className="shell-nav" aria-label={t('a11y.navigation')}>
          {visibleGroups.map((group) => (
            <section className="nav-group" key={group.id} aria-labelledby={`group-${group.id}`}>
              <h2 id={`group-${group.id}`}>{t(group.label)}</h2>
              <div
                id={`group-items-${group.id}`}
                className="nav-group-items"
              >
                {group.views.map((view) => (
                  <button
                    key={view}
                    type="button"
                    className={preferences.view === view ? 'active' : ''}
                    aria-current={preferences.view === view ? 'page' : undefined}
                    onClick={() => selectView(view)}
                  >
                    <span className="nav-icon"><Icon name={navIcons[view]} /></span>
                    <span>{t(`nav.${view}` as MessageKey)}</span>
                    <Icon className="nav-arrow" name="arrow" />
                  </button>
                ))}
              </div>
            </section>
          ))}
          {visibleGroups.length === 0 && <p className="nav-empty">{t('shell.noModules')}</p>}
        </nav>

        <div className={`sidebar-status ${online ? 'is-online' : 'is-offline'}`}>
          <span aria-hidden="true" />
          <div>
            <strong>{syncLabel}</strong>
            <small>{t('sync.saved')}</small>
          </div>
        </div>
      </aside>

      <nav className="mobile-service-dock" aria-label={t('a11y.navigation')}>
        {(['overview', 'orders', 'kds'] as View[]).filter((view) => allowedViews.includes(view)).map((view) => (
          <button
            type="button"
            key={view}
            className={preferences.view === view ? 'active' : ''}
            aria-current={preferences.view === view ? 'page' : undefined}
            onClick={() => selectView(view)}
          >
            <Icon name={navIcons[view]} />
            <span>{t(`nav.${view}` as MessageKey)}</span>
          </button>
        ))}
        <button type="button" aria-expanded={drawerOpen} aria-controls="feastcloud-navigation" onClick={() => setDrawerOpen(true)}>
          <Icon name="sparkles" />
          <span>{t('shell.more')}</span>
        </button>
      </nav>
    </>
  );
}
