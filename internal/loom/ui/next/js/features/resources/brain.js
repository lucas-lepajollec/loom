import { t, locale } from '../../core/i18n.js';
// Brain : les sources de contexte de Loom (notes, docs, dépôts, discussions,
// mémoire), indexées sur cette machine. Les projets y puisent à chaque
// message ; les harnesses peuvent l'interroger par MCP.
import { html, useState, useEffect, useStore, useRef, cls, fmtBytes } from '../../core/lib.js';
import { app, go, refreshWorkspace } from '../../core/state.js';
import { open as openDiscussion } from '../chat/engine.js';
import { Icon } from '../../ui/icons.js';
import { Tip, Empty, Seg, Switch } from '../../ui/controls.js';
import { Modal, confirm, prompt, toast } from '../../ui/dialog.js';
import { FolderPicker } from '../../ui/folder.js';
import { get, post } from '../../core/api.js';
import { useVisibleRefresh } from '../usage/refresh.js';

export const brainTabs = () => [{ value: 'sources', label: t('second_brain.nav') }, { value: 'memory', label: t('memory.tab') }, { value: 'skills', label: t('resources.page.skills') }, { value: 'mcp', label: t('resources.page.serveurs_mcp') }];
const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');
// Sources intégrées : noms affichés en français.
export const brainLabel = s => ({ conversations: t("resources.brain.discussions_de_loom"), memory: t("resources.brain.memoire_de_l_agent_local"), distilled: t("resources.dist.section") })[s.id] || s.label;
const ago = localT => { const d = Date.parse(localT || ''); if (!d || d < 0) return t("resources.brain.jamais"); const m = Math.round((Date.now() - d) / 60000); return m < 1 ? t("resources.brain.a_l_instant") : m < 60 ? t('common.relative.minutes', { n: m }) : m < 1440 ? t('common.relative.hours', { n: Math.round(m / 60) }) : t('common.relative.days', { n: Math.round(m / 1440) }); };

