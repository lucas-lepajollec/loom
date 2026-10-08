import { t } from '../../core/i18n.js';
// Réglages › Machines : l'endroit unique pour connecter une machine et gérer ce
// qui s'y trouve. Cette machine et chaque machine connectée en SSH ont leur
// page : moteur, harnesses (installer, mettre à jour, ajouter à Loom),
// dossiers de travail favoris, dossiers de modèles, terminaux.
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { Empty, Tip, Switch } from '../../ui/controls.js';
import { Modal, confirm, prompt, toast } from '../../ui/dialog.js';
import { FolderPicker } from '../../ui/folder.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshWorkspace, refreshEngineNode, refreshStatus, refreshLibrary } from '../../core/state.js';
import { MachineDialog } from '../harnesses/machines.js';
import { Lifecycle } from '../harnesses/lifecycle.js';
import { openTerminalWith } from '../terminals/page.js';
import { Line, Group } from './kit.js';
import { LoomUpdates } from './updates.js';
import { ModelDirs, DirectEngineForm } from './page.js';

const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');
const NAMES = { hermes: 'Hermes', 'claude-code': 'Claude Code', codex: 'Codex', pi: 'Pi', opencode: 'OpenCode', antigravity: 'Antigravity' };
const LOCAL_HARNESSES = ['claude-code', 'codex', 'pi', 'opencode', 'hermes', 'antigravity'];

// Lien vers cette page depuis les endroits qui en ont besoin.
export const MachinesLink = ({ label }) => html`<a class="btn sm ghost" href="#/machines"><${Icon} n="server" />${label || t("settings.machines.gerer_les_machines")}</a>`;

// Appairage : sur la machine, `loom node pair` affiche un code à usage unique ;
// ici on donne son adresse (ou on la choisit parmi celles trouvées) et le code.
function PairDialog({ start, onClose }) {
  const [v, setV] = useState({ address: (start && start.address) || '', code: '' });
  const [busy, setBusy] = useState(false);
  const fmt = c => { const x = c.toUpperCase().replace(/[^0-9A-Z]/g, '').slice(0, 8); return x.length > 4 ? x.slice(0, 4) + '-' + x.slice(4) : x; };
  const pair = async force => {
    setBusy(true);
    const r = await post('/api/machines/pair', { address: v.address.trim(), code: v.code.replace('-', ''), ...(force ? { force: true } : {}) }, { timeout: 30000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok && r.error_code === 'already_paired' && !force) {
      if (await confirm(t('machines.pair.already_title'), r.error + '. ' + t('machines.pair.already_text'), { ok: t('machines.pair.already_ok') })) return pair(true);
      return;
    }
    if (!r.ok) return toast(r.error || t('machines.pair.failed'), 'err');
    toast(t('machines.pair.done', { name: (r.machine && r.machine.name) || v.address })); await refreshEngineNode(); refreshWorkspace(); onClose(r.machine || true);
  };
  return html`<${Modal} title=${start && start.name ? t('machines.pair.title_named', { name: start.name }) : t('machines.pair.title')} sub=${t('machines.pair.sub')} onClose=${busy ? undefined : () => onClose()}
      foot=${html`<button class="btn ghost" disabled=${busy} onClick=${() => onClose()}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || !v.address.trim() || v.code.replace('-', '').length !== 8} onClick=${() => pair(false)}>${busy ? t('settings.machines.verification') : t('machines.pair.ok')}</button>`}>
    <div class="ws-form">
      <div class="pair-how"><span class="mono">loom node pair</span><span>${t('machines.pair.how')}</span></div>
      <label class="field"><span>${t('machines.pair.address')}</span><input class="input mono" value=${v.address} placeholder="192.168.1.20:2511" onInput=${e => setV({ ...v, address: e.target.value })} /></label>
      <label class="field"><span>${t('machines.pair.code')}</span><input class="input mono pair-code" value=${v.code} placeholder="K7QM-4XPA" autocomplete="one-time-code" onInput=${e => setV({ ...v, code: fmt(e.target.value) })} /></label>
      <p class="note">${t('machines.pair.lan_note')}</p>
    </div></${Modal}>`;
}

