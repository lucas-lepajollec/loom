// Composeur : zone de saisie, pièces jointes, outils de la discussion, jauge de
// contexte, envoi/arrêt. S'adapte au mode (local natif ou discussion commune).
import { html, useState, useRef, useEffect, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Popover } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { request } from '../../core/api.js';
import { runtimeCaps, app } from '../../core/state.js';
import { chat, send, stop, compact } from './engine.js';
import { currentExec } from './picker.js';

const toolOn = n => { try { return localStorage.getItem('loom.chat.' + n) === '1'; } catch (_) { return false; } };
const setToolOn = (n, v) => { try { localStorage.setItem('loom.chat.' + n, v ? '1' : '0'); } catch (_) {} };

// Dépôt d'un fichier par morceaux de 8 Mo en base64 : le serveur attribue un id
// au premier morceau, `more:false` ferme le fichier et renvoie son chemin.
const CHUNK = 8 << 20;
const b64 = blob => new Promise((res, rej) => { const fr = new FileReader(); fr.onload = () => res(String(fr.result || '')); fr.onerror = () => rej(new Error('lecture impossible')); fr.readAsDataURL(blob); });
async function upload(file) {
  let id = '', off = 0, path = '';
  do {
    const end = Math.min(off + CHUNK, file.size), last = end >= file.size;
    const r = await request('/api/chat/upload', { method: 'POST', headers: { 'Content-Type': 'application/json' }, signal: new AbortController().signal,
      body: JSON.stringify({ name: file.name, data: await b64(file.slice(off, end)), id, more: !last, size: id ? 0 : file.size }) });
    const j = await r.json().catch(() => ({}));
    if (!r.ok || !j.ok) throw new Error(j.error || 'dépôt impossible');
    if (j.id) id = j.id;
    if (last) path = j.path;
    off = end;
  } while (off < file.size);
  return { path, name: file.name };
}

export function Composer() {
  const c = useStore(chat, s => ({ busy: s.busy, mode: s.mode, ctx: s.ctxUsed, session: s.session, context: s.context, notice: s.notice }));
  const status = useStore(app, s => s.status);
  const [text, setText] = useState('');
  const [files, setFiles] = useState([]);
  const [tools, setTools] = useState({ internet: toolOn('internet'), mcp: toolOn('mcp') });
  const [menu, setMenu] = useState(null);
  const ta = useRef(), fileIn = useRef();
  const exec = currentExec();
  const native = c.mode === 'native';
  const ctxMax = (status && status.ctx) || 0;
  const pct = ctxMax ? Math.min(100, c.ctx * 100 / ctxMax) : 0;

  useEffect(() => { const el = ta.current; if (!el) return; el.style.height = 'auto'; el.style.height = Math.min(el.scrollHeight, 240) + 'px'; }, [text]);

  let blocked = '';
  if (native && status && !status.health) blocked = status.load_error ? 'Le modèle n’a pas pu se charger.' : !status.active ? 'Moteur arrêté : charge un modèle pour commencer.' : status.model ? 'Chargement du modèle…' : 'Choisis un modèle pour commencer.';
  if (!native && c.session && !c.session.runtime_id) blocked = 'Choisis un modèle pour cette discussion.';
  if (!native && c.session && runtimeCaps(c.session.runtime_id).includes('workdir') && !((c.harness && c.harness.workdir) || c.session.workdir)) blocked = 'Choisis un dossier de travail dans le panneau de droite.';
  if (c.context && c.context.problem) blocked = c.context.problem;
  const hint = c.notice || blocked;

  const submit = async () => {
    const t = text.trim();
    if (!t && !files.length) return;
    if (c.busy || blocked) return;
    if (!native && files.length) { toast('Ce mode accepte uniquement du texte.'); return; }
    setText('');
    const sent = await send(t, { files: native ? files.map(f => f.path) : [], internet: tools.internet, mcp: tools.mcp });
    if (sent) setFiles([]); else setText(t);
  };
  const onKey = e => { if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); submit(); } };
  const pick = async e => {
    for (const f of e.target.files || []) {
      try { const up = await upload(f); setFiles(x => [...x, up]); } catch (err) { toast(f.name + ' : ' + err.message, 'err'); }
    }
    e.target.value = '';
  };
  const toggleTool = n => { const v = !tools[n]; setToolOn(n, v); setTools({ ...tools, [n]: v }); };

  return html`<div class="composer-wrap">
    <div class="composer">
      ${files.length ? html`<div class="attach-row">${files.map((f, i) => html`<span class="file-pill"><${Icon} n="file" />${f.name}<button aria-label="Retirer" onClick=${() => setFiles(files.filter((_, j) => j !== i))}><${Icon} n="close" /></button></span>`)}</div>` : ''}
      <textarea ref=${ta} rows="1" value=${text} onInput=${e => setText(e.target.value)} onKeyDown=${onKey}
        placeholder=${exec.name ? 'Écrire à ' + exec.name + '…' : 'Écrire un message…'} aria-label="Message"></textarea>
      <div class="composer-bar">
        ${native && html`<button class="icon-btn" aria-label="Joindre un fichier" title="Joindre" onClick=${() => fileIn.current.click()}><${Icon} n="paperclip" /></button>
          <input type="file" multiple hidden ref=${fileIn} onChange=${pick} />
          <button class=${cls('chip-btn', (tools.internet || tools.mcp) && 'on')} onClick=${e => setMenu(e.currentTarget)}><${Icon} n="sliders" />Outils${tools.internet || tools.mcp ? html` <span class="n">${(tools.internet ? 1 : 0) + (tools.mcp ? 1 : 0)}</span>` : ''}</button>`}
        <span class="grow"></span>
        ${native && ctxMax ? html`<button class="ctx" title=${'Contexte utilisé : ' + c.ctx + ' / ' + ctxMax + ' tokens'} onClick=${compact}>
          <span class="ctx-ring" style=${`--p:${pct}`}></span><span>${fmtTok(c.ctx)} / ${fmtTok(ctxMax)}</span></button>` : ''}
        ${c.busy ? html`<button class="send stop" aria-label="Arrêter" onClick=${stop}><${Icon} n="stop" /></button>`
          : html`<button class="send" aria-label="Envoyer" disabled=${!!blocked || (!text.trim() && !files.length)} onClick=${submit}><${Icon} n="arrowUp" /></button>`}
      </div>
    </div>
    <div class=${cls('composer-hint', hint && 'warn')}>${hint || 'Entrée pour envoyer · Maj+Entrée pour une nouvelle ligne'}</div>
    ${menu && html`<${Popover} anchor=${menu} onClose=${() => setMenu(null)} place="above" width=${240}>
      <button class="item" onClick=${() => toggleTool('internet')}><${Icon} n="globe" />Recherche web<span class="grow"></span>${tools.internet && html`<${Icon} n="check" />`}</button>
      <button class="item" onClick=${() => toggleTool('mcp')}><${Icon} n="plug" />Outils MCP<span class="grow"></span>${tools.mcp && html`<${Icon} n="check" />`}</button>
    </${Popover}>`}
  </div>`;
}