function AddSource({ source, onClose }) {
  const [v, setV] = useState(source ? { ...source, permission: ['write', 'ask'].includes(source.permission) ? source.permission : 'read', include: (source.include || []).join('\n'), exclude: (source.exclude || []).join('\n') } : { path: '', remote: '', branch: '', label: '', kind: 'repo', connector: 'git-remote', permission: 'read', primary: false, include: '', exclude: '' });
  const [pick, setPick] = useState(false);
  const [busy, setBusy] = useState(false);
  const saving = useRef(false);
  const remote = v.connector === 'git-remote';
  const save = async () => {
    if (saving.current) return;
    saving.current = true; setBusy(true);
    try {
      const fallback = (remote ? v.remote : v.path).replace(/\/$/, '').split(/[/:]/).pop().replace(/\.git$/, '');
      const r = await post('/api/brain/sources', { action: 'add', id: source?.id || '', path: v.path, remote: v.remote, branch: v.branch, label: v.label || fallback, kind: v.kind, connector: v.connector || 'folder', permission: v.primary ? 'write' : v.permission, primary: !!v.primary, secondary: !v.primary && !!v.secondary, include: v.include.split('\n').map(s => s.trim()).filter(Boolean), exclude: v.exclude.split('\n').map(s => s.trim()).filter(Boolean) }, { timeout: 130000 });
      if (r.ok === false || r.error) return toast(r.error || t('resources.brain.ajout_impossible'), 'err');
      toast(t('resources.brain.source_ajoutee_indexation_en_cours')); post('/api/brain/reindex', {}).catch(e => toast(e.message, 'err')); onClose(true);
    } catch (e) { toast(e.message, 'err'); }
    finally { saving.current = false; setBusy(false); }
  };
  const connectors = [
    ['git-remote', 'box', t('second_brain.remote'), t('second_brain.remote_note')],
    ['folder', 'folder', t('second_brain.folder'), t('second_brain.folder_note')],
    ['obsidian', 'file', t('second_brain.obsidian'), t('second_brain.obsidian_note')],
    ['webdav-mount', 'globe', t('second_brain.webdav-mount'), t('second_brain.webdav_note')]
  ];
  return html`<${Modal} wide title=${source ? t('second_brain.edit') : t('second_brain.add')} sub=${t('second_brain.add_note')} onClose=${busy ? undefined : () => onClose()}
      foot=${html`<button class="btn ghost" disabled=${busy} onClick=${() => onClose()}>${t('resources.brain.annuler')}</button><button class="btn primary" disabled=${busy || (!remote && !v.path) || (remote && !v.remote)} onClick=${save}>${t(source ? 'ui.dialog.enregistrer' : 'resources.brain.ajouter')}</button>`}>
    <div class="brain-connectors">${connectors.map(([id, icon, label, note]) => html`<button type="button" class=${cls('brain-connector', v.connector === id && 'on')} disabled=${!!source} onClick=${() => setV({ ...v, connector: id, kind: id.includes('git') ? 'repo' : id === 'obsidian' ? 'personal' : 'context' })}><${Icon} n=${icon} /><span><b>${label}</b><small>${note}</small></span></button>`)}</div>
    ${remote ? html`<label class="field"><span>${t('second_brain.remote_url')}</span><input class="input mono" readonly=${!!source} value=${v.remote || ''} placeholder="https://forge.example/me/brain.git" onInput=${e => setV({ ...v, remote: e.target.value })} /><small>${t('second_brain.credentials')}</small></label><label class="field"><span>${t('second_brain.branch')}</span><input class="input mono" readonly=${!!source} value=${v.branch || ''} placeholder="main" onInput=${e => setV({ ...v, branch: e.target.value })} /></label>` : html`<div class="field"><span>${t('resources.brain.dossier')}</span>${v.path ? html`<button class="hs-dir" onClick=${() => setPick(true)}><${Icon} n="folder" /><span class="mono trunc">${home(v.path)}</span><span class="muted">${t('resources.brain.changer')}</span></button>` : html`<button class="btn" onClick=${() => setPick(true)}><${Icon} n="folder" />${t('resources.brain.choisir_un_dossier')}</button>`}</div>`}
    <label class="field"><span>${t('resources.brain.nom')}</span><input class="input" value=${v.label} placeholder=${(remote ? v.remote : v.path).split('/').pop()?.replace('.git', '') || t('resources.brain.ex_notes')} onInput=${e => setV({ ...v, label: e.target.value })} /></label>
    <div class="brain-role"><span class="lbl">${t('second_brain.role')}</span>
      <${Seg} label=${t('second_brain.role')} value=${v.primary ? 'primary' : v.secondary ? 'secondary' : 'context'} onChange=${r => setV({ ...v, primary: r === 'primary', secondary: r === 'secondary', permission: r === 'primary' ? 'write' : v.permission === 'write' && r !== 'primary' && v.primary ? 'read' : v.permission })}
        options=${[{ value: 'primary', label: t('second_brain.role.primary') }, { value: 'context', label: t('second_brain.role.context') }, { value: 'secondary', label: t('second_brain.role.secondary') }]} />
      <p class="note">${t(v.primary ? 'second_brain.role.primary_note' : v.secondary ? 'second_brain.role.secondary_note' : 'second_brain.role.context_note')}</p>
      ${!v.primary && html`<label class="field"><span>${t('second_brain.permission')}</span><select class="select" value=${v.permission} onChange=${e => setV({ ...v, permission: e.target.value })}><option value="read">${t('second_brain.read')}</option><option value="ask">${t('second_brain.ask')}</option><option value="write">${t('second_brain.write')}</option></select></label>`}</div>
    <details class="brain-advanced"><summary>${t('second_brain.advanced')}</summary><label class="field"><span>${t('second_brain.include')}</span><textarea class="textarea mono" rows="3" value=${v.include} placeholder="**/*.md" onInput=${e => setV({ ...v, include: e.target.value })}></textarea></label><label class="field"><span>${t('second_brain.exclude')}</span><textarea class="textarea mono" rows="3" value=${v.exclude} placeholder="private/**" onInput=${e => setV({ ...v, exclude: e.target.value })}></textarea></label></details>
    ${pick && html`<${FolderPicker} start=${v.path} onClose=${() => setPick(false)} onPick=${d => { setPick(false); setV({ ...v, path: d }); }} />`}
  </${Modal}>`;
}

