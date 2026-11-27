import { t } from '../../core/i18n.js';
// Terminaux : de vrais shells ouverts depuis Loom, sur cette machine ou sur une
// machine distante, dans un dossier, avec une commande facultative (le CLI d'un
// agent pour vérifier ou dépanner, une appli à lancer). Un terminal survit à
// l'onglet : on le retrouve avec sa sortie récente.
import { html, useState, useEffect, useRef, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Tip } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { FolderPicker } from '../../ui/folder.js';
import { get, post } from '../../core/api.js';
import { visibleConnection } from '../../core/poll.js';
import { PreviewPanel } from '../previews/panel.js';
import { loadXterm, disposeTerm } from '../../ui/xterm.js';
import { app, go } from '../../core/state.js';
import { usePhone } from '../../core/viewport.js';

const TOUCH_KEYS = [
  { label: 'Esc', data: '\u001b' },
  { label: 'Tab', data: '\t' },
  { label: 'Ctrl+C', data: '\u0003', danger: true },
  { label: '↑', data: '\u001b[A' },
  { label: '↓', data: '\u001b[B' },
  { label: '←', data: '\u001b[D' },
  { label: '→', data: '\u001b[C' },
  { label: '/', data: '/' },
  { label: '|', data: '|' },
  { label: '~', data: '~' },
];

const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');
const terminalHref = id => '#/terminals/' + encodeURIComponent(id);

// Ouvre un terminal et y emmène l'utilisateur (projets, harnesses, machines).
const opening = new Map();
let autoOpened = false;
export async function openTerminalWith(spec) {
  const body = { target: spec.target || 'local', dir: spec.dir || '', command: spec.command || '', title: spec.title || '' };
  const key = JSON.stringify(body);
  if (opening.has(key)) return opening.get(key);
  const bytes = new Uint8Array(16); crypto.getRandomValues(bytes);
  body.request_id = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
  const request = (async () => {
    try {
      const r = await post('/api/terminals', body);
      if (!r.ok) { toast(r.error || t("terminals.page.terminal_impossible"), 'err'); return null; }
      autoOpened = true;
      go('terminals', r.terminal.id);
      return r.terminal;
    } catch (e) { toast(e.message, 'err'); return null; }
    finally { opening.delete(key); }
  })();
  opening.set(key, request);
  return request;
}

const cssVar = n => getComputedStyle(document.documentElement).getPropertyValue(n).trim();

