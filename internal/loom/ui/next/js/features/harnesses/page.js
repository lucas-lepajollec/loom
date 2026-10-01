// Harnesses : agents qui exécutent (Codex, Antigravity…). Loom garde la
// discussion ; chaque harness garde son compte, ses permissions et sa mémoire.
import { Logo } from '../../ui/logo.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
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
import { MachineDialog, MachinesSection } from './machines.js';
import { Lifecycle, remoteHarnessTarget } from './lifecycle.js';

// Ce que Loom sait vraiment piloter aujourd'hui, par capacité déclarée.
const CAPS = [
  ['chat', 'Discussion dans le fil commun'], ['native-events', 'Outils natifs visibles'], ['usage', 'Tokens et quotas'],
  ['reasoning-summary', 'Résumé de réflexion'], ['approvals', 'Autorisations interactives'], ['skills', 'Skills partagés'], ['mcp', 'Serveurs MCP'],
  ['tools', 'Outils et modifications visibles'], ['plan', 'Plan de l’agent'], ['workdir', 'Dossier de travail'], ['remote', 'Sur une autre machine'],
];
const isACP = rt => (rt.capabilities || []).includes('workdir');

// Ajout ou modification d'un harness ACP personnalisé (n'importe quel agent qui
// parle ACP sur stdio, y compris sur une autre machine via ssh).
// Les harnesses d'une autre machine passent par « Connecter une machine ».
const PRESETS = [{ label: 'Agent sur cette machine', name: '', command: '', args: '', remote: false }];
const splitArgs = t => (String(t).match(/"[^"]*"|'[^']*'|\S+/g) || []).map(a => a.replace(/^(["'])(.*)\1$/, '$2'));
const joinArgs = a => (a || []).map(x => /\s/.test(x) ? '"' + x + '"' : x).join(' ');

function CustomDialog({ agent, onClose }) {
  const [v, setV] = useState(agent ? { name: agent.name, command: agent.command, args: joinArgs(agent.args), remote: !!agent.remote } : { ...PRESETS[0] });
  const [busy, setBusy] = useState(false);
  const set = patch => setV({ ...v, ...patch });
  const save = async () => {
    if (/<hôte>/.test(v.args)) return toast('Remplace <hôte> par ta machine (ex. hermes-lxc)', 'err');
    setBusy(true);
    const r = await post('/api/harness/custom', { id: agent ? agent.id : '', name: v.name, command: v.command, args: splitArgs(v.args), remote: v.remote });
    setBusy(false);
    if (!r.ok) return toast(r.error || 'Enregistrement impossible', 'err');
    toast(v.name + ' ajouté'); await refreshWorkspace(); onClose(r.agent);
  };
  return html`<${Modal} title=${agent ? 'Modifier ' + agent.name : 'Ajouter un harness'} sub="Tout agent qui parle ACP (Agent Client Protocol) sur stdio." onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>Annuler</button><button class="btn primary" disabled=${busy || !v.name || !v.command} onClick=${save}>Enregistrer</button>`}>
    <label class="field"><span>Nom</span><input class="input" value=${v.name} placeholder="ex. Hermes" onInput=${e => set({ name: e.target.value })} /></label>
    <label class="field"><span>Commande</span><input class="input mono" value=${v.command} placeholder="ex. ssh, npx, opencode" onInput=${e => set({ command: e.target.value })} /></label>
    <label class="field"><span>Arguments</span><input class="input mono" value=${v.args} placeholder="ex. -T hermes-lxc hermes acp" onInput=${e => set({ args: e.target.value })} /></label>
    <div class="set-line" style="padding:4px 0;border:0"><div class="set-l"><span>Sur une autre machine</span><${Tip} text="Le dossier de travail est alors un chemin sur cette machine-là. Loom ne lit pas ses fichiers et ne lui transmet pas les serveurs MCP locaux ; les modifications restent visibles dans le fil." /></div>
      <div class="set-c"><${Switch} checked=${v.remote} label="Sur une autre machine" onChange=${on => set({ remote: on })} /></div></div>
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
    if (!await confirm('Connecter ' + rt.name, rt.consent, { ok: 'Connecter' })) return;
    setBusy(true);
    let r;
    try { r = await post('/api/runtimes/' + encodeURIComponent(rt.id) + '/connect', { consent: true }); }
    catch (e) { toast(e.message, 'err'); return; }
    finally { setBusy(false); }
    if (!r.ok) return toast(r.error, 'err'); toast('Modèles de ' + rt.name + ' disponibles'); refreshWorkspace();
  };
  return html`<div class="h-detail anim-fade" key=${rt.id}>
    <div class="h-head"><div style="display:flex;gap:14px;align-items:center"><${Logo} name=${rt.id} size="lg" /><div><h2>${rt.name}</h2><p>${rt.description || ''}</p></div></div>
      ${connectable ? html`<button class="btn" disabled=${busy} onClick=${connect}>${connected ? html`<${Icon} n="refresh" />Actualiser` : 'Connecter'}</button>` : !supported && html`<span class="tag">bientôt</span>`}</div>
    ${!supported ? html`<div class="card pad soon"><${Icon} n="sparkle" /><div><b>Adaptateur en préparation</b><p>Loom ne lance pas encore ${rt.name}. Il apparaîtra dans le sélecteur quand son adaptateur saura gérer la session, les autorisations et les événements.</p></div></div>` : html`
      <div class="grid2">
        <div class="card pad"><div class="sec-h"><h2>Source du modèle<${Tip} text="Natif : le compte du harness choisit parmi ses modèles. Les modèles Loom et les API personnalisées viendront quand l’adaptateur pourra les brancher proprement." /></h2></div>
          <div class="source"><label class="choice on"><input type="radio" checked /><span class="grow"><b>Natif</b><small>Le compte ${rt.name} choisit ses modèles</small></span></label>
            <label class="choice off"><input type="radio" disabled /><span class="grow"><b>Modèle Loom</b><small>Local ou cloud · bientôt</small></span></label></div></div>
        <div class="card pad"><div class="sec-h"><h2>Ce que Loom pilote</h2></div>
          ${CAPS.map(([id, label]) => html`<div class="kv"><span>${label}</span>${(rt.capabilities || []).includes(id) ? html`<span class="state"><${Icon} n="check" />oui</span>` : html`<span class="muted">non</span>`}</div>`)}</div>
      </div>
      <div class="card"><div class="sec-h pad-h"><h2>Modèles <span class="count">${groups.length}</span></h2>${connected && html`<span class="muted">Visibles dans le sélecteur de discussion</span>`}</div>
        ${connected ? html`<div class="rows">${groups.map(g => { const on = g.variants.some(v => v.enabled); return html`<div class="row"><span class="grow" ...${inspectTrigger(() => onInspect(g.variants[0]), 'Inspecter ' + g.name)}><div class="t">${g.name}</div><div class="s">${g.variants.length > 1 ? g.variants.length + ' niveaux de réflexion' : g.variants[0].model}</div></span>
          <${Switch} checked=${on} label=${'Afficher ' + g.name} onChange=${v => g.variants.forEach(x => setVisible(x.id, v))} /></div>`; })}</div>`
          : html`<p class="note pad-b">Connecte ${rt.name} pour lire les modèles de ton compte.</p>`}</div>
      <p class="note">Chaque envoi ouvre une session native avec le texte commun de la discussion. Les autorisations, la mémoire et les réglages de ${rt.name} restent chez lui. <a href="#/usage">Voir les quotas</a></p>`}
    <${MachineState} rt=${rt} />
  </div>`;
}

