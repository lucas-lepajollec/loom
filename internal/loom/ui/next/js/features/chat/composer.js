// Composeur : zone de saisie, pièces jointes, outils de la discussion, jauge de
// contexte, envoi/arrêt. S'adapte au mode (local natif ou discussion commune).
import { html, useState, useRef, useEffect, useStore, cls, fmtTok } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Popover } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { request, get } from '../../core/api.js';

import { runtimeCaps, app } from '../../core/state.js';
import { chat, send, stop, compact } from './engine.js';
import { currentExec } from './picker.js';
import { slashEntries, runChoice } from './slash.js';

// Sonde du harness par runtime (commandes « / », réglages, modes) : évite de
// relancer l'agent avant la première réponse.
const probes = {};

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
  const c = useStore(chat, s => ({ busy: s.busy, mode: s.mode, ctx: s.ctxUsed, session: s.session, context: s.context, notice: s.notice, harness: s.harness }));
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


  const submit = async () => {
    const t = text.trim();
    if (!t && !files.length) return;
    if (c.busy || blocked) return;
    if (!native && files.length) { toast('Ce mode accepte uniquement du texte.'); return; }
    setText('');
    const sent = await send(t, { files: native ? files.map(f => f.path) : [], internet: tools.internet, mcp: tools.mcp });
    if (sent) setFiles([]); else setText(t);
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
  const harness = { config: (h.config || []).length ? h.config : (probe.config || []), modes: (h.modes || []).length ? h.modes : (probe.modes || []), mode: h.mode || probe.mode || '' };
  const entries = slashEntries({ commands, session: !native && c.session, harness, rtId });
  // Commande tapée mais non annoncée par le harness (ex. /usage, propre au terminal de Claude Code).
  const typed = /^\/(\S+)/.exec(text.trim());
  const unknownCmd = typed && entries.length > 0 && !entries.some(x => x.name === typed[1]) ? typed[1] : '';
  const hint = c.notice || blocked || (unknownCmd ? '/' + unknownCmd + ' n’est pas proposée par ce harness via Loom : elle sera envoyée comme un message normal.' : '');

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
    ${(rows.length > 0 || level) && html`<div class="slash" role="listbox" aria-label=${level ? '/' + level.name : 'Commandes'}>
      ${level && html`<div class="slash-head"><button type="button" aria-label="Retour" onMouseDown=${e => { e.preventDefault(); setText('/'); }}><${Icon} n="left" /></button><b>/${level.name}</b>
        <span>${level.loading ? 'Chargement…' : level.empty ? 'Aucun choix proposé' : 'Échap pour revenir'}</span></div>`}
      ${rows.map((x, i) => html`<button type="button" role="option" aria-selected=${String(i === sel)} class=${cls('slash-row', level && 'sub', i === sel && 'on')} onMouseDown=${e => { e.preventDefault(); choose(x); }}>
        ${level ? html`<b>${x.label}</b>${x.current && html`<em>actuel</em>`}<span>${x.description || ''}</span>`
          : html`<b>/${x.name}</b>${x.hint && html`<em>${x.hint}</em>`}<span>${x.description || ''}</span>${(x.children || x.load) && html`<${Icon} n="right" />`}`}</button>`)}</div>`}
    <div class="composer">
      ${files.length ? html`<div class="attach-row">${files.map((f, i) => html`<span class="file-pill"><${Icon} n="file" />${f.name}<button aria-label="Retirer" onClick=${() => setFiles(files.filter((_, j) => j !== i))}><${Icon} n="close" /></button></span>`)}</div>` : ''}
      <textarea ref=${ta} rows="1" value=${text} onInput=${e => setText(e.target.value)} onKeyDown=${onKey}
        placeholder=${exec.name ? 'Écrire à ' + exec.name + '…' : 'Écrire un message…'} aria-label="Message"></textarea>
      <div class="composer-bar">
        ${native && html`<button class="icon-btn" aria-label="Joindre un fichier" title="Joindre" onClick=${() => fileIn.current.click()}><${Icon} n="paperclip" /></button>
          <input type="file" multiple hidden ref=${fileIn} onChange=${pick} />
          <button class=${cls('chip-btn', (tools.internet || tools.mcp) && 'on')} onClick=${e => setMenu(e.currentTarget)}><${Icon} n="sliders" />Outils${tools.internet || tools.mcp ? html` <span class="n">${(tools.internet ? 1 : 0) + (tools.mcp ? 1 : 0)}</span>` : ''}</button>`}
        ${workdir && html`<button class="chip-btn" title=${workdir} onClick=${() => app.set({ inspector: true })}><${Icon} n="folder" />${workdir.split('/').pop()}</button>`}
        ${entries.length > 0 && !text && html`<span class="composer-tip">/ pour les commandes</span>`}
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
