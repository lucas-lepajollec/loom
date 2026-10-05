import { t } from '../../core/i18n.js';
import { localChoice } from './execution.js';
// Un flux de discussion et un seul traitement des événements.
import { createStore } from '../../core/lib.js';
import { get, post, stream } from '../../core/api.js';
import { toast, confirm } from '../../ui/dialog.js';
import { refreshNav, refreshWorkspace, go, app } from '../../core/state.js';

export const chat = createStore({
  harness: { mode: '', modes: [], config: [], files: [], usage: null, commands: [], permission: 'ask', workdir: '' },
  mode: 'native', sessionId: '', session: null, context: null,
  items: [], frozen: null, busy: false, replaying: true, loading: true,
  ctxUsed: 0, compactCount: 0, gen: null, runningTask: '', notice: '',
});

let lastSeq = 0, abort = null, epoch = 0, opening = false, turn;
const newTurn = () => ({ reason: null, content: null, tool: null, tools: new Map(), plan: null, foot: null, tok: null, stats: null });
turn = newTurn();
const sleep = ms => new Promise(r => setTimeout(r, ms));

function items() { return chat.get().items; }
function push(item) {
  const list = items().slice();
  const foot = turn.foot && list.indexOf(turn.foot);
  if (turn.foot && foot >= 0) list.splice(foot, 0, item); else list.push(item);
  chat.set({ items: list }); return item;
}
function touch() { chat.set({ items: items().slice() }); }
const emptyHarness = () => ({ mode: '', modes: [], config: [], files: [], usage: null, commands: [], permission: 'ask', workdir: '' });
function applySession(s, context) {
  chat.set({ harness: { mode: s.mode || '', modes: s.available_modes || [], config: s.available_config_options || [], files: s.changed_files || [], usage: s.harness_usage || null, commands: s.commands || [], permission: s.permission || 'ask', workdir: s.workdir || '' } });
  chat.set({ session: s, context: context || chat.get().context, notice: '', busy: s.status === 'running' });
}
function liveFoot(rt) {
  if (!rt) return;
  if (!turn.foot) turn.foot = push({ k: 'foot', rt, running: true });
  else { turn.foot.rt = rt; touch(); }
  if (rt.usage) turn.tok = rt.usage.completion_tokens;
  bumpGen();
}
function onEvent(d) {
  if (typeof d.seq === 'number' && d.seq > lastSeq) lastSeq = d.seq;
  if (d.reset !== undefined) {
    lastSeq = 0; turn = newTurn();
    chat.set({ harness: emptyHarness(), items: [], busy: false, gen: null, ctxUsed: 0, compactCount: 0, replaying: !!d.replay, loading: !!d.replay });
    refreshNav();
  }
  if (d.session) applySession(d.session, d.context);
  if (d.state) {
    const s = d.state;
    chat.set({ busy: s.generating, compactCount: s.compact_count || 0, ctxUsed: s.ctx_used,
      gen: s.generating ? { start: Date.now() - (+s.gen_elapsed_ms || 0), tok: turn.tok } : null });
  }
  if (d.caught_up) {
    chat.set({ replaying: false, loading: false, notice: '', frozen: null });
    if (d.session && d.session.status === 'running') {
      for (const it of [turn.content, turn.reason]) if (it && !it.summary) it.live = true;
      const rt = (d.session.turns || []).at(-1);
      chat.set({ gen: { start: rt && rt.started_at || Date.now(), tok: rt && rt.usage ? rt.usage.completion_tokens : null } });
      touch();
    }
    return;
  }
  if (!d.type && d.running) liveFoot(d.provenance);
  const replay = chat.get().replaying;
  if (d.compacting !== undefined) chat.set({ compacting: !!d.compacting });
  if (d.compacted) { chat.set({ compacting: false }); push({ k: 'compact' }); }
  if (d.compact_noop) { chat.set({ compacting: false }); if (!replay) toast(t("chat.engine.rien_a_compacter")); }
  if (d.ctx_used !== undefined) chat.set({ ctxUsed: d.ctx_used });
  if (d.compact_count !== undefined) chat.set({ compactCount: d.compact_count });
  switch (d.type) {
    case 'turn_start': {
      turn = newTurn();
      const list = items().slice();
      const pending = list.findIndex(i => i.k === 'user' && i.pending && i.text === d.text);
      if (pending >= 0) list[pending] = { ...list[pending], pending: false, files: d.files || list[pending].files };
      else list.push({ k: 'user', text: d.text, files: d.files || [], plain: !!d.portable_text });
      chat.set({ items: list, busy: true, gen: replay ? null : { start: d.provenance && d.provenance.started_at || Date.now(), tok: null } });
      if (d.provenance && !replay) {
        turn.content = push({ k: 'assistant', text: '', live: true });
        liveFoot(d.provenance);
      }
      if (!replay) refreshNav();
      return;
    }
    case 'turn_done': {
      for (const it of items()) if (it.live) it.live = false;
      const st = d.metrics && d.metrics.stats || turn.stats || {};
      if (turn.foot) { turn.foot.rt = d.provenance; turn.foot.running = false; touch(); }
      else push({ k: 'foot', rt: d.provenance || null, ms: d.elapsed_ms || 0, tok: turn.tok, rate: st.gen_per_second || null, stats: st });
      turn = newTurn(); chat.set({ busy: false, gen: null });
      refreshNav();
      return;
    }
    case 'error':
      for (const it of items()) it.live = false;
      push({ k: 'error', text: d.error }); chat.set({ gen: null }); return;
    case 'plan': {
      let item = turn.plan;
      if (!item) turn.plan = push({ k: 'plan', entries: d.entries || [] });
      else { item.entries = d.entries || []; touch(); }
      return;
    }
    case 'approval_request':
      push({ k: 'approval', approval: d.approval, resolved: false }); return;
    case 'approval_resolved': {
      const item = items().find(i => i.k === 'approval' && i.approval.id === d.id);
      if (item) { item.resolved = { option_id: d.option_id, auto: d.auto }; touch(); }
      return;
    }
    case 'mode':
      chat.set({ harness: { ...chat.get().harness, mode: d.current, ...(d.modes ? { modes: d.modes } : {}) } }); return;
    case 'config':
      chat.set({ harness: { ...chat.get().harness, config: d.options || [] } }); return;
    case 'files':
      chat.set({ harness: { ...chat.get().harness, files: d.files || [] } }); return;
    case 'commands':
      chat.set({ harness: { ...chat.get().harness, commands: d.commands || [] } }); return;
    case 'usage':
      if (d.context || d.cost) chat.set({ harness: { ...chat.get().harness, usage: { context: d.context, cost: d.cost } } });
      if (d.metrics) {
        turn.stats = d.metrics;
        if (d.metrics.prompt_tokens_total) chat.set({ ctxUsed: d.metrics.prompt_tokens_total + (d.metrics.gen_tokens || 0) });
      }
      if (d.provenance) liveFoot(d.provenance);
      return;
    case 'tool_start': case 'tool_delta': case 'tool_end': {
      if (d.native_tool) { liveFoot(d.provenance); return; }
      const tu = d.tool;
      if (!tu) return;
      turn.content = null; turn.reason = null;
      if (tu.id) {
        const done = tu.status === 'completed' || tu.status === 'failed';
        const existing = turn.tools.get(tu.id);
        if (existing) { existing.tool = tu; existing.live = !replay && !done; touch(); }
        else turn.tools.set(tu.id, push({ k: 'tool', tool: tu, live: !replay && !done }));
        return;
      }
      if (!turn.tool) turn.tool = push({ k: 'tool', tu, live: !replay && !tu.done });
      else { turn.tool.tu = tu; turn.tool.live = !replay && !tu.done; touch(); }
      if (tu.arg_toks) { const prev = turn.tool._arg || 0; if (tu.arg_toks > prev) { turn.tok = (turn.tok || 0) + tu.arg_toks - prev; turn.tool._arg = tu.arg_toks; } }
      if (tu.done) turn.tool = null;
      bumpGen(); return;
    }
    case 'reasoning_delta': {
      if (d.drop) { if (turn.reason) chat.set({ items: items().filter(i => i !== turn.reason) }); turn.reason = null; return; }
      if (!turn.reason) {
        turn.reason = push({ k: 'reasoning', text: '', tok: null, live: !replay && !d.summary, summary: !!d.summary });
        if (d.summary && turn.content) {
          const list = items().filter(i => i !== turn.reason);
          list.splice(list.indexOf(turn.content), 0, turn.reason); chat.set({ items: list });
        }
      }
      appendText(turn.reason, d); return;
    }
    case 'text_delta':
      if (turn.reason) turn.reason.live = false;
      if (!turn.content) turn.content = push({ k: 'assistant', text: '', tok: null, live: !replay });
      appendText(turn.content, d);
  }
}
function appendText(item, d) {
  if (d.replace) { turn.tok = (turn.tok || 0) - (item.tok || 0); item.text = ''; item.tok = 0; }
  item.text += d.text || '';
  // Only native journal deltas carry its historical token counter.
  if (typeof d.toks === 'number' || typeof d.seq === 'number') {
    const inc = d.toks || 1; item.tok = (item.tok || 0) + inc; turn.tok = (turn.tok || 0) + inc;
  }
  touch(); bumpGen();
}
function bumpGen() {
  const g = chat.get().gen;
  if (g) chat.set({ gen: { ...g, tok: turn.tok } });
}

