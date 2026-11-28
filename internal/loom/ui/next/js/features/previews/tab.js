import { html, useState, useRef, useEffect, useLayoutEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { copyText } from '../../ui/clipboard.js';
import { toast } from '../../ui/dialog.js';
import { useOrchestrationLive } from '../agents/orchestration.js';
import { matchRenderer } from './renderers.js';
import { appPath, fileQuery } from './navigation.js';

const identity = item => JSON.stringify([item.kind, item.target, item.port || 0, item.path]);
const file = (path, target) => ({ kind: 'file', path, target });
const title = item => item.label || (item.kind === 'port' ? ':' + item.port : item.path.split('/').pop());
function RenderItem(props) {
  const [loaded, setView] = useState(null), [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    props.renderer.load().then(View => { if (active) setView(() => View); }).catch(e => { if (active) setError(e.message); });
    return () => { active = false; };
  }, [props.renderer]);
  const View = loaded;
  return View ? html`<${View} ...${props} />` : html`<p role="status">${error || t('connections.mcp.loading')}</p>`;
}

function WebLauncher({ suggestion, onOpen, cancel }) {
  const [target, setTarget] = useState(suggestion.target || 'local'), [machines, setMachines] = useState([]);
  const [port, setPort] = useState(String(suggestion.port || '')), [ports, setPorts] = useState([]);
  const [path, setPath] = useState(suggestion.path || '/'), [busy, setBusy] = useState(false), [error, setError] = useState('');
  const live = useRef(true), submitting = useRef(false);
  useEffect(() => {
    live.current = true;
    get('/api/machines').then(r => { if (live.current) setMachines(r.machines || []); }).catch(() => {});
    return () => { live.current = false; };
  }, []);
  useEffect(() => {
    let active = true;
    setPorts([]);
    get('/api/previews/ports?target=' + encodeURIComponent(target)).then(r => { if (active) setPorts(r.ports || []); }).catch(() => {});
    return () => { active = false; };
  }, [target]);
  const machine = machines.find(m => m.id === target);
  const needsSSH = machine?.node_id && !machine.user;
  async function open(e) {
    e.preventDefault();
    if (submitting.current) return;
    const item = { kind: 'port', target, port: Number(port), path: appPath(path), label: suggestion.label };
    if (!item.path) { setError(t('preview.bad_path')); return; }
    submitting.current = true; setBusy(true);
    const r = await post('/api/previews', { action: 'open', target, port: item.port, path: item.path, consent: true });
    if (!live.current) return;
    submitting.current = false; setBusy(false);
    if (r.ok) onOpen(item, r);
    else setError(r.error);
  }
  return html`<form class="up-launch" onSubmit=${open}>
    <label class="field"><span>${t('workspaces.machine')}</span><select value=${target} onChange=${e => setTarget(e.currentTarget.value)}>
      <option value="local">${t('workspaces.local')}</option>${machines.map(m => html`<option key=${m.id} value=${m.id}>${m.name || m.host}</option>`)}
    </select></label>
    <label class="field"><span>${t('previews.port')}</span><input class="input" type="number" required min="1024" max="65535" value=${port} onInput=${e => setPort(e.currentTarget.value)} /></label>
    ${ports.length > 0 && html`<div class="up-toolbar" aria-label=${t('preview.ports')}>${ports.map(n => html`<button type="button" class="btn sm" key=${n} aria-pressed=${port === String(n)} onClick=${() => setPort(String(n))}>:${n}</button>`)}</div>`}
    <label class="field"><span>${t('preview.path')}</span><input class="input mono" value=${path} onInput=${e => setPath(e.currentTarget.value)} /></label>
    <p class="note">${t(needsSSH ? 'preview.ssh_needed' : 'previews.note')}</p>${error && html`<p role="alert">${error}</p>`}
    <div class="up-toolbar"><button class="btn primary" disabled=${busy || needsSSH}>${t('previews.open')}</button>
    <button class="btn" type="button" onClick=${cancel} disabled=${busy}>${t('ui.dialog.annuler')}</button></div></form>`;
}

function useSuggestions(id) {
  const [items, setItems] = useState([]);
  useOrchestrationLive();
  useEffect(() => {
    if (!id) return;
    let revision = 0;
    const load = async () => {
      const current = ++revision;
      try {
        const r = await get('/api/previews/session?session_id=' + encodeURIComponent(id));
        if (current === revision) setItems(r.previews || []);
      } catch { /* Keep the last list during reconnect. */ }
    };
    const event = e => { if (!e.detail || e.detail === id) load(); };
    window.addEventListener('loom:preview', event); load();
    return () => { revision++; window.removeEventListener('loom:preview', event); };
  }, [id]);
  return items;
}

