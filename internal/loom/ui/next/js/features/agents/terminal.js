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
const viewport = () => innerWidth <= 760 ? 'mobile' : innerWidth <= 1100 ? 'tablet' : 'desktop';

export function theme() {
  return {
    background: cssVar('--bg-2') || '#0c0c0e', foreground: cssVar('--text') || '#ededef', cursor: cssVar('--text-2') || '#a1a1aa',
    cursorAccent: cssVar('--bg-2') || '#0c0c0e', selectionBackground: 'rgba(127,127,127,.32)',
  };
}

// fit : ajuste la grille au panneau ; sinon le terminal garde la géométrie
// courante du pane (mosaïque en lecture, plusieurs clients).
export function AgentTerminal({ sessionId, fit = true, fontSize = 13, focus = false, readOnly = false, onState }) {
  const box = useRef(), termRef = useRef();
  const [state, setState] = useState('connecting');
  const [busy, setBusy] = useState(false);
  const [over, setOver] = useState(false);
  const report = s => { setState(s); onState && onState(s); };
  useEffect(() => {
    let term, fitAddon, ws, ro, stopConn, detach, alive = true, resizeTimer;
    (async () => {
      const w = await loadXterm();
      if (!alive) return;
      const light = document.documentElement.dataset.theme === 'light';
      term = new w.Terminal({
        fontFamily: cssVar('--mono') || 'monospace', fontSize, lineHeight: 1.18, letterSpacing: 0, cursorBlink: false,
        cursorStyle: 'bar', scrollback: 8000, allowProposedApi: true, convertEol: false, minimumContrastRatio: light ? 4.5 : 1,
        disableStdin: readOnly, theme: theme(),
      });
      termRef.current = term;
      fitAddon = new w.FitAddon.FitAddon();
      term.loadAddon(fitAddon);
      term.open(box.current);
      try { fitAddon.fit(); } catch (_) {}
      const sendResize = force => {
        if (!fit || ws?.readyState !== 1) return;
        ws.send(JSON.stringify({ t: 'z', c: term.cols, r: term.rows, v: viewport(), ...(force ? { f: true } : {}) }));
      };
      // Un seul redimensionnement par geste : pendant le glissement d'un
      // séparateur de la grille, on attend qu'il soit lâché.
      const refit = () => {
        clearTimeout(resizeTimer);
        resizeTimer = setTimeout(() => {
          if (document.body.classList.contains('cm-dragging')) return refit();
          try { if (fit) fitAddon.fit(); sendResize(false); } catch (_) {}
        }, 150);
      };
      // Après un changement de géométrie, l'écran est relu une fois pour
      // effacer ce que les tailles intermédiaires ont laissé.
      let redrawTimer;
      const redraw = () => { clearTimeout(redrawTimer); redrawTimer = setTimeout(() => { if (alive) loadScreen(); }, 400); };
      const loadScreen = async () => {
        try {
          const r = await api.terminal(sessionId);
          if (!alive) return;
          term.reset();
          if (r?.terminalBuffer) term.write(r.terminalBuffer);
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
          else if (m.t === 'zc' && Number.isInteger(m.c) && Number.isInteger(m.r)) { if (m.c !== term.cols || m.r !== term.rows) term.resize(m.c, m.r); if (seen && (m.c !== seen.c || m.r !== seen.r)) redraw(); seen = { c: m.c, r: m.r }; }
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
      term.onData(d => { if (!readOnly && ws?.readyState === 1) ws.send(JSON.stringify({ t: 'i', d })); });
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
      el.addEventListener('paste', onPaste, true);
      el.addEventListener('dragover', onOver);
      el.addEventListener('dragleave', onLeave);
      el.addEventListener('drop', onDrop, true);
      detach = () => { el.removeEventListener('paste', onPaste, true); el.removeEventListener('dragover', onOver); el.removeEventListener('dragleave', onLeave); el.removeEventListener('drop', onDrop, true); };
    })().catch(() => report('error'));
    return () => { alive = false; clearTimeout(resizeTimer); detach?.(); ro?.disconnect(); stopConn?.(); term?.dispose(); };
  }, [sessionId, readOnly]);
  // Un volet qui reçoit le focus donne le clavier à son terminal.
  useEffect(() => { if (focus && !readOnly) termRef.current?.focus(); }, [focus]);
  return html`<div class=${cls('cm-term', 'cm-term-' + state, over && 'drop-on')}>
    <div class="cm-term-box" ref=${box}></div>
    ${busy ? html`<div class="cm-term-state"><span class="spinner"></span>${t('agents.attach.sending')}</div>`
      : over ? html`<div class="cm-term-state"><${Icon} n="upload" />${t('agents.attach.drop')}</div>`
      : state !== 'open' && html`<div class="cm-term-state"><i class=${cls('dot', state === 'connecting' || state === 'disconnected' ? 'amber' : '')}></i>${lt('term', state)}</div>`}
  </div>`;
}
