// Ressources : définies une fois dans Loom. Skills (instructions réutilisables
// choisies par projet) et serveurs MCP (outils des discussions locales).
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tabs, Switch, Empty, Menu, Tip } from '../../ui/controls.js';
import { Logo } from '../../ui/logo.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshWorkspace } from '../../core/state.js';
import { Memory } from './memory.js';
import { FolderPicker } from '../../ui/folder.js';

const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');

// Une skill est un dossier (SKILL.md + fichiers). Celles du dossier de Loom se
// modifient ici ; celles d'un dossier lié se lisent, et peuvent être copiées.
function SkillEditor({ skill, onClose }) {
  const ro = !!(skill && skill.read_only);
  const [name, setName] = useState(skill ? skill.name : '');
  const [desc, setDesc] = useState(skill ? skill.description : '');
  const [instr, setInstr] = useState(skill ? skill.instructions : '');
  const save = async copy => {
    const r = await post('/api/capabilities/save', { id: skill && !copy ? skill.id : '', name, description: desc, instructions: instr });
    if (!r.ok) return toast(r.error, 'err'); toast(copy ? 'Copiée dans Loom' : 'Skill enregistrée'); refreshWorkspace(); onClose();
  };
  return html`<${Modal} wide title=${!skill ? 'Nouvelle skill' : ro ? skill.name : 'Modifier la skill'} sub=${skill && skill.dir ? html`<span class="mono">${home(skill.dir)}/SKILL.md</span>${skill.files ? ' · ' + skill.files + ' autre' + (skill.files > 1 ? 's' : '') + ' fichier' + (skill.files > 1 ? 's' : '') : ''}` : 'Une méthode réutilisable, enregistrée comme dossier dans les skills de Loom.'} onClose=${onClose}
      foot=${ro ? html`<span class="muted grow">Dossier lié (${skill.source_label}) : modifie-la dans ce dossier.</span><button class="btn ghost" onClick=${onClose}>Fermer</button><button class="btn" onClick=${() => save(true)}>Copier dans Loom</button>`
        : html`<button class="btn ghost" onClick=${onClose}>Annuler</button><button class="btn primary" disabled=${!name.trim() || !instr.trim()} onClick=${() => save(false)}>Enregistrer</button>`}>
    <label class="field"><span>Nom</span><input class="input" value=${name} readonly=${ro} onInput=${e => setName(e.target.value)} placeholder="ex. Revue de code" maxlength="160" /></label>
    <label class="field"><span>Quand l’utiliser</span><input class="input" value=${desc} readonly=${ro} onInput=${e => setDesc(e.target.value)} placeholder="ex. Relire une modification avant de la publier" maxlength="500" /></label>
    <label class="field"><span>Instructions</span><textarea class="textarea" rows="12" value=${instr} readonly=${ro} onInput=${e => setInstr(e.target.value)} maxlength="8000"></textarea></label>
  </${Modal}>`;
}

// Dossiers de skills : celui de Loom, et les dossiers liés (lecture seule).
function SkillSources({ onChange }) {
  const [data, setData] = useState(null);
  const [pick, setPick] = useState(false);
  const load = () => get('/api/skills/sources').then(setData).catch(() => setData(null));
  useEffect(() => { load(); }, []);
  const link = async (path, label) => {
    const r = await post('/api/skills/sources', { path, label: label || '' });
    if (!r.ok) return toast(r.error || 'Impossible', 'err');
    toast('Dossier lié'); load(); refreshWorkspace(); onChange && onChange();
  };
  const unlink = async s => {
    if (!await confirm('Ne plus lier ce dossier', 'Ses skills disparaissent de Loom (et des harnesses où Loom les avait mises). Le dossier n’est pas modifié.', { ok: 'Délier' })) return;
    await post('/api/skills/sources', { unlink: s.id }); load(); refreshWorkspace(); onChange && onChange();
  };
  if (!data) return null;
  return html`<section class="sec"><div class="sec-h"><h2>Dossiers de skills<${Tip} text="Chaque skill est un dossier au format Agent Skills (SKILL.md et ses fichiers), lisible par Claude Code, Codex, Pi… Tu peux les modifier avec ton éditeur ou les versionner avec Git. Les dossiers liés sont lus sans être modifiés." /></h2>
      <button class="btn sm ghost" onClick=${() => setPick(true)}><${Icon} n="link" />Lier un dossier</button></div>
    <div class="card rows">${data.sources.map(s => html`<div class="row" key=${s.id}>
      <span class="mx-ico"><${Icon} n=${s.builtin ? 'sparkle' : 'link'} /></span>
      <div class="grow"><div class="t">${s.builtin ? 'Skills de Loom' : s.label}</div><div class="s mono">${home(s.path)}</div></div>
      ${s.error ? html`<span class="tag red">${s.error}</span>` : html`<span class="muted">${s.count} skill${s.count > 1 ? 's' : ''}${s.builtin ? '' : ' · lecture seule'}</span>`}
      ${!s.builtin && html`<button class="btn sm ghost" onClick=${() => unlink(s)}>Délier</button>`}</div>`)}
      ${(data.suggested || []).map(g => html`<div class="row sugg" key=${g.path}>
        <span class="mx-ico"><${Icon} n="folder" /></span>
        <div class="grow"><div class="t">${g.label}</div><div class="s mono">${home(g.path)}</div></div>
        <span class="muted">trouvé sur cette machine</span><button class="btn sm" onClick=${() => link(g.path, g.label)}>Lier</button></div>`)}</div>
    ${pick && html`<${FolderPicker} start="" onClose=${() => setPick(false)} onPick=${d => { setPick(false); link(d); }} />`}
  </section>`;
}

