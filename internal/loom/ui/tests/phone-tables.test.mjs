import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import htm from '../next/vendor/htm.mjs';

const flat = node => Array.isArray(node) ? node.flatMap(flat) : node && typeof node === 'object' ? [node, ...(node.children || []).flatMap(flat)] : [];
function render(file, name, states, overrides = {}) {
  const source = fs.readFileSync(process.env.LOOM_TABLES_BASELINE ? new URL(file, 'file://' + process.env.LOOM_TABLES_BASELINE + '/') : new URL('../next/js/features/' + file, import.meta.url), 'utf8');
  const env = {};
  for (const match of source.matchAll(/^import \{([^}]+)\}/gm)) for (const symbol of match[1].split(',')) { const local = symbol.trim().split(/\s+as\s+/).pop(); env[local] = local; }
  Object.assign(env, { html: htm.bind((tag, props, ...children) => ({ tag, props: props || {}, children: children.flat(Infinity) })), t: key => key,
    useState: initial => [states.length ? states.shift() : typeof initial === 'function' ? initial() : initial, () => {}], useEffect() {}, useRef: value => ({ current: value }), cls: (...args) => args.filter(Boolean).join(' '), ...overrides });
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.view = ' + name + ';', env);
  return flat(env.view({ machines: [], machine: { id: 'synthetic', name: 'Synthetic' } }));
}

test('local model metrics retain their labels when the desktop table header is hidden', () => {
  const nodes = render('local/page.js', 'Library', [], { useStore: (_s, select) => select({ models: [{ name: 'synthetic-Q4_K_M.gguf', path: '/synthetic/model.gguf', dir: '/synthetic', size: 1024 }], presets: [], status: null }),
    app: { get: () => ({}) }, baseName: s => s, fmtBytes: n => n + ' B', vendorOf: () => 'synthetic', inspectTrigger: () => ({}) });
  for (const label of ['local.page.quant', 'local.page.fichier', 'local.page.memoire_estimee']) assert.ok(nodes.some(n => n.props['data-label'] === label), label + ' needs a metric label');
});

test('service cells retain service and machine labels when stacked', () => {
  const nodes = render('environment/page.js', 'Services', [[{ id: 'fixture', name: 'Synthetic service', url: 'http://192.0.2.10:3000', machine: 'local' }], null, false, null]);
  for (const label of ['environment.page.service', 'environment.page.sur']) assert.ok(nodes.some(n => n.tag === 'td' && n.props['data-label'] === label), label + ' needs a cell label');
});

test('Docker cells retain metric labels when stacked', () => {
  const nodes = render('environment/page.js', 'DockerMachine', [{ enabled: true, containers: [{ name: 'synthetic', image: 'registry.test/image:tag', state: 'running', status: 'Up', ports: '3000/tcp' }] }], { /* supply machine through wrapper below */ });
  for (const label of ['environment.page.conteneur', 'environment.page.image', 'environment.page.etat', 'environment.page.ports']) assert.ok(nodes.some(n => n.tag === 'td' && n.props['data-label'] === label), label + ' needs a cell label');
});

test('Proxmox cells retain metric labels when stacked', () => {
  const guests = render('environment/page.js', 'Proxmox', [{ enabled: true }, { resources: [{ vmid: 100, name: 'synthetic', type: 'qemu', node: 'synthetic-node', status: 'running', mem: 1024, maxmem: 2048 }] }, false]);
  for (const label of ['ID', 'environment.page.nom', 'environment.page.type', 'environment.page.n_ud', 'environment.page.etat', 'environment.page.memoire']) assert.ok(guests.some(n => n.tag === 'td' && n.props['data-label'] === label), label + ' needs a cell label');
});
