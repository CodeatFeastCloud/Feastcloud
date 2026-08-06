import { FormEvent, lazy, Suspense, useEffect, useMemo, useState } from 'react';
import { AppHeader } from './components/AppHeader';
import { fetchWorkspaceIdentity, organizationApiBase, type WorkspaceIdentity } from './domain/coreOrganization';
import { useKitchenSystem } from './hooks/useKitchenSystem';
import { createTranslator } from './i18n';

const KitchenDisplay = lazy(() => import('./components/KitchenDisplay').then((module) => ({ default: module.KitchenDisplay })));
const OrderEntry = lazy(() => import('./components/OrderEntry').then((module) => ({ default: module.OrderEntry })));
const ExecutiveDashboard = lazy(() => import('./components/ExecutiveDashboard').then((module) => ({ default: module.ExecutiveDashboard })));
const InventoryDashboard = lazy(() => import('./components/InventoryDashboard').then((module) => ({ default: module.InventoryDashboard })));
const ProductionBoard = lazy(() => import('./components/ProductionBoard').then((module) => ({ default: module.ProductionBoard })));
const PlanningHub = lazy(() => import('./components/PlanningHub').then((module) => ({ default: module.PlanningHub })));
const OperationsControl = lazy(() => import('./components/OperationsControl').then((module) => ({ default: module.OperationsControl })));
const DailyOperations = lazy(() => import('./components/DailyOperations').then((module) => ({ default: module.DailyOperations })));
const CommerceHub = lazy(() => import('./components/CommerceHub').then((module) => ({ default: module.CommerceHub })));
const GuestGrowthHub = lazy(() => import('./components/GuestGrowthHub').then((module) => ({ default: module.GuestGrowthHub })));
const PublicOrdering = lazy(() => import('./components/PublicOrdering').then((module) => ({ default: module.PublicOrdering })));
const OrganizationControl = lazy(() => import('./components/OrganizationControl').then((module) => ({ default: module.OrganizationControl })));
const PlatformOnboarding = lazy(() => import('./components/PlatformOnboarding').then((module) => ({ default: module.PlatformOnboarding })));
const MenuStudio = lazy(() => import('./components/MenuStudio').then((module) => ({ default: module.MenuStudio })));

function AppLoading({ label }: { label: string }) {
  return <div className="loading-state" role="status"><span className="loading-mark" /><p>{label}</p></div>;
}

