import { FormEvent, useState } from 'react';
import { organizationApiBase, provisionTenant, type ProvisionedTenant } from '../domain/coreOrganization';
import type { MessageKey } from '../i18n/messages';
import './organization-control.css';

const api = organizationApiBase((import.meta.env.VITE_CORE_URL as string | undefined)?.trim());

export function PlatformOnboarding({ t }: { t: (key: MessageKey) => string }) {
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<ProvisionedTenant>();
  const [error, setError] = useState('');

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!api) return;
    const form = new FormData(event.currentTarget);
    setBusy(true); setError(''); setResult(undefined);
    try {
      const value = await provisionTenant(api, {
        organizationName: String(form.get('organizationName') ?? '').trim(), legalName: String(form.get('legalName') ?? '').trim(),
        ownerName: String(form.get('ownerName') ?? '').trim(), ownerEmail: String(form.get('ownerEmail') ?? '').trim(),
        defaultLocale: String(form.get('defaultLocale') ?? 'en-IN'), defaultCurrency: String(form.get('defaultCurrency') ?? 'INR'),
        outletName: String(form.get('outletName') ?? '').trim(), outletCode: String(form.get('outletCode') ?? '').trim(), timeZone: String(form.get('timeZone') ?? 'Asia/Kolkata'),
        brandName: String(form.get('brandName') ?? '').trim(), brandCode: String(form.get('brandCode') ?? '').trim(), template: String(form.get('template') ?? 'restaurant') as 'restaurant' | 'cloud' | 'central',
      });
      setResult(value);
      event.currentTarget.reset();
    } catch (reason) { setError(reason instanceof Error && reason.message.includes('platform_control_plane_unavailable') ? t('platform.controlPlaneUnavailable') : t('platform.failed')); }
    finally { setBusy(false); }
  };

  if (!api) return <section className="organization-control-page"><div className="org-empty"><p className="eyebrow">{t('platform.eyebrow')}</p><h1>{t('platform.title')}</h1><p>{t('platform.notConfigured')}</p></div></section>;

  return <section className="organization-control-page"><div className="org-onboarding">
    <p className="eyebrow">{t('platform.eyebrow')}</p><h1>{t('platform.title')}</h1><p>{t('platform.subtitle')}</p>
    {error && <p className="org-message error" role="alert">{error}</p>}
    {result && <div className="org-message success" role="status"><b>{t('platform.success')}</b><br />{result.organization.name} · {result.outlet.name} · {result.brand.name} · {result.stations.length} stations<br /><small>{t('platform.handoff')}: {result.ownerHandoff.name} ({result.ownerHandoff.email}). {t('platform.identityPending')}</small></div>}
    <form onSubmit={(event) => void submit(event)}>
      <label>{t('platform.organization')}<input name="organizationName" required maxLength={160} autoFocus disabled={busy} /></label>
      <label>{t('platform.legalName')}<input name="legalName" maxLength={240} disabled={busy} /></label>
      <div><label>{t('platform.owner')}<input name="ownerName" required maxLength={160} disabled={busy} /></label><label>{t('platform.email')}<input name="ownerEmail" required type="email" maxLength={254} disabled={busy} /></label></div>
      <div><label>{t('platform.outlet')}<input name="outletName" required maxLength={160} disabled={busy} /></label><label>{t('platform.outletCode')}<input name="outletCode" required maxLength={64} placeholder="BLR-01" disabled={busy} /></label></div>
      <div><label>{t('platform.brand')}<input name="brandName" required maxLength={160} disabled={busy} /></label><label>{t('platform.brandCode')}<input name="brandCode" required maxLength={64} placeholder="BRAND" disabled={busy} /></label></div>
      <div><label>{t('organization.defaultLocale')}<input name="defaultLocale" defaultValue="en-IN" maxLength={35} disabled={busy} /></label><label>{t('organization.currency')}<input name="defaultCurrency" defaultValue="INR" maxLength={3} disabled={busy} /></label></div>
      <div><label>{t('organization.timeZone')}<input name="timeZone" defaultValue="Asia/Kolkata" disabled={busy} /></label><label>{t('platform.template')}<select name="template" defaultValue="restaurant" disabled={busy}><option value="restaurant">{t('platform.template.restaurant')}</option><option value="cloud">{t('platform.template.cloud')}</option><option value="central">{t('platform.template.central')}</option></select></label></div>
      <button className="org-primary" disabled={busy}>{busy ? t('platform.provisioning') : t('platform.provision')}</button>
    </form>
  </div></section>;
}
