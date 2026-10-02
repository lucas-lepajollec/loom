import { t } from '../../core/i18n.js';
// Markdown des réponses. Le HTML brut écrit par un modèle est ÉCHAPPÉ : une
// réponse ne peut pas injecter de balises ou de scripts dans l'interface.
const esc = s => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
const safeUrl = u => /^(https?:|mailto:|#|\/)/i.test(String(u || '').trim()) ? u : '#';

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
        return `<a href="${esc(safeUrl(href))}" target="_blank" rel="noopener noreferrer"${title ? ` title="${esc(title)}"` : ''}>${text}</a>`;
      },
      image(href, title, text) {
        if (typeof href === 'object') { ({ href, title, text } = href); }
        return `<a href="${esc(safeUrl(href))}" target="_blank" rel="noopener noreferrer">${esc(text || 'image')}</a>`;
      },
      code(code, lang) {
        if (typeof code === 'object') { lang = code.lang; code = code.text; }
        const l = String(lang || '').split(/\s/)[0];
        return `<div class="code"><div class="code-h"><span>${esc(l || t('chat.md.text'))}</span><button type="button" class="code-copy" data-copy>${esc(t('chat.md.copier'))}</button></div><pre><code>${esc(code)}</code></pre></div>`;
      },
    },
  });
}

// Corrige les fautes de Markdown courantes des LLM (fence collée au texte, blancs).
function fix(s) {
  return String(s || '').replace(/([^\n])(\s*)(```+|~~~+)(?=\w*\s*\n)/g, '$1\n$3').replace(/\n{3,}/g, '\n\n');
}

export function md(src) {
  setup();
  if (!src) return '';
  if (!window.marked) return '<p>' + esc(src).replace(/\n/g, '<br>') + '</p>';
  return window.marked.parse(fix(src));
}
export const plain = src => '<p>' + esc(src || '').replace(/\n/g, '<br>') + '</p>';

// Boutons « Copier » des blocs de code, par délégation.
document.addEventListener('click', e => {
  const b = e.target.closest('[data-copy]');
  if (!b) return;
  const code = b.closest('.code').querySelector('code').textContent;
  navigator.clipboard && navigator.clipboard.writeText(code).then(() => { b.textContent = t("chat.md.copie"); setTimeout(() => { b.textContent = t("chat.md.copier"); }, 1400); });
});
