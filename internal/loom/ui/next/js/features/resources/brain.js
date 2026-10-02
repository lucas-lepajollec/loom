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
const KINDS = [['context', 'Contexte', 'Notes et docs utilisables par défaut.'], ['repo', 'Dépôt', 'Le README, docs/ et les fichiers .md d’un dépôt de code.'], ['personal', 'Personnel', 'Jamais lu sauf si un projet ou une demande le choisit explicitement.']];
// Sources intégrées : noms affichés en français.
export const brainLabel = s => ({ conversations: 'Discussions de Loom', memory: 'Mémoire de l’agent local' })[s.id] || s.label;
const kindLabel = k => (KINDS.find(x => x[0] === k) || [k, k])[1];
const ago = t => { const d = Date.parse(t || ''); if (!d || d < 0) return 'jamais'; const m = Math.round((Date.now() - d) / 60000); return m < 1 ? 'à l’instant' : m < 60 ? 'il y a ' + m + ' min' : m < 1440 ? 'il y a ' + Math.round(m / 60) + ' h' : 'il y a ' + Math.round(m / 1440) + ' j'; };

function AddSource({ onClose }) {
  const [v, setV] = useState({ path: '', label: '', kind: 'context' });
  const [pick, setPick] = useState(false);
  const save = async () => {
    const r = await post('/api/brain/sources', { action: 'add', path: v.path, label: v.label || v.path.split('/').pop(), kind: v.kind });
    if (r.ok === false || r.error) return toast(r.error || 'Ajout impossible', 'err');
    toast('Source ajoutée · indexation en cours'); post('/api/brain/reindex', {}); onClose(true);
  };
  return html`<${Modal} title="Ajouter une source" sub="Un dossier de cette machine. Loom l’indexe ici, sans rien envoyer ailleurs." onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>Annuler</button><button class="btn primary" disabled=${!v.path} onClick=${save}>Ajouter</button>`}>
    <div class="field"><span>Dossier</span>${v.path ? html`<button class="hs-dir" onClick=${() => setPick(true)}><${Icon} n="folder" /><span class="mono trunc">${home(v.path)}</span><span class="muted">Changer</span></button>`
      : html`<button class="btn" onClick=${() => setPick(true)}><${Icon} n="folder" />Choisir un dossier</button>`}</div>
    <label class="field"><span>Nom</span><input class="input" value=${v.label} placeholder=${v.path.split('/').pop() || 'ex. Notes'} onInput=${e => setV({ ...v, label: e.target.value })} /></label>
    <div class="field"><span>Type</span><div class="brain-kinds">${KINDS.map(([k, l, d]) => html`<label class=${cls('brain-kind', v.kind === k && 'on')} key=${k}><input type="radio" name="kind" checked=${v.kind === k} onChange=${() => setV({ ...v, kind: k })} /><b>${l}</b><small>${d}</small></label>`)}</div></div>
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
  return html`<section class="sec"><div class="sec-h"><h2>Chercher dans le Brain<${Tip} text="Les sources personnelles ne sont pas incluses ici." /></h2></div>
    <form class="brain-search" onSubmit=${run}><label class="search"><${Icon} n="search" /><input placeholder="ex. nom de la VM de dev, convention de commit…" value=${q} onInput=${e => setQ(e.target.value)} /></label><button class="btn" type="submit">Chercher</button></form>
    ${hits && (hits.length ? html`<div class="card rows">${hits.map(h => html`<button class="row brain-hit" key=${h.chunk_id} onClick=${() => read(h)}>
        <div class="grow"><div class="t"><span class="mono">${h.path}</span>${h.heading && h.heading.length > 0 && html`<span class="muted"> › ${h.heading.join(' › ')}</span>`}</div><div class="s">${mark(h)}</div></div>
        <span class="tag">${label(h.source)}</span></button>`)}</div>` : html`<p class="note">Aucun passage trouvé.</p>`)}
    ${open && html`<${Modal} wide title=${open.path} sub=${(open.heading || []).join(' › ')} onClose=${() => setOpen(null)}><pre class="brain-read">${open.text}</pre></${Modal}>`}
  </section>`;
}

