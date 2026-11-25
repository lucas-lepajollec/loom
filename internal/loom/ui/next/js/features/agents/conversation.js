// Conversation d'une session : les blocs que Codeman reconstitue pour chaque
// CLI (prompt, réponse, statut, outil). Pour Claude ils viennent de sa
// transcription JSONL ; pour Codex de son rollout ; pour les autres CLI de la
// segmentation de l'écran. Rien n'est inventé : sans bloc, la vue renvoie au
// terminal natif.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls, fmtSecs } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Modal, toast } from '../../ui/dialog.js';
import { md } from '../chat/md.js';
import { api, sendPrompt, flattenPrompt } from './client.js';
import { agents, stateOf, capsOf, cliOf, projectOf } from './store.js';
import { agentDraftKey, interrupt, answerApproval, dismissApproval } from './actions.js';
import { gitFiles } from './inspector.js';
import { AgentTerminal } from './terminal.js';
import { filesOf, hasFiles, quotePath, uploadFiles } from './attach.js';

// Regroupe les statuts/outils consécutifs d'un tour en un pli d'activité.
export function groupBlocks(blocks) {
  const out = [];
  for (const b of blocks || []) {
    if (!b || typeof b.text !== 'string') continue;
    if (b.kind === 'status' || b.kind === 'tool') {
      const last = out[out.length - 1];
      if (last && last.kind === 'activity') last.items.push(b);
      else out.push({ kind: 'activity', items: [b] });
    } else {
      // Les réponses successives d'un même tour forment un seul message.
      const last = out[out.length - 1];
      if (b.kind === 'response' && last && last.kind === 'response' && last.turn === b.turn) out[out.length - 1] = { ...last, text: last.text + '\n\n' + b.text };
      else out.push(b);
    }
  }
  return out;
}

function Activity({ items }) {
  const [open, setOpen] = useState(false);
  const tools = items.filter(i => i.kind === 'tool');
  const start = items[0]?.timestamp;
  const end = items.at(-1)?.timestamp;
  const seconds = typeof start === 'number' && typeof end === 'number' && end > start ? (end - start) / 1000 : 0;
  const label = seconds >= 1 ? t('noir.worked', { duration: fmtSecs(seconds) })
    : tools.length ? t('agents.conv.tools', { n: tools.length }) : t('agents.conv.status', { n: items.length });
  return html`<div class=${cls('work', open && 'open')}>
    <button type="button" class="work-h" aria-expanded=${String(open)} onClick=${() => setOpen(!open)}>
      <${Icon} n="activity" /><span>${label}</span><${Icon} n="chevron" class="caret" /></button>
    ${open && html`<div class="work-b cm-activity">${items.map((b, i) => html`<details key=${i} class="agent-tool">
      <summary><${Icon} n=${b.kind === 'tool' ? 'tool' : 'activity'} /><span class="trunc">${b.text.split('\n')[0]}</span></summary>
      <pre class=${'cm-act ' + b.kind}>${b.text}</pre>
    </details>`)}</div>`}
  </div>`;
}

export function Approval({ item, session, onTerminal, children, disabled = false, onAlternative }) {
  const [busy, setBusy] = useState(false);
  const run = async body => {
    setBusy(true);
    try { await answerApproval(item, body); }
    catch (e) { toast(e.code === 'CONFLICT' ? t('agents.approval.gone') : e.message, 'err'); }
    finally { setBusy(false); }
  };
  const inTerminal = item.answerInTerminal || capsOf(session?.mode).approvals !== 'answer';
  return html`<div class="cm-approval anim-rise" role="alert">
    ${children}
    <div class="cm-approval-h"><${Icon} n="alert" /><b>${item.kind === 'question' ? t('agents.approval.question') : t('agents.approval.permission')}</b>
      ${item.toolName && html`<span class="tag">${item.toolName}</span>`}</div>
    ${item.message && item.message !== item.toolSummary && html`<p class="cm-approval-question">${item.message}</p>`}
    ${item.toolSummary && html`<pre class="cm-approval-msg">${item.toolSummary}</pre>`}
    ${item.context && html`<details class="cm-approval-ctx"><summary>${t('agents.approval.screen')}</summary><pre>${item.context}</pre></details>`}
    <div class="cm-approval-acts">
      ${inTerminal ? html`<span class="muted">${t('agents.approval.in_terminal')}</span><button class="btn sm primary" onClick=${onTerminal}><${Icon} n="terminal" />${t('agents.approval.open_terminal')}</button>`
        : html`${(item.options && item.options.length ? item.options : [{ n: 1, label: t('agents.approval.approve') }]).map(o => html`<button class=${cls('btn sm', o.n === 1 && 'primary')} disabled=${busy || disabled}
              onClick=${() => run(item.options && item.options.length ? { action: 'option', option: o.n } : { action: 'approve' })}>${o.label}</button>`)}
          <button class="btn sm" disabled=${busy || disabled} onClick=${() => run({ action: 'deny' })}>${t('agents.approval.deny')}</button>`}
      ${onAlternative && !inTerminal && item.kind === 'permission' && html`<button class="btn sm approval-alternative" disabled=${busy || disabled} onClick=${onAlternative}>${t('noir.alternative')}</button>`}
      <span class="grow"></span>
      <button class="btn sm" disabled=${busy || disabled} title=${t('agents.approval.dismiss_hint')} onClick=${() => dismissApproval(item).catch(e => toast(e.message, 'err'))}>${t('agents.approval.dismiss')}</button>
    </div></div>`;
}

