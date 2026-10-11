import { t } from '../../core/i18n.js';
// Composeur : zone de saisie, pièces jointes, outils de la discussion, jauge de
// contexte, envoi/arrêt. S'adapte au mode (local natif ou discussion commune).
import { html, useState, useRef, useEffect, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Popover, Tip } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { request, get } from '../../core/api.js';
import { validData } from '../../core/shape.js';

import { runtimeCaps, runtimeKind, app } from '../../core/state.js';
import { chat, send, stop, compact, compactSession, continueSession } from './engine.js';
import { prompt } from '../../ui/dialog.js';
import { currentExec } from './picker.js';
import { attachPaste, pastedMessage, downloadPaste, MAX_MESSAGE_BYTES } from './pasted-text.js';

// Sonde du harness par runtime (commandes « / », réglages, modes) : évite de
// relancer l'agent avant la première réponse.

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
    if (!validData(j, '/api/chat/upload') || (!last && typeof j.id !== 'string') || (last && typeof j.path !== 'string')) throw new Error(t('chat.composer.depot_impossible'));
    if (j.id) id = j.id;
    if (last) path = j.path;
    off = end;
  } while (off < file.size);
  return { path, name: file.name };
}

export function Composer() {
  const c = useStore(chat, s => ({ busy: s.busy, mode: s.mode, ctx: s.ctxUsed, session: s.session, context: s.context, notice: s.notice }));
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
  const canAttach = native || kind === 'local' || kind === 'cloud';
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
  if (!native && c.session && runtimeKind(c.session.runtime_id) === 'retired') blocked = t('runtime.retired');
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
  const hint = c.notice || blocked;
  const onKey = e => { if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); submit(); } };
  const workdir = !native && (c.session && c.session.workdir) || '';
  const pick = async e => {
    for (const f of e.target.files || []) {
      try { const up = await upload(f); setFiles(x => [...x, up]); } catch (err) { toast(f.name + ' : ' + err.message, 'err'); }
    }
    e.target.value = '';
  };
  const toggleTool = n => { const v = !tools[n]; setToolOn(n, v); setTools({ ...tools, [n]: v }); };

  return html`<div class="composer-wrap">
    <div class="composer">
      ${files.length ? html`<div class="attach-row">${files.map((f, i) => html`<span class="file-pill"><${Icon} n="file" />${f.name}${typeof f.content === 'string' && html`<button title=${t('chat.composer.download_text')} aria-label=${t('chat.composer.download_text')} onClick=${() => downloadPaste(f)}><${Icon} n="download" /></button>`}<button aria-label="${t("chat.composer.retirer")}" onClick=${() => setFiles(files.filter((_, j) => j !== i))}><${Icon} n="close" /></button></span>`)}</div>` : ''}
      <textarea ref=${ta} rows="1" value=${text} onInput=${e => setText(e.target.value)} onKeyDown=${onKey} onPaste=${onPaste}
        placeholder=${exec.name ? t("chat.composer.ecrire_a") + exec.name + '…' : t('chat.composer.placeholder')} aria-label="${t("chat.composer.message")}"></textarea>
      <div class="composer-bar">
        ${canAttach && html`<button class="icon-btn" aria-label="${t("chat.composer.joindre_un_fichier")}" title=${t("chat.composer.joindre")} onClick=${() => fileIn.current.click()}><${Icon} n="paperclip" /></button>
          <input type="file" multiple hidden ref=${fileIn} onChange=${pick} />`}
        ${toolList.length > 0 && html`<button class=${cls('chip-btn', activeTools.length && 'on')} onClick=${e => setMenu(e.currentTarget)}><${Icon} n="sliders" />${t("chat.composer.outils")}${activeTools.length ? html` <span class="n">${activeTools.length}</span>` : ''}</button>`}
        ${workdir && html`<button class="chip-btn" title=${workdir} onClick=${() => app.set({ inspector: true })}><${Icon} n="folder" />${workdir.split('/').pop()}</button>`}
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