export default function App() {
  const kitchen = useKitchenSystem();
  const [workspace, setWorkspace] = useState<WorkspaceIdentity>();
  const [pairingCode, setPairingCode] = useState('');
  const [pairingError, setPairingError] = useState('');
  const [pairing, setPairing] = useState(false);
  const t = useMemo(() => createTranslator(kitchen.preferences.locale), [kitchen.preferences.locale]);
  const publicSlug = window.location.pathname.match(/^\/order\/([A-Za-z0-9_-]{16,96})$/)?.[1];
  const incomingFlow = new URLSearchParams(window.location.search).get('flow') === 'incoming';
  const organizationApi = useMemo(() => organizationApiBase((import.meta.env.VITE_CORE_URL as string | undefined)?.trim()), []);

  useEffect(() => {
    document.title = `${t('app.name')} · ${t('app.product')}`;
  }, [t]);

  useEffect(() => {
    if (!organizationApi) return;
    let current = true;
    void fetchWorkspaceIdentity(organizationApi, kitchen.snapshot.organizationId, kitchen.snapshot.outletId)
      .then((identity) => { if (current) setWorkspace(identity); })
      .catch(() => { /* An offline outlet retains its local, safe fallback label. */ });
    return () => { current = false; };
  }, [kitchen.snapshot.organizationId, kitchen.snapshot.outletId, organizationApi]);

  const submitPairing = async (event: FormEvent) => {
    event.preventDefault();
    setPairing(true);
    setPairingError('');
    try {
      await kitchen.pairWithEdge(pairingCode);
    } catch {
      setPairingError(t('pairing.invalid'));
    } finally {
      setPairing(false);
    }
  };

  if (publicSlug) {
    return <Suspense fallback={<AppLoading label={t('common.loading')} />}><PublicOrdering slug={publicSlug} locale={kitchen.preferences.locale} t={t} /></Suspense>;
  }

  if (kitchen.pairingRequired) {
    return (
      <main className="pairing-shell">
        <form className="pairing-card" onSubmit={(event) => void submitPairing(event)}>
          <img src="/icons/feastcloud.svg" alt="" />
          <p className="eyebrow">{t('app.product')}</p>
          <h1>{t('pairing.title')}</h1>
          <p>{t('pairing.body')}</p>
          <label>
            <span>{t('pairing.code')}</span>
            <input inputMode="numeric" autoComplete="one-time-code" maxLength={8} value={pairingCode} onChange={(event) => setPairingCode(event.target.value.replace(/\D/g, ''))} autoFocus />
          </label>
          {pairingError && <p className="pairing-error" role="alert">{pairingError}</p>}
          <button type="submit" disabled={pairing || pairingCode.length !== 8}>{pairing ? t('pairing.connecting') : t('pairing.connect')}</button>
        </form>
      </main>
    );
  }

  return (
    <div className={`app role-${kitchen.preferences.role}`}>
      <AppHeader
        preferences={kitchen.preferences}
        allowedViews={kitchen.allowedViews}
        online={kitchen.online}
        syncState={kitchen.syncState}
        t={t}
        onPreferencesChange={kitchen.updatePreferences}
        onSync={() => void kitchen.flushOutbox()}
        roleLocked={kitchen.roleLocked}
        workspace={workspace}
      />

      <main id="main-content" tabIndex={-1}>
        <Suspense fallback={<AppLoading label={t('common.loading')} />}>
        {!kitchen.hydrated ? (
          <div className="loading-state" role="status">
            <span className="loading-mark" />
            <p>{t('common.loading')}</p>
          </div>
        ) : kitchen.preferences.view === 'overview' ? (
          <ExecutiveDashboard
            snapshot={kitchen.snapshot}
            preferences={kitchen.preferences}
            t={t}
            onNavigate={(view) => kitchen.updatePreferences({ view })}
          />
        ) : kitchen.preferences.view === 'inventory' ? (
          <InventoryDashboard snapshot={kitchen.snapshot} t={t} />
        ) : kitchen.preferences.view === 'production' ? (
          <ProductionBoard snapshot={kitchen.snapshot} t={t} />
        ) : kitchen.preferences.view === 'planning' ? (
          <PlanningHub snapshot={kitchen.snapshot} t={t} />
        ) : kitchen.preferences.view === 'operations' ? (
          <OperationsControl snapshot={kitchen.snapshot} t={t} />
        ) : kitchen.preferences.view === 'daily' ? (
          <DailyOperations snapshot={kitchen.snapshot} t={t} />
        ) : incomingFlow ? (
          <OrderEntry
            locale={kitchen.preferences.locale}
            tenantId={kitchen.snapshot.organizationId}
            outletId={kitchen.snapshot.outletId}
            t={t}
            onSubmit={kitchen.submitOrder}
          />
        ) : kitchen.preferences.view === 'commerce' ? (
          <CommerceHub snapshot={kitchen.snapshot} t={t} />
        ) : kitchen.preferences.view === 'growth' ? (
          <GuestGrowthHub snapshot={kitchen.snapshot} t={t} />
        ) : kitchen.preferences.view === 'organization' ? (
          <OrganizationControl snapshot={kitchen.snapshot} t={t} />
        ) : kitchen.preferences.view === 'platform' ? (
          <PlatformOnboarding t={t} />
        ) : kitchen.preferences.view === 'menu' ? (
          <MenuStudio snapshot={kitchen.snapshot} t={t} />
        ) : kitchen.preferences.view === 'orders' ? (
          <OrderEntry
            locale={kitchen.preferences.locale}
            tenantId={kitchen.snapshot.organizationId}
            outletId={kitchen.snapshot.outletId}
            t={t}
            onSubmit={kitchen.submitOrder}
          />
        ) : (
          <KitchenDisplay
            snapshot={kitchen.snapshot}
            locale={kitchen.preferences.locale}
            t={t}
            onAdvanceOrder={kitchen.moveOrderForward}
            onAdvanceTicket={kitchen.moveTicketForward}
          />
        )}
        </Suspense>
      </main>
    </div>
  );
}
