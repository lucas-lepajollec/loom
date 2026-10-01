// Projet : ce qui relie ses discussions. Son dossier (et le dépôt Git qui s'y
// trouve), le contexte commun (instructions, fichiers choisis du dossier,
// skills), le dossier de travail par défaut des harnesses et l'exécution avec
// laquelle une nouvelle discussion démarre.
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Tip } from '../../ui/controls.js';
import { FolderPicker } from '../../ui/folder.js';
import { confirm, prompt, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshNav, refreshWorkspace } from '../../core/state.js';
import { open, newDiscussion, chooseRemote } from '../chat/engine.js';
import { openTerminalWith } from '../terminals/page.js';

const kb = b => b < 1024 ? b + ' o' : (b / 1024).toFixed(b < 10240 ? 1 : 0) + ' Ko';
const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');
const ago = t => {
  if (!t) return '';
  const m = Math.round((Date.now() - t) / 60000);
  return m < 60 ? 'il y a ' + Math.max(1, m) + ' min' : m < 1440 ? 'il y a ' + Math.round(m / 60) + ' h' : 'il y a ' + Math.round(m / 1440) + ' j';
};

// Nouvelle discussion du projet, sur son exécution par défaut si elle existe.
export async function newProjectDiscussion(p) {
  await newDiscussion(p.id);
  const ws = app.get().workspace;
  const choice = p.default_choice && ((ws && ws.models) || []).find(m => m.id === p.default_choice && m.enabled);
  if (choice) await chooseRemote(choice);
}

