import { t } from '../../core/i18n.js';
// Ressources : définies une fois dans Loom. Skills (instructions réutilisables
// choisies par projet) et serveurs MCP (outils des discussions locales).
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tabs, Switch, Empty, Menu, Tip } from '../../ui/controls.js';
import { Logo } from '../../ui/logo.js';
import { Modal, confirm, prompt, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshWorkspace } from '../../core/state.js';
import { Memory } from './memory.js';
import { Brain } from './brain.js';
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
    if (!r.ok) return toast(r.error, 'err'); toast(copy ? t("resources.page.copiee_dans_loom") : t("resources.page.skill_enregistree")); refreshWorkspace(); onClose();
  };
  return html`<${Modal} wide title=${!skill ? t("resources.page.nouvelle_skill") : ro ? skill.name : t("resources.page.modifier_la_skill")} sub=${skill && skill.dir ? html`<span class="mono">${home(skill.dir)}${t("resources.page.skill_md")}</span>${skill.files ? ' · ' + skill.files + t("resources.page.autre") + (skill.files > 1 ? 's' : '') + t("resources.page.fichier") + (skill.files > 1 ? 's' : '') : ''}` : t("resources.page.une_methode_reutilisable_enregistree_comme_dossier_dans_les_skill")} onClose=${onClose}
      foot=${ro ? html`<span class="muted grow">${t("resources.page.dossier_lie")}${skill.source_label}${t("resources.page.modifie_la_dans_ce_dossier")}</span><button class="btn ghost" onClick=${onClose}>${t("resources.page.fermer")}</button><button class="btn" onClick=${() => save(true)}>${t("resources.page.copier_dans_loom")}</button>`
        : html`<button class="btn ghost" onClick=${onClose}>${t("resources.page.annuler")}</button><button class="btn primary" disabled=${!name.trim() || !instr.trim()} onClick=${() => save(false)}>${t("resources.page.enregistrer")}</button>`}>
    <label class="field"><span>${t("resources.page.nom")}</span><input class="input" value=${name} readonly=${ro} onInput=${e => setName(e.target.value)} placeholder="${t("resources.page.ex_revue_de_code")}" maxlength="160" /></label>
    <label class="field"><span>${t("resources.page.quand_l_utiliser")}</span><input class="input" value=${desc} readonly=${ro} onInput=${e => setDesc(e.target.value)} placeholder="${t("resources.page.ex_relire_une_modification_avant_de_la_publier")}" maxlength="500" /></label>
    <label class="field"><span>${t("resources.page.instructions")}</span><textarea class="textarea" rows="12" value=${instr} readonly=${ro} onInput=${e => setInstr(e.target.value)} maxlength="8000"></textarea></label>
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
    if (!r.ok) return toast(r.error || t("resources.page.impossible"), 'err');
    toast(t("resources.page.dossier_lie_2")); load(); refreshWorkspace(); onChange && onChange();
  };
  const unlink = async s => {
    if (!await confirm(t("resources.page.ne_plus_lier_ce_dossier"), t("resources.page.ses_skills_disparaissent_de_loom_et_des_harnesses_ou_loom_les_ava"), { ok: t("resources.page.delier") })) return;
    await post('/api/skills/sources', { unlink: s.id }); load(); refreshWorkspace(); onChange && onChange();
  };
  if (!data) return null;
  return html`<section class="sec"><div class="sec-h"><h2>${t("resources.page.dossiers_de_skills")}<${Tip} text="${t("resources.page.chaque_skill_est_un_dossier_au_format_agent_skills_skill_md_et_se")}" /></h2>
      <button class="btn sm ghost" onClick=${() => setPick(true)}><${Icon} n="link" />${t("resources.page.lier_un_dossier")}</button></div>
    <div class="card rows">${data.sources.map(s => html`<div class="row" key=${s.id}>
      <span class="mx-ico"><${Icon} n=${s.builtin ? 'sparkle' : 'link'} /></span>
      <div class="grow"><div class="t">${s.builtin ? t("resources.page.skills_de_loom") : s.label}</div><div class="s mono">${home(s.path)}</div></div>
      ${s.error ? html`<span class="tag red">${s.error}</span>` : html`<span class="muted">${s.count} ${t("resources.page.skill")}${s.count > 1 ? 's' : ''}${s.builtin ? '' : t("resources.page.lecture_seule")}</span>`}
      ${!s.builtin && html`<button class="btn sm ghost" onClick=${() => unlink(s)}>${t("resources.page.delier")}</button>`}</div>`)}
      ${(data.suggested || []).map(g => html`<div class="row sugg" key=${g.path}>
        <span class="mx-ico"><${Icon} n="folder" /></span>
        <div class="grow"><div class="t">${g.label}</div><div class="s mono">${home(g.path)}</div></div>
        <span class="muted">${t("resources.page.trouve_sur_cette_machine")}</span><button class="btn sm" onClick=${() => link(g.path, g.label)}>${t("resources.page.lier")}</button></div>`)}</div>
    ${pick && html`<${FolderPicker} start="" onClose=${() => setPick(false)} onPick=${d => { setPick(false); link(d); }} />`}
  </section>`;
}

const toLines = o => Object.entries(o || {}).map(([k, v]) => k + '=' + v).join('\n');
const fromLines = localT => Object.fromEntries(String(localT || '').split('\n').map(l => l.trim()).filter(l => l.includes('=')).map(l => [l.slice(0, l.indexOf('=')).trim(), l.slice(l.indexOf('=') + 1).trim()]));

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
    if (!r.ok) return toast(r.error, 'err'); toast(t("resources.page.serveur_mcp_enregistre")); onSaved(r.servers || []); onClose();
  };
  return html`<${Modal} title=${server ? t("environment.page.modifier_prefix") + server.name : t("resources.page.ajouter_un_serveur_mcp")} sub="${t("resources.page.les_outils_du_serveur_deviennent_disponibles_dans_les_discussions")}" onClose=${onClose}
      foot=${html`<button class="btn ghost" onClick=${onClose}>${t("resources.page.annuler")}</button><button class="btn primary" disabled=${!name.trim() || (mode === 'stdio' ? !cmd.trim() : !url.trim())} onClick=${save}>${t("resources.page.enregistrer")}</button>`}>
    <label class="field"><span>${t("resources.page.nom")}</span><input class="input" value=${name} readonly=${!!server} onInput=${e => setName(e.target.value)} placeholder="${t("resources.page.ex_github")}" /></label>
    <div class="seg" role="tablist"><button type="button" role="tab" aria-selected=${String(mode === 'stdio')} onClick=${() => setMode('stdio')}>${t("resources.page.programme_local")}</button><button type="button" role="tab" aria-selected=${String(mode === 'http')} onClick=${() => setMode('http')}>${t("resources.page.serveur_http")}</button></div>
    ${mode === 'stdio' ? html`<label class="field"><span>${t("resources.page.commande")}</span><input class="input mono" value=${cmd} onInput=${e => setCmd(e.target.value)} placeholder="${t("resources.page.npx_y_modelcontextprotocol_server_filesystem_home_moi")}" /><small>${t("resources.page.ce_programme_s_execute_sur_cette_machine_avec_tes_droits")}</small></label>`
      : html`<label class="field"><span>URL</span><input class="input mono" value=${url} onInput=${e => setUrl(e.target.value)} placeholder="https://mcp.exemple.com/mcp" /></label>`}
    <label class="field"><span>${mode === 'stdio' ? t("resources.page.variables_d_environnement") : t("resources.page.en_tetes")}</span><textarea class="textarea mono" rows="3" value=${kv} onInput=${e => setKv(e.target.value)} placeholder="${t("resources.page.cle_valeur_une_par_ligne")}"></textarea></label>
  </${Modal}>`;
}

// Distribution : Loom écrit ses skills dans le dossier natif de chaque famille
// de harness (dossiers loom-* uniquement), sur activation explicite.
function Distribution({ count }) {
  const [targets, setTargets] = useState(null);
  useEffect(() => { get('/api/skills/targets').then(r => setTargets(r.targets || [])).catch(() => setTargets([])); }, [count]);
  const toggle = async (localT, on) => {
    if (on && !await confirm(t("resources.page.distribuer_a") + localT.name, t("resources.page.loom_placera_dans") + localT.dir + t("resources.page.un_lien_vers_chaque_skill_pas_de_copie_une_seule_version_la_tienn"), { ok: t("resources.page.distribuer") })) return;
    const r = await post('/api/skills/targets', { id: localT.id, enabled: on });
    if (!r.ok) return toast(r.error || t("resources.page.impossible"), 'err');
    setTargets(r.targets || []);
  };
  if (!targets || !targets.length) return null;
  return html`<section class="sec"><div class="sec-h"><h2>${t("resources.page.distribution_aux_harnesses")}<${Tip} text="${t("resources.page.les_harnesses_lisent_leurs_skills_dans_un_dossier_a_eux_loom_y_pl")}" /></h2></div>
    <div class="card rows">${targets.map(localT => html`<div class="row" key=${localT.id}>
      <span class="dist-logos">${localT.harnesses.map(h => html`<${Logo} name=${h} size="sm" />`)}</span>
      <div class="grow"><div class="t">${localT.name}</div><div class="s mono">${localT.dir.replace(/^\/home\/[^/]+/, '~')}</div></div>
      ${localT.error ? html`<span class="tag amber" title=${localT.error}>${t("resources.page.attention")}</span>` : ''}${localT.enabled ? html`<span class="state"><i class="dot green"></i>${localT.written.length} ${t("resources.page.skill")}${localT.written.length > 1 ? 's' : ''}</span>` : ''}
      <${Switch} checked=${localT.enabled} label=${t("resources.page.distribuer_a") + localT.name} onChange=${on => toggle(localT, on)} /></div>`)}</div></section>`;
}

function Skills() {
  const ws = useStore(app, s => s.workspace);
  const [dlg, setDlg] = useState(null);
  const skills = (ws && ws.capabilities) || [], projects = (ws && ws.projects) || [];
  const del = async s => { if (!await confirm(t("resources.page.supprimer_la_skill"), t("resources.page.son_dossier") + home(s.dir) + t("resources.page.est_supprime") + s.name + t("resources.page.ne_sera_plus_ajoutee_aux_projets_ni_aux_harnesses"), { ok: t("resources.page.supprimer"), danger: true })) return; const r = await post('/api/capabilities/delete', { id: s.id }); if (!r.ok) toast(r.error, 'err'); refreshWorkspace(); };
  return html`<div>
    <div class="toolbar"><span class="grow muted" style="font-size:13.5px">${t("resources.page.une_skill_est_une_methode_reutilisable_rangee_dans_un_dossier_cho")}</span>
      <button class="btn primary" onClick=${() => setDlg({})}><${Icon} n="plus" />${t("resources.page.nouvelle_skill")}</button></div>
    ${skills.length ? html`<div class="grid3 stagger" style="margin-top:14px">${skills.map(s => { const used = projects.filter(p => (p.capability_ids || []).includes(s.id)).length;
      return html`<div class="card skill"><div class="skill-h"><span class="skill-ico"><${Icon} n=${s.read_only ? 'link' : 'sparkle'} /></span><b>${s.name}</b>${s.read_only && html`<span class="tag" title=${home(s.dir)}>${s.source_label}</span>`}</div>
        <p>${s.description || t("resources.page.instructions_reutilisables")}</p>
        <div class="skill-f"><span class="muted">${used ? used + t("resources.page.projet") + (used > 1 ? 's' : '') : t("resources.page.aucun_projet")}</span><span class="grow"></span>
          <button class="btn sm ghost" onClick=${() => setDlg({ skill: s })}>${s.read_only ? t("resources.page.voir") : t("harnesses.machines.modifier")}</button>${!s.read_only && html`<button class="icon-btn" aria-label="${t("resources.page.supprimer")}" onClick=${() => del(s)}><${Icon} n="trash" /></button>`}</div></div>`; })}</div>`
      : html`<div class="card" style="margin-top:14px"><${Empty} icon="sparkle" title="${t("resources.page.aucun_skill")}" text="${t("resources.page.une_methode_de_revue_un_style_d_ecriture_une_convention_de_code_e")}">
        <button class="btn primary" onClick=${() => setDlg({})}>${t("resources.page.creer_un_skill")}</button></${Empty}></div>`}
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
  const del = async s => { if (!await confirm(t("resources.page.retirer_le_serveur"), '« ' + s.name + t("resources.page.et_ses_outils_seront_retires"), { ok: t("resources.page.retirer"), danger: true })) return; await post('/api/mcp/delete', { name: s.name }); load(); };
  const test = async s => { const r = await post('/api/mcp/test', { name: s.name }); toast(r.ok ? (s.name + t("resources.page.repond") + ((r.tools || []).length || (r.server && r.server.tools || []).length || 0) + t("resources.page.outils")) : (r.error || t("resources.page.echec")), r.ok ? '' : 'err'); load(); };
  return html`<div>
    <div class="toolbar"><span class="grow muted" style="font-size:13.5px">${t("resources.page.outils_externes_fichiers_bases_apis_pour_les_discussions_locales")}</span>
      <button class="btn primary" onClick=${() => setDlg({})}><${Icon} n="plus" />${t("resources.page.ajouter_un_serveur")}</button></div>
    ${list === null ? html`<div class="skeleton" style="height:120px;margin-top:14px"></div>` : list.length ? html`<div class="card rows stagger" style="margin-top:14px">${list.map(s => html`<div class="row">
        <i class=${'dot ' + (!s.enabled ? '' : s.connected ? 'green' : s.error ? 'red' : 'amber')}></i>
        <div class="grow"><div class="t">${s.name} <span class="tag">${s.transport === 'http' ? 'HTTP' : 'local'}</span></div>
          <div class="s">${s.error ? s.error : s.enabled ? (s.tools || []).length + t("resources.page.outils") + ((s.disabled || []).length ? ' · ' + s.disabled.length + t("resources.page.masques") : '') : t("resources.page.desactive")}</div></div>
        <${Switch} checked=${s.enabled} label=${t("resources.page.activer") + s.name} onChange=${v => toggle(s, v)} />
        <button class="icon-btn" aria-label="${t("resources.page.actions")}" onClick=${e => setMenu({ a: e.currentTarget, items: [{ label: t("resources.page.tester"), icon: 'play', run: () => test(s) }, { label: t("resources.page.modifier"), icon: 'edit', run: () => setDlg({ server: s }) }, '-', { label: t("resources.page.retirer"), icon: 'trash', danger: true, run: () => del(s) }] })}><${Icon} n="more" /></button>
      </div>`)}</div>`
      : html`<div class="card" style="margin-top:14px"><${Empty} icon="plug" title="${t("resources.page.aucun_serveur_mcp")}" text="${t("resources.page.branche_un_serveur_de_fichiers_une_base_de_donnees_ou_une_api_pou")}">
        <button class="btn primary" onClick=${() => setDlg({})}>${t("resources.page.ajouter_un_serveur")}</button></${Empty}></div>`}
    <${McpFile} />
    <${McpSources} onAdopted=${load} />
    ${menu && html`<${Menu} anchor=${menu.a} onClose=${() => setMenu(null)} items=${menu.items} />`}
    ${dlg && html`<${McpEditor} server=${dlg.server} onClose=${() => setDlg(null)} onSaved=${setList} />`}
  </div>`;
}