let streaming = false;
export async function connectNative() {
  if (streaming) return;
  streaming = true;
  for (;;) {
    while (document.hidden || opening) await sleep(500);
    const ep = epoch, st = chat.get();
    abort = new AbortController();
    try {
      await stream('/api/discussion/events', { id: st.sessionId, from: lastSeq }, d => { if (ep === epoch) onEvent(d); }, abort.signal);
    } catch (_) {}
    if (ep === epoch && !document.hidden) chat.set({ notice: t("chat.engine.connexion_interrompue_nouvelle_tentative") });
    await sleep(600);
  }
}
document.addEventListener('visibilitychange', () => { if (abort) abort.abort(); });
globalThis.addEventListener?.('pageshow', () => { if (abort) abort.abort(); });
globalThis.addEventListener?.('online', () => { if (abort) abort.abort(); });

function restartNative(replay) {
  lastSeq = 0; turn = newTurn();
  chat.set({ harness: emptyHarness(), mode: 'native', items: [], busy: false, gen: null, replaying: replay, loading: replay });
  if (abort) abort.abort();
}

// ---------------------------------------------------------------- ouverture
// open(id) : une discussion commune (id de session) ou une archive native.
// soft : garder le fil affiché pendant la relecture (changement de modèle).
export async function open(id, soft, quiet) {
  const ep = ++epoch; opening = true; if (abort) abort.abort();
  chat.set({ frozen: soft && items().length ? items() : null });
  if (soft) setTimeout(() => { if (ep === epoch) chat.set({ frozen: null }); }, 8000);
  const nav = app.get().nav.conversations;
  const entry = nav.find(c => c.id === id);
  try {
    if (entry && !entry.workspace) {
      // Archive native : le serveur la recharge et le flux la rejoue.
      restartNative(true);
      remember('');
      chat.set({ sessionId: '', session: null, context: null });
      const r = await post('/api/chat/history/restore', { id });
      if (!r.ok) throw new Error(r.error || t("chat.engine.ouverture_impossible"));
      refreshNav();
      return true;
    }
    const r = await get('/api/runtime/sessions?id=' + encodeURIComponent(id));
    if (ep !== epoch) return false;
    if (!r.ok) throw new Error(r.error);
    remember(id);
    if (r.session.runtime_id === 'llama.cpp') {
      const local = await post('/api/runtime/sessions/local', { id });
      if (!local.ok) throw new Error(local.error);
      if (ep !== epoch) return false;
      restartNative(true);
      chat.set({ sessionId: id, session: local.session, context: local.context });
    } else {
      lastSeq = 0; turn = newTurn();
      chat.set({ mode: 'thread', sessionId: id, items: [], replaying: true, loading: true, busy: false, gen: null });
      applySession(r.session, r.context);
    }
    if (abort) abort.abort();
    return true;
  } catch (e) { chat.set({ frozen: null }); if (quiet) remember(''); else toast(e.message, 'err'); return false; }
  finally { if (ep === epoch) { opening = false; if (abort) abort.abort(); } }
}

