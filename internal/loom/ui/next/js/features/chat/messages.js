import { t, getLang } from '../../core/i18n.js';
// Fil de discussion : rendu des éléments produits par le moteur.
import { html, useState, useEffect, useRef, useMemo, cls, fmtTok, fmtSecs } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { md, plain } from './md.js';
import { ToolCard, QuietGroup, Plan, Approval, QUIET } from './tools.js';
import { post } from '../../core/api.js';
import { toast } from '../../ui/dialog.js';
import { runtimeKind } from '../../core/state.js';

const TOOL = () => ({
  bash: ['terminal', 'Terminal'], write: ['file', t("chat.messages.ecriture")], edit: ['edit', t("chat.messages.edition")],
  web_search: ['search', t("chat.messages.recherche_web")], web_open: ['globe', t("chat.messages.page_web")], web_read: ['globe', t("chat.messages.lecture_de_page")], web_grep: ['globe', t("chat.messages.recherche_dans_la_page")],
  mem_search: ['brain', t("chat.messages.recherche_memoire")], mem_read: ['brain', t("chat.messages.lecture_memoire")], mem_add: ['brain', t("chat.messages.nouvelle_note")], mem_edit: ['brain', t("chat.messages.note_modifiee")], mem_delete: ['brain', t("chat.messages.note_supprimee")],
  see_image: ['eye', 'Vision'],
});
function toolMeta(name) {
  if (TOOL()[name]) return TOOL()[name];
  if (name && name.startsWith('mcp__')) { const [srv, ...rest] = name.slice(5).split('__'); return ['plug', rest.join('__') + ' · ' + srv]; }
  return ['tool', name || t("chat.messages.outil")];
}

// Markdown mémorisé sur la longueur du texte : un bloc qui ne bouge plus n'est
// pas re-parsé à chaque frame du bloc voisin.
function Body({ text, isPlain, live }) {
  const out = useMemo(() => (isPlain ? plain(text) : md(text)), [text, isPlain, getLang()]);
  return html`<div class=${cls('md', live && 'streaming')} dangerouslySetInnerHTML=${{ __html: out }}></div>`;
}

function Collapsible({ icon, label, open: startOpen, live, children, cls: c }) {
  const [open, setOpen] = useState(!!startOpen);
  useEffect(() => { if (live) setOpen(true); }, [live]);
  const wasLive = useRef(live);
  useEffect(() => { if (wasLive.current && !live) setOpen(false); wasLive.current = live; }, [live]);
  return html`<div class=${cls('fold', c, open && 'open', live && 'live')}>
    <button type="button" class="fold-h" aria-expanded=${String(open)} onClick=${() => setOpen(!open)}>
      <${Icon} n=${icon} /><span>${label}</span><${Icon} n="chevron" class="caret" /></button>
    <div class="fold-b"><div class="fold-in">${open && children}</div></div></div>`;
}

function Tool({ tu, live }) {
  const [ico, name] = toolMeta(tu.name);
  let add = 0, del = 0;
  if (tu.diff && tu.diff.length) tu.diff.forEach(l => { if (l.op === '+') add++; else if (l.op === '-') del++; });
  else if (tu.body) add = tu.body.split('\n').length;
  const label = html`${name}${add || del ? html` <em class="plus">+${add}</em>${del ? html` <em class="minus">−${del}</em>` : ''}` : ''}${tu.result ? html` <em>· ${Math.max(1, Math.round(tu.result.length / 4))} ${t("chat.messages.tok")}</em>` : ''}`;
  return html`<${Collapsible} icon=${ico} label=${label} live=${live} cls="tool">
    ${tu.label && html`<pre class="tool-cmd"><code>${tu.label}${tu.typing ? html`<span class="caret-blink">▋</span>` : ''}</code></pre>`}
    ${tu.diff && tu.diff.length ? html`<pre class="diff">${tu.diff.map(l => html`<span class=${'dl ' + (l.op === '+' ? 'add' : l.op === '-' ? 'del' : '')}><b>${l.op === ' ' ? ' ' : l.op}</b>${l.t != null ? l.t : l.text}</span>`)}</pre>` :
      tu.body ? html`<pre class="diff">${tu.body.split('\n').map(localT => html`<span class="dl add"><b>+</b>${localT}</span>`)}</pre>` : ''}
    ${tu.result && html`<pre class="tool-out">${tu.result.length > 4000 ? tu.result.slice(0, 4000) + '\n…' : tu.result}</pre>`}
  </${Collapsible}>`;
}

