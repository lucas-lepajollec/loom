// Fonctions pures du panneau latéral : bornage de largeur et extraction des
// chemins de documents/fichiers cités par un agent (conversation ou terminal CLI).

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
  if (/^(?:https?:|mailto:|data:|javascript:|ftp:)/i.test(s) || s.startsWith('#')) return '';
  if (/^file:\/\//i.test(s)) {
    const stripped = s.replace(/^file:\/\/(?:localhost)?/i, '');
    try { s = decodeURIComponent(stripped); } catch (_) { s = stripped; }
  }
  // Retire les ancres de lignes (#L12-L45, :12:3) et guillemets/ponctuation.
  s = s
    .replace(/^['"`(\[<]+|['"`)\]>,.;:!?]+$/g, '')
    .replace(/#L?\d+(?:-L?\d+)?$/i, '')
    .replace(/:\d+(?::\d+)?$/, '')
    .replace(/^['"`(\[<]+|['"`)\]>,.;:!?]+$/g, '');
  if (!s || s.includes('..') || /\s/.test(s)) return '';
  const root = String(workingDir || '').replace(/\/+$/, '');
  if (root && s.startsWith(root + '/')) s = s.slice(root.length + 1);
  s = s.replace(/^\.\//, '');
  if (!DOC_EXT.test(s) && !SPECIAL_NAMES.test(s)) return '';
  return s;
}

// Extrait les URI d'hyperliens terminaux OSC 8 (\x1b]8;params;URI\x07 ou ST).
const OSC8_RE = /\x1b\]8;[^;\x07\x1b]*;([^\x07\x1b]+)(?:\x07|\x1b\\)/g;
export function extractOsc8Uris(raw) {
  const out = [];
  if (!raw || !raw.includes('\x1b]8;')) return out;
  for (const m of String(raw).matchAll(OSC8_RE)) {
    if (m[1]) out.push(m[1]);
  }
  return out;
}

const ANSI_RE = /\x1b(?:\][^\x07\x1b]*(?:\x07|\x1b\\)|\[[0-?]*[ -/]*[@-~]|[@-Z\\-_])/g;
export function stripAnsi(raw) {
  return String(raw || '').replace(ANSI_RE, '');
}

const MD_LINK_RE = /\[[^\]]{1,120}\]\(([^)\s]+)\)/g;
const CODE_PATH_RE = /`([^`\n]{2,220})`/g;
// Chemins bruts dans du texte (terminal ou prose) : file://..., chemins avec /,
// ou noms de fichiers Markdown autonomes.
const LINE_TOKEN_RE = /(?:file:\/\/(?:localhost)?\/[^\s"'`<>()\[\]]+|(?:https?:\/\/[^\s"'`<>()\[\]]+)|(?:(?:\.{1,2}\/|\/|[A-Za-z0-9_.-]+\/)[A-Za-z0-9_.@\/-]+\.[A-Za-z0-9]{1,10}(?:#L?\d+(?:-L?\d+)?|:\d+(?::\d+)?)?)|(?:\b[A-Za-z0-9_.-]+\.(?:md|mdx|markdown)\b(?:#L?\d+(?:-L?\d+)?|:\d+(?::\d+)?)?))/g;

// Trouve tous les liens de fichiers cliquables dans une ligne de texte (terminal xterm).
export function findFileLinksInLine(line, workingDir = '', knownPaths = []) {
  const out = [];
  const text = String(line || '');
  if (!text) return out;
  for (const m of text.matchAll(LINE_TOKEN_RE)) {
    const raw = m[0];
    const start = m.index;
    // Ignore les URLs web (capturées pour empêcher qu'un suffixe /foo.md matche).
    if (/^https?:\/\//i.test(raw)) continue;
    // Ignore les fragments précédés immédiatement par un caractère de mot ou ://.
    if (start > 0 && /[A-Za-z0-9_:@]/.test(text[start - 1])) continue;
    let path = normalizeSessionPath(raw, workingDir);
    if (!path) continue;
    // Si le chemin est un nom de fichier nu (ex: NOW.md) et qu'un chemin complet
    // récent se termine par /NOW.md, on le résout automatiquement.
    if (!path.includes('/') && knownPaths && knownPaths.length) {
      const hit = knownPaths.find(p => p === path || p.endsWith('/' + path));
      if (hit) path = hit;
    }
    const trimmed = raw.replace(/[.,;:!?)\]>'"]+$/, '');
    out.push({ start, end: start + trimmed.length, raw: trimmed, path });
  }
  return out;
}

// Extrait les fichiers et documents cités dans un texte arbitraire (buffer
// terminal ou message d'agent), les plus récents d'abord, Markdown en tête.
export function extractFilesFromText(text, workingDir = '', extraUris = []) {
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
  // Les URI OSC 8 les plus récentes d'abord (chemins complets file://...).
  for (let i = (extraUris || []).length - 1; i >= 0; i--) push(extraUris[i]);
  const clean = stripAnsi(text);
  // Parcourt du bas vers le haut pour prioriser les fichiers les plus récents.
  const lines = clean.split(/\r?\n/);
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i];
    if (!line || line.length < 4) continue;
    for (const m of line.matchAll(MD_LINK_RE)) push(m[1]);
    for (const m of line.matchAll(CODE_PATH_RE)) {
      if (m[1].includes('/') || DOC_EXT.test(m[1])) push(m[1]);
    }
    for (const link of findFileLinksInLine(line, workingDir)) {
      // Évite de polluer avec un nom nu si un chemin complet du même fichier existe déjà.
      if (!link.path.includes('/') && [...seen].some(p => p.endsWith('/' + link.path))) continue;
      push(link.path);
    }
    if (docs.length + others.length >= 16) break;
  }
  return [...docs, ...others].slice(0, 8);
}

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
      for (const link of findFileLinksInLine(text, workingDir)) {
        if (!link.path.includes('/') && [...seen].some(p => p.endsWith('/' + link.path))) continue;
        push(link.path);
      }
    }
    if (docs.length + others.length >= 12) break;
  }
  return [...docs, ...others].slice(0, 8);
}
