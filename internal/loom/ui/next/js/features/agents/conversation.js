import { extractUrlsFromText } from './panel-helpers.js';
import { mergePreviewUrls } from './panel-state.js';
// Conversation d'une session : les blocs que Codeman reconstitue pour chaque
// CLI (prompt, réponses, statuts, outils), rangés en tours avec le kit commun
// (features/conversation). Pour Claude ils viennent de sa transcription JSONL,
// pour Codex de son rollout, pour les autres CLI de la segmentation de
// l'écran. Les fichiers modifiés viennent de l'historique d'édition de
// Codeman, les commandes en cours de ses outils actifs. Rien n'est inventé :
// sans bloc, la vue renvoie au terminal natif.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useLayoutEffect, useRef, useMemo, useCallback, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { toast } from '../../ui/dialog.js';
import { md } from '../chat/md.js';
import { api, sendPrompt, flattenPrompt } from './client.js';
import { agents, stateOf, capsOf, cliOf } from './store.js';
import { agentDraftKey, interrupt, answerApproval, answerOnScreen, dismissApproval } from './actions.js';
import { AgentTerminal } from './terminal.js';
import { filesOf, hasFiles, quotePath, uploadFiles } from './attach.js';
import { buildTurns, changedFiles, screenChoices, duration } from '../conversation/turns.js';
import { WorkFold, workLabel, Step, Changes, TurnFoot, JumpBottom, Tray } from '../conversation/kit.js';
import { Minimap } from '../conversation/minimap.js';
import { openPreview, openPanelTab, mergeDetectedDocs, extractMentionedFiles, normalizeSessionPath } from './panel-state.js';

// Cache LRU par session (stale-while-revalidate) : revenir sur une conversation
// déjà ouverte l'affiche immédiatement sans squelettes, puis la rafraîchit en
// arrière-plan. Borné en nombre de sessions pour garder la mémoire légère.
const CONV_CACHE_MAX = 24;
const convCache = new Map();
function cacheGet(id) {
  const hit = convCache.get(id);
  if (!hit) return null;
  convCache.delete(id);
  convCache.set(id, hit);
  return hit;
}
function cacheSet(id, patch) {
  const next = { ...(convCache.get(id) || { blocks: null, edits: [], scrollTop: 0, stick: true }), ...patch };
  convCache.delete(id);
  convCache.set(id, next);
  while (convCache.size > CONV_CACHE_MAX) convCache.delete(convCache.keys().next().value);
}
export function evictConversationCache(id) {
  if (id) convCache.delete(id);
  else convCache.clear();
}

function sameBlocks(a, b) {
  if (a === b) return true;
  if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false;
  if (!a.length) return true;
  const i = a.length - 1;
  const x = a[i], y = b[i];
  if (x?.text !== y?.text || x?.role !== y?.role || x?.kind !== y?.kind || x?.timestamp !== y?.timestamp) return false;
  const f1 = a[0], f2 = b[0];
  if (f1?.text !== f2?.text || f1?.timestamp !== f2?.timestamp) return false;
  if (i > 1) {
    const p1 = a[i - 1], p2 = b[i - 1];
    if (p1?.text !== p2?.text || p1?.timestamp !== p2?.timestamp) return false;
  }
  return true;
}

function sameEdits(a, b) {
  if (a === b) return true;
  if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false;
  if (!a.length) return true;
  const i = a.length - 1;
  return a[i]?.id === b[i]?.id && a[0]?.id === b[0]?.id;
}

function sameTools(a, b) {
  if (a === b) return true;
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i]?.id !== b[i]?.id || a[i]?.command !== b[i]?.command || a[i]?.startedAt !== b[i]?.startedAt) return false;
  }
  return true;
}