const toLines = o => Object.entries(o || {}).map(([k, v]) => k + '=' + v).join('\n');
const fromLines = t => Object.fromEntries(String(t || '').split('\n').map(l => l.trim()).filter(l => l.includes('=')).map(l => [l.slice(0, l.indexOf('=')).trim(), l.slice(l.indexOf('=') + 1).trim()]));

function McpEditor({ server, onClose, onSaved }) {
  const [name, setName] = useState(server ? server.name : '');
  const [mode, setMode] = useState(server && server.url ? 'http' : 'stdio');
  const [cmd, setCmd] = useState(server ? [server.command, ...(server.args || [])].filter(Boolean).join(' ') : '');
  const [url, setUrl] = useState(server ? server.url || '' : '');
  const [kv, setKv] = useState(server ? toLines(mode === 'http' ? server.headers : server.env) : '');
  const save = async () => {
    const parts = cmd.match(/(?:[^\s"]+|"[^"]*")+/g) || [];
    const body = { name, enabled: server ? server.enabled : true };
    if (mode === 'stdio') Object.assign(body, { command: (parts[0] || '').replace(/"/g, ''), args: parts.slice(1).map(a => a.replace(/^"|"$/g, '')), env: fromLines(kv) });
    else Object.assign(body, { url, headers: fromLines(kv) });
    const r = await post('/api/mcp/save', body);
    if (!r.ok) return toast(r.error, 'err'); toast('Serveur MCP enregistré'); onSaved(r.servers || []); onClose();
  };
  return html`<${Modal} title=${server ? 'Modifier ' + server.name : 'Ajouter un serveur MCP'} sub="Les outils du serveur deviennent disponibles dans les discussions locales, activables par discussion." onClose=${onClose}
      foot=${html`<button class="btn ghost" onClick=${onClose}>Annuler</button><button class="btn primary" disabled=${!name.trim() || (mode === 'stdio' ? !cmd.trim() : !url.trim())} onClick=${save}>Enregistrer</button>`}>
    <label class="field"><span>Nom</span><input class="input" value=${name} readonly=${!!server} onInput=${e => setName(e.target.value)} placeholder="ex. github" /></label>
    <div class="seg" role="tablist"><button type="button" role="tab" aria-selected=${String(mode === 'stdio')} onClick=${() => setMode('stdio')}>Programme local</button><button type="button" role="tab" aria-selected=${String(mode === 'http')} onClick=${() => setMode('http')}>Serveur HTTP</button></div>
    ${mode === 'stdio' ? html`<label class="field"><span>Commande</span><input class="input mono" value=${cmd} onInput=${e => setCmd(e.target.value)} placeholder="npx -y @modelcontextprotocol/server-filesystem /home/moi" /><small>Ce programme s’exécute sur cette machine avec tes droits.</small></label>`
      : html`<label class="field"><span>URL</span><input class="input mono" value=${url} onInput=${e => setUrl(e.target.value)} placeholder="https://mcp.exemple.com/mcp" /></label>`}
    <label class="field"><span>${mode === 'stdio' ? 'Variables d’environnement' : 'En-têtes'}</span><textarea class="textarea mono" rows="3" value=${kv} onInput=${e => setKv(e.target.value)} placeholder="CLE=valeur (une par ligne)"></textarea></label>
  </${Modal}>`;
}

// Distribution : Loom écrit ses skills dans le dossier natif de chaque famille
// de harness (dossiers loom-* uniquement), sur activation explicite.
function Distribution({ count }) {
  const [targets, setTargets] = useState(null);
  useEffect(() => { get('/api/skills/targets').then(r => setTargets(r.targets || [])).catch(() => setTargets([])); }, [count]);
  const toggle = async (t, on) => {
    if (on && !await confirm('Distribuer à ' + t.name, 'Loom placera dans ' + t.dir + ' un lien vers chaque skill (pas de copie : une seule version, la tienne). Il ne touche jamais aux dossiers qu’il n’a pas créés ; désactiver retire ses liens.', { ok: 'Distribuer' })) return;
    const r = await post('/api/skills/targets', { id: t.id, enabled: on });
    if (!r.ok) return toast(r.error || 'Impossible', 'err');
    setTargets(r.targets || []);
  };
  if (!targets || !targets.length) return null;
  return html`<section class="sec"><div class="sec-h"><h2>Distribution aux harnesses<${Tip} text="Les harnesses lisent leurs skills dans un dossier à eux. Loom y place un lien vers chaque skill choisie (une copie sous Windows), sans toucher aux autres dossiers." /></h2></div>
    <div class="card rows">${targets.map(t => html`<div class="row" key=${t.id}>
      <span class="dist-logos">${t.harnesses.map(h => html`<${Logo} name=${h} size="sm" />`)}</span>
      <div class="grow"><div class="t">${t.name}</div><div class="s mono">${t.dir.replace(/^\/home\/[^/]+/, '~')}</div></div>
      ${t.error ? html`<span class="tag amber" title=${t.error}>attention</span>` : ''}${t.enabled ? html`<span class="state"><i class="dot green"></i>${t.written.length} skill${t.written.length > 1 ? 's' : ''}</span>` : ''}
      <${Switch} checked=${t.enabled} label=${'Distribuer à ' + t.name} onChange=${on => toggle(t, on)} /></div>`)}</div></section>`;
}

function Skills() {
  const ws = useStore(app, s => s.workspace);
  const [dlg, setDlg] = useState(null);
  const skills = (ws && ws.capabilities) || [], projects = (ws && ws.projects) || [];
  const del = async s => { if (!await confirm('Supprimer la skill', 'Son dossier (' + home(s.dir) + ') est supprimé ; « ' + s.name + ' » ne sera plus ajoutée aux projets ni aux harnesses.', { ok: 'Supprimer', danger: true })) return; const r = await post('/api/capabilities/delete', { id: s.id }); if (!r.ok) toast(r.error, 'err'); refreshWorkspace(); };
  return html`<div>
    <div class="toolbar"><span class="grow muted" style="font-size:13.5px">Une skill est une méthode réutilisable, rangée dans un dossier. Choisis-la dans un projet, ou distribue-la aux harnesses.</span>
      <button class="btn primary" onClick=${() => setDlg({})}><${Icon} n="plus" />Nouvelle skill</button></div>
    ${skills.length ? html`<div class="grid3 stagger" style="margin-top:14px">${skills.map(s => { const used = projects.filter(p => (p.capability_ids || []).includes(s.id)).length;
      return html`<div class="card skill"><div class="skill-h"><span class="skill-ico"><${Icon} n=${s.read_only ? 'link' : 'sparkle'} /></span><b>${s.name}</b>${s.read_only && html`<span class="tag" title=${home(s.dir)}>${s.source_label}</span>`}</div>
        <p>${s.description || 'Instructions réutilisables.'}</p>
        <div class="skill-f"><span class="muted">${used ? used + ' projet' + (used > 1 ? 's' : '') : 'aucun projet'}</span><span class="grow"></span>
          <button class="btn sm ghost" onClick=${() => setDlg({ skill: s })}>${s.read_only ? 'Voir' : 'Modifier'}</button>${!s.read_only && html`<button class="icon-btn" aria-label="Supprimer" onClick=${() => del(s)}><${Icon} n="trash" /></button>`}</div></div>`; })}</div>`
      : html`<div class="card" style="margin-top:14px"><${Empty} icon="sparkle" title="Aucun skill" text="Une méthode de revue, un style d’écriture, une convention de code : écris-la une fois et réutilise-la dans tes projets.">
        <button class="btn primary" onClick=${() => setDlg({})}>Créer un skill</button></${Empty}></div>`}
    <${SkillSources} />
    <${Distribution} count=${skills.length} />
    ${dlg && html`<${SkillEditor} skill=${dlg.skill} onClose=${() => setDlg(null)} />`}
  </div>`;
}

function Mcp() {
  const [list, setList] = useState(null);
  const [dlg, setDlg] = useState(null);
  const [menu, setMenu] = useState(null);
  const load = async () => { const r = await get('/api/mcp'); setList(r.servers || []); };
  useEffect(() => { load(); }, []);
  const toggle = async (s, on) => { const r = await post('/api/mcp/toggle', { name: s.name, on }); if (r.ok === false) toast(r.error, 'err'); load(); };
  const del = async s => { if (!await confirm('Retirer le serveur', '« ' + s.name + ' » et ses outils seront retirés.', { ok: 'Retirer', danger: true })) return; await post('/api/mcp/delete', { name: s.name }); load(); };
  const test = async s => { const r = await post('/api/mcp/test', { name: s.name }); toast(r.ok ? (s.name + ' répond · ' + ((r.tools || []).length || (r.server && r.server.tools || []).length || 0) + ' outils') : (r.error || 'Échec'), r.ok ? '' : 'err'); load(); };
  return html`<div>
    <div class="toolbar"><span class="grow muted" style="font-size:13.5px">Outils externes (fichiers, bases, APIs) pour les discussions locales. Active-les par discussion avec le bouton Outils.</span>
      <button class="btn primary" onClick=${() => setDlg({})}><${Icon} n="plus" />Ajouter un serveur</button></div>
    ${list === null ? html`<div class="skeleton" style="height:120px;margin-top:14px"></div>` : list.length ? html`<div class="card rows stagger" style="margin-top:14px">${list.map(s => html`<div class="row">
        <i class=${'dot ' + (!s.enabled ? '' : s.connected ? 'green' : s.error ? 'red' : 'amber')}></i>
        <div class="grow"><div class="t">${s.name} <span class="tag">${s.transport === 'http' ? 'HTTP' : 'local'}</span></div>
          <div class="s">${s.error ? s.error : s.enabled ? (s.tools || []).length + ' outils' + ((s.disabled || []).length ? ' · ' + s.disabled.length + ' masqués' : '') : 'désactivé'}</div></div>
        <${Switch} checked=${s.enabled} label=${'Activer ' + s.name} onChange=${v => toggle(s, v)} />
        <button class="icon-btn" aria-label="Actions" onClick=${e => setMenu({ a: e.currentTarget, items: [{ label: 'Tester', icon: 'play', run: () => test(s) }, { label: 'Modifier', icon: 'edit', run: () => setDlg({ server: s }) }, '-', { label: 'Retirer', icon: 'trash', danger: true, run: () => del(s) }] })}><${Icon} n="more" /></button>
      </div>`)}</div>`
      : html`<div class="card" style="margin-top:14px"><${Empty} icon="plug" title="Aucun serveur MCP" text="Branche un serveur de fichiers, une base de données ou une API pour donner des outils à tes modèles locaux.">
        <button class="btn primary" onClick=${() => setDlg({})}>Ajouter un serveur</button></${Empty}></div>`}
    ${menu && html`<${Menu} anchor=${menu.a} onClose=${() => setMenu(null)} items=${menu.items} />`}
    ${dlg && html`<${McpEditor} server=${dlg.server} onClose=${() => setDlg(null)} onSaved=${setList} />`}
  </div>`;
}

export function ResourcesPage({ route }) {
  const tab = ['mcp', 'memory'].includes(route.sub) ? route.sub : 'skills';
  const ws = useStore(app, s => s.workspace);
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>Ressources</h1><p>Définies une fois dans Loom, utilisées par tes projets et tes discussions.</p></div></div>
    <${Tabs} value=${tab} onChange=${t => go('resources', t)} label="Ressources" options=${[{ value: 'skills', label: 'Skills', count: ws ? (ws.capabilities || []).length : null }, { value: 'mcp', label: 'Serveurs MCP' }, { value: 'memory', label: 'Mémoire' }]} />
    <div class="tab-body" key=${tab}>${tab === 'skills' ? html`<${Skills} />` : tab === 'mcp' ? html`<${Mcp} />` : html`<${Memory} />`}</div>
  </div></div>`;
}
