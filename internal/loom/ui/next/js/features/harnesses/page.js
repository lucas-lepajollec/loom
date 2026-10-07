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
function NativeSessions({ rt }) {
  const [list, setList] = useState(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState('');
  const [q, setQ] = useState('');
  const [project, setProject] = useState('');
  const [fresh, setFresh] = useState(true);
  const projects = useStore(app, s => s.workspace?.projects || []);
  const load = async () => {
    setErr(''); setList(null);
    const r = await get('/api/runtimes/' + rt.id + '/sessions', { timeout: 95000 }).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) { setErr(r.error || t("harnesses.page.lecture_impossible")); setList([]); return; }
    setList((r.sessions || []).sort((a, b) => String(b.updatedAt || '').localeCompare(String(a.updatedAt || ''))));
  };
  useEffect(() => { if (rt.available !== false) load(); }, [rt.id]);
  const importOne = async x => {
    if (x.imported) { openChat(x.imported); go('chat'); return; }
    setBusy(x.sessionId);
    const r = await post('/api/runtimes/' + rt.id + '/sessions/import', { sessionId: x.sessionId, cwd: x.cwd, title: x.title || '', project_id: project, fresh }, { timeout: 190000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t("harnesses.page.import_impossible"), 'err');
    await refreshNav(); openChat(r.session.id); go('chat');
  };
  const shown = (list || []).filter(x => !q || ((x.title || '') + ' ' + x.cwd).toLowerCase().includes(q.toLowerCase()));
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
function ModelSource({ rt }) {
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
// Harness sur une machine connectée : sa version et ses mises à jour, là-bas.
function RemoteState({ rt }) {
  const [localT, setT] = useState(undefined);
  useEffect(() => { remoteHarnessTarget(rt.id).then(setT); }, [rt.id]);
  if (!localT) return null;
  return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.sur")} ${localT.machine.name}</h2></div>
    <div class="card pad"><${Lifecycle} target=${localT.target} id=${localT.id} name=${rt.name} where=${t("harnesses.page.sur_2") + localT.machine.name} /></div></section>`;
}

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

function AcpDetail({ rt, models, onEdit }) {
  const nav = useStore(app, a => a.nav);
  const [probe, setProbe] = useState(null);
  const [busy, setBusy] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const [accountRevision, setAccountRevision] = useState(0);
  const [extra, setExtra] = useState({ usage: [], quota: null, target: null, mcp: [] });
  const [native, setNative] = useState(null);
  // Usage natif (toutes les sessions du harness, 7 jours), lu en arrière-plan.
  useEffect(() => { setNative(null); get('/api/usage/native?days=7').then(r => { const h = (r.harnesses || []).find(x => x.runtime_id === rt.id); setNative(h && !h.error && (h.sessions || h.total_tokens) ? h : null); }).catch(() => {}); }, [rt.id]);
  useVisibleRefresh(async alive => {
    if (accountOpen || rt.connected !== true || rt.available === false || !(rt.capabilities || []).includes('quota')) return;
    await post('/api/runtimes/' + encodeURIComponent(rt.id) + '/quota', {}, { retryAuth: false, timeout: 65000 }).catch(() => null);
    const result = await get('/api/usage', { retryAuth: false });
    if (alive() && result.ok !== false) setExtra(previous => ({ ...previous, quota: (result.quotas || []).find(q => q.runtime_id === rt.id) || null }));
  }, 30000, rt.id + '|' + rt.connected + '|' + accountOpen);
  const [custom, setCustom] = useState(null);
  const load = async () => {
    const [p, u, localT, m] = await Promise.all([get('/api/runtimes/' + rt.id + '/probe').catch(() => ({})), get('/api/usage').catch(() => ({})),
      get('/api/skills/targets').catch(() => ({})), get('/api/mcp').catch(() => ({}))]);
    setProbe((p && p.probe) || {});
    setExtra({ usage: ((u && u.models) || []).filter(x => x.runtime_id === rt.id), quota: ((u && u.quotas) || []).find(q => q.runtime_id === rt.id) || null,
      target: ((localT && localT.targets) || []).find(x => x.harnesses.includes(rt.id)) || null, mcp: Object.keys((m && m.servers) || {}) });
  };
  useEffect(() => { load(); if (rt.custom) get('/api/harness/custom').then(r => setCustom((r.agents || []).find(a => a.id === rt.id) || null)).catch(() => {}); }, [rt.id]);
  const refresh = async () => {
    if (!rt.connected && !await confirm(t('harnesses.connection.connect'), t('harnesses.connection.consent'))) return;
    setBusy(true);
    const r = await post('/api/runtimes/' + rt.id + '/connect', { consent: true }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok) toast(r.error || t("harnesses.page.lecture_impossible"), 'err');
    await refreshWorkspace(); load();
  };
  const disconnect = async () => {
    if (!await confirm(t('harnesses.connection.disconnect'), t('harnesses.connection.disconnect_note'))) return;
    const r = await post('/api/runtimes/' + rt.id + '/disconnect', {});
    if (!r.ok) return toast(r.error, 'err');
    await refreshWorkspace();
  };
  const login = async () => {
    setAccountOpen(true);
  };
  const startWith = async choice => {
    if (!choice) return toast(t("harnesses.page.ce_harness_n_est_pas_encore_disponible"), 'err');
    await newDiscussion(); await chooseRemote(choice);
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
  const choices = models.filter(m => m.runtime_id === rt.id);
  const choiceFor = v => choices.find(c => c.id === rt.id + ':' + v) || choices[0];
  const talks = ((nav && nav.conversations) || []).filter(c => c.runtime_id === rt.id).sort((x, y) => (y.updated_at || 0) - (x.updated_at || 0));
  const turns = extra.usage.reduce((n, u) => n + u.turns, 0);
  const cost = extra.usage.reduce((n, u) => n + (u.reported_cost || 0), 0);
  const currency = (extra.usage.find(u => u.currency) || {}).currency || 'USD';
  const launch = custom ? [custom.command, ...(custom.args || [])].join(' ') : rt.install_hint || rt.cli;
  const ver = probe && probe.agent && probe.agent.version;
  const missing = rt.available === false;
  return html`<div class="h-detail anim-fade">
    <div class="h-head"><div style="display:flex;gap:14px;align-items:center"><${Logo} name=${rt.logo || rt.id} size="lg" />
        <div><h2>${rt.name}${ver && (rt.id === 'antigravity' ? html` <span class="tag" title="${t("harnesses.page.antigravity_est_pilote_par_le_pont_acp_integre_a_loom")}">${t("harnesses.page.pont_loom")}</span>` : html` <span class="tag" title="${t("harnesses.page.version_de_l_adaptateur_acp_utilise_par_loom")}">ACP ${ver}</span>`)}</h2><p>${tSource(rt.description) || ''}</p></div></div>
      <div class="acts">${rt.custom && html`<button class="btn ghost" onClick=${() => onEdit(custom)}>${t("harnesses.page.modifier")}</button><button class="icon-btn" aria-label="${t("harnesses.page.supprimer_2")}" onClick=${del}><${Icon} n="trash" /></button>`}
        <button class="btn" disabled=${busy || missing} onClick=${refresh}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`}${rt.connected ? t("harnesses.page.actualiser") : t("harnesses.connection.connect")}</button>
        <button class="btn primary" disabled=${missing || !choices.length} onClick=${() => startWith(choiceFor(modelOpt && modelOpt.currentValue))}><${Icon} n="plus" />${t("harnesses.page.nouvelle_discussion")}</button></div></div>

    <section class="sec"><div class="card pad"><div class="kv"><span>${t('harnesses.connection.loom')}</span><span>${rt.connected ? t('harnesses.connection.connected') : t('harnesses.connection.disconnected')}</span></div><div class="acts"><button class="btn" onClick=${login}>${t('harnesses.connection.login')}</button>${rt.connected && html`<button class="btn ghost" onClick=${disconnect}>${t('harnesses.connection.disconnect')}</button>`}</div><p class="note">${t('harnesses.connection.account_note')}</p></div></section>
    <div class=${cls('card local-strip' , native && 'five')}>
      <div><div class="lbl">${t("harnesses.page.etat")}</div><div class="v"><i class=${'dot ' + (missing ? '' : probe && probe.error && !cfg.length ? 'red' : 'green')}></i><span class="t">${missing ? t("harnesses.account.unavailable") : probe && probe.error && !cfg.length ? t("harnesses.page.a_verifier") : t("harnesses.page.pret")}</span></div>
        <div class="sub">${probe && probe.at ? t("harnesses.page.lu") + ago(probe.at) : t("harnesses.page.pas_encore_lu")}</div></div>
      <div><div class="lbl">${t("harnesses.page.modeles")}</div><div class="v"><b>${list.length || '—'}</b></div><div class="sub">${modelOpt ? t("harnesses.page.par_defaut") + ((list.find(x => x.value === modelOpt.currentValue) || {}).name || modelOpt.currentValue) : t("harnesses.page.non_annonces")}</div></div>
      <div><div class="lbl">${t("harnesses.page.discussions")}</div><div class="v"><b>${talks.length}</b></div><div class="sub">${turns} ${t("harnesses.page.tour")}${turns > 1 ? 's' : ''} ${t("harnesses.page.dans_loom")}</div></div>
      <div><div class="lbl">${t("harnesses.page.cout_declare")}<${Tip} text="${t("harnesses.page.cout_que_le_harness_declare_lui_meme_pour_les_sessions_ouvertes_p")}" /></div>
        <div class="v"><b>${cost ? Number(cost).toLocaleString(locale(), { style: 'currency', currency, maximumFractionDigits: 2 }) : '—'}</b></div><div class="sub">${extra.quota && extra.quota.windows ? t("harnesses.page.quotas_lisibles_dans_usage") : t("harnesses.page.sessions_loom")}</div></div>
      ${native && html`<div><div class="lbl">${t('harnesses.native.label')}<${Tip} text=${t('harnesses.native.tip')} /></div>
        <div class="v"><b>${fmtTok(native.total_tokens)}</b><span class="t">tokens</span></div><div class="sub"><a href="#/usage">${t('harnesses.native.sessions', { n: native.sessions })}</a></div></div>`}
    </div>
    ${!missing && probe && probe.error && html`<div class="alert amber" style="margin-top:12px"><${Icon} n="alert" /><span>${probe.error}</span></div>`}
    ${accountOpen && html`<${HarnessAccount} rt=${rt} onClose=${() => setAccountOpen(false)} onChanged=${() => setAccountRevision(n => n + 1)} onConnect=${refresh} />`}
    ${!rt.custom && html`<${MachineState} rt=${rt} accountRevision=${accountRevision} commands=${probe && probe.commands ? probe.commands.length : null} />`}
    ${rt.custom && rt.machine && html`<${RemoteState} rt=${rt} />`}
    <${ModelSource} rt=${rt} />
    <${LoomResources} rt=${rt} />

    <section class="sec"><div class="sec-h"><h2>${t("harnesses.page.modeles")} <span class="count">${list.length}</span></h2></div>
      ${list.length ? html`<div class="card rows">${list.map(m => html`<div class="row" key=${m.value}>
          <div class="grow"><div class="t">${m.name || m.value}${modelOpt.currentValue === m.value && html` <span class="tag">${t("harnesses.page.par_defaut_2")}</span>`}</div><div class="s">${m.description || m.value}</div></div>
          <button class="btn sm ghost" disabled=${missing} onClick=${() => startWith(choiceFor(m.value))}>${t("harnesses.page.discuter")}</button></div>`)}</div>`
        : html`<div class="card pad"><p class="note">${missing ? t("harnesses.page.installe_le_cli_pour_lire_ses_modeles") : t("harnesses.page.pas_encore_lus_actualiser_ouvre_une_session_vide_sans_prompt_pour")}</p></div>`}
    </section>

    ${(effortOpt || (probe && probe.modes && probe.modes.length)) && html`<div class="grid2 sec">
      ${effortOpt && html`<div class="card pad"><div class="sec-h"><h2>${t("harnesses.page.reflexion")}</h2></div>
        <div class="chips">${optValues(effortOpt).map(v => html`<span class=${cls('tag', v.value === effortOpt.currentValue && 'on')}>${v.name || v.value}</span>`)}</div>
        <p class="note" style="margin-top:10px">${t("harnesses.page.reglable_par_discussion_dans_le_panneau_de_droite")}</p></div>`}
      ${probe && probe.modes && probe.modes.length > 0 && html`<div class="card pad"><div class="sec-h"><h2>${t("harnesses.page.modes_de_l_agent")}</h2></div>
        ${probe.modes.map(m => html`<div class="kv" key=${m.id}><span>${rt.id === 'antigravity' ? tSource(m.name) || m.id : m.name || m.id}${m.description && html`<${Tip} text=${rt.id === 'antigravity' ? tSource(m.description) : m.description} />`}</span>${m.id === probe.mode ? html`<span class="tag">${t("harnesses.page.par_defaut_2")}</span>` : html`<span></span>`}</div>`)}</div>`}
    </div>`}

    <section class="sec"><div class="sec-h"><h2>${t("harnesses.page.integration_loom")}</h2></div>
      <div class="card">
        <div class="set-line"><div class="set-l"><span>${t("harnesses.page.dossier_et_autorisations")}</span><${Tip} text="${t("harnesses.page.choisis_les_par_discussion_dans_le_panneau_de_droite_demander_mod")}" /></div><div class="set-c"><span class="state">${t("harnesses.page.par_discussion")}</span></div></div>
        <div class="set-line"><div class="set-l"><span>${t("harnesses.page.skills_loom")}</span></div><div class="set-c">${extra.target ? html`<span class="state"><i class=${'dot ' + (extra.target.enabled ? 'green' : '')}></i>${extra.target.enabled ? extra.target.written.length + t("harnesses.page.distribuees") : t("harnesses.page.non_distribuees")}</span><a class="btn sm ghost" href="#/brain">${t("harnesses.page.gerer")}</a>` : html`<span class="muted">${t("harnesses.page.pas_de_dossier_de_skills_connu")}</span>`}</div></div>
        <div class="set-line"><div class="set-l"><span>${t("harnesses.page.serveurs_mcp_loom")}</span></div><div class="set-c">${(rt.capabilities || []).includes('mcp') ? html`<span class="state">${extra.mcp.length ? extra.mcp.length + t("harnesses.page.transmis_a_chaque_session") : t("harnesses.page.aucun_defini")}</span><a class="btn sm ghost" href="#/brain/mcp">${t("harnesses.page.gerer")}</a>` : html`<span class="muted">${t("harnesses.page.non_transmis_machine_distante")}</span>`}</div></div>
        <div class="set-line"><div class="set-l"><span>${t("harnesses.page.lancement")}</span><${Tip} text="${t("harnesses.page.commande_executee_par_loom_l_agent_parle_acp_sur_son_entree_et_sa")}" /></div><div class="set-c"><code class="mono trunc" style="max-width:420px">${launch}</code></div></div>
        <div class="set-line"><div class="set-l"><span>${t("harnesses.page.compte")}</span></div><div class="set-c"><span class="state">${(probe && probe.auth && probe.auth.length) ? probe.auth.map(a => a.name || a.id).join(' · ') : t("harnesses.page.celui_du_cli")}</span></div></div>
        ${rt.docs && html`<div class="set-line"><div class="set-l"><span>${t("harnesses.page.documentation")}</span></div><div class="set-c"><a class="btn sm ghost" href=${rt.docs} target="_blank" rel="noopener noreferrer">${t("harnesses.page.ouvrir")}</a></div></div>`}
      </div></section>

    ${(probe && probe.capabilities && probe.capabilities.sessionCapabilities && probe.capabilities.sessionCapabilities.list) ? html`<${NativeSessions} rt=${rt} />` : ''}

    <section class="sec"><div class="sec-h"><h2>${t("harnesses.page.discussions_recentes_dans_loom")} <span class="count">${talks.length}</span></h2></div>
      ${talks.length ? html`<div class="card rows">${talks.slice(0, 8).map(c => html`<button type="button" class="row link-row" key=${c.id} onClick=${() => { openChat(c.id); go('chat'); }}>
          <div class="grow"><div class="t">${c.title || t("harnesses.page.discussion")}</div><div class="s">${[c.model && c.model !== 'default' ? c.model : '', c.workdir ? c.workdir.split('/').pop() : '', c.updated_at ? ago(c.updated_at) : ''].filter(Boolean).join(' · ')}</div></div>
          <${Icon} n="right" /></button>`)}</div>`
        : html`<div class="card pad"><p class="note">${t("harnesses.page.aucune_discussion_avec")} ${rt.name} ${t("harnesses.page.pour_l_instant")}</p></div>`}
    </section>
  </div>`;
}

// Où une famille d'agents est gérée : une puce par machine, verte si l'agent
// y mène des discussions Loom.
const installationsOf = (rt, all) => all.filter(i => i.managed && i.installed && (i.harness === rt.id || i.runtime_id === rt.id));

function Card({ rt, models, installs = [] }) {
  const n = groupVariants(models.filter(m => m.runtime_id === rt.id)).length;
  const supported = rt.implemented && rt.capabilities && rt.capabilities.length > 0;
  const caps = CAPS().filter(([id]) => (rt.capabilities || []).includes(id)).map(([id]) => [id, SHORT()[id]]);
  const acp = isACP(rt), missing = rt.available === false;
  const state = !supported ? null : missing ? ['', t("harnesses.page.non_installe")] : acp ? (installs.some(i => i.enabled) || rt.connected ? ['green', t('agents.use')] : ['', t('agents.state.managed')]) : n ? ['green', t("harnesses.page.connecte_3")] : ['', t("harnesses.page.non_connecte")];
  return html`<button type="button" class=${cls('hx', (!supported || missing) && 'is-soon')} onClick=${() => go('harnesses', rt.id)}>
    <div class="hx-top"><${Logo} name=${rt.id} />
      <span class="grow"><b>${rt.name}</b>${rt.machine ? html`<code>${t("harnesses.page.sur_3")} ${rt.machine}</code>` : rt.cli && html`<code>${acp ? 'ACP' + (rt.cli === 'npx' ? '' : ' · ' + rt.cli.split('/').pop()) : rt.cli}</code>`}</span>
      ${!supported ? html`<span class="soon-pill">${t("harnesses.page.bientot_2")}</span>` : html`<span class="state"><i class=${'dot ' + state[0]}></i>${state[1]}</span>`}</div>
    ${rt.description && html`<p>${tSource(rt.description)}</p>`}
    ${supported && installs.length > 0 && html`<div class="hx-inst"><span>${t('agents.installations')}</span>${installs.map(i => html`<span class="hx-chip" key=${i.machine} title=${i.enabled ? t('agents.use') : t('agents.managed_only')}><i class=${'dot ' + (i.enabled ? 'green' : '')}></i>${i.machine === 'local' ? t('agents.this_machine') : i.machine_name}${i.version && html`<em>${i.version.replace(/^v/, '').split(' ')[0]}</em>`}</span>`)}</div>`}
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
    ${cur ? html`<button class="btn ghost sm back" onClick=${() => go('harnesses')}><${Icon} n="left" />${t("harnesses.page.harnesses")}</button>
      <div style="margin-top:14px">${isACP(cur) ? html`<${AcpDetail} key=${cur.id} rt=${cur} models=${models} onEdit=${a => setDlg({ agent: a })} />`
        : html`<${Detail} key=${cur.id} rt=${cur} models=${models} onInspect=${m => setSelected({ runtime: cur.id, model: m.id })} />`}</div>`
    : html`<${SectionTabs} /><div class="page-head"><div><h1>${t('app.groups.agents')}</h1><p>${t("harnesses.page.des_agents_qui_gardent_leurs_outils_leur_compte_et_leurs_permissi")}</p></div>
        <div class="acts"><a class="btn" href="#/machines"><${Icon} n="server" />${t("harnesses.page.machines")}</a><button class="btn primary" onClick=${() => setDlg({ add: true })}><${Icon} n="plus" />${t('agents.add.title')}</button></div></div>
      ${!ws ? html`<div class="skeleton" style="height:220px"></div>` : html`${[true, false].map(connected => html`<section class="sec"><div class="sec-h"><h2>${connected ? t('agents.section.used') : t('agents.section.managed')}</h2></div><div class="hx-grid stagger">${families.filter(r => used(r) === connected).sort((a, b) => order(a) - order(b)).map(r => html`<${Card} key=${r.id} rt=${r} models=${models} installs=${installationsOf(r, installs)} />`)}</div></section>`)}`}
`}
    ${dlg && dlg.add && html`<${AddAgentDialog} installs=${installs} onClose=${() => setDlg(null)} onCustom=${() => setDlg({})} onChanged=${list => list ? setInstalls(list) : loadInstalls()} />`}
    ${dlg && !dlg.add && !dlg.machine && html`<${CustomDialog} agent=${dlg.agent} onClose=${a => { setDlg(null); if (a && !dlg.agent) go('harnesses', a.id); }} />`}
    ${selectedRuntime && html`<${Drawer} title=${selectedModel?.name || selectedRuntime.name} onClose=${() => setSelected(null)}><${SelectionInfo} model=${selectedModel} runtime=${selectedRuntime} models=${models} /></${Drawer}>`}
  </div></div>`;
}