// Nouvelle discussion : le chat natif archive la courante et repart à zéro.
export async function newDiscussion(projectId) {
  ++epoch; opening = true; if (abort) abort.abort();
  remember('');
  restartNative(false);
  chat.set({ sessionId: '', session: null, context: null, loading: false });
  let r;
  try { r = await post('/api/chat/reset', { project_id: projectId || '' }); }
  finally { opening = false; if (abort) abort.abort(); }
  if (!r.ok) toast(t("chat.engine.impossible_de_creer_la_discussion"), 'err');
  refreshNav();
  go('chat');
}

// ---------------------------------------------------------------- envoi
export async function send(text, opts = {}) {
  const st = chat.get();
  if (st.mode === 'thread') return sendThread(text);
  if (st.busy) return false;
  push({ k: 'user', text, files: opts.files || [], pending: true });
  for (let i = 0; i < 3; i++) {
    try {
      const r = await post('/api/chat/send', { message: text, files: opts.files || [], ctx_used: st.ctxUsed, internet: !!opts.internet, mcp: !!opts.mcp });
      if (r.ok || r.status === 409) return true;
      if (r.status < 500) { dropPending(text); toast(r.error || t("chat.engine.envoi_refuse"), 'err'); return false; }
    } catch (_) {}
    await sleep(600);
  }
  dropPending(text); toast(t("chat.engine.echec_de_l_envoi"), 'err'); return false;
}
function dropPending(text) { chat.set({ items: items().filter(i => !(i.k === 'user' && i.pending && i.text === text)) }); }

