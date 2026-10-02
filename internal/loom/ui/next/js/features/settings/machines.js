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
export const MachinesLink = ({ label }) => html`<a class="btn sm ghost" href="#/settings/machines"><${Icon} n="server" />${label || 'Gérer les machines'}</a>`;

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
    if (id !== 'local' && !m) return html`<${Empty} icon="server" title="Machine introuvable" />`;
    return html`<${MachineDetail} m=${m} local=${local} offers=${m ? (data.offers || {})[m.id] || [] : null} onChange=${load} onEdit=${() => setDlg({ machine: m })} />
      ${dlg && html`<${MachineDialog} machine=${dlg.machine} onClose=${x => { setDlg(null); if (x) load(); }} />`}`;
  }
  return html`
    <${Group} title="Machines">
      <div class="set-note">Loom travaille sur cette machine et sur les machines que tu connectes en SSH : leurs harnesses, leur moteur, leurs dossiers et des terminaux y sont gérés depuis ici.</div>
      <a class="mc-row" href="#/settings/machines/local"><span class="mx-ico"><${Icon} n="chip" /></span>
        <span class="grow"><b>${(local && local.hostname) || 'Cette machine'}</b><small>cette machine${local ? ' · ' + local.os : ''}</small></span><${Icon} n="right" /></a>
      ${data.machines.map(m => html`<a class="mc-row" key=${m.id} href=${'#/settings/machines/' + encodeURIComponent(m.id)}><span class="mx-ico"><${Icon} n="server" /></span>
        <span class="grow"><b>${m.name}</b><small class="mono">${m.user}@${m.host}${m.port !== 22 ? ':' + m.port : ''}${m.os ? ' · ' + m.os : ''}</small></span>
        <span class="mc-hs">${(m.harnesses || []).map(h => html`<${Logo} key=${h} name=${h} size="sm" />`)}</span><${Icon} n="right" /></a>`)}
      <div class="set-actions"><button class="btn" onClick=${() => setDlg({})}><${Icon} n="plus" />Connecter une machine</button></div>
    </${Group}>
    ${dlg && html`<${MachineDialog} machine=${null} onClose=${x => { setDlg(null); if (x) { load(); go('settings', 'machines', x.id); } }} />`}`;
}

