import { useVisibleRefresh } from '../usage/refresh.js';
import { t, locale, tSource } from '../../core/i18n.js';
import { SectionTabs } from '../../app/sections.js';
// Harnesses : agents qui exécutent (Codex, Antigravity…). Loom garde la
// discussion ; chaque harness garde son compte, ses permissions et sa mémoire.
import { Logo } from '../../ui/logo.js';
import { html, useState, useEffect, useStore, cls, fmtTok } from '../../core/lib.js';
import { Drawer, inspectTrigger } from '../../ui/drawer.js';
import { SelectionInfo } from '../inspector/selection.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Tip, Seg } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshWorkspace, refreshNav } from '../../core/state.js';
import { groupVariants } from '../chat/picker.js';
import { splitEffort } from '../inspector/inspector.js';
import { setVisible } from '../cloud/page.js';
import { newDiscussion, chooseRemote, open as openChat } from '../chat/engine.js';

import { Lifecycle, remoteHarnessTarget } from './lifecycle.js';
import { HarnessAccount } from './account.js';
import { HarnessHistory } from './history.js';

// Ce que Loom sait vraiment piloter aujourd'hui, par capacité déclarée.
const CAPS = () => ([
  ['chat', t("harnesses.page.discussion_dans_le_fil_commun")], ['native-events', t("harnesses.page.outils_natifs_visibles")], ['usage', t("harnesses.page.tokens_et_quotas")],
  ['reasoning-summary', t("harnesses.page.resume_de_reflexion")], ['approvals', t("harnesses.page.autorisations_interactives")], ['skills', t("harnesses.page.skills_partages")], ['mcp', t("harnesses.page.serveurs_mcp")],
  ['tools', t("harnesses.page.outils_et_modifications_visibles")], ['plan', t("harnesses.page.plan_de_l_agent")], ['workdir', t("harnesses.page.dossier_de_travail")], ['remote', t("harnesses.page.sur_une_autre_machine")],
]);
const isACP = rt => (rt.capabilities || []).includes('workdir');