const SHORT = { chat: 'Discussion', 'native-events': 'Outils natifs', usage: 'Tokens et coût', 'reasoning-summary': 'Résumé de réflexion', approvals: 'Autorisations', skills: 'Skills', mcp: 'MCP Loom', tools: 'Outils et diffs', plan: 'Plan', workdir: 'Dossier de travail', remote: 'Machine distante' };

// Harness ACP : tout ce que Loom sait de l'agent (version, modèles, réflexion,
// modes, intégration Loom, usage, discussions) et les actions utiles.
const optValues = o => { const out = []; const walk = l => (l || []).forEach(x => x.options ? walk(x.options) : out.push(x)); walk(o && o.options); return out; };
const byCat = (cfg, cat) => (cfg || []).find(o => o.category === cat) || (cfg || []).find(o => o.id === cat);
const ago = ms => { const m = Math.round((Date.now() - ms) / 60000); return m < 1 ? 'à l’instant' : m < 60 ? 'il y a ' + m + ' min' : m < 1440 ? 'il y a ' + Math.round(m / 60) + ' h' : 'il y a ' + Math.round(m / 1440) + ' j'; };

// Sessions déjà ouvertes dans le harness (hors Loom) : les importer pour les
// continuer ici, avec la mémoire native du harness.
function NativeSessions({ rt }) {
  const [list, setList] = useState(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState('');
  const [q, setQ] = useState('');
  const load = async () => {
    setErr(''); setList(null);
    const r = await get('/api/runtimes/' + rt.id + '/sessions').catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) { setErr(r.error || 'Lecture impossible'); setList([]); return; }
    setList((r.sessions || []).sort((a, b) => String(b.updatedAt || '').localeCompare(String(a.updatedAt || ''))));
  };
  useEffect(() => { if (rt.available !== false) load(); }, [rt.id]);
  const importOne = async x => {
    if (x.imported) { openChat(x.imported); go('chat'); return; }
    setBusy(x.sessionId);
    const r = await post('/api/runtimes/' + rt.id + '/sessions/import', { sessionId: x.sessionId, cwd: x.cwd, title: x.title || '' }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || 'Import impossible', 'err');
    await refreshNav(); openChat(r.session.id); go('chat');
  };
  const shown = (list || []).filter(x => !q || ((x.title || '') + ' ' + x.cwd).toLowerCase().includes(q.toLowerCase()));
  return html`<section class="sec"><div class="sec-h"><h2>Sessions de ${rt.name} <span class="count">${list ? list.length : ''}</span><${Tip} text=${'Les discussions que tu as eues directement dans ' + rt.name + '. Les importer les ajoute à Loom ; tu les continues ici avec la mémoire du harness.'} /></h2>
      <span style="display:flex;gap:8px">${list && list.length > 6 && html`<label class="search" style="width:220px"><${Icon} n="search" /><input placeholder="Filtrer" aria-label="Filtrer les sessions" value=${q} onInput=${e => setQ(e.target.value)} /></label>`}
      <button class="btn sm ghost" onClick=${load}><${Icon} n="refresh" />Actualiser</button></span></div>
    ${list === null ? html`<div class="card pad"><div class="state"><span class="spinner"></span>Lecture des sessions de ${rt.name}…</div></div>`
      : err ? html`<div class="card pad"><p class="note err">${err}</p></div>`
      : !shown.length ? html`<div class="card pad"><p class="note">${q ? 'Aucune session ne correspond.' : 'Aucune session trouvée.'}</p></div>`
      : html`<div class="card rows">${shown.slice(0, 30).map(x => html`<div class="row" key=${x.sessionId}>
          <div class="grow"><div class="t">${x.title || 'Session sans titre'}</div>
            <div class="s">${[x.cwd.replace(/^\/home\/[^/]+/, '~'), x.updatedAt ? ago(Date.parse(x.updatedAt)) : ''].filter(Boolean).join(' · ')}</div></div>
          ${x.imported && html`<span class="tag">dans Loom</span>`}
          <button class="btn sm" disabled=${!!busy} onClick=${() => importOne(x)}>${busy === x.sessionId ? html`<span class="spinner"></span>Import…` : x.imported ? 'Ouvrir' : 'Importer'}</button></div>`)}</div>`}
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
    const msg = x.format === 'env'
      ? 'Tes modèles locaux apparaissent sous ' + rt.name + ' dans le sélecteur. Quand tu en choisis un, Loom lance ' + rt.name + ' avec son API locale comme fournisseur ; aucun fichier de ' + rt.name + ' n’est modifié et ses modèles natifs restent disponibles.'
      : 'Loom ajoute un fournisseur « loom » dans ' + x.file + ' avec tes modèles locaux. Tes autres fournisseurs ne sont pas modifiés. Le désactiver le retire.';
    if (on && !await confirm('Modèles de Loom dans ' + rt.name, msg, { ok: 'Activer' })) return;
    setBusy(true);
    const r = await post('/api/harness/model-source', { id: rt.id, enabled: on });
    setBusy(false);
    if (!r.ok) return toast(r.error, 'err');
    toast(on ? 'Modèles de Loom proposés dans ' + rt.name : 'Modèles de Loom retirés de ' + rt.name);
    load(); refreshWorkspace(); setTimeout(refreshWorkspace, 8000);
  };
  return html`<section class="sec"><div class="sec-h"><h2>Source du modèle<${Tip} text="Natif : le harness utilise ses propres modèles et son compte. Loom : tes modèles locaux de Loom apparaissent dans la liste du harness, servis par l’API locale de Loom." /></h2></div>
    <div class="card">
      <div class="set-line"><div class="set-l"><span>Natif</span></div><div class="set-c"><span class="state"><i class="dot green"></i>toujours disponible</span></div></div>
      <div class="set-line"><div class="set-l"><span>Modèles de Loom</span>${x.supported && html`<span class="muted" style="font-size:12px;margin-left:8px">${x.models} modèle${x.models > 1 ? 's' : ''} local${x.models > 1 ? 'aux' : ''}</span>`}</div>
        <div class="set-c">${x.supported ? html`<${Switch} checked=${x.enabled} disabled=${busy} label=${'Proposer les modèles de Loom dans ' + rt.name} onChange=${toggle} />`
          : html`<span class="muted">pas encore pris en charge par ce harness</span>`}</div></div>
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
    const [m, t] = await Promise.all([get('/api/harness/bindings?id=' + encodeURIComponent(rt.id)).catch(() => null), get('/api/skills/targets').catch(() => null)]);
    setMcp(m && m.ok ? m : { mcp: [], custom: false });
    const target = ((t && t.targets) || []).find(x => x.harnesses.includes(rt.id)) || null;
    setSk({ target, bindings: (t && t.bindings) || {} });
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
  return html`<section class="sec"><div class="sec-h"><h2>Ressources Loom transmises<${Tip} text=${'Loom garde la définition de tes skills et serveurs MCP ; tu choisis ce que ' + rt.name + ' reçoit. Le harness reste maître de leur exécution.'} /></h2>
      <a class="btn sm ghost" href="#/resources">Gérer les ressources</a></div>
    <div class="grid2">
      <div class="card pad"><div class="sec-h"><h2>Serveurs MCP</h2>${mcp.custom && html`<button class="btn sm ghost" onClick=${resetMcp}>Tous</button>`}</div>
        ${remote ? html`<p class="note">Non transmis : ce harness tourne sur une autre machine.</p>`
          : mcp.mcp.length ? mcp.mcp.map(r => html`<div class="kv" key=${r.name}><span>${r.name}${!r.enabled && html` <span class="muted">· désactivé dans Loom</span>`}</span>
              <${Switch} checked=${r.bound && r.enabled} disabled=${!r.enabled} label=${'Transmettre ' + r.name + ' à ' + rt.name} onChange=${on => setMcpBound(r.name, on)} /></div>`)
          : html`<p class="note">Aucun serveur MCP dans Loom.</p>`}</div>
      <div class="card pad"><div class="sec-h"><h2>Skills</h2></div>
        ${!sk.target ? html`<p class="note">Loom ne connaît pas encore le dossier de skills de ce harness.</p>`
          : !sk.target.enabled ? html`<div class="kv"><span>Distribution vers ${sk.target.dir.replace(/^\/home\/[^/]+/, '~')}</span><button class="btn sm" onClick=${enableTarget}>Activer</button></div>`
          : skills.length ? skills.map(c => html`<div class="kv" key=${c.id}><span class="trunc" title=${c.description || ''}>${c.name}</span>
              <${Switch} checked=${bound(c.id)} label=${'Envoyer ' + c.name + ' à ' + rt.name} onChange=${on => setSkill(c.id, on)} /></div>`)
          : html`<p class="note">Aucune skill dans Loom.</p>`}
        ${sk.target && sk.target.harnesses.length > 1 && sk.target.enabled && html`<p class="note" style="margin-top:8px">Dossier partagé avec ${sk.target.harnesses.filter(h => h !== rt.id).join(', ')}.</p>`}</div>
    </div></section>`;
}