function Search({ sources }) {
  const [q, setQ] = useState('');
  const [hits, setHits] = useState(null);
  const [selected, setSelected] = useState(new Set());
  const [personal, setPersonal] = useState(false);
  const scope = () => { const query = new URLSearchParams(); if (selected.size) query.set('sources', [...selected].join(',')); if (personal && sources.some(s => s.kind === 'personal' && selected.has(s.id))) query.set('personal', 'true'); return query.toString(); };
  const selectSource = id => { const next = new Set(selected); next.has(id) ? next.delete(id) : next.add(id); setSelected(next); setHits(null); };
  const [open, setOpen] = useState(null);
  const run = async e => {
    e && e.preventDefault();
    if (!q.trim()) return;
    const r = await get('/api/brain/search?limit=12&query=' + encodeURIComponent(q) + '&' + scope()).catch(() => null);
    setHits(r && r.hits ? r.hits : []);
  };
  const read = async h => { const r = await get('/api/brain/read?chunk_id=' + encodeURIComponent(h.chunk_id) + '&' + scope()).catch(() => null); if (r && r.text) setOpen({ ...h, text: r.text }); };
  const label = id => { const s = sources.find(x => x.id === id); return s ? brainLabel(s) : id; };
  const mark = h => { const out = []; let at = 0; const chars = [...h.snippet]; for (const r of h.highlights || []) { out.push(chars.slice(at, r.start).join('')); out.push(html`<mark>${chars.slice(r.start, r.end).join('')}</mark>`); at = r.end; } out.push(chars.slice(at).join('')); return out; };
  return html`<section class="sec"><div class="sec-h"><h2>${t("resources.brain.chercher_dans_le_brain")}<${Tip} text=${t('second_brain.search_note')} /></h2></div>
    <form class="brain-search" onSubmit=${run}><label class="search"><${Icon} n="search" /><input placeholder="${t("resources.brain.ex_nom_de_la_vm_de_dev_convention_de_commit")}" value=${q} onInput=${e => setQ(e.target.value)} /></label><button class="btn" type="submit">${t("resources.brain.chercher")}</button></form>
    <div class="chips">${sources.map(s => html`<button class=${cls('chip-btn', selected.has(s.id) && 'on')} disabled=${s.kind === 'personal' && !personal} onClick=${() => selectSource(s.id)}>${s.kind === 'personal' && html`<${Icon} n="lock" />`}${brainLabel(s)}</button>`)}</div>
    ${sources.some(s => s.kind === 'personal') && html`<label class="check"><input type="checkbox" checked=${personal} onChange=${e => { setPersonal(e.target.checked); setHits(null); if (!e.target.checked) setSelected(new Set([...selected].filter(id => sources.find(s => s.id === id)?.kind !== 'personal'))); }} /><span>${t('second_brain.personal_search')}</span></label>`}
    ${hits && (hits.length ? html`<div class="card rows">${hits.map(h => html`<button class="row brain-hit" key=${h.chunk_id} onClick=${() => read(h)}>
        <div class="grow"><div class="t"><span class="mono">${h.path}</span>${h.heading && h.heading.length > 0 && html`<span class="muted"> › ${h.heading.join(' › ')}</span>`}</div><div class="s">${mark(h)}</div></div>
        <span class="tag">${label(h.source)}</span></button>`)}</div>` : html`<p class="note">${t("resources.brain.aucun_passage_trouve")}</p>`)}
    ${open && html`<${Modal} wide title=${open.path} sub=${(open.heading || []).join(' › ')} onClose=${() => setOpen(null)}><pre class="brain-read">${open.text}</pre></${Modal}>`}
  </section>`;
}

