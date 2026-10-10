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

function ServerCard({ s: sv, onToggle, onTest, onEdit, onDelete }) {
  const [anchor, setAnchor] = useState(null);
  return html`<div class="mcard mcp-card">
    <div class="mcard-h"><span class="mx-ico"><${Icon} n="plug" /></span>
      <span class="grow"><b>${sv.name}</b><small>${sv.transport === 'http' ? 'HTTP' : t('mcp.local')}${sv.enabled && !sv.error ? ' · ' + t('mcp.tools', { n: (sv.tools || []).length }) : ''}</small></span>
      <${Switch} checked=${sv.enabled} label=${t("resources.page.activer") + sv.name} onChange=${onToggle} />
      <button class="icon-btn" aria-label=${t("resources.page.actions")} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${Icon} n="more" /></button></div>
    <div class="ws-card-f">${sv.error ? html`<span class="bs-state err" title=${sv.error}><i class="dot red"></i>${t('mcp.card.error')}</span>` : sv.enabled ? html`<span class="bs-state"><i class=${'dot ' + (sv.connected ? 'green' : 'amber')}></i>${sv.connected ? t('mcp.card.connected') : t('mcp.card.idle')}</span>${(sv.disabled || []).length ? html`<span class="muted">${sv.disabled.length}${t("resources.page.masques")}</span>` : ''}` : html`<span class="muted">${t("resources.page.desactive")}</span>`}</div>
    ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${[{ label: t("resources.page.tester"), icon: 'play', run: onTest }, { label: t("resources.page.modifier"), icon: 'edit', run: onEdit }, '-', { label: t("resources.page.retirer"), icon: 'trash', danger: true, run: onDelete }]} />`}
  </div>`;
}

export function Mcp({ q = '' }) {
  const [list, setList] = useState(null);
  const [dlg, setDlg] = useState(null);
  const load = () => get('/api/mcp').then(r => setList(r.servers || [])).catch(() => {});
  useEffect(() => { load(); }, []);
  const toggle = async (s, on) => { const r = await post('/api/mcp/toggle', { name: s.name, on }); if (r.ok === false) toast(r.error, 'err'); load(); };
  const del = async s => { if (!await confirm(t("resources.page.retirer_le_serveur"), '« ' + s.name + t("resources.page.et_ses_outils_seront_retires"), { ok: t("resources.page.retirer"), danger: true })) return; await post('/api/mcp/delete', { name: s.name }); load(); };
  const test = async s => { const r = await post('/api/mcp/test', { name: s.name }); toast(r.ok ? (s.name + t("resources.page.repond") + ((r.tools || []).length || (r.server && r.server.tools || []).length || 0) + t("resources.page.outils")) : (r.error || t("resources.page.echec")), r.ok ? '' : 'err'); load(); };
  return html`<div class="bs">
    <${McpFile} />
    <section class="sec"><div class="sec-h"><h2>${t('mcp.servers')}${list && list.length > 0 && html` <span class="count">${list.length}</span>`}<${Tip} text=${t('mcp.servers_tip')} /></h2></div>
      <${Portable} onImported=${load} />
      ${list === null ? html`<div class="skeleton" style="height:120px"></div>` : html`<div class="mcards">
        ${list.filter(s => !q.trim() || s.name.toLowerCase().includes(q.trim().toLowerCase())).map(s => html`<${ServerCard} key=${s.name} s=${s} onToggle=${v => toggle(s, v)} onTest=${() => test(s)} onEdit=${() => setDlg({ server: s })} onDelete=${() => del(s)} />`)}
        <button type="button" class="mcard add" onClick=${() => setDlg({})}><${Icon} n="plus" /><span>${t("resources.page.ajouter_un_serveur")}</span><small>${t('mcp.card.add_note')}</small></button></div>`}
    </section>
    <${Gateway} />
    <${McpSources} onAdopted=${load} />
    ${dlg && html`<${McpEditor} server=${dlg.server} onClose=${() => setDlg(null)} onSaved=${setList} />`}
  </div>`;
}

// Le fichier mcp.json de Loom : modifiable à la main, relu automatiquement.
function McpFile() {
  const [f, setF] = useState(null);
  useEffect(() => { get('/api/mcp/file').then(setF).catch(() => setF(null)); }, []);
  if (!f || !f.path) return null;
  return html`<div class="card loc-strip"><span class="mono-tile"><${Icon} n="box" /></span>
    <div class="grow"><div class="bs-name">${t('mcp.loc.title')}</div><div class="bs-sub"><span class="mono trunc" title=${f.path}>${home(f.path)}</span><span>${t('mcp.loc.note')}</span>${f.error && html`<span class="bs-state err" title=${f.error}>${t("resources.page.fichier_invalide_la_derniere_version_correcte_reste_utilisee")}</span>`}</div></div></div>`;
}

// Reprendre depuis tes agents : Loom lit (sans les modifier) les configurations
// MCP d'autres outils et copie un serveur choisi dans Loom, désactivé. L'entrée
// « loom » de la passerelle n'est jamais proposée.
function McpSources({ onAdopted }) {
  const [data, setData] = useState(null);
  const [busy, setBusy] = useState('');
  const load = () => get('/api/mcp/sources').then(setData).catch(() => setData(null));
  useEffect(() => { load(); }, []);
  const link = async (path, label, unlink) => {
    setBusy(path);
    const r = await post('/api/mcp/sources', { path, label: label || '', action: unlink ? 'unlink' : 'link' }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
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
  const sources = (data.sources || []).map(src => ({ ...src, servers: (src.servers || []).filter(sv => sv.name !== 'loom') }));
  const sugg = data.suggested || [];
  return html`<section class="sec"><div class="sec-h"><h2>${t('mcp.import.title')}<${Tip} text=${t('mcp.import.tip')} /></h2><span class="grow"></span>
      <button class="btn sm ghost" onClick=${addFile}><${Icon} n="file" />${t('mcp.import.file')}</button></div>
    ${sources.length || sugg.length ? html`<div class="card bs-list">
      ${sources.map(src => html`<div class="mcp-src-b" key=${src.path}>
        <div class="bs-row"><span class="mono-tile"><${Icon} n="file" /></span>
          <div class="grow"><div class="bs-name">${src.label}</div><div class="bs-sub"><span class="mono trunc">${home(src.path)}</span>${src.error ? html`<span class="bs-state err" title=${src.error}>${t("resources.page.illisible")}</span>` : html`<span>${t('mcp.import.count', { n: src.servers.length })}</span>`}</div></div>
          <button class="btn sm ghost" onClick=${() => link(src.path, '', true)}>${t('mcp.import.hide')}</button></div>
        ${src.servers.map(sv => html`<div class="bs-row mcp-sv" key=${sv.name + (sv.project || '')}>
          <div class="grow"><div class="bs-name">${sv.name}<span class="tag">${sv.transport === 'http' ? 'HTTP' : t('mcp.local')}</span></div>${(sv.project || (sv.env_names || []).length > 0) && html`<div class="bs-sub">${sv.project && html`<span class="mono trunc">${home(sv.project)}</span>`}${(sv.env_names || []).length > 0 && html`<span class="mono">${sv.env_names.join(' · ')}</span>`}</div>`}</div>
          <button class="btn sm" disabled=${busy === sv.source + sv.name} onClick=${() => adopt(sv)}>${t('mcp.import.take')}</button></div>`)}
      </div>`)}
      ${sugg.map(g => html`<div class="bs-row" key=${g.path}><span class="mono-tile"><${Icon} n="file" /></span>
        <div class="grow"><div class="bs-name">${g.label}<span class="tag">${t('skills.linked.found')}</span></div><div class="bs-sub"><span class="mono trunc">${home(g.path)}</span></div></div>
        <button class="btn sm" disabled=${busy === g.path} onClick=${() => link(g.path, g.label)}>${t('mcp.import.show')}</button></div>`)}
    </div>` : html`<div class="card bs-none"><div class="grow"><p>${t('mcp.import.empty')}</p></div></div>`}
  </section>`;
}
