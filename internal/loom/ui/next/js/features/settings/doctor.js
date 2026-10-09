// Loom Doctor : un bilan de santé lisible (cœur, machines, moteur, agents,
// cerveau, notifications, sécurité) avec la marche à suivre pour chaque
// problème, et un paquet de diagnostic anonymisé à joindre à un rapport de bug.
// Contrat : docs/doctor.md.
import { html, useState, useEffect, useStore } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { app } from '../../core/state.js';
import { toast } from '../../ui/dialog.js';
import { Icon } from '../../ui/icons.js';
import { Group } from './kit.js';

const AREAS = ['core', 'engine', 'machines', 'agents', 'brain', 'notifications', 'security'];
const TONE = { ok: 'green', warn: 'amber', fail: 'red', skip: '' };
const AREA_LABEL = () => ({ core: t('doctor.area.core'), engine: t('doctor.area.engine'), machines: t('doctor.area.machines'), agents: t('doctor.area.agents'), brain: t('doctor.area.brain'), notifications: t('doctor.area.notifications'), security: t('doctor.area.security') });
const fmtBytes = n => n >= 1e9 ? (n / 1e9).toFixed(1) + ' Go' : (n / 1e6).toFixed(0) + ' Mo';

// Titre lisible : les vérifications par entité réutilisent le nom connu.
function title(check, names) {
  const [kind, ...rest] = check.id.split('.');
  const id = rest.join('.');
  if (kind === 'agent') return t('doctor.t.agent', { name: names.agents[id] || id });
  if (kind === 'machine') return t('doctor.t.machine', { name: names.machines[id] || id });
  if (kind === 'mcp') return t('doctor.t.mcp', { name: id });
  const key = 'doctor.t.' + check.id;
  const s = t(key);
  return s === key ? check.id : s;
}
function detail(check) {
  const d = String(check.detail || '');
  if (check.id === 'core.version') return d.replace('; ', ' · ');
  if (d.startsWith('available_bytes:')) return t('doctor.d.available', { size: fmtBytes(Number(d.split(':')[1])) });
  const key = 'doctor.d.' + d;
  const s = t(key);
  return s === key ? d : s;
}

export function DoctorSettings() {
  const ws = useStore(app, a => a.workspace);
  const [report, setReport] = useState(null), [busy, setBusy] = useState(false), [error, setError] = useState('');
  const run = async () => {
    setBusy(true); setError('');
    const r = await post('/api/doctor/run', {}, { timeout: 60000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false) return setError(r.error || t('doctor.failed'));
    setReport(r);
  };
  useEffect(() => { run(); }, []);
  const bundle = async () => {
    setBusy(true);
    try {
      const res = await fetch('/api/doctor/bundle', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}', credentials: 'same-origin' });
      if (!res.ok) throw new Error(t('doctor.bundle_failed'));
      const url = URL.createObjectURL(await res.blob());
      const a = document.createElement('a'); a.href = url; a.download = 'loom-diagnostics.zip'; a.click();
      setTimeout(() => URL.revokeObjectURL(url), 5000);
      toast(t('doctor.bundle_ready'));
    } catch (e) { toast(e.message, 'err'); }
    setBusy(false);
  };
  const names = { agents: Object.fromEntries(((ws && ws.runtimes) || []).map(r => [r.id, r.name])), machines: {} };
  ((ws && ws.machines) || []).forEach(m => { names.machines[m.id] = m.name; });
  const checks = (report && report.checks) || [];
  const count = s => checks.filter(c => c.status === s).length;
  return html`
    <${Group} title=${t('doctor.title')}>
      <div class="set-line"><div class="set-l"><span>${report ? t('doctor.summary', { fail: count('fail'), warn: count('warn'), ok: count('ok') }) : t('doctor.running')}</span></div>
        <div class="set-c"><button class="btn sm" disabled=${busy} onClick=${run}><${Icon} n="refresh" />${t('doctor.run')}</button></div></div>
      ${error && html`<div class="pad"><p class="note err">${error}</p></div>`}
    </${Group}>
    ${AREAS.map(area => {
      const list = checks.filter(c => c.area === area).sort((a, b) => ['fail', 'warn', 'ok', 'skip'].indexOf(a.status) - ['fail', 'warn', 'ok', 'skip'].indexOf(b.status));
      return list.length > 0 && html`<${Group} key=${area} title=${AREA_LABEL()[area]}>
        ${list.map(c => html`<div class="set-line doc-row" key=${c.id}>
          <div class="set-l"><i class=${'dot ' + (TONE[c.status] || '')}></i><span>${title(c, names)}</span></div>
          <div class="set-c"><span class=${'note' + (c.status === 'fail' ? ' err' : '')}>${detail(c)}</span>
            ${c.fix && c.fix.href && c.status !== 'ok' && html`<a class="btn sm ghost" href=${c.fix.href}>${t('doctor.fix')}</a>`}</div></div>`)}
      </${Group}>`;
    })}
    <${Group} title=${t('doctor.bundle')}>
      <div class="pad"><p class="note">${t('doctor.bundle_note')}</p></div>
      <div class="set-actions"><button class="btn" disabled=${busy} onClick=${bundle}><${Icon} n="download" />${t('doctor.bundle_download')}</button></div>
    </${Group}>`;
}
