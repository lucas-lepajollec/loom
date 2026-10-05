import { html, useState, useEffect, useStore, useRef } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshNav } from '../../core/state.js';
import { open as openChat } from '../chat/engine.js';
import { Icon } from '../../ui/icons.js';
import { confirm, toast } from '../../ui/dialog.js';

// Native histories stay on their source machine. Only an explicitly selected
// transcript is imported, then continued in a fresh destination session.
export function HarnessHistory() {
  const ws = useStore(app, s => s.workspace);
  const projects = useStore(app, s => s.nav.projects);
  const entry = useStore(app, s => s.route.id);
  const machine = entry?.startsWith('remote:') ? entry.split(':')[1] : entry;

  const [sources, setSources] = useState([]), [source, setSource] = useState('');
  const [rows, setRows] = useState(null), [error, setError] = useState('');
  const [choice, setChoice] = useState(''), [project, setProject] = useState('');
  const [busy, setBusy] = useState(''), [query, setQuery] = useState(''), [revision, reload] = useState(0);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; const ac = new AbortController();
    get('/api/harness-history', { signal: ac.signal }).then(r => {
      if (!mounted.current) return;
      if (!r.ok) { setError(r.error); return; }
      setSources(r.sources || []); setSource(s => r.sources?.find(x => entry && x.id === entry)?.id || r.sources?.find(x => machine && x.id.startsWith('remote:' + machine + ':'))?.id || (r.sources?.some(x => x.id === s) ? s : '') || r.sources?.[0]?.id || '');
    }).catch(e => { if (mounted.current && !ac.signal.aborted) setError(e.message); });
    return () => { mounted.current = false; ac.abort(); };
  }, [entry]);
  useEffect(() => {
    setRows(null); setError(''); if (!source) return;
    const ac = new AbortController();
    get('/api/harness-history?source=' + encodeURIComponent(source), { timeout: 95000, signal: ac.signal }).then(r => {
      if (ac.signal.aborted) return;
      if (r.ok) setRows(r.sessions || []); else { setRows([]); setError(r.error); }
    }).catch(e => { if (!ac.signal.aborted) { setRows([]); setError(e.message); } });
    return () => ac.abort();
  }, [source, revision]);
  const destinations = (ws?.models || []).filter(m => m.kind === 'harness' && m.ready && m.enabled);
  const runtimeName = id => (ws?.runtimes || []).find(r => r.id === id);
  const transfer = async row => {
    if (!choice || busy) return;
    if (!await confirm(t('history.transfer'), t('history.consent'), { ok: t('history.transfer') })) return;
    setBusy(row.sessionId);
    try {
      const r = await post('/api/harness-history/transfer', { source, choice_id: choice, project_id: project, sessionId: row.sessionId, cwd: row.cwd, title: row.title, consent: true }, { timeout: 190000 });
      if (!mounted.current) return;
      if (!r.ok) { setError(r.error); return; }
      toast(t('history.done')); await refreshNav(); await openChat(r.session.id); go('chat');
    } finally { if (mounted.current) setBusy(''); }
  };
  const filtered = (rows || []).filter(r => [r.title, r.cwd, r.sessionId].join(' ').toLowerCase().includes(query.toLowerCase()));
  return html`<div class="view page"><div class="page-in wide">
    <button class="btn ghost sm back" onClick=${() => go('settings', 'machines', machine || '')}><${Icon} n="left" />${t('settings.page.machines')}</button>
    <div class="page-head"><div><h1>${t('history.title')}</h1><p>${t('history.intro')}</p></div></div>
    <section class="sec"><div class="card pad history-options">
      <label class="field"><span>${t('history.source')}</span><select class="select" disabled=${!!busy} value=${source} onChange=${e => setSource(e.target.value)}>
        <option value="">${t('history.choose')}</option>${sources.map(s => html`<option key=${s.id} value=${s.id}>${s.name} · ${s.machine === 'local' ? t('history.local') : s.machine}</option>`)}</select></label>
      <label class="field"><span>${t('history.destination')}</span><select class="select" disabled=${!!busy} value=${choice} onChange=${e => setChoice(e.target.value)}>
        <option value="">${t('history.choose')}</option>${destinations.map(m => { const rt = runtimeName(m.runtime_id); return html`<option key=${m.id} value=${m.id}>${rt?.name || m.runtime_id} · ${rt?.machine || t('history.local')} · ${m.name}</option>`; })}</select></label>
      <label class="field"><span>${t('harnesses.import.project')}</span><select class="select" disabled=${!!busy} value=${project} onChange=${e => setProject(e.target.value)}><option value="">—</option>${projects.map(p => html`<option key=${p.id} value=${p.id}>${p.name || p.title}</option>`)}</select></label>
    </div><p class="note">${t('history.limit')}</p></section>
    ${error && html`<div class="notice warn" role="alert">${error}</div>`}
    <section class="sec"><div class="sec-h"><h2>${t('history.sessions')}</h2><button class="btn sm ghost" disabled=${!source || !!busy} onClick=${() => reload(n => n + 1)}><${Icon} n="refresh" />${t('harnesses.page.actualiser')}</button></div>
      <label class="search"><${Icon} n="search" /><input aria-label=${t('harnesses.page.filtrer_les_sessions')} placeholder=${t('harnesses.page.filtrer')} value=${query} onInput=${e => setQuery(e.target.value)} /></label>
      ${!source ? html`<div class="card pad"><p class="note">${t('history.no_sources')}</p></div>` : rows === null ? html`<div class="skeleton" style="height:120px"></div>` : filtered.length ? html`<div class="card rows">${filtered.map(row => html`<div class="row history-row" key=${row.sessionId}><div class="grow"><div class="t">${row.title || row.sessionId}</div><div class="s">${row.cwd || ''}</div></div><button class="btn sm" disabled=${!choice || !!busy} onClick=${() => transfer(row)}>${busy === row.sessionId ? t('history.transferring') : t('history.transfer')}</button></div>`)}</div>` : html`<div class="card pad"><p class="note">${t('history.empty')}</p></div>`}
    </section>
  </div></div>`;
}
