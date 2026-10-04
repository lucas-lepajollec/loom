import { t } from '../../core/i18n.js';
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
import { brainLabel } from '../resources/brain.js';

const kb = b => b < 1024 ? b + t("projects.page.o") : (b / 1024).toFixed(b < 10240 ? 1 : 0) + t("projects.page.ko");
const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');
const ago = localT => {
  if (!localT) return '';
  const m = Math.round((Date.now() - localT) / 60000);
  return m < 60 ? t("harnesses.page.il_y_a") + Math.max(1, m) + ' min' : m < 1440 ? t("harnesses.page.il_y_a") + Math.round(m / 60) + ' h' : t("harnesses.page.il_y_a") + Math.round(m / 1440) + t("environment.page.j");
};

// Nouvelle discussion du projet, sur son exécution par défaut si elle existe.
export async function newProjectDiscussion(p) {
  await newDiscussion(p.id);
  const ws = app.get().workspace;
  const choice = p.default_choice && ((ws && ws.models) || []).find(m => m.id === p.default_choice && m.enabled);
  if (choice) await chooseRemote(choice);
}

function Repo({ info }) {
  if (!info) return html`<p class="note">${t("projects.page.lecture_du_dossier")}</p>`;
  if (info.missing) return html`<p class="note err">${t("projects.page.ce_dossier_n_existe_plus_ou_n_est_pas_accessible")}</p>`;
  const r = info.repo;
  if (!r) return html`<p class="note">${t("projects.page.pas_de_depot_git_dans_ce_dossier")}</p>`;
  return html`<div class="pj-repo">
    <div><span>${t("projects.page.branche")} <b>${r.branch || t("projects.page.detachee")}</b></span><span class="muted">${r.changed ? r.changed + t("projects.page.fichier") + (r.changed > 1 ? 's' : '') + t("projects.page.modifie") + (r.changed > 1 ? 's' : '') : t("projects.page.a_jour")}</span></div>
    ${r.remote && html`<div class="mono trunc muted" title=${r.remote}>${r.remote.replace(/^(https?|ssh):\/\//, '')}</div>`}
    ${r.last && html`<div class="trunc muted" title=${r.last}>${t("projects.page.dernier_commit")} ${ago(r.last_at)} · ${r.last}</div>`}
  </div>`;
}

const projectForm = p => ({ name: p.name, directory: p.directory || '', machine: p.machine || '', extra: p.extra_dirs || [], brain: new Set(p.brain_sources || []), budget: p.brain_budget || 0,
  continuity: { ...(p.continuity || {}), core: { purpose: '', rationale: '', constraints: '', decisions: '', ...(p.continuity?.core || {}) }, working_state: p.continuity?.working_state || '', references: p.continuity?.references || [] },
  instructions: p.instructions || '', skills: new Set(p.capability_ids || []), files: new Set(p.context_files || []), def: p.default_choice || '' });

export function ProjectPage({ route }) {
  const { ws, nav } = useStore(app, s => ({ ws: s.workspace, nav: s.nav }));
  const p = ws && (ws.projects || []).find(x => x.id === route.sub);
  const [f, setF] = useState(null);
  const [info, setInfo] = useState(null);
  const [pick, setPick] = useState(false);
  const [machines, setMachines] = useState([]);
  const [brainSources, setBrainSources] = useState(null);
  useEffect(() => { get('/api/brain/sources').then(r => setBrainSources(r.sources || [])).catch(() => setBrainSources([])); }, []);
  useEffect(() => { get('/api/machines').then(r => setMachines(r.ok ? r.machines : [])).catch(() => {}); }, []);
  const reset = () => p && setF(projectForm(p));
  useEffect(reset, [p && p.id]);
  const loadInfo = () => p && get('/api/projects/info?id=' + encodeURIComponent(p.id)).then(setInfo, () => setInfo({}));
  useEffect(() => { setInfo(null); loadInfo(); }, [p && p.id, p && p.directory]);
  if (!ws) return html`<div class="view page"><div class="page-in"><div class="skeleton" style="height:200px"></div></div></div>`;
  if (!p) return html`<div class="view page"><div class="page-in"><${Empty} icon="folder" title="${t("projects.page.projet_introuvable")}" /></div></div>`;
  if (!f) return null;
  const chats = nav.conversations.filter(c => c.project_id === p.id);
  const same = (a, b) => [...a].sort().join('\n') === [...b].sort().join('\n');
  const dirty = JSON.stringify(f.continuity) !== JSON.stringify(projectForm(p).continuity) || f.name !== p.name || f.directory !== (p.directory || '') || f.machine !== (p.machine || '') || f.extra.join('\n') !== (p.extra_dirs || []).join('\n') || !same(f.brain, p.brain_sources || []) || f.budget !== (p.brain_budget || 0) || f.instructions !== (p.instructions || '') || !same(f.skills, p.capability_ids || []) || !same(f.files, p.context_files || []) || f.def !== (p.default_choice || '');
  const save = async () => {
    const r = await post('/api/projects/context', { id: p.id, name: f.name, directory: f.directory, machine: f.machine, extra_dirs: f.extra, continuity: f.continuity, brain_sources: [...f.brain], brain_budget: f.budget, instructions: f.instructions, capability_ids: [...f.skills], context_files: !f.machine && f.directory === (p.directory || '') ? [...f.files] : [], default_choice: f.def });
    if (!r.ok) return toast(r.error, 'err');
    toast(t("projects.page.projet_enregistre_applique_aux_prochains_messages"));
    const updated = await refreshWorkspace();
    const saved = updated?.projects?.find(x => x.id === p.id);
    if (saved) setF(projectForm(saved));
    refreshNav();
  };
  const del = async () => { if (!await confirm(t("projects.page.supprimer_le_projet"), t("projects.page.ses_discussions_ne_sont_pas_effacees_elles_reviennent_dans_recent"), { ok: t("projects.page.supprimer"), danger: true })) return; await post('/api/projects/delete', { id: p.id }); refreshWorkspace(); refreshNav(); go('chat'); };
  const toggle = (key, id) => { const s = new Set(f[key]); s.has(id) ? s.delete(id) : s.add(id); setF({ ...f, [key]: s }); };
  const candidates = f.directory === (p.directory || '') ? (info && info.candidates) || [] : [];
  // Fichiers choisis absents de la liste proposée (ajoutés autrement) : gardés visibles.
  const extra = [...f.files].filter(x => !candidates.some(c => c.path === x)).map(path => ({ path, size: 0, missing: true }));
  const filesBytes = [...candidates, ...extra].filter(c => f.files.has(c.path)).reduce((n, c) => n + c.size, 0);
  const choices = ((ws.models || []).filter(m => m.enabled && (m.kind === 'cloud' || m.kind === 'harness')));
  const groups = [...new Set(choices.map(m => m.kind === 'harness' ? 'Harness · ' + m.provider_name : 'Cloud · ' + m.provider_name))];
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>${p.name}</h1><p>${t("projects.page.ce_qui_relie_les_discussions_de_ce_projet_son_dossier_son_context")}</p></div>
      <div class="acts">${p.directory && html`<button class="btn" onClick=${() => openTerminalWith({ target: p.machine || 'local', dir: p.directory, title: p.name })}><${Icon} n="prompt" />${t("projects.page.terminal")}</button>`}<button class="btn primary" onClick=${() => newProjectDiscussion(p)}><${Icon} n="plus" />${t("projects.page.nouvelle_discussion")}</button></div></div>

    <div class="grid-proj">
      <div class="pj-col">
        <section class="card pad pj-sec">
          <div class="sec-h"><h2>${t("projects.page.dossier_du_projet")}<${Tip} text="${t("projects.page.les_harnesses_de_la_machine_du_projet_codex_claude_code_hermes_y")}" /></h2></div>
          ${machines.length > 0 && html`<div class="chips"><button type="button" class=${cls('chip-btn', !f.machine && 'on')} onClick=${() => setF({ ...f, machine: '', directory: '', extra: [], files: new Set() })}><${Icon} n="chip" />${t("projects.page.cette_machine")}</button>
            ${machines.map(m => html`<button type="button" class=${cls('chip-btn', f.machine === m.id && 'on')} onClick=${() => setF({ ...f, machine: m.id, directory: '', extra: [], files: new Set() })}><${Icon} n="server" />${m.name}</button>`)}</div>`}
          ${f.machine ? html`<input class="input mono" placeholder=${t("projects.page.dossier_sur") + ((machines.find(m => m.id === f.machine) || {}).name || t("projects.page.la_machine")) + t("projects.page.ex_home_moi_projet")} value=${f.directory} onInput=${e => setF({ ...f, directory: e.target.value })} />`
            : f.directory ? html`<button class="hs-dir" onClick=${() => setPick('main')} title=${f.directory}><${Icon} n="folder" /><span class="mono trunc">${home(f.directory)}</span><span class="muted">${t("projects.page.changer")}</span></button>`
            : html`<button class="btn" onClick=${() => setPick('main')}><${Icon} n="folder" />${t("projects.page.choisir_le_dossier_du_projet")}</button>`}
          ${!f.machine && f.directory && f.directory === (p.directory || '') && html`<${Repo} info=${info} />`}
          ${f.directory && html`<div class="pj-extra"><span class="muted">${t("projects.page.dossiers_supplementaires")}<${Tip} text="${t("projects.page.d_autres_dossiers_de_la_meme_machine_auxquels_les_harnesses_du_pr")}" /></span>
            ${f.extra.map(d => html`<div class="pj-extra-row" key=${d}><${Icon} n="folder" /><span class="mono trunc">${home(d)}</span><button class="icon-btn" aria-label="${t("projects.page.retirer")}" onClick=${() => setF({ ...f, extra: f.extra.filter(x => x !== d) })}><${Icon} n="close" /></button></div>`)}
            <button class="btn sm ghost" onClick=${async () => { if (!f.machine) return setPick('extra'); const d = await prompt(t("projects.page.dossier_supplementaire"), { placeholder: t("projects.page.home_moi_autre"), ok: t("projects.page.ajouter") }); if (d) setF({ ...f, extra: [...f.extra, d.trim()] }); }}><${Icon} n="plus" />${t("projects.page.ajouter_un_dossier")}</button></div>`}
          ${!f.machine && f.directory && f.directory !== (p.directory || '') && html`<p class="note">${t("projects.page.enregistre_pour_lire_ce_dossier")}</p>`}
        </section>

        <section class="card pad pj-sec">
          <div class="sec-h"><h2>${t('continuity.core')}</h2></div>
          ${['purpose', 'rationale', 'constraints', 'decisions'].map(key => html`<label class="field"><span>${t({ purpose: 'continuity.purpose', rationale: 'continuity.rationale', constraints: 'continuity.constraints', decisions: 'continuity.decisions' }[key])}</span><textarea class="textarea" rows="3" maxlength="12000" value=${f.continuity.core[key] || ''} onInput=${e => setF({ ...f, continuity: { ...f.continuity, core: { ...f.continuity.core, [key]: e.target.value } } })}></textarea></label>`)}
          <label class="field"><span>${t('continuity.state')}</span><textarea class="textarea" rows="5" maxlength="4000" value=${f.continuity.working_state || ''} onInput=${e => setF({ ...f, continuity: { ...f.continuity, working_state: e.target.value } })}></textarea><small>${t('continuity.state_note')}</small></label>
          ${p.continuity?.state_updated_at && html`<p class="note">${new Date(p.continuity.state_updated_at).toLocaleString()}</p>`}
        </section>
        <section class="card pad pj-sec">
          <div class="sec-h"><h2>${t('continuity.references')}</h2></div><p class="note">${t('continuity.references_note')}</p>
          ${f.continuity.references.map((ref, i) => html`<div class="field" key=${ref.discussion_id}><div class="btn-row"><a class="linkish" href="#/chat" onClick=${() => open(ref.discussion_id)}>${nav.conversations.find(c => c.id === ref.discussion_id)?.title || ref.discussion_id}</a><button class="btn sm ghost" onClick=${() => setF({ ...f, continuity: { ...f.continuity, references: f.continuity.references.filter((_, n) => n !== i) } })}>${t('projects.page.retirer')}</button></div><label class="field"><span>${t('continuity.capsule')}</span><textarea class="textarea" rows="3" maxlength="1000" value=${ref.capsule} onInput=${e => setF({ ...f, continuity: { ...f.continuity, references: f.continuity.references.map((r, n) => n === i ? { ...r, capsule: e.target.value } : r) } })}></textarea></label></div>`)}
          ${f.continuity.references.length < 8 && html`<label class="field"><span>${t('continuity.link')}</span><select class="select" value="" onChange=${e => { if (e.target.value) setF({ ...f, brain: new Set([...f.brain, 'conversations']), budget: f.budget || 1500, continuity: { ...f.continuity, references: [...f.continuity.references, { discussion_id: e.target.value, capsule: '' }] } }); }}><option value="">—</option>${nav.conversations.filter(c => !f.continuity.references.some(r => r.discussion_id === c.id)).map(c => html`<option value=${c.id}>${c.title}</option>`)}</select></label>`}
        </section>

        <section class="card pad pj-sec">
          <div class="sec-h"><h2>${t("projects.page.contexte_du_projet")}<${Tip} text="${t("projects.page.ajoute_aux_messages_de_chaque_discussion_du_projet_quel_que_soit")}" /></h2></div>
          <label class="field"><span>${t("projects.page.instructions")}</span><textarea class="textarea" rows="6" value=${f.instructions} onInput=${e => setF({ ...f, instructions: e.target.value })} placeholder="${t("projects.page.objectifs_conventions_ce_qu_il_faut_savoir")}"></textarea></label>
          <div class="field"><span>${t("projects.page.fichiers_du_dossier")}${filesBytes > 0 && html` <small class="muted">· ${kb(filesBytes)} ${t("projects.page.48_ko")}</small>`}</span>
            ${f.machine ? html`<small>${t("projects.page.lus_seulement_quand_le_dossier_du_projet_est_sur_cette_machine")}</small>`
              : !f.directory ? html`<small>${t("projects.page.choisis_d_abord_le_dossier_du_projet")}</small>`
              : candidates.length + extra.length === 0 ? html`<small>${info ? t("projects.page.aucun_agents_md_readme_md_ni_fichier_docs_md_dans_ce_dossier") : t("projects.page.lecture")}</small>`
              : html`<div class="pj-files">${[...candidates, ...extra].map(c => html`<label class=${cls('pj-file', c.missing && 'off')} key=${c.path}>
                  <input type="checkbox" checked=${f.files.has(c.path)} onChange=${() => toggle('files', c.path)} /><span class="mono trunc">${c.path}</span><span class="muted">${c.missing ? t('common.not_found') : kb(c.size)}</span></label>`)}</div>`}
            ${filesBytes > 48 * 1024 && html`<small class="note warn">${t("projects.page.au_dela_de_48_ko_les_derniers_fichiers_ne_sont_pas_envoyes")}</small>`}</div>
          <div class="field"><span>${t("projects.page.skills")}</span>${(ws.capabilities || []).length ? html`<div class="chips">${ws.capabilities.map(c => html`<button type="button" class=${cls('chip-btn', f.skills.has(c.id) && 'on')} onClick=${() => toggle('skills', c.id)}>${f.skills.has(c.id) && html`<${Icon} n="check" />`}${c.name}</button>`)}</div>`
            : html`<small>${t("projects.page.aucun_skill")} <a href="#/brain/skills">${t("projects.page.creer_un_skill")}</a></small>`}</div>
        </section>

        <section class="card pad pj-sec">
          <div class="sec-h"><h2>${t("projects.page.brain")}<${Tip} text="${t("projects.page.a_chaque_message_loom_cherche_dans_ces_sources_les_passages_utile")}" /></h2><a class="btn sm ghost" href="#/brain/brain">${t("projects.page.gerer_les_sources")}</a></div>
          ${!brainSources ? html`<small class="muted">${t("projects.page.lecture")}</small>` : brainSources.length === 0 ? html`<small class="muted">${t("projects.page.aucune_source_ajoute_tes_notes_ou_docs_dans_ressources_brain")}</small>`
            : html`<div class="chips">${brainSources.map(b => html`<button type="button" class=${cls('chip-btn', f.brain.has(b.id) && 'on')} onClick=${() => { const s = new Set(f.brain); s.has(b.id) ? s.delete(b.id) : s.add(b.id); setF({ ...f, brain: s, budget: s.size && !f.budget ? 1500 : s.size ? f.budget : 0 }); }}>${f.brain.has(b.id) && html`<${Icon} n="check" />`}${brainLabel(b)}${b.kind === 'personal' && html` <${Icon} n="lock" />`}</button>`)}</div>`}
          ${f.brain.size > 0 && html`<label class="field"><span>${t("projects.page.budget_par_message")}</span><select class="select" value=${String(f.budget)} onChange=${e => setF({ ...f, budget: Number(e.target.value) })}>${[500, 1000, 1500, 3000, 5000, 8000].map(n => html`<option value=${n} selected=${n === f.budget}>${n} ${t("projects.page.tokens")}</option>`)}</select></label>`}
        </section>

        <section class="card pad pj-sec">
          <div class="sec-h"><h2>${t("projects.page.execution_par_defaut")}<${Tip} text="${t("projects.page.le_modele_ou_le_harness_avec_lequel_une_nouvelle_discussion_du_pr")}" /></h2></div>
          <select class="select" value=${f.def} onChange=${e => setF({ ...f, def: e.target.value })}>
            <option value="">${t("projects.page.modele_local_charge")}</option>
            ${groups.map(g => html`<optgroup label=${g}>${choices.filter(m => (m.kind === 'harness' ? 'Harness · ' : 'Cloud · ') + m.provider_name === g).map(m => html`<option value=${m.id} selected=${m.id === f.def}>${m.name === m.provider_name ? m.name : m.provider_name + ' · ' + m.name}</option>`)}</optgroup>`)}
          </select>
          ${f.def && !choices.some(m => m.id === f.def) && html`<p class="note warn">${t("projects.page.ce_choix_n_est_plus_disponible_dans_le_selecteur")}</p>`}
        </section>

        <div class="form-foot"><button class="btn danger ghost" onClick=${del}>${t("projects.page.supprimer_le_projet")}</button><span class="grow"></span>
          ${dirty && html`<button class="btn ghost" onClick=${reset}>${t("projects.page.annuler")}</button>`}<button class="btn primary" disabled=${!dirty} onClick=${save}>${t("projects.page.enregistrer")}</button></div>
      </div>

      <div class="card"><div class="sec-h pad-h"><h2>${t("projects.page.discussions")} <span class="count">${chats.length}</span></h2></div>
        ${chats.length ? html`<div class="rows">${chats.map(c => html`<button class="row link-row" onClick=${() => { open(c.id); go('chat'); }}><span class="grow t">${c.title || t("projects.page.discussion")}</span><${Icon} n="right" /></button>`)}</div>`
          : html`<p class="note pad-b">${t("projects.page.aucune_discussion_pour_l_instant")}</p>`}</div>
    </div>
    ${pick && html`<${FolderPicker} start=${f.directory} onClose=${() => setPick(false)} onPick=${d => { setPick(false); setF(pick === 'extra' ? { ...f, extra: [...f.extra, d] } : { ...f, directory: d, files: new Set() }); }} />`}
  </div></div>`;
}