// Choix d'une demande : réponse par Codeman (Claude), sinon touche lue à
// l'écran (Codex…), sinon renvoi au terminal.
export function approvalChoices(item, session, run) {
  const answerable = !item.answerInTerminal && capsOf(session?.mode).approvals === 'answer';
  if (answerable) {
    const options = item.options?.length ? item.options : [{ n: 1, label: t('agents.approval.approve') }];
    const list = options.map((o, i) => ({ label: o.label, primary: i === 0 && item.kind !== 'question',
      run: () => run(() => answerApproval(item, item.options?.length ? { action: 'option', option: o.n } : { action: 'approve' })) }));
    if (item.kind !== 'question') list.push({ label: t('agents.approval.deny'), deny: true, hint: 'Esc', run: () => run(() => answerApproval(item, { action: 'deny' })) });
    return { list, deny: () => answerApproval(item, { action: 'deny' }) };
  }
  const screen = screenChoices(item.context);
  if (screen.length) {
    return {
      list: screen.map((c, i) => ({ label: c.label, hint: c.hint, primary: i === 0 && !c.deny, deny: c.deny, run: () => run(() => answerOnScreen(item, c)) })),
      deny: () => { const d = screen.find(c => c.deny); return d ? answerOnScreen(item, d) : Promise.resolve(); },
    };
  }
  return { list: [], deny: null };
}

export function ApprovalTray({ item, session, onTerminal }) {
  const [busy, setBusy] = useState(false);
  const cli = cliOf(session?.mode).label;
  const run = async fn => {
    setBusy(true);
    try { await fn(); }
    catch (e) { toast(e.code === 'CONFLICT' ? t('agents.approval.gone') : e.message, 'err'); }
    finally { setBusy(false); }
  };
  const { list } = approvalChoices(item, session, run);
  const question = item.kind === 'question';
  const title = question ? (item.message || item.toolSummary || t('agents.tray.question', { cli }))
    : item.toolName ? t('agents.tray.wants_tool', { cli, tool: item.toolName }) : t('agents.tray.wants', { cli });
  const detail = question ? '' : item.toolSummary || item.message || '';
  const extra = !list.length && onTerminal && html`<button type="button" class="cv-choice" onClick=${onTerminal}><span class="cv-choice-t"><span>${t('agents.approval.open_terminal')}</span></span></button>`;
  const aside = html`<button type="button" class="cv-quiet" disabled=${busy} title=${t('agents.approval.dismiss_hint')} onClick=${() => dismissApproval(item).catch(e => toast(e.message, 'err'))}>${t('agents.approval.dismiss')}</button>`;
  return html`<${Tray} icon=${question ? 'info' : 'lock'} title=${title} detail=${detail} busy=${busy} vertical=${question}
    meta=${!list.length ? t('agents.approval.in_terminal') : ''} choices=${list} extra=${extra} aside=${aside} />`;
}

// Gardé pour la boîte de réception : la même carte, hors du composeur.
export function Approval({ item, session, onTerminal, children }) {
  return html`<div class="cv-approval">${children}<${ApprovalTray} item=${item} session=${session} onTerminal=${onTerminal} /></div>`;
}