const pendingReq = new Map();
async function sendThread(text) {
  const s = chat.get().session;
  if (!s || chat.get().busy || s.status === 'running') return false;
  let p = pendingReq.get(s.id);
  if (!p || p.text !== text) { p = { text, request_id: crypto.randomUUID ? crypto.randomUUID() : String(Date.now()) + Math.random() }; pendingReq.set(s.id, p); }
  chat.set({ busy: true });
  let r;
  try {
    let revision = chat.get().context?.revision || '';
    const project = app.get().workspace?.projects?.find(project => project.id === s.project_id);
    if (project?.brain_budget > 0 && project.brain_sources?.length) {
      const preview = await post('/api/runtime/sessions/preview', { id: s.id, text });
      if (!preview.ok || preview.preview?.problem) throw new Error(preview.error || preview.preview?.problem);
      revision = preview.preview.context.revision;
      if (chat.get().session?.id !== s.id) return false;
      chat.set({ context: preview.preview.context });
    }
    r = await post('/api/runtime/sessions/send', { id: s.id, ...p, context_revision: revision });
  }
  catch (e) { chat.set({ busy: false, notice: e.message }); return false; }
  if (!r.ok) { chat.set({ busy: false, notice: r.error }); toast(r.error, 'err'); return false; }
  pendingReq.delete(s.id);
  return true;
}

export async function stop() {
  const st = chat.get();
  if (st.mode === 'thread' && st.session) { await post('/api/runtime/sessions/stop', { id: st.session.id }); return; }
  await post('/api/chat/stop', {});
}

