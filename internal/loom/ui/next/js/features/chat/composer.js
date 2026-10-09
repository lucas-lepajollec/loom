import { configOptions, modeOptions } from '../harnesses/options.js';
import { t } from '../../core/i18n.js';
// Composeur : zone de saisie, pièces jointes, outils de la discussion, jauge de
// contexte, envoi/arrêt. S'adapte au mode (local natif ou discussion commune).
import { html, useState, useRef, useEffect, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Popover, Tip } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { request, get } from '../../core/api.js';

import { runtimeCaps, runtimeKind, app } from '../../core/state.js';
import { chat, send, stop, compact, compactSession, continueSession } from './engine.js';
import { prompt } from '../../ui/dialog.js';
import { currentExec } from './picker.js';
import { slashEntries, runChoice } from './slash.js';
import { attachPaste, pastedMessage, downloadPaste, MAX_MESSAGE_BYTES } from './pasted-text.js';

// Sonde du harness par runtime (commandes « / », réglages, modes) : évite de
// relancer l'agent avant la première réponse.
const probes = {};

const toolOn = n => { try { return localStorage.getItem('loom.chat.' + n) === '1'; } catch (_) { return false; } };
const setToolOn = (n, v) => { try { localStorage.setItem('loom.chat.' + n, v ? '1' : '0'); } catch (_) {} };

// Dépôt d'un fichier par morceaux de 8 Mo en base64 : le serveur attribue un id
// au premier morceau, `more:false` ferme le fichier et renvoie son chemin.
const CHUNK = 8 << 20;
const b64 = blob => new Promise((res, rej) => { const fr = new FileReader(); fr.onload = () => res(String(fr.result || '')); fr.onerror = () => rej(new Error(t("chat.composer.lecture_impossible"))); fr.readAsDataURL(blob); });
async function upload(file) {
  let id = '', off = 0, path = '';
  do {
    const end = Math.min(off + CHUNK, file.size), last = end >= file.size;
    const r = await request('/api/chat/upload', { method: 'POST', headers: { 'Content-Type': 'application/json' }, signal: new AbortController().signal,
      body: JSON.stringify({ name: file.name, data: await b64(file.slice(off, end)), id, more: !last, size: id ? 0 : file.size }) });
    const j = await r.json().catch(() => ({}));
    if (!r.ok || !j.ok) throw new Error(j.error || t("chat.composer.depot_impossible"));
    if (j.id) id = j.id;
    if (last) path = j.path;
    off = end;
  } while (off < file.size);
  return { path, name: file.name };
}

