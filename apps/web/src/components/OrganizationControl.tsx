import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import type { KitchenSnapshot } from '../domain/types';
import type { MessageKey } from '../i18n/messages';
import {
  createBrand,
  createOrganization,
  createOutlet,
  createStation,
  fetchOrganizationControlData,
  fetchOutletProfile,
  fetchStationCapacity,
  organizationApiBase,
  saveOutletProfile,
  saveStationCapacity,
  setBrandOutletAssignment,
  type Brand,
  type BrandOutletAssignment,
  type OrganizationControlData,
  type Outlet,
  type OutletControlProfile,
  type StationCapacityLimit,
  type StationType,
} from '../domain/coreOrganization';
import './organization-control.css';

const api = organizationApiBase((import.meta.env.VITE_CORE_URL as string | undefined)?.trim());

type FeatureName = 'pos' | 'ordering' | 'aggregators' | 'transfers' | 'forecast';

const allFeatures: FeatureName[] = ['pos', 'ordering', 'aggregators', 'transfers', 'forecast'];
const stationTypes: StationType[] = ['preparation', 'cooking', 'beverage', 'assembly', 'expo', 'packing'];

const templateFeatures: Record<'restaurant' | 'cloud' | 'central', Record<FeatureName, boolean>> = {
  restaurant: { pos: true, ordering: true, aggregators: true, transfers: true, forecast: true },
  cloud: { pos: false, ordering: true, aggregators: true, transfers: true, forecast: true },
  central: { pos: false, ordering: false, aggregators: false, transfers: true, forecast: true },
};

function defaultFeatures(): Record<FeatureName, boolean> {
  return { ...templateFeatures.restaurant };
}

function isMissingProfile(error: unknown): boolean {
  return error instanceof Error && error.message.includes(': 404');
}

function readFeatureProfile(value: OutletControlProfile | undefined): Record<FeatureName, boolean> {
  const defaults = defaultFeatures();
  if (!value) return defaults;
  for (const feature of allFeatures) {
    if (typeof value.featureProfile[feature] === 'boolean') defaults[feature] = value.featureProfile[feature] as boolean;
  }
  return defaults;
}

function stationTypeLabel(type: StationType, t: OrganizationControlProps['t']): string {
  return t(`organization.station.${type}` as MessageKey);
}

function useOutletControls(core: string | undefined, tenantId: string, outletId: string | undefined) {
  const [profile, setProfile] = useState<OutletControlProfile>();
  const [capacities, setCapacities] = useState<StationCapacityLimit[]>([]);
  const [controlsAvailable, setControlsAvailable] = useState(true);

  const load = useCallback(async () => {
    if (!core || !outletId) {
      setProfile(undefined);
      setCapacities([]);
      return;
    }
    try {
      const [nextProfile, nextCapacities] = await Promise.all([
        fetchOutletProfile(core, tenantId, outletId).catch((error: unknown) => {
          if (isMissingProfile(error)) return undefined;
          throw error;
        }),
        fetchStationCapacity(core, tenantId, outletId),
      ]);
      setProfile(nextProfile);
      setCapacities(nextCapacities);
      setControlsAvailable(true);
    } catch {
      setProfile(undefined);
      setCapacities([]);
      setControlsAvailable(false);
    }
  }, [core, outletId, tenantId]);

  useEffect(() => { void load(); }, [load]);
  return { profile, capacities, controlsAvailable, reloadControls: load };
}

interface OrganizationControlProps {
  snapshot: KitchenSnapshot;
  t: (key: MessageKey, replacements?: Record<string, string | number>) => string;
}

