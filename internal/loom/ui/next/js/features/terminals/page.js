import { t } from '../../core/i18n.js';
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
  if (!r.ok) { toast(r.error || t("terminals.page.terminal_impossible"), 'err'); return null; }
  go('terminals', r.terminal.id);
  return r.terminal;
}

const cssVar = n => getComputedStyle(document.documentElement).getPropertyValue(n).trim();

function TermView({ t: localT, onExit }) {
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
      if (!localT.running) { setState('finished'); }
      const tk = await post('/api/terminals/ticket', { id: localT.id });
      if (!alive) return;
      if (!tk.ok) { setState('introuvable'); return; }
      ws = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/api/terminals/ws?ticket=' + encodeURIComponent(tk.ticket));
      ws.binaryType = 'arraybuffer';
      const size = () => { try { fit.fit(); if (ws.readyState === 1) ws.send(JSON.stringify({ resize: [term.cols, term.rows] })); } catch (_) {} };
      ws.onopen = () => { setState(localT.running ? 'ouvert' : 'finished'); size(); term.focus(); };
      ws.onmessage = e => {
        if (typeof e.data === 'string') { if (e.data.includes('"exit"')) { setState('finished'); onExit && onExit(); } return; }
        term.write(new Uint8Array(e.data));
      };
      ws.onclose = () => alive && setState(s => s === 'ouvert' ? 'disconnected' : s);
      term.onData(d => { if (ws.readyState === 1) ws.send(d); });
      ro = new ResizeObserver(size); ro.observe(box.current);
    })().catch(() => setState('erreur'));
    return () => { alive = false; ro && ro.disconnect(); ws && ws.close(); term && term.dispose(); };
  }, [localT.id]);
  return html`<div class="term-view">
    <div class="term-head"><b>${localT.title}</b><span class="muted mono trunc">${localT.target === 'local' ? t("terminals.page.cette_machine") : localT.target} · ${home(localT.dir) || '~'}${localT.command ? ' · ' + localT.command : ''}</span>
      <span class="grow"></span><span class=${cls('state', state === 'ouvert' && 'ok')}><i class=${'dot ' + (state === 'ouvert' ? 'green' : '')}></i>${{ connexion: t('terminals.status.connecting'), ouvert: t('terminals.status.open'), finished: t('terminals.status.finished'), disconnected: t('terminals.status.disconnected'), introuvable: t('common.not_found'), erreur: t('common.error') }[state] || state}</span></div>
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
  const submit = async () => { setBusy(true); const localT = await openTerminalWith(v); setBusy(false); if (localT) onClose(localT); };
  return html`<${Modal} title="${t("terminals.page.nouveau_terminal")}" sub="${t("terminals.page.un_shell_sur_cette_machine_ou_une_machine_connectee")}" onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>${t("terminals.page.annuler")}</button><button class="btn primary" disabled=${busy} onClick=${submit}>${busy ? t("terminals.page.ouverture") : t("terminals.page.ouvrir")}</button>`}>
    <div class="field"><span>${t("terminals.page.ou")}</span><div class="chips">
      <button type="button" class=${cls('chip-btn', !remote && 'on')} onClick=${() => setV({ ...v, target: 'local', dir: '' })}><${Icon} n="chip" />${t("terminals.page.cette_machine_2")}</button>
      ${machines.map(x => html`<button type="button" class=${cls('chip-btn', v.target === x.id && 'on')} onClick=${() => setV({ ...v, target: x.id, dir: x.home || '' })}><${Icon} n="server" />${x.name}</button>`)}
    </div><small>${machines.length ? '' : t("terminals.page.pour_une_autre_machine")}<a href="#/settings/machines" onClick=${() => onClose()}>${machines.length ? t("terminals.page.gerer_les_machines") : t("terminals.page.reglages_machines")}</a></small></div>
    <div class="field"><span>${t("terminals.page.dossier")}</span>
      ${remote ? html`<input class="input mono" placeholder=${(m && m.home) || '/home/moi'} value=${v.dir} onInput=${e => setV({ ...v, dir: e.target.value })} />`
        : html`<button class="hs-dir" onClick=${() => setPick(true)}><${Icon} n="folder" /><span class="mono trunc">${home(v.dir) || t("terminals.page.dossier_personnel")}</span><span class="muted">${t("terminals.page.changer")}</span></button>`}
      ${favs.length > 0 && html`<div class="chips">${favs.map(f => html`<button type="button" class=${cls('chip-btn', v.dir === f && 'on')} onClick=${() => setV({ ...v, dir: f })}><${Icon} n="folder" />${f.split('/').pop() || f}</button>`)}</div>`}
      ${!remote && projects.length > 0 && html`<div class="chips">${projects.map(p => html`<button type="button" class=${cls('chip-btn', v.dir === p.directory && 'on')} onClick=${() => setV({ ...v, dir: p.directory, title: v.title || p.name })}>${p.name}</button>`)}</div>`}</div>
    <div class="field"><span>${t("terminals.page.lancer")}<${Tip} text="${t("terminals.page.rien_un_shell_sinon_la_commande_demarre_dans_le_terminal_par_exem")}" /></span>
      <div class="chips">${QUICK.map(([c, l]) => html`<button type="button" class=${cls('chip-btn', v.command === c && 'on')} onClick=${() => setV({ ...v, command: c })}>${l}</button>`)}</div>
      <input class="input mono" placeholder="${t("terminals.page.ou_une_commande_npm_run_dev_htop")}" value=${v.command} onInput=${e => setV({ ...v, command: e.target.value })} /></div>
    <label class="field"><span>${t("terminals.page.nom")}</span><input class="input" placeholder="${t("terminals.page.facultatif")}" value=${v.title} onInput=${e => setV({ ...v, title: e.target.value })} /></label>
    ${pick && html`<${FolderPicker} start=${v.dir} onClose=${() => setPick(false)} onPick=${d => { setPick(false); setV({ ...v, dir: d }); }} />`}
  </${Modal}>`;
}

