import { t } from '../../core/i18n.js';
// Brain : les sources de contexte de Loom (notes, docs, dépôts, discussions,
// mémoire), indexées sur cette machine. Les projets y puisent à chaque
// message ; les harnesses peuvent l'interroger par MCP.
import { html, useState, useEffect, useStore, useRef, cls, fmtBytes } from '../../core/lib.js';
import { app, refreshWorkspace } from '../../core/state.js';
import { Icon } from '../../ui/icons.js';
import { Tip, Seg, Switch, Menu } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { FolderPicker } from '../../ui/folder.js';
import { get, post } from '../../core/api.js';
import { useVisibleRefresh } from '../usage/refresh.js';

export const brainTabs = () => [{ value: 'sources', label: t('brain.tab.sources') }, { value: 'memory', label: t('memory.tab') }, { value: 'search', label: t('brain.tab.search') }, { value: 'skills', label: t('resources.page.skills') }, { value: 'mcp', label: t('brain.tab.mcp') }];
const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');
// Sources intégrées : noms affichés en français.
export const brainLabel = s => ({ conversations: t("resources.brain.discussions_de_loom"), memory: t("resources.brain.memoire_de_l_agent_local"), distilled: t("resources.dist.section") })[s.id] || s.label;
const ago = localT => { const d = Date.parse(localT || ''); if (!d || d < 0) return t("resources.brain.jamais"); const m = Math.round((Date.now() - d) / 60000); return m < 1 ? t("resources.brain.a_l_instant") : m < 60 ? t('common.relative.minutes', { n: m }) : m < 1440 ? t('common.relative.hours', { n: Math.round(m / 60) }) : t('common.relative.days', { n: Math.round(m / 1440) }); };

function AddSource({ source, preset, onClose }) {
  const [v, setV] = useState(source ? { ...source, permission: ['write', 'ask'].includes(source.permission) ? source.permission : 'read', include: (source.include || []).join('\n'), exclude: (source.exclude || []).join('\n') } : { path: '', remote: '', branch: '', label: '', kind: 'repo', connector: 'git-remote', permission: 'read', primary: false, include: '', exclude: '', ...(preset || {}) });
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
      ${!v.primary && html`<label class="field"><span>${t('second_brain.permission')}</span><${ListPick} label=${t('second_brain.permission')} value=${v.permission} onChange=${permission => setV({ ...v, permission })} options=${[{ value: 'read', label: t('second_brain.read') }, { value: 'ask', label: t('second_brain.ask') }, { value: 'write', label: t('second_brain.write') }]} /></label>`}</div>
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
          <div class="bs-pick"><${ListPick} label=${t('resources.sem.model')} value=${st.model} disabled=${busy || st.downloading || st.indexing} onChange=${model => send({ action: 'enable', model })} options=${(st.models || []).map(m => ({ value: m.id, label: EMBED_NAMES[m.id] || m.id }))} /></div>
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
      <label class="field"><span>${t('resources.sem.provider')}</span><${ListPick} label=${t('resources.sem.provider')} value=${cloud.provider} onChange=${provider => setCloud({ ...cloud, provider })} options=${providers.map(p => ({ value: p.id, label: p.name }))} /></label>
      <label class="field"><span>${t('resources.sem.embed_model')}</span><input class="input mono" placeholder="text-embedding-3-small" value=${cloud.model} onInput=${e => setCloud({ ...cloud, model: e.target.value })} /></label>
      <label class="check"><input type="checkbox" checked=${cloud.consent} onChange=${e => setCloud({ ...cloud, consent: e.target.checked })} /><span>${t('resources.sem.consent')}</span></label>
    </${Modal}>`}
  </section>`;
}