export function OrganizationControl({ snapshot, t }: OrganizationControlProps) {
  const [data, setData] = useState<OrganizationControlData>();
  const [selectedOutletId, setSelectedOutletId] = useState<string>();
  const [loading, setLoading] = useState(Boolean(api));
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [profileName, setProfileName] = useState('');
  const [features, setFeatures] = useState<Record<FeatureName, boolean>>(defaultFeatures);

  const load = useCallback(async () => {
    if (!api) return;
    setLoading(true);
    setError('');
    try {
      const next = await fetchOrganizationControlData(api, snapshot.organizationId);
      setData(next);
      setSelectedOutletId((current) => current && next.outlets.some((outlet) => outlet.id === current)
        ? current
        : next.outlets.find((outlet) => outlet.id === snapshot.outletId)?.id ?? next.outlets[0]?.id);
    } catch {
      setError(t('organization.unavailable'));
    } finally {
      setLoading(false);
    }
  }, [snapshot.organizationId, snapshot.outletId, t]);

  useEffect(() => { void load(); }, [load]);

  const selectedOutlet = data?.outlets.find((outlet) => outlet.id === selectedOutletId);
  const { profile, capacities, controlsAvailable, reloadControls } = useOutletControls(api, snapshot.organizationId, selectedOutlet?.id);
  const stations = useMemo(() => data?.stations.filter((station) => station.outletId === selectedOutlet?.id) ?? [], [data?.stations, selectedOutlet?.id]);
  const capacityByStation = useMemo(() => new Map(capacities.map((capacity) => [capacity.stationId, capacity])), [capacities]);
  const organization = data?.organizations[0];

  useEffect(() => {
    setProfileName(profile?.profileName ?? '');
    setFeatures(readFeatureProfile(profile));
  }, [profile, selectedOutlet?.id]);

  const complete = (message = t('organization.saved')) => {
    setNotice(message);
    setError('');
  };

  const createNewOutlet = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!api || !organization) return;
    const form = new FormData(event.currentTarget);
    const name = String(form.get('name') ?? '').trim();
    const code = String(form.get('code') ?? '').trim();
    const timeZone = String(form.get('timeZone') ?? '').trim();
    const currency = String(form.get('currency') ?? '').trim();
    if (!name || !code || !timeZone || !currency) return;
    setBusy('outlet');
    try {
      const created = await createOutlet(api, snapshot.organizationId, snapshot.outletId, organization.id, { name, code, timeZone, currency });
      event.currentTarget.reset();
      await load();
      setSelectedOutletId(created.id);
      complete();
    } catch {
      setError(t('organization.actionFailed'));
    } finally {
      setBusy('');
    }
  };

  const initializeOrganization = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!api) return;
    const form = new FormData(event.currentTarget);
    const name = String(form.get('name') ?? '').trim();
    const legalName = String(form.get('legalName') ?? '').trim();
    const defaultLocale = String(form.get('defaultLocale') ?? '').trim();
    const defaultCurrency = String(form.get('defaultCurrency') ?? '').trim();
    if (!name || !defaultLocale || !defaultCurrency) return;
    setBusy('organization');
    try {
      await createOrganization(api, snapshot.organizationId, snapshot.outletId, { name, legalName, defaultLocale, defaultCurrency });
      await load();
      complete();
    } catch {
      setError(t('organization.actionFailed'));
    } finally {
      setBusy('');
    }
  };

  const createNewBrand = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!api || !organization) return;
    const form = new FormData(event.currentTarget);
    const name = String(form.get('name') ?? '').trim();
    const code = String(form.get('code') ?? '').trim();
    if (!name || !code) return;
    setBusy('brand');
    try {
      await createBrand(api, snapshot.organizationId, snapshot.outletId, organization.id, { name, code });
      event.currentTarget.reset();
      await load();
      complete();
    } catch {
      setError(t('organization.actionFailed'));
    } finally {
      setBusy('');
    }
  };

  const createNewStation = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!api || !selectedOutlet) return;
    const form = new FormData(event.currentTarget);
    const name = String(form.get('name') ?? '').trim();
    const code = String(form.get('code') ?? '').trim();
    const type = String(form.get('type') ?? '') as StationType;
    if (!name || !code || !stationTypes.includes(type)) return;
    setBusy('station');
    try {
      await createStation(api, snapshot.organizationId, selectedOutlet.id, { name, code, type });
      event.currentTarget.reset();
      await load();
      complete();
    } catch {
      setError(t('organization.actionFailed'));
    } finally {
      setBusy('');
    }
  };

  const toggleBrandRollout = async (brand: Brand, assignment: BrandOutletAssignment | undefined) => {
    if (!api || !selectedOutlet) return;
    setBusy(`brand:${brand.id}`);
    try {
      await setBrandOutletAssignment(api, snapshot.organizationId, selectedOutlet.id, {
        brandId: brand.id,
        active: assignment ? !assignment.active : true,
        expectedVersion: assignment?.version,
      });
      await load();
      complete();
    } catch {
      setError(t('organization.actionFailed'));
    } finally {
      setBusy('');
    }
  };

  const saveProfile = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!api || !selectedOutlet || !profileName.trim()) return;
    setBusy('profile');
    try {
      await saveOutletProfile(api, snapshot.organizationId, selectedOutlet.id, {
        profileName: profileName.trim(),
        featureProfile: features,
        approvalPolicy: { managerApprovalFor: ['stock_transfer', 'menu_rollout', 'refund'] },
      });
      await reloadControls();
      complete();
    } catch {
      setError(t('organization.actionFailed'));
    } finally {
      setBusy('');
    }
  };

  const saveCapacity = async (event: FormEvent<HTMLFormElement>, stationId: string) => {
    event.preventDefault();
    if (!api || !selectedOutlet) return;
    const value = Number(new FormData(event.currentTarget).get('capacity'));
    if (!Number.isInteger(value) || value < 0 || value > 999) return;
    setBusy(`capacity:${stationId}`);
    try {
      await saveStationCapacity(api, snapshot.organizationId, selectedOutlet.id, stationId, value);
      await reloadControls();
      complete();
    } catch {
      setError(t('organization.actionFailed'));
    } finally {
      setBusy('');
    }
  };

  if (!api) {
    return <section className="organization-control-page"><div className="org-empty"><p className="eyebrow">{t('organization.eyebrow')}</p><h1>{t('organization.title')}</h1><p>{t('organization.notConfigured')}</p></div></section>;
  }

  if (loading && !data) {
    return <section className="organization-control-page"><div className="org-empty" role="status"><span className="loading-mark" /><p>{t('common.loading')}</p></div></section>;
  }

  if (!organization) {
    return <section className="organization-control-page"><div className="org-onboarding">
      <p className="eyebrow">{t('organization.eyebrow')}</p>
      <h1>{t('organization.title')}</h1>
      <p>{t('organization.subtitle')}</p>
      {error && <p className="org-message error" role="alert">{error}</p>}
      <form onSubmit={(event) => void initializeOrganization(event)}>
        <label>{t('organization.organizationName')}<input name="name" required maxLength={160} autoFocus disabled={Boolean(busy)} /></label>
        <label>{t('organization.legalName')}<input name="legalName" maxLength={240} disabled={Boolean(busy)} /></label>
        <div><label>{t('organization.defaultLocale')}<input name="defaultLocale" required defaultValue="en-IN" maxLength={35} disabled={Boolean(busy)} /></label><label>{t('organization.currency')}<input name="defaultCurrency" required defaultValue="INR" maxLength={3} disabled={Boolean(busy)} /></label></div>
        <button className="org-primary" disabled={Boolean(busy)}>{busy === 'organization' ? t('organization.creating') : t('organization.initialize')}</button>
      </form>
    </div></section>;
  }

  return (
    <section className="organization-control-page">
      <header className="organization-heading">
        <div>
          <p className="eyebrow">{t('organization.eyebrow')}</p>
          <h1>{t('organization.title')}</h1>
          <p>{t('organization.subtitle')}</p>
        </div>
        <button type="button" className="org-refresh" onClick={() => void load()} disabled={loading || Boolean(busy)}>{t('common.refresh')}</button>
      </header>

      {error && <p className="org-message error" role="alert">{error}</p>}
      {notice && <p className="org-message success" role="status">{notice}</p>}

      <section className="org-identity" aria-label={t('organization.organization')}>
        <div className="org-identity-mark" aria-hidden="true">{organization?.name.slice(0, 1) ?? 'F'}</div>
        <div><span>{t('organization.organization')}</span><h2>{organization?.name ?? t('organization.organization')}</h2><small>{organization?.legalName || organization?.defaultCurrency || '—'}</small></div>
        <div className="org-stats"><strong><b>{data?.outlets.length ?? 0}</b><small>{t('organization.outlets')}</small></strong><strong><b>{data?.brands.length ?? 0}</b><small>{t('organization.brands')}</small></strong><strong><b>{data?.stations.length ?? 0}</b><small>{t('organization.stations')}</small></strong></div>
      </section>

      <div className="org-control-grid">
        <aside className="org-directory">
          <section>
            <header><div><span>{t('organization.outlets')}</span><h2>{t('organization.outlets')}</h2></div></header>
            <div className="org-outlet-list">
              {(data?.outlets ?? []).map((outlet) => (
                <button type="button" key={outlet.id} className={outlet.id === selectedOutlet?.id ? 'selected' : ''} onClick={() => setSelectedOutletId(outlet.id)}>
                  <span className={`org-status ${outlet.active ? 'active' : ''}`} />
                  <div><b>{outlet.name}</b><small>{outlet.code} · {outlet.timeZone}</small></div>
                  {outlet.id === snapshot.outletId && <em>{t('organization.currentEdge')}</em>}
                </button>
              ))}
              {!data?.outlets.length && <p className="org-muted">{t('organization.noOutlets')}</p>}
            </div>
          </section>

          {organization && <form className="org-inline-form" onSubmit={(event) => void createNewOutlet(event)}>
            <b>{t('organization.createOutlet')}</b>
            <input name="name" required maxLength={160} placeholder={t('organization.outletName')} disabled={Boolean(busy)} />
            <div><input name="code" required maxLength={64} placeholder={t('organization.code')} disabled={Boolean(busy)} /><input name="currency" required maxLength={3} defaultValue={organization.defaultCurrency} placeholder={t('organization.currency')} disabled={Boolean(busy)} /></div>
            <input name="timeZone" required defaultValue="Asia/Kolkata" placeholder={t('organization.timeZone')} disabled={Boolean(busy)} />
            <button disabled={Boolean(busy)}>{busy === 'outlet' ? t('organization.creating') : t('organization.create')}</button>
          </form>}
        </aside>

        <div className="org-workspace">
          {!selectedOutlet ? <div className="org-empty compact"><p>{t('organization.selectOutlet')}</p></div> : <>
            <section className="org-workspace-head">
              <div><span>{selectedOutlet.code} · {selectedOutlet.currency}</span><h2>{selectedOutlet.name}</h2><p>{selectedOutlet.timeZone}</p></div>
              <span className={`org-state ${selectedOutlet.active ? 'active' : ''}`}>{selectedOutlet.active ? t('organization.active') : t('organization.paused')}</span>
            </section>

            <div className="org-modules-grid">
              <section className="org-card org-rollout">
                <header><div><span>{t('organization.brands')}</span><h2>{t('organization.brandRollout')}</h2></div></header>
                <p className="org-muted">{t('organization.brandRolloutHelp')}</p>
                <div className="org-brand-list">
                  {(data?.brands ?? []).map((brand) => {
                    const assignment = data?.brandAssignments.find((item) => item.brandId === brand.id && item.outletId === selectedOutlet.id);
                    const active = assignment?.active ?? false;
                    return <article key={brand.id}><div><b>{brand.name}</b><small>{brand.code}</small></div><button type="button" className={active ? 'pause' : ''} disabled={Boolean(busy)} onClick={() => void toggleBrandRollout(brand, assignment)}>{busy === `brand:${brand.id}` ? t('organization.creating') : active ? t('organization.disable') : t('organization.enable')}</button></article>;
                  })}
                  {!data?.brands.length && <p className="org-muted">{t('organization.noBrands')}</p>}
                </div>
                {organization && <form className="org-compact-create" onSubmit={(event) => void createNewBrand(event)}><input name="name" required maxLength={160} placeholder={t('organization.brandName')} disabled={Boolean(busy)} /><input name="code" required maxLength={64} placeholder={t('organization.code')} disabled={Boolean(busy)} /><button disabled={Boolean(busy)}>{busy === 'brand' ? t('organization.creating') : t('organization.createBrand')}</button></form>}
              </section>

              <section className="org-card org-profile">
                <header><div><span>{t('organization.profile')}</span><h2>{t('organization.profile')}</h2></div></header>
                <p className="org-muted">{t('organization.profileHelp')}</p>
                {!controlsAvailable ? <p className="org-control-note">{t('organization.unavailable')}</p> : <form onSubmit={(event) => void saveProfile(event)}>
                  <label>{t('organization.profileName')}<input required value={profileName} onChange={(event) => setProfileName(event.target.value)} placeholder={t('organization.template.restaurant')} disabled={Boolean(busy)} /></label>
                  <div className="org-templates">{(['restaurant', 'cloud', 'central'] as const).map((template) => <button key={template} type="button" disabled={Boolean(busy)} onClick={() => { setProfileName(t(`organization.template.${template}` as MessageKey)); setFeatures({ ...templateFeatures[template] }); }}>{t(`organization.template.${template}` as MessageKey)}</button>)}</div>
                  <fieldset><legend>{t('organization.features')}</legend>{allFeatures.map((feature) => <label key={feature}><input type="checkbox" checked={features[feature]} onChange={(event) => setFeatures((current) => ({ ...current, [feature]: event.target.checked }))} disabled={Boolean(busy)} />{t(`organization.feature.${feature}` as MessageKey)}</label>)}</fieldset>
                  <button className="org-primary" disabled={Boolean(busy) || !profileName.trim()}>{busy === 'profile' ? t('organization.creating') : t('organization.saveProfile')}</button>
                </form>}
              </section>
            </div>

            <section className="org-card org-stations">
              <header><div><span>{t('organization.stations')}</span><h2>{t('organization.stations')}</h2></div><p>{t('organization.capacityHelp')}</p></header>
              <div className="org-station-list">
                {stations.map((station) => {
                  const capacity = capacityByStation.get(station.id);
                  return <article key={station.id}><div className="station-kind"><span>{stationTypeLabel(station.type, t)}</span><b>{station.name}</b><small>{station.code} · {station.active ? t('organization.active') : t('organization.paused')}</small></div>{controlsAvailable && <form onSubmit={(event) => void saveCapacity(event, station.id)}><label><span>{t('organization.maxTickets')}</span><input name="capacity" type="number" min="0" max="999" step="1" defaultValue={capacity?.maxActiveTickets ?? 12} disabled={Boolean(busy)} /></label><button disabled={Boolean(busy)}>{busy === `capacity:${station.id}` ? t('organization.creating') : t('organization.saveCapacity')}</button></form>}</article>;
                })}
                {stations.length === 0 && <p className="org-muted">{t('organization.noStations')}</p>}
              </div>
              <form className="org-compact-create station-create" onSubmit={(event) => void createNewStation(event)}><input name="name" required maxLength={160} placeholder={t('organization.stationName')} disabled={Boolean(busy)} /><input name="code" required maxLength={64} placeholder={t('organization.code')} disabled={Boolean(busy)} /><select name="type" defaultValue="cooking" disabled={Boolean(busy)}>{stationTypes.map((type) => <option key={type} value={type}>{stationTypeLabel(type, t)}</option>)}</select><button disabled={Boolean(busy)}>{busy === 'station' ? t('organization.creating') : t('organization.createStation')}</button></form>
            </section>

            <aside className="org-safety-note"><span aria-hidden="true">✓</span><div><b>{t('organization.safeScope')}</b><p>{t('organization.safeScopeBody')}</p></div></aside>
          </>}
        </div>
      </div>
    </section>
  );
}
