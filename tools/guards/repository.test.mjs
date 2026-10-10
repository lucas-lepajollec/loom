import test from 'node:test';
import assert from 'node:assert/strict';
import { check, structureErrors, copyErrors, parityErrors, userCopy } from './repository.mjs';

test('checkout satisfies structure and bilingual UI guards', async () => assert.deepEqual(await check(), []));
test('a new root Go file needs a documented ownership decision', () => {
  const baseline = { rootGo: { 'old.go': 'Application orchestration' }, jsBytes: 10, jsGrandfathered: {} };
  assert.deepEqual(structureErrors(['old.go'], {}, baseline), []);
  assert.match(structureErrors(['old.go', 'new.go'], {}, baseline).join(), /new.go/);
  assert.match(structureErrors([], {}, baseline).join(), /obsolete/);
});
test('new and grandfathered JS modules fail on growth beyond their byte budgets', () => {
  const baseline = { rootGo: {}, jsBytes: 10, jsGrandfathered: { 'old.js': 20 } };
  assert.deepEqual(structureErrors([], { 'old.js': 'x'.repeat(20), 'new.js': 'x'.repeat(10) }, baseline), []);
  assert.match(structureErrors([], { 'old.js': 'x'.repeat(21), 'new.js': 'x'.repeat(11) }, baseline).join(), /old.js.*new.js/s);
  assert.match(structureErrors([], { 'old.js': 'é'.repeat(11) }, baseline).join(), /22 > 20/);
});
test('translation parity guard detects removed keys, variables and plural forms', () => {
  const en = { save: 'Save {name}', rows: { one: '{n} row', other: '{n} rows' } };
  const fr = { save: 'Sauver {name}', rows: { one: '{n} ligne', other: '{n} lignes' } };
  assert.deepEqual(parityErrors(en, fr), []);
  assert.match(parityErrors(en, { rows: fr.rows }).join(), /save/);
  assert.match(parityErrors(en, { ...fr, save: 'Sauver' }).join(), /variable/);
  assert.match(parityErrors(en, { ...fr, rows: { one: '{n} ligne' } }).join(), /plural/);
});
test('new English/French copy fails in nested templates, attributes, expressions and notifications', () => {
  for (const literal of ['Save changes', 'Enregistrer']) for (const source of [
    'html`<button>' + literal + '</button>`',
    'html`<input aria-label="' + literal + '" />`',
    'html`<b>${ok ? "' + literal + '" : t("saved")}</b>`',
    'html`<div>${ok && html`<b>' + literal + '</b>`}</div>`',
    'toast("' + literal + '")', 'const field = { label: "' + literal + '" };',
  ]) assert.match(copyErrors({ 'new.js': source }, {}).join(), /untranslated/);
  assert.deepEqual(copyErrors({ 'new.js': 'html`<button>${t("save")}</button>`; toast(t("saved"));' }, {}), []);
});
test('copy grandfathering is exact, occurrence-bounded and rejects stale exceptions', () => {
  const source = 'html`<b>Save</b>`', baseline = { 'old.js': userCopy(source) };
  assert.deepEqual(copyErrors({ 'old.js': source }, baseline), []);
  assert.match(copyErrors({ 'old.js': source + source }, baseline).join(), /untranslated/);
  assert.match(copyErrors({ 'new.js': source }, baseline).join(), /untranslated/);
  assert.match(copyErrors({ 'old.js': '' }, baseline).join(), /obsolete/);
});

test('template strings and conditional notification copy cannot evade the new-copy guard', () => {
  for (const source of [
    'toast(`Saved ${name}`)', 'const item = { label: `Save ${name}` };',
    'html`<b>${`Save ${name}`}</b>`',
    'toast(ok ? "Saved" : "Failed")',
  ]) assert.match(copyErrors({ 'new.js': source }, {}).join(), /untranslated/);
});

test('the filesystem entry point rejects a bad checkout, not only helper snapshots', async () => {
  const fs = await import('node:fs');
  const path = await import('node:path');
  const os = await import('node:os');
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'loom-guard-fixture-'));
  try {
    const write = (file, source) => {
      const full = path.join(dir, file);
      fs.mkdirSync(path.dirname(full), { recursive: true });
      fs.writeFileSync(full, source);
    };
    write('package.json', '{"type":"module"}');
    write('tools/guards/structure.json', JSON.stringify({ rootGo: {}, jsBytes: 60, jsGrandfathered: {} }));
    write('tools/guards/ui-copy.json', '{}');
    write('internal/loom/new.go', 'package loom');
    write('internal/loom/ui/next/js/i18n/en.js', 'export default { save: "Save" };');
    write('internal/loom/ui/next/js/i18n/fr.js', 'export default {};');
    write('internal/loom/ui/next/js/new.js', 'const view = html`<button>Save changes</button>`;\n' + ' '.repeat(70));
    const errors = (await check(dir)).join('\n');
    assert.match(errors, /root Go file.*new.go/);
    assert.match(errors, /JS size budget.*new.js/);
    assert.match(errors, /untranslated.*Save changes/);
    assert.match(errors, /translation key.*save/);
  } finally { fs.rmSync(dir, { recursive: true, force: true }); }
});
