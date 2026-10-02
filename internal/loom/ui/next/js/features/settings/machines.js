import { t } from '../../core/i18n.js';
// Réglages › Machines : l'endroit unique pour connecter une machine et gérer ce
// qui s'y trouve. Cette machine et chaque machine connectée en SSH ont leur
// page : moteur, harnesses (installer, mettre à jour, ajouter à Loom),
// dossiers de travail favoris, dossiers de modèles, terminaux.
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { Empty, Tip } from '../../ui/controls.js';
import { confirm, prompt, toast } from '../../ui/dialog.js';
import { FolderPicker } from '../../ui/folder.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshWorkspace, refreshEngineNode, refreshStatus, refreshLibrary } from '../../core/state.js';
import { MachineDialog } from '../harnesses/machines.js';
import { Lifecycle } from '../harnesses/lifecycle.js';
import { openTerminalWith } from '../terminals/page.js';
import { Line, Group } from './kit.js';
import { ModelDirs, DirectEngineForm } from './page.js';

const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');
const NAMES = { hermes: 'Hermes', 'claude-code': 'Claude Code', codex: 'Codex', pi: 'Pi', gemini: 'Gemini', opencode: 'OpenCode', antigravity: 'Antigravity' };
const LOCAL_HARNESSES = ['claude-code', 'codex', 'gemini', 'pi', 'opencode', 'hermes', 'antigravity'];

// Lien vers cette page depuis les endroits qui en ont besoin.
export const MachinesLink = ({ label }) => html`<a class="btn sm ghost" href="#/settings/machines"><${Icon} n="server" />${label || t("settings.machines.gerer_les_machines")}</a>`;

export function MachinesSettings({ route }) {
  const [data, setData] = useState(null);
  const [local, setLocal] = useState(null);
  const [dlg, setDlg] = useState(null);
  const load = () => Promise.all([
    get('/api/machines').then(r => setData(r.ok ? r : { machines: [], offers: {} })).catch(() => setData({ machines: [], offers: {} })),
    get('/api/machines/local').then(setLocal).catch(() => setLocal(null)),
  ]);
  useEffect(() => { load(); }, []);
  const id = route && route.id;
  if (!data) return html`<div class="skeleton" style="height:240px"></div>`;
  if (id) {
    const m = id === 'local' ? null : data.machines.find(x => x.id === id);
    if (id !== 'local' && !m) return html`<${Empty} icon="server" title="${t("settings.machines.machine_introuvable")}" />`;
    return html`<${MachineDetail} m=${m} local=${local} offers=${m ? (data.offers || {})[m.id] || [] : null} onChange=${load} onEdit=${() => setDlg({ machine: m })} />
      ${dlg && html`<${MachineDialog} machine=${dlg.machine} onClose=${x => { setDlg(null); if (x) load(); }} />`}`;
  }
  return html`
    <${Group} title="${t("settings.machines.machines")}">
      <div class="set-note">${t("settings.machines.loom_travaille_sur_cette_machine_et_sur_les_machines_que_tu_conne")}</div>
      <a class="mc-row" href="#/settings/machines/local"><span class="mx-ico"><${Icon} n="chip" /></span>
        <span class="grow"><b>${(local && local.hostname) || t("settings.machines.cette_machine_2")}</b><small>${t("settings.machines.cette_machine")}${local ? ' · ' + local.os : ''}</small></span><${Icon} n="right" /></a>
      ${data.machines.map(m => html`<a class="mc-row" key=${m.id} href=${'#/settings/machines/' + encodeURIComponent(m.id)}><span class="mx-ico"><${Icon} n="server" /></span>
        <span class="grow"><b>${m.name}</b><small class="mono">${m.user}@${m.host}${m.port !== 22 ? ':' + m.port : ''}${m.os ? ' · ' + m.os : ''}</small></span>
        <span class="mc-hs">${(m.harnesses || []).map(h => html`<${Logo} key=${h} name=${h} size="sm" />`)}</span><${Icon} n="right" /></a>`)}
      <div class="set-actions"><button class="btn" onClick=${() => setDlg({})}><${Icon} n="plus" />${t("settings.machines.connecter_une_machine")}</button></div>
    </${Group}>
    ${dlg && html`<${MachineDialog} machine=${null} onClose=${x => { setDlg(null); if (x) { load(); go('settings', 'machines', x.id); } }} />`}`;
}

