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
      const declared = new RegExp(`(function\\s+${name}\\b|(const|let|var)\\s+${name}\\b|import\\s*\\{[^}]*\\b${name}\\b[^}]*\\}|import\\s+${name}\\b|\\b${name}\\s*:)`).test(src);
      if (!declared) missing.push(path.relative(root, f) + ' : ' + name);
    }
  }
  assert.deepEqual(missing, []);
});