export function TermView({ t: localT, machineName, onExit, phone }) {
  const box = useRef();
  const wsRef = useRef(null);
  const termRef = useRef(null);
  const [state, setState] = useState('connexion');
  useEffect(() => {
    let term, fit, ws, ro, stopConnection, alive = true;
    (async () => {
      const w = await loadXterm();
      if (!alive) return;
      term = new w.Terminal({
        fontFamily: cssVar('--mono') || 'monospace', fontSize: phone ? 12.5 : 13, lineHeight: 1.25, cursorBlink: true, scrollback: 5000,
        theme: { background: cssVar('--bg-2') || '#111', foreground: cssVar('--text') || '#eee', cursor: cssVar('--text') || '#eee', selectionBackground: 'rgba(127,127,127,.35)' },
      });
      termRef.current = term;
      fit = new w.FitAddon.FitAddon();
      term.loadAddon(fit);
      term.open(box.current);
      // Rendu GPU et largeurs Unicode 11, comme le terminal des agents.
      if (w.WebglAddon) try { const gl = new w.WebglAddon.WebglAddon(); gl.onContextLoss(() => { try { gl.dispose(); } catch (_) {} }); term.loadAddon(gl); } catch (_) {}
      if (w.Unicode11Addon) try { term.loadAddon(new w.Unicode11Addon.Unicode11Addon()); term.unicode.activeVersion = '11'; } catch (_) {}
      fit.fit();
      if (!localT.running) { setState('finished'); }
      const size = () => { try { fit.fit(); if (ws?.readyState === 1) ws.send(JSON.stringify({ resize: [term.cols, term.rows] })); } catch (_) {} };
      // Reattach the existing terminal, never open a shell or replay input.
      stopConnection = visibleConnection(async owner => {
        setState('connexion');
        const tk = await post('/api/terminals/ticket', { id: localT.id }, { signal: owner.signal, retryAuth: false });
        if (!owner.alive()) return;
        if (!tk.ok || typeof tk.ticket !== 'string') { setState('disconnected'); owner.retry(); return; }
        const socket = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/api/terminals/ws?ticket=' + encodeURIComponent(tk.ticket));
        ws = socket; wsRef.current = socket; socket.binaryType = 'arraybuffer';
        let exited = false;
        socket.onopen = () => { if (owner.alive()) { term.reset(); setState(localT.running ? 'ouvert' : 'finished'); size(); term.focus(); } };
        socket.onmessage = e => {
          if (!owner.alive()) return;
          if (typeof e.data === 'string') { if (e.data.includes('"exit"')) { exited = true; setState('finished'); onExit && onExit(); } return; }
          term.write(new Uint8Array(e.data));
        };
        socket.onclose = () => { if (owner.alive() && !exited) { setState('disconnected'); owner.retry(); } };
        socket.onerror = () => {};
        return () => { if (wsRef.current === socket) wsRef.current = null; socket.onopen = null; socket.onmessage = null; socket.onclose = null; socket.close(); };
      });
      term.onData(d => { if (ws?.readyState === 1) ws.send(d); });
      ro = new ResizeObserver(size); ro.observe(box.current);
    })().catch(() => setState('erreur'));
    return () => { alive = false; wsRef.current = null; termRef.current = null; ro && ro.disconnect(); stopConnection && stopConnection(); disposeTerm(term); };
  }, [localT.id]);
  const sendKey = data => {
    if (wsRef.current?.readyState === 1) wsRef.current.send(data);
    termRef.current?.focus();
  };
  const label = { connexion: t('terminals.status.connecting'), ouvert: t('terminals.status.open'), finished: t('terminals.status.finished'), disconnected: t('terminals.status.disconnected'), introuvable: t('common.not_found'), erreur: t('common.error') }[state] || state;
  return html`<div class=${cls('term-view', phone && 'term-view-phone')} aria-label=${localT.title + ' · ' + (machineName || localT.target)}>
    ${state !== 'ouvert' && html`<span class="term-state" role="status">${label}</span>`}
    <div class="term-box" ref=${box}></div>
    ${phone && html`<div class="cm-acc term-acc" role="toolbar" aria-label=${t('terminals.page.terminaux')}>
      ${TOUCH_KEYS.map(k => html`<button type="button" key=${k.label} class=${cls(k.danger && 'cm-acc-break')} onClick=${() => sendKey(k.data)}><span>${k.label}</span></button>`)}
    </div>`}
  </div>`;
}

const QUICK = () => [['', t('agents.quick.shell')], ['claude', 'Claude Code'], ['codex', 'Codex'], ['agy', 'Antigravity'], ['pi', 'Pi'], ['hermes', 'Hermes'], ['opencode', 'OpenCode']];

