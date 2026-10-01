// Terminaux : de vrais shells ouverts depuis Loom, sur cette machine ou sur une
// machine distante, dans un dossier, avec une commande facultative (le CLI d'un
// agent pour vérifier ou dépanner, une appli à lancer). Un terminal survit à
// l'onglet : on le retrouve avec sa sortie récente.
import { html, useState, useEffect, useRef, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Tip } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { FolderPicker } from '../../ui/folder.js';
import { get, post } from '../../core/api.js';
import { app, go } from '../../core/state.js';

const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');

// xterm.js (MIT, vendor/xterm) n'est chargé que sur cette page.
let xtermReady = null;
function loadXterm() {
  if (!xtermReady) xtermReady = new Promise((res, rej) => {
    const css = document.createElement('link');
    css.rel = 'stylesheet'; css.href = '/next/vendor/xterm/xterm.css';
    document.head.appendChild(css);
    const load = src => new Promise((ok, ko) => { const s = document.createElement('script'); s.src = src; s.onload = ok; s.onerror = ko; document.head.appendChild(s); });
    load('/next/vendor/xterm/xterm.js').then(() => load('/next/vendor/xterm/addon-fit.js')).then(() => res(window), rej);
  });
  return xtermReady;
}

// Ouvre un terminal et y emmène l'utilisateur (projets, harnesses, machines).
export async function openTerminalWith(spec) {
  const r = await post('/api/terminals', spec);
  if (!r.ok) { toast(r.error || 'Terminal impossible', 'err'); return null; }
  go('terminals', r.terminal.id);
  return r.terminal;
}

const cssVar = n => getComputedStyle(document.documentElement).getPropertyValue(n).trim();

function TermView({ t, onExit }) {
  const box = useRef();
  const [state, setState] = useState('connexion');
  useEffect(() => {
    let term, fit, ws, ro, alive = true;
    (async () => {
      const w = await loadXterm();
      if (!alive) return;
      term = new w.Terminal({
        fontFamily: cssVar('--mono') || 'monospace', fontSize: 13, lineHeight: 1.2, cursorBlink: true, scrollback: 5000,
        theme: { background: cssVar('--bg-2') || '#111', foreground: cssVar('--text') || '#eee', cursor: cssVar('--text') || '#eee', selectionBackground: 'rgba(127,127,127,.35)' },
      });
      fit = new w.FitAddon.FitAddon();
      term.loadAddon(fit);
      term.open(box.current);
      fit.fit();
      if (!t.running) { setState('terminé'); }
      const tk = await post('/api/terminals/ticket', { id: t.id });
      if (!alive) return;
      if (!tk.ok) { setState('introuvable'); return; }
      ws = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/api/terminals/ws?ticket=' + encodeURIComponent(tk.ticket));
      ws.binaryType = 'arraybuffer';
      const size = () => { try { fit.fit(); if (ws.readyState === 1) ws.send(JSON.stringify({ resize: [term.cols, term.rows] })); } catch (_) {} };
      ws.onopen = () => { setState(t.running ? 'ouvert' : 'terminé'); size(); term.focus(); };
      ws.onmessage = e => {
        if (typeof e.data === 'string') { if (e.data.includes('"exit"')) { setState('terminé'); onExit && onExit(); } return; }
        term.write(new Uint8Array(e.data));
      };
      ws.onclose = () => alive && setState(s => s === 'ouvert' ? 'déconnecté' : s);
      term.onData(d => { if (ws.readyState === 1) ws.send(d); });
      ro = new ResizeObserver(size); ro.observe(box.current);
    })().catch(() => setState('erreur'));
    return () => { alive = false; ro && ro.disconnect(); ws && ws.close(); term && term.dispose(); };
  }, [t.id]);
  return html`<div class="term-view">
    <div class="term-head"><b>${t.title}</b><span class="muted mono trunc">${t.target === 'local' ? 'cette machine' : t.target} · ${home(t.dir) || '~'}${t.command ? ' · ' + t.command : ''}</span>
      <span class="grow"></span><span class=${cls('state', state === 'ouvert' && 'ok')}><i class=${'dot ' + (state === 'ouvert' ? 'green' : '')}></i>${state}</span></div>
    <div class="term-box" ref=${box}></div>
  </div>`;
}

const QUICK = [['', 'Shell'], ['claude', 'Claude Code'], ['codex', 'Codex'], ['gemini', 'Gemini'], ['pi', 'Pi'], ['hermes', 'Hermes'], ['opencode', 'OpenCode']];