function MachineDetail({ m, local, offers, onChange, onEdit }) {
  const isLocal = !m;
  const target = isLocal ? 'local' : m.id;
  const name = isLocal ? (local && local.hostname) || t("settings.machines.cette_machine_2") : m.name;
  const where = isLocal ? t("settings.machines.sur_cette_machine") : t("settings.machines.sur") + m.name;
  const remove = async () => {
    if (!await confirm(t("settings.machines.retirer") + m.name, t("settings.machines.ses_harnesses_disparaissent_de_loom_les_discussions_deja_faites_r"), { ok: t("settings.machines.retirer_2"), danger: true })) return;
    const r = await post('/api/machines/delete', { id: m.id });
    if (!r.ok) return toast(r.error || t("settings.machines.suppression_impossible"), 'err');
    await refreshWorkspace(); go('settings', 'machines');
  };
  return html`
    <div class="mc-head anim-rise"><a class="btn sm ghost" href="#/settings/machines"><${Icon} n="left" />${t("settings.machines.machines")}</a>
      <div class="mc-title"><span class="mx-ico"><${Icon} n=${isLocal ? 'chip' : 'server'} /></span><div><h2>${name}</h2>
        <p class="mono">${isLocal ? (local ? local.user + ' · ' + home(local.home) + ' · ' + local.os : '') : m.user + '@' + m.host + (m.port !== 22 ? ':' + m.port : '') + (m.os ? ' · ' + m.os : '') + (m.home ? ' · ' + m.home : '')}</p></div>
        <span class="grow"></span>
        <button class="btn sm" onClick=${() => openTerminalWith({ target, dir: isLocal ? '' : m.home || '', title: name })}><${Icon} n="prompt" />${t("settings.machines.terminal")}</button>
        ${!isLocal && html`<button class="btn sm ghost" onClick=${onEdit}>${t("settings.machines.modifier")}</button><button class="icon-btn" aria-label=${t("settings.machines.retirer") + m.name} onClick=${remove}><${Icon} n="trash" /></button>`}</div></div>
    <${EngineSection} m=${m} />
    <${HarnessesSection} m=${m} target=${target} where=${where} offers=${offers} onChange=${onChange} />
    <${FoldersSection} target=${target} m=${m} />
    <${TerminalsSection} target=${target} name=${name} m=${m} />`;
}

// Moteur : sur cette machine, ou le Loom d'une machine distante lié comme moteur.
function EngineSection({ m }) {
  const node = useStore(app, a => a.engineNode);
  const [form, setForm] = useState(null);
  const [direct, setDirect] = useState(false);
  const [busy, setBusy] = useState(false);
  const isLocal = !m;
  const sameHost = n => { try { return n && m && n.hostname === m.host || new URL(n.url).hostname === m.host; } catch (_) { return false; } };
  const owns = isLocal ? !node : sameHost(node);
  const link = async () => {
    setBusy(true);
    const r = await post('/api/engine/node', { url: form.url, key: form.key });
    setBusy(false);
    if (!r.ok) return toast(r.error || t("settings.machines.liaison_impossible"), 'err');
    setForm(null); toast(t("settings.machines.moteur_de") + r.hostname + t("settings.machines.utilise")); await refreshEngineNode(); refreshStatus(); refreshLibrary();
  };
  const unlink = async () => { await post('/api/engine/node', { unlink: true }); await refreshEngineNode(); refreshStatus(); refreshLibrary(); };
  return html`<${Group} title="${t("settings.machines.moteur")}">
    ${isLocal ? html`<${Line} label="${t("settings.machines.moteur_de_loom")}" tip="${t("settings.machines.le_moteur_llama_cpp_qui_sert_les_modeles_locaux_dans_tes_discussi")}">
        ${owns ? html`<span class="state"><i class="dot green"></i>${t("settings.machines.sur_cette_machine")}</span><a class="btn sm ghost" href="#/settings/engine">${t("settings.machines.reglages_du_moteur")}</a>`
          : html`<span class="state">${t("settings.machines.celui_de")} ${node.hostname}</span><button class="btn sm ghost" onClick=${unlink}>${t("settings.machines.utiliser_celui_de_cette_machine")}</button>`}</${Line}>`
      : owns ? html`<${Line} label="${t("settings.machines.moteur_de_loom")}"><span class="state"><i class=${'dot ' + (node.reachable ? 'green' : 'red')}></i>${node.direct ? t("settings.machines.serveur") + node.kind + t("settings.machines.utilise_par_loom") : t("settings.machines.moteur_utilise_par_loom")}</span><a class="btn sm ghost" href="#/settings/engine">${t("settings.machines.reglages_du_moteur")}</a><button class="btn sm ghost" onClick=${unlink}>${t("settings.machines.ne_plus_l_utiliser")}</button></${Line}>`
      : html`<${Line} label="${t("settings.machines.moteur_de_loom")}" tip="${t("settings.machines.si_cette_machine_a_une_carte_graphique_et_loom_installe_loom_peut")}">
          ${!form && !direct && html`<button class="btn sm" onClick=${() => setDirect(true)}>${t("settings.machines.lier_son_serveur_llama_cpp_vllm")}</button><button class="btn sm ghost" onClick=${() => setForm({ url: 'http://' + m.host + ':8091', key: '' })}>${t("settings.machines.utiliser_le_loom_de_cette_machine")}</button>`}</${Line}>
        ${direct && html`<${DirectEngineForm} start=${'http://' + m.host + ':8080'} onDone=${() => setDirect(false)} />`}
        ${form && html`<div class="eng-link">
          <p class="note">${t("settings.machines.sur_2")} ${m.name} ${t("settings.machines.loom_reglages_acces_reseau_active_interface_sur_le_reseau_et_api")}</p>
          <label class="field"><span>${t("settings.machines.adresse_du_loom_de")} ${m.name}</span><input class="input mono" value=${form.url} onInput=${e => setForm({ ...form, url: e.target.value })} /></label>
          <label class="field"><span>${t("settings.machines.sa_cle_de_pilotage")}</span><input class="input mono" type="password" placeholder="loom-web-…" value=${form.key} onInput=${e => setForm({ ...form, key: e.target.value })} /></label>
          <div class="form-foot"><span class="grow"></span><button class="btn ghost" onClick=${() => setForm(null)}>${t("settings.machines.annuler")}</button><button class="btn primary" disabled=${busy || !form.url || !form.key} onClick=${link}>${busy ? t("settings.machines.verification") : t("settings.machines.utiliser_ce_moteur")}</button></div>
        </div>`}`}
  </${Group}>
  ${owns && !(node && node.direct) && html`<${ModelDirs} />`}`;
}