// Les nœuds Loom du réseau local qui répondent à la recherche (sans secret).
function Discovered({ onPair }) {
  const [nodes, setNodes] = useState(null);
  const [busy, setBusy] = useState(false);
  const scan = async () => { setBusy(true); const r = await get('/api/machines/discover', { timeout: 8000 }).catch(() => null); setBusy(false); setNodes(r && r.nodes ? r.nodes : []); };
  useEffect(() => { scan(); }, []);
  if (nodes === null || (!nodes.length && !busy)) return html`<div class="disc-empty"><button class="btn sm ghost" disabled=${busy} onClick=${scan}><${Icon} n="search" />${busy ? t('machines.disc.scanning') : t('machines.disc.scan')}</button></div>`;
  return html`<section class="sec"><div class="sec-h"><h2>${t('machines.disc.title')}<${Tip} text=${t('machines.disc.tip')} /></h2><span class="grow"></span>
      <button class="icon-btn" disabled=${busy} aria-label=${t('machines.disc.scan')} onClick=${scan}><${Icon} n="refresh" /></button></div>
    <div class="card bs-list">${nodes.map(n => html`<div class="bs-row" key=${n.id}>
      <span class="mono-tile"><${Icon} n="server" /></span>
      <div class="grow"><div class="bs-name">${n.name}${n.paired && html`<span class="tag">${t('machines.disc.paired_elsewhere')}</span>`}</div><div class="bs-sub"><span class="mono">${n.address}${n.port ? ':' + n.port : ''}</span><span>Loom ${n.version}</span></div></div>
      <button class="btn sm" onClick=${() => onPair({ address: n.address + (n.port ? ':' + n.port : ''), name: n.name })}>${t('machines.pair.ok')}</button></div>`)}</div>
  </section>`;
}

// SSH machines show user@host; machines paired by code only have their node.
const where = m => (m.user ? m.user + '@' + m.host + (m.port && m.port !== 22 ? ':' + m.port : '') : m.host + ' · ' + t('machines.node_label')) + (m.os ? ' · ' + m.os : '');

// Jauges d'une machine : processeur, mémoire, carte graphique, disque.
const pct = (u, t) => t ? Math.min(100, Math.round(u * 100 / t)) : 0;
const gb = n => (n / 1073741824).toFixed(n >= 107374182400 ? 0 : 1);
function Meters({ m }) {
  if (!m) return html`<div class="mmeters muted">…</div>`;
  if (m.error || m.supported === false) return html`<div class="mmeters"><span class="muted mmeter-none" title=${m.error || ''}>${t('machines.metrics.none')}</span></div>`;
  const gpu = (m.gpus || [])[0];
  const row = (label, value, sub, title) => html`<div class="mmeter" title=${title || ''}><span>${label}</span><i class="mmeter-bar"><b style=${`width:${value}%`} class=${value >= 90 ? 'hot' : value >= 70 ? 'warm' : ''}></b></i><em>${sub}</em></div>`;
  return html`<div class="mmeters">
    ${row(t('machines.metrics.cpu'), Math.round(m.cpu || 0), Math.round(m.cpu || 0) + ' %', m.cores ? t('machines.metrics.cores', { n: m.cores }) : '')}
    ${row(t('machines.metrics.ram'), pct(m.ram_used, m.ram_total), gb(m.ram_used) + ' / ' + gb(m.ram_total) + t('common.units.gb'))}
    ${gpu && row(t('machines.metrics.gpu'), pct(gpu.vram_used, gpu.vram_total), gb(gpu.vram_used) + ' / ' + gb(gpu.vram_total) + t('common.units.gb'), gpu.name + ' · ' + Math.round(gpu.util || 0) + ' %')}
    ${m.disk && m.disk.total ? row(t('machines.metrics.disk'), pct(m.disk.used, m.disk.total), gb(m.disk.used) + ' / ' + gb(m.disk.total) + t('common.units.gb'), m.disk.path) : ''}
  </div>`;
}

