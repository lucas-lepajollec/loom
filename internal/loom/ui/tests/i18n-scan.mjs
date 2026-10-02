// Small dependency-free lexer for native JS modules and nested html templates.
// It ignores comments/regexes and keeps template text separate from JS values.
export function scan(source) {
  const tokens = [], templates = [], stringsInHTML = [];
  const push = (type, value, start, inHTML) => { const token = { type, value, start }; tokens.push(token); if (type === 'string' && inHTML) stringsInHTML.push(token); };
  function quoted(i, inHTML) {
    const start = i, quote = source[i++]; let value = '';
    while (i < source.length && source[i] !== quote) {
      if (source[i] === '\\') { value += source.slice(i, i + 2); i += 2; } else value += source[i++];
    }
    push('string', value, start, inHTML); return i + 1;
  }
  function template(i, isHTML, inHTML) {
    const start = i++; let from = i; const parts = [];
    while (i < source.length) {
      if (source[i] === '\\') { i += 2; continue; }
      if (source[i] === '`') { parts.push({ value: source.slice(from, i), start: from }); if (isHTML) templates.push({ start, parts }); return i + 1; }
      if (source[i] === '$' && source[i + 1] === '{') {
        parts.push({ value: source.slice(from, i), start: from });
        i = code(i + 2, true, isHTML || inHTML); from = i;
      } else i++;
    }
    return i;
  }
  function regex(i) {
    i++; let bracket = false;
    while (i < source.length) {
      if (source[i] === '\\') { i += 2; continue; }
      if (source[i] === '[') bracket = true;
      if (source[i] === ']') bracket = false;
      if (source[i++] === '/' && !bracket) { while (/[a-z]/i.test(source[i] || '') && i < source.length) i++; return i; }
    }
    return i;
  }
  function code(i, stop, inHTML) {
    let depth = 0, previous = '';
    while (i < source.length) {
      const ch = source[i];
      if (/\s/.test(ch)) { i++; continue; }
      if (source.startsWith('//', i)) { const end = source.indexOf('\n', i); i = end < 0 ? source.length : end; continue; }
      if (source.startsWith('/*', i)) { const end = source.indexOf('*/', i + 2); i = end < 0 ? source.length : end + 2; continue; }
      if (ch === "'" || ch === '"') { i = quoted(i, inHTML); previous = 'value'; continue; }
      if (ch === '`') { i = template(i, previous === 'html', inHTML); previous = 'value'; continue; }
      if (ch === '/' && (!previous || /^(?:[=(:,!&|?;{\[]|return|=>)$/.test(previous))) { i = regex(i); previous = 'value'; continue; }
      if (/[a-zA-Z_$]/.test(ch)) { const start = i++; while (i < source.length && /[\w$]/.test(source[i])) i++; previous = source.slice(start, i); push('identifier', previous, start); continue; }
      if (ch === '{') depth++;
      if (ch === '}') { if (!depth && stop) return i + 1; depth--; }
      const arrow = ch === '=' && source[i + 1] === '>';
      previous = arrow ? '=>' : ch; push('punctuation', previous, i); i += arrow ? 2 : 1;
    }
    return i;
  }
  code(0, false, false);
  return { tokens: tokens.sort((a, b) => a.start - b.start), templates, stringsInHTML };
}

// The tag/quote state survives expressions such as <${Line} title=${...}>.
export function templateCopy(template) {
  const copy = []; let inTag = false, quote = '', attribute = '';
  for (const { value: text, start } of template.parts) {
    let i = 0;
    while (i < text.length) {
      if (!inTag) {
        if (text[i] === '<') { inTag = true; i++; continue; }
        let end = text.indexOf('<', i); if (end < 0) end = text.length;
        copy.push({ value: text.slice(i, end).trim(), start: start + i }); i = end;
      } else if (quote) {
        const end = text.indexOf(quote, i), stop = end < 0 ? text.length : end;
        if (['title', 'aria-label', 'label', 'tip', 'placeholder', 'text', 'sub', 'alt', 'k'].includes(attribute)) copy.push({ value: text.slice(i, stop), start: start + i });
        i = stop; if (end >= 0) { i++; quote = ''; attribute = ''; }
      } else if (text[i] === '>') { inTag = false; i++; }
      else {
        const match = text.slice(i).match(/^([\w-]+)=(["'])/);
        if (match) { attribute = match[1]; quote = match[2]; i += match[0].length; } else i++;
      }
    }
  }
  return copy;
}

export const frenchLooking = value => /[àâäéèêëîïôöùûüçœ]|\b(?:bonjour|aucune?|annuler|ajouter|ouvrir|fermer|enregistrer|chargement|rechercher|recherche|outils|dossier|réglages|modèles?|moteur|pour|dans|sans|avec|une?|le|la|les|du|des|non|oui|connecter|commande|serveur|disponible)\b/i.test(value);
