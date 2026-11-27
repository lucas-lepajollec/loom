import { t } from '../../core/i18n.js';
// Markdown des réponses. Le HTML brut écrit par un modèle est ÉCHAPPÉ : une
// réponse ne peut pas injecter de balises ou de scripts dans l'interface.
const esc = s => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
const safeUrl = u => /^(https?:|mailto:|#|\/)/i.test(String(u || '').trim()) ? u : '#';
const fileHref = u => {
  const s = String(u || '').trim();
  if (!s || s.startsWith('#') || s.startsWith('//')) return '';
  if (/^file:\/\//i.test(s)) return s;
  if (/^[a-z][a-z0-9+.-]*:/i.test(s)) return '';
  return s;
};

let ready = false;
function setup() {
  if (ready || !window.marked) return;
  ready = true;
  window.marked.use({
    gfm: true, breaks: false,
    renderer: {
      html(token) { return esc(typeof token === 'string' ? token : token.text || token.raw || ''); },
      link(href, title, text) {
        if (typeof href === 'object') { ({ href, title, text } = href); }
        const local = fileHref(href);
        const safe = safeUrl(href);
        const extra = local ? ` data-file-href="${esc(local)}"` : '';
        const target = local ? '' : ' target="_blank" rel="noopener noreferrer"';
        return `<a href="${esc(safe)}"${target}${extra}${title ? ` title="${esc(title)}"` : ''}>${text}</a>`;
      },
      image(href, title, text) {
        if (typeof href === 'object') { ({ href, title, text } = href); }
        return `<a href="${esc(safeUrl(href))}" target="_blank" rel="noopener noreferrer">${esc(text || 'image')}</a>`;
      },
      code(code, lang) {
        if (typeof code === 'object') { lang = code.lang; code = code.text; }
        const l = String(lang || '').split(/\s/)[0];
        return `<div class="code"><div class="code-h"><span>${esc(l || t('chat.md.text'))}</span><button type="button" class="btn sm" data-copy>${esc(t('chat.md.copier'))}</button></div><pre><code>${esc(code)}</code></pre></div>`;
      },
    },
  });
}

// Corrige les fautes de Markdown courantes des LLM (fence collée au texte, blancs).
function fix(s) {
  return String(s || '').replace(/([^\n])(\s*)(```+|~~~+)(?=\w*\s*\n)/g, '$1\n$3').replace(/\n{3,}/g, '\n\n');
}

// Cache LRU borné : évite de re-parser le Markdown des messages inchangés à
// chaque tick ou changement d'état du fil. La clé inclut le libellé du bouton
// Copier pour suivre un changement de langue sans importer i18n/getLang.
const MD_CACHE_MAX = 400;
const MD_CACHE_MAX_LEN = 200000;
const mdCache = new Map();

export function md(src) {
  setup();
  if (!src) return '';
  if (!window.marked) return '<p>' + esc(src).replace(/\n/g, '<br>') + '</p>';
  const text = String(src);
  if (text.length <= MD_CACHE_MAX_LEN) {
    const key = t('chat.md.copier') + '\0' + text;
    const hit = mdCache.get(key);
    if (hit !== undefined) {
      mdCache.delete(key);
      mdCache.set(key, hit);
      return hit;
    }
    const out = window.marked.parse(fix(text));
    mdCache.set(key, out);
    if (mdCache.size > MD_CACHE_MAX) mdCache.delete(mdCache.keys().next().value);
    return out;
  }
  return window.marked.parse(fix(text));
}
export const plain = src => '<p>' + esc(src || '').replace(/\n/g, '<br>') + '</p>';

// Boutons « Copier » des blocs de code, par délégation.
document.addEventListener('click', e => {
  const b = e.target.closest('[data-copy]');
  if (!b) return;
  const code = b.closest('.code').querySelector('code').textContent;
  navigator.clipboard && navigator.clipboard.writeText(code).then(() => { b.textContent = t("chat.md.copie"); setTimeout(() => { b.textContent = t("chat.md.copier"); }, 1400); });
});
