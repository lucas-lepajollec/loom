// Fil de discussion : rendu des éléments produits par le moteur.
import { html, useState, useEffect, useRef, useMemo, cls, fmtTok, fmtSecs } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { md, plain } from './md.js';
import { ToolCard, QuietGroup, Plan, Approval, QUIET } from './tools.js';
import { post } from '../../core/api.js';
import { toast } from '../../ui/dialog.js';

const TOOL = {
  bash: ['terminal', 'Terminal'], write: ['file', 'Écriture'], edit: ['edit', 'Édition'],
  web_search: ['search', 'Recherche web'], web_open: ['globe', 'Page web'], web_read: ['globe', 'Lecture de page'], web_grep: ['globe', 'Recherche dans la page'],
  mem_search: ['brain', 'Recherche mémoire'], mem_read: ['brain', 'Lecture mémoire'], mem_add: ['brain', 'Nouvelle note'], mem_edit: ['brain', 'Note modifiée'], mem_delete: ['brain', 'Note supprimée'],
  see_image: ['eye', 'Vision'],
};
function toolMeta(name) {
  if (TOOL[name]) return TOOL[name];
  if (name && name.startsWith('mcp__')) { const [srv, ...rest] = name.slice(5).split('__'); return ['plug', rest.join('__') + ' · ' + srv]; }
  return ['tool', name || 'Outil'];
}