// Recherche par le sens : un petit modèle d'embedding tourne sur cette machine
// (téléchargé à la demande), ou un fournisseur cloud choisi avec consentement.
// Sans lui, le Brain reste en recherche par mots-clés.
const EMBED_NAMES = { nomic: 'nomic-embed-text v1.5', 'bge-small': 'bge-small-en v1.5', 'nomic-v2': 'nomic-embed-text v2 MoE · multilingual' };
function Semantic() {
  const ws = useStore(app, x => x.workspace);
  const providers = ((ws && ws.providers) || []).filter(p => p.ready);
  const [st, setSt] = useState(null);
  const [busy, setBusy] = useState(false);
  const [cloud, setCloud] = useState({ provider: '', model: '', consent: false });
  const load = () => get('/api/brain/semantic').then(setSt).catch(() => setSt({ error: t('resources.sem.unavailable') }));
  useEffect(() => { load(); }, []);
  useEffect(() => { if (!st || !(st.downloading || st.indexing)) return; const id = setTimeout(load, 1500); return () => clearTimeout(id); }, [st]);
  const send = async body => {
    setBusy(true);
    const r = await post('/api/brain/semantic', body).catch(e => ({ error: e.message }));
    setBusy(false);
    if (r.ok === false || (r.error && !r.enabled && body.action !== 'disable')) toast(r.error || t('resources.sem.failed'), 'err');
    load();
    return r;
  };
  if (!st) return html`<div class="skeleton" style="height:120px"></div>`;
  const local = !st.provider_id;
  const mode = st.enabled ? (local ? 'local' : 'cloud') : null;
  const pct = st.chunks_total ? Math.round(st.chunks_embedded / st.chunks_total * 100) : 0;
  const ready = st.enabled && (local ? st.model_present : true);
  const provName = id => (providers.find(p => p.id === id) || {}).name || id;
  return html`<section class="sec"><div class="sec-h"><h2>${t('resources.sem.title')}<${Tip} text=${t('resources.sem.tip')} /></h2></div>
    <div class="card rows">
      <div class="row"><div class="grow"><div class="t">${t('resources.sem.enable')}</div><div class="s">${st.enabled ? (local ? t('resources.sem.local_on', { model: EMBED_NAMES[st.model] || st.model }) : t('resources.sem.cloud_on', { provider: provName(st.provider_id), model: st.model })) : t('resources.sem.off')}</div></div>
        <${Switch} label=${t('resources.sem.enable')} checked=${st.enabled} disabled=${busy} onChange=${on => send(on ? { action: 'enable', model: st.model || 'nomic' } : { action: 'disable' })} /></div>
      ${st.enabled && html`
        <div class="row"><div class="grow"><div class="t">${t('resources.sem.where')}</div><div class="s">${local ? t('resources.sem.where_local') : t('resources.sem.where_cloud')}</div></div>
          <${Seg} size="sm" label=${t('resources.sem.where')} value=${mode} onChange=${v => v === 'local' ? send({ action: 'enable', model: 'nomic' }) : setCloud({ ...cloud, open: true })} options=${[{ value: 'local', label: t('resources.sem.this_machine') }, { value: 'cloud', label: t('resources.sem.cloud'), disabled: !providers.length }]} /></div>
        ${local && html`<div class="row"><div class="grow"><div class="t">${t('resources.sem.model')}</div><div class="s">${st.downloading ? t('resources.sem.downloading', { done: fmtBytes(st.download_done), total: fmtBytes(st.download_total) }) : st.model_present ? t('resources.sem.model_here') : t('resources.sem.model_missing')}</div>
            ${st.downloading && html`<div class="meter" style="margin-top:8px"><i style=${`width:${st.download_total ? Math.round(st.download_done / st.download_total * 100) : 2}%;background:var(--text)`}></i></div>`}</div>
          <select class="select sm" style="min-width:190px" value=${st.model} disabled=${busy || st.downloading || st.indexing} onChange=${e => send({ action: 'enable', model: e.target.value })}>${(st.models || []).map(m => html`<option value=${m.id}>${EMBED_NAMES[m.id] || m.id}</option>`)}</select>
          ${!st.model_present && !st.downloading && html`<button class="btn sm primary" disabled=${busy} onClick=${() => send({ action: 'download' })}><${Icon} n="download" />${t('resources.sem.download')}</button>`}</div>`}
        <div class="row"><div class="grow"><div class="t">${t('brain.semantic.auto')}</div><div class="s">${t('brain.semantic.auto_note')}</div></div><${Switch} label=${t('brain.semantic.auto')} checked=${st.auto_index} disabled=${busy} onChange=${auto_index => send({ action: 'auto', auto_index })} /></div>
        <div class="row"><div class="grow"><div class="t">${t('resources.sem.index')} ${st.server_running && html`<span class="tag green">${t('resources.sem.server_on')}</span>`}</div>
            <div class="s">${t('resources.sem.progress', { done: st.chunks_embedded, total: st.chunks_total })}</div>
            <div class="meter" style="margin-top:8px"><i style=${`width:${pct}%;background:${pct === 100 ? 'var(--green)' : 'var(--text)'}`}></i></div></div>
          <button class="btn sm" disabled=${busy || !ready || st.indexing || st.downloading} onClick=${() => send({ action: 'index' })}>${st.indexing ? html`<span class="spinner"></span>${t('resources.sem.indexing')}` : html`<${Icon} n="refresh" />${t('resources.sem.index_now')}`}</button></div>`}
    </div>
    ${st.error && html`<p class="note err">${st.error}</p>`}
    ${cloud.open && html`<${Modal} title=${t('resources.sem.cloud_title')} sub=${t('resources.sem.cloud_sub')} onClose=${() => setCloud({ provider: '', model: '', consent: false })}
        foot=${html`<button class="btn ghost" onClick=${() => setCloud({ provider: '', model: '', consent: false })}>${t('resources.brain.annuler')}</button><button class="btn primary" disabled=${!cloud.provider || !cloud.model.trim() || !cloud.consent} onClick=${async () => { const r = await send({ action: 'enable', provider_id: cloud.provider, model: cloud.model.trim(), consent: true }); if (r.ok !== false) setCloud({ provider: '', model: '', consent: false }); }}>${t('resources.sem.use')}</button>`}>
      <label class="field"><span>${t('resources.sem.provider')}</span><select class="select" value=${cloud.provider} onChange=${e => setCloud({ ...cloud, provider: e.target.value })}><option value="">—</option>${providers.map(p => html`<option value=${p.id}>${p.name}</option>`)}</select></label>
      <label class="field"><span>${t('resources.sem.embed_model')}</span><input class="input mono" placeholder="text-embedding-3-small" value=${cloud.model} onInput=${e => setCloud({ ...cloud, model: e.target.value })} /></label>
      <label class="check"><input type="checkbox" checked=${cloud.consent} onChange=${e => setCloud({ ...cloud, consent: e.target.checked })} /><span>${t('resources.sem.consent')}</span></label>
    </${Modal}>`}
  </section>`;
}

