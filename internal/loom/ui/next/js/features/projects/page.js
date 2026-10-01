// Projet : contexte commun à ses discussions (instructions, skills, dossier).
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { post } from '../../core/api.js';
import { app, go, refreshNav, refreshWorkspace } from '../../core/state.js';
import { open, newDiscussion } from '../chat/engine.js';

export function ProjectPage({ route }) {
  const { ws, nav } = useStore(app, s => ({ ws: s.workspace, nav: s.nav }));
  const p = ws && (ws.projects || []).find(x => x.id === route.sub);
  const [f, setF] = useState(null);
  useEffect(() => { if (p) setF({ name: p.name, directory: p.directory || '', instructions: p.instructions || '', skills: new Set(p.capability_ids || []) }); }, [p && p.id]);
  if (!ws) return html`<div class="view page"><div class="page-in"><div class="skeleton" style="height:200px"></div></div></div>`;
  if (!p) return html`<div class="view page"><div class="page-in"><${Empty} icon="folder" title="Projet introuvable" /></div></div>`;
  if (!f) return null;
  const chats = nav.conversations.filter(c => c.project_id === p.id);
  const dirty = f.name !== p.name || f.directory !== (p.directory || '') || f.instructions !== (p.instructions || '') || [...f.skills].sort().join() !== [...(p.capability_ids || [])].sort().join();
  const save = async () => {
    const r = await post('/api/projects/context', { id: p.id, name: f.name, directory: f.directory, instructions: f.instructions, capability_ids: [...f.skills] });
    if (!r.ok) return toast(r.error, 'err'); toast('Projet enregistré · appliqué aux prochains messages'); refreshWorkspace(); refreshNav();
  };
  const del = async () => { if (!await confirm('Supprimer le projet', 'Ses discussions ne sont pas effacées, elles reviennent dans Récents.', { ok: 'Supprimer', danger: true })) return; await post('/api/projects/delete', { id: p.id }); refreshWorkspace(); refreshNav(); go('chat'); };
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>${p.name}</h1><p>Le contexte commun à toutes les discussions de ce projet, quel que soit le modèle.</p></div>
      <div class="acts"><button class="btn" onClick=${() => newDiscussion(p.id)}><${Icon} n="plus" />Nouvelle discussion</button></div></div>
    <div class="grid-proj">
      <div class="card pad form-card">
        <label class="field"><span>Nom</span><input class="input" value=${f.name} onInput=${e => setF({ ...f, name: e.target.value })} /></label>
        <label class="field"><span>Instructions du projet</span><textarea class="textarea" rows="8" value=${f.instructions} onInput=${e => setF({ ...f, instructions: e.target.value })} placeholder="Contexte, conventions, objectifs… Ajouté aux prochains messages de chaque discussion du projet."></textarea></label>
        <label class="field"><span>Dossier de travail</span><input class="input mono" value=${f.directory} onInput=${e => setF({ ...f, directory: e.target.value })} placeholder="/chemin/absolu (facultatif)" /><small>Référence pour les futurs harnesses et terminaux. Rien n’est lu automatiquement.</small></label>
        <div class="field"><span>Skills</span>${(ws.capabilities || []).length ? html`<div class="chips">${ws.capabilities.map(c => html`<button type="button" class=${cls('chip-btn', f.skills.has(c.id) && 'on')} onClick=${() => { const s = new Set(f.skills); s.has(c.id) ? s.delete(c.id) : s.add(c.id); setF({ ...f, skills: s }); }}>${f.skills.has(c.id) && html`<${Icon} n="check" />`}${c.name}</button>`)}</div>`
          : html`<small>Aucun skill. <a href="#/resources/skills">Créer un skill</a></small>`}</div>
        <div class="form-foot"><button class="btn danger ghost" onClick=${del}>Supprimer le projet</button><span class="grow"></span><button class="btn primary" disabled=${!dirty} onClick=${save}>Enregistrer</button></div>
      </div>
      <div class="card"><div class="sec-h pad-h"><h2>Discussions <span class="count">${chats.length}</span></h2></div>
        ${chats.length ? html`<div class="rows">${chats.map(c => html`<button class="row link-row" onClick=${() => { open(c.id); go('chat'); }}><span class="grow t">${c.title || 'Discussion'}</span><${Icon} n="right" /></button>`)}</div>`
          : html`<p class="note pad-b">Aucune discussion pour l’instant.</p>`}</div>
    </div>
  </div></div>`;
}
