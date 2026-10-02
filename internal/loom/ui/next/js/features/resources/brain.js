import { t } from '../../core/i18n.js';
// Brain : les sources de contexte de Loom (notes, docs, dépôts, discussions,
// mémoire), indexées sur cette machine. Les projets y puisent à chaque
// message ; les harnesses peuvent l'interroger par MCP.
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tip, Empty } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { FolderPicker } from '../../ui/folder.js';
import { get, post } from '../../core/api.js';
import { copyText } from '../../ui/clipboard.js';

const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');
const KINDS = () => ([['context', t("resources.brain.contexte"), t("resources.brain.notes_et_docs_utilisables_par_defaut")], ['repo', t("resources.brain.depot"), t("resources.brain.le_readme_docs_et_les_fichiers_md_d_un_depot_de_code")], ['personal', t("resources.brain.personnel"), t("resources.brain.jamais_lu_sauf_si_un_projet_ou_une_demande_le_choisit_expliciteme")]]);
// Sources intégrées : noms affichés en français.
export const brainLabel = s => ({ conversations: t("resources.brain.discussions_de_loom"), memory: t("resources.brain.memoire_de_l_agent_local") })[s.id] || s.label;
const kindLabel = k => (KINDS().find(x => x[0] === k) || [k, k])[1];
const ago = localT => { const d = Date.parse(localT || ''); if (!d || d < 0) return t("resources.brain.jamais"); const m = Math.round((Date.now() - d) / 60000); return m < 1 ? t("resources.brain.a_l_instant") : m < 60 ? t('common.relative.minutes', { n: m }) : m < 1440 ? t('common.relative.hours', { n: Math.round(m / 60) }) : t('common.relative.days', { n: Math.round(m / 1440) }); };

