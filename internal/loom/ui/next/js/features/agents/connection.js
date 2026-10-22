// Connexion à Codeman : adresse vue par le serveur Loom, identifiant et mot
// de passe (scellé côté Loom, jamais renvoyé au navigateur), adresse de
// l'interface officielle pour ce navigateur, et ce que chaque CLI expose.
import { lt } from './labels.js';
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { toast } from '../../ui/dialog.js';
import { gateway } from './client.js';
import { agents, refreshConnection, refreshAll, capsOf } from './store.js';
import { CliMark } from './new-session.js';

const CAP_ROWS = [['claude', 'agents.caps.claude'], ['codex', 'agents.caps.codex'], ['opencode', 'agents.caps.screen'], ['antigravity', 'agents.caps.screen'], ['pi', 'agents.caps.screen'], ['shell', 'agents.caps.shell']];

export function ConnectionPanel({ gate }) {
  const conn = useStore(agents, s => s.conn);
  const clis = useStore(agents, s => s.clis);
  const [v, setV] = useState({ url: '', username: '', password: '', ui_url: '' });
  const [busy, setBusy] = useState(false);
  useEffect(() => { gateway.config().then(c => setV({ url: c.url || '', username: c.username || '', password: '', ui_url: c.ui_url || '' })).catch(() => {}); }, []);
  const save = async (extra = {}) => {
    setBusy(true);
    try {
      await gateway.save({ url: v.url, username: v.username, ui_url: v.ui_url, ...(v.password ? { password: v.password } : {}), ...extra });
      setV({ ...v, password: '' });
      const state = await refreshConnection();
      if (state === 'ready') { toast(t('agents.conn.saved_ok')); refreshAll().catch(() => {}); }
      else toast(lt('conn', state), 'err');
    } catch (e) { toast(e.message, 'err'); } finally { setBusy(false); }
  };
  const ok = conn.state === 'ready';
  return html`<div class=${cls('cm-connect', gate && 'gate')}>
    <div class="cm-connect-h">
      <span class=${cls('cm-connect-ico', ok ? 'ok' : 'bad')}><${Icon} n=${ok ? 'check' : 'plug'} /></span>
      <div><h2>${gate ? t('agents.conn.gate_title') : t('agents.conn.title')}</h2>
        <p class="muted">${ok ? t('agents.conn.ready', { v: conn.version || '?', user: conn.username || '' }) : lt('connText', conn.state || 'error')}</p></div>
    </div>
    <div class="cm-connect-form">
      <label class="field"><span>${t('agents.conn.url')}</span><input class="input mono" value=${v.url} placeholder="http://127.0.0.1:3000" onInput=${e => setV({ ...v, url: e.target.value })} />
        <small>${t('agents.conn.url_hint')}</small></label>
      <div class="cm-row2">
        <label class="field"><span>${t('agents.conn.user')}</span><input class="input" autocomplete="off" value=${v.username} onInput=${e => setV({ ...v, username: e.target.value })} /></label>
        <label class="field"><span>${t('agents.conn.password')}</span><input class="input" type="password" autocomplete="new-password" value=${v.password}
          placeholder=${conn.password_set ? t('agents.conn.password_kept') : t('agents.conn.password_ph')} onInput=${e => setV({ ...v, password: e.target.value })} /></label>
      </div>
      <label class="field"><span>${t('agents.conn.ui_url')}</span><input class="input mono" value=${v.ui_url} placeholder=${conn.ui_url || 'http://host:3000'} onInput=${e => setV({ ...v, ui_url: e.target.value })} />
        <small>${t('agents.conn.ui_url_hint')}</small></label>
      <div class="cm-connect-acts">
        <button class="btn primary" disabled=${busy || !v.url.trim()} onClick=${() => save()}>${busy ? html`<span class="spinner"></span>` : ''}${t('agents.conn.save')}</button>
        <button class="btn ghost" disabled=${busy} onClick=${() => refreshConnection()}><${Icon} n="refresh" />${t('agents.conn.test')}</button>
        ${conn.password_set && html`<button class="btn ghost danger" disabled=${busy} onClick=${() => save({ clear_password: true })}>${t('agents.conn.forget')}</button>`}
        ${conn.ui_url && html`<a class="btn ghost" href=${conn.ui_url + '/'} target="codeman" rel="noopener"><${Icon} n="link" />${t('agents.official')}</a>`}
      </div>
      <p class="note muted">${t('agents.conn.security')}</p>
    </div>
    ${ok && !gate && html`<section class="cm-caps"><h3>${t('agents.caps.title')}</h3><p class="muted">${t('agents.caps.lead')}</p>
      <ul>${CAP_ROWS.map(([mode, key]) => { const c = clis.find(x => x.id === mode); const caps = capsOf(mode); return html`<li key=${mode}>
        <${CliMark} mode=${mode} size="sm" /><span class="grow"><b>${c ? c.label : mode}</b><small class="muted">${t(key)}</small></span>
        <span class=${cls('tag', c?.installed ? 'green' : '')}>${c ? (c.installed ? t('agents.caps.installed') : t('agents.new.not_installed')) : t('agents.caps.unknown')}</span>
        <span class="tag">${lt('approvals', caps.approvals)}</span></li>`; })}</ul></section>`}
  </div>`;
}