// Ajout ou modification d'un harness ACP personnalisé (n'importe quel agent qui
// parle ACP sur stdio, y compris sur une autre machine via ssh).
// Les harnesses d'une autre machine passent par « Connecter une machine ».
const PRESETS = () => ([{ label: t("harnesses.page.agent_sur_cette_machine"), name: '', command: '', args: '', remote: false }]);
const splitArgs = localT => (String(localT).match(/"[^"]*"|'[^']*'|\S+/g) || []).map(a => a.replace(/^(["'])(.*)\1$/, '$2'));
const joinArgs = a => (a || []).map(x => /\s/.test(x) ? '"' + x + '"' : x).join(' ');

// Ajouter un agent : d'abord ceux déjà détectés sur tes machines (à gérer, et
// éventuellement à utiliser), puis ceux à installer ici, puis une commande ACP.
// Catalogue officiel ACP (registre tenu par le projet ACP) : tout agent listé
// s'ajoute d'un clic, à la version publiée. Les agents que Loom intègre déjà
// nativement n'y sont pas proposés en double.
function Catalogue({ onAdded }) {
  const [list, setList] = useState(null);
  const [q, setQ] = useState('');
  const [busy, setBusy] = useState('');
  useEffect(() => { get('/api/agents/catalog').then(r => setList(Array.isArray(r) ? r : [])).catch(() => setList([])); }, []);
  const add = async a => {
    setBusy(a.id);
    const r = await post('/api/agents/catalog/add', { id: a.id }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.install_hint ? t('agents.catalog.install_first', { name: a.name }) : r.error || t('agents.change_failed'), 'err');
    await refreshWorkspace(); onAdded(r.agent && r.agent.id);
  };
  if (list === null) return html`<div class="skeleton" style="height:120px"></div>`;
  const needle = q.trim().toLowerCase();
  const rows = list.filter(a => !a.builtin && (!needle || (a.name + ' ' + (a.description || '')).toLowerCase().includes(needle)));
  return html`<div class="cat-search"><${Icon} n="search" /><input class="input" placeholder=${t('agents.catalog.search')} value=${q} onInput=${e => setQ(e.target.value)} /></div>
    <div class="card cat-list">${rows.map(a => html`<div class="set-line" key=${a.id}>
      <div class="set-l cat-l">${a.icon ? html`<img class="cat-ico" src=${a.icon} alt="" loading="lazy" />` : html`<${Logo} name=${a.id} size="sm" />`}
        <div class="grow"><div class="cat-name">${a.name} <span class="muted mono">${a.version}</span></div>${a.description && html`<div class="cat-d">${a.description}</div>`}</div></div>
      <div class="set-c">${a.added ? html`<span class="muted">${t('agents.catalog.added')}</span>`
        : a.installed ? html`<button class="btn sm" disabled=${!!busy} onClick=${() => add(a)}>${t('agents.catalog.add')}</button>`
        : html`<a class="btn sm ghost" href=${a.repository || '#'} target="_blank" rel="noopener noreferrer" title=${t('agents.catalog.not_installed')}>${t('agents.catalog.install')}</a>`}</div></div>`)}
      ${!rows.length && html`<p class="note pad">${t('agents.catalog.empty')}</p>`}</div>
    <p class="note">${t('agents.catalog.note')}</p>`;
}

function AddAgentDialog({ installs, onClose, onChanged, onCustom }) {
  const [busy, setBusy] = useState('');
  const detected = installs.filter(i => i.installed && !i.managed);
  const missing = installs.filter(i => i.machine === 'local' && !i.installed);
  const apply = async (i, use) => {
    if (use && !await confirm(t('agents.enable_title', { name: i.name }), t('agents.enable_note', { name: i.name, machine: i.machine_name }), { ok: t('agents.enable_ok') })) return;
    setBusy(i.machine + i.harness);
    const r = await post('/api/agents/installations', { machine: i.machine, harness: i.harness, managed: true, ...(use ? { enabled: true, consent: true } : {}) }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t('agents.change_failed'), 'err');
    await refreshWorkspace(); onChanged(r.installations || []);
  };
  return html`<${Modal} title=${t('agents.add.title')} sub=${t('agents.add.sub')} onClose=${onClose}>
    <div class="add-agents">
      <h4>${t('agents.add.detected')}</h4>
      ${detected.length ? html`<div class="card">${detected.map(i => html`<div class="set-line" key=${i.machine + i.harness}>
          <div class="set-l"><${Logo} name=${i.logo || i.harness} size="sm" /><span>${i.name}</span><span class="muted">· ${i.machine === 'local' ? t('agents.this_machine') : i.machine_name}${i.version ? ' · ' + i.version.split(' ')[0] : ''}</span></div>
          <div class="set-c"><button class="btn sm ghost" disabled=${!!busy} onClick=${() => apply(i, false)}>${t('agents.manage')}</button>
            <button class="btn sm" disabled=${!!busy || !i.ready} onClick=${() => apply(i, true)}>${t('agents.add.manage_use')}</button></div></div>`)}</div>`
        : html`<p class="note">${t('agents.add.none_detected')}</p>`}
      ${missing.length > 0 && html`<h4>${t('agents.add.install')}</h4><div class="card">${missing.map(i => html`<div class="set-line" key=${i.harness}>
          <div class="set-l"><${Logo} name=${i.logo || i.harness} size="sm" /><span>${i.name}</span></div>
          <div class="set-c"><${Lifecycle} compact target="local" id=${i.harness} name=${i.name} where=${t('agents.this_machine')} onChange=${() => onChanged(null)} /></div></div>`)}</div>
        <p class="note">${t('agents.add.install_note')}</p>`}
      <h4>${t('agents.catalog.title')}</h4>
      <${Catalogue} onAdded=${id => { onClose(); if (id) go('harnesses', id); }} />
      <h4>${t('agents.add.custom')}</h4>
      <div class="card"><div class="set-line"><div class="set-l"><span>${t('agents.add.custom_text')}</span></div><div class="set-c"><button class="btn sm ghost" onClick=${onCustom}>${t('agents.add.custom_btn')}</button></div></div></div>
    </div></${Modal}>`;
}

function CustomDialog({ agent, onClose }) {
  const [v, setV] = useState(agent ? { name: agent.name, command: agent.command, args: joinArgs(agent.args), remote: !!agent.remote } : { ...PRESETS()[0] });
  const [busy, setBusy] = useState(false);
  const set = patch => setV({ ...v, ...patch });
  const save = async () => {
    if (/<hôte>/.test(v.args)) return toast(t("harnesses.page.remplace_hote_par_ta_machine_ex_hermes_lxc"), 'err');
    setBusy(true);
    const r = await post('/api/harness/custom', { id: agent ? agent.id : '', name: v.name, command: v.command, args: splitArgs(v.args), remote: v.remote });
    setBusy(false);
    if (!r.ok) return toast(r.error || t("harnesses.page.enregistrement_impossible"), 'err');
    toast(v.name + t("harnesses.page.ajoute")); await refreshWorkspace(); onClose(r.agent);
  };
  return html`<${Modal} title=${agent ? t("environment.page.modifier_prefix") + agent.name : t("harnesses.page.ajouter_un_harness")} sub="${t("harnesses.page.tout_agent_qui_parle_acp_agent_client_protocol_sur_stdio")}" onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>${t("harnesses.page.annuler")}</button><button class="btn primary" disabled=${busy || !v.name || !v.command} onClick=${save}>${t("harnesses.page.enregistrer")}</button>`}>
    <label class="field"><span>${t("harnesses.page.nom")}</span><input class="input" value=${v.name} placeholder="${t("harnesses.page.ex_hermes")}" onInput=${e => set({ name: e.target.value })} /></label>
    <label class="field"><span>${t("harnesses.page.commande")}</span><input class="input mono" value=${v.command} placeholder="${t("harnesses.page.ex_ssh_npx_opencode")}" onInput=${e => set({ command: e.target.value })} /></label>
    <label class="field"><span>${t("harnesses.page.arguments")}</span><input class="input mono" value=${v.args} placeholder="${t("harnesses.page.ex_t_hermes_lxc_hermes_acp")}" onInput=${e => set({ args: e.target.value })} /></label>
    <div class="set-line" style="padding:4px 0;border:0"><div class="set-l"><span>${t("harnesses.page.sur_une_autre_machine")}</span><${Tip} text="${t("harnesses.page.le_dossier_de_travail_est_alors_un_chemin_sur_cette_machine_la_lo")}" /></div>
      <div class="set-c"><${Switch} checked=${v.remote} label="${t("harnesses.page.sur_une_autre_machine")}" onChange=${on => set({ remote: on })} /></div></div>
  </${Modal}>`;
}

function Detail({ rt, models, onInspect }) {
  const native = models.filter(m => m.runtime_id === rt.id);
  const groups = groupVariants(native);
  const connected = native.length > 0;
  const supported = rt.implemented && rt.capabilities && rt.capabilities.length > 0;
  const connectable = supported && rt.capabilities.includes('connect');
  const [busy, setBusy] = useState(false);
  const connect = async () => {
    if (!connectable || !rt.cli || !rt.consent) return;
    if (!await confirm(t("harnesses.page.connecter_prefix") + rt.name, tSource(rt.consent), { ok: t("harnesses.page.connecter") })) return;
    setBusy(true);
    let r;
    try { r = await post('/api/runtimes/' + encodeURIComponent(rt.id) + '/connect', { consent: true }); }
    catch (e) { toast(e.message, 'err'); return; }
    finally { setBusy(false); }
    if (!r.ok) return toast(r.error, 'err'); toast(t("harnesses.page.modeles_de") + rt.name + t("harnesses.page.disponibles")); refreshWorkspace();
  };
  return html`<div class="h-detail anim-fade" key=${rt.id}>
    <div class="h-head"><div style="display:flex;gap:14px;align-items:center"><${Logo} name=${rt.id} size="lg" /><div><h2>${rt.name}</h2><p>${tSource(rt.description) || ''}</p></div></div>
      ${connectable ? html`<button class="btn" disabled=${busy} onClick=${connect}>${connected ? html`<${Icon} n="refresh" />${t("harnesses.page.actualiser")}` : t("cloud.page.connecter")}</button>` : !supported && html`<span class="tag">${t("harnesses.page.bientot")}</span>`}</div>
    ${!supported ? html`<div class="card pad soon"><${Icon} n="sparkle" /><div><b>${t("harnesses.page.adaptateur_en_preparation")}</b><p>${t("harnesses.page.loom_ne_lance_pas_encore")} ${rt.name}${t("harnesses.page.il_apparaitra_dans_le_selecteur_quand_son_adaptateur_saura_gerer")}</p></div></div>` : html`
      <div class="grid2">
        <div class="card pad"><div class="sec-h"><h2>${t("harnesses.page.source_du_modele")}<${Tip} text="${t("harnesses.page.natif_le_compte_du_harness_choisit_parmi_ses_modeles_les_modeles")}" /></h2></div>
          <div class="source"><label class="choice on"><input type="radio" checked /><span class="grow"><b>${t("harnesses.page.natif")}</b><small>${t("harnesses.page.le_compte")} ${rt.name} ${t("harnesses.page.choisit_ses_modeles")}</small></span></label>
            <label class="choice off"><input type="radio" disabled /><span class="grow"><b>${t("harnesses.page.modele_loom")}</b><small>${t("harnesses.page.local_ou_cloud_bientot")}</small></span></label></div></div>
        <div class="card pad"><div class="sec-h"><h2>${t("harnesses.page.ce_que_loom_pilote")}</h2></div>
          ${CAPS().map(([id, label]) => html`<div class="kv"><span>${label}</span>${(rt.capabilities || []).includes(id) ? html`<span class="state"><${Icon} n="check" />${t("harnesses.page.oui")}</span>` : html`<span class="muted">${t("harnesses.page.non")}</span>`}</div>`)}</div>
      </div>
      <div class="card"><div class="sec-h pad-h"><h2>${t("harnesses.page.modeles")} <span class="count">${groups.length}</span></h2>${connected && html`<span class="muted">${t("harnesses.page.visibles_dans_le_selecteur_de_discussion")}</span>`}</div>
        ${connected ? html`<div class="rows">${groups.map(g => { const on = g.variants.some(v => v.enabled); return html`<div class="row"><span class="grow" ...${inspectTrigger(() => onInspect(g.variants[0]), t("cloud.page.inspecter") + g.name)}><div class="t">${g.name}</div><div class="s">${g.variants.length > 1 ? g.variants.length + t("harnesses.page.niveaux_de_reflexion") : g.variants[0].model}</div></span>
          <${Switch} checked=${on} label=${t("cloud.page.afficher") + g.name} onChange=${v => g.variants.forEach(x => setVisible(x.id, v))} /></div>`; })}</div>`
          : html`<p class="note pad-b">${t("harnesses.page.connecte")} ${rt.name} ${t("harnesses.page.pour_lire_les_modeles_de_ton_compte")}</p>`}</div>
      <p class="note">${t("harnesses.page.chaque_envoi_ouvre_une_session_native_avec_le_texte_commun_de_la")} ${rt.name} ${t("harnesses.page.restent_chez_lui")} <a href="#/usage">${t("harnesses.page.voir_les_quotas")}</a></p>`}
    <${MachineState} rt=${rt} />
  </div>`;
}

const SHORT = () => ({ chat: t("harnesses.page.discussion"), 'native-events': t("harnesses.page.outils_natifs"), usage: t("harnesses.page.tokens_et_cout"), 'reasoning-summary': t("harnesses.page.resume_de_reflexion"), approvals: t("harnesses.page.autorisations"), skills: 'Skills', mcp: 'MCP Loom', tools: t("harnesses.page.outils_et_diffs"), plan: 'Plan', workdir: t("harnesses.page.dossier_de_travail"), remote: t("harnesses.page.machine_distante") });

// Harness ACP : tout ce que Loom sait de l'agent (version, modèles, réflexion,
// modes, intégration Loom, usage, discussions) et les actions utiles.
const optValues = o => { const out = []; const walk = l => (l || []).forEach(x => x.options ? walk(x.options) : out.push(x)); walk(o && o.options); return out; };
const byCat = (cfg, cat) => (cfg || []).find(o => o.category === cat) || (cfg || []).find(o => o.id === cat);
const ago = ms => { const m = Math.round((Date.now() - ms) / 60000); return m < 1 ? t("harnesses.page.a_l_instant") : m < 60 ? t('common.relative.minutes', { n: m }) : m < 1440 ? t('common.relative.hours', { n: Math.round(m / 60) }) : t('common.relative.days', { n: Math.round(m / 1440) }); };

// Sessions déjà ouvertes dans le harness (hors Loom) : les importer pour les
// continuer ici, avec la mémoire native du harness.
// Sessions natives d'un agent, sur cette machine ou sur une autre machine où
// il est géré. Une session d'une autre machine est reprise ici par l'agent
// utilisé (target), avec sa transcription.
function NativeSessions({ rt, embedded, onCount, source = 'local', target = null }) {
  const [list, setList] = useState(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState('');
  const [q, setQ] = useState('');
  const [project, setProject] = useState('');
  const [fresh, setFresh] = useState(true);
  const projects = useStore(app, s => s.workspace?.projects || []);
  const load = async () => {
    setErr(''); setList(null);
    const r = await get(source === 'local' ? '/api/runtimes/' + rt.id + '/sessions' : '/api/harness-history?source=' + encodeURIComponent(source), { timeout: 95000 }).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) { setErr(r.error || t("harnesses.page.lecture_impossible")); setList([]); return; }
    // Sessions Loom itself opened in the agent carry Loom's handoff text: they
    // are already discussions here.
    const own = x => /^Loom portable discussion/.test(x.title || '');
    const sessions = (r.sessions || []).filter(x => !own(x)).sort((a, b) => String(b.updatedAt || '').localeCompare(String(a.updatedAt || '')));
    setList(sessions); onCount && onCount(sessions.filter(x => !x.imported).length);
  };
  useEffect(() => { if (source !== 'local' || rt.available !== false) load(); }, [rt.id, source]);
  const importOne = async x => {
    if (x.imported) { openChat(x.imported); go('chat'); return; }
    if (source !== 'local') {
      if (!target) return toast(t('agents.import.no_target'), 'err');
      if (!await confirm(t('agents.import.remote_title'), t('agents.import.remote_note', { name: rt.name, target: target.name }), { ok: t("harnesses.page.importer") })) return;
      setBusy(x.sessionId);
      const r = await post('/api/harness-history/transfer', { source, choice_id: target.id, project_id: project, consent: true, ...x }, { timeout: 190000 }).catch(e => ({ ok: false, error: e.message }));
      setBusy('');
      if (!r.ok) return toast(r.error || t("harnesses.page.import_impossible"), 'err');
      await refreshNav(); openChat(r.session.id); go('chat'); return;
    }
    setBusy(x.sessionId);
    const r = await post('/api/runtimes/' + rt.id + '/sessions/import', { sessionId: x.sessionId, cwd: x.cwd, title: x.title || '', project_id: project, fresh }, { timeout: 190000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t("harnesses.page.import_impossible"), 'err');
    await refreshNav(); openChat(r.session.id); go('chat');
  };
  const shown = (list || []).filter(x => !q || ((x.title || '') + ' ' + x.cwd).toLowerCase().includes(q.toLowerCase()));
  const rows = list === null ? html`<div class="card pad"><div class="state"><span class="spinner"></span>${t("harnesses.page.lecture_des_sessions_de")} ${rt.name}…</div></div>`
      : err ? html`<div class="card pad"><p class="note err">${err}</p></div>`
      : !shown.length ? html`<div class="card pad"><p class="note">${q ? t("harnesses.page.aucune_session_ne_correspond") : t('agents.native_empty', { name: rt.name })}</p></div>`
      : html`<div class="card rows">${shown.slice(0, 30).map(x => html`<div class="row" key=${x.sessionId}>
          <div class="grow"><div class="t">${x.title || t("harnesses.page.session_sans_titre")}</div>
            <div class="s">${[x.cwd.replace(/^\/home\/[^/]+/, '~'), x.updatedAt ? ago(Date.parse(x.updatedAt)) : ''].filter(Boolean).join(' · ')}</div></div>
          <button class=${cls('btn sm', x.imported && 'ghost')} disabled=${!!busy} onClick=${() => importOne(x)}>${busy === x.sessionId ? html`<span class="spinner"></span>${t("harnesses.page.import")}` : x.imported ? t("harnesses.page.ouvrir") : t("harnesses.page.importer")}</button></div>`)}</div>`;
  if (embedded) return html`<div class="import-bar">
      <label class="field inline"><span>${t('harnesses.import.project')}</span><select class="select sm" value=${project} onChange=${e => setProject(e.target.value)}><option value="">—</option>${projects.map(p => html`<option value=${p.id}>${p.name}</option>`)}</select></label>
      ${source === 'local' && html`<label class="check"><input type="checkbox" checked=${fresh} onChange=${e => setFresh(e.target.checked)} /><span>${t('agents.import_fresh')}</span><${Tip} text=${t('harnesses.import.fresh') + ' ' + t('harnesses.import.note')} /></label>`}
      <span class="grow"></span>${list && list.length > 6 && html`<label class="search" style="width:200px"><${Icon} n="search" /><input placeholder="${t("harnesses.page.filtrer")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>`}
      <button class="btn sm ghost" onClick=${load}><${Icon} n="refresh" />${t("harnesses.page.actualiser")}</button></div>${rows}`;
  return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.sessions_de")} ${rt.name} <span class="count">${list ? list.length : ''}</span><${Tip} text=${t("harnesses.page.les_discussions_que_tu_as_eues_directement_dans") + rt.name + t("harnesses.page.les_importer_les_ajoute_a_loom_tu_les_continues_ici_avec_la_memoi")} /></h2>
      <span style="display:flex;gap:8px">${list && list.length > 6 && html`<label class="search" style="width:220px"><${Icon} n="search" /><input placeholder="${t("harnesses.page.filtrer")}" aria-label="${t("harnesses.page.filtrer_les_sessions")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>`}
      <button class="btn sm ghost" onClick=${load}><${Icon} n="refresh" />${t("harnesses.page.actualiser")}</button></span></div>
    <label class="field"><span>${t('harnesses.import.project')}</span><select class="select" value=${project} onChange=${e => setProject(e.target.value)}><option value="">—</option>${projects.map(p => html`<option value=${p.id}>${p.name}</option>`)}</select></label>
    <label class="check"><input type="checkbox" checked=${fresh} onChange=${e => setFresh(e.target.checked)} /><span>${t('harnesses.import.fresh')}</span></label><p class="note">${t('harnesses.import.note')}</p>
    ${list === null ? html`<div class="card pad"><div class="state"><span class="spinner"></span>${t("harnesses.page.lecture_des_sessions_de")} ${rt.name}…</div></div>`
      : err ? html`<div class="card pad"><p class="note err">${err}</p></div>`
      : !shown.length ? html`<div class="card pad"><p class="note">${q ? t("harnesses.page.aucune_session_ne_correspond") : t("harnesses.page.aucune_session_trouvee")}</p></div>`
      : html`<div class="card rows">${shown.slice(0, 30).map(x => html`<div class="row" key=${x.sessionId}>
          <div class="grow"><div class="t">${x.title || t("harnesses.page.session_sans_titre")}</div>
            <div class="s">${[x.cwd.replace(/^\/home\/[^/]+/, '~'), x.updatedAt ? ago(Date.parse(x.updatedAt)) : ''].filter(Boolean).join(' · ')}</div></div>
          ${x.imported && html`<span class="tag">${t("harnesses.page.dans_loom")}</span>`}
          <button class="btn sm" disabled=${!!busy} onClick=${() => importOne(x)}>${busy === x.sessionId ? html`<span class="spinner"></span>${t("harnesses.page.import")}` : x.imported ? t("harnesses.page.ouvrir") : t("harnesses.page.importer")}</button></div>`)}</div>`}
  </section>`;
}

// Source du modèle : Natif (le harness choisit parmi ses modèles) et, quand
// le harness le permet, Loom (tes modèles locaux ajoutés à sa propre liste).
function ModelSource({ rt, line }) {
  const [x, setX] = useState(null);
  const [busy, setBusy] = useState(false);
  const load = () => get('/api/harness/model-source?id=' + encodeURIComponent(rt.id)).then(setX).catch(() => setX(null));
  useEffect(() => { load(); }, [rt.id]);
  if (!x) return null;
  const toggle = async on => {
    const msg = x.format === 'opencode'
      ? t("harnesses.page.tes_modeles_locaux_et_tes_fournisseurs_cloud_apparaissent_dans_la") + rt.name + t("harnesses.page.quand_loom_le_lance_rien_n_est_ecrit_dans_ses_fichiers_et_les_cle")
      : x.format === 'pi'
      ? t("harnesses.page.loom_ajoute_a") + x.file + t("harnesses.page.un_fournisseur_loom_tes_modeles_locaux_et_un_par_fournisseur_clou")
      : x.format === 'env'
      ? t("harnesses.page.tes_modeles_locaux_apparaissent_sous") + rt.name + t("harnesses.page.dans_le_selecteur_quand_tu_en_choisis_un_loom_lance") + rt.name + t("harnesses.page.avec_son_api_locale_comme_fournisseur_aucun_fichier_de") + rt.name + t("harnesses.page.n_est_modifie_et_ses_modeles_natifs_restent_disponibles")
      : t("harnesses.page.loom_ajoute_un_fournisseur_loom_dans") + x.file + t("harnesses.page.avec_tes_modeles_locaux_tes_autres_fournisseurs_ne_sont_pas_modif");
    if (on && !await confirm(t("harnesses.page.modeles_de_loom_dans") + rt.name, msg, { ok: t("harnesses.page.activer") })) return;
    setBusy(true);
    const r = await post('/api/harness/model-source', { id: rt.id, enabled: on });
    setBusy(false);
    if (!r.ok) return toast(r.error, 'err');
    toast(on ? t("harnesses.page.modeles_de_loom_proposes_dans") + rt.name : t("harnesses.page.modeles_de_loom_retires_de") + rt.name);
    load(); refreshWorkspace(); setTimeout(refreshWorkspace, 8000);
  };
  if (line) return html`<div class="set-line"><div class="set-l"><span>${t("harnesses.page.modeles_de_loom")}</span><${Tip} text="${t("harnesses.page.natif_le_harness_utilise_ses_propres_modeles_et_son_compte_loom_t")}" />${x.supported && html`<span class="muted" style="font-size:12px;margin-left:8px">${t('harnesses.models.local', { n: x.models })}</span>`}</div>
    <div class="set-c">${x.supported ? html`<${Switch} checked=${x.enabled} disabled=${busy} label=${t("harnesses.page.proposer_les_modeles_de_loom_dans") + rt.name} onChange=${toggle} />` : html`<span class="muted">${t("harnesses.page.pas_encore_pris_en_charge_par_ce_harness")}</span>`}</div></div>`;
  return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.source_du_modele")}<${Tip} text="${t("harnesses.page.natif_le_harness_utilise_ses_propres_modeles_et_son_compte_loom_t")}" /></h2></div>
    <div class="card">
      <div class="set-line"><div class="set-l"><span>${t("harnesses.page.natif")}</span></div><div class="set-c"><span class="state"><i class="dot green"></i>${t("harnesses.page.toujours_disponible")}</span></div></div>
      <div class="set-line"><div class="set-l"><span>${t("harnesses.page.modeles_de_loom")}</span>${x.supported && html`<span class="muted" style="font-size:12px;margin-left:8px">${t('harnesses.models.local', { n: x.models })}</span>`}</div>
        <div class="set-c">${x.supported ? html`<${Switch} checked=${x.enabled} disabled=${busy} label=${t("harnesses.page.proposer_les_modeles_de_loom_dans") + rt.name} onChange=${toggle} />`
          : html`<span class="muted">${t("harnesses.page.pas_encore_pris_en_charge_par_ce_harness")}</span>`}</div></div>
    </div></section>`;
}

