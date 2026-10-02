import { t } from '../../core/i18n.js';
// Rendu des actions d'un harness (contrat docs/agents/harness-acp-brief.md §5) :
// lectures et recherches condensées, commandes avec sortie, modifications avec
// diff, plan, demandes d'autorisation. Le texte des outils est affiché brut.
import { html, useState, useMemo, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';

const KIND = () => ({
  read: ['file', t("chat.tools.lecture")], search: ['search', t("chat.tools.recherche")], fetch: ['globe', 'Web'], execute: ['terminal', t("harnesses.page.commande")],
  edit: ['edit', t("chat.tools.modification")], delete: ['trash', t("chat.tools.suppression")], move: ['file', t("chat.tools.deplacement")], think: ['brain', t("chat.tools.reflexion")], other: ['tool', t("chat.tools.outil")],
});
export const QUIET = new Set(['read', 'search', 'fetch', 'think']);
export const rel = (p, root) => { const s = String(p || ''); return root && s.startsWith(root + '/') ? s.slice(root.length + 1) : s.replace(/^\/home\/[^/]+/, '~'); };

// Diff ligne à ligne (LCS) borné : au-delà, on affiche l'ancien et le nouveau en bloc.
export function lineDiff(a, b) {
  const x = String(a || '').split('\n'), y = String(b || '').split('\n');
  if (!a) return y.map(localT => ({ op: '+', t: localT }));
  if (x.length * y.length > 400000) return [...x.map(localT => ({ op: '-', t: localT })), ...y.map(localT => ({ op: '+', t: localT }))];
  const n = x.length, m = y.length, L = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) L[i][j] = x[i] === y[j] ? L[i + 1][j + 1] + 1 : Math.max(L[i + 1][j], L[i][j + 1]);
  const out = []; let i = 0, j = 0;
  while (i < n && j < m) { if (x[i] === y[j]) { out.push({ op: ' ', t: x[i] }); i++; j++; } else if (L[i + 1][j] >= L[i][j + 1]) out.push({ op: '-', t: x[i++] }); else out.push({ op: '+', t: y[j++] }); }
  while (i < n) out.push({ op: '-', t: x[i++] }); while (j < m) out.push({ op: '+', t: y[j++] });
  return out;
}
// Garde 3 lignes de contexte autour des changements.
function hunks(lines) {
  const keep = new Set();
  lines.forEach((l, k) => { if (l.op !== ' ') for (let d = -3; d <= 3; d++) keep.add(k + d); });
  const out = []; let gap = false;
  lines.forEach((l, k) => { if (keep.has(k)) { out.push(l); gap = false; } else if (!gap) { out.push({ op: '…', t: '' }); gap = true; } });
  return out;
}
export const diffStat = lines => lines.reduce((s, l) => (l.op === '+' ? s.add++ : l.op === '-' ? s.del++ : 0, s), { add: 0, del: 0 });

export function Diff({ d }) {
  const lines = useMemo(() => hunks(lineDiff(d.old, d.new)), [d.old, d.new]);
  return html`<pre class="diff">${lines.map(l => l.op === '…' ? html`<span class="dl gap">⋯</span>`
    : html`<span class=${'dl ' + (l.op === '+' ? 'add' : l.op === '-' ? 'del' : '')}><b>${l.op === ' ' ? '' : l.op}</b>${l.t}</span>`)}</pre>`;
}

const Status = ({ s }) => s === 'failed' ? html`<span class="tc-st err"><${Icon} n="close" /></span>`
  : s === 'interrupted' ? html`<span class="tc-st muted">${t("chat.tools.interrompu")}</span>` : s === 'completed' ? null : html`<span class="spinner tc-spin"></span>`;

// Une action : ligne compacte (lecture, recherche) ou carte dépliable.
export function ToolCard({ tool: localT, root }) {
  const [open, setOpen] = useState(false);
  const [ico, label] = KIND()[localT.kind] || KIND().other;
  const diffs = localT.diffs || [];
  const stats = diffs.map(d => diffStat(lineDiff(d.old, d.new)));
  const add = stats.reduce((a, s) => a + s.add, 0), del = stats.reduce((a, s) => a + s.del, 0);
  const where = (localT.locations || [])[0];
  const title = localT.title || label;
  const body = !!(localT.kind === 'execute' || localT.output || diffs.length);
  if (QUIET.has(localT.kind) && !diffs.length) {
    return html`<div class=${cls('tc-line', localT.status === 'failed' && 'failed')}><${Icon} n=${ico} />
      <span class="t">${title}</span>${where && !title.includes(rel(where.path, root)) && html`<span class="p">${rel(where.path, root)}${where.line ? ':' + where.line : ''}</span>`}<${Status} s=${localT.status} /></div>`;
  }
  return html`<div class=${cls('tc', open && 'open', localT.status === 'failed' && 'failed')}>
    <button type="button" class="tc-h" aria-expanded=${String(open)} onClick=${() => body && setOpen(!open)}>
      <${Icon} n=${ico} /><span class="t">${localT.kind === 'execute' ? html`<code>${title}</code>` : title}</span>
      ${(add > 0 || del > 0) && html`<span class="tc-n"><em class="plus">+${add}</em> <em class="minus">−${del}</em></span>`}
      <${Status} s=${localT.status} />${body && html`<${Icon} n="chevron" class="caret" />`}</button>
    ${open && html`<div class="tc-b">
      ${diffs.map(d => html`<div class="tc-file"><div class="tc-path">${rel(d.path, root)}</div><${Diff} d=${d} /></div>`)}
      ${localT.output && html`<pre class="tool-out">${localT.output}</pre>`}
      ${!localT.output && localT.kind === 'execute' && html`<pre class="tool-out muted">${localT.status === 'completed' ? t("chat.tools.aucune_sortie") : t("chat.tools.en_cours")}</pre>`}
    </div>`}
  </div>`;
}

