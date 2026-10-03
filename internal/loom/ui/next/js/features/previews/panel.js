import { html, useState, useEffect, useRef } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { Icon } from '../../ui/icons.js';
import { Modal, toast, confirm } from '../../ui/dialog.js';

async function launch(body) {
  // Reserve the tab during the user gesture, before awaiting SSH/API work.
  const tab = window.open('about:blank', '_blank');
  if (tab) tab.opener = null;
  try {
    const r = await post('/api/previews', { ...body, consent: true });
    if (!r.ok) throw new Error(r.error || t('previews.failed'));
    if (tab) tab.location.replace(r.url);
    else { toast(t('previews.popup_blocked'), 'err'); return { ...r, blocked: true }; }
    return r;
  } catch (e) { if (tab) tab.close(); throw e; }
}
function PreviewForm({ target = 'local', onClose, onDone }) {
  const [machines, setMachines] = useState([]);
  const [where, setWhere] = useState(target);
  const [port, setPort] = useState('5173');
  const [busy, setBusy] = useState(false);
  const running = useRef(false);
  useEffect(() => { get('/api/machines').then(r => setMachines(r.machines || [])).catch(() => {}); }, []);
  const open = async () => {
    if (running.current) return;
    running.current = true; setBusy(true);
    try { const r = await launch({ action: 'open', target: where, port: Number(port) }); onDone(r); onClose(); }
    catch (e) { toast(e.message, 'err'); }
    finally { running.current = false; setBusy(false); }
  };
  return html`<${Modal} title=${t('previews.title')} onClose=${busy ? undefined : onClose}
    foot=${html`<button class="btn ghost" disabled=${busy} onClick=${onClose}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || Number(port) < 1024 || Number(port) > 65535} onClick=${open}>${t('previews.open')}</button>`}>
    <label class="field"><span>${t('workspaces.machine')}</span><select class="select" value=${where} onChange=${e => setWhere(e.target.value)}><option value="local">${t('workspaces.local')}</option>${machines.map(m => html`<option value=${m.id}>${m.name || m.host}</option>`)}</select></label>
    <label class="field"><span>${t('previews.port')}</span><input class="input mono" type="number" min="1024" max="65535" value=${port} onInput=${e => setPort(e.target.value)} /></label>
    <p class="note">${t('previews.note')}</p>
  </${Modal}>`;
}
export function PreviewPanel({ target = 'local' }) {
  const [items, setItems] = useState([]);
  const [form, setForm] = useState(false);
  const [busy, setBusy] = useState('');
  const [fallback, setFallback] = useState('');
  const load = () => get('/api/previews').then(r => setItems(r.previews || [])).catch(() => {});
  useEffect(() => { load(); }, []);
  const open = async p => {
    setBusy(p.id);
    try { const r = await launch({ action: 'ticket', id: p.id }); setFallback(r.blocked ? r.url : ''); }
    catch (e) { toast(e.message, 'err'); }
    finally { setBusy(''); }
  };
  const close = async p => {
    if (!await confirm(t('previews.close'), t('previews.close_note'))) return;
    const r = await post('/api/previews', { action: 'close', id: p.id });
    if (!r.ok) return toast(r.error, 'err');
    setFallback(''); load();
  };
  return html`<section class="sec"><div class="sec-h"><h2>${t('previews.title')}</h2><button class="btn sm" onClick=${() => setForm(true)}><${Icon} n="globe" />${t('previews.add')}</button></div>
    ${items.length > 0 && html`<div class="card rows">${items.map(p => html`<div class="row" key=${p.id}><div class="grow"><div class="t">${p.target === 'local' ? t('workspaces.local') : p.target} · ${p.port}</div><div class="s mono">${p.origin}</div></div><button class="btn sm" disabled=${!!busy} onClick=${() => open(p)}>${t('previews.open')}</button><button class="icon-btn" aria-label=${t('previews.close')} onClick=${() => close(p)}><${Icon} n="close" /></button></div>`)}</div>`}
    ${fallback && html`<a class="btn" href=${fallback} target="_blank" rel="noopener noreferrer">${t('previews.open')}</a>`}
    ${form && html`<${PreviewForm} target=${target} onClose=${() => setForm(false)} onDone=${r => { setFallback(r.blocked ? r.url : ''); load(); }} />`}
  </section>`;
}