// Ressources Loom reçues par ce harness : serveurs MCP (transmis à chaque
// session) et skills (copiées dans son dossier de skills), choisies une à une.
function LoomResources({ rt }) {
  const ws = useStore(app, a => a.workspace);
  const skills = (ws && ws.capabilities) || [];
  const [mcp, setMcp] = useState(null);
  const [sk, setSk] = useState(null);
  const remote = (rt.capabilities || []).includes('remote');
  const load = async () => {
    const [m, localT] = await Promise.all([get('/api/harness/bindings?id=' + encodeURIComponent(rt.id)).catch(() => null), get('/api/skills/targets').catch(() => null)]);
    setMcp(m && m.ok ? m : { mcp: [], custom: false });
    const target = ((localT && localT.targets) || []).find(x => x.harnesses.includes(rt.id)) || null;
    setSk({ target, bindings: (localT && localT.bindings) || {} });
  };
  useEffect(() => { load(); }, [rt.id]);
  const setMcpBound = async (name, on) => {
    const names = mcp.mcp.filter(r => (r.name === name ? on : r.bound)).map(r => r.name);
    const r = await post('/api/harness/bindings', { id: rt.id, mcp: names });
    if (!r.ok) return toast(r.error, 'err'); load();
  };
  const resetMcp = async () => { await post('/api/harness/bindings', { id: rt.id, mcp: null }); load(); };
  const enableTarget = async () => {
    const r = await post('/api/skills/targets', { id: sk.target.id, enabled: true });
    if (!r.ok) return toast(r.error, 'err'); load();
  };
  const setSkill = async (id, on) => {
    const r = await post('/api/skills/binding', { skill_id: id, target: sk.target.id, enabled: on });
    if (!r.ok) return toast(r.error, 'err'); load();
  };
  if (!mcp || !sk) return null;
  const bound = id => !(sk.bindings[id] && sk.bindings[id][sk.target.id] === false);
  return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.ressources_loom_transmises")}<${Tip} text=${t("harnesses.page.loom_garde_la_definition_de_tes_skills_et_serveurs_mcp_tu_choisis") + rt.name + t("harnesses.page.recoit_le_harness_reste_maitre_de_leur_execution")} /></h2>
      <a class="btn sm ghost" href="#/brain">${t("harnesses.page.gerer_les_ressources")}</a></div>
    <div class="grid2">
      <div class="card pad"><div class="sec-h"><h2>${t("harnesses.page.serveurs_mcp")}</h2>${mcp.custom && html`<button class="btn sm ghost" onClick=${resetMcp}>${t("harnesses.page.tous")}</button>`}</div>
        ${remote ? html`<p class="note">${t("harnesses.page.non_transmis_ce_harness_tourne_sur_une_autre_machine")}</p>`
          : mcp.mcp.length ? mcp.mcp.map(r => html`<div class="kv" key=${r.name}><span>${r.name}${!r.enabled && html` <span class="muted">${t("harnesses.page.desactive_dans_loom")}</span>`}</span>
              <${Switch} checked=${r.bound && r.enabled} disabled=${!r.enabled} label=${t("harnesses.page.transmettre") + r.name + t("harnesses.page.a") + rt.name} onChange=${on => setMcpBound(r.name, on)} /></div>`)
          : html`<p class="note">${t("harnesses.page.aucun_serveur_mcp_dans_loom")}</p>`}</div>
      <div class="card pad"><div class="sec-h"><h2>${t("harnesses.page.skills")}</h2></div>
        ${!sk.target ? html`<p class="note">${t("harnesses.page.loom_ne_connait_pas_encore_le_dossier_de_skills_de_ce_harness")}</p>`
          : !sk.target.enabled ? html`<div class="kv"><span>${t("harnesses.page.distribution_vers")} ${sk.target.dir.replace(/^\/home\/[^/]+/, '~')}</span><button class="btn sm" onClick=${enableTarget}>${t("harnesses.page.activer")}</button></div>`
          : skills.length ? skills.map(c => html`<div class="kv" key=${c.id}><span class="trunc" title=${c.description || ''}>${c.name}</span>
              <${Switch} checked=${bound(c.id)} label=${t("harnesses.page.envoyer") + c.name + t("harnesses.page.a") + rt.name} onChange=${on => setSkill(c.id, on)} /></div>`)
          : html`<p class="note">${t("harnesses.page.aucune_skill_dans_loom")}</p>`}
        ${sk.target && sk.target.harnesses.length > 1 && sk.target.enabled && html`<p class="note" style="margin-top:8px">${t("harnesses.page.dossier_partage_avec")} ${sk.target.harnesses.filter(h => h !== rt.id).join(', ')}.</p>`}</div>
    </div></section>`;
}

// Ce que le harness possède déjà sur cette machine, lu dans son CLI et ses
// dossiers : version, compte, clés présentes, MCP, plugins, skills. Mise à jour
// par sa propre commande, sur demande.
const MCP_STATE = () => ({ connected: ['green', t("harnesses.page.connecte_2")], enabled: ['green', t("harnesses.page.active")], 'needs-auth': ['amber', t("harnesses.page.a_authentifier")], disabled: ['', t("harnesses.page.desactive")], error: ['red', 'erreur'] });
function MachineState({ rt, commands, accountRevision = 0 }) {
  const [x, setX] = useState(null);
  const load = async refresh => {
    const r = await get('/api/runtimes/' + rt.id + '/inspect' + (refresh ? '?refresh=1' : '')).catch(() => null);
    setX(r && r.ok ? r.inspection : false);
  };
  useEffect(() => { load(accountRevision > 0); }, [rt.id, accountRevision]);
  const adopt = async m => {
    const r = await post('/api/runtimes/' + rt.id + '/mcp/adopt', { name: m.name }).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) return toast(r.error || t("harnesses.page.adoption_impossible"), 'err');
    toast(r.name + t("harnesses.page.ajoute_a_loom_desactive") + (r.env_to_fill && r.env_to_fill.length ? t("harnesses.page.a_completer") + r.env_to_fill.join(', ') : ''));
  };
  if (x === false) return null;
  if (!x) return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.sur_cette_machine")}</h2></div><div class="card pad"><div class="state"><span class="spinner"></span>${t("harnesses.page.lecture_de")} ${rt.name}…</div></div></section>`;
  if (!x.installed) return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.sur_cette_machine")}</h2></div><div class="card pad"><${Lifecycle} target="local" id=${rt.id} name=${rt.name} where="sur cette machine" onChange=${() => { setX(null); load(true); }} />${rt.docs ? html`<p class="note"><a href=${rt.docs} target="_blank" rel="noopener noreferrer">${t("harnesses.page.documentation_de")} ${rt.name}</a></p>` : ''}</div></section>`;
  const a = x.auth || {};
  return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.sur_cette_machine")}<${Tip} text=${t("harnesses.page.lu_directement_dans_le_cli_de") + rt.name + t("harnesses.page.et_ses_dossiers_loom_ne_lit_jamais_la_valeur_des_cles_il_indique")} /></h2>
      <button class="btn sm ghost" onClick=${() => { setX(null); load(true); }}><${Icon} n="refresh" />${t("harnesses.page.relire")}</button></div>
    <div class="grid2">
      <div class="card pad">
        <div class="kv"><span>${t("harnesses.page.emplacement")}</span><code class="mono trunc" style="max-width:62%">${x.path.replace(/^\/home\/[^/]+/, '~')}</code></div>
        <div class="kv"><span>${t("harnesses.page.compte")}</span><span class="state"><i class=${'dot ' + (a.connected ? 'green' : '')}></i>${a.connected ? [a.method, a.account].filter(Boolean).join(' · ') || a.status || t("harnesses.page.connecte_2") : a.providers ? t("harnesses.page.aucun_fournisseur") : a.status || t("harnesses.page.non_lu")}</span></div>
        ${a.providers && a.providers.length > 0 && html`<div class="kv"><span>${t("harnesses.page.fournisseurs")}</span><span>${a.providers.join(' · ')}</span></div>`}
        <div class="kv"><span>${t("harnesses.page.cles_d_api_detectees")}</span><span>${x.env.length ? x.env.map(e => html`<span class="tag">${e}</span> `) : html`<span class="muted">${t("harnesses.page.aucune_dans_l_environnement")}</span>`}</span></div>
        <${Lifecycle} target="local" id=${rt.id} name=${rt.name} where="sur cette machine" onChange=${() => load(true)} />
      </div>
      <div class="card pad">
        <div class="kv"><span>${t("harnesses.page.serveurs_mcp_du_harness")}</span><span class="num">${x.mcp_known ? x.mcp.length : t("harnesses.page.non_lus")}</span></div>
        ${x.mcp.map(m => { const st = MCP_STATE()[m.status] || ['', m.status || '']; return html`<div class="kv" key=${m.name}><span class="trunc" title=${m.target || ''}>${m.name}</span><span class="state">${st[1] && html`<i class=${'dot ' + st[0]}></i>`}${st[1]}
          ${(m.command || m.url) && html`<button class="btn sm ghost" title="${t("harnesses.page.copier_ce_serveur_dans_loom_pour_le_donner_aussi_aux_autres_harne")}" onClick=${() => adopt(m)}>${t("harnesses.page.adopter")}</button>`}</span></div>`; })}
        <div class="kv"><span>${t("harnesses.page.plugins")}</span><span class="num">${x.plugins.length}</span></div>
        ${x.plugins.map(p => html`<div class="kv" key=${p}><span>${p}</span><span></span></div>`)}
        <div class="kv"><span>${t("harnesses.page.skills_installees")}</span><span class="num">${x.skills.length}</span></div>
        ${x.skills.map(k => html`<div class="kv" key=${k.folder + k.name}><span class="trunc" title=${k.description || ''}>${k.name}</span>${k.from_loom ? html`<span class="tag">${t("harnesses.page.via_loom")}</span>` : html`<span class="muted mono" style="font-size:11.5px">${k.folder}</span>`}</div>`)}
        ${commands != null && html`<div class="kv"><span>${t("harnesses.page.commandes_disponibles")}<${Tip} text="${t("harnesses.page.commandes_et_skills_que_le_harness_annonce_dans_une_session_plugi")}" /></span><span class="num">${commands}</span></div>`}
      </div>
    </div>
  </section>`;
}

// ---------------------------------------------------------------- page d'un agent
// Une page = l'état de l'agent et l'action utile, où il est installé, ses
// discussions, ses modèles, ses réglages. Le reste est dans « Détails ».

// Une ligne par machine : version (et mise à jour), gérer, utiliser.
function MachineRow({ i, onChanged }) {
  const [x, setX] = useState(null);
  const [busy, setBusy] = useState('');
  const load = () => i.managed ? get('/api/harness/lifecycle?target=' + encodeURIComponent(i.machine) + '&id=' + encodeURIComponent(i.harness)).then(r => setX(r.state || false)).catch(() => setX(false)) : setX(false);
  useEffect(() => { setX(null); load(); }, [i.machine, i.harness, i.managed]);
  const where = i.machine === 'local' ? t('agents.this_machine') : i.machine_name;
  const change = async patch => {
    if (patch.enabled && !await confirm(t('agents.enable_title', { name: i.name }), t('agents.enable_note', { name: i.name, machine: where }), { ok: t('agents.enable_ok') })) return;
    setBusy('switch');
    const r = await post('/api/agents/installations', { machine: i.machine, harness: i.harness, ...patch, consent: !!patch.enabled }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t('agents.change_failed'), 'err');
    await refreshWorkspace(); onChanged();
  };
  const update = async () => {
    if (!await confirm(t('harnesses.lifecycle.mettre_a_jour') + ' ' + i.name, t('agents.update_note', { name: i.name, machine: where }))) return;
    setBusy('update');
    const r = await post('/api/harness/lifecycle', { target: i.machine, id: i.harness, action: 'update' }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (r.state) setX(r.state); else load();
    r.ok ? toast(i.name + t('harnesses.lifecycle.mis_a_jour')) : toast(r.error || t('harnesses.lifecycle.echec'), 'err');
  };
  const version = String((x && x.version) || i.version || '').match(/\d+(?:\.\d+)+[\w.+-]*/)?.[0] || '';
  return html`<div class="am-row">
    <span class="am-m"><${Icon} n=${i.machine === 'local' ? 'chip' : 'server'} /><b>${where}</b></span>
    <span class="am-v">${!i.installed ? html`<${Lifecycle} compact target=${i.machine} id=${i.harness} name=${i.name} where=${where} onChange=${onChanged} />` : !i.managed ? html`<span class="muted">—</span>` : x === null ? html`<span class="spinner"></span>` : html`<span class="mono">${version || '?'}</span>
      ${x && x.update_available ? html`<button class="btn sm" disabled=${!!busy} onClick=${update}>${busy === 'update' ? html`<span class="spinner"></span>` : html`<${Icon} n="download" />`}${x.latest}</button>` : x && x.latest ? html`<span class="muted">${t('harnesses.lifecycle.a_jour')}</span>` : ''}`}</span>
    <${Switch} label=${t('agents.manage')} checked=${i.managed} disabled=${!!busy} onChange=${v => change({ managed: v })} />
    <${Switch} label=${t('agents.use')} checked=${i.enabled} disabled=${!!busy || !i.ready} onChange=${v => change({ enabled: v })} />
  </div>`;
}

function AgentMachines({ installs, onChanged }) {
  // Installed somewhere, plus Loom's machine where it can be installed.
  const list = installs.filter(i => i.installed || i.machine === 'local');
  if (!list.length) return null;
  return html`<section class="sec"><div class="sec-h"><h2>${t('agents.machines')}<${Tip} text=${t('agents.machine_tip')} /></h2></div>
    <div class="card am">
      <div class="am-row am-head"><span>${t('agents.col.machine')}</span><span>${t('harnesses.lifecycle.version')}</span><span>${t('agents.manage')}</span><span>${t('agents.use')}</span></div>
      ${list.map(i => html`<${MachineRow} key=${i.machine} i=${i} onChanged=${onChanged} />`)}
    </div></section>`;
}

// Discussions : celles de Loom, et celles faites directement dans l'agent
// (hors sessions que Loom a lui-même ouvertes), importables ici.
function AgentDiscussions({ rt, talks, canList, installs, target }) {
  const [tab, setTab] = useState('loom');
  const [native, setNative] = useState(null);
  // Où lire les sessions : chaque machine où l'agent est géré et prêt.
  const sources = installs.filter(i => i.managed && i.installed && (i.machine !== 'local' ? i.ready : canList))
    .map(i => ({ value: i.machine === 'local' ? 'local' : 'remote:' + i.machine + ':' + i.harness, label: i.machine === 'local' ? t('agents.this_machine') : i.machine_name }));
  const [source, setSource] = useState(null);
  const from = source || (sources[0] && sources[0].value) || 'local';
  return html`<section class="sec"><div class="sec-h"><h2>${t('agents.discussions')}</h2>
      ${sources.length > 0 && html`<${Seg} size="sm" value=${tab} onChange=${setTab} label=${t('agents.discussions')} options=${[{ value: 'loom', label: t('agents.tab.loom'), count: talks.length }, { value: 'native', label: t('agents.tab.native', { name: rt.name }), count: native == null ? '' : native }]} />`}</div>
    ${tab === 'native' && sources.length ? html`${sources.length > 1 && html`<div class="src-pick"><span>${t('agents.import.from')}</span><${Seg} size="sm" value=${from} onChange=${v => { setSource(v); setNative(null); }} label=${t('agents.import.from')} options=${sources} /></div>`}
        <${NativeSessions} key=${from} rt=${rt} embedded source=${from} target=${target} onCount=${setNative} />`
      : talks.length ? html`<div class="card rows">${talks.slice(0, 8).map(c => html`<button type="button" class="row link-row" key=${c.id} onClick=${() => { openChat(c.id); go('chat'); }}>
          <div class="grow"><div class="t">${c.title || t("harnesses.page.discussion")}</div><div class="s">${[c.model && c.model !== 'default' ? c.model : '', c.workdir ? c.workdir.split('/').pop() : '', c.updated_at ? ago(c.updated_at) : ''].filter(Boolean).join(' · ')}</div></div>
          <${Icon} n="right" /></button>`)}</div>`
      : html`<div class="card pad"><p class="note">${t('agents.no_talks', { name: rt.name })}</p></div>`}
  </section>`;
}

const MODEL_LIMIT = 8;
// Voie de connexion officielle de chaque agent (voir docs/agents-compat.md).
const PROTOCOL = { 'app-server': 'App Server', 'pi-rpc': 'RPC', 'opencode-http': 'Serveur', 'agy-stream-json': 'Stream JSON', acp: 'ACP' };

function AgentDetail({ rt, models, onEdit }) {
  const nav = useStore(app, a => a.nav);
  const [installs, setInstalls] = useState([]);
  const [probe, setProbe] = useState(null);
  const [info, setInfo] = useState(null);
  const [busy, setBusy] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const [resources, setResources] = useState(false);
  const [custom, setCustom] = useState(null);
  const [usage, setUsage] = useState(null);
  const [allModels, setAllModels] = useState(false);
  const family = rt.machine ? rt.logo : rt.id;
  const loadInstalls = () => get('/api/agents/installations').then(r => r.ok && setInstalls((r.installations || []).filter(i => i.harness === family || i.runtime_id === rt.id))).catch(() => {});
  const loadProbe = () => get('/api/runtimes/' + rt.id + '/probe').then(p => setProbe((p && p.probe) || {})).catch(() => setProbe({}));
  const loadInfo = refresh => get('/api/runtimes/' + rt.id + '/inspect' + (refresh ? '?refresh=1' : '')).then(r => setInfo(r && r.ok ? r.inspection : false)).catch(() => setInfo(false));
  useEffect(() => {
    loadInstalls(); loadProbe(); if (!rt.machine) loadInfo();
    if (rt.custom) get('/api/harness/custom').then(r => setCustom((r.agents || []).find(a => a.id === rt.id) || null)).catch(() => {});
    get('/api/usage/native?days=7').then(r => setUsage((r.harnesses || []).find(x => x.runtime_id === rt.id) || null)).catch(() => {});
  }, [rt.id]);
  const here = installs.find(i => i.runtime_id === rt.id) || installs.find(i => i.machine === 'local');
  const managed = !!(here && here.managed) || !!rt.custom, used = !!rt.connected;
  const missing = rt.available === false;
  const setScope = async patch => {
    if (patch.enabled && !await confirm(t('agents.enable_title', { name: rt.name }), t('agents.enable_note', { name: rt.name, machine: here ? (here.machine === 'local' ? t('agents.this_machine') : here.machine_name) : '' }), { ok: t('agents.enable_ok') })) return;
    setBusy(true);
    const r = await post('/api/agents/installations', { machine: here.machine, harness: here.harness, ...patch, consent: !!patch.enabled }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok) return toast(r.error || t('agents.change_failed'), 'err');
    await refreshWorkspace(); loadInstalls(); loadProbe();
  };
  const reread = async () => {
    setBusy(true);
    const r = await post('/api/runtimes/' + rt.id + '/connect', { consent: true }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok) toast(r.error || t("harnesses.page.lecture_impossible"), 'err');
    await refreshWorkspace(); loadProbe(); if (!rt.machine) loadInfo(true);
  };
  const del = async () => {
    if (!await confirm(t("harnesses.page.supprimer") + rt.name, t("harnesses.page.le_harness_disparait_de_loom_les_discussions_passees_restent_lisi"), { ok: t("harnesses.page.supprimer_2"), danger: true })) return;
    const r = await post('/api/harness/custom/delete', { id: rt.id });
    if (!r.ok) return toast(r.error, 'err');
    await refreshWorkspace(); go('harnesses');
  };
  const cfg = (probe && probe.config) || [];
  const modelOpt = byCat(cfg, 'model'), effortOpt = byCat(cfg, 'thought_level');
  const list = optValues(modelOpt);
  const groups = splitEffort(list);
  const choices = models.filter(m => m.runtime_id === rt.id);
  const choiceFor = v => choices.find(c => c.id === rt.id + ':' + v) || choices[0];
  const startWith = async choice => { if (!choice) return toast(t("harnesses.page.ce_harness_n_est_pas_encore_disponible"), 'err'); await newDiscussion(); await chooseRemote(choice); };
  const talks = ((nav && nav.conversations) || []).filter(c => c.runtime_id === rt.id).sort((x, y) => (y.updated_at || 0) - (x.updated_at || 0));
  const auth = (info && info.auth) || {};
  // An agent that lists its models has a working account even when Loom cannot read it.
  const accountKnown = !!info && info.installed !== false && managed && (auth.connected || !cfg.length);
  const account = info ? (auth.connected ? [auth.method, auth.account].filter(Boolean).join(' · ') || t("harnesses.page.connecte_2") : auth.status || t('agents.account.none')) : '';
  const ver = probe && probe.agent && probe.agent.version;
  const compat = (probe && probe.compatibility) || rt.compatibility || {};
  const canList = !!(probe && probe.capabilities && probe.capabilities.sessionCapabilities && probe.capabilities.sessionCapabilities.list);
  const [tone, state] = missing ? ['', t("harnesses.page.non_installe")] : used ? ['green', t('agents.state.used')] : managed ? ['amber', t('agents.state.managed')] : ['', t('agents.state.unmanaged')];
  const launch = custom ? [custom.command, ...(custom.args || [])].join(' ') : rt.install_hint || rt.cli;
  return html`<div class="agent anim-fade">
    <div class="agent-h"><${Logo} name=${rt.logo || rt.id} size="lg" />
      <div class="grow"><div class="agent-t"><h2>${rt.name}</h2><span class=${'pill ' + tone}><i class=${'dot ' + tone}></i>${state}</span></div>
        <p>${tSource(rt.description) || ''}</p>
        <div class="agent-meta">${(compat.protocol || ver) && html`<span title=${compat.tested_version ? t('agents.compat.tested', { v: compat.tested_version }) : ''}>${PROTOCOL[compat.protocol] || (rt.id === 'antigravity' ? t("harnesses.page.pont_loom") : 'ACP')} ${compat.version || ver || ''}</span>`}${accountKnown && html`<span><i class=${'dot ' + (auth.connected ? 'green' : 'amber')}></i>${account}</span>`}${usage && usage.total_tokens ? html`<a href="#/usage">${fmtTok(usage.total_tokens)} tokens · 7 j</a>` : ''}</div></div>
      <div class="acts">
        ${rt.custom && html`<button class="btn ghost" onClick=${() => onEdit(custom)}>${t("harnesses.page.modifier")}</button><button class="icon-btn" aria-label=${t("harnesses.page.supprimer_2")} onClick=${del}><${Icon} n="trash" /></button>`}
        ${accountKnown && !auth.connected && !missing && html`<button class="btn" onClick=${() => setAccountOpen(true)}>${t('agents.account.login')}</button>`}
        ${missing ? '' : !managed ? html`<button class="btn primary" disabled=${busy || !here} onClick=${() => setScope({ managed: true })}>${t('agents.manage')}</button>`
          : !used ? html`<button class="btn primary" disabled=${busy || !here} onClick=${() => setScope({ enabled: true })}>${t('agents.use')}</button>`
          : html`<button class="icon-btn" title=${t("harnesses.page.actualiser")} aria-label=${t("harnesses.page.actualiser")} disabled=${busy} onClick=${reread}><${Icon} n="refresh" /></button>
            <button class="btn primary" disabled=${!choices.length} onClick=${() => startWith(choiceFor(modelOpt && modelOpt.currentValue))}><${Icon} n="plus" />${t("harnesses.page.nouvelle_discussion")}</button>`}</div></div>
    ${!missing && probe && probe.error && html`<div class="alert amber"><${Icon} n="alert" /><span>${probe.error}</span></div>`}
    ${!missing && compat.warning && html`<div class="alert"><${Icon} n="info" /><span>${compat.version && compat.tested_version ? t('agents.compat.drift', { v: compat.version, tested: compat.tested_version }) : t('agents.compat.unverified')}</span></div>`}
    ${accountOpen && html`<${HarnessAccount} rt=${rt} onClose=${() => setAccountOpen(false)} onChanged=${() => loadInfo(true)} onConnect=${reread} />`}

    <${AgentMachines} installs=${installs} onChanged=${() => { loadInstalls(); loadProbe(); }} />
    ${managed && !missing && html`<${AgentDiscussions} rt=${rt} talks=${talks} canList=${canList} installs=${installs} target=${used ? choiceFor(modelOpt && modelOpt.currentValue) : null} />`}

    ${groups.length > 0 && html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.modeles")} <span class="count">${groups.length}</span></h2></div>
      <div class="model-chips">${(allModels ? groups : groups.slice(0, MODEL_LIMIT)).map(g => { const def = g.levels.find(l => l.value === modelOpt.currentValue) || g.levels.find(l => l.level === 'medium') || g.levels[0]; const isDef = g.levels.some(l => l.value === modelOpt.currentValue);
        return html`<button type="button" class=${cls('mchip', isDef && 'on')} disabled=${!used} title=${used ? t("harnesses.page.discuter") : ''} onClick=${() => startWith(choiceFor(def.value))}><b>${g.name}</b>${g.levels.length > 1 ? html`<small>${g.levels.map(l => l.label || l.level).join(' · ')}</small>` : (list.find(x => x.value === def.value) || {}).description ? html`<small>${(list.find(x => x.value === def.value) || {}).description}</small>` : ''}${isDef && html`<em>${t("harnesses.page.par_defaut_2")}</em>`}</button>`; })}</div>${groups.length > MODEL_LIMIT && html`<button class="btn sm ghost more-models" onClick=${() => setAllModels(!allModels)}>${allModels ? t('agents.models.less') : t('agents.models.all', { n: groups.length })}</button>`}</section>`}

    ${managed && !missing && html`<section class="sec"><div class="sec-h"><h2>${t('agents.settings')}</h2></div>
      <div class="card">
        <${ModelSource} rt=${rt} line />
        <div class="set-line"><div class="set-l"><span>${t('agents.resources')}</span><${Tip} text=${t("harnesses.page.loom_garde_la_definition_de_tes_skills_et_serveurs_mcp_tu_choisis") + rt.name + t("harnesses.page.recoit_le_harness_reste_maitre_de_leur_execution")} /></div>
          <div class="set-c"><button class="btn sm ghost" onClick=${() => setResources(true)}>${t('agents.choose')}</button></div></div>
        <div class="set-line"><div class="set-l"><span>${t("harnesses.page.dossier_et_autorisations")}</span><${Tip} text=${t("harnesses.page.choisis_les_par_discussion_dans_le_panneau_de_droite_demander_mod")} /></div><div class="set-c"><span class="muted">${t('agents.per_discussion')}</span></div></div>
      </div></section>`}
    ${resources && html`<${Modal} wide title=${t('agents.resources')} onClose=${() => setResources(false)}><${LoomResources} rt=${rt} /></${Modal}>`}

    <details class="sec more-info"><summary>${t('agents.details')}</summary>
      <div class="card pad details-kv">
        ${info && info.path && html`<div class="kv"><span>${t("harnesses.page.emplacement")}</span><code class="mono">${info.path.replace(/^\/home\/[^/]+/, '~')}</code></div>`}
        <div class="kv"><span>${t("harnesses.page.lancement")}</span><code class="mono">${launch || '—'}</code></div>
        ${info && info.installed !== false && info.env && html`<div class="kv"><span>${t("harnesses.page.cles_d_api_detectees")}</span><span>${info.env.length ? info.env.join(' · ') : t("harnesses.page.aucune_dans_l_environnement")}</span></div>
          <div class="kv"><span>${t("harnesses.page.serveurs_mcp_du_harness")}</span><span>${info.mcp_known ? info.mcp.map(m => m.name).join(' · ') || '0' : t("harnesses.page.non_lus")}</span></div>
          <div class="kv"><span>${t("harnesses.page.plugins")}</span><span>${info.plugins.join(' · ') || '0'}</span></div>
          <div class="kv"><span>${t("harnesses.page.skills_installees")}</span><span>${info.skills.map(k => k.name).join(' · ') || '0'}</span></div>`}
        ${probe && probe.commands && html`<div class="kv"><span>${t("harnesses.page.commandes_disponibles")}</span><span>${probe.commands.length}</span></div>`}
        ${probe && probe.modes && probe.modes.length > 0 && html`<div class="kv"><span>${t("harnesses.page.modes_de_l_agent")}</span><span>${probe.modes.map(m => rt.id === 'antigravity' ? tSource(m.name) || m.id : m.name || m.id).join(' · ')}</span></div>`}
        ${effortOpt && html`<div class="kv"><span>${t("harnesses.page.reflexion")}</span><span>${optValues(effortOpt).map(v => v.name || v.value).join(' · ')}</span></div>`}
        ${rt.docs && html`<div class="kv"><span>${t("harnesses.page.documentation")}</span><a href=${rt.docs} target="_blank" rel="noopener noreferrer">${rt.docs.replace(/^https?:\/\//, '')}</a></div>`}
      </div></details>
  </div>`;
}


// Où une famille d'agents est gérée : une puce par machine, verte si l'agent
// y mène des discussions Loom.
const installationsOf = (rt, all) => all.filter(i => i.managed && (i.installed || i.machine === 'local') && (i.harness === rt.id || i.runtime_id === rt.id));

function Card({ rt, models, installs = [] }) {
  const n = groupVariants(models.filter(m => m.runtime_id === rt.id)).length;
  const supported = rt.implemented && rt.capabilities && rt.capabilities.length > 0;
  const caps = CAPS().filter(([id]) => (rt.capabilities || []).includes(id)).map(([id]) => [id, SHORT()[id]]);
  const acp = isACP(rt), missing = rt.available === false;
  const state = !supported ? null : missing ? ['', t("harnesses.page.non_installe")] : acp ? (installs.some(i => i.enabled) || rt.connected ? ['green', t('agents.state.used')] : ['', t('agents.state.managed')]) : n ? ['green', t("harnesses.page.connecte_3")] : ['', t("harnesses.page.non_connecte")];
  return html`<button type="button" class=${cls('hx', (!supported || missing) && 'is-soon')} onClick=${() => go('harnesses', rt.id)}>
    <div class="hx-top"><${Logo} name=${rt.id} />
      <span class="grow"><b>${rt.name}</b>${rt.machine ? html`<code>${t("harnesses.page.sur_3")} ${rt.machine}</code>` : rt.cli && html`<code>${acp ? 'ACP' + (rt.cli === 'npx' ? '' : ' · ' + rt.cli.split('/').pop()) : rt.cli}</code>`}</span>
      ${!supported ? html`<span class="soon-pill">${t("harnesses.page.bientot_2")}</span>` : html`<span class="state"><i class=${'dot ' + state[0]}></i>${state[1]}</span>`}</div>
    ${rt.description && html`<p>${tSource(rt.description)}</p>`}
    ${supported && installs.length > 0 && html`<div class="hx-inst"><span>${t('agents.installations')}</span>${installs.map(i => html`<span class="hx-chip" key=${i.machine} title=${i.enabled ? t('agents.state.used') : t('agents.managed_only')}><i class=${'dot ' + (i.enabled ? 'green' : '')}></i>${i.machine === 'local' ? t('agents.this_machine') : i.machine_name}${!i.installed && html`<em>${t('agents.to_install')}</em>`}${i.version && html`<em>${i.version.replace(/^v/, '').split(' ')[0]}</em>`}</span>`)}</div>`}
    <div class="hx-foot">${!supported ? (rt.id === 'hermes' ? t("harnesses.page.connecte_sa_machine_avec_connecter_une_machine") : t("harnesses.page.adaptateur_en_preparation")) : missing ? t("harnesses.page.installe") + (rt.cli === 'npx' ? t("harnesses.page.node_js_et_le_cli") : rt.cli) + t("harnesses.page.pour_l_utiliser")
      : acp ? (rt.custom ? t("harnesses.page.personnalise") : '') + t("harnesses.page.dossier_outils_et_autorisations_dans_loom") : n ? n + t("harnesses.page.modele_2") + (n > 1 ? 's' : '') + t("harnesses.page.dans_le_selecteur") : t("harnesses.page.ouvre_pour_connecter_ton_compte")}</div>
  </button>`;
}

export function HarnessesPage({ route }) {
  const ws = useStore(app, s => s.workspace);
  const all = ((ws && ws.runtimes) || []).filter(r => r.kind === 'harness');
  // La carte d'attente « Hermes » disparaît dès qu'un Hermes est branché.
  const runtimes = all.filter(r => !(r.id === 'hermes' && !(r.capabilities || []).length && all.some(x => x.id !== r.id && /(^|-)hermes$/.test(x.id))));
  const models = (ws && ws.models) || [];
  const [selected, setSelected] = useState(null);
  const [dlg, setDlg] = useState(null);
  const [installs, setInstalls] = useState([]);
  const loadInstalls = () => get('/api/agents/installations').then(r => r.ok && setInstalls(r.installations || [])).catch(() => {});
  useEffect(() => { loadInstalls(); }, []);
  useEffect(() => { setSelected(s => s && s.runtime !== route.sub ? null : s); }, [route.sub]);
  // Une carte par famille gérée quelque part ; un agent personnalisé a la sienne.
  const families = runtimes.filter(r => !r.machine && (r.custom || installationsOf(r, installs).length > 0));
  const used = r => r.custom ? !!r.connected : installationsOf(r, installs).some(i => i.enabled);
  const selectedRuntime = runtimes.find(r => r.id === selected?.runtime);
  const selectedModel = models.find(m => m.id === selected?.model);
  const cur = runtimes.find(r => r.id === route.sub);
  const order = r => (r.implemented && r.capabilities && r.capabilities.length ? 0 : 1);
  if (route.sub === 'history') return html`<${HarnessHistory} />`;
  return html`<div class="view page"><div class="page-in wide">
    ${cur ? html`<button class="btn ghost sm back" onClick=${() => go('harnesses')}><${Icon} n="left" />${t('app.groups.agents')}</button>
      <div style="margin-top:14px">${isACP(cur) ? html`<${AgentDetail} key=${cur.id} rt=${cur} models=${models} onEdit=${a => setDlg({ agent: a })} />`
        : html`<${Detail} key=${cur.id} rt=${cur} models=${models} onInspect=${m => setSelected({ runtime: cur.id, model: m.id })} />`}</div>`
    : html`<${SectionTabs} /><div class="page-head"><div><h1>${t('app.groups.agents')}</h1><p>${t("harnesses.page.des_agents_qui_gardent_leurs_outils_leur_compte_et_leurs_permissi")}</p></div>
        <div class="acts"><a class="btn" href="#/machines"><${Icon} n="server" />${t("harnesses.page.machines")}</a><button class="btn primary" onClick=${() => setDlg({ add: true })}><${Icon} n="plus" />${t('agents.add.title')}</button></div></div>
      ${!ws ? html`<div class="skeleton" style="height:220px"></div>` : html`${[true, false].filter(connected => families.some(r => used(r) === connected)).map(connected => html`<section class="sec"><div class="sec-h"><h2>${connected ? t('agents.section.used') : t('agents.section.managed')}</h2></div><div class="hx-grid stagger">${families.filter(r => used(r) === connected).sort((a, b) => order(a) - order(b)).map(r => html`<${Card} key=${r.id} rt=${r} models=${models} installs=${installationsOf(r, installs)} />`)}</div></section>`)}`}
`}
    ${dlg && dlg.add && html`<${AddAgentDialog} installs=${installs} onClose=${() => setDlg(null)} onCustom=${() => setDlg({})} onChanged=${list => list ? setInstalls(list) : loadInstalls()} />`}
    ${dlg && !dlg.add && !dlg.machine && html`<${CustomDialog} agent=${dlg.agent} onClose=${a => { setDlg(null); if (a && !dlg.agent) go('harnesses', a.id); }} />`}
    ${selectedRuntime && html`<${Drawer} title=${selectedModel?.name || selectedRuntime.name} onClose=${() => setSelected(null)}><${SelectionInfo} model=${selectedModel} runtime=${selectedRuntime} models=${models} /></${Drawer}>`}
  </div></div>`;
}