// Ce que le harness possède déjà sur cette machine, lu dans son CLI et ses
// dossiers : version, compte, clés présentes, MCP, plugins, skills. Mise à jour
// par sa propre commande, sur demande.
const MCP_STATE = { connected: ['green', 'connecté'], enabled: ['green', 'activé'], 'needs-auth': ['amber', 'à authentifier'], disabled: ['', 'désactivé'], error: ['red', 'erreur'] };
// Harness sur une machine connectée : sa version et ses mises à jour, là-bas.
function RemoteState({ rt }) {
  const [t, setT] = useState(undefined);
  useEffect(() => { remoteHarnessTarget(rt.id).then(setT); }, [rt.id]);
  if (!t) return null;
  return html`<section class="sec"><div class="sec-h"><h2>Sur ${t.machine.name}</h2></div>
    <div class="card pad"><${Lifecycle} target=${t.target} id=${t.id} name=${rt.name} where=${'sur ' + t.machine.name} /></div></section>`;
}

function MachineState({ rt, commands }) {
  const [x, setX] = useState(null);
  const load = async refresh => {
    const r = await get('/api/runtimes/' + rt.id + '/inspect' + (refresh ? '?refresh=1' : '')).catch(() => null);
    setX(r && r.ok ? r.inspection : false);
  };
  useEffect(() => { load(false); }, [rt.id]);
  const adopt = async m => {
    const r = await post('/api/runtimes/' + rt.id + '/mcp/adopt', { name: m.name }).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) return toast(r.error || 'Adoption impossible', 'err');
    toast(r.name + ' ajouté à Loom, désactivé' + (r.env_to_fill && r.env_to_fill.length ? ' · à compléter : ' + r.env_to_fill.join(', ') : ''));
  };
  if (x === false) return null;
  if (!x) return html`<section class="sec"><div class="sec-h"><h2>Sur cette machine</h2></div><div class="card pad"><div class="state"><span class="spinner"></span>Lecture de ${rt.name}…</div></div></section>`;
  if (!x.installed) return html`<section class="sec"><div class="sec-h"><h2>Sur cette machine</h2></div><div class="card pad"><${Lifecycle} target="local" id=${rt.id} name=${rt.name} where="sur cette machine" onChange=${() => { setX(null); load(true); }} />${rt.docs ? html`<p class="note"><a href=${rt.docs} target="_blank" rel="noopener noreferrer">Documentation de ${rt.name}</a></p>` : ''}</div></section>`;
  const a = x.auth || {};
  return html`<section class="sec"><div class="sec-h"><h2>Sur cette machine<${Tip} text=${'Lu directement dans le CLI de ' + rt.name + ' et ses dossiers. Loom ne lit jamais la valeur des clés : il indique seulement lesquelles sont présentes.'} /></h2>
      <button class="btn sm ghost" onClick=${() => { setX(null); load(true); }}><${Icon} n="refresh" />Relire</button></div>
    <div class="grid2">
      <div class="card pad">
        <div class="kv"><span>Emplacement</span><code class="mono trunc" style="max-width:62%">${x.path.replace(/^\/home\/[^/]+/, '~')}</code></div>
        <div class="kv"><span>Compte</span><span class="state"><i class=${'dot ' + (a.connected ? 'green' : '')}></i>${a.connected ? [a.method, a.account].filter(Boolean).join(' · ') || a.status || 'connecté' : a.providers ? 'aucun fournisseur' : a.status || 'non lu'}</span></div>
        ${a.providers && a.providers.length > 0 && html`<div class="kv"><span>Fournisseurs</span><span>${a.providers.join(' · ')}</span></div>`}
        <div class="kv"><span>Clés d’API détectées</span><span>${x.env.length ? x.env.map(e => html`<span class="tag">${e}</span> `) : html`<span class="muted">aucune dans l’environnement</span>`}</span></div>
        <${Lifecycle} target="local" id=${rt.id} name=${rt.name} where="sur cette machine" onChange=${() => load(true)} />
      </div>
      <div class="card pad">
        <div class="kv"><span>Serveurs MCP du harness</span><span class="num">${x.mcp_known ? x.mcp.length : 'non lus'}</span></div>
        ${x.mcp.map(m => { const st = MCP_STATE[m.status] || ['', m.status || '']; return html`<div class="kv" key=${m.name}><span class="trunc" title=${m.target || ''}>${m.name}</span><span class="state">${st[1] && html`<i class=${'dot ' + st[0]}></i>`}${st[1]}
          ${(m.command || m.url) && html`<button class="btn sm ghost" title="Copier ce serveur dans Loom pour le donner aussi aux autres harnesses" onClick=${() => adopt(m)}>Adopter</button>`}</span></div>`; })}
        <div class="kv"><span>Plugins</span><span class="num">${x.plugins.length}</span></div>
        ${x.plugins.map(p => html`<div class="kv" key=${p}><span>${p}</span><span></span></div>`)}
        <div class="kv"><span>Skills installées</span><span class="num">${x.skills.length}</span></div>
        ${x.skills.map(k => html`<div class="kv" key=${k.folder + k.name}><span class="trunc" title=${k.description || ''}>${k.name}</span>${k.from_loom ? html`<span class="tag">via Loom</span>` : html`<span class="muted mono" style="font-size:11.5px">${k.folder}</span>`}</div>`)}
        ${commands != null && html`<div class="kv"><span>Commandes « / » disponibles<${Tip} text="Commandes et skills que le harness annonce dans une session, plugins compris." /></span><span class="num">${commands}</span></div>`}
      </div>
    </div>
  </section>`;
}

