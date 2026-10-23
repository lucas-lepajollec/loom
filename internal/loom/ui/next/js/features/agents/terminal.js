// Terminal natif d'une session Codeman, rendu par xterm.js. Le WebSocket ne
// renvoie pas l'historique : on lit d'abord l'écran courant par HTTP, puis on
// s'attache au flux. Une reconnexion relit l'écran, ne rejoue aucune saisie.
import { lt } from './labels.js';
import { t } from '../../core/i18n.js';
import { html, useEffect, useRef, useState, cls } from '../../core/lib.js';
import { visibleConnection } from '../../core/poll.js';
import { loadXterm } from '../../ui/xterm.js';
import { Icon } from '../../ui/icons.js';
import { api, terminalClientId } from './client.js';
import { toast } from '../../ui/dialog.js';
import { filesOf, hasFiles, quotePath, uploadFiles } from './attach.js';

const cssVar = n => getComputedStyle(document.documentElement).getPropertyValue(n).trim();
// Mêmes seuils que Codeman (MobileDetection.getDeviceType) : il refuse la taille
// d'un téléphone ou d'une tablette tant qu'un ordinateur est actif.
const viewport = () => innerWidth < 600 ? 'mobile' : innerWidth < 768 ? 'tablet' : 'desktop';

// xterm mesure la largeur d'un caractère une fois, à sa création. La police
// du terminal (Geist Mono) est en font-display: swap : mesurée sur la police
// de secours, plus étroite, elle donne trop de colonnes et le texte déborde du
// panneau quand la vraie police arrive. On l'attend (borné), et on re-mesure
// si elle arrive après.
function fontsReady(size) {
  const fam = cssVar('--mono').split(',')[0].trim() || 'monospace';
  if (!document.fonts?.load) return Promise.resolve();
  const wait = Promise.all([document.fonts.load(`${size}px ${fam}`), document.fonts.load(`bold ${size}px ${fam}`)]).catch(() => {});
  return Promise.race([wait, new Promise(r => setTimeout(r, 3000))]);
}

export function theme() {
  return {
    background: cssVar('--bg-2') || '#0c0c0e', foreground: cssVar('--text') || '#ededef', cursor: cssVar('--text-2') || '#a1a1aa',
    cursorAccent: cssVar('--bg-2') || '#0c0c0e', selectionBackground: 'rgba(127,127,127,.32)',
  };
}

// fit : ajuste la grille au panneau ; sinon le terminal garde la géométrie
// courante du pane (mosaïque en lecture, plusieurs clients).
// Chargement de l'écran, comme l'interface officielle : l'historique tmux
// rendu (borné en lignes et en octets) plutôt que le seul cadre visible ; un
// shell dans un écran alternatif (vim, less, tmux imbriqué) garde
// l'enregistrement brut.
const TAIL = 1024 * 1024;
const historyQuery = (shell, lines) => `full=1&tail=${TAIL}&lines=${lines}` + (shell ? '&alt=recording' : '');