export function TerminalsPage({ route }) {
  const [list, setList] = useState(null);
  const [supported, setSupported] = useState(true);
  const [dlg, setDlg] = useState(false);
  const load = () => get('/api/terminals').then(r => { setList(r.terminals || []); setSupported(r.supported !== false); }).catch(() => setList([]));
  useEffect(() => { load(); }, [route.sub]);
  const cur = list && (list.find(localT => localT.id === route.sub) || list[list.length - 1]);
  const close = async localT => {
    if (localT.running && !await confirm(t("terminals.page.fermer_le_terminal"), t("terminals.page.le_processus_en_cours") + (localT.command || 'shell') + t("terminals.page.sera_arrete"), { ok: t("terminals.page.fermer"), danger: true })) return;
    await post('/api/terminals/close', { id: localT.id });
    load();
  };
  return html`<div class="view page"><div class="page-in wide">
    <div class="page-head"><div><h1>${t("terminals.page.terminaux")}</h1><p>${t("terminals.page.lance_des_applis_ou_le_cli_d_un_agent_pour_verifier_ou_depanner_i")}</p></div>
      <div class="acts"><button class="btn primary" disabled=${!supported} onClick=${() => setDlg(true)}><${Icon} n="plus" />${t("terminals.page.nouveau_terminal")}</button></div></div>
    ${!supported ? html`<${Empty} icon="info" title="${t("terminals.page.pas_encore_disponible_sur_ce_systeme")}" text="${t("terminals.page.les_terminaux_fonctionnent_sous_linux_et_macos_windows_arrive_plu")}" />`
      : !list ? html`<div class="skeleton" style="height:320px"></div>`
      : !list.length ? html`<${Empty} icon="terminal" title="${t("terminals.page.aucun_terminal_ouvert")}" text="${t("terminals.page.un_terminal_reste_ouvert_meme_si_tu_fermes_l_onglet_tu_le_retrouv")}"><button class="btn" onClick=${() => setDlg(true)}>${t("terminals.page.ouvrir_un_terminal")}</button></${Empty}>`
      : html`<div class="term-layout">
          <div class="term-list card">${list.map(localT => html`<div class=${cls('term-item', cur && cur.id === localT.id && 'on')} key=${localT.id}>
              <button class="term-pick" onClick=${() => go('terminals', localT.id)}><i class=${'dot ' + (localT.running ? 'green' : '')}></i><span class="grow"><b class="trunc">${localT.title}</b><small class="trunc">${localT.target === 'local' ? t("terminals.page.cette_machine") : localT.target} · ${home(localT.dir) || '~'}</small></span></button>
              <button class="icon-btn" aria-label=${t("terminals.page.fermer_2") + localT.title} onClick=${() => close(localT)}><${Icon} n="close" /></button></div>`)}</div>
          ${cur && html`<${TermView} key=${cur.id} t=${cur} onExit=${load} />`}
        </div>`}
    ${dlg && html`<${NewTerminal} onClose=${() => { setDlg(false); load(); }} />`}
  </div></div>`;
}
