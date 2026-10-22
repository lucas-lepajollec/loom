// Connexion à Codeman, pensée pour se faire en un clic : Loom cherche un
// Codeman sur sa machine (port, service, .env officiels), propose de s'y
// connecter et ne demande un mot de passe que s'il ne le trouve pas. Un
// Codeman distant se connecte par adresse + mot de passe. Chaque connexion
// est testée avant d'être enregistrée ; le mot de passe reste côté serveur.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { confirm, toast } from '../../ui/dialog.js';
import { copyText } from '../../ui/clipboard.js';
import { gateway } from './client.js';
import { agents, refreshConnection, refreshAll, capsOf } from './store.js';
import { lt } from './labels.js';
import { CliMark } from './new-session.js';

const INSTALL = 'curl -fsSL https://getcodeman.com/install | bash';
const CAP_ROWS = [['claude', 'agents.caps.claude'], ['codex', 'agents.caps.codex'], ['opencode', 'agents.caps.screen'], ['antigravity', 'agents.caps.screen'], ['pi', 'agents.caps.screen'], ['shell', 'agents.caps.shell']];
const ERRORS = { unreachable: 'agents.setup.err_unreachable', 'not-codeman': 'agents.setup.err_not_codeman', password: 'agents.setup.err_password', rejected: 'agents.setup.err_rejected', invalid: 'agents.setup.err_invalid' };
const SOURCES = { systemd: 'agents.setup.src_systemd', launchd: 'agents.setup.src_launchd', 'env-file': 'agents.setup.src_env', environment: 'agents.setup.src_environment' };
const hostOf = u => String(u || '').replace(/^https?:\/\//, '');

async function connect(body) {
  const r = await gateway.connect(body);
  if (!r.ok) throw Object.assign(new Error(t(ERRORS[r.code] || 'agents.setup.err_invalid')), { code: r.code });
  await refreshConnection();
  refreshAll().catch(() => {});
  toast(t('agents.conn.saved_ok'));
  return r;
}

function Found({ f, onDone }) {
  const [pw, setPw] = useState('');
  const [user, setUser] = useState('admin');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const needs = f.auth === 'required' && !f.local_credentials;
  const go = async () => {
    setBusy(true); setErr('');
    try { await connect(needs ? { url: f.url, username: user, password: pw } : { url: f.url, use_local: !!f.local_credentials }); onDone && onDone(); }
    catch (e) { setErr(e.message); } finally { setBusy(false); }
  };
  return html`<div class="cm-found">
    <div class="cm-found-h"><span class="cm-connect-ico ok"><${Icon} n="check" /></span>
      <div class="grow"><b>${t('agents.setup.found', { v: f.version || '' })}</b>
        <small class="muted mono">${hostOf(f.url)}</small>
        <small class="muted">${f.local_credentials ? t(SOURCES[f.source] || 'agents.setup.src_env') : f.auth === 'open' ? t('agents.setup.open') : t('agents.setup.needs_password')}</small></div>
      ${!needs && html`<button class="btn primary" disabled=${busy} onClick=${go}>${busy ? html`<span class="spinner"></span>` : ''}${t('agents.setup.connect')}</button>`}</div>
    ${needs && html`<div class="cm-found-pw">
      <input class="input" type="password" autocomplete="current-password" placeholder=${t('agents.conn.password')} value=${pw} onInput=${e => setPw(e.target.value)} onKeyDown=${e => e.key === 'Enter' && pw && go()} />
      <input class="input cm-user" autocomplete="username" aria-label=${t('agents.conn.user')} value=${user} onInput=${e => setUser(e.target.value)} />
      <button class="btn primary" disabled=${busy || !pw} onClick=${go}>${busy ? html`<span class="spinner"></span>` : ''}${t('agents.setup.connect')}</button>
      <small class="muted">${t('agents.setup.password_hint')}</small></div>`}
    ${err && html`<p class="note err">${err}</p>`}
  </div>`;
}

function Manual({ onDone, initial }) {
  const [v, setV] = useState({ url: initial?.url || '', username: initial?.username || 'admin', password: '' });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const go = async () => {
    setBusy(true); setErr('');
    try { await connect(v); onDone && onDone(); } catch (e) { setErr(e.message); } finally { setBusy(false); }
  };
  return html`<div class="cm-manual">
    <label class="field"><span>${t('agents.conn.url')}</span><input class="input mono" placeholder="192.168.1.20:3000" value=${v.url} onInput=${e => setV({ ...v, url: e.target.value })} />
      <small>${t('agents.setup.url_hint')}</small></label>
    <div class="cm-row2">
      <label class="field"><span>${t('agents.conn.user')}</span><input class="input" autocomplete="off" value=${v.username} onInput=${e => setV({ ...v, username: e.target.value })} /></label>
      <label class="field"><span>${t('agents.conn.password')}</span><input class="input" type="password" autocomplete="new-password" placeholder=${t('agents.setup.password_optional')} value=${v.password} onInput=${e => setV({ ...v, password: e.target.value })} onKeyDown=${e => e.key === 'Enter' && v.url && go()} /></label>
    </div>
    ${err && html`<p class="note err">${err}</p>`}
    <div><button class="btn primary" disabled=${busy || !v.url.trim()} onClick=${go}>${busy ? html`<span class="spinner"></span>` : ''}${t('agents.setup.test_connect')}</button></div>
  </div>`;
}

function Setup({ onDone, initial }) {
  const [found, setFound] = useState(null);
  const [manual, setManual] = useState(false);
  const scan = () => { setFound(null); gateway.discover().then(r => setFound(r.found || [])).catch(() => setFound([])); };
  useEffect(scan, []);
  return html`<div class="cm-setup">
    ${found === null ? html`<div class="cm-scan"><span class="spinner"></span><span>${t('agents.setup.searching')}</span></div>`
      : found.length ? html`<div class="cm-found-list">${found.map(f => html`<${Found} key=${f.url} f=${f} onDone=${onDone} />`)}</div>`
      : html`<div class="cm-none">
          <b>${t('agents.setup.none')}</b>
          <p class="muted">${t('agents.setup.install_text')}</p>
          <div class="cm-cmd"><code>${INSTALL}</code><button class="icon-btn" aria-label=${t('agents.setup.copy')} onClick=${() => copyText(INSTALL).then(() => toast(t('agents.copied')))}><${Icon} n="copy" /></button></div>
          <div class="cm-none-acts"><button class="btn" onClick=${scan}><${Icon} n="refresh" />${t('agents.setup.rescan')}</button>
            <a class="btn ghost" href="https://github.com/Ark0N/Codeman" target="_blank" rel="noopener noreferrer"><${Icon} n="link" />${t('agents.setup.learn')}</a></div>
        </div>`}
    ${found !== null && found.length > 0 && html`<button class="cm-link" onClick=${scan}><${Icon} n="refresh" />${t('agents.setup.rescan')}</button>`}
    <div class="cm-other">
      <button class="cm-link" aria-expanded=${String(manual)} onClick=${() => setManual(!manual)}><${Icon} n=${manual ? 'chevron' : 'right'} />${t('agents.setup.elsewhere')}</button>
      ${manual && html`<${Manual} onDone=${onDone} initial=${initial} />`}
    </div>
  </div>`;
}

function Connected({ conn, onChange }) {
  const clis = useStore(agents, s => s.clis);
  const [ui, setUi] = useState('');
  const [adv, setAdv] = useState(false);
  useEffect(() => { gateway.config().then(c => setUi(c.ui_url || '')).catch(() => {}); }, []);
  const saveUi = async () => {
    try { const c = await gateway.config(); await gateway.save({ url: c.url, username: c.username, ui_url: ui }); await refreshConnection(); toast(t('agents.conn.saved_ok')); }
    catch (e) { toast(e.message, 'err'); }
  };
  const disconnect = async () => {
    if (!await confirm(t('agents.setup.disconnect'), t('agents.setup.disconnect_text'), { ok: t('agents.setup.disconnect'), danger: true })) return;
    try { await gateway.disconnect(); await refreshConnection(); } catch (e) { toast(e.message, 'err'); }
  };
  return html`<div class="cm-connected">
    <div class="cm-found-h"><span class="cm-connect-ico ok"><${Icon} n="check" /></span>
      <div class="grow"><b>${t('agents.conn.ready', { v: conn.version || '?', user: conn.username || '' })}</b><small class="muted mono">${hostOf(conn.url)}</small></div>
      <a class="btn primary" href="#/agents">${t('agents.setup.open_agents')}</a></div>
    <div class="cm-connect-acts">
      <button class="btn ghost" onClick=${onChange}><${Icon} n="refresh" />${t('agents.setup.change')}</button>
      ${conn.ui_url && html`<a class="btn ghost" href=${conn.ui_url + '/'} target="codeman" rel="noopener"><${Icon} n="link" />${t('agents.official')}</a>`}
      <button class="btn ghost danger" onClick=${disconnect}>${t('agents.setup.disconnect')}</button>
    </div>
    <button class="cm-link" aria-expanded=${String(adv)} onClick=${() => setAdv(!adv)}><${Icon} n=${adv ? 'chevron' : 'right'} />${t('agents.setup.advanced')}</button>
    ${adv && html`<div class="cm-adv">
      <label class="field"><span>${t('agents.conn.ui_url')}</span>
        <div class="cm-inline-field"><input class="input mono" value=${ui} placeholder=${conn.ui_url || 'http://host:3000'} onInput=${e => setUi(e.target.value)} /><button class="btn" onClick=${saveUi}>${t('agents.save')}</button></div>
        <small>${t('agents.conn.ui_url_hint')}</small></label>
      <section class="cm-caps"><h3>${t('agents.caps.title')}</h3><p class="muted">${t('agents.caps.lead')}</p>
        <ul>${CAP_ROWS.map(([mode, key]) => { const c = clis.find(x => x.id === mode); return html`<li key=${mode}>
          <${CliMark} mode=${mode} size="sm" /><span class="grow"><b>${c ? c.label : mode}</b><small class="muted">${t(key)}</small></span>
          <span class=${cls('tag', c?.installed ? 'green' : '')}>${c ? (c.installed ? t('agents.caps.installed') : t('agents.new.not_installed')) : t('agents.caps.unknown')}</span>
          <span class="tag">${lt('approvals', capsOf(mode).approvals)}</span></li>`; })}</ul></section>
    </div>`}
  </div>`;
}

// gate : affiché à la place du module tant que Codeman n'est pas connecté.
export function ConnectionPanel({ gate, embedded }) {
  const conn = useStore(agents, s => s.conn);
  const [changing, setChanging] = useState(false);
  useEffect(() => { refreshConnection(); }, []);
  const ok = conn.state === 'ready';
  const broken = conn.configured && ['unreachable', 'rejected', 'auth', 'error'].includes(conn.state);
  return html`<div class=${cls('cm-connect', gate && 'gate', embedded && 'embedded')}>
    <div class="cm-connect-h">
      <span class=${cls('cm-connect-ico', ok ? 'ok' : 'bad')}><${Icon} n=${ok ? 'agent' : 'plug'} /></span>
      <div><h2>${ok ? t('agents.conn.title') : t('agents.conn.gate_title')}</h2>
        <p class="muted">${ok ? t('agents.setup.lead_ok') : broken ? lt('connText', conn.state) : t('agents.setup.lead')}</p></div>
    </div>
    ${ok && !changing ? html`<${Connected} conn=${conn} onChange=${() => setChanging(true)} />`
      : html`<${Setup} initial=${conn.configured ? { url: conn.url, username: conn.username } : null} onDone=${() => setChanging(false)} />`}
    <p class="note muted">${t('agents.conn.security')}</p>
  </div>`;
}