function AcpDetail({ rt, models, onEdit }) {
  const nav = useStore(app, a => a.nav);
  const [probe, setProbe] = useState(null);
  const [busy, setBusy] = useState(false);
  const [extra, setExtra] = useState({ usage: [], quota: null, target: null, mcp: [] });
  const [custom, setCustom] = useState(null);
  const load = async () => {
    const [p, u, t, m] = await Promise.all([get('/api/runtimes/' + rt.id + '/probe').catch(() => ({})), get('/api/usage').catch(() => ({})),
      get('/api/skills/targets').catch(() => ({})), get('/api/mcp').catch(() => ({}))]);
    setProbe((p && p.probe) || {});
    setExtra({ usage: ((u && u.models) || []).filter(x => x.runtime_id === rt.id), quota: ((u && u.quotas) || []).find(q => q.runtime_id === rt.id) || null,
      target: ((t && t.targets) || []).find(x => x.harnesses.includes(rt.id)) || null, mcp: Object.keys((m && m.servers) || {}) });
  };
  useEffect(() => { load(); if (rt.custom) get('/api/harness/custom').then(r => setCustom((r.agents || []).find(a => a.id === rt.id) || null)).catch(() => {}); }, [rt.id]);
  const refresh = async () => {
    setBusy(true);
    const r = await post('/api/runtimes/' + rt.id + '/probe', {}).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok) toast(r.error || 'Lecture impossible', 'err'); else toast('Modèles de ' + rt.name + ' à jour');
    await refreshWorkspace(); load();
  };
  const startWith = async choice => {
    if (!choice) return toast('Ce harness n’est pas encore disponible', 'err');
    await newDiscussion(); await chooseRemote(choice);
  };
  const del = async () => {
    if (!await confirm('Supprimer ' + rt.name, 'Le harness disparaît de Loom. Les discussions passées restent lisibles.', { ok: 'Supprimer', danger: true })) return;
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
        <div><h2>${rt.name}${ver && html` <span class="tag" title="Version de l’adaptateur ACP utilisé par Loom">ACP ${ver}</span>`}</h2><p>${rt.description || ''}</p></div></div>
      <div class="acts">${rt.custom && html`<button class="btn ghost" onClick=${() => onEdit(custom)}>Modifier</button><button class="icon-btn" aria-label="Supprimer" onClick=${del}><${Icon} n="trash" /></button>`}
        <button class="btn" disabled=${busy || missing} onClick=${refresh}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`}Actualiser</button>
        <button class="btn primary" disabled=${missing || !choices.length} onClick=${() => startWith(choiceFor(modelOpt && modelOpt.currentValue))}><${Icon} n="plus" />Nouvelle discussion</button></div></div>

    <div class="card local-strip">
      <div><div class="lbl">État</div><div class="v"><i class=${'dot ' + (missing ? '' : probe && probe.error && !cfg.length ? 'red' : 'green')}></i><span class="t">${missing ? 'Non installé' : probe && probe.error && !cfg.length ? 'À vérifier' : 'Prêt'}</span></div>
        <div class="sub">${probe && probe.at ? 'Lu ' + ago(probe.at) : 'Pas encore lu'}</div></div>
      <div><div class="lbl">Modèles</div><div class="v"><b>${list.length || '—'}</b></div><div class="sub">${modelOpt ? 'par défaut : ' + ((list.find(x => x.value === modelOpt.currentValue) || {}).name || modelOpt.currentValue) : 'non annoncés'}</div></div>
      <div><div class="lbl">Discussions</div><div class="v"><b>${talks.length}</b></div><div class="sub">${turns} tour${turns > 1 ? 's' : ''} dans Loom</div></div>
      <div><div class="lbl">Coût déclaré<${Tip} text="Coût que le harness déclare lui-même pour les sessions ouvertes par Loom. Un abonnement peut ne rien facturer." /></div>
        <div class="v"><b>${cost ? Number(cost).toLocaleString('fr-FR', { style: 'currency', currency, maximumFractionDigits: 2 }) : '—'}</b></div><div class="sub">${extra.quota && extra.quota.windows ? 'quotas lisibles dans Usage' : 'sessions Loom'}</div></div>
    </div>
    ${probe && probe.error && html`<div class="alert amber" style="margin-top:12px"><${Icon} n="alert" /><span>${probe.error}</span></div>`}
    ${!rt.custom && html`<${MachineState} rt=${rt} commands=${probe && probe.commands ? probe.commands.length : null} />`}
    ${rt.custom && rt.machine && html`<${RemoteState} rt=${rt} />`}
    <${ModelSource} rt=${rt} />
    <${LoomResources} rt=${rt} />

    <section class="sec"><div class="sec-h"><h2>Modèles <span class="count">${list.length}</span></h2></div>
      ${list.length ? html`<div class="card rows">${list.map(m => html`<div class="row" key=${m.value}>
          <div class="grow"><div class="t">${m.name || m.value}${modelOpt.currentValue === m.value && html` <span class="tag">par défaut</span>`}</div><div class="s">${m.description || m.value}</div></div>
          <button class="btn sm ghost" disabled=${missing} onClick=${() => startWith(choiceFor(m.value))}>Discuter</button></div>`)}</div>`
        : html`<div class="card pad"><p class="note">${missing ? 'Installe le CLI pour lire ses modèles.' : 'Pas encore lus. « Actualiser » ouvre une session vide, sans prompt, pour les découvrir.'}</p></div>`}
    </section>

    ${(effortOpt || (probe && probe.modes && probe.modes.length)) && html`<div class="grid2 sec">
      ${effortOpt && html`<div class="card pad"><div class="sec-h"><h2>Réflexion</h2></div>
        <div class="chips">${optValues(effortOpt).map(v => html`<span class=${cls('tag', v.value === effortOpt.currentValue && 'on')}>${v.name || v.value}</span>`)}</div>
        <p class="note" style="margin-top:10px">Réglable par discussion dans le panneau de droite.</p></div>`}
      ${probe && probe.modes && probe.modes.length > 0 && html`<div class="card pad"><div class="sec-h"><h2>Modes de l’agent</h2></div>
        ${probe.modes.map(m => html`<div class="kv" key=${m.id}><span>${m.name || m.id}${m.description && html`<${Tip} text=${m.description} />`}</span>${m.id === probe.mode ? html`<span class="tag">par défaut</span>` : html`<span></span>`}</div>`)}</div>`}
    </div>`}

    <section class="sec"><div class="sec-h"><h2>Intégration Loom</h2></div>
      <div class="card">
        <div class="set-line"><div class="set-l"><span>Dossier et autorisations</span><${Tip} text="Choisis-les par discussion dans le panneau de droite : Demander, Modifs auto ou Tout." /></div><div class="set-c"><span class="state">par discussion</span></div></div>
        <div class="set-line"><div class="set-l"><span>Skills Loom</span></div><div class="set-c">${extra.target ? html`<span class="state"><i class=${'dot ' + (extra.target.enabled ? 'green' : '')}></i>${extra.target.enabled ? extra.target.written.length + ' distribuées' : 'non distribuées'}</span><a class="btn sm ghost" href="#/resources">Gérer</a>` : html`<span class="muted">pas de dossier de skills connu</span>`}</div></div>
        <div class="set-line"><div class="set-l"><span>Serveurs MCP Loom</span></div><div class="set-c">${(rt.capabilities || []).includes('mcp') ? html`<span class="state">${extra.mcp.length ? extra.mcp.length + ' transmis à chaque session' : 'aucun défini'}</span><a class="btn sm ghost" href="#/resources/mcp">Gérer</a>` : html`<span class="muted">non transmis (machine distante)</span>`}</div></div>
        <div class="set-line"><div class="set-l"><span>Lancement</span><${Tip} text="Commande exécutée par Loom. L’agent parle ACP sur son entrée et sa sortie standard." /></div><div class="set-c"><code class="mono trunc" style="max-width:420px">${launch}</code></div></div>
        <div class="set-line"><div class="set-l"><span>Compte</span></div><div class="set-c"><span class="state">${(probe && probe.auth && probe.auth.length) ? probe.auth.map(a => a.name || a.id).join(' · ') : 'celui du CLI'}</span></div></div>
        ${rt.docs && html`<div class="set-line"><div class="set-l"><span>Documentation</span></div><div class="set-c"><a class="btn sm ghost" href=${rt.docs} target="_blank" rel="noopener noreferrer">Ouvrir</a></div></div>`}
      </div></section>

    ${(probe && probe.capabilities && probe.capabilities.sessionCapabilities && probe.capabilities.sessionCapabilities.list) ? html`<${NativeSessions} rt=${rt} />` : ''}

    <section class="sec"><div class="sec-h"><h2>Discussions récentes dans Loom <span class="count">${talks.length}</span></h2></div>
      ${talks.length ? html`<div class="card rows">${talks.slice(0, 8).map(c => html`<button type="button" class="row link-row" key=${c.id} onClick=${() => { openChat(c.id); go('chat'); }}>
          <div class="grow"><div class="t">${c.title || 'Discussion'}</div><div class="s">${[c.model && c.model !== 'default' ? c.model : '', c.workdir ? c.workdir.split('/').pop() : '', c.updated_at ? ago(c.updated_at) : ''].filter(Boolean).join(' · ')}</div></div>
          <${Icon} n="right" /></button>`)}</div>`
        : html`<div class="card pad"><p class="note">Aucune discussion avec ${rt.name} pour l’instant.</p></div>`}
    </section>
  </div>`;
}