// Une source du Brain : son menu regroupe les actions rares, la ligne garde
// seulement ce qu'il faut voir d'un coup d'œil (rôle, accès, fraîcheur).
function SourceMenu({ s, busy, onEdit, onSync, onSkills, onRemove }) {
  const [anchor, setAnchor] = useState(null);
  const items = [{ icon: 'edit', label: t('second_brain.edit'), run: onEdit }, s.remote && { icon: 'refresh', label: t('second_brain.sync'), run: onSync }, { icon: 'sparkle', label: t('second_brain.link_skills'), run: onSkills }, '-', { icon: 'trash', label: t('resources.brain.retirer_2'), danger: true, run: onRemove }].filter(Boolean);
  return html`<button class="icon-btn" disabled=${busy} aria-label=${t('brain.src.actions')} title=${t('brain.src.actions')} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${Icon} n="more" /></button>
    ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${items} />`}`;
}

export function Brain() {
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
  const sources = (data.sources || []).filter(s => !s.read_only);
  const primary = sources.find(s => s.primary);
  const others = sources.filter(s => !s.primary);
  const freshness = source => {
    if (data.refreshing) return t('brain.refresh.running');
    const last = Date.parse(source.last_checked || source.last_indexed);
    if (!last || last < 1) return t('brain.refresh.pending');
    return Date.now() - last > 360000 ? t('brain.refresh.stale') : ago(source.last_checked || source.last_indexed);
  };
  const connector = s => ({ 'git-remote': t('second_brain.remote'), git: t('second_brain.git'), folder: t('second_brain.folder'), obsidian: t('second_brain.obsidian'), 'webdav-mount': t('second_brain.webdav-mount') })[s.connector] || t('second_brain.folder');
  const access = s => s.primary || s.permission === 'write' ? t('second_brain.write') : s.permission === 'ask' ? t('second_brain.ask') : t('second_brain.read');
  const icon = s => s.connector?.includes('git') ? 'box' : s.kind === 'personal' ? 'file' : 'folder';
  const status = s => s.error ? html`<span class="bs-state err" title=${s.error}><i class="dot red"></i>${t('resources.brain.erreur')}</span>`
    : html`<span class="bs-state"><i class="dot green"></i>${t('brain.src.files', { n: s.files || 0 })} · ${freshness(s)}</span>`;
  const menu = s => html`<${SourceMenu} s=${s} busy=${busy} onEdit=${() => setDlg(s)} onSync=${() => sync(s)} onSkills=${() => setSkillPick(s)} onRemove=${() => remove(s)} />`;
  return html`<div class="bs">
    ${data.error && html`<p class="note err">${data.error}</p>`}
    <section class="sec"><div class="sec-h"><h2>${t('second_brain.primary')}<${Tip} text=${t('second_brain.role.primary_note')} /></h2></div>
      ${primary ? html`<div class="card bs-primary">
          <span class="mono-tile"><${Icon} n=${icon(primary)} /></span>
          <div class="grow"><div class="bs-name">${primary.label}</div>
            <div class="bs-sub"><span>${connector(primary)}</span><span class="mono trunc" title=${primary.remote || primary.path}>${primary.remote || home(primary.path)}</span></div>
            <div class="bs-sub">${status(primary)}<span>${t('brain.src.memory_here')}</span></div></div>
          ${primary.remote && html`<button class="btn sm ghost bs-wide" disabled=${busy} onClick=${() => sync(primary)}><${Icon} n="refresh" />${t('second_brain.sync')}</button>`}
          ${menu(primary)}</div>`
        : html`<div class="card bs-none"><div class="grow"><b>${t('brain.src.no_primary')}</b><p>${t('brain.src.no_primary_note')}</p></div><button class="btn primary" onClick=${() => setDlg({ primary: true })}><${Icon} n="plus" />${t('brain.src.choose_primary')}</button></div>`}
    </section>
    <section class="sec"><div class="sec-h"><h2>${t('brain.src.others')}<${Tip} text=${t('brain.src.others_tip')} /></h2><span class="grow"></span>
        <button class="icon-btn" disabled=${busy} aria-label=${t('resources.brain.reindexer')} title=${t('resources.brain.reindexer')} onClick=${reindex}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`}</button>
        <button class="btn sm" onClick=${() => setDlg(true)}><${Icon} n="plus" />${t('brain.src.add')}</button></div>
      ${others.length ? html`<div class="card bs-list">${others.map(s => html`<div class="bs-row" key=${s.id}>
          <span class="mono-tile"><${Icon} n=${icon(s)} /></span>
          <div class="grow"><div class="bs-name">${s.label}<span class="tag">${t(s.secondary ? 'second_brain.role.secondary' : 'second_brain.role.context')}</span></div>
            <div class="bs-sub"><span>${connector(s)}</span><span>${access(s)}</span>${status(s)}</div></div>
          ${menu(s)}</div>`)}</div>`
        : html`<div class="card bs-none"><div class="grow"><p>${t('brain.src.others_empty')}</p></div></div>`}
    </section>
    ${dlg && html`<${AddSource} source=${dlg === true ? null : dlg.id ? dlg : null} preset=${dlg.primary && !dlg.id ? { primary: true } : null} onClose=${ok => { setDlg(false); if (ok) setTimeout(load, 1500); }} />`}
    ${skillPick && html`<${FolderPicker} start=${skillPick.path} onClose=${() => setSkillPick(null)} onPick=${linkSkills} />`}
  </div>`;
}

// Recherche : ce que les agents trouvent dans le Brain, et l'index par le sens.
export function BrainSearch() {
  const [sources, setSources] = useState(null);
  useEffect(() => { get('/api/brain/sources').then(r => setSources(r.sources || [])).catch(() => setSources([])); }, []);
  return html`<div class="bs">${sources ? html`<${Search} sources=${sources} />` : html`<div class="skeleton" style="height:90px"></div>`}<${Semantic} /></div>`;
}