export function MachinesSettings({ route }) {
  const [data, setData] = useState(null);
  const [local, setLocal] = useState(null);
  const [dlg, setDlg] = useState(null);
  const [pair, setPair] = useState(null);
  const [metrics, setMetrics] = useState({});
  useEffect(() => {
    let alive = true;
    const tick = () => get('/api/machines/metrics', { timeout: 9000 }).then(r => { if (alive && r && r.metrics) setMetrics(r.metrics); }).catch(() => {});
    tick(); const id = setInterval(tick, 10000);
    return () => { alive = false; clearInterval(id); };
  }, []);
  const load = () => Promise.all([
    get('/api/machines').then(r => setData(r.ok ? r : { machines: [], offers: {} })).catch(() => setData({ machines: [], offers: {} })),
    get('/api/machines/local').then(setLocal).catch(() => setLocal(null)),
  ]);
  const [installs, setInstalls] = useState([]);
  const [terminals, setTerminals] = useState([]);
  const node = useStore(app, x => x.engineNode);
  // Le moteur tourne sur cette machine, sauf s'il est lié ailleurs.
  const engineAt = node && node.machine_id ? node.machine_id : node && (node.direct || node.remote) ? '' : 'local';
  useEffect(() => { load(); get('/api/agents/installations').then(r => setInstalls(r.installations || [])).catch(() => {}); get('/api/terminals').then(r => setTerminals(r.terminals || [])).catch(() => {}); }, []);
  const id = route && route.id;
  if (!data) return html`<div class="skeleton" style="height:240px"></div>`;
  if (id) {
    const m = id === 'local' ? null : data.machines.find(x => x.id === id);
    if (id !== 'local' && !m) return html`<${Empty} icon="server" title="${t("settings.machines.machine_introuvable")}" />`;
    return html`<${MachineDetail} m=${m} local=${local} offers=${m ? (data.offers || {})[m.id] || [] : null} onChange=${load} onEdit=${() => setDlg({ machine: m })} />
      ${dlg && html`<${MachineDialog} machine=${dlg.machine} onClose=${x => { setDlg(null); if (x) load(); }} />`}`;
  }
  // Une carte par machine : ce qui y tourne pour Loom, d'un coup d'œil.
  const card = (id, name, sub, icon) => {
    const mine = installs.filter(i => i.machine === id && i.installed);
    const used = mine.filter(i => i.enabled).length, managed = mine.filter(i => i.managed).length;
    const terms = terminals.filter(x => x.target === id).length;
    return html`<a class="mcard" key=${id} href=${'#/machines/' + encodeURIComponent(id)}>
      <div class="mcard-h"><span class="mx-ico"><${Icon} n=${icon} /></span><span class="grow"><b>${name}</b><small class="mono">${sub}</small></span><${Icon} n="right" /></div>
      <${Meters} m=${metrics[id]} />
      <div class="mcard-s">
        <div><span>${t('app.groups.agents')}</span><b>${mine.length ? t('machines.card.agents', { used, managed }) : '—'}</b></div>
        <div><span>${t('engine.page.title')}</span><b>${id === engineAt ? t('machines.card.engine_here') : '—'}</b></div>
        <div><span>${t('app.routes.terminaux')}</span><b>${terms || '—'}</b></div>
      </div>
      ${mine.length > 0 && html`<div class="mcard-a">${mine.map(i => html`<span class=${'mcard-chip' + (i.enabled ? ' on' : '')} key=${i.harness} title=${i.name}><${Logo} name=${i.logo || i.harness} size="sm" /></span>`)}</div>`}
    </a>`;
  };
  return html`
    <div class="mcards">
      ${card('local', (local && local.hostname) || t("settings.machines.cette_machine_2"), t("settings.machines.cette_machine") + (local ? ' · ' + local.os : ''), 'chip')}
      ${data.machines.map(m => card(m.id, m.name, where(m), 'server'))}
      <button type="button" class="mcard add" onClick=${() => setPair({})}><${Icon} n="key" /><span>${t('machines.pair.card')}</span><small>${t('machines.pair.card_note')}</small></button>
      <button type="button" class="mcard add" onClick=${() => setDlg({})}><${Icon} n="plus" /><span>${t("settings.machines.connecter_une_machine")}</span><small>${t('machines.ssh.card_note')}</small></button>
    </div>
    <${Discovered} onPair=${setPair} />
    ${pair && html`<${PairDialog} start=${pair} onClose=${x => { setPair(null); if (x) { load(); if (x.id) go('machines', x.id); } }} />`}
    ${dlg && html`<${MachineDialog} machine=${null} onClose=${x => { setDlg(null); if (x) { load(); go('machines', x.id); } }} />`}`;
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
    await refreshWorkspace(); go('machines');
  };
  return html`
    <div class="mc-head anim-rise"><a class="btn sm ghost" href="#/machines"><${Icon} n="left" />${t("settings.machines.machines")}</a>
      <div class="mc-title"><span class="mx-ico"><${Icon} n=${isLocal ? 'chip' : 'server'} /></span><div><h2>${name}</h2>
        <p class="mono">${isLocal ? (local ? local.user + ' · ' + home(local.home) + ' · ' + local.os : '') : where(m) + (m.home ? ' · ' + m.home : '')}</p></div>
        <span class="grow"></span>
        ${(isLocal || m.user) && html`<button class="btn sm" onClick=${() => openTerminalWith({ target, dir: isLocal ? '' : m.home || '', title: name })}><${Icon} n="prompt" />${t("settings.machines.terminal")}</button>`}
        ${!isLocal && html`<button class="btn sm ghost" onClick=${onEdit}>${t("settings.machines.modifier")}</button><button class="icon-btn" aria-label=${t("settings.machines.retirer") + m.name} onClick=${remove}><${Icon} n="trash" /></button>`}</div></div>
    <${HarnessesSection} m=${m} onChange=${onChange} />
    <${EngineSection} m=${m} />
    <${FoldersSection} target=${target} m=${m} />
    <${TerminalsSection} target=${target} name=${name} m=${m} />`;
}