function AddSource({ onClose }) {
  const [v, setV] = useState({ path: '', label: '', kind: 'context' });
  const [pick, setPick] = useState(false);
  const save = async () => {
    const r = await post('/api/brain/sources', { action: 'add', path: v.path, label: v.label || v.path.split('/').pop(), kind: v.kind });
    if (r.ok === false || r.error) return toast(r.error || t("resources.brain.ajout_impossible"), 'err');
    toast(t("resources.brain.source_ajoutee_indexation_en_cours")); post('/api/brain/reindex', {}); onClose(true);
  };
  return html`<${Modal} title="${t("resources.brain.ajouter_une_source")}" sub="${t("resources.brain.un_dossier_de_cette_machine_loom_l_indexe_ici_sans_rien_envoyer_a")}" onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>${t("resources.brain.annuler")}</button><button class="btn primary" disabled=${!v.path} onClick=${save}>${t("resources.brain.ajouter")}</button>`}>
    <div class="field"><span>${t("resources.brain.dossier")}</span>${v.path ? html`<button class="hs-dir" onClick=${() => setPick(true)}><${Icon} n="folder" /><span class="mono trunc">${home(v.path)}</span><span class="muted">${t("resources.brain.changer")}</span></button>`
      : html`<button class="btn" onClick=${() => setPick(true)}><${Icon} n="folder" />${t("resources.brain.choisir_un_dossier")}</button>`}</div>
    <label class="field"><span>${t("resources.brain.nom")}</span><input class="input" value=${v.label} placeholder=${v.path.split('/').pop() || t("resources.brain.ex_notes")} onInput=${e => setV({ ...v, label: e.target.value })} /></label>
    <div class="field"><span>${t("resources.brain.type")}</span><div class="brain-kinds">${KINDS().map(([k, l, d]) => html`<label class=${cls('brain-kind', v.kind === k && 'on')} key=${k}><input type="radio" name="kind" checked=${v.kind === k} onChange=${() => setV({ ...v, kind: k })} /><b>${l}</b><small>${d}</small></label>`)}</div></div>
    ${pick && html`<${FolderPicker} start=${v.path} onClose=${() => setPick(false)} onPick=${d => { setPick(false); setV({ ...v, path: d }); }} />`}
  </${Modal}>`;
}

function Search({ sources }) {
  const [q, setQ] = useState('');
  const [hits, setHits] = useState(null);
  const [open, setOpen] = useState(null);
  const run = async e => {
    e && e.preventDefault();
    if (!q.trim()) return;
    const r = await get('/api/brain/search?limit=12&query=' + encodeURIComponent(q)).catch(() => null);
    setHits(r && r.hits ? r.hits : []);
  };
  const read = async h => { const r = await get('/api/brain/read?chunk_id=' + encodeURIComponent(h.chunk_id)).catch(() => null); if (r && r.text) setOpen({ ...h, text: r.text }); };
  const label = id => { const s = sources.find(x => x.id === id); return s ? brainLabel(s) : id; };
  const mark = h => { const out = []; let at = 0; const chars = [...h.snippet]; for (const r of h.highlights || []) { out.push(chars.slice(at, r.start).join('')); out.push(html`<mark>${chars.slice(r.start, r.end).join('')}</mark>`); at = r.end; } out.push(chars.slice(at).join('')); return out; };
  return html`<section class="sec"><div class="sec-h"><h2>${t("resources.brain.chercher_dans_le_brain")}<${Tip} text="${t("resources.brain.les_sources_personnelles_ne_sont_pas_incluses_ici")}" /></h2></div>
    <form class="brain-search" onSubmit=${run}><label class="search"><${Icon} n="search" /><input placeholder="${t("resources.brain.ex_nom_de_la_vm_de_dev_convention_de_commit")}" value=${q} onInput=${e => setQ(e.target.value)} /></label><button class="btn" type="submit">${t("resources.brain.chercher")}</button></form>
    ${hits && (hits.length ? html`<div class="card rows">${hits.map(h => html`<button class="row brain-hit" key=${h.chunk_id} onClick=${() => read(h)}>
        <div class="grow"><div class="t"><span class="mono">${h.path}</span>${h.heading && h.heading.length > 0 && html`<span class="muted"> › ${h.heading.join(' › ')}</span>`}</div><div class="s">${mark(h)}</div></div>
        <span class="tag">${label(h.source)}</span></button>`)}</div>` : html`<p class="note">${t("resources.brain.aucun_passage_trouve")}</p>`)}
    ${open && html`<${Modal} wide title=${open.path} sub=${(open.heading || []).join(' › ')} onClose=${() => setOpen(null)}><pre class="brain-read">${open.text}</pre></${Modal}>`}
  </section>`;
}

export function Brain() {
  const [data, setData] = useState(null);
  const [dlg, setDlg] = useState(false);
  const [busy, setBusy] = useState(false);
  const load = () => get('/api/brain/sources').then(r => setData(r.sources ? r : { sources: [], error: r.error })).catch(() => setData({ sources: [] }));
  useEffect(() => { load(); }, []);
  const reindex = async () => { setBusy(true); const r = await post('/api/brain/reindex', {}); setBusy(false); if (r.ok === false || r.error) return toast(r.error || t("resources.brain.indexation_impossible"), 'err'); toast(t("resources.brain.index_a_jour")); load(); };
  const remove = async s => { if (!await confirm(t("resources.brain.retirer") + s.label, t("resources.brain.la_source_n_est_plus_indexee_le_dossier_n_est_pas_modifie"), { ok: t("resources.brain.retirer_2") })) return; await post('/api/brain/sources', { action: 'remove', id: s.id }); load(); };
  const mcpURL = location.origin + '/mcp/brain';
  const copyCmd = async cmd => toast(await copyText(cmd) ? t("resources.brain.commande_copiee") : t("resources.brain.copie_refusee"));
  if (!data) return html`<div class="skeleton" style="height:200px;margin-top:14px"></div>`;
  const sources = data.sources || [];
  return html`<div>
    <div class="toolbar"><span class="grow muted" style="font-size:13.5px">${t("resources.brain.le_brain_cherche_dans_tes_notes_docs_depots_et_discussions_un_pro")}</span>
      <button class="btn" disabled=${busy} onClick=${reindex}><${Icon} n="refresh" />${busy ? t("resources.brain.indexation") : t("resources.brain.reindexer")}</button><button class="btn primary" onClick=${() => setDlg(true)}><${Icon} n="plus" />${t("resources.brain.ajouter_une_source")}</button></div>
    ${data.error && html`<p class="note err">${data.error}</p>`}
    ${sources.length ? html`<div class="card rows" style="margin-top:14px">${sources.map(s => html`<div class="row" key=${s.id}>
        <span class="mx-ico"><${Icon} n=${s.read_only ? 'chat' : s.kind === 'personal' ? 'lock' : s.kind === 'repo' ? 'box' : 'file'} /></span>
        <div class="grow"><div class="t">${brainLabel(s)} <span class="tag">${s.read_only ? t("resources.brain.integree") : kindLabel(s.kind)}</span></div><div class="s mono">${s.path ? home(s.path) : t("resources.brain.integree_a_loom")}</div></div>
        ${s.error ? html`<span class="tag red" title=${s.error}>${t("resources.brain.erreur")}</span>` : html`<span class="muted">${s.files} ${t("resources.brain.fichier")}${s.files > 1 ? 's' : ''} · ${s.chunks} ${t("resources.brain.passages")} ${ago(s.last_indexed)}</span>`}
        ${!s.read_only && html`<button class="icon-btn" aria-label=${t("resources.brain.retirer") + s.label} onClick=${() => remove(s)}><${Icon} n="close" /></button>`}</div>`)}</div>`
      : html`<div class="card" style="margin-top:14px"><${Empty} icon="brain" title="${t("resources.brain.aucune_source")}" text="${t("resources.brain.ajoute_un_dossier_de_notes_ou_de_docs_tes_projets_pourront_s_en_s")}"><button class="btn primary" onClick=${() => setDlg(true)}>${t("resources.brain.ajouter_une_source")}</button></${Empty}></div>`}
    <${Search} sources=${sources} />
    <section class="sec"><div class="sec-h"><h2>${t("resources.brain.pour_les_harnesses_mcp")}<${Tip} text="${t("resources.brain.les_harnesses_peuvent_interroger_le_brain_eux_memes_meme_sans_pas")}" /></h2></div>
      <div class="card rows">
        <div class="row"><div class="grow"><div class="t">${t("resources.brain.adresse_mcp")}</div><div class="s mono">${mcpURL}</div></div><button class="btn sm ghost" onClick=${() => copyCmd(mcpURL)}>${t("resources.brain.copier")}</button></div>
        <div class="row"><div class="grow"><div class="t">${t("resources.brain.claude_code")}</div><div class="s mono">${t("resources.brain.claude_mcp_add_transport_http_loom_brain")} ${mcpURL}</div></div><button class="btn sm ghost" onClick=${() => copyCmd('claude mcp add --transport http loom-brain ' + mcpURL)}>${t("resources.brain.copier")}</button></div>
        <div class="row"><div class="grow"><div class="t">${t("resources.brain.codex")}</div><div class="s mono">${t("resources.brain.codex_mcp_add_loom_brain_url")} ${mcpURL}</div></div><button class="btn sm ghost" onClick=${() => copyCmd('codex mcp add loom-brain --url ' + mcpURL)}>${t("resources.brain.copier")}</button></div>
      </div></section>
    ${dlg && html`<${AddSource} onClose=${ok => { setDlg(false); if (ok) setTimeout(load, 1500); }} />`}
  </div>`;
}
