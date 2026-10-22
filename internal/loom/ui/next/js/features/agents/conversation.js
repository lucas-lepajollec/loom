// Conversation d'une session : les blocs que Codeman reconstitue pour chaque
// CLI (prompt, réponse, statut, outil). Pour Claude ils viennent de sa
// transcription JSONL ; pour Codex de son rollout ; pour les autres CLI de la
// segmentation de l'écran. Rien n'est inventé : sans bloc, la vue renvoie au
// terminal natif.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { toast } from '../../ui/dialog.js';
import { md } from '../chat/md.js';
import { api, sendPrompt, flattenPrompt } from './client.js';
import { agents, stateOf, approvalsFor, capsOf, cliOf } from './store.js';
import { interrupt, answerApproval, dismissApproval } from './actions.js';
import { AgentTerminal } from './terminal.js';

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
  const tools = items.filter(i => i.kind === 'tool').length;
  return html`<div class=${cls('work', open && 'open')}>
    <button type="button" class="work-h" aria-expanded=${String(open)} onClick=${() => setOpen(!open)}>
      <${Icon} n=${tools ? 'tool' : 'activity'} /><span>${tools ? t('agents.conv.tools', { n: tools }) : t('agents.conv.status', { n: items.length })}</span><${Icon} n="chevron" class="caret" /></button>
    ${open && html`<div class="work-b cm-activity">${items.map((b, i) => html`<pre key=${i} class=${'cm-act ' + b.kind}>${b.text}</pre>`)}</div>`}
  </div>`;
}

function Approval({ item, session, onTerminal }) {
  const [busy, setBusy] = useState(false);
  const run = async body => {
    setBusy(true);
    try { await answerApproval(item, body); }
    catch (e) { toast(e.code === 'CONFLICT' ? t('agents.approval.gone') : e.message, 'err'); }
    finally { setBusy(false); }
  };
  const inTerminal = item.answerInTerminal || capsOf(session.mode).approvals !== 'answer';
  return html`<div class="cm-approval anim-rise" role="alert">
    <div class="cm-approval-h"><${Icon} n="alert" /><b>${item.kind === 'question' ? t('agents.approval.question') : t('agents.approval.permission')}</b>
      ${item.toolName && html`<span class="tag">${item.toolName}</span>`}</div>
    ${(item.toolSummary || item.message) && html`<p class="cm-approval-msg">${item.toolSummary || item.message}</p>`}
    ${item.context && html`<details class="cm-approval-ctx"><summary>${t('agents.approval.screen')}</summary><pre>${item.context}</pre></details>`}
    <div class="cm-approval-acts">
      ${inTerminal ? html`<span class="muted">${t('agents.approval.in_terminal')}</span><button class="btn sm primary" onClick=${onTerminal}><${Icon} n="terminal" />${t('agents.approval.open_terminal')}</button>`
        : html`${(item.options && item.options.length ? item.options : [{ n: 1, label: t('agents.approval.approve') }]).map(o => html`<button class=${cls('btn sm', o.n === 1 && 'primary')} disabled=${busy}
              onClick=${() => run(item.options && item.options.length ? { action: 'option', option: o.n } : { action: 'approve' })}>${o.label}</button>`)}
          <button class="btn sm ghost danger" disabled=${busy} onClick=${() => run({ action: 'deny' })}>${t('agents.approval.deny')}</button>`}
      <span class="grow"></span>
      <button class="btn sm ghost" disabled=${busy} title=${t('agents.approval.dismiss_hint')} onClick=${() => dismissApproval(item).catch(e => toast(e.message, 'err'))}>${t('agents.approval.dismiss')}</button>
    </div></div>`;
}

