import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshNav, refreshWorkspace } from '../../core/state.js';
import { open, newDiscussion, chooseRemote } from '../chat/engine.js';
import { openTerminalWith } from '../terminals/page.js';
import { WorkspacePicker } from '../workspaces/folders.js';

export async function newProjectDiscussion(p) {
  await newDiscussion(p.id);
  const ws = app.get().workspace;
  const choice = p.default_choice && ((ws && ws.models) || []).find(m => m.id === p.default_choice && m.enabled);
  if (choice) await chooseRemote(choice);
}

const projectForm = p => ({ name: p.name, machine: p.machine || '', directory: p.directory || '', def: p.default_choice || '' });
const machineName = (machines, id) => id ? ((machines.find(m => m.id === id) || {}).name || id) : t('workspaces.local');

export function ProjectPage({ route }) {
  const { ws, nav } = useStore(app, s => ({ ws: s.workspace, nav: s.nav }));
  const p = ws && (ws.projects || []).find(x => x.id === route.sub);
  const [f, setF] = useState(null);
  const [machines, setMachines] = useState([]);
  useEffect(() => { get('/api/machines').then(r => setMachines(r.machines || [])).catch(() => {}); }, []);
  useEffect(() => { if (p) setF(projectForm(p)); }, [p && p.id]);
  if (!ws) return html`<div class="view page"><div class="page-in"><div class="skeleton" style="height:200px"></div></div></div>`;
  if (!p) return html`<div class="view page"><div class="page-in"><${Empty} icon="folder" title=${t('projects.page.projet_introuvable')} /></div></div>`;
  if (!f) return null;

  const chats = (nav.conversations || []).filter(c => c.project_id === p.id);
  const dirty = f.name.trim() !== p.name || f.machine !== (p.machine || '') || f.directory !== (p.directory || '') || f.def !== (p.default_choice || '');
  const save = async () => {
    const r = await post('/api/projects/context', {
      id: p.id, name: f.name.trim(), machine: f.machine, directory: f.directory, default_choice: f.def,
      continuity: p.continuity, instructions: p.instructions || '', extra_dirs: p.extra_dirs || [],
      brain_sources: p.brain_sources || [], brain_budget: p.brain_budget || 0,
      capability_ids: p.capability_ids || [], context_files: p.context_files || [], mcp_servers: p.mcp_servers
    });
    if (!r.ok) return toast(r.error, 'err');
    const updated = await refreshWorkspace();
    const saved = updated?.projects?.find(x => x.id === p.id);
    if (saved) setF(projectForm(saved));
    refreshNav();
    toast(t('projects.page.projet_enregistre_applique_aux_prochains_messages'));
  };
  const del = async () => {
    if (!await confirm(t('projects.page.supprimer_le_projet'), t('projects.page.ses_discussions_ne_sont_pas_effacees_elles_reviennent_dans_recent'), { ok: t('projects.page.supprimer'), danger: true })) return;
    await post('/api/projects/delete', { id: p.id });
    await refreshWorkspace(); refreshNav(); go('chat');
  };
  const models = (ws.models || []).filter(m => m.enabled);
  const groups = [['local', t('app.routes.local')], ['cloud', t('app.routes.cloud')], ['harness', t('app.routes.harnesses')]];

  return html`<div class="view page"><div class="page-in project-simple">
    <div class="page-head"><div><h1>${p.name}</h1><p>${t('project.simple.subtitle')}</p></div>
      <div class="acts">${p.directory && html`<button class="btn" onClick=${() => openTerminalWith({ target: p.machine || 'local', dir: p.directory, title: p.name })}><${Icon} n="prompt" />${t('projects.page.terminal')}</button>`}<button class="btn primary" onClick=${() => newProjectDiscussion(p)}><${Icon} n="plus" />${t('projects.page.nouvelle_discussion')}</button></div></div>

    <div class="project-simple-grid">
      <section class="card project-settings-card">
        <div class="project-setting"><div><b>${t('project.simple.name')}</b><small>${t('project.simple.name_note')}</small></div><input class="input" value=${f.name} maxlength="80" onInput=${e => setF({ ...f, name: e.target.value })} /></div>
        <div class="project-setting"><div><b>${t('project.simple.machine')}</b><small>${t('project.simple.machine_note')}</small></div><select class="select" value=${f.machine} onChange=${e => setF({ ...f, machine: e.target.value, directory: '' })}><option value="">${t('workspaces.local')}</option>${machines.map(m => html`<option value=${m.id}>${m.name || m.host}</option>`)}</select></div>
        <div class="project-setting"><div><b>${t('project.simple.workspace')}</b><small>${f.directory ? t('project.simple.workspace_fixed') : t('project.simple.workspace_inherited', { machine: machineName(machines, f.machine) })}</small></div><${WorkspacePicker} target=${f.machine || 'local'} current=${f.directory} inheritDefault onPick=${w => setF({ ...f, directory: w.path || '' })} /></div>
        <div class="project-setting"><div><b>${t('project.simple.executor')}</b><small>${t('project.simple.executor_note')}</small></div><select class="select" value=${f.def} onChange=${e => setF({ ...f, def: e.target.value })}><option value="">${t('project.simple.executor_current')}</option>${groups.map(([kind, label]) => models.some(m => m.kind === kind) && html`<optgroup label=${label}>${models.filter(m => m.kind === kind).map(m => html`<option value=${m.id}>${m.provider_name && m.name !== m.provider_name ? m.provider_name + ' · ' + m.name : m.name}</option>`)}</optgroup>`)}</select></div>
        <div class="project-save"><button class="btn danger ghost" onClick=${del}>${t('projects.page.supprimer_le_projet')}</button><span class="grow"></span>${dirty && html`<button class="btn ghost" onClick=${() => setF(projectForm(p))}>${t('projects.page.annuler')}</button>`}<button class="btn primary" disabled=${!dirty || !f.name.trim()} onClick=${save}>${t('projects.page.enregistrer')}</button></div>
      </section>

      <section class="card project-conversations"><div class="sec-h pad-h"><div><h2>${t('projects.page.discussions')} <span class="count">${chats.length}</span></h2><p>${t('project.simple.memory_note')}</p></div></div>
        ${chats.length ? html`<div class="rows">${chats.map(c => html`<button class="row link-row" onClick=${() => { open(c.id); go('chat'); }}><span class="grow t">${c.title || t('projects.page.discussion')}</span><${Icon} n="right" /></button>`)}</div>` : html`<${Empty} icon="chat" title=${t('projects.page.aucune_discussion_pour_l_instant')}><button class="btn" onClick=${() => newProjectDiscussion(p)}>${t('projects.page.nouvelle_discussion')}</button></${Empty}>`}
      </section>
    </div>
  </div></div>`;
}