function MachineDetail({ m, local, offers, onChange, onEdit }) {
  const isLocal = !m;
  const target = isLocal ? 'local' : m.id;
  const name = isLocal ? (local && local.hostname) || 'Cette machine' : m.name;
  const where = isLocal ? 'sur cette machine' : 'sur ' + m.name;
  const remove = async () => {
    if (!await confirm('Retirer ' + m.name, 'Ses harnesses disparaissent de Loom. Les discussions déjà faites restent ; rien n’est modifié sur la machine (la clé de Loom y reste autorisée tant que tu ne la retires pas).', { ok: 'Retirer', danger: true })) return;
    const r = await post('/api/machines/delete', { id: m.id });
    if (!r.ok) return toast(r.error || 'Suppression impossible', 'err');
    await refreshWorkspace(); go('settings', 'machines');
  };
  return html`
    <div class="mc-head anim-rise"><a class="btn sm ghost" href="#/settings/machines"><${Icon} n="left" />Machines</a>
      <div class="mc-title"><span class="mx-ico"><${Icon} n=${isLocal ? 'chip' : 'server'} /></span><div><h2>${name}</h2>
        <p class="mono">${isLocal ? (local ? local.user + ' · ' + home(local.home) + ' · ' + local.os : '') : m.user + '@' + m.host + (m.port !== 22 ? ':' + m.port : '') + (m.os ? ' · ' + m.os : '') + (m.home ? ' · ' + m.home : '')}</p></div>
        <span class="grow"></span>
        <button class="btn sm" onClick=${() => openTerminalWith({ target, dir: isLocal ? '' : m.home || '', title: name })}><${Icon} n="prompt" />Terminal</button>
        ${!isLocal && html`<button class="btn sm ghost" onClick=${onEdit}>Modifier</button><button class="icon-btn" aria-label=${'Retirer ' + m.name} onClick=${remove}><${Icon} n="trash" /></button>`}</div></div>
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
    if (!r.ok) return toast(r.error || 'Liaison impossible', 'err');
    setForm(null); toast('Moteur de ' + r.hostname + ' utilisé'); await refreshEngineNode(); refreshStatus(); refreshLibrary();
  };
  const unlink = async () => { await post('/api/engine/node', { unlink: true }); await refreshEngineNode(); refreshStatus(); refreshLibrary(); };
  return html`<${Group} title="Moteur">
    ${isLocal ? html`<${Line} label="Moteur de Loom" tip="Le moteur (llama.cpp) qui sert les modèles locaux dans tes discussions.">
        ${owns ? html`<span class="state"><i class="dot green"></i>sur cette machine</span><a class="btn sm ghost" href="#/settings/engine">Réglages du moteur</a>`
          : html`<span class="state">celui de ${node.hostname}</span><button class="btn sm ghost" onClick=${unlink}>Utiliser celui de cette machine</button>`}</${Line}>`
      : owns ? html`<${Line} label="Moteur de Loom"><span class="state"><i class=${'dot ' + (node.reachable ? 'green' : 'red')}></i>${node.direct ? 'serveur ' + node.kind + ' utilisé par Loom' : 'moteur utilisé par Loom'}</span><a class="btn sm ghost" href="#/settings/engine">Réglages du moteur</a><button class="btn sm ghost" onClick=${unlink}>Ne plus l’utiliser</button></${Line}>`
      : html`<${Line} label="Moteur de Loom" tip="Si cette machine a une carte graphique et Loom installé, Loom peut utiliser son moteur : ses modèles servent alors tes discussions ici.">
          ${!form && !direct && html`<button class="btn sm" onClick=${() => setDirect(true)}>Lier son serveur llama.cpp / vLLM</button><button class="btn sm ghost" onClick=${() => setForm({ url: 'http://' + m.host + ':8091', key: '' })}>Utiliser le Loom de cette machine</button>`}</${Line}>
        ${direct && html`<${DirectEngineForm} start=${'http://' + m.host + ':8080'} onDone=${() => setDirect(false)} />`}
        ${form && html`<div class="eng-link">
          <p class="note">Sur ${m.name} : Loom › Réglages › Accès réseau, active « Interface sur le réseau » et « API /v1 sur le réseau », puis recopie ici sa clé de pilotage.</p>
          <label class="field"><span>Adresse du Loom de ${m.name}</span><input class="input mono" value=${form.url} onInput=${e => setForm({ ...form, url: e.target.value })} /></label>
          <label class="field"><span>Sa clé de pilotage</span><input class="input mono" type="password" placeholder="loom-web-…" value=${form.key} onInput=${e => setForm({ ...form, key: e.target.value })} /></label>
          <div class="form-foot"><span class="grow"></span><button class="btn ghost" onClick=${() => setForm(null)}>Annuler</button><button class="btn primary" disabled=${busy || !form.url || !form.key} onClick=${link}>${busy ? 'Vérification…' : 'Utiliser ce moteur'}</button></div>
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
  if (isLocal && !states) return html`<${Group} title="Harnesses"><div class="set-note"><span class="spinner"></span> Lecture des harnesses de cette machine…</div></${Group}>`;
  const list = isLocal ? LOCAL_HARNESSES.filter(id => states[id]).map(id => ({ id, name: NAMES[id] || id, logo: id }))
    : (offers || []).map(o => ({ ...o }));
  const added = id => isLocal || (m.harnesses || []).includes('custom-' + m.id + '-' + id);
  const add = async o => {
    setBusy(o.id);
    const ids = (m.harnesses || []).map(h => h.slice(('custom-' + m.id + '-').length));
    const r = await post('/api/machines', { machine: { id: m.id, name: m.name, host: m.host, user: m.user, port: m.port }, harnesses: [...new Set([...ids, o.id])] }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || 'Ajout impossible', 'err');
    toast(o.name + ' ajouté à Loom'); await refreshWorkspace(); onChange();
  };
  const runtime = id => runtimes.find(r => r.id === id) || {};
  const installed = o => isLocal ? !!(states[o.id] && states[o.id].installed) : o.installed;
  const card = o => html`<div class="mh-item card pad" key=${o.id}>
      <div class="mh-head"><${Logo} name=${o.logo || o.id} /><b>${o.name}</b><span class="grow"></span>
        ${isLocal ? (runtime(o.id).id ? html`<a class="btn sm ghost" href=${'#/harnesses/' + o.id}>Page du harness</a>` : html`<span class="muted">pas encore piloté par Loom</span>`)
          : added(o.id) ? html`<span class="state"><i class="dot green"></i>dans Loom</span><a class="btn sm ghost" href=${'#/harnesses/custom-' + m.id + '-' + o.id}>Page du harness</a>`
          : o.ready ? html`<button class="btn sm" disabled=${busy === o.id} onClick=${() => add(o)}>Ajouter à Loom</button>`
          : o.missing ? html`<span class="state err">${o.missing}</span>` : ''}</div>
      <${Lifecycle} target=${target} id=${o.id} name=${o.name} where=${where} onChange=${onChange} />
    </div>`;
  const absent = list.filter(o => !installed(o));
  return html`<section class="set-group anim-rise"><h3>Harnesses<${Tip} text=${'Version installée et dernière publiée, installation et mises à jour ' + where + '. Un harness ajouté à Loom apparaît dans le sélecteur de modèles.'} /></h3>
    <div class="mh-list">${list.filter(installed).map(card)}
      ${absent.length > 0 && html`<div class="card mh-absent"><div class="mh-absent-h">À installer ${where}</div>
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
    if (!r.ok) return toast(r.error || 'Enregistrement impossible', 'err');
    setFolders(r.folders);
  };
  const add = async () => {
    if (!m) return setPick(true);
    const p = await prompt('Dossier sur ' + m.name, { placeholder: (m.home || '/home/moi') + '/projet', ok: 'Ajouter' });
    if (p) save([...(folders || []), p]);
  };
  return html`<${Group} title="Dossiers de travail">
    <div class="set-note">Proposés quand tu choisis où travaille un harness ou où s’ouvre un terminal${m ? ' sur ' + m.name : ''}.</div>
    ${(folders || []).map(f => html`<div class="set-line" key=${f}><div class="set-l"><${Icon} n="folder" /><span class="mono path">${home(f)}</span></div>
      <div class="set-c"><button class="btn sm ghost" onClick=${() => openTerminalWith({ target, dir: f })}><${Icon} n="prompt" />Terminal</button>
        <button class="icon-btn" aria-label="Retirer" onClick=${() => save(folders.filter(x => x !== f))}><${Icon} n="close" /></button></div></div>`)}
    <div class="set-actions"><button class="btn ghost" onClick=${add}><${Icon} n="plus" />Ajouter un dossier</button></div>
    ${pick && html`<${FolderPicker} start="" onClose=${() => setPick(false)} onPick=${d => { setPick(false); save([...(folders || []), d]); }} />`}
  </${Group}>`;
}

function TerminalsSection({ target, name, m }) {
  const [list, setList] = useState(null);
  useEffect(() => { get('/api/terminals').then(r => setList((r.terminals || []).filter(t => t.target === target))).catch(() => setList([])); }, [target]);
  return html`<${Group} title="Terminaux">
    ${list && list.length ? list.map(t => html`<a class="set-line link-line" key=${t.id} href=${'#/terminals/' + t.id}><div class="set-l"><i class=${'dot ' + (t.running ? 'green' : '')}></i><span>${t.title}</span><span class="muted mono">${home(t.dir)}</span></div><div class="set-c"><${Icon} n="right" /></div></a>`)
      : html`<div class="set-note">Aucun terminal ouvert ${m ? 'sur ' + name : 'sur cette machine'}.</div>`}
    <div class="set-actions"><button class="btn ghost" onClick=${() => openTerminalWith({ target, dir: m ? m.home || '' : '', title: name })}><${Icon} n="prompt" />Nouveau terminal</button></div>
  </${Group}>`;
}