export function Composer({ session, onTerminal, compact, phone, approvals = [], branch = '' }) {
  const state = stateOf(session);
  const draftKey = agentDraftKey(session.id);
  const [text, setText] = useState(() => { try { return sessionStorage.getItem(draftKey) || ''; } catch (_) { return ''; } });
  const [sending, setSending] = useState(false);
  const [files, setFiles] = useState([]);
  const [over, setOver] = useState(false);
  const [seen, setSeen] = useState('');
  const [alternative, setAlternative] = useState('');
  const host = useStore(agents, s => s.conn.ui_url || s.conn.url);
  let machine = '';
  try { machine = new URL(host).hostname; } catch (_) {}
  const approval = approvals[0];
  const canReplace = approvals.length === 1 && approval.kind === 'permission' && !approval.answerInTerminal && capsOf(session.mode).approvals === 'answer';
  const replacing = canReplace && (!phone || alternative === approval.id);
  const returnToComposer = () => {
    setSeen(approval.id); setAlternative(approval.id);
    requestAnimationFrame(() => area.current?.focus());
  };
  const area = useRef(), file = useRef(), inflight = useRef(false);
  const ready = state === 'idle' || state === 'working' || state === 'waiting';
  useEffect(() => { try { setText(sessionStorage.getItem(draftKey) || ''); } catch (_) { setText(''); } setFiles([]); }, [session.id]);
  useEffect(() => { try { text ? sessionStorage.setItem(draftKey, text) : sessionStorage.removeItem(draftKey); } catch (_) {} }, [text]);
  useEffect(() => { const a = area.current; if (a) { a.style.height = 'auto'; a.style.height = Math.min(compact || phone ? 140 : 240, a.scrollHeight) + 'px'; } }, [text, phone]);
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
    if (inflight.current || !ready || pending || !flattenPrompt(body()) || approvals.length > 0 && !replacing) return;
    inflight.current = true; setSending(true);
    try {
      if (replacing) {
        await answerApproval(approval, { action: 'deny' });
        if (agents.get().approvals.some(a => a.sessionId === session.id && a.kind !== 'idle')) {
          toast(t('noir.other_approval'));
          return;
        }
      }
      await sendPrompt(session.id, body()); setText(''); files.forEach(f => f.preview && URL.revokeObjectURL(f.preview)); setFiles([]);
    }
    catch (e) { toast(e.code === 'NOT_DELIVERED' ? t('agents.composer.not_delivered') : e.message, 'err'); }
    finally { inflight.current = false; setSending(false); area.current?.focus(); }
  };
  const onPaste = e => { const list = filesOf(e.clipboardData); if (list.length && ready) { e.preventDefault(); add(list); } };
  const dnd = {
    onDragOver: e => { if (hasFiles(e) && ready) { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; setOver(true); } },
    onDragLeave: e => { if (!e.currentTarget.contains(e.relatedTarget)) setOver(false); },
    onDrop: e => { setOver(false); if (!hasFiles(e) || !ready) return; e.preventDefault(); add(filesOf(e.dataTransfer)); },
  };
  const sendLabel = replacing ? t('noir.deny_and_send') : t('agents.composer.send');
  const showApproval = phone && approval && approval.id !== seen;
  const approvalCard = approval && html`<${Approval} key=${approval.id} item=${approval} session=${session} disabled=${sending}
    onAlternative=${canReplace ? returnToComposer : undefined} onTerminal=${() => { setSeen(approval.id); onTerminal?.(); }} />`;
  const touch = matchMedia('(hover: none)').matches;
  const hint = state === 'starting' ? t('agents.composer.starting') : state === 'stopped' ? t('agents.composer.stopped') : /\n/.test(text) ? t('agents.composer.single_line') : '';
  return html`<div class=${cls('composer-wrap cm-composer', compact && 'compact', phone && 'phone-composer')} ...${dnd}>
    <div class=${cls('composer', over && 'drop-on')}>
      ${!phone && approvals.map(a => html`<${Approval} key=${a.id} item=${a} session=${session} disabled=${sending} onTerminal=${onTerminal} />`)}
      ${phone && approval && html`<button class="btn sm approval-reopen" onClick=${() => setSeen('')}><${Icon} n="inbox" />${t('side.waiting_count', { n: approvals.length })}</button>`}
      ${files.length > 0 && html`<div class="cm-attach">${files.map(f => html`<span key=${f.key} class=${cls('cm-att', !f.path && 'busy')} title=${f.path || f.name}>
        ${f.preview ? html`<img src=${f.preview} alt="" />` : html`<${Icon} n="file" />`}
        <span class="trunc">${f.name}</span>${!f.path && html`<span class="spinner"></span>`}
        <button type="button" class="cm-att-x" aria-label=${t('agents.attach.remove')} onClick=${() => drop(f.key)}><${Icon} n="close" /></button></span>`)}</div>`}
      <input type="file" multiple ref=${file} hidden onChange=${e => { const list = [...(e.target.files || [])]; e.target.value = ''; add(list); }} />
      ${phone && html`<button type="button" class="icon-btn composer-add" aria-label=${t('agents.composer.attach')} disabled=${!ready} onClick=${() => file.current.click()}><${Icon} n="plus" /></button>`}
      <textarea ref=${area} rows="1" value=${text} disabled=${!ready && state !== 'starting'}
        aria-label=${t('chat.composer.message')} placeholder=${replacing ? t('noir.alternative') + '…' : t('agents.composer.placeholder', { cli: phone ? session.name || cliOf(session.mode).label : cliOf(session.mode).label })}
        onInput=${e => setText(e.target.value)} onPaste=${onPaste}
        onKeyDown=${e => { if (e.key === 'Enter' && !e.shiftKey && !touch && !e.isComposing) { e.preventDefault(); send(); } }}></textarea>
      <div class="composer-bar">
        ${!phone && html`<button class="icon-btn" aria-label=${t('agents.composer.attach')} title=${t('agents.composer.attach_hint')} disabled=${!ready} onClick=${() => file.current.click()}><${Icon} n="paperclip" /></button>`}
        ${onTerminal && html`<button class="icon-btn" aria-label=${t('agents.view.terminal')} title=${t('agents.view.terminal')} onClick=${onTerminal}><${Icon} n="terminal" /></button>`}
        ${!phone && html`<span class="grow composer-hint ${hint ? 'warn' : ''}">${over ? t('agents.attach.drop') : hint}</span>`}
        ${state === 'working' && html`<button class=${phone ? 'send stop' : 'btn sm'} aria-label=${t('agents.composer.interrupt')} onClick=${() => interrupt(session.id).catch(e => toast(e.message, 'err'))} title=${t('agents.composer.interrupt_hint')}><${Icon} n="stop" />${compact || phone ? '' : t('agents.composer.interrupt')}</button>`}
        ${(!phone || state !== 'working' || !!flattenPrompt(body())) && html`<button class=${phone ? 'send cm-send' : 'btn primary cm-send'} aria-label=${sendLabel} title=${sendLabel} disabled=${!ready || sending || pending || !flattenPrompt(body()) || approvals.length > 0 && !replacing} onClick=${send}>${sending ? html`<span class="spinner"></span>` : html`<${Icon} n="arrowUp" />`}</button>`}
      </div>
    </div>
    ${!compact && html`<div class="agent-location">
      ${machine && html`<span title=${machine}><${Icon} n="server" />${machine}</span>`}
      ${session.workingDir && html`<span title=${session.workingDir}><${Icon} n="folder" />${projectOf(session.workingDir)}</span>`}
      ${branch && html`<span class="mono" title=${branch}><${Icon} n="git" />${branch}</span>`}
    </div>`}
    ${showApproval && html`<${Modal} title=${approval.kind === 'question' ? t('agents.approval.question') : t('agents.approval.permission')}
      class="agent-approval-sheet" focusField=${false} onClose=${() => setSeen(approval.id)}>${approvalCard}</${Modal}>`}
    ${phone && hint && html`<span class="composer-hint warn">${hint}</span>`}
  </div>`;
}

