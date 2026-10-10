import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import htm from '../next/vendor/htm.mjs';
import { phoneLayoutIssues } from '../../../../tools/tests/ui-routes/layout.mjs';

// Semantic phone-table regression, socket-free. The same controls must retain
// their visible labels and version when the desktop header is removed.
const harnessRoot = new URL('../next/js/features/harnesses/', import.meta.url);
const rowFile = fs.existsSync(new URL('agent-machines.js', harnessRoot)) ? 'agent-machines.js' : 'page.js';
test('machine controls have visible labels independent of the table header', () => {
  const source = fs.readFileSync(process.env.LOOM_HARNESS_BASELINE || new URL(rowFile, harnessRoot), 'utf8');
  const state = [false, '', null];
  const env = { html: htm.bind((tag, props, ...children) => ({ tag, props: props || {}, children: children.flat(Infinity) })),
    useState: () => [state.shift(), () => {}], useEffect() {}, t: key => key,
    lifecycleVersion: s => s, lifecycleCurrent: () => false, Icon() {}, Switch() {}, Lifecycle() {}, get() {}, post() {}, confirm() {}, toast() {}, refreshWorkspace() {},
  };
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.row = MachineRow;', env);
  const row = env.row({ i: { machine: 'local', name: 'Codex', installed: true, managed: true, ready: true, version: '1.2.3' }, onChanged() {} });
  const flat = node => Array.isArray(node) ? node.flatMap(flat) : node && typeof node === 'object' ? [node, ...(node.children || []).flatMap(flat)] : [];
  const nodes = flat(row);
  for (const key of ['agents.manage', 'agents.use']) {
    assert.ok(nodes.some(n => n.tag === 'span' && n.children.includes(key)), key + ' needs a visible mobile label');
  }
  assert.ok(nodes.some(n => n.props['data-label'] === 'harnesses.lifecycle.version'), 'version is labeled on phones');
});

// Exercise the actual browser detector against deterministic DOM geometry.
function geometry({ width = 390, squeezed = false, overflow = false, overlap = false } = {}) {
  const rect = (left, top, w, h) => ({ left, top, width: w, height: h, right: left + w, bottom: top + h });
  const element = (tagName, className, textContent, bounds) => ({ tagName, className, textContent, children: [], parentElement: null,
    clientWidth: bounds.width, scrollWidth: bounds.width, getBoundingClientRect: () => bounds, getClientRects: () => [bounds],
    matches(selector) { return selector.split(',').some(x => x.trim() === tagName.toLowerCase() || x.trim() === '.' + className); },
    querySelectorAll() { return this.children.flatMap(c => [c, ...c.querySelectorAll()]); } });
  const view = element('DIV', 'view', 'Agent', rect(0, 0, width, 400));
  if (overflow) view.scrollWidth = width + 26;
  const head = element('DIV', 'agent-t', 'Agent Used in Loom', rect(16, 20, width - 32, 70));
  const title = element('H2', '', 'Agent', rect(16, 20, 90, 26));
  const pill = element('SPAN', 'pill', 'Used in Loom', rect(overlap ? 50 : 120, 20, squeezed ? 28 : 140, 24));
  const button = element('BUTTON', 'btn', 'New discussion', rect(16, 120, overflow ? width + 10 : width - 32, 32));
  view.children = [head, button]; head.parentElement = view; button.parentElement = view;
  head.children = [title, pill]; title.parentElement = head; pill.parentElement = head;
  const nodes = [title, pill, button].map(parentElement => ({ textContent: parentElement.textContent, parentElement }));
  const dom = { documentElement: { clientWidth: width }, querySelectorAll: () => [view],
    createTreeWalker: el => { const list = nodes.filter(n => n.parentElement === el); return { nextNode: () => list.shift() }; },
    createRange: () => { let node; return { selectNodeContents: n => node = n, getClientRects: () => node.parentElement === pill && squeezed ? [rect(120, 20, 28, 18), rect(120, 38, 25, 18), rect(120, 56, 22, 18)] : [node.parentElement.getBoundingClientRect()] }; } };
  return { document: dom, NodeFilter: { SHOW_TEXT: 4 }, getComputedStyle: () => ({ visibility: 'visible', display: 'flex', position: 'static', overflowX: 'visible' }) };
}
for (const width of [390, 320]) {
  test(`phone layout detector accepts readable controls at ${width}px`, () => {
    const issues = vm.runInNewContext('(' + phoneLayoutIssues + ')()', geometry({ width }));
    assert.equal(issues.length, 0);
  });
  test(`phone layout detector rejects squeezed pill, overlap and oversized button at ${width}px`, () => {
    const issues = vm.runInNewContext('(' + phoneLayoutIssues + ')()', geometry({ width, squeezed: true, overlap: true, overflow: true }));
    for (const expected of ['text column below 40px', 'text paints outside control', 'overlapping header siblings', 'button wider than viewport', 'outside viewport', 'horizontal overflow']) assert.ok(issues.some(s => s.includes(expected)), expected);
  });
}