// Markdown mémorisé sur la longueur du texte : un bloc qui ne bouge plus n'est
// pas re-parsé à chaque frame du bloc voisin.
function Body({ text, isPlain, live }) {
  const out = useMemo(() => (isPlain ? plain(text) : md(text)), [text, isPlain]);
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
  const label = html`${name}${add || del ? html` <em class="plus">+${add}</em>${del ? html` <em class="minus">−${del}</em>` : ''}` : ''}${tu.result ? html` <em>· ${Math.max(1, Math.round(tu.result.length / 4))} tok</em>` : ''}`;
  return html`<${Collapsible} icon=${ico} label=${label} live=${live} cls="tool">
    ${tu.label && html`<pre class="tool-cmd"><code>${tu.label}${tu.typing ? html`<span class="caret-blink">▋</span>` : ''}</code></pre>`}
    ${tu.diff && tu.diff.length ? html`<pre class="diff">${tu.diff.map(l => html`<span class=${'dl ' + (l.op === '+' ? 'add' : l.op === '-' ? 'del' : '')}><b>${l.op === ' ' ? ' ' : l.op}</b>${l.t != null ? l.t : l.text}</span>`)}</pre>` :
      tu.body ? html`<pre class="diff">${tu.body.split('\n').map(t => html`<span class="dl add"><b>+</b>${t}</span>`)}</pre>` : ''}
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
  if (it.running) parts.push('en cours');
  if (it.ms > 0) parts.push(fmtSecs(it.ms / 1000));
  else if (rt.duration_seconds > 0) parts.push(fmtSecs(rt.duration_seconds));
  const u = rt.usage;
  if (it.tok > 0) parts.push(fmtTok(it.tok) + ' tok');
  else if (u && u.completion_tokens) parts.push(fmtTok(u.completion_tokens) + ' tok');
  if (it.rate) parts.push(it.rate.toFixed(1) + ' tok/s');
  if (rt.runtime_id === 'llama.cpp' || (!rt.runtime_id && it.rate)) parts.push('sans coût API');
  if (!parts.length) return null;
  const events = rt.events || [];
  return html`<div class="turn-foot anim-fade">
    <${Icon} n=${rt.runtime_id && rt.runtime_id !== 'llama.cpp' ? (['antigravity', 'codex'].includes(rt.runtime_id) ? 'terminal' : 'cloud') : 'chip'} />
    <span>${parts.join(' · ')}</span>
    ${events.length ? html`<span class="muted"> · ${events.length} outil${events.length > 1 ? 's' : ''} natif${events.length > 1 ? 's' : ''}</span>` : ''}
  </div>`;
}

function Gen({ gen, compacting }) {
  const [, tick] = useState(0);
  useEffect(() => { const t = setInterval(() => tick(x => x + 1), 500); return () => clearInterval(t); }, []);
  const secs = (Date.now() - gen.start) / 1000;
  return html`<div class="gen"><span class="gen-mark"><${Icon} n="loom" /></span>
    <span>${compacting ? 'Compactage du contexte…' : [fmtSecs(secs), gen.tok ? fmtTok(gen.tok) + ' tok' : ''].filter(Boolean).join(' · ')}</span></div>`;
}

// Regroupe les lectures/recherches consécutives d'un harness et ne garde que le
// dernier plan (il est republié en entier à chaque mise à jour).
function arrange(items) {
  const out = [];
  const lastPlan = items.map(i => i.k).lastIndexOf('plan');
  items.forEach((it, i) => {
    if (it.k === 'plan' && i !== lastPlan) return;
    const quiet = it.k === 'tool' && it.tool && QUIET.has(it.tool.kind) && !(it.tool.diffs || []).length;
    const prev = out[out.length - 1];
    if (quiet && prev && prev.k === 'quiet') prev.tools.push(it.tool);
    else if (quiet) out.push({ k: 'quiet', tools: [it.tool], key: 'q' + i });
    else out.push({ ...it, key: i });
  });
  return out.map(it => (it.k === 'quiet' && it.tools.length === 1 ? { k: 'tool', tool: it.tools[0], key: it.key } : it));
}

async function answer(sessionId, approvalId, optionId) {
  const r = await post('/api/runtime/sessions/approval', { id: sessionId, approval_id: approvalId, option_id: optionId });
  if (!r.ok) toast(r.error || 'Réponse impossible', 'err');
}

export function Messages({ items, gen, compacting, root, sessionId }) {
  return html`${arrange(items).map(it => {
    const i = it.key;
    switch (it.k) {
      case 'quiet': return html`<${QuietGroup} key=${i} tools=${it.tools} root=${root} />`;
      case 'plan': return html`<${Plan} key=${i} entries=${it.entries || []} />`;
      case 'approval': return html`<${Approval} key=${i} a=${it.approval} resolved=${it.resolved} root=${root} onAnswer=${opt => answer(sessionId, it.approval.id, opt)} />`;
      case 'tool': if (it.tool) return html`<${ToolCard} key=${i} tool=${it.tool} root=${root} />`; return html`<${Tool} key=${i} tu=${it.tu} live=${it.live} />`;
      case 'user': return html`<div key=${i} class=${cls('msg-user', it.pending && 'pending')}>
        ${it.files && it.files.length ? html`<div class="msg-files">${it.files.map(f => html`<span class="file-pill"><${Icon} n="file" />${f.name || String(f).split('/').pop()}</span>`)}</div>` : ''}
        <div class="bubble">${it.text}</div></div>`;
      case 'reasoning': return html`<${Collapsible} key=${i} icon="brain" live=${it.live} cls="reason"
        label=${it.live ? 'Réflexion en cours…' : it.summary ? 'Résumé de réflexion' : 'Réflexion · ' + fmtTok(it.tok) + ' tok'}>
        <${Body} text=${it.text} isPlain=${it.summary} live=${it.live} /></${Collapsible}>`;
      case 'assistant': return html`<div key=${i} class="msg-ai"><${Body} text=${it.text} isPlain=${it.plain} live=${it.live} /></div>`;
      case 'foot': return html`<${Foot} key=${i} it=${it} />`;
      case 'error': return html`<div key=${i} class="msg-err"><${Icon} n="alert" /><span>${it.text}</span></div>`;
      case 'compact': return html`<div key=${i} class="compact-mark"><span>Contexte compacté · anciens tours résumés</span></div>`;
      default: return null;
    }
  })}${gen && html`<${Gen} gen=${gen} compacting=${compacting} />`}`;
}