export function Conversation({ session, onTerminal, terminalVisible, compact, phone, hidden, git, onReview }) {
  const id = session.id;
  const turn = useStore(agents, s => s.turns[id] || 0);
  const approvals = useStore(agents, s => s.approvals.filter(a => a.sessionId === id && a.kind !== 'idle'));
  const state = stateOf(session);
  const [blocks, setBlocks] = useState(null);
  const [failed, setFailed] = useState('');
  const [tools, setTools] = useState([]);
  const scroller = useRef(), stick = useRef(true), seq = useRef(0);
  const load = async () => {
    const mine = ++seq.current;
    try {
      const r = await api.transcript(id);
      if (mine !== seq.current) return;
      setBlocks(Array.isArray(r?.messages) ? r.messages : []); setFailed('');
    } catch (e) { if (mine === seq.current) setFailed(e.message); }
  };
  useEffect(() => { setBlocks(null); stick.current = true; load(); api.viewed(id).catch(() => {}); }, [id]);
  useEffect(() => { if (turn) load(); }, [turn]);
  // Pendant un tour, la transcription grandit sans événement dédié pour la
  // plupart des CLI : relecture régulière tant que la session travaille.
  useEffect(() => {
    if (state !== 'working' && state !== 'waiting') { setTools([]); return; }
    const timer = setInterval(() => {
      if (document.hidden) return;
      load();
      if (session.mode === 'claude') api.activeTools(id).then(r => setTools(Array.isArray(r?.tools) ? r.tools : [])).catch(() => {});
    }, 2500);
    return () => clearInterval(timer);
  }, [id, state]);
  useEffect(() => { const el = scroller.current; if (el && stick.current) el.scrollTop = el.scrollHeight; }, [blocks, approvals.length, state]);
  const grouped = groupBlocks(blocks);
  const changes = gitFiles(git);
  const branch = git?.repos?.length === 1 ? git.repos[0].status?.branch : '';
  const hasPrompt = (blocks || []).some(b => b.role === 'user');
  // Un prompt a été soumis mais Codeman ne sait pas le distinguer à l'écran de
  // ce CLI : l'écran en direct reste la vue fidèle.
  const submitted = !hasPrompt && !!session.lastSubmitAt;
  // Avant le premier prompt, les blocs ne sont que la bannière du CLI : on
  // montre son écran réel, où se règlent mises à jour et dialogues de confiance.
  const shown = hasPrompt ? grouped : [];
  const cli = cliOf(session.mode);
  return html`<div class=${cls('cm-conv', compact && 'compact')} hidden=${hidden}>
    <div class="thread" ref=${scroller} onScroll=${e => { const el = e.currentTarget; stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80; }}>
      <div class="thread-in">
        ${blocks === null && !failed && html`<div class="skeleton" style="height:64px"></div><div class="skeleton" style="height:120px;width:80%"></div>`}
        ${failed && html`<div class="msg-err"><${Icon} n="alert" /><span>${t('agents.conv.unavailable')} — ${failed}</span></div>`}
        ${blocks && !hasPrompt && !terminalVisible && !hidden && session.mode !== 'shell' && html`<div class=${cls('cm-boot anim-rise', submitted && 'live')}>
          <div class="cm-boot-h"><b>${state === 'starting' ? t('agents.conv.empty_starting', { cli: cli.label }) : submitted ? t('agents.conv.live_title', { cli: cli.label }) : t('agents.conv.boot_title', { cli: cli.label })}</b>
            <span class="muted">${submitted ? t('agents.conv.live_text') : t('agents.conv.boot_text')}</span></div>
          <div class="cm-boot-term"><${AgentTerminal} sessionId=${id} mode=${session.mode} scrollback=${2000} /></div></div>`}
        ${blocks && !grouped.length && (terminalVisible || session.mode === 'shell') && html`<div class="cm-conv-empty">
          <p>${t('agents.conv.empty', { cli: cli.label })}</p></div>`}
        ${hasPrompt && capsOf(session.mode).transcript === 'screen' && html`<p class="cm-screen-note muted"><${Icon} n="info" />${t('agents.conv.screen_note')}</p>`}
        ${shown.map((b, i) => b.kind === 'activity' ? html`<${Activity} key=${'a' + i} items=${b.items} />`
          : b.role === 'user' ? html`<div key=${i} class="msg-user"><div class="bubble" data-palette-message tabindex="-1">${b.text}</div></div>`
          : html`<div key=${i} class="msg-ai" data-palette-message tabindex="-1"><div class="md" dangerouslySetInnerHTML=${{ __html: md(b.text) }}></div></div>`)}
        ${changes.length > 0 && onReview && html`<div class="agent-changes card">
          <span><${Icon} n="file" />${t('noir.changed_files', { n: new Set(changes.map(f => f.repo + '/' + f.path)).size })}</span>
          <button type="button" class="btn sm" onClick=${onReview}>${t('noir.review')}</button></div>`}
        ${(state === 'working') && html`<div class="cm-working"><span class="working-text">${t('agents.conv.working', { cli: cli.label })}</span>
          ${tools.map(x => html`<span class="tag">${x.name || x.toolName || x}</span>`)}</div>`}
      </div>
    </div>
    <${Composer} session=${session} compact=${compact} phone=${phone} approvals=${hidden ? [] : approvals} branch=${branch} onTerminal=${onTerminal} />
  </div>`;
}
