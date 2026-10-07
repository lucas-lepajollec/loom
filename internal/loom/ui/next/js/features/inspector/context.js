import { t, locale } from '../../core/i18n.js';
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
  return html`<${Modal} wide title="${t("inspector.context.texte_prepare_pour_le_modele")}" sub="${t("inspector.context.verification_locale_aucun_envoi")}" onClose=${onClose}>
    ${!p ? html`<div class="skeleton" style="height:120px"></div>` : html`
      <p class="note">${p.messages.length} ${t("inspector.context.messages")} ${p.text_bytes.toLocaleString(locale())} / ${p.max_bytes.toLocaleString(locale())} ${t("inspector.context.octets_de_texte")}</p>
      ${p.messages.map((m, i) => html`<details class="pv" open=${m.role === 'system'}><summary>${i + 1}. ${{ system: t("inspector.context.instructions_partagees"), user: t("inspector.context.toi"), assistant: 'Assistant' }[m.role] || m.role}</summary><pre>${m.content}</pre></details>`)}`}
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
    toast(t("inspector.context.contexte_enregistre_applique_au_prochain_message")); onClose(); refreshNav(); open(s.id);
  };
  return html`<${Modal} title="${t("inspector.context.contexte_de_la_discussion")}" onClose=${onClose}
      foot=${html`<button class="btn ghost" onClick=${onClose}>${t("inspector.context.annuler")}</button><button class="btn primary" disabled=${!consent || !title.trim()} onClick=${save}>${t("inspector.context.enregistrer")}</button>`}>
    <label class="field"><span>${t("inspector.context.titre")}</span><input class="input" value=${title} onInput=${e => setTitle(e.target.value)} maxlength="100" /></label>
    <label class="field"><span>${t("inspector.context.projet")}</span><select class="select" value=${project} onChange=${e => setProject(e.target.value)}>
      <option value="">${t("inspector.context.aucun_projet")}</option>${projects.map(p => html`<option value=${p.id} selected=${p.id === project}>${p.name}</option>`)}</select>
      <small>${t("inspector.context.les_instructions_et_skills_du_projet_sont_ajoutes_aux_prochains_m")}</small></label>
    <label class="field"><span>${t("inspector.context.consigne_de_cette_discussion")}</span><textarea class="textarea" rows="5" value=${instr} onInput=${e => setInstr(e.target.value)} placeholder="${t("inspector.context.ex_reponds_en_francais_avec_des_exemples_courts")}"></textarea></label>
    ${external && html`<label class="check"><input type="checkbox" checked=${consent} onChange=${e => setConsent(e.target.checked)} /><span>${t("inspector.context.j_autorise_l_envoi_de_ce_contexte_a")} ${s.provider_name} ${t("inspector.context.lors_des_prochains_messages")}</span></label>`}
  </${Modal}>`;
}

// Pourquoi chaque élément est dans le contexte : sa nature, la raison et son
// coût en tokens, dans l'ordre du texte préparé pour le modèle.
const KIND = () => ({ global_preferences: [t('why.kind.preferences'), 'sliders'], project: [t('why.kind.project'), 'folder'], project_files: [t('why.kind.project_files'), 'file'], brain_passage: [t('why.kind.brain'), 'brain'], memory: [t('why.kind.memory'), 'brain'], skill: [t('why.kind.skill'), 'sparkle'], primary_brain: [t('why.kind.primary'), 'brain'], secondary_brains: [t('why.kind.secondary'), 'brain'], discussion_instructions: [t('why.kind.discussion'), 'edit'] });
const MEMORY_REASON = () => ({ reflex: t('why.reason.reflex'), working: t('why.reason.working') });
const CLASS = () => ({ reflex: t('memory.class.reflex'), working: t('memory.class.working'), procedural: t('memory.class.procedural'), semantic: t('memory.class.semantic'), episodic: t('memory.class.episodic'), session: t('memory.class.session') });

function WhyInContext({ ctx }) {
  const items = ctx.items || [];
  if (!items.length) return null;
  const mem = ctx.budget && ctx.budget.memory;
  const pct = mem && mem.available ? Math.min(100, Math.round(mem.used * 100 / mem.available)) : 0;
  return html`<div class="ctx-block why"><div class="lbl">${t('why.title')}<span class="muted">~${(ctx.estimated_tokens || 0).toLocaleString(locale())} tok</span></div>
    ${mem && html`<div class="why-budget"><span>${t('why.memory_budget')}</span><b>${mem.used} / ${mem.available}</b><div class="gauge"><i style=${`width:${pct}%`}></i></div></div>`}
    <div class="why-list">${items.map((it, i) => { const [kind, ico] = KIND()[it.kind] || [it.kind, 'info'];
      const reason = it.kind === 'memory' ? (MEMORY_REASON()[it.class] || t('why.reason.matches')) : it.kind === 'brain_passage' ? t('why.reason.passage') : '';
      return html`<div class="why-item" key=${i} title=${it.reason || ''}><${Icon} n=${ico} /><div class="grow"><b class="trunc">${it.label || kind}</b>
        <small>${[it.kind === 'memory' ? CLASS()[it.class] || it.class : kind, reason].filter(Boolean).join(' · ')}</small></div><span class="mono muted">${it.tokens || 0}</span></div>`; })}</div>
  </div>`;
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
      <div class="kv"><span>${t("inspector.context.projet")}</span><span>${ctx.project_name || t("inspector.context.aucun")}</span></div>
      <div class="kv"><span>${t("inspector.context.historique_partage")}</span><span>${textual} ${t("inspector.context.messages_2")}</span></div>
      <div class="kv"><span>${t("inspector.context.skills")}</span><span>${skills.length}</span></div>
    </div>
    ${(ctx.items || []).length ? html`<${WhyInContext} ctx=${ctx} />` : html`<div class="kv"><span>${t('brain.context.budget')}</span><span>~${ctx.estimated_tokens || 0}</span></div>`}
    ${ctx.global_preferences && html`<details class="pv"><summary>${t('brain.preferences.title')}</summary><pre>${ctx.global_preferences}</pre></details>`}
    ${ctx.minimum && html`<details class="pv"><summary>${t('brain.context.minimum')}</summary><pre>${ctx.minimum}</pre></details>`}
    ${(ctx.reference_ids || []).length > 0 && html`<div class="ctx-block"><div class="lbl">${t('brain.context.references')}</div>${ctx.reference_ids.map(id => html`<button class="btn sm ghost" onClick=${() => open(id)}>${id}</button>`)}</div>`}
    ${(ctx.brain_citations || []).length > 0 && html`<details class="pv"><summary>Brain</summary><pre>${ctx.brain_citations.join('\n')}</pre></details>`}
    <div class="ctx-block"><div class="lbl">${t("inspector.context.instructions_du_projet")}</div><p>${ctx.project_instructions || html`<span class="muted">${t("inspector.context.aucune")}</span>`}</p></div>
    <div class="ctx-block"><div class="lbl">${t("inspector.context.consigne_de_la_discussion")}</div><p>${ctx.discussion_instructions || html`<span class="muted">${t("inspector.context.aucune")}</span>`}</p></div>
    ${skills.length > 0 && html`<div class="ctx-block"><div class="lbl">${t("inspector.context.skills")}</div><div class="chips">${skills.map(k => html`<span class="tag">${k.name}</span>`)}</div></div>`}
    <div class="btn-row"><button class="btn" onClick=${() => setDlg('edit')}><${Icon} n="edit" />${t("inspector.context.modifier")}</button><button class="btn ghost" onClick=${() => setDlg('preview')}><${Icon} n="eye" />${t("inspector.context.apercu_du_texte")}</button></div>
    <p class="note">${t("inspector.context.le_cache_le_raisonnement_interne_la_memoire_privee_et_les_autoris")}</p>
    ${dlg === 'edit' && html`<${Edit} onClose=${() => setDlg('')} />`}
    ${dlg === 'preview' && html`<${Preview} onClose=${() => setDlg('')} />`}
  </div>`;
}