function provenance(rt) {
  if (!rt || !rt.runtime_id) return '';
  const model = rt.model ? String(rt.model).split(/[\\/]/).pop() : '';
  return (rt.runtime_id === 'llama.cpp' ? 'local' : rt.provider_name || rt.runtime_id) + (model ? ' · ' + model : '');
}
function Foot({ it }) {
  const rt = it.rt || {};
  const parts = [];
  const prov = provenance(rt); if (prov) parts.push(prov);
  if (it.running) parts.push(t("chat.messages.en_cours"));
  if (it.ms > 0) parts.push(fmtSecs(it.ms / 1000));
  else if (rt.duration_seconds > 0) parts.push(fmtSecs(rt.duration_seconds));
  const u = rt.usage;
  if (it.tok > 0) parts.push(fmtTok(it.tok) + ' tok');
  else if (u && u.completion_tokens) parts.push(fmtTok(u.completion_tokens) + ' tok');
  if (it.rate) parts.push(it.rate.toFixed(1) + ' tok/s');
  if (rt.runtime_id === 'llama.cpp' || (!rt.runtime_id && it.rate)) parts.push(t("chat.messages.sans_cout_api"));
  if (!parts.length) return null;
  const events = rt.events || [];
  return html`<div class="turn-foot anim-fade">
    <${Icon} n=${rt.runtime_id && rt.runtime_id !== 'llama.cpp' ? (runtimeKind(rt.runtime_id) === 'harness' ? 'terminal' : 'cloud') : 'chip'} />
    <span>${parts.join(' · ')}</span>
    ${events.length ? html`<span class="muted"> · ${events.length} ${t("chat.messages.outil_2")}${events.length > 1 ? 's' : ''} ${t("chat.messages.natif")}${events.length > 1 ? 's' : ''}</span>` : ''}
  </div>`;
}

function Gen({ gen, compacting }) {
  const [, tick] = useState(0);
  useEffect(() => { const localT = setInterval(() => tick(x => x + 1), 500); return () => clearInterval(localT); }, []);
  const secs = (Date.now() - gen.start) / 1000;
  return html`<div class="gen"><span class="gen-mark"><${Icon} n="loom" /></span>
    <span>${compacting ? t("chat.messages.compactage_du_contexte") : [fmtSecs(secs), gen.tok ? fmtTok(gen.tok) + ' tok' : ''].filter(Boolean).join(' · ')}</span></div>`;
}

// Regroupe les lectures/recherches consécutives d'un harness et ne garde que le
// dernier plan (il est republié en entier à chaque mise à jour).
function group(items, base) {
  const out = [];
  const lastPlan = items.map(i => i.k).lastIndexOf('plan');
  items.forEach((it, i) => {
    if (it.k === 'plan' && i !== lastPlan) return;
    const quiet = it.k === 'tool' && it.tool && QUIET.has(it.tool.kind) && !(it.tool.diffs || []).length;
    const prev = out[out.length - 1];
    if (quiet && prev && prev.k === 'quiet') prev.tools.push(it.tool);
    else if (quiet) out.push({ k: 'quiet', tools: [it.tool], key: base + 'q' + i });
    else out.push({ ...it, key: it.key ?? base + i });
  });
  return out.map(it => (it.k === 'quiet' && it.tools.length === 1 ? { k: 'tool', tool: it.tools[0], key: it.key } : it));
}

// Une fois un tour terminé, tout le travail (réflexion, outils, plan, messages
// intermédiaires, autorisations réglées) se replie au-dessus de la réponse finale.
const WORK = new Set(['reasoning', 'tool', 'plan', 'approval', 'assistant']);
function arrange(items, live) {
  const turns = [];
  items.forEach((it, i) => { if (it.k === 'user' || !turns.length) turns.push([]); turns[turns.length - 1].push({ ...it, key: i }); });
  const out = [];
  turns.forEach((turn, n) => {
    const last = n === turns.length - 1;
    // Un tour antérieur ne peut plus rien attendre : demandes et actions en suspens sont closes.
    if (!last) turn = turn.map(it => it.k === 'approval' && !it.resolved ? { ...it, resolved: { cancelled: true } }
      : it.k === 'tool' && it.tool && !['completed', 'failed'].includes(it.tool.status) ? { ...it, tool: { ...it.tool, status: 'interrupted' } }
      : it.k === 'plan' ? { ...it, entries: (it.entries || []).map(e => e.status === 'in_progress' ? { ...e, status: 'pending' } : e) }
      : it.k === 'foot' && it.running ? { ...it, running: false } : it);
    const open = (last && live) || turn.some(it => it.live || (it.k === 'approval' && !it.resolved) || (it.k === 'foot' && it.running));
    const final = turn.map(it => it.k).lastIndexOf('assistant');
    const work = open || final < 0 ? [] : turn.filter((it, i) => i < final && WORK.has(it.k));
    if (work.length < 2 && !(work.length === 1 && work[0].k !== 'assistant')) { out.push(...group(turn, 't' + n)); return; }
    const foot = turn.find(it => it.k === 'foot');
    const users = turn.filter(it => it.k === 'user');
    const rest = turn.filter(it => it.k !== 'user' && !work.includes(it));
    out.push(...users, { k: 'work', items: group(work, 'w' + n), raw: work, foot, key: 'w' + n }, ...group(rest, 't' + n));
  });
  return out;
}

