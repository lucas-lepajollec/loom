// Fonctions pures du panneau latéral : bornage de largeur et extraction des
// chemins de documents/fichiers cités par un agent dans la conversation.

export const DEFAULT_PANEL_WIDTH = 360;
export const MIN_PANEL_WIDTH = 280;
export const MAX_PANEL_WIDTH = 780;

export function clampPanelWidth(w, viewport = typeof innerWidth === 'number' ? innerWidth : 1440) {
  const cap = Math.max(MIN_PANEL_WIDTH, Math.min(MAX_PANEL_WIDTH, Math.round(viewport * 0.65)));
  const n = Math.round(Number(w) || DEFAULT_PANEL_WIDTH);
  return Math.max(MIN_PANEL_WIDTH, Math.min(cap, n));
}

// Extensions de fichiers lisibles en aperçu (documents Markdown, config, code).
const DOC_EXT = /\.(?:md|mdx|markdown|txt|rst|json|ya?ml|toml|ini|conf|cfg|env|go|js|mjs|cjs|ts|tsx|jsx|py|rs|rb|java|kt|c|h|cc|cpp|hpp|cs|php|sh|bash|zsh|sql|html?|css|scss|svg|xml|csv|diff|patch|log)$/i;
const SPECIAL_NAMES = /(?:^|\/)(?:Dockerfile|Makefile|README|CHANGELOG|LICENSE|AGENTS\.md|CLAUDE\.md|GEMINI\.md)$/i;
const MD_EXT = /\.(?:md|mdx|markdown)$/i;

export const isMarkdownPath = p => MD_EXT.test(String(p || ''));

// Normalise un chemin cité par un agent vers un chemin relatif au dossier de la
// session quand c'est possible (requis par /sessions/:id/file-content de Codeman).
export function normalizeSessionPath(raw, workingDir = '') {
  let s = String(raw || '').trim();
  if (!s) return '';
  if (/^(?:https?:|mailto:|data:|javascript:)/i.test(s) || s.startsWith('#')) return '';
  if (/^file:\/\//i.test(s)) {
    try { s = decodeURIComponent(s.replace(/^file:\/\//i, '')); } catch (_) { s = s.replace(/^file:\/\//i, ''); }
  }
  // Retire les ancres de lignes (#L12-L45, :12:3) et guillemets/ponctuation.
  s = s.replace(/#L?\d+(?:-L?\d+)?$/i, '').replace(/:\d+(?::\d+)?$/, '').replace(/^['"`(]+|['"`),.;:]+$/g, '');
  if (!s || s.includes('..') || /\s/.test(s)) return '';
  const root = String(workingDir || '').replace(/\/+$/, '');
  if (root && s.startsWith(root + '/')) s = s.slice(root.length + 1);
  s = s.replace(/^\.\//, '');
  if (!DOC_EXT.test(s) && !SPECIAL_NAMES.test(s)) return '';
  return s;
}

const MD_LINK_RE = /\[[^\]]{1,120}\]\(([^)\s]+)\)/g;
const CODE_PATH_RE = /`([^`\n]{2,220})`/g;

// Extrait les fichiers et documents cités dans les tours récents de l'agent,
// les plus récents d'abord, en mettant les fichiers Markdown en tête.
export function extractMentionedFiles(turns, workingDir = '') {
  const seen = new Set();
  const docs = [];
  const others = [];
  const push = raw => {
    const p = normalizeSessionPath(raw, workingDir);
    if (!p || seen.has(p)) return;
    seen.add(p);
    if (isMarkdownPath(p)) docs.push(p);
    else others.push(p);
  };
  const recent = (turns || []).slice(-6).reverse();
  for (const turn of recent) {
    const texts = [];
    if (turn.answer) texts.push(turn.answer);
    for (const item of [...(turn.items || []), ...(turn.work || [])].reverse()) {
      if (item && item.k === 'say' && item.text) texts.push(item.text);
    }
    for (const text of texts) {
      for (const m of text.matchAll(MD_LINK_RE)) push(m[1]);
      for (const m of text.matchAll(CODE_PATH_RE)) {
        if (m[1].includes('/') || DOC_EXT.test(m[1])) push(m[1]);
      }
    }
    if (docs.length + others.length >= 12) break;
  }
  return [...docs, ...others].slice(0, 8);
}