export function Brain() {
  const [data, setData] = useState(null);
  const [dlg, setDlg] = useState(false);
  const [busy, setBusy] = useState(false);
  const load = () => get('/api/brain/sources').then(r => setData(r.sources ? r : { sources: [], error: r.error })).catch(() => setData({ sources: [] }));
  useEffect(() => { load(); }, []);
  const reindex = async () => { setBusy(true); const r = await post('/api/brain/reindex', {}); setBusy(false); if (r.ok === false || r.error) return toast(r.error || 'Indexation impossible', 'err'); toast('Index à jour'); load(); };
  const remove = async s => { if (!await confirm('Retirer ' + s.label, 'La source n’est plus indexée. Le dossier n’est pas modifié.', { ok: 'Retirer' })) return; await post('/api/brain/sources', { action: 'remove', id: s.id }); load(); };
  const mcpURL = location.origin + '/mcp/brain';
  const copyCmd = async cmd => toast(await copyText(cmd) ? 'Commande copiée' : 'Copie refusée');
  if (!data) return html`<div class="skeleton" style="height:200px;margin-top:14px"></div>`;
  const sources = data.sources || [];
  return html`<div>
    <div class="toolbar"><span class="grow muted" style="font-size:13.5px">Le Brain cherche dans tes notes, docs, dépôts et discussions. Un projet y puise à chaque message, dans un budget de tokens, avec la source de chaque passage.</span>
      <button class="btn" disabled=${busy} onClick=${reindex}><${Icon} n="refresh" />${busy ? 'Indexation…' : 'Réindexer'}</button><button class="btn primary" onClick=${() => setDlg(true)}><${Icon} n="plus" />Ajouter une source</button></div>
    ${data.error && html`<p class="note err">${data.error}</p>`}
    ${sources.length ? html`<div class="card rows" style="margin-top:14px">${sources.map(s => html`<div class="row" key=${s.id}>
        <span class="mx-ico"><${Icon} n=${s.read_only ? 'chat' : s.kind === 'personal' ? 'lock' : s.kind === 'repo' ? 'box' : 'file'} /></span>
        <div class="grow"><div class="t">${brainLabel(s)} <span class="tag">${s.read_only ? 'intégrée' : kindLabel(s.kind)}</span></div><div class="s mono">${s.path ? home(s.path) : 'intégrée à Loom'}</div></div>
        ${s.error ? html`<span class="tag red" title=${s.error}>erreur</span>` : html`<span class="muted">${s.files} fichier${s.files > 1 ? 's' : ''} · ${s.chunks} passages · ${ago(s.last_indexed)}</span>`}
        ${!s.read_only && html`<button class="icon-btn" aria-label=${'Retirer ' + s.label} onClick=${() => remove(s)}><${Icon} n="close" /></button>`}</div>`)}</div>`
      : html`<div class="card" style="margin-top:14px"><${Empty} icon="brain" title="Aucune source" text="Ajoute un dossier de notes ou de docs : tes projets pourront s’en servir comme contexte."><button class="btn primary" onClick=${() => setDlg(true)}>Ajouter une source</button></${Empty}></div>`}
    <${Search} sources=${sources} />
    <section class="sec"><div class="sec-h"><h2>Pour les harnesses (MCP)<${Tip} text="Les harnesses peuvent interroger le Brain eux-mêmes, même sans passer par une discussion Loom : outils brain_search, brain_pack et brain_read, en lecture seule. Si Loom a une clé de pilotage, ajoute-la en en-tête Authorization." /></h2></div>
      <div class="card rows">
        <div class="row"><div class="grow"><div class="t">Adresse MCP</div><div class="s mono">${mcpURL}</div></div><button class="btn sm ghost" onClick=${() => copyCmd(mcpURL)}>Copier</button></div>
        <div class="row"><div class="grow"><div class="t">Claude Code</div><div class="s mono">claude mcp add --transport http loom-brain ${mcpURL}</div></div><button class="btn sm ghost" onClick=${() => copyCmd('claude mcp add --transport http loom-brain ' + mcpURL)}>Copier</button></div>
        <div class="row"><div class="grow"><div class="t">Codex</div><div class="s mono">codex mcp add loom-brain --url ${mcpURL}</div></div><button class="btn sm ghost" onClick=${() => copyCmd('codex mcp add loom-brain --url ' + mcpURL)}>Copier</button></div>
      </div></section>
    ${dlg && html`<${AddSource} onClose=${ok => { setDlg(false); if (ok) setTimeout(load, 1500); }} />`}
  </div>`;
}