function Repo({ info }) {
  if (!info) return html`<p class="note">Lecture du dossier…</p>`;
  if (info.missing) return html`<p class="note err">Ce dossier n’existe plus ou n’est pas accessible.</p>`;
  const r = info.repo;
  if (!r) return html`<p class="note">Pas de dépôt Git dans ce dossier.</p>`;
  return html`<div class="pj-repo">
    <div><span>Branche <b>${r.branch || 'détachée'}</b></span><span class="muted">${r.changed ? r.changed + ' fichier' + (r.changed > 1 ? 's' : '') + ' modifié' + (r.changed > 1 ? 's' : '') : 'à jour'}</span></div>
    ${r.remote && html`<div class="mono trunc muted" title=${r.remote}>${r.remote.replace(/^(https?|ssh):\/\//, '')}</div>`}
    ${r.last && html`<div class="trunc muted" title=${r.last}>Dernier commit ${ago(r.last_at)} · ${r.last}</div>`}
  </div>`;
}

export function ProjectPage({ route }) {
  const { ws, nav } = useStore(app, s => ({ ws: s.workspace, nav: s.nav }));
  const p = ws && (ws.projects || []).find(x => x.id === route.sub);
  const [f, setF] = useState(null);
  const [info, setInfo] = useState(null);
  const [pick, setPick] = useState(false);
  const [machines, setMachines] = useState([]);
  useEffect(() => { get('/api/machines').then(r => setMachines(r.ok ? r.machines : [])).catch(() => {}); }, []);
  const reset = () => p && setF({ name: p.name, directory: p.directory || '', machine: p.machine || '', extra: p.extra_dirs || [], instructions: p.instructions || '', skills: new Set(p.capability_ids || []), files: new Set(p.context_files || []), def: p.default_choice || '' });
  useEffect(reset, [p && p.id]);
  const loadInfo = () => p && get('/api/projects/info?id=' + encodeURIComponent(p.id)).then(setInfo, () => setInfo({}));
  useEffect(() => { setInfo(null); loadInfo(); }, [p && p.id, p && p.directory]);
  if (!ws) return html`<div class="view page"><div class="page-in"><div class="skeleton" style="height:200px"></div></div></div>`;
  if (!p) return html`<div class="view page"><div class="page-in"><${Empty} icon="folder" title="Projet introuvable" /></div></div>`;
  if (!f) return null;
  const chats = nav.conversations.filter(c => c.project_id === p.id);
  const same = (a, b) => [...a].sort().join('\n') === [...b].sort().join('\n');
  const dirty = f.name !== p.name || f.directory !== (p.directory || '') || f.machine !== (p.machine || '') || f.extra.join('\n') !== (p.extra_dirs || []).join('\n') || f.instructions !== (p.instructions || '') || !same(f.skills, p.capability_ids || []) || !same(f.files, p.context_files || []) || f.def !== (p.default_choice || '');
  const save = async () => {
    const r = await post('/api/projects/context', { id: p.id, name: f.name, directory: f.directory, machine: f.machine, extra_dirs: f.extra, instructions: f.instructions, capability_ids: [...f.skills], context_files: !f.machine && f.directory === (p.directory || '') ? [...f.files] : [], default_choice: f.def });
    if (!r.ok) return toast(r.error, 'err');
    toast('Projet enregistré · appliqué aux prochains messages'); await refreshWorkspace(); refreshNav();
  };
  const del = async () => { if (!await confirm('Supprimer le projet', 'Ses discussions ne sont pas effacées, elles reviennent dans Récents. Rien n’est supprimé dans son dossier.', { ok: 'Supprimer', danger: true })) return; await post('/api/projects/delete', { id: p.id }); refreshWorkspace(); refreshNav(); go('chat'); };
  const toggle = (key, id) => { const s = new Set(f[key]); s.has(id) ? s.delete(id) : s.add(id); setF({ ...f, [key]: s }); };
  const candidates = f.directory === (p.directory || '') ? (info && info.candidates) || [] : [];
  // Fichiers choisis absents de la liste proposée (ajoutés autrement) : gardés visibles.
  const extra = [...f.files].filter(x => !candidates.some(c => c.path === x)).map(path => ({ path, size: 0, missing: true }));
  const filesBytes = [...candidates, ...extra].filter(c => f.files.has(c.path)).reduce((n, c) => n + c.size, 0);
  const choices = ((ws.models || []).filter(m => m.enabled && (m.kind === 'cloud' || m.kind === 'harness')));
  const groups = [...new Set(choices.map(m => m.kind === 'harness' ? 'Harness · ' + m.provider_name : 'Cloud · ' + m.provider_name))];
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>${p.name}</h1><p>Ce qui relie les discussions de ce projet : son dossier, son contexte et l’exécution par défaut.</p></div>
      <div class="acts">${p.directory && html`<button class="btn" onClick=${() => openTerminalWith({ target: p.machine || 'local', dir: p.directory, title: p.name })}><${Icon} n="prompt" />Terminal</button>`}<button class="btn primary" onClick=${() => newProjectDiscussion(p)}><${Icon} n="plus" />Nouvelle discussion</button></div></div>

    <div class="grid-proj">
      <div class="pj-col">
        <section class="card pad pj-sec">
          <div class="sec-h"><h2>Dossier du projet<${Tip} text="Les harnesses de la machine du projet (Codex, Claude Code, Hermes…) y travaillent par défaut, avec les dossiers supplémentaires. Loom ne lit que les fichiers que tu choisis comme contexte." /></h2></div>
          ${machines.length > 0 && html`<div class="chips"><button type="button" class=${cls('chip-btn', !f.machine && 'on')} onClick=${() => setF({ ...f, machine: '', directory: '', extra: [], files: new Set() })}><${Icon} n="chip" />Cette machine</button>
            ${machines.map(m => html`<button type="button" class=${cls('chip-btn', f.machine === m.id && 'on')} onClick=${() => setF({ ...f, machine: m.id, directory: '', extra: [], files: new Set() })}><${Icon} n="server" />${m.name}</button>`)}</div>`}
          ${f.machine ? html`<input class="input mono" placeholder=${'Dossier sur ' + ((machines.find(m => m.id === f.machine) || {}).name || 'la machine') + ' (ex. /home/moi/projet)'} value=${f.directory} onInput=${e => setF({ ...f, directory: e.target.value })} />`
            : f.directory ? html`<button class="hs-dir" onClick=${() => setPick('main')} title=${f.directory}><${Icon} n="folder" /><span class="mono trunc">${home(f.directory)}</span><span class="muted">Changer</span></button>`
            : html`<button class="btn" onClick=${() => setPick('main')}><${Icon} n="folder" />Choisir le dossier du projet</button>`}
          ${!f.machine && f.directory && f.directory === (p.directory || '') && html`<${Repo} info=${info} />`}
          ${f.directory && html`<div class="pj-extra"><span class="muted">Dossiers supplémentaires<${Tip} text="D’autres dossiers de la même machine auxquels les harnesses du projet ont aussi accès (une bibliothèque partagée, la doc…)." /></span>
            ${f.extra.map(d => html`<div class="pj-extra-row" key=${d}><${Icon} n="folder" /><span class="mono trunc">${home(d)}</span><button class="icon-btn" aria-label="Retirer" onClick=${() => setF({ ...f, extra: f.extra.filter(x => x !== d) })}><${Icon} n="close" /></button></div>`)}
            <button class="btn sm ghost" onClick=${async () => { if (!f.machine) return setPick('extra'); const d = await prompt('Dossier supplémentaire', { placeholder: '/home/moi/autre', ok: 'Ajouter' }); if (d) setF({ ...f, extra: [...f.extra, d.trim()] }); }}><${Icon} n="plus" />Ajouter un dossier</button></div>`}
          ${!f.machine && f.directory && f.directory !== (p.directory || '') && html`<p class="note">Enregistre pour lire ce dossier.</p>`}
        </section>

        <section class="card pad pj-sec">
          <div class="sec-h"><h2>Contexte du projet<${Tip} text="Ajouté aux messages de chaque discussion du projet, quel que soit le modèle. Les fichiers sont relus à chaque message : ils restent à jour." /></h2></div>
          <label class="field"><span>Instructions</span><textarea class="textarea" rows="6" value=${f.instructions} onInput=${e => setF({ ...f, instructions: e.target.value })} placeholder="Objectifs, conventions, ce qu’il faut savoir…"></textarea></label>
          <div class="field"><span>Fichiers du dossier${filesBytes > 0 && html` <small class="muted">· ${kb(filesBytes)} / 48 Ko</small>`}</span>
            ${f.machine ? html`<small>Lus seulement quand le dossier du projet est sur cette machine.</small>`
              : !f.directory ? html`<small>Choisis d’abord le dossier du projet.</small>`
              : candidates.length + extra.length === 0 ? html`<small>${info ? 'Aucun AGENTS.md, README.md ni fichier docs/*.md dans ce dossier.' : 'Lecture…'}</small>`
              : html`<div class="pj-files">${[...candidates, ...extra].map(c => html`<label class=${cls('pj-file', c.missing && 'off')} key=${c.path}>
                  <input type="checkbox" checked=${f.files.has(c.path)} onChange=${() => toggle('files', c.path)} /><span class="mono trunc">${c.path}</span><span class="muted">${c.missing ? 'introuvable' : kb(c.size)}</span></label>`)}</div>`}
            ${filesBytes > 48 * 1024 && html`<small class="note warn">Au-delà de 48 Ko, les derniers fichiers ne sont pas envoyés.</small>`}</div>
          <div class="field"><span>Skills</span>${(ws.capabilities || []).length ? html`<div class="chips">${ws.capabilities.map(c => html`<button type="button" class=${cls('chip-btn', f.skills.has(c.id) && 'on')} onClick=${() => toggle('skills', c.id)}>${f.skills.has(c.id) && html`<${Icon} n="check" />`}${c.name}</button>`)}</div>`
            : html`<small>Aucun skill. <a href="#/resources/skills">Créer un skill</a></small>`}</div>
        </section>

        <section class="card pad pj-sec">
          <div class="sec-h"><h2>Exécution par défaut<${Tip} text="Le modèle ou le harness avec lequel une nouvelle discussion du projet démarre. Tu peux toujours en changer dans la discussion." /></h2></div>
          <select class="select" value=${f.def} onChange=${e => setF({ ...f, def: e.target.value })}>
            <option value="">Modèle local chargé</option>
            ${groups.map(g => html`<optgroup label=${g}>${choices.filter(m => (m.kind === 'harness' ? 'Harness · ' : 'Cloud · ') + m.provider_name === g).map(m => html`<option value=${m.id} selected=${m.id === f.def}>${m.name === m.provider_name ? m.name : m.provider_name + ' · ' + m.name}</option>`)}</optgroup>`)}
          </select>
          ${f.def && !choices.some(m => m.id === f.def) && html`<p class="note warn">Ce choix n’est plus disponible dans le sélecteur.</p>`}
        </section>

        <div class="form-foot"><button class="btn danger ghost" onClick=${del}>Supprimer le projet</button><span class="grow"></span>
          ${dirty && html`<button class="btn ghost" onClick=${reset}>Annuler</button>`}<button class="btn primary" disabled=${!dirty} onClick=${save}>Enregistrer</button></div>
      </div>

      <div class="card"><div class="sec-h pad-h"><h2>Discussions <span class="count">${chats.length}</span></h2></div>
        ${chats.length ? html`<div class="rows">${chats.map(c => html`<button class="row link-row" onClick=${() => { open(c.id); go('chat'); }}><span class="grow t">${c.title || 'Discussion'}</span><${Icon} n="right" /></button>`)}</div>`
          : html`<p class="note pad-b">Aucune discussion pour l’instant.</p>`}</div>
    </div>
    ${pick && html`<${FolderPicker} start=${f.directory} onClose=${() => setPick(false)} onPick=${d => { setPick(false); setF(pick === 'extra' ? { ...f, extra: [...f.extra, d] } : { ...f, directory: d, files: new Set() }); }} />`}
  </div></div>`;
}