// Plusieurs lectures/recherches d'affilée : « 4 actions · 3 lectures, 1 recherche ».
export function QuietGroup({ tools, root }) {
  const [open, setOpen] = useState(false);
  const live = tools.some(localT => !['completed', 'failed', 'interrupted'].includes(localT.status));
  const count = {}; tools.forEach(localT => { count[localT.kind] = (count[localT.kind] || 0) + 1; });
  const words = { read: [t("chat.tools.lecture_word"), t("chat.tools.lectures")], search: [t("chat.tools.recherche_word"), t("chat.tools.recherches")], fetch: [t("chat.tools.page_web"), t("chat.tools.pages_web")], think: [t("chat.tools.reflexion_2"), t("chat.tools.reflexions")] };
  const sum = Object.entries(count).map(([k, n]) => n + ' ' + (words[k] || ['action', 'actions'])[n > 1 ? 1 : 0]).join(', ');
  return html`<div class=${cls('tc-group', open && 'open')}>
    <button type="button" class="tc-gh" aria-expanded=${String(open)} onClick=${() => setOpen(!open)}>
      ${live ? html`<span class="spinner tc-spin"></span>` : html`<${Icon} n="search" />`}<span>${sum}</span><${Icon} n="chevron" class="caret" /></button>
    ${open && html`<div class="tc-gb">${tools.map(localT => html`<${ToolCard} key=${localT.id} tool=${localT} root=${root} />`)}</div>`}
  </div>`;
}

export function Plan({ entries }) {
  const done = entries.filter(e => e.status === 'completed').length;
  return html`<div class="plan">
    <div class="plan-h"><${Icon} n="check" /><span>${t("chat.tools.plan")}</span><span class="num muted">${done}/${entries.length}</span></div>
    <ol>${entries.map(e => html`<li class=${e.status}><i></i><span>${e.content}</span></li>`)}</ol>
  </div>`;
}

const OPT_LABEL = { get allow_once() { return t("chat.tools.autoriser"); }, get allow_always() { return t("chat.tools.toujours_autoriser"); }, get reject_once() { return t("chat.tools.refuser"); }, get reject_always() { return t("chat.tools.toujours_refuser"); } };
const OPT_ORDER = { allow_once: 0, allow_always: 1, reject_once: 2, reject_always: 3 };
export function Approval({ a, resolved, root, onAnswer }) {
  const localT = a.tool || {};
  const [ico] = KIND()[localT.kind] || KIND().other;
  const opts = [...(a.options || [])].sort((x, y) => (OPT_ORDER[x.kind] ?? 9) - (OPT_ORDER[y.kind] ?? 9));
  const diffs = localT.diffs || [];
  return html`<div class=${cls('approval', resolved && 'done')}>
    <div class="ap-h"><${Icon} n="lock" /><span>${resolved ? (resolved.cancelled ? t("chat.tools.demande_annulee") : resolved.allowed ? t("chat.tools.autorise") + (resolved.auto ? t("chat.tools.automatiquement") : '') : t("chat.tools.refuse")) : t("chat.tools.autorisation_demandee")}</span></div>
    <div class="ap-tool"><${Icon} n=${ico} />${localT.kind === 'execute' ? html`<code>${localT.title}</code>` : html`<span>${localT.title}</span>`}</div>
    ${!resolved && diffs.map(d => html`<div class="tc-file"><div class="tc-path">${rel(d.path, root)}</div><${Diff} d=${d} /></div>`)}
    ${!resolved && html`<div class="ap-acts">${opts.map(o => html`<button class=${cls('btn sm', o.kind === 'allow_once' && 'primary', o.kind && o.kind.startsWith('reject') && 'ghost')} onClick=${() => onAnswer(o.id)}>${OPT_LABEL[o.kind] || o.name}</button>`)}</div>`}
  </div>`;
}