export async function compact() {
  if (!await confirm(t("chat.engine.compacter_le_contexte"), t("chat.engine.les_anciens_tours_sont_resumes_pour_liberer_de_la_place_la_discus"), { ok: t("chat.engine.compacter") })) return;
  const r = await post('/api/chat/compact', {});
  if (!r.ok) toast(r.error || t("chat.engine.compactage_impossible"), 'err');
}

// ---------------------------------------------------------------- exécution
// Choix cloud/harness : la discussion devient (ou reste) commune, avec accord
// explicite avant tout envoi vers l'extérieur.
export async function chooseRemote(choice) {
  const st = chat.get();
  if (st.busy) { toast(t("chat.engine.attends_la_fin_de_la_reponse_avant_de_changer")); return false; }
  const dest = choice.kind === 'harness' ? choice.provider_name : choice.endpoint || choice.provider_name;
  const ok = await confirm(t("chat.engine.continuer_avec") + choice.name,
    t("chat.engine.le_texte_de_la_discussion_et_le_contexte_du_projet_instructions_e") + dest + t("chat.engine.les_autres_fichiers_les_outils_et_la_memoire_privee_ne_sont_pas_t"), { ok: t("chat.engine.continuer") });
  if (!ok) return false;
  try {
    let id = st.sessionId;
    if (!id) {
      const hist = await get('/api/chat/history');
      const sessions = await get('/api/runtime/sessions');
      const bound = (sessions.sessions || []).find(s => s.native_archive === hist.active);
      if (bound) id = bound.id;
      else {
        const archived = (hist.conversations || []).some(c => c.id === hist.active);
        const created = archived ? await post('/api/runtime/sessions/import', { id: hist.active }) : await post('/api/runtime/sessions/create', { project_id: hist.project_id || '' });
        if (!created.ok) throw new Error(created.error);
        id = created.session.id;
      }
    }
    const r = await post('/api/runtime/sessions/select', { id, choice_id: choice.id, consent: true });
    if (!r.ok) throw new Error(r.error);
    await refreshNav();
    return await open(id, true);
  } catch (e) { toast(e.message, 'err'); return false; }
}

// Choix local : dans une discussion commune on bascule d'abord sa route vers ce
// modèle, puis on charge le modèle/preset dans le moteur.
export async function chooseLocal(target) {
  const st = chat.get();
  if (st.busy) { toast(t("chat.engine.attends_la_fin_de_la_reponse_avant_de_changer")); return false; }
  try {
    if (st.sessionId) {
      const w = await refreshWorkspace();
      const choice = localChoice(w && w.ok && w.models, target);
      if (!choice) { toast(t('chat.engine.local_choice_missing'), 'err'); return false; }
      if (chat.get().sessionId !== st.sessionId || chat.get().busy) return false;
      const r = await post('/api/runtime/sessions/select', { id: st.sessionId, choice_id: choice.id, consent: false });
      if (!r.ok) { toast(r.error, 'err'); return false; }
      if (!await open(st.sessionId, true)) return false;
    }
    const r = target.presetIndex ? await post('/api/switch', { n: target.presetIndex }) : await post('/api/load-model', { model: target.model });
    if (!r.ok) { toast(r.error || t("chat.engine.chargement_impossible"), 'err'); return false; }
    toast(t("chat.engine.chargement_de") + target.name + '…');
    refreshNav();
    return true;
  } catch (e) { toast(e.message, 'err'); return false; }
}

// La dernière discussion commune ouverte est rouverte au rechargement.
const LAST = 'loom.next.lastChat';
const remember = id => { try { id ? localStorage.setItem(LAST, id) : localStorage.removeItem(LAST); } catch (_) {} };
export function init() {
  connectNative();
  let last = '';
  try { last = localStorage.getItem(LAST) || ''; } catch (_) {}
  if (last) get('/api/runtime/sessions?id=' + encodeURIComponent(last)).then(r => { if (r && r.ok) open(last, false, true); else remember(''); }).catch(() => remember(''));
}
