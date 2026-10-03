import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

function render(source) {
  const env = { window: {}, document: { addEventListener() {} }, t: key => key };
  vm.createContext(env);
  vm.runInContext(fs.readFileSync(new URL('../marked.min.js', import.meta.url), 'utf8'), env);
  env.window.marked = env.marked;
  const module = fs.readFileSync(new URL('../next/js/features/chat/md.js', import.meta.url), 'utf8');
  vm.runInContext(module.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.renderMarkdown = md;', env);
  return env.renderMarkdown(source);
}

test('model and model-card HTML cannot create executable DOM', () => {
  for (const input of ['<script>alert(1)</script>', '<img src=x onerror=alert(1)>', '<svg onload=alert(1)>', '<iframe srcdoc="<script>alert(1)</script>"></iframe>']) {
    const out = render(input);
    assert.doesNotMatch(out, /<(script|img|svg|iframe)\b/i);
    assert.match(out, /&lt;/);
  }
});

test('markdown links and images reject script schemes and escape attributes', () => {
  for (const input of ['[click](javascript:alert%281%29)', '[click](data:text/html,evil)', '![image](javascript:alert%281%29)', '[click](https://example.test "\" onmouseover=evil")']) {
    const out = render(input);
    assert.doesNotMatch(out, /(?:href|src)="(?:javascript|data):/i);
    assert.doesNotMatch(out, /" onmouseover=/i);
  }
  assert.match(render('[docs](https://example.test/docs)'), /rel="noopener noreferrer"/);
});

test('code blocks remain inert and retain readable code', () => {
  const out = render('```html\n<script>alert(1)</script>\n```');
  assert.doesNotMatch(out, /<script>/);
  assert.match(out, /&lt;script&gt;/);
});