function HarnessesSection({ m, target, where, offers, onChange }) {
  const [busy, setBusy] = useState('');
  const ws = useStore(app, a => a.workspace);
  const isLocal = !m;
  const runtimes = ((ws && ws.runtimes) || []);
  // Sur cette machine : l'état réel de chaque harness du catalogue.
  const [states, setStates] = useState(null);
  useEffect(() => {
    if (!isLocal) return;
    Promise.all(LOCAL_HARNESSES.map(id => get('/api/harness/lifecycle?target=local&id=' + id).then(r => [id, r.state]).catch(() => [id, null])))
      .then(list => setStates(Object.fromEntries(list)));
  }, [isLocal]);
  if (isLocal && !states) return html`<${Group} title="${t("settings.machines.harnesses")}"><div class="set-note"><span class="spinner"></span> ${t("settings.machines.lecture_des_harnesses_de_cette_machine")}</div></${Group}>`;
  const list = isLocal ? LOCAL_HARNESSES.filter(id => states[id]).map(id => ({ id, name: NAMES[id] || id, logo: id }))
    : (offers || []).map(o => ({ ...o }));
  const added = id => isLocal || (m.harnesses || []).includes('custom-' + m.id + '-' + id);
  const add = async o => {
    setBusy(o.id);
    const ids = (m.harnesses || []).map(h => h.slice(('custom-' + m.id + '-').length));
    const r = await post('/api/machines', { machine: { id: m.id, name: m.name, host: m.host, user: m.user, port: m.port }, harnesses: [...new Set([...ids, o.id])] }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t("settings.machines.ajout_impossible"), 'err');
    toast(o.name + t("settings.machines.ajoute_a_loom")); await refreshWorkspace(); onChange();
  };
  const runtime = id => runtimes.find(r => r.id === id) || {};
  const installed = o => isLocal ? !!(states[o.id] && states[o.id].installed) : o.installed;
  const card = o => html`<div class="mh-item card pad" key=${o.id}>
      <div class="mh-head"><${Logo} name=${o.logo || o.id} /><b>${o.name}</b><span class="grow"></span>
        ${isLocal ? (runtime(o.id).id ? html`<a class="btn sm ghost" href=${'#/harnesses/' + o.id}>${t("settings.machines.page_du_harness")}</a>` : html`<span class="muted">${t("settings.machines.pas_encore_pilote_par_loom")}</span>`)
          : added(o.id) ? html`<span class="state"><i class="dot green"></i>${t("settings.machines.dans_loom")}</span><a class="btn sm ghost" href=${'#/harnesses/custom-' + m.id + '-' + o.id}>${t("settings.machines.page_du_harness")}</a>`
          : o.ready ? html`<button class="btn sm" disabled=${busy === o.id} onClick=${() => add(o)}>${t("settings.machines.ajouter_a_loom")}</button>`
          : o.missing ? html`<span class="state err">${o.missing}</span>` : ''}</div>
      <${Lifecycle} target=${target} id=${o.id} name=${o.name} where=${where} onChange=${onChange} />
    </div>`;
  const absent = list.filter(o => !installed(o));
  return html`<section class="set-group anim-rise"><h3>${t("settings.machines.harnesses")}<${Tip} text=${t("settings.machines.version_installee_et_derniere_publiee_installation_et_mises_a_jou") + where + t("settings.machines.un_harness_ajoute_a_loom_apparait_dans_le_selecteur_de_modeles")} /></h3>
    <div class="mh-list">${list.filter(installed).map(card)}
      ${absent.length > 0 && html`<div class="card mh-absent"><div class="mh-absent-h">${t("settings.machines.a_installer")} ${where}</div>
        ${absent.map(o => html`<div class="mh-row" key=${o.id}><${Logo} name=${o.logo || o.id} size="sm" /><span class="grow">${o.name}</span>
          <${Lifecycle} compact target=${target} id=${o.id} name=${o.name} where=${where} onChange=${onChange} /></div>`)}</div>`}
    </div></section>`;
}