function workLabel(raw, foot) {
  const tools = raw.filter(i => i.k === 'tool').length;
  const think = raw.some(i => i.k === 'reasoning');
  const parts = [];
  if (think) parts.push(t("chat.messages.reflexion"));
  if (tools) parts.push(tools + ' action' + (tools > 1 ? 's' : ''));
  if (!parts.length) parts.push(t("chat.messages.etapes_intermediaires"));
  const rt = foot && foot.rt || {};
  const secs = foot && foot.ms > 0 ? foot.ms / 1000 : rt.duration_seconds;
  return parts.join(' · ') + (secs > 0 ? ' · ' + fmtSecs(secs) : '');
}

function Work({ it, render }) {
  const [open, setOpen] = useState(false);
  return html`<div class=${cls('work', open && 'open')}>
    <button type="button" class="work-h" aria-expanded=${String(open)} onClick=${() => setOpen(!open)}>
      <${Icon} n=${it.raw.some(i => i.k === 'tool') ? 'tool' : 'brain'} /><span>${workLabel(it.raw, it.foot)}</span><${Icon} n="chevron" class="caret" /></button>
    ${open && html`<div class="work-b">${it.items.map(render)}</div>`}
  </div>`;
}

async function answer(sessionId, approvalId, optionId) {
  const r = await post('/api/runtime/sessions/approval', { id: sessionId, approval_id: approvalId, option_id: optionId });
  if (!r.ok) toast(r.error || t("chat.messages.reponse_impossible"), 'err');
}

export function Messages({ items, gen, compacting, root, sessionId }) {
  const render = it => {
    const i = it.key;
    switch (it.k) {
      case 'work': return html`<${Work} key=${i} it=${it} render=${render} />`;
      case 'quiet': return html`<${QuietGroup} key=${i} tools=${it.tools} root=${root} />`;
      case 'plan': return html`<${Plan} key=${i} entries=${it.entries || []} />`;
      case 'approval': return html`<${Approval} key=${i} a=${it.approval} resolved=${it.resolved} root=${root} onAnswer=${opt => answer(sessionId, it.approval.id, opt)} />`;
      case 'tool': if (it.tool) return html`<${ToolCard} key=${i} tool=${it.tool} root=${root} />`; return html`<${Tool} key=${i} tu=${it.tu} live=${it.live} />`;
      case 'user': return html`<div key=${i} class=${cls('msg-user', it.pending && 'pending')}>
        ${it.files && it.files.length ? html`<div class="msg-files">${it.files.map(f => html`<span class="file-pill"><${Icon} n="file" />${f.name || String(f).split('/').pop()}</span>`)}</div>` : ''}
        <div class="bubble">${it.text}</div></div>`;
      case 'reasoning': return html`<${Collapsible} key=${i} icon="brain" live=${it.live} cls="reason"
        label=${it.live ? t("chat.messages.reflexion_en_cours") : it.summary ? t("chat.messages.resume_de_reflexion") : it.tok > 0 ? t("chat.messages.reflexion_2") + fmtTok(it.tok) + ' tok' : t("chat.messages.reflexion")}>
        <${Body} text=${it.text} isPlain=${it.summary} live=${it.live} /></${Collapsible}>`;
      case 'assistant': return html`<div key=${i} class="msg-ai"><${Body} text=${it.text} isPlain=${it.plain} live=${it.live} /></div>`;
      case 'foot': return html`<${Foot} key=${i} it=${it} />`;
      case 'error': return html`<div key=${i} class="msg-err"><${Icon} n="alert" /><span>${it.text}</span></div>`;
      case 'compact': return html`<div key=${i} class="compact-mark"><span>${t("chat.messages.contexte_compacte_anciens_tours_resumes")}</span></div>`;
      default: return null;
    }
  };
  return html`${arrange(items, !!gen).map(render)}${gen && html`<${Gen} gen=${gen} compacting=${compacting} />`}`;
}