function Card({ rt, models }) {
  const n = groupVariants(models.filter(m => m.runtime_id === rt.id)).length;
  const supported = rt.implemented && rt.capabilities && rt.capabilities.length > 0;
  const caps = CAPS.filter(([id]) => (rt.capabilities || []).includes(id)).map(([id]) => [id, SHORT[id]]);
  const acp = isACP(rt), missing = rt.available === false;
  const state = !supported ? null : missing ? ['', 'Non installé'] : acp ? ['green', 'Prêt'] : n ? ['green', 'Connecté'] : ['', 'Non connecté'];
  return html`<button type="button" class=${cls('hx', (!supported || missing) && 'is-soon')} onClick=${() => go('harnesses', rt.id)}>
    <div class="hx-top"><${Logo} name=${rt.id} />
      <span class="grow"><b>${rt.name}</b>${rt.machine ? html`<code>sur ${rt.machine}</code>` : rt.cli && html`<code>${acp ? 'ACP' + (rt.cli === 'npx' ? '' : ' · ' + rt.cli.split('/').pop()) : rt.cli}</code>`}</span>
      ${!supported ? html`<span class="soon-pill">Bientôt</span>` : html`<span class="state"><i class=${'dot ' + state[0]}></i>${state[1]}</span>`}</div>
    ${rt.description && html`<p>${rt.description}</p>`}
    ${caps.length > 0 && html`<ul>${caps.map(([id, label]) => html`<li key=${id}><${Icon} n="check" />${label}</li>`)}</ul>`}
    <div class="hx-foot">${!supported ? (rt.id === 'hermes' ? 'Connecte sa machine avec « Connecter une machine »' : 'Adaptateur en préparation') : missing ? 'Installe ' + (rt.cli === 'npx' ? 'Node.js et le CLI' : rt.cli) + ' pour l’utiliser'
      : acp ? (rt.custom ? 'Personnalisé · ' : '') + 'Dossier, outils et autorisations dans Loom' : n ? n + ' modèle' + (n > 1 ? 's' : '') + ' dans le sélecteur' : 'Ouvre pour connecter ton compte'}</div>
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
  useEffect(() => { setSelected(s => s && s.runtime !== route.sub ? null : s); }, [route.sub]);
  const selectedRuntime = runtimes.find(r => r.id === selected?.runtime);
  const selectedModel = models.find(m => m.id === selected?.model);
  const cur = runtimes.find(r => r.id === route.sub);
  const order = r => (r.implemented && r.capabilities && r.capabilities.length ? 0 : 1);
  return html`<div class="view page"><div class="page-in wide">
    ${cur ? html`<button class="btn ghost sm back" onClick=${() => go('harnesses')}><${Icon} n="left" />Harnesses</button>
      <div style="margin-top:14px">${isACP(cur) ? html`<${AcpDetail} key=${cur.id} rt=${cur} models=${models} onEdit=${a => setDlg({ agent: a })} />`
        : html`<${Detail} key=${cur.id} rt=${cur} models=${models} onInspect=${m => setSelected({ runtime: cur.id, model: m.id })} />`}</div>`
    : html`<div class="page-head"><div><h1>Harnesses</h1><p>Des agents qui gardent leurs outils, leur compte et leurs permissions. Loom leur passe la discussion.</p></div>
        <div class="acts"><button class="btn" onClick=${() => setDlg({ machine: true })}><${Icon} n="server" />Connecter une machine</button><button class="btn primary" onClick=${() => setDlg({})}><${Icon} n="plus" />Ajouter un harness</button></div></div>
      ${!ws ? html`<div class="skeleton" style="height:220px"></div>` : html`<div class="hx-grid stagger">${[...runtimes].sort((a, b) => order(a) - order(b)).map(r => html`<${Card} key=${r.id} rt=${r} models=${models} />`)}</div>`}
      <${MachinesSection} onEdit=${m => setDlg({ machine: m })} />`}
    ${dlg && dlg.machine && html`<${MachineDialog} machine=${dlg.machine === true ? null : dlg.machine} onClose=${m => { setDlg(null); if (m) MachinesSection.reload && MachinesSection.reload(); }} />`}
    ${dlg && !dlg.machine && html`<${CustomDialog} agent=${dlg.agent} onClose=${a => { setDlg(null); if (a && !dlg.agent) go('harnesses', a.id); }} />`}
    ${selectedRuntime && html`<${Drawer} title=${selectedModel?.name || selectedRuntime.name} onClose=${() => setSelected(null)}><${SelectionInfo} model=${selectedModel} runtime=${selectedRuntime} models=${models} /></${Drawer}>`}
  </div></div>`;
}