// Mémoire distillée : décisions, faits, tâches et préférences tirés des
// discussions par le moteur de chat, uniquement à la demande. Chaque élément
// garde sa provenance et se supprime un par un.
const DKINDS = () => ({ decision: t('resources.dist.decision'), fact: t('resources.dist.fact'), todo: t('resources.dist.todo'), preference: t('resources.dist.preference') });
function DistillDialog({ onClose }) {
  const convs = useStore(app, x => (x.nav && x.nav.conversations) || []);
  const [mode, setMode] = useState('one');
  const [id, setId] = useState(convs[0] ? convs[0].id : '');
  const [since, setSince] = useState(new Date(Date.now() - 7 * 864e5).toISOString().slice(0, 10));
  const [busy, setBusy] = useState(false);
  const run = async consent => {
    setBusy(true);
    const body = mode === 'one' ? { discussion_id: id } : { since };
    const r = await post('/api/brain/distill', consent ? { ...body, consent: true } : body, { timeout: 610000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.error && /consent required/i.test(r.error) && !consent) {
      if (await confirm(t('resources.dist.remote_title'), t('resources.dist.remote_text'), { ok: t('resources.dist.send') })) return run(true);
      return;
    }
    if (r.ok === false || r.error) return toast(r.error || t('resources.dist.failed'), 'err');
    toast(t('resources.dist.done', { n: (r.items || []).length }));

    onClose(true);
  };
  return html`<${Modal} title=${t('resources.dist.title')} sub=${t('resources.dist.sub')} onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>${t('resources.brain.annuler')}</button><button class="btn primary" disabled=${busy || (mode === 'one' && !id)} onClick=${() => run(false)}>${busy ? html`<span class="spinner"></span>${t('resources.dist.running')}` : t('resources.dist.run')}</button>`}>
    <${Seg} label=${t('resources.dist.what')} value=${mode} onChange=${setMode} options=${[{ value: 'one', label: t('resources.dist.one') }, { value: 'since', label: t('resources.dist.since') }]} />
    ${mode === 'one' ? html`<label class="field"><span>${t('resources.dist.discussion')}</span><select class="select" value=${id} onChange=${e => setId(e.target.value)}>${convs.map(c => html`<option value=${c.id}>${c.title || t('app.palette.nouvelle_discussion')}</option>`)}</select></label>`
      : html`<label class="field"><span>${t('resources.dist.since_date')}</span><input class="input" type="date" value=${since} onInput=${e => setSince(e.target.value)} /></label>`}
    <p class="note">${t('resources.dist.engine_note')}</p>
  </${Modal}>`;
}
export function Distilled() {
  const [items, setItems] = useState(null);
  const [dlg, setDlg] = useState(false);
  const [kind, setKind] = useState('all');
  const load = () => get('/api/brain/distilled').then(r => setItems(r.items || [])).catch(() => setItems([]));
  useEffect(() => { load(); }, []);
  const del = async it => { const r = await post('/api/brain/distilled/delete', { id: it.id }).catch(e => ({ ok: false, error: e.message })); if (r.ok === false || r.error) return toast(r.error, 'err'); load(); };
  const review = async (it, status, edit = false) => {
    const text = edit ? await prompt(t('brain.review.edit'), { value: it.text }) : null;
    if (edit && !text) return;
    const r = await post('/api/brain/distilled/review', { id: it.id, review: status, ...(text === null ? {} : { text }) }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error, 'err');
    load();
  };
  const openSource = async it => { await openDiscussion(it.source.discussion_id); go('chat'); };
  const kinds = DKINDS();
  const list = (items || []).filter(it => it.review !== 'rejected' && (kind === 'all' || it.kind === kind)).slice().sort((a, b) => Date.parse(b.date) - Date.parse(a.date));
  const count = k => (items || []).filter(it => it.kind === k).length;
  return html`<section class="sec"><div class="sec-h"><h2>${t('resources.dist.section')}<${Tip} text=${t('resources.dist.tip')} /></h2>
      <button class="btn sm" onClick=${() => setDlg(true)}><${Icon} n="sparkle" />${t('resources.dist.open')}</button></div><p class="note">${t('brain.review.note')}</p>
    ${items === null ? html`<div class="skeleton" style="height:100px"></div>` : items.length ? html`
      <div class="dist-filter"><${Seg} size="sm" label=${t('resources.dist.filter')} value=${kind} onChange=${setKind} options=${[{ value: 'all', label: t('resources.dist.all'), count: items.length }, ...Object.keys(kinds).map(k => ({ value: k, label: kinds[k], count: count(k) || '' }))]} /></div>
      <div class="card rows" style="margin-top:10px">${list.map(it => html`<div class="row dist-item" key=${it.id}>
        <span class=${cls('tag', 'dk-' + it.kind)}>${kinds[it.kind] || it.kind}</span><span class="tag">${it.review === 'pending' ? t('brain.review.pending') : t('brain.review.accepted')}</span>
        <div class="grow"><div class="t dist-t">${it.text}</div><div class="s"><button class="linkish" onClick=${() => openSource(it)}>${t('resources.dist.from', { n: it.source.message_index + 1 })}</button> · ${new Date(it.date).toLocaleDateString(locale())}</div></div>
        <div class="btn-row">${it.review === 'pending' && html`<button class="btn sm" onClick=${() => review(it, 'accepted')}>${t('brain.review.accept')}</button>`}<button class="btn sm ghost" onClick=${() => review(it, 'accepted', true)}>${t('brain.review.edit')}</button><button class="btn sm ghost" onClick=${() => review(it, 'rejected')}>${t('brain.review.reject')}</button></div>
        <button class="icon-btn" aria-label=${t('resources.dist.delete')} title=${t('resources.dist.delete')} onClick=${() => del(it)}><${Icon} n="close" /></button></div>`)}</div>`
      : html`<div class="card"><${Empty} icon="brain" title=${t('resources.dist.empty_title')} text=${t('resources.dist.empty_text')}><button class="btn primary" onClick=${() => setDlg(true)}>${t('resources.dist.open')}</button></${Empty}></div>`}
    ${dlg && html`<${DistillDialog} onClose=${ok => { setDlg(false); if (ok) load(); }} />`}
  </section>`;
}

export function Brain({ section = 'overview' }) {
  const [data, setData] = useState(null);
  const [dlg, setDlg] = useState(false);
  const [busy, setBusy] = useState(false);
  const [skillPick, setSkillPick] = useState(null);
  const load = () => get('/api/brain/sources').then(r => setData(r.sources ? r : { sources: [], error: r.error })).catch(() => setData({ sources: [] }));
  useVisibleRefresh(async alive => {
    try {
      const r = await get('/api/brain/sources', { timeout: 10000, retryAuth: false });
      if (alive()) setData(r.sources ? r : { sources: [], error: r.error });
    } catch (e) { if (alive()) setData(old => ({ ...(old || { sources: [] }), error: e.message })); }
  }, 15000);
  const reindex = async () => { setBusy(true); const r = await post('/api/brain/reindex', {}); setBusy(false); if (r.ok === false || r.error) return toast(r.error || t("resources.brain.indexation_impossible"), 'err'); toast(t("resources.brain.index_a_jour")); load(); };
  const remove = async s => { if (!await confirm(t("resources.brain.retirer") + s.label, t("resources.brain.la_source_n_est_plus_indexee_le_dossier_n_est_pas_modifie"), { ok: t("resources.brain.retirer_2") })) return; await post('/api/brain/sources', { action: 'remove', id: s.id }); load(); };
  const sync = async s => { setBusy(true); const r = await post('/api/brain/sources', { action: 'sync', id: s.id }, { timeout: 100000 }).catch(e => ({ error: e.message })); setBusy(false); if (r.error || r.ok === false) return toast(r.error, 'err'); toast(t('second_brain.synced')); load(); };
  const linkSkills = async path => { const r = await post('/api/skills/sources', { path, label: skillPick.label }); if (!r.ok) return toast(r.error, 'err'); toast(t('second_brain.skills_linked')); setSkillPick(null); refreshWorkspace(); };
  if (!data) return html`<div class="skeleton" style="height:200px;margin-top:14px"></div>`;
  const sources = data.sources || [];
  const connected = sources.filter(s => !s.read_only);
  const freshness = source => {
    if (source.error) return t('resources.brain.erreur');
    if (data.refreshing) return t('brain.refresh.running');
    const last = Date.parse(source.last_checked || source.last_indexed);
    if (!last || last < 1) return t('brain.refresh.pending');
    return Date.now() - last > 360000 ? t('brain.refresh.stale') : ago(source.last_checked || source.last_indexed);
  };
  const connector = s => ({ 'git-remote': t('second_brain.remote'), git: t('second_brain.git'), folder: t('second_brain.folder'), obsidian: t('second_brain.obsidian'), 'webdav-mount': t('second_brain.webdav-mount') })[s.connector] || t('second_brain.folder');
  const permission = s => s.primary ? t('second_brain.primary') : (s.secondary ? t('second_brain.role.secondary') + ' · ' : '') + (s.permission === 'write' ? t('second_brain.write') : s.permission === 'ask' ? t('second_brain.ask') : t('second_brain.read'));
  return html`<div class="second-brains">
    <div class="brain-intro"><p>${t('second_brain.intro')}</p><div class="acts"><button class="btn" disabled=${busy} onClick=${reindex}><${Icon} n="refresh" />${busy ? t('resources.brain.indexation') : t('resources.brain.reindexer')}</button><button class="btn primary" onClick=${() => setDlg(true)}><${Icon} n="plus" />${t('second_brain.add')}</button></div></div>
    ${data.error && html`<p class="note err">${data.error}</p>`}
    ${connected.length ? html`<div class="brain-grid">${connected.map(s => html`<article class=${cls('card', 'second-brain-card', s.primary && 'primary')} key=${s.id}>
      <div class="second-brain-head"><span class="mono-tile"><${Icon} n=${s.connector?.includes('git') ? 'box' : s.kind === 'personal' ? 'file' : 'folder'} /></span><div class="grow"><div class="second-brain-title"><b>${s.label}</b>${s.primary && html`<span class="tag">${t('second_brain.primary')}</span>`}</div><span>${connector(s)} · ${permission(s)}</span></div><button class="icon-btn" aria-label=${t('second_brain.edit')} onClick=${() => setDlg(s)}><${Icon} n="edit" /></button></div>
      <div class="second-brain-path mono" title=${s.remote || s.path}>${s.remote || home(s.path)}</div>
      <div class="second-brain-status">${s.error ? html`<span class="state err"><i class="dot red"></i>${s.error}</span>` : html`<span class="state"><i class="dot green"></i>${s.files} ${t('resources.brain.fichier')}${s.files > 1 ? 's' : ''} · ${freshness(s)}</span>`}</div>
      <div class="second-brain-actions">${s.remote && html`<button class="btn sm ghost" disabled=${busy} onClick=${() => sync(s)}><${Icon} n="refresh" />${t('second_brain.sync')}</button>`}<button class="btn sm ghost" onClick=${() => setSkillPick(s)}><${Icon} n="sparkle" />${t('second_brain.link_skills')}</button><span class="grow"></span><button class="btn sm ghost" onClick=${() => remove(s)}>${t('resources.brain.retirer_2')}</button></div>
    </article>`)}</div>` : html`<div class="card brain-empty"><${Empty} icon="brain" title=${t('second_brain.empty')} text=${t('second_brain.empty_note')}><button class="btn primary" onClick=${() => setDlg(true)}>${t('second_brain.add')}</button></${Empty}></div>`}
    <div class="loom-memory-strip"><span class="mono-tile sm"><${Icon} n="brain" /></span><div><b>${t('second_brain.loom_memory')}</b><p>${t('second_brain.loom_memory_note')}</p></div></div>
    ${dlg && html`<${AddSource} source=${dlg === true ? null : dlg} onClose=${ok => { setDlg(false); if (ok) setTimeout(load, 1500); }} />`}
    ${skillPick && html`<${FolderPicker} start=${skillPick.path} onClose=${() => setSkillPick(null)} onPick=${linkSkills} />`}
  </div>`;
}
