// Rendu des actions d'un harness (contrat docs/agents/harness-acp-brief.md §5) :
// lectures et recherches condensées, commandes avec sortie, modifications avec
// diff, plan, demandes d'autorisation. Le texte des outils est affiché brut.
import { html, useState, useMemo, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';

const KIND = {
  read: ['file', 'Lecture'], search: ['search', 'Recherche'], fetch: ['globe', 'Web'], execute: ['terminal', 'Commande'],
  edit: ['edit', 'Modification'], delete: ['trash', 'Suppression'], move: ['file', 'Déplacement'], think: ['brain', 'Réflexion'], other: ['tool', 'Outil'],
};
export const QUIET = new Set(['read', 'search', 'fetch', 'think']);
export const rel = (p, root) => { const s = String(p || ''); return root && s.startsWith(root + '/') ? s.slice(root.length + 1) : s.replace(/^\/home\/[^/]+/, '~'); };

// Diff ligne à ligne (LCS) borné : au-delà, on affiche l'ancien et le nouveau en bloc.
export function lineDiff(a, b) {
  const x = String(a || '').split('\n'), y = String(b || '').split('\n');
  if (!a) return y.map(t => ({ op: '+', t }));
  if (x.length * y.length > 400000) return [...x.map(t => ({ op: '-', t })), ...y.map(t => ({ op: '+', t }))];
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
  : s === 'completed' ? null : html`<span class="spinner tc-spin"></span>`;

// Une action : ligne compacte (lecture, recherche) ou carte dépliable.
export function ToolCard({ tool: t, root }) {
  const [open, setOpen] = useState(false);
  const [ico, label] = KIND[t.kind] || KIND.other;
  const diffs = t.diffs || [];
  const stats = diffs.map(d => diffStat(lineDiff(d.old, d.new)));
  const add = stats.reduce((a, s) => a + s.add, 0), del = stats.reduce((a, s) => a + s.del, 0);
  const where = (t.locations || [])[0];
  const title = t.title || label;
  const body = t.kind === 'execute' || t.output || diffs.length;
  if (QUIET.has(t.kind) && !diffs.length) {
    return html`<div class=${cls('tc-line', t.status === 'failed' && 'failed')}><${Icon} n=${ico} />
      <span class="t">${title}</span>${where && !title.includes(rel(where.path, root)) && html`<span class="p">${rel(where.path, root)}${where.line ? ':' + where.line : ''}</span>`}<${Status} s=${t.status} /></div>`;
  }
  return html`<div class=${cls('tc', open && 'open', t.status === 'failed' && 'failed')}>
    <button type="button" class="tc-h" aria-expanded=${String(open)} onClick=${() => body && setOpen(!open)}>
      <${Icon} n=${ico} /><span class="t">${t.kind === 'execute' ? html`<code>${title}</code>` : title}</span>
      ${(add > 0 || del > 0) && html`<span class="tc-n"><em class="plus">+${add}</em> <em class="minus">−${del}</em></span>`}
      <${Status} s=${t.status} />${body && html`<${Icon} n="chevron" class="caret" />`}</button>
    ${open && html`<div class="tc-b">
      ${diffs.map(d => html`<div class="tc-file"><div class="tc-path">${rel(d.path, root)}</div><${Diff} d=${d} /></div>`)}
      ${t.output && html`<pre class="tool-out">${t.output}</pre>`}
      ${!t.output && t.kind === 'execute' && html`<pre class="tool-out muted">${t.status === 'completed' ? 'Aucune sortie.' : 'En cours…'}</pre>`}
    </div>`}
  </div>`;
}

// Plusieurs lectures/recherches d'affilée : « 4 actions · 3 lectures, 1 recherche ».
export function QuietGroup({ tools, root }) {
  const [open, setOpen] = useState(false);
  const live = tools.some(t => t.status !== 'completed' && t.status !== 'failed');
  const count = {}; tools.forEach(t => { count[t.kind] = (count[t.kind] || 0) + 1; });
  const words = { read: ['lecture', 'lectures'], search: ['recherche', 'recherches'], fetch: ['page web', 'pages web'], think: ['réflexion', 'réflexions'] };
  const sum = Object.entries(count).map(([k, n]) => n + ' ' + (words[k] || ['action', 'actions'])[n > 1 ? 1 : 0]).join(', ');
  return html`<div class=${cls('tc-group', open && 'open')}>
    <button type="button" class="tc-gh" aria-expanded=${String(open)} onClick=${() => setOpen(!open)}>
      ${live ? html`<span class="spinner tc-spin"></span>` : html`<${Icon} n="search" />`}<span>${sum}</span><${Icon} n="chevron" class="caret" /></button>
    ${open && html`<div class="tc-gb">${tools.map(t => html`<${ToolCard} key=${t.id} tool=${t} root=${root} />`)}</div>`}
  </div>`;
}

export function Plan({ entries }) {
  const done = entries.filter(e => e.status === 'completed').length;
  return html`<div class="plan">
    <div class="plan-h"><${Icon} n="check" /><span>Plan</span><span class="num muted">${done}/${entries.length}</span></div>
    <ol>${entries.map(e => html`<li class=${e.status}><i></i><span>${e.content}</span></li>`)}</ol>
  </div>`;
}

const OPT_ORDER = { allow_once: 0, allow_always: 1, reject_once: 2, reject_always: 3 };
export function Approval({ a, resolved, root, onAnswer }) {
  const t = a.tool || {};
  const [ico] = KIND[t.kind] || KIND.other;
  const opts = [...(a.options || [])].sort((x, y) => (OPT_ORDER[x.kind] ?? 9) - (OPT_ORDER[y.kind] ?? 9));
  const diffs = t.diffs || [];
  return html`<div class=${cls('approval', resolved && 'done')}>
    <div class="ap-h"><${Icon} n="lock" /><span>${resolved ? (resolved.cancelled ? 'Demande annulée' : resolved.allowed ? 'Autorisé' + (resolved.auto ? ' automatiquement' : '') : 'Refusé') : 'Autorisation demandée'}</span></div>
    <div class="ap-tool"><${Icon} n=${ico} />${t.kind === 'execute' ? html`<code>${t.title}</code>` : html`<span>${t.title}</span>`}</div>
    ${!resolved && diffs.map(d => html`<div class="tc-file"><div class="tc-path">${rel(d.path, root)}</div><${Diff} d=${d} /></div>`)}
    ${!resolved && html`<div class="ap-acts">${opts.map(o => html`<button class=${cls('btn sm', o.kind === 'allow_once' && 'primary', o.kind && o.kind.startsWith('reject') && 'ghost')} onClick=${() => onAnswer(o.id)}>${o.name}</button>`)}</div>`}
  </div>`;
}