// Le fichier mcp.json de Loom : modifiable à la main, relu automatiquement.
function McpFile() {
  const [f, setF] = useState(null);
  useEffect(() => { get('/api/mcp/file').then(setF).catch(() => setF(null)); }, []);
  if (!f || !f.path) return null;
  return html`<div class=${cls('mcp-file', f.error && 'err')}><${Icon} n="file" /><span>${t("resources.page.serveurs_enregistres_dans")} <code class="mono">${home(f.path)}</code>${t("resources.page.au_format_standard_claude_cursor_modifiable_avec_ton_editeur_relu")}</span>
    ${f.error && html`<span class="tag red" title=${f.error}>${t("resources.page.fichier_invalide_la_derniere_version_correcte_reste_utilisee")}</span>`}</div>`;
}

// Fichiers MCP d'autres outils, liés en lecture seule : leurs serveurs
// s'affichent ici et peuvent être adoptés (copiés dans Loom, désactivés).
function McpSources({ onAdopted }) {
  const [data, setData] = useState(null);
  const [busy, setBusy] = useState('');
  const load = () => get('/api/mcp/sources').then(setData).catch(() => setData(null));
  useEffect(() => { load(); }, []);
  const link = async (path, label, unlink) => {
    const r = await post('/api/mcp/sources', { path, label: label || '', action: unlink ? 'unlink' : 'link' });
    if (!r.ok) return toast(r.error || t("resources.page.impossible"), 'err');
    setData(r);
  };
  const addFile = async () => { const p = await prompt(t("resources.page.lier_un_fichier_mcp"), { message: t("resources.page.fichier_de_configuration_mcp_d_un_autre_outil_mcp_json_mcp_json_c"), placeholder: t("resources.page.chemin_vers_mcp_json"), ok: t("resources.page.lier") }); if (p) link(p.trim()); };
  const adopt = async sv => {
    const withEnv = sv.env_names && sv.env_names.length > 0 && await confirm(t("resources.page.adopter_prefix") + sv.name, t("resources.page.copier_aussi_les_valeurs_de_ses_variables") + sv.env_names.join(', ') + t("resources.page.sinon_loom_copie_seulement_leurs_noms_et_tu_les_remplis_toi_meme"), { ok: t("resources.page.copier_les_valeurs") });
    setBusy(sv.source + sv.name);
    const r = await post('/api/mcp/sources/adopt', { source: sv.source, name: sv.name, with_env: !!withEnv });
    setBusy('');
    if (!r.ok) return toast(r.error || t("resources.page.adoption_impossible"), 'err');
    toast(sv.name + t("resources.page.ajoute_a_loom_desactive") + (r.env_to_fill && r.env_to_fill.length ? t("resources.page.a_completer") + r.env_to_fill.join(', ') : '')); onAdopted();
  };
  if (!data) return null;
  return html`<section class="sec"><div class="sec-h"><h2>${t("resources.page.fichiers_mcp_lies")}<${Tip} text="${t("resources.page.les_serveurs_mcp_configures_dans_d_autres_outils_loom_lit_ces_fic")}" /></h2>
      <button class="btn sm ghost" onClick=${addFile}><${Icon} n="link" />${t("resources.page.lier_un_fichier")}</button></div>
    ${(data.sources || []).map(src => html`<div class="card mcp-src" key=${src.path}>
      <div class="row"><span class="mx-ico"><${Icon} n="link" /></span><div class="grow"><div class="t">${src.label}</div><div class="s mono">${home(src.path)}</div></div>
        ${src.error ? html`<span class="tag red" title=${src.error}>${t("resources.page.illisible")}</span>` : html`<span class="muted">${src.servers.length} ${t("resources.page.serveur")}${src.servers.length > 1 ? 's' : ''}</span>`}
        <button class="btn sm ghost" onClick=${() => link(src.path, '', true)}>${t("resources.page.delier")}</button></div>
      ${src.servers.map(sv => html`<div class="row sub" key=${sv.name + (sv.project || '')}><span class="grow"><b>${sv.name}</b> <span class="tag">${sv.transport === 'http' ? 'HTTP' : 'local'}</span>${sv.project && html` <span class="muted mono">${home(sv.project)}</span>`}
          ${sv.env_names && sv.env_names.length > 0 && html`<div class="s mono">${sv.env_names.join(' · ')}</div>`}</span>
        <button class="btn sm" disabled=${busy === sv.source + sv.name} onClick=${() => adopt(sv)}>${t("resources.page.adopter")}</button></div>`)}
    </div>`)}
    ${(data.suggested || []).length > 0 && html`<div class="card rows">${data.suggested.map(g => html`<div class="row sugg" key=${g.path}>
      <span class="mx-ico"><${Icon} n="file" /></span><div class="grow"><div class="t">${g.label}</div><div class="s mono">${home(g.path)}</div></div>
      <span class="muted">${t("resources.page.trouve_sur_cette_machine")}</span><button class="btn sm" onClick=${() => link(g.path, g.label)}>${t("resources.page.lier")}</button></div>`)}</div>`}
  </section>`;
}

export function ResourcesPage({ route }) {
  const tab = ['mcp', 'brain', 'memory'].includes(route.sub) ? route.sub : 'skills';
  const ws = useStore(app, s => s.workspace);
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>${t("resources.page.ressources")}</h1><p>${t("resources.page.definies_une_fois_dans_loom_utilisees_par_tes_projets_et_tes_disc")}</p></div></div>
    <${Tabs} value=${tab} onChange=${localT => go('resources', localT)} label="${t("resources.page.ressources")}" options=${[{ value: 'skills', label: t("resources.page.skills"), count: ws ? (ws.capabilities || []).length : null }, { value: 'mcp', label: t("resources.page.serveurs_mcp") }, { value: 'brain', label: t("resources.page.brain") }, { value: 'memory', label: t("resources.page.memoire") }]} />
    <div class="tab-body" key=${tab}>${tab === 'skills' ? html`<${Skills} />` : tab === 'mcp' ? html`<${Mcp} />` : tab === 'brain' ? html`<${Brain} />` : html`<${Memory} />`}</div>
  </div></div>`;
}