export function AgentTerminal({ sessionId, mode = '', shell = false, fit = true, fontSize = 13, focus = false, readOnly = false, scrollback = 20000, onState }) {
  const box = useRef(), termRef = useRef();
  const [state, setState] = useState('connecting');
  const [busy, setBusy] = useState(false);
  const [over, setOver] = useState(false);
  // Taille imposée par un autre écran, plus grande que ce panneau.
  const [foreign, setForeignState] = useState(false);
  const reclaimRef = useRef(null);
  const report = s => { setState(s); onState && onState(s); };
  useEffect(() => {
    let term, fitAddon, ws, ro, stopConn, detach, alive = true, resizeTimer, hollow = false, isForeign = false;
    const setForeign = v => { if (v !== isForeign) { isForeign = v; if (alive) setForeignState(v); } };
    (async () => {
      const w = await loadXterm();
      // Comme Codeman : texte plus petit sur téléphone pour garder des colonnes.
      if (viewport() === 'mobile' && fontSize > 10) fontSize = 10;
      await fontsReady(fontSize);
      if (!alive) return;
      const light = document.documentElement.dataset.theme === 'light';
      term = new w.Terminal({
        fontFamily: cssVar('--mono') || 'monospace', fontSize, lineHeight: 1.18, letterSpacing: 0, cursorBlink: false,
        cursorStyle: 'bar', scrollback, allowProposedApi: true, convertEol: false, minimumContrastRatio: light ? 4.5 : 1,
        disableStdin: readOnly, theme: theme(),
      });
      termRef.current = term;
      fitAddon = new w.FitAddon.FitAddon();
      term.loadAddon(fitAddon);
      term.open(box.current);
      try { fitAddon.fit(); } catch (_) {}
      const remeasure = () => {
        if (!alive) return;
        const fam = term.options.fontFamily;
        term.options.fontFamily = fam + ' ';
        term.options.fontFamily = fam;
        refit();
      };
      document.fonts?.addEventListener?.('loadingdone', remeasure);
      // La taille que ce panneau voudrait. Elle est DEMANDÉE à Codeman ; le
      // terminal ne prend que celle que Codeman confirme (zc). Redimensionner
      // localement d'abord faisait sauter l'affichage à chaque geste quand la
      // demande était refusée (téléphone pendant qu'un ordinateur est actif).
      const wanted = () => { try { return fitAddon.proposeDimensions(); } catch (_) { return null; } };
      const sendResize = force => {
        const d = fit && wanted();
        if (!d || !Number.isFinite(d.cols) || !Number.isFinite(d.rows)) return;
        if (ws?.readyState !== 1) { if (d.cols !== term.cols || d.rows !== term.rows) term.resize(d.cols, d.rows); return; }
        ws.send(JSON.stringify({ t: 'z', c: d.cols, r: d.rows, v: viewport(), ...(force ? { f: true } : {}) }));
      };
      // Un seul redimensionnement par geste : pendant le glissement d'un
      // séparateur de la grille, on attend qu'il soit lâché.
      const refit = () => {
        clearTimeout(resizeTimer);
        resizeTimer = setTimeout(() => {
          if (document.body.classList.contains('cm-dragging')) return refit();
          try { sendResize(false); } catch (_) {}
        }, 150);
      };
      // Plusieurs écrans partagent le pane ; Codeman lui donne la taille du
      // dernier ordinateur qui la demande. Celui qu'on utilise la reprend :
      // clic, frappe, molette, retour sur l'onglet, ou la pastille.
      // Codeman ne prévient pas les autres écrans d'un changement de taille :
      // celui qu'on utilise la réaffirme (au plus toutes les 2 s ; une taille
      // inchangée ne coûte rien côté Codeman).
      let lastClaim = 0;
      const reclaim = now => { if (!fit || readOnly) return; if (!now && Date.now() - lastClaim < 2000) return; lastClaim = Date.now(); sendResize(false); };
      reclaimRef.current = () => reclaim(true);
      // Après un changement de géométrie, l'écran est relu une fois pour
      // effacer ce que les tailles intermédiaires ont laissé.
      let redrawTimer;
      const redraw = () => { clearTimeout(redrawTimer); redrawTimer = setTimeout(() => { if (alive) loadScreen(); }, 400); };
      const loadScreen = async () => {
        try {
          const r = await api.terminal(sessionId, historyQuery(shell, scrollback + term.rows));
          if (!alive) return;
          term.reset();
          if (r?.terminalBuffer) term.write(r.terminalBuffer);
          // Pas d'historique tmux : le CLI dessine en plein écran et garde lui-même son historique.
          hollow = r?.paneHistoryLines === 0;
          // La capture dit la taille réelle du pane : plus large que ce panneau, il déborde.
          const want = fit && wanted();
          if (want && Number.isInteger(r?.captureCols)) setForeign(viewport() === 'desktop' && r.captureCols > want.cols);
        } catch (_) {}
      };
      stopConn = visibleConnection(async owner => {
        report('connecting');
        await loadScreen();
        if (!owner.alive()) return;
        const proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
        const socket = new WebSocket(proto + location.host + '/api/codeman/ws/' + encodeURIComponent(sessionId) + '?cid=' + encodeURIComponent(terminalClientId()));
        ws = socket;
        let ended = false, seen = null;
        socket.onopen = () => { if (!owner.alive()) return; report('open'); sendResize(true); if (focus && !readOnly) term.focus(); };
        socket.onmessage = e => {
          if (!owner.alive() || typeof e.data !== 'string') return;
          let m; try { m = JSON.parse(e.data); } catch (_) { return; }
          if (m.t === 'o' && typeof m.d === 'string') term.write(m.d);
          else if (m.t === 'c') term.clear();
          else if (m.t === 'r') loadScreen();
          // Géométrie réellement retenue par le pane (arbitrage entre appareils).
          else if (m.t === 'zc' && Number.isInteger(m.c) && Number.isInteger(m.r)) { if (m.c !== term.cols || m.r !== term.rows) term.resize(m.c, m.r); const want = wanted(); setForeign(viewport() === 'desktop' && !!want && (m.c > want.cols || m.r > want.rows)); if (seen && (m.c !== seen.c || m.r !== seen.r)) redraw(); seen = { c: m.c, r: m.r }; }
        };
        socket.onclose = e => {
          if (!owner.alive()) return;
          // 4009 : session terminée ; 4004 : introuvable ; 4003 : refusé.
          if (e.code === 4009 || e.code === 4004) { ended = true; report(e.code === 4009 ? 'exited' : 'missing'); return; }
          if (e.code === 4010) return; // remplacé par notre propre reconnexion
          report('disconnected'); owner.retry();
        };
        socket.onerror = () => {};
        return () => { socket.onopen = socket.onmessage = socket.onclose = null; if (!ended) socket.close(); };
      }, 1500);
      // Ctrl+V (Cmd+V) reste au navigateur : son collage apporte le texte ou
      // l'image, et le CLI ne reçoit pas ^V (Claude Code lirait alors le
      // presse-papiers du serveur : « No image found in clipboard »).
      term.attachCustomKeyEventHandler(ev => !(ev.type === 'keydown' && (ev.ctrlKey || ev.metaKey) && !ev.altKey && ev.key.toLowerCase() === 'v'));
      // Le terminal où l'on clique reprend sa taille (plusieurs écrans
      // partagent le même pane ; Codeman suit le dernier qui la demande).
      term.textarea?.addEventListener('focus', () => reclaim(true));
      term.onData(d => { if (readOnly || ws?.readyState !== 1) return; reclaim(); ws.send(JSON.stringify({ t: 'i', d })); });
      const onBack = () => { if (!document.hidden && box.current?.offsetParent) reclaim(isForeign); };
      box.current.addEventListener('pointerdown', onBack);
      window.addEventListener('focus', onBack);
      document.addEventListener('visibilitychange', onBack);
      ro = new ResizeObserver(refit); ro.observe(box.current);
      // Une image collée ou un fichier déposé part vers Codeman ; son chemin
      // est tapé dans le terminal, comme dans l'interface officielle.
      const attach = async files => {
        if (readOnly || !files.length) return;
        setBusy(true);
        try {
          const paths = await uploadFiles(sessionId, files);
          if (ws?.readyState === 1) ws.send(JSON.stringify({ t: 'i', d: paths.map(quotePath).join(' ') + ' ' }));
          term.focus();
        } catch (e) { toast(e.message, 'err'); }
        finally { if (alive) setBusy(false); }
      };
      const el = box.current;
      const onPaste = e => { const files = filesOf(e.clipboardData); if (files.length) { e.preventDefault(); e.stopPropagation(); attach(files); } };
      const onOver = e => { if (hasFiles(e) && !readOnly) { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; setOver(true); } };
      const onLeave = e => { if (!el.contains(e.relatedTarget)) setOver(false); };
      const onDrop = e => { setOver(false); if (!hasFiles(e) || readOnly) return; e.preventDefault(); e.stopPropagation(); attach(filesOf(e.dataTransfer)); };
      // Molette, comme l'interface officielle : un CLI plein écran (Claude
      // Code) garde l'historique de sa conversation et le fait défiler lui-même.
      // Claude reçoit des événements de molette SGR à la position du pointeur,
      // les autres CLI des pages (PageUp/PageDown). Maj+molette, un shell ou un
      // programme qui suit déjà la souris gardent le défilement de xterm.
      let travel = 0;
      const send = d => { if (ws?.readyState === 1) ws.send(JSON.stringify({ t: 'i', d })); };
      // Le défilement va-t-il au CLI (plein écran, sans historique local) ?
      const toCli = () => {
        if (readOnly || shell) return false;
        const tracking = term.modes?.mouseTrackingMode;
        if ((tracking && tracking !== 'none') || term.buffer.active.type === 'alternate') return false;
        return hollow || term.buffer.active.baseY === 0;
      };
      // lines : lignes de défilement (négatif = vers le haut), au point x/y.
      const scrollCli = (lines, x, y) => {
        const screen = el.querySelector('.xterm-screen').getBoundingClientRect();
        const cellH = screen.height / term.rows, cellW = screen.width / term.cols;
        travel += lines;
        if (mode === 'claude') {
          const n = Math.trunc(travel);
          if (!n) return;
          travel -= n;
          const col = Math.min(term.cols, Math.max(1, Math.floor((x - screen.left) / cellW) + 1));
          const row = Math.min(term.rows, Math.max(1, Math.floor((y - screen.top) / cellH) + 1));
          send(`\x1b[<${n < 0 ? 64 : 65};${col};${row}M`.repeat(Math.min(Math.abs(n), 5)));
        } else {
          const page = Math.max(2, Math.round(term.rows / 2));
          const pages = Math.trunc(travel / page);
          if (!pages) return;
          travel -= pages * page;
          send((pages < 0 ? '\x1b[5~' : '\x1b[6~').repeat(Math.min(Math.abs(pages), 4)));
        }
      };
      const cellHeight = () => el.querySelector('.xterm-screen').getBoundingClientRect().height / term.rows;
      const onWheel = ev => {
        reclaim();
        if (ev.shiftKey || ev.ctrlKey || Math.abs(ev.deltaX) > Math.abs(ev.deltaY) || !toCli()) return;
        ev.preventDefault(); ev.stopPropagation();
        scrollCli(ev.deltaMode === 1 ? ev.deltaY : ev.deltaMode === 2 ? ev.deltaY * term.rows : ev.deltaY / cellHeight(), ev.clientX, ev.clientY);
      };
      // Au doigt : un glissement vertical fait défiler le CLI comme la molette ;
      // un glissement horizontal reste au panneau (terminal plus large que
      // l'écran quand un ordinateur fixe la taille).
      let touch = null;
      const onTouchStart = ev => { const t0 = ev.touches[0]; touch = ev.touches.length === 1 && toCli() ? { x: t0.clientX, y: t0.clientY, axis: '' } : null; };
      const onTouchMove = ev => {
        if (!touch) return;
        const t1 = ev.touches[0], dx = t1.clientX - touch.x, dy = t1.clientY - touch.y;
        if (!touch.axis) { if (Math.abs(dx) < 6 && Math.abs(dy) < 6) return; touch.axis = Math.abs(dy) > Math.abs(dx) ? 'y' : 'x'; }
        if (touch.axis !== 'y') return;
        ev.preventDefault();
        scrollCli(-dy / cellHeight(), t1.clientX, t1.clientY);
        touch.x = t1.clientX; touch.y = t1.clientY;
      };
      const onTouchEnd = () => { touch = null; };
      el.addEventListener('touchstart', onTouchStart, { passive: true });
      el.addEventListener('touchmove', onTouchMove, { passive: false });
      el.addEventListener('touchend', onTouchEnd);
      el.addEventListener('wheel', onWheel, { capture: true, passive: false });
      el.addEventListener('paste', onPaste, true);
      el.addEventListener('dragover', onOver);
      el.addEventListener('dragleave', onLeave);
      el.addEventListener('drop', onDrop, true);
      detach = () => { el.removeEventListener('touchstart', onTouchStart); el.removeEventListener('touchmove', onTouchMove); el.removeEventListener('touchend', onTouchEnd); document.fonts?.removeEventListener?.('loadingdone', remeasure); window.removeEventListener('focus', onBack); document.removeEventListener('visibilitychange', onBack); el.removeEventListener('pointerdown', onBack); el.removeEventListener('wheel', onWheel, true); el.removeEventListener('paste', onPaste, true); el.removeEventListener('dragover', onOver); el.removeEventListener('dragleave', onLeave); el.removeEventListener('drop', onDrop, true); };
    })().catch(() => report('error'));
    return () => { alive = false; clearTimeout(resizeTimer); detach?.(); ro?.disconnect(); stopConn?.(); term?.dispose(); };
  }, [sessionId, readOnly]);
  // Un volet qui reçoit le focus donne le clavier à son terminal.
  useEffect(() => { if (focus && !readOnly) termRef.current?.focus(); }, [focus]);
  return html`<div class=${cls('cm-term', 'cm-term-' + state, over && 'drop-on')}>
    <div class="cm-term-box" ref=${box}></div>
    ${busy ? html`<div class="cm-term-state"><span class="spinner"></span>${t('agents.attach.sending')}</div>`
      : over ? html`<div class="cm-term-state"><${Icon} n="upload" />${t('agents.attach.drop')}</div>`
      : foreign && state === 'open' ? html`<button type="button" class="cm-term-state cm-term-foreign" onClick=${() => reclaimRef.current?.()}><${Icon} n="expand" />${t('agents.term.foreign')}</button>`
      : state !== 'open' && html`<div class="cm-term-state"><i class=${cls('dot', state === 'connecting' || state === 'disconnected' ? 'amber' : '')}></i>${lt('term', state)}</div>`}
  </div>`;
}
