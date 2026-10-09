// Notifications : être prévenu sur son téléphone quand un agent attend une
// réponse, finit ou échoue, et répondre depuis la notification (ntfy). Tout est
// désactivé par défaut ; aucun texte de discussion n'est envoyé sans l'option
// « inclure un résumé ». Contrat : docs/notifications.md.
import { html, useState, useEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { Switch } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { Line, Group } from './kit.js';

const EVENTS = ['task.waiting', 'task.completed', 'task.failed', 'node.offline', 'engine.down'];
const EVENT_LABEL = () => ({ 'task.waiting': t('notify.ev.task_waiting'), 'task.completed': t('notify.ev.task_completed'), 'task.failed': t('notify.ev.task_failed'), 'node.offline': t('notify.ev.node_offline'), 'engine.down': t('notify.ev.engine_down') });
const secure = () => (window.isSecureContext ? 'true' : 'false');

export function NotificationSettings() {
  const [env, setEnv] = useState(null), [cfg, setCfg] = useState(null);
  const [ntfyToken, setNtfyToken] = useState(null), [hookSecret, setHookSecret] = useState(null);
  const [busy, setBusy] = useState(''), [dirty, setDirty] = useState(false);
  const load = () => get('/api/notify?secure=' + secure()).then(r => { if (r.ok) { setEnv(r); setCfg(r.config); setDirty(false); setNtfyToken(null); setHookSecret(null); } }).catch(() => {});
  useEffect(() => { load(); }, []);
  if (!cfg) return html`<${Group} title=${t('notify.title')}><div class="pad"><p class="note">${t('startup.loading')}</p></div></${Group}>`;
  const set = (path, value) => {
    const next = structuredClone(cfg); let o = next; const keys = path.split('.');
    keys.slice(0, -1).forEach(k => { o = o[k]; }); o[keys[keys.length - 1]] = value;
    setCfg(next); setDirty(true);
  };
  const save = async () => {
    setBusy('save');
    const body = { config: cfg, ...(ntfyToken !== null ? { ntfy_token: ntfyToken } : {}), ...(hookSecret !== null ? { webhook_secret: hookSecret } : {}) };
    const r = await post('/api/notify?secure=' + secure(), body).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t('notify.save_failed'), 'err');
    setEnv(r); setCfg(r.config); setDirty(false); setNtfyToken(null); setHookSecret(null); toast(t('notify.saved'));
  };
  const test = async channel => {
    if (dirty) await save();
    setBusy(channel);
    const r = await post('/api/notify/test', { channel }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    r.ok ? toast(t('notify.test_sent')) : toast(r.error || t('notify.test_failed'), 'err');
  };
  const subscribe = async () => {
    setBusy('push');
    try {
      const reg = await navigator.serviceWorker.ready;
      const key = Uint8Array.from(atob(env.push_public_key.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((env.push_public_key.length + 3) % 4)), c => c.charCodeAt(0));
      const sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
      const r = await post('/api/notify/push/subscribe?secure=' + secure(), sub.toJSON());
      if (!r.ok) throw new Error(r.error);
      toast(t('notify.push_ready')); load();
    } catch (e) { toast(e.message || t('notify.push_failed'), 'err'); }
    setBusy('');
  };
  const rules = cfg.rules || {};
  const on = ev => (rules.events || []).includes(ev);
  const toggleEvent = (ev, yes) => set('rules.events', yes ? [...new Set([...(rules.events || []), ev])] : (rules.events || []).filter(x => x !== ev));
  const err = ch => env.last_errors && env.last_errors[ch];
  return html`
    <${Group} title=${t('notify.address')}>
      <${Line} label=${t('notify.public_url')} tip=${t('notify.public_url_tip')} stack>
        <input class="input" value=${cfg.public_base_url} placeholder=${location.origin} onInput=${e => set('public_base_url', e.target.value.trim())} /></${Line}>
      ${env.public_url_warning && html`<div class="pad"><p class="note warn">${t('notify.public_url_warning')}</p></div>`}
    </${Group}>
    <${Group} title=${t('notify.ntfy')}>
      <div class="pad"><p class="note">${t('notify.ntfy_note')}</p></div>
      <${Line} label=${t('notify.enabled')}><${Switch} label=${t('notify.ntfy')} checked=${cfg.ntfy.enabled} onChange=${v => set('ntfy.enabled', v)} /></${Line}>
      <${Line} label=${t('notify.server')} stack><input class="input" value=${cfg.ntfy.server_url} onInput=${e => set('ntfy.server_url', e.target.value.trim())} /></${Line}>
      <${Line} label=${t('notify.topic')} tip=${t('notify.topic_tip')} stack><input class="input mono" value=${cfg.ntfy.topic} placeholder="loom-…" onInput=${e => set('ntfy.topic', e.target.value.trim())} /></${Line}>
      <${Line} label=${t('notify.token')} tip=${t('notify.token_tip')} stack><input class="input" type="password" autocomplete="off" value=${ntfyToken ?? ''} placeholder=${cfg.ntfy.token_set ? t('notify.secret_kept') : t('notify.optional')} onInput=${e => { setNtfyToken(e.target.value); setDirty(true); }} /></${Line}>
      <${Line} label=${t('notify.test')}>${err('ntfy') && html`<span class="note err">${err('ntfy')}</span>`}<button class="btn sm" disabled=${!!busy || !cfg.ntfy.topic} onClick=${() => test('ntfy')}>${t('notify.send_test')}</button></${Line}>
    </${Group}>
    <${Group} title=${t('notify.webhook')}>
      <${Line} label=${t('notify.enabled')}><${Switch} label=${t('notify.webhook')} checked=${cfg.webhook.enabled} onChange=${v => set('webhook.enabled', v)} /></${Line}>
      <${Line} label="URL" stack><input class="input" value=${cfg.webhook.url} placeholder="https://…" onInput=${e => set('webhook.url', e.target.value.trim())} /></${Line}>
      <${Line} label=${t('notify.secret')} tip=${t('notify.secret_tip')} stack><input class="input" type="password" autocomplete="off" value=${hookSecret ?? ''} placeholder=${cfg.webhook.secret_set ? t('notify.secret_kept') : t('notify.optional')} onInput=${e => { setHookSecret(e.target.value); setDirty(true); }} /></${Line}>
      <${Line} label=${t('notify.test')}>${err('webhook') && html`<span class="note err">${err('webhook')}</span>`}<button class="btn sm" disabled=${!!busy || !cfg.webhook.url} onClick=${() => test('webhook')}>${t('notify.send_test')}</button></${Line}>
    </${Group}>
    <${Group} title=${t('notify.push')}>
      ${env.secure ? html`
        <${Line} label=${t('notify.enabled')}><${Switch} label=${t('notify.push')} checked=${cfg.push.enabled} onChange=${v => set('push.enabled', v)} /></${Line}>
        <${Line} label=${t('notify.this_device')} tip=${t('notify.this_device_tip')}><span class="muted">${t('notify.devices', { n: env.push_subscription_count || 0 })}</span><button class="btn sm" disabled=${!!busy || !('serviceWorker' in navigator)} onClick=${subscribe}>${t('notify.subscribe')}</button></${Line}>`
      : html`<div class="pad"><p class="note">${t('notify.push_needs_https')}</p></div>`}
    </${Group}>
    <${Group} title=${t('notify.when')}>
      ${EVENTS.map(ev => html`<${Line} key=${ev} label=${EVENT_LABEL()[ev]}><${Switch} label=${ev} checked=${on(ev)} onChange=${v => toggleEvent(ev, v)} /></${Line}>`)}
      <${Line} label=${t('notify.long_only')} tip=${t('notify.long_only_tip')}><div class="set-pick"><select class="select" value=${rules.completed_after_seconds} onChange=${e => set('rules.completed_after_seconds', Number(e.target.value))}>
        ${[0, 60, 120, 300, 600, 1800].map(s => html`<option value=${s}>${s ? t('notify.minutes', { n: s / 60 }) : t('notify.always')}</option>`)}</select></div></${Line}>
      <${Line} label=${t('notify.summaries')} tip=${t('notify.summaries_tip')}><${Switch} label=${t('notify.summaries')} checked=${rules.include_summaries} onChange=${v => set('rules.include_summaries', v)} /></${Line}>
      <${Line} label=${t('notify.quiet')} tip=${t('notify.quiet_tip')}><${Switch} label=${t('notify.quiet')} checked=${rules.quiet_hours && rules.quiet_hours.enabled} onChange=${v => set('rules.quiet_hours', { ...(rules.quiet_hours || {}), enabled: v, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone })} /></${Line}>
      ${rules.quiet_hours && rules.quiet_hours.enabled && html`<${Line} label=${t('notify.quiet_range')}><input class="input" type="time" value=${rules.quiet_hours.start} onInput=${e => set('rules.quiet_hours.start', e.target.value)} style="width:120px" /><span class="muted">→</span><input class="input" type="time" value=${rules.quiet_hours.end} onInput=${e => set('rules.quiet_hours.end', e.target.value)} style="width:120px" /></${Line}>`}
    </${Group}>
    <div class="set-save"><button class="btn primary" disabled=${!dirty || !!busy} onClick=${save}>${t('settings.page.enregistrer')}</button></div>`;
}
