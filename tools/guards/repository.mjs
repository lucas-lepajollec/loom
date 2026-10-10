// Offline repository checks. Reuse the UI lexer; do not add a parser dependency.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { scan, templateCopy } from '../../internal/loom/ui/tests/i18n-scan.mjs';

export const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
export function filesUnder(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap(e => e.isDirectory()
    ? filesUnder(path.join(dir, e.name)) : [path.join(dir, e.name)]);
}

// Count occurrences, not only distinct literals: old copy cannot spread to new UI.
export function userCopy(source) {
  const parsed = scan(source), found = new Map();
  const add = value => {
    value = value.trim();
    if (/\p{L}/u.test(value)) found.set(value, (found.get(value) || 0) + 1);
  };
  parsed.templates.flatMap(templateCopy).forEach(x => add(x.value));
  const copyFields = /^(?:label|title|tip|placeholder|text|sub|alt|message|error)$/;
  const copyCalls = /^(?:toast|alert|confirm|prompt|Error)$/;
  const notificationRanges = [];
  for (let i = 0; i < parsed.tokens.length; i++) {
    if (!copyCalls.test(parsed.tokens[i].value) || parsed.tokens[i + 1]?.value !== '(') continue;
    let depth = 0;
    for (let j = i + 1; j < parsed.tokens.length; j++) {
      if (parsed.tokens[j].value === '(') depth++;
      if (parsed.tokens[j].value === ')' && --depth === 0) {
        notificationRanges.push([parsed.tokens[i + 1].start, parsed.tokens[j].start]); break;
      }
    }
  }
  const inNotification = start => notificationRanges.some(([a, b]) => start > a && start < b);
  for (let i = 0; i < parsed.tokens.length; i++) {
    const token = parsed.tokens[i], prev = parsed.tokens[i - 1], before = parsed.tokens[i - 2];
    if (token.type !== 'string') continue;
    if (prev?.value === '(' && before?.value === 't') continue;
    // Expressions rendered in html, labeled data objects and notification calls.
    if (parsed.stringsInHTML.includes(token) || inNotification(token.start) ||
      (prev?.value === ':' && copyFields.test(before?.value || ''))) add(token.value);
  }
  for (const template of parsed.stringTemplates) {
    const field = source.slice(0, template.start).match(/([\w-]+)\s*:\s*$/)?.[1];
    if (template.inHTML || inNotification(template.start) || copyFields.test(field || '')) template.parts.forEach(p => add(p.value));
  }
  return Object.fromEntries([...found].sort(([a], [b]) => a.localeCompare(b, 'en')));
}

export function structureErrors(rootGoFiles, modules, baseline) {
  const errors = [];
  for (const file of rootGoFiles) if (!Object.hasOwn(baseline.rootGo, file)) errors.push('root Go file needs an ownership allowlist entry: ' + file);
  for (const [file, reason] of Object.entries(baseline.rootGo)) if (!reason || !rootGoFiles.includes(file)) errors.push('obsolete/undocumented root Go entry: ' + file);
  for (const [file, source] of Object.entries(modules)) {
    const budget = baseline.jsGrandfathered[file] ?? baseline.jsBytes;
    if (Buffer.byteLength(source) > budget) errors.push(`JS size budget exceeded: ${file} (${Buffer.byteLength(source)} > ${budget})`);
  }
  for (const file of Object.keys(baseline.jsGrandfathered)) if (!Object.hasOwn(modules, file)) errors.push('obsolete JS budget: ' + file);
  return errors;
}

export function copyErrors(modules, baseline) {
  const errors = [];
  for (const [file, source] of Object.entries(modules)) {
    if (file.startsWith('i18n/')) continue; // dictionary values are the translation source
    const found = userCopy(source), allowed = baseline[file] || {};
    for (const [literal, count] of Object.entries(found)) if (count > (allowed[literal] || 0)) errors.push(`${file}: untranslated UI literal ${JSON.stringify(literal)}`);
    for (const [literal, count] of Object.entries(allowed)) if ((found[literal] || 0) < count) errors.push(`${file}: remove obsolete copy exception ${JSON.stringify(literal)}`);
  }
  for (const file of Object.keys(baseline)) if (!Object.hasOwn(modules, file)) errors.push('obsolete copy baseline: ' + file);
  return errors;
}

export function parityErrors(en, fr) {
  const errors = [];
  const variables = value => [...value.matchAll(/\{(\w+)\}/g)].map(m => m[1]).sort().join(',');
  for (const key of new Set([...Object.keys(en), ...Object.keys(fr)])) {
    if (!Object.hasOwn(en, key) || !Object.hasOwn(fr, key) || typeof en[key] !== typeof fr[key]) { errors.push('translation key/type mismatch: ' + key); continue; }
    if (typeof en[key] === 'string') {
      if (variables(en[key]) !== variables(fr[key])) errors.push('translation variable mismatch: ' + key);
    } else if (en[key] && fr[key] && typeof en[key] === 'object') {
      if (Object.keys(en[key]).sort().join(',') !== 'one,other' || Object.keys(fr[key]).sort().join(',') !== 'one,other') errors.push('translation plural mismatch: ' + key);
      errors.push(...parityErrors(en[key], fr[key]).map(e => key + ': ' + e));
    } else errors.push('invalid translation value: ' + key);
  }
  return errors;
}

export async function check(repository = root) {
  const dir = path.join(repository, 'internal/loom/ui/next/js');
  const modules = Object.fromEntries(filesUnder(dir).filter(f => f.endsWith('.js')).map(f => [path.relative(dir, f), fs.readFileSync(f, 'utf8')]));
  const rootGo = fs.readdirSync(path.join(repository, 'internal/loom')).filter(f => f.endsWith('.go'));
  const baseline = JSON.parse(fs.readFileSync(path.join(repository, 'tools/guards/structure.json')));
  const copy = JSON.parse(fs.readFileSync(path.join(repository, 'tools/guards/ui-copy.json')));
  const en = (await import(path.join(dir, 'i18n/en.js'))).default;
  const fr = (await import(path.join(dir, 'i18n/fr.js'))).default;
  return [...structureErrors(rootGo, modules, baseline), ...copyErrors(modules, copy), ...parityErrors(en, fr)];
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const errors = await check();
  if (errors.length) { console.error(errors.join('\n')); process.exitCode = 1; }
  else console.log('Repository structure and bilingual UI guards passed.');
}
