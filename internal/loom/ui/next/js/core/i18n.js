import fr from '../i18n/fr.js';
import en from '../i18n/en.js';

const dictionaries = { fr, en };
const sourceKeys = new Map(Object.entries(fr).filter(([, value]) => typeof value === 'string').map(([key, value]) => [value, key]));
const sourcePatterns = Object.entries(fr).filter(([key, value]) => /^(runtime|engine)\.copy\./.test(key) && typeof value === 'string' && /\{\w+\}/.test(value))
  .map(([key, value]) => {
    const names = [...value.matchAll(/\{(\w+)\}/g)].map(match => match[1]);
    const pattern = new RegExp('^' + value.split(/\{\w+\}/).map(part => part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('(.+?)') + '$');
    return { key, names, pattern };
  });
const valid = lang => lang === 'fr' || lang === 'en';
const listeners = new Set();
let revision = 0;
let pendingSave = Promise.resolve();

function initialLanguage() {
  try {
    const saved = globalThis.localStorage?.getItem('loom-lang');
    if (valid(saved)) return saved;
  } catch (_) {}
  return /^fr/i.test(globalThis.navigator?.language || '') ? 'fr' : 'en';
}

let current = initialLanguage();
export const getLang = () => current;
export const locale = () => current === 'fr' ? 'fr-FR' : 'en-US';
export const language = {
  get: getLang,
  subscribe(fn) { listeners.add(fn); return () => listeners.delete(fn); },
};

function apply(lang) {
  const changed = current !== lang;
  current = lang;
  if (globalThis.document) document.documentElement.lang = lang;
  try { globalThis.localStorage?.setItem('loom-lang', lang); } catch (_) {}
  if (changed) listeners.forEach(fn => fn());
}

if (globalThis.document) document.documentElement.lang = current;

// Values are returned as text. The template/Markdown renderer owns escaping.
export function t(key, vars = {}) {
  let text = dictionaries[current][key];
  if (text == null) return key;
  if (typeof text === 'object') {
    const form = new Intl.PluralRules(locale()).select(Number(vars.n));
    text = text[form] ?? text.other;
  }
  return text.replace(/\{([a-zA-Z_][\w]*)\}/g, (match, name) =>
    Object.prototype.hasOwnProperty.call(vars, name) ? String(vars[name]) : match);
}

// Only for Loom-owned catalog copy received from the server. Native output,
// user content and API errors must be displayed verbatim.
export function tSource(text) {
  const key = sourceKeys.get(text);
  if (key) return t(key);
  if (typeof text === 'string') {
    for (const { key, names, pattern } of sourcePatterns) {
      const match = text.match(pattern);
      if (match) return t(key, Object.fromEntries(names.map((name, i) => [name, match[i + 1]])));
    }
  }
  return text;
}

// Update synchronously; serialize saves so the last selection wins on the server.
export function setLang(lang) {
  if (!valid(lang)) return Promise.resolve(false);
  revision++;
  apply(lang);
  pendingSave = pendingSave.catch(() => false).then(async () => {
    const { post } = await import('./api.js');
    const response = await post('/api/prefs', { lang });
    return response.ok !== false && response.prefs?.lang === lang;
  });
  return pendingSave;
}

// A late preference read must never undo a selection made while it was pending.
export async function initLang() {
  const before = revision;
  try {
    const { get } = await import('./api.js');
    const response = await get('/api/prefs');
    if (revision === before && valid(response.prefs?.lang)) apply(response.prefs.lang);
  } catch (_) {} // Offline: keep the browser's remembered language.
}