// Dossiers de travail favoris : proposés pour un harness, un terminal…
function FoldersSection({ target, m }) {
  const [folders, setFolders] = useState(null);
  const [pick, setPick] = useState(false);
  useEffect(() => { get('/api/machines/folders?machine=' + encodeURIComponent(target)).then(r => setFolders(r.folders || [])).catch(() => setFolders([])); }, [target]);
  const save = async next => {
    const r = await post('/api/machines/folders', { machine: target, folders: next });
    if (!r.ok) return toast(r.error || t("settings.machines.enregistrement_impossible"), 'err');
    setFolders(r.folders);
  };
  const add = async () => {
    if (!m) return setPick(true);
    const p = await prompt(t("settings.machines.dossier_sur") + m.name, { placeholder: (m.home || '/home/moi') + t("settings.machines.projet"), ok: t("settings.machines.ajouter") });
    if (p) save([...(folders || []), p]);
  };
  return html`<${Group} title="${t("settings.machines.dossiers_de_travail")}">
    <div class="set-note">${t("settings.machines.proposes_quand_tu_choisis_ou_travaille_un_harness_ou_ou_s_ouvre_u")}${m ? t("settings.machines.sur_3") + m.name : ''}.</div>
    ${(folders || []).map(f => html`<div class="set-line" key=${f}><div class="set-l"><${Icon} n="folder" /><span class="mono path">${home(f)}</span></div>
      <div class="set-c"><button class="btn sm ghost" onClick=${() => openTerminalWith({ target, dir: f })}><${Icon} n="prompt" />${t("settings.machines.terminal")}</button>
        <button class="icon-btn" aria-label="${t("settings.machines.retirer_2")}" onClick=${() => save(folders.filter(x => x !== f))}><${Icon} n="close" /></button></div></div>`)}
    <div class="set-actions"><button class="btn ghost" onClick=${add}><${Icon} n="plus" />${t("settings.machines.ajouter_un_dossier")}</button></div>
    ${pick && html`<${FolderPicker} start="" onClose=${() => setPick(false)} onPick=${d => { setPick(false); save([...(folders || []), d]); }} />`}
  </${Group}>`;
}

function TerminalsSection({ target, name, m }) {
  const [list, setList] = useState(null);
  useEffect(() => { get('/api/terminals').then(r => setList((r.terminals || []).filter(localT => localT.target === target))).catch(() => setList([])); }, [target]);
  return html`<${Group} title="${t("settings.machines.terminaux")}">
    ${list && list.length ? list.map(localT => html`<a class="set-line link-line" key=${localT.id} href=${'#/terminals/' + localT.id}><div class="set-l"><i class=${'dot ' + (localT.running ? 'green' : '')}></i><span>${localT.title}</span><span class="muted mono">${home(localT.dir)}</span></div><div class="set-c"><${Icon} n="right" /></div></a>`)
      : html`<div class="set-note">${t("settings.machines.aucun_terminal_ouvert")} ${m ? t("settings.machines.sur") + name : t("settings.machines.sur_cette_machine")}.</div>`}
    <div class="set-actions"><button class="btn ghost" onClick=${() => openTerminalWith({ target, dir: m ? m.home || '' : '', title: name })}><${Icon} n="prompt" />${t("settings.machines.nouveau_terminal")}</button></div>
  </${Group}>`;
}
