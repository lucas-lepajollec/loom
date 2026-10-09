// HTTPS sur le réseau local : les navigateurs n'autorisent le micro (mode
// vocal) et les notifications qu'en HTTPS. Loom sert la même interface sur un
// second port, avec un certificat qu'il génère (à accepter une fois) ou celui
// fourni (par exemple `tailscale cert`). Contrat : docs/voice.md.
import { html, useState, useEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { Switch } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { Icon } from '../../ui/icons.js';
import { Line, Group } from './kit.js';

export function HttpsSettings() {
  const [s, setS] = useState(null), [busy, setBusy] = useState(false);
  const load = () => get('/api/https').then(r => setS(r.ok === false ? null : r)).catch(() => setS(null));
  useEffect(() => { load(); }, []);
  if (!s) return null;
  const save = async patch => {
    setBusy(true);
    const r = await post('/api/https', { enabled: s.enabled, port: s.port, mode: s.mode, ...patch }, { timeout: 30000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false) return toast(r.error || t('https.failed'), 'err');
    setS(r);
  };
  const urls = s.urls || [];
  return html`<${Group} title=${t('https.title')}>
    <div class="pad"><p class="note">${t('https.lead')}</p></div>
    <${Line} label=${t('https.enable')} tip=${t('https.enable_tip')}>${s.running && html`<span class="state"><i class="dot green"></i>${t('https.running')}</span>`}<${Switch} label=${t('https.enable')} disabled=${busy} checked=${s.enabled} onChange=${on => save({ enabled: on })} /></${Line}>
    ${s.error && html`<div class="pad"><p class="note err">${s.error}</p></div>`}
    ${s.enabled && urls.map(u => html`<${Line} key=${u} label=${t('https.address')}><a class="mono" href=${u + location.pathname + location.hash}>${u}</a><button class="icon-btn" aria-label=${t('local.page.copier')} onClick=${() => navigator.clipboard && navigator.clipboard.writeText(u).then(() => toast(t('local.page.copie')))}><${Icon} n="copy" /></button></${Line}>`)}
    ${s.enabled && s.fingerprint_sha256 && html`<${Line} label=${t('https.fingerprint')} tip=${t('https.fingerprint_tip')} stack><code class="mono path">${s.fingerprint_sha256.match(/.{1,2}/g).join(':')}</code></${Line}>`}
    ${s.enabled && s.mode === 'self-signed' && html`<${Line} label=${t('https.regenerate')} tip=${t('https.regenerate_tip')}><button class="btn sm ghost" disabled=${busy} onClick=${() => save({ regenerate: true })}>${t('https.regenerate_btn')}</button></${Line}>`}
    ${s.enabled && html`<div class="pad"><p class="note">${t('https.accept_note')}</p></div>`}
  </${Group}>`;
}
