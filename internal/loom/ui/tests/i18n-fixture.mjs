import fr from '../next/js/i18n/fr.js';
export const french = (key, vars = {}) => {
  let text = fr[key];
  if (typeof text === 'object') text = text[new Intl.PluralRules('fr').select(vars.n)] || text.other;
  if (text === undefined) throw new Error('Missing French key: ' + key);
  return text.replace(/\{(\w+)\}/g, (match, name) => name in vars ? String(vars[name]) : match);
};