function NewTerminal({ onClose, preset }) {
  const [machines, setMachines] = useState([]);
  const [v, setV] = useState({ target: 'local', dir: '', command: '', title: '', ...(preset || {}) });
  const [pick, setPick] = useState(false);
  const [busy, setBusy] = useState(false);
  const ws = useStore(app, a => a.workspace);
  const [favs, setFavs] = useState([]);
  useEffect(() => { get('/api/machines').then(r => setMachines(r.machines || [])).catch(() => {}); }, []);
  useEffect(() => { get('/api/machines/folders?machine=' + encodeURIComponent(v.target)).then(r => setFavs(r.folders || [])).catch(() => setFavs([])); }, [v.target]);
  const projects = ((ws && ws.projects) || []).filter(p => p.directory);
  const remote = v.target !== 'local';
  const m = machines.find(x => x.id === v.target);
  const pending = useRef(false);
  const submit = async () => { if (pending.current) return; pending.current = true; setBusy(true); try { const localT = await openTerminalWith(v); if (localT) onClose(localT); } finally { pending.current = false; setBusy(false); } };
  return html`<${Modal} title="${t("terminals.page.nouveau_terminal")}" sub="${t("terminals.page.un_shell_sur_cette_machine_ou_une_machine_connectee")}" onClose=${() => onClose()}
      foot=${html`<button class="btn" onClick=${() => onClose()}>${t("terminals.page.annuler")}</button><button class="btn primary" disabled=${busy} onClick=${submit}>${busy ? t("terminals.page.ouverture") : t("terminals.page.ouvrir")}</button>`}>
    <div class="field"><span>${t("terminals.page.ou")}</span>
      <${ListPick} label=${t("terminals.page.ou")} value=${v.target}
        options=${[{ value: 'local', label: t("terminals.page.cette_machine_2"), icon: 'chip' }, ...machines.map(x => ({ value: x.id, label: x.name, icon: 'server' }))]}
        onChange=${target => setV({ ...v, target, dir: machines.find(x => x.id === target)?.home || '' })} />
      <small>${machines.length ? '' : t("terminals.page.pour_une_autre_machine")}<a href="#/machines" onClick=${() => onClose()}>${machines.length ? t("terminals.page.gerer_les_machines") : t("terminals.page.reglages_machines")}</a></small></div>
    <div class="field"><span>${t("terminals.page.dossier")}</span>
      ${remote ? html`<input class="input mono" placeholder=${(m && m.home) || '/home/moi'} value=${v.dir} onInput=${e => setV({ ...v, dir: e.target.value })} />`
        : html`<button class="btn hs-dir" onClick=${() => setPick(true)}><${Icon} n="folder" /><span class="mono trunc">${home(v.dir) || t("terminals.page.dossier_personnel")}</span><span class="muted">${t("terminals.page.changer")}</span></button>`}
      ${favs.length > 0 && html`<div class="chips">${favs.map(f => html`<button type="button" class=${cls('btn sm', v.dir === f && 'on')} onClick=${() => setV({ ...v, dir: f })}><${Icon} n="folder" />${f.split('/').pop() || f}</button>`)}</div>`}
      ${!remote && projects.length > 0 && html`<div class="chips">${projects.map(p => html`<button type="button" class=${cls('btn sm', v.dir === p.directory && 'on')} onClick=${() => setV({ ...v, dir: p.directory, title: v.title || p.name })}>${p.name}</button>`)}</div>`}</div>
    <div class="field"><span>${t("terminals.page.lancer")}<${Tip} text="${t("terminals.page.rien_un_shell_sinon_la_commande_demarre_dans_le_terminal_par_exem")}" /></span>
      <div class="chips">${QUICK().map(([c, l]) => html`<button type="button" class=${cls('btn sm', v.command === c && 'on')} onClick=${() => setV({ ...v, command: c })}>${l}</button>`)}</div>
      <input class="input mono" placeholder="${t("terminals.page.ou_une_commande_npm_run_dev_htop")}" value=${v.command} onInput=${e => setV({ ...v, command: e.target.value })} /></div>
    <label class="field"><span>${t("terminals.page.nom")}</span><input class="input" placeholder="${t("terminals.page.facultatif")}" value=${v.title} onInput=${e => setV({ ...v, title: e.target.value })} /></label>
    ${pick && html`<${FolderPicker} start=${v.dir} onClose=${() => setPick(false)} onPick=${d => { setPick(false); setV({ ...v, dir: d }); }} />`}
  </${Modal}>`;
}

