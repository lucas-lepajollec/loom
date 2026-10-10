import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

// Chaque composant utilisé dans un gabarit (<${Nom} …>) doit être défini ou
// importé dans le même fichier : sinon la page plante seulement à l'affichage.
const root = new URL('../next/js/', import.meta.url).pathname;
const files = [];
const walk = d => { for (const e of fs.readdirSync(d, { withFileTypes: true })) { const p = path.join(d, e.name); e.isDirectory() ? walk(p) : p.endsWith('.js') && files.push(p); } };
walk(root);

test('composants utilisés = composants définis ou importés', () => {
  const missing = [];
  for (const f of files) {
    const src = fs.readFileSync(f, 'utf8');
    const used = new Set([...src.matchAll(/<\$\{([A-Z][A-Za-z0-9_]*)\}/g)].map(m => m[1]));
    for (const name of used) {
      const declared = new RegExp(`(function\\s+${name}\\b|class\\s+${name}\\b|(const|let|var)\\s+${name}\\b|import\\s*\\{[^}]*\\b${name}\\b[^}]*\\}|import\\s+${name}\\b|\\b${name}\\s*:)`).test(src);
      if (!declared) missing.push(path.relative(root, f) + ' : ' + name);
    }
  }
  assert.deepEqual(missing, []);
});

// Syntax checking alone cannot catch a misspelled imported name after splitting
// a page: ESM rejects the graph before PageGuard can render a useful error.
test('relative static imports resolve existing files and exported bindings', () => {
  const failures = [];
  const exported = (source, name) => new RegExp(`export\\s+(?:(?:async\\s+)?function|class|const|let|var)\\s+${name}\\b|export\\s*\\{[^}]*\\b${name}\\b`).test(source);
  for (const file of files) {
    const source = fs.readFileSync(file, 'utf8');
    for (const match of source.matchAll(/import\s+(?:([^;'"\n]+?)\s+from\s+)?['"](\.[^'"]+)['"]/g)) {
      const target = path.resolve(path.dirname(file), match[2]);
      if (!fs.existsSync(target)) { failures.push(path.relative(root, file) + ': missing ' + match[2]); continue; }
      const named = /\{([^}]+)\}/.exec(match[1] || '');
      if (!named) continue;
      const declarations = fs.readFileSync(target, 'utf8');
      for (const binding of named[1].split(',')) {
        const name = binding.trim().split(/\s+as\s+/)[0];
        if (name && !exported(declarations, name)) failures.push(path.relative(root, file) + ': ' + match[2] + ' does not export ' + name);
      }
    }
  }
  assert.deepEqual(failures, []);
});
