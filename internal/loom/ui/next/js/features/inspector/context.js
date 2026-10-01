// Contexte partagé de la discussion : projet, consignes, skills, et aperçu du
// texte préparé pour le modèle (vérification locale, sans appel au modèle).
import { html, useState, useEffect, useStore } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Modal, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, refreshNav, refreshWorkspace } from '../../core/state.js';
import { chat, open } from '../chat/engine.js';

function Preview({ onClose }) {
  const [data, setData] = useState(null);
  const s = chat.get().session;
  useEffect(() => { get('/api/runtime/sessions/preview?id=' + encodeURIComponent(s.id)).then(r => r.ok ? setData(r) : (toast(r.error, 'err'), onClose())); }, []);
  const p = data && data.preview;
  return html`<${Modal} wide title="Texte préparé pour le modèle" sub="Vérification locale · aucun envoi" onClose=${onClose}>
    ${!p ? html`<div class="skeleton" style="height:120px"></div>` : html`
      <p class="note">${p.messages.length} messages · ${p.text_bytes.toLocaleString('fr-FR')} / ${p.max_bytes.toLocaleString('fr-FR')} octets de texte</p>
      ${p.messages.map((m, i) => html`<details class="pv" open=${m.role === 'system'}><summary>${i + 1}. ${{ system: 'Instructions partagées', user: 'Toi', assistant: 'Assistant' }[m.role] || m.role}</summary><pre>${m.content}</pre></details>`)}`}
  </${Modal}>`;
}

function Edit({ onClose }) {
  const s = chat.get().session, ctx = chat.get().context || {};
  const projects = (app.get().workspace && app.get().workspace.projects) || app.get().nav.projects || [];
  const [title, setTitle] = useState(s.title || '');
  const [project, setProject] = useState(s.project_id || '');
  const [instr, setInstr] = useState(s.instructions || '');
  const external = s.runtime_id !== 'llama.cpp';
  const [consent, setConsent] = useState(!external);
  const save = async () => {
    const r = await post('/api/runtime/sessions/configure', { id: s.id, title, project_id: project, instructions: instr, context_revision: ctx.revision || '', consent });
    if (!r.ok) { toast(r.error, 'err'); return; }
    toast('Contexte enregistré · appliqué au prochain message'); onClose(); refreshNav(); open(s.id);
  };
  return html`<${Modal} title="Contexte de la discussion" onClose=${onClose}
      foot=${html`<button class="btn ghost" onClick=${onClose}>Annuler</button><button class="btn primary" disabled=${!consent || !title.trim()} onClick=${save}>Enregistrer</button>`}>
    <label class="field"><span>Titre</span><input class="input" value=${title} onInput=${e => setTitle(e.target.value)} maxlength="100" /></label>
    <label class="field"><span>Projet</span><select class="select" value=${project} onChange=${e => setProject(e.target.value)}>
      <option value="">Aucun projet</option>${projects.map(p => html`<option value=${p.id} selected=${p.id === project}>${p.name}</option>`)}</select>
      <small>Les instructions et skills du projet sont ajoutés aux prochains messages.</small></label>
    <label class="field"><span>Consigne de cette discussion</span><textarea class="textarea" rows="5" value=${instr} onInput=${e => setInstr(e.target.value)} placeholder="Ex. Réponds en français avec des exemples courts."></textarea></label>
    ${external && html`<label class="check"><input type="checkbox" checked=${consent} onChange=${e => setConsent(e.target.checked)} /><span>J’autorise l’envoi de ce contexte à ${s.provider_name} lors des prochains messages.</span></label>`}
  </${Modal}>`;
}

export function ContextPanel() {
  const { session: s, context: c } = useStore(chat, x => ({ session: x.session, context: x.context }));
  const [dlg, setDlg] = useState('');
  if (!s) return null;
  const ctx = c || {}, skills = ctx.skills || [];
  const textual = (s.messages || []).filter(m => m.content).length;
  return html`<div class="insp-body">
    ${ctx.problem && html`<div class="alert red"><${Icon} n="alert" />${ctx.problem}</div>`}
    ${ctx.warning && html`<div class="alert amber"><${Icon} n="info" />${ctx.warning}</div>`}
    <div class="card pad-sm">
      <div class="kv"><span>Projet</span><span>${ctx.project_name || 'Aucun'}</span></div>
      <div class="kv"><span>Historique partagé</span><span>${textual} messages</span></div>
      <div class="kv"><span>Skills</span><span>${skills.length}</span></div>
    </div>
    <div class="ctx-block"><div class="lbl">Instructions du projet</div><p>${ctx.project_instructions || html`<span class="muted">Aucune</span>`}</p></div>
    <div class="ctx-block"><div class="lbl">Consigne de la discussion</div><p>${ctx.discussion_instructions || html`<span class="muted">Aucune</span>`}</p></div>
    ${skills.length > 0 && html`<div class="ctx-block"><div class="lbl">Skills</div><div class="chips">${skills.map(k => html`<span class="tag">${k.name}</span>`)}</div></div>`}
    <div class="btn-row"><button class="btn" onClick=${() => setDlg('edit')}><${Icon} n="edit" />Modifier</button><button class="btn ghost" onClick=${() => setDlg('preview')}><${Icon} n="eye" />Aperçu du texte</button></div>
    <p class="note">Le cache, le raisonnement interne, la mémoire privée et les autorisations de chaque runtime ne sont pas transférés.</p>
    ${dlg === 'edit' && html`<${Edit} onClose=${() => setDlg('')} />`}
    ${dlg === 'preview' && html`<${Preview} onClose=${() => setDlg('')} />`}
  </div>`;
}