// Moteur : sur cette machine, ou le Loom d'une machine distante lié comme moteur.
function EngineSection({ m }) {
  const node = useStore(app, a => a.engineNode);
  const [direct, setDirect] = useState(false);
  const [pairing, setPairing] = useState(false);
  const [busy, setBusy] = useState(false);
  const isLocal = !m;
  const sameHost = n => { try { return n && m && n.hostname === m.host || new URL(n.url).hostname === m.host; } catch (_) { return false; } };
  const owns = isLocal ? !node : sameHost(node);
  const paired = m && !m.user;
  // A paired machine's credential is already known: no key to copy.
  const useEngine = async () => {
    setBusy(true);
    const r = await post('/api/engine/node', { machine: m.id }, { timeout: 30000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok) return toast(r.error || t("settings.machines.liaison_impossible"), 'err');
    toast(t("settings.machines.moteur_de") + r.hostname + t("settings.machines.utilise")); await refreshEngineNode(); refreshStatus(); refreshLibrary();
  };
  const unlink = async () => { await post('/api/engine/node', { unlink: true }); await refreshEngineNode(); refreshStatus(); refreshLibrary(); };
  return html`<${Group} title="${t("settings.machines.moteur")}">
    ${isLocal ? html`<${Line} label="${t("settings.machines.moteur_de_loom")}" tip="${t("settings.machines.le_moteur_llama_cpp_qui_sert_les_modeles_locaux_dans_tes_discussi")}">
        ${owns ? html`<span class="state"><i class="dot green"></i>${t("settings.machines.sur_cette_machine")}</span><a class="btn sm ghost" href="#/engine">${t("settings.machines.reglages_du_moteur")}</a>`
          : html`<span class="state">${t("settings.machines.celui_de")} ${node.hostname}</span><button class="btn sm ghost" onClick=${unlink}>${t("settings.machines.utiliser_celui_de_cette_machine")}</button>`}</${Line}>`
      : owns ? html`<${Line} label="${t("settings.machines.moteur_de_loom")}"><span class="state"><i class=${'dot ' + (node.reachable ? 'green' : 'red')}></i>${node.direct ? t("settings.machines.serveur") + node.kind + t("settings.machines.utilise_par_loom") : t("settings.machines.moteur_utilise_par_loom")}</span><a class="btn sm ghost" href="#/engine">${t("settings.machines.reglages_du_moteur")}</a><button class="btn sm ghost" onClick=${unlink}>${t("settings.machines.ne_plus_l_utiliser")}</button></${Line}>`
      : html`<${Line} label="${t("settings.machines.moteur_de_loom")}" tip="${t("settings.machines.si_cette_machine_a_une_carte_graphique_et_loom_installe_loom_peut")}">
          ${!direct && html`<button class="btn sm" onClick=${() => setDirect(true)}>${t("settings.machines.lier_son_serveur_llama_cpp_vllm")}</button>${paired ? html`<button class="btn sm ghost" disabled=${busy} onClick=${useEngine}>${t('machines.node.use_engine')}</button>` : html`<button class="btn sm ghost" onClick=${() => setPairing(true)}>${t("settings.machines.utiliser_le_loom_de_cette_machine")}</button>`}`}</${Line}>
        ${pairing && html`<${PairDialog} start=${{ address: m.host + ':2511', name: m.name }} onClose=${() => setPairing(false)} />`}
        ${direct && html`<${DirectEngineForm} start=${'http://' + m.host + ':8080'} onDone=${() => setDirect(false)} />`}
`}
  </${Group}>
  ${owns && !(node && node.direct) && html`<${ModelDirs} />`}
  ${m && html`<${MachineNodeMaintenance} key=${m.id} m=${m} />`}`;
}

function MachineNodeMaintenance({ m }) {
  const base = '/api/machines/' + encodeURIComponent(m.id) + '/node';
  const [info, setInfo] = useState(null), [pairing, setPairing] = useState(false);
  const load = () => get(base).then(r => { if (r.ok) setInfo(r); }).catch(() => {});
  useEffect(() => { load(); }, [m.id]);
  return html`<${Group} title=${t('node.maintenance')}>
    <p class="set-note">${t('node.maintenance_note')}</p>
    <${Line} label=${t('node.address')}>
      ${info?.linked ? html`<span class="mono">${info.url}</span>` : html`<span class="muted">${t('machines.node.not_paired')}</span>`}
      <button class="btn sm ghost" onClick=${() => setPairing(true)}>${info?.linked ? t('machines.node.repair') : t('machines.pair.ok')}</button>
    </${Line}>
    ${pairing && html`<${PairDialog} start=${{ address: (info?.url || m.host + ':2511').replace(/^https?:\/\//, ''), name: m.name }} onClose=${ok => { setPairing(false); if (ok) load(); }} />`}
  </${Group}>${info?.linked && html`<${LoomUpdates} key=${info.url} node=${true} endpoint=${base + '/update'} />`}`;
}

// Agents détectés sur cette machine : deux choix seulement ici. « Gérer »
// (Loom suit l'agent : compte, versions, import de discussions) et « Utiliser
// dans Loom » (il peut mener des discussions). Tout le reste est sur la page
// Agents.
function HarnessesSection({ m, onChange }) {
  const machine = m ? m.id : 'local';
  const [list, setList] = useState(null);
  const [busy, setBusy] = useState('');
  const load = () => get('/api/agents/installations').then(r => setList((r.installations || []).filter(i => i.machine === machine))).catch(() => setList([]));
  useEffect(() => { load(); }, [machine]);
  const change = async (i, patch) => {
    if (patch.enabled && !await confirm(t('agents.enable_title', { name: i.name }), t('agents.enable_note', { name: i.name, machine: i.machine_name }), { ok: t('agents.enable_ok') })) return;
    setBusy(i.harness);
    const r = await post('/api/agents/installations', { machine, harness: i.harness, ...patch, consent: !!patch.enabled }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t('agents.change_failed'), 'err');
    setList((r.installations || []).filter(x => x.machine === machine));
    await refreshWorkspace(); onChange && onChange();
  };
  if (!list) return html`<${Group} title=${t('app.groups.agents')}><div class="set-note"><span class="spinner"></span> ${t('settings.machines.lecture_des_harnesses_de_cette_machine')}</div></${Group}>`;
  const installed = list.filter(i => i.installed), absent = list.filter(i => !i.installed);
  return html`<section class="set-group anim-rise"><h3>${t('app.groups.agents')}<${Tip} text=${t('agents.machine_tip')} /></h3>
    <div class="card">
      ${installed.length ? html`<div class="ag-row ag-head"><span></span><span>${t('agents.manage')}</span><span>${t('agents.use')}</span></div>` : ''}
      ${installed.map(i => html`<div class="ag-row" key=${i.harness}>
        <span class="ag-name"><${Logo} name=${i.logo || i.harness} size="sm" /><b>${i.name}</b>${i.version && html`<em class="mono">${i.version.split(' ')[0]}</em>`}
          ${i.managed && html`<a class="btn sm ghost" href=${'#/harnesses/' + i.runtime_id}>${t('agents.open')}</a>`}</span>
        <${Switch} label=${t('agents.manage')} checked=${i.managed} disabled=${busy === i.harness} onChange=${v => change(i, { managed: v })} />
        <${Switch} label=${t('agents.use')} checked=${i.enabled} disabled=${busy === i.harness || !i.ready} onChange=${v => change(i, { enabled: v })} />
      </div>`)}
      ${!installed.length && html`<div class="set-note">${t('agents.none_here')}</div>`}
      ${absent.length > 0 && html`<div class="set-note">${t('agents.not_installed')} ${absent.map(i => i.name).join(', ')}. <a href="#/harnesses">${t('agents.install_from_agents')}</a></div>`}
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
  const paired = m && !m.user;
  useEffect(() => { get('/api/terminals').then(r => setList((r.terminals || []).filter(localT => localT.target === target))).catch(() => setList([])); }, [target]);
  return html`<${Group} title="${t("settings.machines.terminaux")}">
    ${list && list.length ? list.map(localT => html`<a class="set-line link-line" key=${localT.id} href=${'#/terminals/' + localT.id}><div class="set-l"><i class=${'dot ' + (localT.running ? 'green' : '')}></i><span>${localT.title}</span><span class="muted mono">${home(localT.dir)}</span></div><div class="set-c"><${Icon} n="right" /></div></a>`)
      : html`<div class="set-note">${t("settings.machines.aucun_terminal_ouvert")} ${m ? t("settings.machines.sur") + name : t("settings.machines.sur_cette_machine")}.</div>`}
    ${paired ? html`<div class="set-note">${t('machines.node.terminals_ssh')}</div>`
      : html`<div class="set-actions"><button class="btn ghost" onClick=${() => openTerminalWith({ target, dir: m ? m.home || '' : '', title: name })}><${Icon} n="prompt" />${t("settings.machines.nouveau_terminal")}</button></div>`}
  </${Group}>`;
}