export function Composer({ session, onTerminal, compact, phone, approval }) {
  const state = stateOf(session);
  const draftKey = agentDraftKey(session.id);
  const [text, setText] = useState(() => { try { return sessionStorage.getItem(draftKey) || ''; } catch (_) { return ''; } });
  const [sending, setSending] = useState(false);
  const [files, setFiles] = useState([]);
  const [over, setOver] = useState(false);
  const area = useRef(), file = useRef(), inflight = useRef(false), prevId = useRef(session.id);
  const ready = state === 'idle' || state === 'working' || state === 'waiting';
  const cli = cliOf(session.mode);
  useEffect(() => {
    if (prevId.current === session.id) return;
    prevId.current = session.id;
    try { setText(sessionStorage.getItem(draftKey) || ''); } catch (_) { setText(''); }
    setFiles([]);
  }, [session.id]);
  useEffect(() => { try { text ? sessionStorage.setItem(draftKey, text) : sessionStorage.removeItem(draftKey); } catch (_) {} }, [text]);
  useEffect(() => { const a = area.current; if (a) { a.style.height = 'auto'; a.style.height = Math.min(compact || phone ? 140 : Math.round(innerHeight * 0.4), a.scrollHeight) + 'px'; } }, [text, phone]);
  useEffect(() => () => files.forEach(f => f.preview && URL.revokeObjectURL(f.preview)), []);
  // Chaque fichier part tout de suite vers Codeman ; le prompt n'est envoyé
  // qu'une fois tous les chemins connus.
  const add = list => {
    if (!list.length) return;
    const items = list.map(f => ({ key: Math.random().toString(36).slice(2), name: f.name, image: /^image\//.test(f.type), preview: /^image\//.test(f.type) ? URL.createObjectURL(f) : '', path: '', error: '' }));
    setFiles(cur => [...cur, ...items]);
    items.forEach((it, i) => uploadFiles(session.id, [list[i]])
      .then(([path]) => setFiles(cur => cur.map(x => x.key === it.key ? { ...x, path } : x)))
      .catch(e => { toast(e.message, 'err'); setFiles(cur => cur.filter(x => x.key !== it.key)); }));
    area.current?.focus();
  };
  const drop = key => setFiles(cur => { const f = cur.find(x => x.key === key); if (f?.preview) URL.revokeObjectURL(f.preview); return cur.filter(x => x.key !== key); });
  const pending = files.some(f => !f.path);
  const body = () => [files.map(f => quotePath(f.path)).join(' '), text].filter(x => flattenPrompt(x)).join(' ');
  const send = async () => {
    if (inflight.current || !ready || pending || !flattenPrompt(body())) return;
    inflight.current = true; setSending(true);
    try {
      // Une demande est ouverte : écrire, c'est dire à l'agent quoi faire à la
      // place. On la refuse d'abord, puis on envoie le message.
      if (approval) {
        const { deny } = approvalChoices(approval, session, fn => fn());
        if (deny) { await deny(); await new Promise(r => setTimeout(r, 400)); }
      }
      await sendPrompt(session.id, body()); setText(''); files.forEach(f => f.preview && URL.revokeObjectURL(f.preview)); setFiles([]);
    } catch (e) { toast(e.code === 'NOT_DELIVERED' ? t('agents.composer.not_delivered') : e.code === 'CONFLICT' ? t('agents.approval.gone') : e.message, 'err'); }
    finally { inflight.current = false; setSending(false); area.current?.focus(); }
  };
  const onPaste = e => { const list = filesOf(e.clipboardData); if (list.length && ready) { e.preventDefault(); add(list); } };
  const dnd = {
    onDragOver: e => { if (hasFiles(e) && ready) { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; setOver(true); } },
    onDragLeave: e => { if (!e.currentTarget.contains(e.relatedTarget)) setOver(false); },
    onDrop: e => { setOver(false); if (!hasFiles(e) || !ready) return; e.preventDefault(); add(filesOf(e.dataTransfer)); },
  };
  const touch = matchMedia('(hover: none)').matches;
  const hint = state === 'starting' ? t('agents.composer.starting') : state === 'stopped' ? t('agents.composer.stopped') : /\n/.test(text) ? t('agents.composer.single_line') : '';
  const placeholder = approval ? t('agents.tray.instead', { cli: cli.label })
    : t('agents.composer.placeholder', { cli: phone ? session.name || cli.label : cli.label });
  const model = session.displayModel?.model;
  const canSend = ready && !sending && !pending && !!flattenPrompt(body());
  return html`<div class=${cls('composer-wrap cm-composer cv-comp', compact && 'compact', phone && 'phone-composer')} ...${dnd}>
    <div class="cv-stack">
      ${approval && html`<${ApprovalTray} key=${approval.id} item=${approval} session=${session} onTerminal=${onTerminal} />`}
      <div class=${cls('composer', over && 'drop-on')}>
        ${files.length > 0 && html`<div class="cm-attach">${files.map(f => html`<span key=${f.key} class=${cls('cm-att', !f.path && 'busy')} title=${f.path || f.name}>
          ${f.preview ? html`<img src=${f.preview} alt="" />` : html`<${Icon} n="file" />`}
          <span class="trunc">${f.name}</span>${!f.path && html`<span class="spinner"></span>`}
          <button type="button" class="cm-att-x" aria-label=${t('agents.attach.remove')} onClick=${() => drop(f.key)}><${Icon} n="close" /></button></span>`)}</div>`}
        <input type="file" multiple ref=${file} hidden onChange=${e => { const list = [...(e.target.files || [])]; e.target.value = ''; add(list); }} />
        <textarea ref=${area} rows="1" value=${text} disabled=${!ready && state !== 'starting'}
          aria-label=${t('chat.composer.message')} placeholder=${placeholder}
          onInput=${e => setText(e.target.value)} onPaste=${onPaste}
          onKeyDown=${e => { if (e.key === 'Enter' && !e.shiftKey && !touch && !e.isComposing) { e.preventDefault(); send(); } }}></textarea>
        <div class="composer-bar">
          <button type="button" class="icon-btn" aria-label=${t('agents.composer.attach')} title=${t('agents.composer.attach_hint')} disabled=${!ready} onClick=${() => file.current.click()}><${Icon} n="plus" /></button>
          ${model && !phone && html`<span class="cv-chip" title=${t('agents.info.model')}>${model}</span>`}
          ${onTerminal && !phone && html`<button type="button" class="icon-btn" aria-label=${t('agents.view.terminal')} title=${t('agents.view.terminal')} onClick=${onTerminal}><${Icon} n="terminal" /></button>`}
          <span class=${cls('grow composer-hint', hint && 'warn')}>${over ? t('agents.attach.drop') : phone ? '' : hint}</span>
          ${state === 'working' && !canSend ? html`<button type="button" class="send stop" aria-label=${t('agents.composer.interrupt')} title=${t('agents.composer.interrupt_hint')}
              onClick=${() => interrupt(session.id).catch(e => toast(e.message, 'err'))}><${Icon} n="stop" /></button>`
            : html`<button type="button" class="send cm-send" aria-label=${t('agents.composer.send')} disabled=${!canSend} onClick=${send}>${sending ? html`<span class="spinner"></span>` : html`<${Icon} n="arrowUp" />`}</button>`}
        </div>
      </div>
      ${!phone && !compact && html`<div class="cv-hint"><span><kbd>${t('conv.key_enter')}</kbd> ${t('conv.send')}</span><span><kbd>${t('conv.key_shift_enter')}</kbd> ${t('conv.newline')}</span>${state === 'working' && html`<span><kbd>Esc</kbd> ${t('agents.composer.interrupt').toLowerCase()}</span>`}</div>`}
      ${phone && hint && html`<span class="composer-hint warn">${hint}</span>`}
    </div>
  </div>`;
}

// Demande de l'utilisateur : les fichiers joints (chemins déposés par Codeman)
// deviennent des puces ; une interruption est une simple ligne.
const UPLOAD = /(?:^|\s)(\/\S*\.codeman-uploads\/\S+)/g;
function Prompt({ text }) {
  if (/^\[Request interrupted by user[^\]]*\]$/.test(text.trim())) return html`<div class="cv-note">${t('conv.interrupted')}</div>`;
  const files = [...text.matchAll(UPLOAD)].map(m => m[1]);
  const rest = text.replace(UPLOAD, ' ').trim();
  return html`<div class="msg-user" data-mark="user">
    ${files.length > 0 && html`<div class="cv-atts">${files.map(f => html`<span class="cv-att" title=${f}><${Icon} n="file" />${f.split('/').pop().replace(/^paste-\d+-/, '')}</span>`)}</div>`}
    ${rest && html`<div class="bubble">${rest}</div>`}</div>`;
}

// Lignes de travail d'un tour : ce que l'agent dit en route, ses étapes.
const WorkItems = ({ items }) => items.map((x, i) => x.k === 'say'
  ? html`<div key=${i} class="cv-say md" dangerouslySetInnerHTML=${{ __html: md(x.text) }}></div>`
  : html`<${Step} key=${i} icon=${x.kind === 'tool' ? 'terminal' : 'activity'} title=${x.full}>${x.text}</${Step}>`);

function Turn({ turn, tools, waiting, loadDiff, model, workingDir, onPreview, onReview }) {
  const changes = useMemo(() => changedFiles(turn.edits), [turn.edits]);
  const answerHtml = useMemo(() => (turn.answer ? md(turn.answer) : ''), [turn.answer]);
  const docs = useMemo(() => extractMentionedFiles([turn], workingDir).slice(0, 4), [turn.answer, turn.items, turn.work, workingDir]);
  if (turn.live) {
    return html`<div class="cv-turn msg-ai" data-mark="ai">
      <${WorkItems} items=${turn.items} />
      ${waiting ? html`<${Step} live>${t('agents.tray.waiting')}</${Step}>`
        : tools.length ? tools.map(x => html`<${Step} key=${x.id || x.command} live since=${x.startedAt}>${t('conv.running')} <code>${x.command}</code></${Step}>`)
        : html`<${Step} live since=${turn.start || Date.now()}>${t('conv.working')}</${Step}>`}
      <${Changes} changes=${changes} loadDiff=${loadDiff} onReview=${onReview} />
    </div>`;
  }
  return html`<div class="cv-turn msg-ai" data-mark="ai">
    ${turn.work.length > 0 && html`<${WorkFold} label=${workLabel(turn.ms)}><${WorkItems} items=${turn.work} /></${WorkFold}>`}
    ${answerHtml && html`<div class="md" dangerouslySetInnerHTML=${{ __html: answerHtml }}></div>`}
    ${docs.length > 0 && html`<div class="cv-docs">${docs.map(p => {
      const name = p.slice(p.lastIndexOf('/') + 1);
      return html`<button type="button" key=${p} class="cv-doc-chip" title=${p} onClick=${() => onPreview(p)}>
        <${Icon} n="file" /><span class="trunc">${name}</span><${Icon} n="eye" class="cv-doc-eye" />
      </button>`;
    })}</div>`}
    <${Changes} changes=${changes} loadDiff=${loadDiff} onReview=${onReview} />
    ${turn.answer && html`<${TurnFoot} parts=${[turn.ms >= 1000 && duration(turn.ms), model]} copy=${turn.answer} />`}
  </div>`;
}

export function Conversation({ session, onTerminal, terminalVisible, compact, phone }) {
  const id = session.id;
  const turnSeq = useStore(agents, s => s.turns[id] || 0);
  // Retourne directement le dernier accord actif de cette session (ou null) :
  // évite de créer un tableau neuf et de re-rendre toute la conversation à
  // chaque événement SSE des autres sessions.
  const approval = useStore(agents, s => {
    const list = s.approvals;
    for (let i = list.length - 1; i >= 0; i--) {
      const a = list[i];
      if (a && a.sessionId === id && a.kind !== 'idle') return a;
    }
    return null;
  });
  const state = stateOf(session);
  const live = state === 'working' || state === 'waiting';
  const initial = cacheGet(id);
  const [blocks, setBlocks] = useState(() => initial?.blocks ?? null);
  const [edits, setEdits] = useState(() => initial?.edits ?? []);
  const [failed, setFailed] = useState('');
  const [tools, setTools] = useState([]);
  const [count, setCount] = useState(30);
  const scroller = useRef();
  const stick = useRef(initial ? initial.stick : true);
  const restoreScroll = useRef(initial && !initial.stick ? initial.scrollTop : -1);
  const seq = useRef(0);
  const inflight = useRef(null);
  const editsInflight = useRef(null);
  const blocksRef = useRef(blocks);
  blocksRef.current = blocks;
  const editsRef = useRef(edits);
  editsRef.current = edits;

  const load = useCallback(async (sync, { force = false } = {}) => {
    // Évite d'empiler des lectures complètes de transcription quand un sondage
    // arrive avant la fin du précédent.
    if (!force && inflight.current) return;
    if (inflight.current) { inflight.current.abort(); inflight.current = null; }
    const ac = new AbortController();
    inflight.current = ac;
    const mine = ++seq.current;
    // L'historique d'édition peut être long à relire : il n'attend pas le fil,
    // et n'est pas lancé en double.
    if (session.mode === 'claude' && (force || !editsInflight.current)) {
      if (editsInflight.current) editsInflight.current.abort();
      const eac = new AbortController();
      editsInflight.current = eac;
      api.edits(id, sync, { signal: eac.signal }).then(e => {
        if (mine !== seq.current || !Array.isArray(e?.entries)) return;
        if (!sameEdits(editsRef.current, e.entries)) {
          setEdits(e.entries);
          cacheSet(id, { edits: e.entries });
        }
      }).catch(() => {}).finally(() => { if (editsInflight.current === eac) editsInflight.current = null; });
    }
    try {
      const r = await api.transcript(id, { signal: ac.signal });
      if (mine !== seq.current) return;
      const next = Array.isArray(r?.messages) ? r.messages : [];
      if (!sameBlocks(blocksRef.current, next)) {
        setBlocks(next);
        cacheSet(id, { blocks: next });
      } else if (blocksRef.current === null) {
        setBlocks(next);
        cacheSet(id, { blocks: next });
      }
      setFailed('');
    } catch (e) {
      if (e?.name === 'AbortError') return;
      if (mine === seq.current && blocksRef.current === null) setFailed(e.message);
    } finally {
      if (inflight.current === ac) inflight.current = null;
    }
  }, [id, session.mode]);

  useEffect(() => {
    const cached = cacheGet(id);
    setBlocks(cached?.blocks ?? null);
    setEdits(cached?.edits ?? []);
    setFailed('');
    setCount(30);
    stick.current = cached ? cached.stick : true;
    restoreScroll.current = cached && !cached.stick ? cached.scrollTop : -1;
    // Avec un cache chaud, l'ouverture est immédiate ; la synchronisation des
    // éditions suit sans bloquer la transcription.
    load(!cached, { force: true });
    api.viewed(id).catch(() => {});
    const onVis = () => { if (!document.hidden) load(false); };
    document.addEventListener('visibilitychange', onVis);
    return () => {
      document.removeEventListener('visibilitychange', onVis);
      const el = scroller.current;
      if (el) cacheSet(id, { scrollTop: el.scrollTop, stick: stick.current });
      if (inflight.current) { inflight.current.abort(); inflight.current = null; }
      if (editsInflight.current) { editsInflight.current.abort(); editsInflight.current = null; }
    };
  }, [id, load]);

  useEffect(() => { if (turnSeq) load(true, { force: true }); }, [turnSeq, load]);

  // Pendant un tour, la transcription grandit sans événement dédié pour la
  // plupart des CLI : relecture régulière tant que la session travaille.
  useEffect(() => {
    if (!live) { setTools(prev => prev.length ? [] : prev); return; }
    let n = 0, alive = true, toolsBusy = false;
    const pollTools = () => {
      if (session.mode !== 'claude' || toolsBusy) return;
      toolsBusy = true;
      api.activeTools(id).then(r => {
        if (!alive) return;
        const next = Array.isArray(r?.tools) ? r.tools.filter(x => x && x.command) : [];
        setTools(prev => sameTools(prev, next) ? prev : next);
      }).catch(() => {}).finally(() => { toolsBusy = false; });
    };
    pollTools();
    const timer = setInterval(() => {
      if (document.hidden) return;
      load(++n % 4 === 0);
      pollTools();
    }, 2500);
    return () => { alive = false; clearInterval(timer); };
  }, [id, live, session.mode, load]);

  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    if (restoreScroll.current >= 0) {
      el.scrollTop = restoreScroll.current;
      restoreScroll.current = -1;
      return;
    }
    if (stick.current) el.scrollTop = el.scrollHeight;
  }, [blocks, edits, approval?.id, state, tools.length]);

  const turns = useMemo(() => buildTurns(blocks, edits, live), [blocks, edits, live]);
  useEffect(() => {
    const text = turns.slice(-6).flatMap(turn => [turn.answer, turn.prompt, ...(turn.items || []).map(item => item.text), ...(turn.work || []).map(item => item.text)]).filter(Boolean).join('\n');
    mergePreviewUrls(id, extractUrlsFromText(text));
  }, [id, turns]);
  const detectedDocs = useMemo(() => extractMentionedFiles(turns, session.workingDir), [turns, session.workingDir]);
  useEffect(() => { if (detectedDocs.length) mergeDetectedDocs(id, detectedDocs); }, [id, detectedDocs]);
  const onPreview = useCallback(path => openPreview(id, path), [id]);
  const onReview = useCallback(() => openPanelTab('changes'), []);
  const onThreadClick = useCallback(e => {
    const a = e.target.closest('a');
    if (a) {
      const raw = a.getAttribute('data-file-href') || a.getAttribute('href') || '';
      const rel = normalizeSessionPath(raw, session.workingDir);
      if (rel) { e.preventDefault(); openPreview(id, rel); }
      return;
    }
    const code = e.target.closest('code');
    if (code && !code.closest('pre')) {
      const rel = normalizeSessionPath(code.textContent, session.workingDir);
      if (rel) openPreview(id, rel);
    }
  }, [id, session.workingDir]);
  const hasPrompt = turns.length > 0;
  // Un prompt a été soumis mais Codeman ne sait pas le distinguer à l'écran de
  // ce CLI : l'écran en direct reste la vue fidèle.
  const submitted = !hasPrompt && !!session.lastSubmitAt;
  const cli = cliOf(session.mode);
  const loadDiff = useCallback(async file => {
    const parts = await Promise.all(file.ids.map(e => api.editDiff(id, e)));
    return { text: parts.map(p => p?.diff || '').join('\n'), truncated: parts.some(p => p?.truncated) };
  }, [id]);
  const model = session.displayModel?.model;
  const onScroll = e => {
    const el = e.currentTarget;
    stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  };
  return html`<div class=${cls('cm-conv', compact && 'compact')}>
    <div class="cv-wrap">
      ${!compact && !phone && html`<${Minimap} scroller=${scroller} />`}
      <div class="thread" ref=${scroller} onScroll=${onScroll}>
        <div class="thread-in" onClick=${onThreadClick}>
          ${blocks === null && !failed && html`<div class="skeleton" style="height:64px"></div><div class="skeleton" style="height:120px;width:80%"></div>`}
          ${failed && html`<div class="msg-err"><${Icon} n="alert" /><span>${t('agents.conv.unavailable')} — ${failed}</span></div>`}
          ${blocks && !hasPrompt && !terminalVisible && session.mode !== 'shell' && html`<div class=${cls('cm-boot anim-rise', submitted && 'live')}>
            <div class="cm-boot-h"><b>${state === 'starting' ? t('agents.conv.empty_starting', { cli: cli.label }) : submitted ? t('agents.conv.live_title', { cli: cli.label }) : t('agents.conv.boot_title', { cli: cli.label })}</b>
              <span class="muted">${submitted ? t('agents.conv.live_text') : t('agents.conv.boot_text')}</span></div>
            <div class="cm-boot-term"><${AgentTerminal} sessionId=${id} mode=${session.mode} workingDir=${session.workingDir} scrollback=${2000} /></div></div>`}
          ${blocks && !hasPrompt && (terminalVisible || session.mode === 'shell') && html`<div class="cm-conv-empty">
            <p>${t('agents.conv.empty', { cli: cli.label })}</p></div>`}
          ${hasPrompt && capsOf(session.mode).transcript === 'screen' && html`<p class="cm-screen-note muted"><${Icon} n="info" />${t('agents.conv.screen_note')}</p>`}
          ${turns.length > count && html`<button type="button" class="cv-more" onClick=${() => { stick.current = false; setCount(count + 30); }}>${t('conv.earlier', { n: turns.length - count })}</button>`}
          ${turns.slice(-count).map(turn => html`<div key=${turn.key} class="cv-pair">
            <${Prompt} text=${turn.prompt} />
            <${Turn} turn=${turn} tools=${turn.live ? tools : EMPTY_TOOLS} waiting=${turn.live && !!approval} loadDiff=${loadDiff} model=${model} workingDir=${session.workingDir} onPreview=${onPreview} onReview=${onReview} />
          </div>`)}
        </div>
      </div>
      <${JumpBottom} scroller=${scroller} watch=${turns.length} />
    </div>
    <${Composer} session=${session} compact=${compact} phone=${phone} onTerminal=${onTerminal} approval=${approval} />
  </div>`;
}
const EMPTY_TOOLS = [];