export function Composer() {
  const c = useStore(chat, s => ({ busy: s.busy, mode: s.mode, ctx: s.ctxUsed, session: s.session, context: s.context, notice: s.notice, harness: s.harness }));
  const status = useStore(app, s => s.status);
  const runtimes = useStore(app, s => s.workspace?.runtimes || []);
  const [text, setText] = useState('');
  const [files, setFiles] = useState([]);
  const [tools, setTools] = useState({ internet: toolOn('internet'), mcp: toolOn('mcp') });
  const [menu, setMenu] = useState(null);
  const ta = useRef(), fileIn = useRef(), pasteNumber = useRef(0), submitting = useRef(false);
  const exec = currentExec();
  const native = c.mode === 'native';
  // What this route can take: local and cloud get files and web search, agents
  // get files (they read them from disk) and keep their own tools.
  const kind = native ? 'native' : c.session ? runtimeKind(c.session.runtime_id) : '';
  const remote = !native && c.session && runtimeCaps(c.session.runtime_id).includes('remote');
  const canAttach = native || kind === 'local' || kind === 'cloud' || (kind === 'harness' && !remote);
  const toolList = native || kind === 'cloud' || kind === 'local' ? ['internet', 'mcp'] : [];
  const [webOn, setWebOn] = useState(null);
  useEffect(() => { get('/api/internet').then(r => setWebOn(r && r.ok !== false ? !!r.enabled : false)).catch(() => setWebOn(false)); }, []);
  const activeTools = toolList.filter(n => tools[n] && (n !== 'internet' || webOn !== false));
  const ctxMax = (status && status.ctx) || 0;
  const sc = (!native && c.session && c.session.context) || null;
  const used = native ? c.ctx : sc ? sc.used || 0 : 0, size = native ? ctxMax : sc ? sc.size || 0 : 0;
  const pct = size ? Math.min(100, used * 100 / size) : 0;
  // Native chat compacts itself at 75 %: past 90 % it is off or failing.
  const warn = native ? pct >= 90 : !!(c.session && c.session.context_warning);
  const newProject = async () => { const name = await prompt(t('chat.limit.project_title'), { message: t('chat.limit.project_text'), placeholder: t('chat.limit.project_placeholder'), ok: t('chat.limit.project_ok') }); if (name && name.trim()) continueSession(name.trim()); };

  useEffect(() => { const el = ta.current; if (!el) return; el.style.height = 'auto'; el.style.height = Math.min(el.scrollHeight, 240) + 'px'; }, [text]);

  let blocked = '';
  if (native && status && !status.health) blocked = status.load_error ? t("chat.composer.le_modele_n_a_pas_pu_se_charger") : !status.active ? t("chat.composer.moteur_arrete_charge_un_modele_pour_commencer") : status.model ? t("chat.composer.chargement_du_modele") : t("chat.composer.choisis_un_modele_pour_commencer");
  if (!native && c.session && !c.session.runtime_id) blocked = t("chat.composer.choisis_un_modele_pour_cette_discussion");
  if (!native && c.session && runtimeCaps(c.session.runtime_id).includes('workdir') && !runtimeCaps(c.session.runtime_id).includes('remote') && !((c.harness && c.harness.workdir) || c.session.workdir)) blocked = t("chat.composer.choisis_un_dossier_de_travail_dans_le_panneau_de_droite");
  if (c.context && c.context.problem) blocked = c.context.problem;


  const submit = async () => {
    const localT = pastedMessage(text, files);
    if (!localT && !files.length) return;
    if (c.busy || blocked || submitting.current) return;
    const uploaded = files.filter(f => f.path);
    if (new TextEncoder().encode(localT).length > MAX_MESSAGE_BYTES) { toast(t('chat.composer.text_too_large'), 'err'); return; }
    submitting.current = true;
    // Keep the editable draft and attachments intact until acceptance.
    try {
      const sent = await send(localT, { files: uploaded.map(f => f.path), internet: tools.internet && toolList.includes('internet'), mcp: tools.mcp && toolList.includes('mcp') });
      if (sent) { setText(current => current === text ? '' : current); setFiles(current => current.filter(f => !files.includes(f))); }
    } finally { submitting.current = false; }
  };
  const onPaste = e => {
    if (c.busy || submitting.current) return;
    const content = e.clipboardData?.getData('text/plain') || '';
    const next = attachPaste(text, e.currentTarget.selectionStart, e.currentTarget.selectionEnd, content, pasteNumber.current + 1);
    if (!next) return;
    if (new TextEncoder().encode(pastedMessage(next.text, [...files, next.file])).length > MAX_MESSAGE_BYTES || files.length >= 32) {
      e.preventDefault(); toast(t('chat.composer.text_too_large'), 'err'); return;
    }
    e.preventDefault(); pasteNumber.current++;
    setText(next.text); setFiles(current => [...current, next.file]);
    setTimeout(() => ta.current?.setSelectionRange(next.caret, next.caret), 0);
  };
  // Commandes « / » annoncées par le harness (available_commands_update).
  const rtId = !native && c.session ? c.session.runtime_id : '';
  const [, bump] = useState(0);
  // Liste lue par la sonde du harness ; relue tant qu'elle est vide (la sonde
  // tourne en arrière-plan au démarrage de Loom).
  const loadCommands = () => {
    if (!rtId || probes[rtId] === 'loading' || (probes[rtId] && (probes[rtId].commands || []).length)) return;
    probes[rtId] = 'loading';
    get('/api/runtimes/' + rtId + '/probe').then(r => { probes[rtId] = (r && r.probe) || {}; bump(x => x + 1); })
      .catch(() => { probes[rtId] = {}; });
  };
  useEffect(loadCommands, [rtId]);
  useEffect(() => { if (text.startsWith('/')) loadCommands(); }, [text.startsWith('/'), rtId]);
  // Session en cours, sinon ce que la sonde a vu.
  const probe = rtId && typeof probes[rtId] === 'object' ? probes[rtId] : {};
  const h = (!native && c.harness) || {};
  const commands = (h.commands || []).length ? h.commands : (probe.commands || []);
  const features = runtimes.find(r => r.id === rtId)?.features || probe.features;
  const harness = { features, config: configOptions(features, (h.config || []).length ? h.config : probe.config), modes: modeOptions(features, (h.modes || []).length ? h.modes : probe.modes), mode: h.mode || probe.mode || '' };
  const entries = slashEntries({ commands, session: !native && c.session, harness, rtId });
  // Commande tapée mais non annoncée par le harness (ex. /usage, propre au terminal de Claude Code).
  const typed = /^\/(\S+)/.exec(text.trim());
  const unknownCmd = typed && entries.length > 0 && !entries.some(x => x.name === typed[1]) ? typed[1] : '';
  const hint = c.notice || blocked || (unknownCmd ? '/' + unknownCmd + t("chat.composer.n_est_pas_proposee_par_ce_harness_via_loom_elle_sera_envoyee_comm") : '');

  // Premier niveau : « /mot ». Second niveau : « /commande filtre » quand la
  // commande a des choix (réglages, sessions, options de l'indice).
  const [loaded, setLoaded] = useState({});
  const [sel, setSel] = useState(0);
  const first = /^\/(\S*)$/.exec(text);
  const second = /^\/(\S+) (.*)$/s.exec(text);
  const parent = second && entries.find(e => e.name === second[1] && (e.children || e.load));
  useEffect(() => {
    if (!parent || !parent.load || loaded[parent.name]) return;
    setLoaded(l => ({ ...l, [parent.name]: 'loading' }));
    parent.load().then(items => setLoaded(l => ({ ...l, [parent.name]: items })), () => setLoaded(l => ({ ...l, [parent.name]: [] })));
  }, [parent && parent.name]);
  useEffect(() => setLoaded({}), [rtId, c.session && c.session.id]);
  const low = v => String(v || '').toLowerCase();
  let rows = [], level = null;
  if (first) {
    const q = low(first[1]);
    const rank = x => (low(x.name).startsWith(q) ? 2 : 0) + (x.children || x.load ? 1 : 0);
    rows = entries.filter(x => low(x.name).includes(q)).sort((a, b) => rank(b) - rank(a)).slice(0, 40);
  } else if (parent) {
    const q = low(second[2]).trim();
    const items = parent.children || (Array.isArray(loaded[parent.name]) ? loaded[parent.name] : []);
    level = { name: parent.name, loading: !parent.children && loaded[parent.name] !== undefined && !Array.isArray(loaded[parent.name]), empty: !items.length };
    rows = items.filter(x => !q || low(x.label).includes(q) || low(x.description).includes(q)).slice(0, 60);
  }
  useEffect(() => setSel(0), [first ? 'a' : parent ? 'b' + parent.name : '', rows.length]);
  const focus = () => ta.current && ta.current.focus();
  const choose = async x => {
    setSel(0);
    if (!level) { setText('/' + x.name + ' '); focus(); return; }
    if (x.insert && !x.complete) { setText(x.insert + ' '); focus(); return; }
    if (x.insert) { setText(''); const ok = await send(x.insert, {}); if (!ok) setText(x.insert); return; }
    setText(''); await runChoice(x.run, { session: c.session, rtId }); focus();
  };
  const onKey = e => {
    if (level && e.key === 'Escape') { e.preventDefault(); setText('/' + level.name); return; }
    if (rows.length) {
      if (e.key === 'ArrowDown') { e.preventDefault(); setSel((sel + 1) % rows.length); return; }
      if (e.key === 'ArrowUp') { e.preventDefault(); setSel((sel - 1 + rows.length) % rows.length); return; }
      const pick = rows[Math.min(sel, rows.length - 1)];
      const leaf = !level && !(pick.children || pick.load);
      if ((e.key === 'Tab' || (e.key === 'Enter' && !leaf)) && !e.shiftKey && !e.isComposing) { e.preventDefault(); choose(pick); return; }
      if (e.key === 'Enter' && leaf && !e.shiftKey && first && first[1] !== pick.name) { e.preventDefault(); choose(pick); return; }
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); submit(); }
  };
  const workdir = !native && ((c.harness && c.harness.workdir) || (c.session && c.session.workdir)) || '';
  const pick = async e => {
    for (const f of e.target.files || []) {
      try { const up = await upload(f); setFiles(x => [...x, up]); } catch (err) { toast(f.name + ' : ' + err.message, 'err'); }
    }
    e.target.value = '';
  };
  const toggleTool = n => { const v = !tools[n]; setToolOn(n, v); setTools({ ...tools, [n]: v }); };

  return html`<div class="composer-wrap">
    ${(rows.length > 0 || level) && html`<div class="slash" role="listbox" aria-label=${level ? '/' + level.name : t("chat.composer.commandes")}>
      ${level && html`<div class="slash-head"><button type="button" aria-label="${t("chat.composer.retour")}" onMouseDown=${e => { e.preventDefault(); setText('/'); }}><${Icon} n="left" /></button><b>/${level.name}</b>
        <span>${level.loading ? t("chat.composer.chargement") : level.empty ? t("chat.composer.aucun_choix_propose") : t("chat.composer.echap_pour_revenir")}</span></div>`}
      ${rows.map((x, i) => html`<button type="button" role="option" aria-selected=${String(i === sel)} class=${cls('slash-row', level && 'sub', i === sel && 'on')} onMouseDown=${e => { e.preventDefault(); choose(x); }}>
        ${level ? html`<b>${x.label}</b>${x.current && html`<em>${t("chat.composer.actuel")}</em>`}<span>${x.description || ''}</span>`
          : html`<b>/${x.name}</b>${x.hint && html`<em>${x.hint}</em>`}<span>${x.description || ''}</span>${(x.children || x.load) && html`<${Icon} n="right" />`}`}</button>`)}</div>`}
    ${warn && html`<div class="ctx-warn" role="status"><${Icon} n="alert" /><div class="grow"><b>${t('chat.limit.title', { pct: Math.round(pct) || 85 })}</b><span>${t('chat.limit.text')} <a href="#/settings/general">${t('chat.limit.setting')}</a></span></div>
      <div class="ctx-warn-acts"><button class="btn sm" onClick=${native ? compact : compactSession}>${t('chat.limit.compact')}</button>
        ${!native && html`<button class="btn sm ghost" onClick=${() => continueSession()}>${t(c.session && c.session.project_id ? 'chat.limit.continue_project' : 'chat.limit.continue')}</button>
          ${!(c.session && c.session.project_id) && html`<button class="btn sm ghost" onClick=${newProject}>${t('chat.limit.new_project')}</button>`}`}</div></div>`}
    <div class="composer">
      ${files.length ? html`<div class="attach-row">${files.map((f, i) => html`<span class="file-pill"><${Icon} n="file" />${f.name}${typeof f.content === 'string' && html`<button title=${t('chat.composer.download_text')} aria-label=${t('chat.composer.download_text')} onClick=${() => downloadPaste(f)}><${Icon} n="download" /></button>`}<button aria-label="${t("chat.composer.retirer")}" onClick=${() => setFiles(files.filter((_, j) => j !== i))}><${Icon} n="close" /></button></span>`)}</div>` : ''}
      <textarea ref=${ta} rows="1" value=${text} onInput=${e => setText(e.target.value)} onKeyDown=${onKey} onPaste=${onPaste}
        placeholder=${exec.name ? t("chat.composer.ecrire_a") + exec.name + '…' : t('chat.composer.placeholder')} aria-label="${t("chat.composer.message")}"></textarea>
      <div class="composer-bar">
        ${canAttach && html`<button class="icon-btn" aria-label="${t("chat.composer.joindre_un_fichier")}" title=${kind === 'harness' ? t('chat.composer.attach_agent') : t("chat.composer.joindre")} onClick=${() => fileIn.current.click()}><${Icon} n="paperclip" /></button>
          <input type="file" multiple hidden ref=${fileIn} onChange=${pick} />`}
        ${toolList.length > 0 && html`<button class=${cls('chip-btn', activeTools.length && 'on')} onClick=${e => setMenu(e.currentTarget)}><${Icon} n="sliders" />${t("chat.composer.outils")}${activeTools.length ? html` <span class="n">${activeTools.length}</span>` : ''}</button>`}
        ${workdir && html`<button class="chip-btn" title=${workdir} onClick=${() => app.set({ inspector: true })}><${Icon} n="folder" />${workdir.split('/').pop()}</button>`}
        ${entries.length > 0 && !text && html`<span class="composer-tip">${t("chat.composer.pour_les_commandes")}</span>`}
        <span class="grow"></span>
        <button class="icon-btn vm-open" aria-label=${t('vm.open')} title=${t('vm.open')} onClick=${() => app.set({ voiceMode: { discussion: chat.get().sessionId || (app.get().nav && app.get().nav.active) || '', internet: tools.internet } })}><${Icon} n="mic" /></button>
        ${size ? html`<button class=${cls('ctx', pct >= 85 && 'hot')} title=${t("chat.composer.contexte_utilise") + used + ' / ' + size + ' tokens'} onClick=${native ? compact : compactSession}>
          <span class="ctx-ring" style=${`--p:${pct}`}></span><span>${fmtTok(used)} / ${fmtTok(size)}</span></button>` : ''}
        ${c.busy ? html`<button class="send stop" aria-label="${t("chat.composer.arreter")}" onClick=${stop}><${Icon} n="stop" /></button>`
          : html`<button class="send" aria-label="${t("chat.composer.envoyer")}" disabled=${!!blocked || (!text.trim() && !files.length)} onClick=${submit}><${Icon} n="arrowUp" /></button>`}
      </div>
    </div>
    <div class=${cls('composer-hint', hint && 'warn')}>${hint || t("chat.composer.entree_pour_envoyer_maj_entree_pour_une_nouvelle_ligne")}</div>
    ${menu && html`<${Popover} anchor=${menu} onClose=${() => setMenu(null)} place="above" width=${240}>
      <button class="item" disabled=${webOn === false} onClick=${() => toggleTool('internet')}><${Icon} n="globe" />${t("chat.composer.recherche_web")}<${Tip} text=${webOn === false ? t('chat.tools.web_off') : t('chat.tools.web_tip')} /><span class="grow"></span>${tools.internet && webOn !== false && html`<${Icon} n="check" />`}</button>
      ${toolList.includes('mcp') && html`<button class="item" onClick=${() => toggleTool('mcp')}><${Icon} n="plug" />${t("chat.composer.outils_mcp")}<${Tip} text=${t('chat.tools.mcp_tip')} /><span class="grow"></span>${tools.mcp && html`<${Icon} n="check" />`}</button>`}
      <a class="item sub" href="#/settings/internet" onClick=${() => setMenu(null)}><${Icon} n="gear" />${t('chat.tools.settings')}</a>
    </${Popover}>`}
  </div>`;
}