export function Composer({ session, onTerminal }) {
  const state = stateOf(session);
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [uploading, setUploading] = useState(false);
  const area = useRef(), file = useRef(), inflight = useRef(false);
  const ready = state === 'idle' || state === 'working' || state === 'waiting';
  const draftKey = 'loom.codeman.draft.' + session.id;
  useEffect(() => { try { setText(sessionStorage.getItem(draftKey) || ''); } catch (_) { setText(''); } }, [session.id]);
  useEffect(() => { try { text ? sessionStorage.setItem(draftKey, text) : sessionStorage.removeItem(draftKey); } catch (_) {} }, [text]);
  useEffect(() => { const a = area.current; if (a) { a.style.height = 'auto'; a.style.height = Math.min(240, a.scrollHeight) + 'px'; } }, [text]);
  const send = async () => {
    if (inflight.current || !ready || !flattenPrompt(text)) return;
    inflight.current = true; setSending(true);
    try { await sendPrompt(session.id, text); setText(''); }
    catch (e) { toast(e.code === 'NOT_DELIVERED' ? t('agents.composer.not_delivered') : e.message, 'err'); }
    finally { inflight.current = false; setSending(false); area.current?.focus(); }
  };
  const attach = async e => {
    const f = e.target.files && e.target.files[0]; e.target.value = '';
    if (!f) return;
    setUploading(true);
    try { const r = await api.upload(session.id, f); if (r?.path) setText(v => (v ? v.replace(/\s*$/, ' ') : '') + r.path + ' '); }
    catch (err) { toast(err.message, 'err'); }
    finally { setUploading(false); }
  };
  const touch = matchMedia('(hover: none)').matches;
  const hint = state === 'starting' ? t('agents.composer.starting') : state === 'stopped' ? t('agents.composer.stopped') : /\n/.test(text) ? t('agents.composer.single_line') : '';
  return html`<div class="composer-wrap cm-composer">
    <div class="composer">
      <textarea ref=${area} rows="1" value=${text} disabled=${!ready && state !== 'starting'}
        placeholder=${t('agents.composer.placeholder', { cli: cliOf(session.mode).label })}
        onInput=${e => setText(e.target.value)}
        onKeyDown=${e => { if (e.key === 'Enter' && !e.shiftKey && !touch && !e.isComposing) { e.preventDefault(); send(); } }}></textarea>
      <div class="composer-bar">
        <input type="file" ref=${file} hidden onChange=${attach} />
        <button class="icon-btn" aria-label=${t('agents.composer.attach')} title=${t('agents.composer.attach')} disabled=${!ready || uploading} onClick=${() => file.current.click()}>${uploading ? html`<span class="spinner"></span>` : html`<${Icon} n="paperclip" />`}</button>
        <button class="icon-btn" aria-label=${t('agents.view.terminal')} title=${t('agents.view.terminal')} onClick=${onTerminal}><${Icon} n="terminal" /></button>
        <span class="grow composer-hint ${hint ? 'warn' : ''}">${hint}</span>
        ${state === 'working' && html`<button class="btn sm" onClick=${() => interrupt(session.id).catch(e => toast(e.message, 'err'))} title=${t('agents.composer.interrupt_hint')}><${Icon} n="stop" />${t('agents.composer.interrupt')}</button>`}
        <button class="cm-send" aria-label=${t('agents.composer.send')} disabled=${!ready || sending || !flattenPrompt(text)} onClick=${send}>${sending ? html`<span class="spinner"></span>` : html`<${Icon} n="arrowUp" />`}</button>
      </div>
    </div>
  </div>`;
}

export function Conversation({ session, onTerminal, terminalVisible }) {
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
  const hasPrompt = (blocks || []).some(b => b.role === 'user');
  // Un prompt a été soumis mais Codeman ne sait pas le distinguer à l'écran de
  // ce CLI : l'écran en direct reste la vue fidèle.
  const submitted = !hasPrompt && !!session.lastSubmitAt;
  // Avant le premier prompt, les blocs ne sont que la bannière du CLI : on
  // montre son écran réel, où se règlent mises à jour et dialogues de confiance.
  const shown = hasPrompt ? grouped : [];
  const cli = cliOf(session.mode);
  return html`<div class="cm-conv">
    <div class="thread" ref=${scroller} onScroll=${e => { const el = e.currentTarget; stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80; }}>
      <div class="thread-in">
        ${blocks === null && !failed && html`<div class="skeleton" style="height:64px"></div><div class="skeleton" style="height:120px;width:80%"></div>`}
        ${failed && html`<div class="msg-err"><${Icon} n="alert" /><span>${t('agents.conv.unavailable')} — ${failed}</span></div>`}
        ${blocks && !hasPrompt && !terminalVisible && session.mode !== 'shell' && html`<div class=${cls('cm-boot anim-rise', submitted && 'live')}>
          <div class="cm-boot-h"><b>${state === 'starting' ? t('agents.conv.empty_starting', { cli: cli.label }) : submitted ? t('agents.conv.live_title', { cli: cli.label }) : t('agents.conv.boot_title', { cli: cli.label })}</b>
            <span class="muted">${submitted ? t('agents.conv.live_text') : t('agents.conv.boot_text')}</span></div>
          <div class="cm-boot-term"><${AgentTerminal} sessionId=${id} /></div></div>`}
        ${blocks && !grouped.length && (terminalVisible || session.mode === 'shell') && html`<div class="cm-conv-empty">
          <p>${t('agents.conv.empty', { cli: cli.label })}</p></div>`}
        ${hasPrompt && capsOf(session.mode).transcript === 'screen' && html`<p class="cm-screen-note muted"><${Icon} n="info" />${t('agents.conv.screen_note')}</p>`}
        ${shown.map((b, i) => b.kind === 'activity' ? html`<${Activity} key=${'a' + i} items=${b.items} />`
          : b.role === 'user' ? html`<div key=${i} class="msg-user"><div class="bubble">${b.text}</div></div>`
          : html`<div key=${i} class="msg-ai"><div class="md" dangerouslySetInnerHTML=${{ __html: md(b.text) }}></div></div>`)}
        ${(state === 'working') && html`<div class="cm-working"><span class="spinner"></span><span>${t('agents.conv.working', { cli: cli.label })}</span>
          ${tools.map(x => html`<span class="tag">${x.name || x.toolName || x}</span>`)}</div>`}
        ${approvals.map(a => html`<${Approval} key=${a.id} item=${a} session=${session} onTerminal=${onTerminal} />`)}
      </div>
    </div>
    <${Composer} session=${session} onTerminal=${onTerminal} />
  </div>`;
}