export function TerminalsPage({ route }) {
  const phone = usePhone();
  const [list, setList] = useState(null);
  const [supported, setSupported] = useState(true);
  const [dlg, setDlg] = useState(false);
  const [previews, setPreviews] = useState(false);
  const [machines, setMachines] = useState([]);
  const [local, setLocal] = useState(null);
  const read = useRef(0);
  const load = async () => {
    const id = ++read.current;
    try {
      const result = await get('/api/terminals');
      if (id !== read.current) return;
      const terms = (result.terminals || []).filter(term => typeof term.id === 'string');
      setList(terms);
      setSupported(result.supported !== false);
      // Keep the first-visit shell, but never open a shell from a stale link
      // or as a side effect of closing the last terminal.
      if (terms.length) autoOpened = true;
      else if (!route.sub && !autoOpened && result.supported !== false) {
        autoOpened = true;
        await openTerminalWith({});
      }
    } catch (_) { if (id === read.current) setList(old => old || []); }
  };
  useEffect(() => {
    load();
    return () => { read.current++; };
  }, [route.sub]);
  useEffect(() => {
    const controller = new AbortController();
    const options = { signal: controller.signal };
    get('/api/machines', options).then(r => setMachines(r.machines || [])).catch(() => {});
    get('/api/machines/local', options).then(setLocal).catch(() => {});
    return () => controller.abort();
  }, []);
  const names = new Map([['local', local?.hostname || t('terminals.page.cette_machine_2')], ...machines.map(machine => [machine.id, machine.name])]);
  const cur = list && (list.find(term => term.id === route.sub) || list[list.length - 1]);
  const close = async localT => {
    if (localT.running && !await confirm(t("terminals.page.fermer_le_terminal"), t("terminals.page.le_processus_en_cours") + (localT.command || t('agents.quick.shell')) + t("terminals.page.sera_arrete"), { ok: t("terminals.page.fermer"), danger: true })) return;
    const result = await post('/api/terminals/close', { id: localT.id });
    if (!result.ok && result.status !== 404) return toast(result.error || t('common.error'), 'err');
    const remaining = (list || []).filter(term => term.id !== localT.id);
    setList(remaining);
    // Plus aucun terminal : retour aux machines.
    if (!remaining.length) return go('machines');
    if (localT.id === cur?.id) go('terminals', remaining.at(-1)?.id);
    load();
  };
  return html`<div class=${cls('view term-page', phone && 'term-page-phone')}>
    <h1 class="sr">${t("terminals.page.terminaux")}</h1>
    <header class="term-head">
      ${!phone && html`<a class="btn sm machine-back" href="#/machines"><${Icon} n="left" /><span>${t('settings.machines.machines')}</span></a>`}
      <nav class="terminal-tabs" aria-label=${t('settings.machines.open_terminals')}>
        ${(list || []).map(term => html`<div class=${cls('terminal-tab', cur?.id === term.id && 'current')} key=${term.id}>
          <a href=${terminalHref(term.id)} aria-current=${cur?.id === term.id ? 'page' : undefined} title=${term.title + ' · ' + (names.get(term.target) || term.target)}>
            <i class=${cls('dot', term.running && 'green')}></i><b>${term.title}</b><small>${names.get(term.target) || term.target}</small>
          </a>
          <button class="icon-btn" aria-label=${t("terminals.page.fermer_2") + term.title} onClick=${() => close(term)}><${Icon} n="close" /></button>
        </div>`)}
      </nav>
      <div class="term-head-acts">
        <button class=${cls(phone ? 'icon-btn' : 'btn sm term-ghost-btn')} aria-label=${t('terminals.page.previews')} title=${t('terminals.page.previews')} onClick=${() => setPreviews(true)}><${Icon} n="globe" />${!phone && html`<span>${t('terminals.page.previews')}</span>`}</button>
        <button class=${cls(phone ? 'icon-btn' : 'btn sm primary')} disabled=${!supported} aria-label=${t("terminals.page.nouveau_terminal")} title=${t("terminals.page.nouveau_terminal")} onClick=${() => setDlg(true)}><${Icon} n="plus" />${!phone && html`<span>${t("terminals.page.nouveau_terminal")}</span>`}</button>
      </div>
    </header>
    <div class="term-stage">
      ${!supported ? html`<${Empty} icon="info" title="${t("terminals.page.pas_encore_disponible_sur_ce_systeme")}" text="${t("terminals.page.les_terminaux_fonctionnent_sous_linux_et_macos_windows_arrive_plu")}" />`
        : !list ? html`<div class="skeleton term-skeleton"></div>`
        : !list.length ? html`<${Empty} icon="terminal" title="${t("terminals.page.aucun_terminal_ouvert")}" text="${t("terminals.page.un_terminal_reste_ouvert_meme_si_tu_fermes_l_onglet_tu_le_retrouv")}"><button class="btn primary sm" onClick=${() => setDlg(true)}><${Icon} n="plus" />${t("terminals.page.ouvrir_un_terminal")}</button></${Empty}>`
        : cur && html`<${TermView} key=${cur.id} t=${cur} machineName=${names.get(cur.target) || cur.target} onExit=${load} phone=${phone} />`}
    </div>
    ${previews && html`<${Modal} title=${t('previews.title')} onClose=${() => setPreviews(false)}><${PreviewPanel} target=${cur?.target || 'local'} /></${Modal}>`}
    ${dlg && html`<${NewTerminal} onClose=${() => { setDlg(false); load(); }} />`}
  </div>`;
}