function NewTerminal({ onClose, preset }) {
  const [machines, setMachines] = useState([]);
  const [v, setV] = useState({ target: 'local', dir: '', command: '', title: '', ...(preset || {}) });
  const [pick, setPick] = useState(false);
  const [busy, setBusy] = useState(false);
  const ws = useStore(app, a => a.workspace);
  const [favs, setFavs] = useState([]);
  useEffect(() => { get('/api/machines').then(r => setMachines(r.ok ? r.machines : [])).catch(() => {}); }, []);
  useEffect(() => { get('/api/machines/folders?machine=' + encodeURIComponent(v.target)).then(r => setFavs(r.folders || [])).catch(() => setFavs([])); }, [v.target]);
  const projects = ((ws && ws.projects) || []).filter(p => p.directory);
  const remote = v.target !== 'local';
  const m = machines.find(x => x.id === v.target);
  const submit = async () => { setBusy(true); const t = await openTerminalWith(v); setBusy(false); if (t) onClose(t); };
  return html`<${Modal} title="Nouveau terminal" sub="Un shell sur cette machine ou une machine connectée." onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>Annuler</button><button class="btn primary" disabled=${busy} onClick=${submit}>${busy ? 'Ouverture…' : 'Ouvrir'}</button>`}>
    <div class="field"><span>Où</span><div class="chips">
      <button type="button" class=${cls('chip-btn', !remote && 'on')} onClick=${() => setV({ ...v, target: 'local', dir: '' })}><${Icon} n="chip" />Cette machine</button>
      ${machines.map(x => html`<button type="button" class=${cls('chip-btn', v.target === x.id && 'on')} onClick=${() => setV({ ...v, target: x.id, dir: x.home || '' })}><${Icon} n="server" />${x.name}</button>`)}
    </div><small>${machines.length ? '' : 'Pour une autre machine : '}<a href="#/settings/machines" onClick=${() => onClose()}>${machines.length ? 'Gérer les machines' : 'Réglages › Machines'}</a></small></div>
    <div class="field"><span>Dossier</span>
      ${remote ? html`<input class="input mono" placeholder=${(m && m.home) || '/home/moi'} value=${v.dir} onInput=${e => setV({ ...v, dir: e.target.value })} />`
        : html`<button class="hs-dir" onClick=${() => setPick(true)}><${Icon} n="folder" /><span class="mono trunc">${home(v.dir) || '~ (dossier personnel)'}</span><span class="muted">Changer</span></button>`}
      ${favs.length > 0 && html`<div class="chips">${favs.map(f => html`<button type="button" class=${cls('chip-btn', v.dir === f && 'on')} onClick=${() => setV({ ...v, dir: f })}><${Icon} n="folder" />${f.split('/').pop() || f}</button>`)}</div>`}
      ${!remote && projects.length > 0 && html`<div class="chips">${projects.map(p => html`<button type="button" class=${cls('chip-btn', v.dir === p.directory && 'on')} onClick=${() => setV({ ...v, dir: p.directory, title: v.title || p.name })}>${p.name}</button>`)}</div>`}</div>
    <div class="field"><span>Lancer<${Tip} text="Rien : un shell. Sinon la commande démarre dans le terminal, par exemple le CLI d’un agent pour vérifier ou dépanner quelque chose." /></span>
      <div class="chips">${QUICK.map(([c, l]) => html`<button type="button" class=${cls('chip-btn', v.command === c && 'on')} onClick=${() => setV({ ...v, command: c })}>${l}</button>`)}</div>
      <input class="input mono" placeholder="ou une commande : npm run dev, htop…" value=${v.command} onInput=${e => setV({ ...v, command: e.target.value })} /></div>
    <label class="field"><span>Nom</span><input class="input" placeholder="facultatif" value=${v.title} onInput=${e => setV({ ...v, title: e.target.value })} /></label>
    ${pick && html`<${FolderPicker} start=${v.dir} onClose=${() => setPick(false)} onPick=${d => { setPick(false); setV({ ...v, dir: d }); }} />`}
  </${Modal}>`;
}

export function TerminalsPage({ route }) {
  const [list, setList] = useState(null);
  const [supported, setSupported] = useState(true);
  const [dlg, setDlg] = useState(false);
  const load = () => get('/api/terminals').then(r => { setList(r.terminals || []); setSupported(r.supported !== false); }).catch(() => setList([]));
  useEffect(() => { load(); }, [route.sub]);
  const cur = list && (list.find(t => t.id === route.sub) || list[list.length - 1]);
  const close = async t => {
    if (t.running && !await confirm('Fermer le terminal', 'Le processus en cours (' + (t.command || 'shell') + ') sera arrêté.', { ok: 'Fermer', danger: true })) return;
    await post('/api/terminals/close', { id: t.id });
    load();
  };
  return html`<div class="view page"><div class="page-in wide">
    <div class="page-head"><div><h1>Terminaux</h1><p>Lance des applis, ou le CLI d’un agent pour vérifier ou dépanner, ici ou sur une machine connectée.</p></div>
      <div class="acts"><button class="btn primary" disabled=${!supported} onClick=${() => setDlg(true)}><${Icon} n="plus" />Nouveau terminal</button></div></div>
    ${!supported ? html`<${Empty} icon="info" title="Pas encore disponible sur ce système" text="Les terminaux fonctionnent sous Linux et macOS ; Windows arrive plus tard." />`
      : !list ? html`<div class="skeleton" style="height:320px"></div>`
      : !list.length ? html`<${Empty} icon="terminal" title="Aucun terminal ouvert" text="Un terminal reste ouvert même si tu fermes l’onglet : tu le retrouves ici."><button class="btn" onClick=${() => setDlg(true)}>Ouvrir un terminal</button></${Empty}>`
      : html`<div class="term-layout">
          <div class="term-list card">${list.map(t => html`<div class=${cls('term-item', cur && cur.id === t.id && 'on')} key=${t.id}>
              <button class="term-pick" onClick=${() => go('terminals', t.id)}><i class=${'dot ' + (t.running ? 'green' : '')}></i><span class="grow"><b class="trunc">${t.title}</b><small class="trunc">${t.target === 'local' ? 'cette machine' : t.target} · ${home(t.dir) || '~'}</small></span></button>
              <button class="icon-btn" aria-label=${'Fermer ' + t.title} onClick=${() => close(t)}><${Icon} n="close" /></button></div>`)}</div>
          ${cur && html`<${TermView} key=${cur.id} t=${cur} onExit=${load} />`}
        </div>`}
    ${dlg && html`<${NewTerminal} onClose=${() => { setDlg(false); load(); }} />`}
  </div></div>`;
}