export function PreviewTab({ session, preview, onClose, onFile }) {
  const pushed = useSuggestions(session.id);
  const [picked, setPicked] = useState(null), [recent, setRecent] = useState([]), [grant, setGrant] = useState(null);
  const [launcher, setLauncher] = useState(null), [full, setFull] = useState(false), [revision, refresh] = useState(0);
  const [target, setTarget] = useState(session.agent ? '' : (session.target || 'local'));
  const dialog = useRef(), expand = useRef(), runningApps = useRef(new Map());
  useEffect(() => {
    let active = true;
    if (session.agent) get('/api/machines/roles').then(r => { if (active) setTarget(r.roles?.codeman || ''); }).catch(() => {});
    return () => { active = false; };
  }, [session.agent]);
  // openPreview also replaces the recent list when the user reopens the
  // same path from a terminal link while a different viewer is showing.
  const fileSelection = session.agent ? preview.recent : preview.active;
  useEffect(() => {
    if (preview.active) { setPicked(null); setLauncher(null); setGrant(null); }
  }, [preview.active, fileSelection]);
  useLayoutEffect(() => {
    const el = dialog.current;
    if (full) { el.close(); el.showModal(); }
    else if (!el.open) el.show();
    return () => el.close();
  }, [full]);
  function reduce() { setFull(false); requestAnimationFrame(() => expand.current?.focus()); }
  const preferred = preview.active || preview.detected[0] || preview.recent[0];
  const active = picked || (preferred && file(preferred, target));
  const items = new Map();
  for (const item of [
    ...pushed.map(p => ({ ...p, explicitTarget: true })), ...recent,
    ...[...preview.recent, ...preview.detected].map(p => file(p, target)),
    ...(preview.urls || []).map(u => {
      const url = new URL(u); return { kind: 'port', target, port: Number(url.port), path: url.pathname + url.search + url.hash };
    }),
  ]) {
    if (!items.has(identity(item))) items.set(identity(item), item);
  }
  function select(item, opened) {
    if (item.kind === 'port') {
      const key = JSON.stringify([item.target, item.port]);
      if (opened) {
        runningApps.current.set(key, opened.preview);
        if (session.id) {
          post('/api/previews/session', { ...item, session_id: session.id }).then(result => {
            if (!result.ok) toast(result.error, 'err');
          });
        }
      } else {
        const info = runningApps.current.get(key);
        if (!info || info.expires * 1000 <= Date.now()) { setLauncher(item); return; }
        opened = { preview: info, url: info.origin + (appPath(item.path || '/') || '/') };
      }
    }
    setLauncher(null); setGrant(opened || null);
    setRecent(list => [item, ...list.filter(p => identity(p) !== identity(item))].slice(0, 16));
    if (item.kind === 'file' && !item.explicitTarget) onFile?.(item.path);
    setPicked(item);
  }
  async function close() {
    if (grant && active?.kind === 'port') {
      const result = await post('/api/previews', { action: 'close', id: grant.preview.id });
      if (!result.ok) { toast(result.error, 'err'); return; }
      runningApps.current.delete(JSON.stringify([active.target, active.port]));
    }
    setGrant(null); setPicked(null); setFull(false); onClose();
  }
  const renderer = active && matchRenderer(active);
  const fileURL = path => '/api/files/raw?' + fileQuery(active, session, path);
  // Codeman may run outside any paired machine. Relative text still uses its
  // official file-content endpoint. Never substitute the local binary file.
  const missingMachine = active?.kind === 'file' && !active.target &&
    (!renderer?.readsText || active.path.startsWith('/'));
  return html`<dialog ref=${dialog} aria-label=${t('agents.insp.preview')} class=${'cm-preview up-tab' + (full ? ' up-full' : '')}
    onCancel=${e => { e.preventDefault(); reduce(); }} onKeyDown=${e => { if (full && e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); reduce(); } }}>
    <div class="cm-preview-head"><button class="btn sm" onClick=${() => setLauncher({ target: target || 'local' })}>${t('preview.add_web')}</button>
      <button ref=${expand} class="btn sm" aria-pressed=${full} onClick=${() => full ? reduce() : setFull(true)}>${t(full ? 'preview.reduce' : 'preview.expand')}</button>
      <button class="btn sm" onClick=${close}>${t('agents.preview.close')}</button></div>
    ${items.size > 0 && html`<div class="cm-preview-pills">${[...items].map(([key, item]) => html`<button key=${key} class=${'cm-preview-pill' + (active && key === identity(active) ? ' on' : '')} title=${[item.target, item.path].filter(Boolean).join(' · ')}
      onClick=${() => select(item)}><span aria-hidden="true">${matchRenderer(item)?.icon}</span><span class="trunc">${title(item)}</span></button>`)}</div>`}
    <div class="cm-preview-body">
    ${launcher ? html`<${WebLauncher} key=${identity(launcher)} suggestion=${launcher} onOpen=${select} cancel=${() => setLauncher(null)} />`
    : !active ? html`<p class="muted">${t('preview.empty')}</p>`
    : html`<div class="up-location"><b class="mono">${active.kind === 'file' ? active.path : active.target + ' · :' + active.port}</b>
      ${active.kind === 'file' && html`<div class="up-toolbar">
        <button class="btn sm" onClick=${() => copyText(active.path).then(() => toast(t('agents.copied')))}>${t('agents.files.copy_path')}</button>
        <button class="btn sm" onClick=${() => refresh(n => n + 1)}>${t('common.refresh')}</button>
        ${active.target && !renderer.readsText && html`<a class="btn sm" href=${fileURL(active.path)} target="_blank" rel="noopener noreferrer">${t('preview.external')}</a>`}</div>`}</div>
      ${missingMachine ? html`<p role="status">${t('preview.unknown_machine')}</p>` : html`<${RenderItem} key=${identity(active) + revision} renderer=${renderer} grant=${grant}
        item=${active} session=${session} select=${select} fileURL=${fileURL} />`}`}
    </div></dialog>`;
}
