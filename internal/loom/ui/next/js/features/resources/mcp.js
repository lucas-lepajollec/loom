// Outils MCP : définis une fois dans Loom (secrets chiffrés chez Loom, liste
// sans secret dans le cerveau), donnés aux agents par une seule entrée « loom ».
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Empty, Menu, Tip } from '../../ui/controls.js';
import { Logo } from '../../ui/logo.js';
import { Modal, confirm, prompt, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';

const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');

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

// Passerelle : une entrée « loom » dans la configuration de chaque agent donne
// tous les serveurs, la mémoire et les skills, sans recopier chaque serveur.
function Gateway() {
  const [g, setG] = useState(null);
  const [busy, setBusy] = useState('');
  const [anchor, setAnchor] = useState(null);
  const load = () => get('/api/mcp/gateway').then(r => setG(r.ok === false ? { error: r.error } : r)).catch(e => setG({ error: e.message }));
  useEffect(() => { load(); }, []);
  const toggle = async (h, enabled) => {
    if (enabled && !await confirm(t('mcp.gw.add_title', { name: h.name }), t('mcp.gw.add_text', { file: home(h.file) }), { ok: t('mcp.gw.add_ok') })) return;
    setBusy(h.id);
    const r = await post('/api/mcp/gateway', { harness: h.id, enabled }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (r.ok === false || r.error) toast(r.error || t('resources.page.impossible'), 'err');
    load();
  };
  const rotate = async () => {
    if (!await confirm(t('mcp.gw.rotate'), t('mcp.gw.rotate_text'), { ok: t('mcp.gw.rotate') })) return;
    const r = await post('/api/mcp/gateway/token', { rotate: true }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false || r.error) return toast(r.error, 'err');
    toast(t('mcp.gw.rotated')); load();
  };
  if (!g) return html`<div class="skeleton" style="height:120px"></div>`;
  if (g.error) return html`<p class="note err">${g.error}</p>`;
  const list = g.harnesses || [];
  return html`<section class="sec"><div class="sec-h"><h2>${t('mcp.gw.title')}<${Tip} text=${t('mcp.gw.tip')} /></h2><span class="grow"></span>
      <button class="icon-btn" aria-label=${t('brain.src.actions')} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${Icon} n="more" /></button>
      ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${[{ icon: 'key', label: t('mcp.gw.rotate'), run: rotate }]} />`}</div>
    <div class="card bs-list">${list.map(h => html`<div class="bs-row" key=${h.id}>
      <${Logo} name=${h.id} />
      <div class="grow"><div class="bs-name">${h.name}</div><div class="bs-sub">${h.supported ? html`<span class="mono trunc">${home(h.file)}</span>${h.registered && html`<span class="bs-state"><i class="dot green"></i>${t('mcp.gw.on')}</span>`}` : html`<span>${t('mcp.gw.unsupported')}</span>`}</div></div>
      <${Switch} checked=${!!h.registered} disabled=${!h.supported || busy === h.id} label=${t('mcp.gw.title') + ' · ' + h.name} onChange=${on => toggle(h, on)} /></div>`)}
      <div class="bs-row mcp-url"><span class="grow"><span class="muted">${t('mcp.gw.url')}</span> <code class="mono">${g.url}</code></span></div></div>
  </section>`;
}

// Serveurs trouvés dans la liste portable du cerveau et absents ici.
function Portable({ onImported }) {
  const [p, setP] = useState(null);
  const load = () => get('/api/mcp/portable').then(r => setP(r.ok === false ? null : r)).catch(() => setP(null));
  useEffect(() => { load(); }, []);
  const entries = (p && p.servers) || [];
  if (!entries.length) return null;
  const names = entries.map(e => e.name || e);
  const run = async () => {
    const r = await post('/api/mcp/portable/import', { names }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false || r.error) return toast(r.error, 'err');
    toast(t('mcp.portable.done', { n: names.length })); load(); onImported();
  };
  return html`<div class="card bs-none mcp-portable"><span class="mono-tile"><${Icon} n="brain" /></span><div class="grow"><b>${t('mcp.portable.title', { n: names.length })}</b><p>${names.join(', ')} · ${t('mcp.portable.note')}</p></div><button class="btn sm primary" onClick=${run}>${t('mcp.portable.import')}</button></div>`;
}

function ServerRow({ s, onToggle, onTest, onEdit, onDelete }) {
  const [anchor, setAnchor] = useState(null);
  return html`<div class="bs-row">
    <i class=${'dot ' + (!s.enabled ? '' : s.connected ? 'green' : s.error ? 'red' : 'amber')}></i>
    <div class="grow"><div class="bs-name">${s.name}<span class="tag">${s.transport === 'http' ? 'HTTP' : t('mcp.local')}</span></div>
      <div class="bs-sub">${s.error ? html`<span class="bs-state err">${s.error}</span>` : s.enabled ? html`<span>${t('mcp.tools', { n: (s.tools || []).length })}${(s.disabled || []).length ? ' · ' + s.disabled.length + t("resources.page.masques") : ''}</span>` : html`<span>${t("resources.page.desactive")}</span>`}</div></div>
    <${Switch} checked=${s.enabled} label=${t("resources.page.activer") + s.name} onChange=${onToggle} />
    <button class="icon-btn" aria-label=${t("resources.page.actions")} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${Icon} n="more" /></button>
    ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${[{ label: t("resources.page.tester"), icon: 'play', run: onTest }, { label: t("resources.page.modifier"), icon: 'edit', run: onEdit }, '-', { label: t("resources.page.retirer"), icon: 'trash', danger: true, run: onDelete }]} />`}
  </div>`;
}

export function Mcp() {
  const [list, setList] = useState(null);
  const [dlg, setDlg] = useState(null);
  const load = async () => { const r = await get('/api/mcp'); setList(r.servers || []); };
  useEffect(() => { load(); }, []);
  const toggle = async (s, on) => { const r = await post('/api/mcp/toggle', { name: s.name, on }); if (r.ok === false) toast(r.error, 'err'); load(); };
  const del = async s => { if (!await confirm(t("resources.page.retirer_le_serveur"), '« ' + s.name + t("resources.page.et_ses_outils_seront_retires"), { ok: t("resources.page.retirer"), danger: true })) return; await post('/api/mcp/delete', { name: s.name }); load(); };
  const test = async s => { const r = await post('/api/mcp/test', { name: s.name }); toast(r.ok ? (s.name + t("resources.page.repond") + ((r.tools || []).length || (r.server && r.server.tools || []).length || 0) + t("resources.page.outils")) : (r.error || t("resources.page.echec")), r.ok ? '' : 'err'); load(); };
  return html`<div class="bs">
    <${Gateway} />
    <section class="sec"><div class="sec-h"><h2>${t('mcp.servers')}${list && list.length > 0 && html` <span class="count">${list.length}</span>`}<${Tip} text=${t('mcp.servers_tip')} /></h2><span class="grow"></span>
        <button class="btn sm primary" onClick=${() => setDlg({})}><${Icon} n="plus" />${t("resources.page.ajouter_un_serveur")}</button></div>
      <${Portable} onImported=${load} />
      ${list === null ? html`<div class="skeleton" style="height:120px"></div>` : list.length ? html`<div class="card bs-list">${list.map(s => html`<${ServerRow} key=${s.name} s=${s} onToggle=${v => toggle(s, v)} onTest=${() => test(s)} onEdit=${() => setDlg({ server: s })} onDelete=${() => del(s)} />`)}</div>`
        : html`<div class="card"><${Empty} icon="plug" title="${t("resources.page.aucun_serveur_mcp")}" text=${t('mcp.empty')}><button class="btn primary" onClick=${() => setDlg({})}>${t("resources.page.ajouter_un_serveur")}</button></${Empty}></div>`}
    </section>
    <details class="brain-advanced mcp-more"><summary>${t('mcp.files')}</summary><${McpFile} /><${McpSources} onAdopted=${load} /></details>
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

