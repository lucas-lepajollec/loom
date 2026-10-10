// Subprocess fixture: observe real promise rejection handling without depending
// on sockets, a browser, native harness accounts or model calls.
import fs from 'node:fs';
import vm from 'node:vm';
import htm from '../next/vendor/htm.mjs';
const root = process.env.LOOM_PAGES_BASELINE ? new URL('file://' + process.env.LOOM_PAGES_BASELINE + '/') : new URL('../next/js/', import.meta.url);
const failures = [];
process.on('unhandledRejection', e => failures.push(e.message));
const cases = [
  ['features/settings/page.js', ['General', 'Job', 'Engine', 'Internet', 'About']],
  ['features/resources/mcp.js', ['Mcp']],
  ['features/harnesses/machines.js', ['MachineDialog']],
  ['features/local/hub.js', ['Detail']],
  ['features/inspector/context.js', ['Preview']],
];
for (const [file, components] of cases) {
  const source = fs.readFileSync(new URL(file, root), 'utf8');
  for (const name of components) {
    const effects = [], state = { theme: 'dark', workspace: null, status: null, serverInfo: null, engineNode: null, models: [], presets: [] };
    const env = {};
    for (const match of source.matchAll(/^import \{([^}]+)\}/gm)) for (const symbol of match[1].split(',')) env[symbol.trim()] = symbol.trim();
    Object.assign(env, {
      t: key => key, getLang: () => 'en', html: htm.bind(() => null), cls: () => '',
      app: { get: () => state }, useStore: (_s, select) => select(state),
      chat: { get: () => ({ session: { id: 'fixture' } }) },
      useState: v => [typeof v === 'function' ? v() : v, () => {}], useRef: v => ({ current: v }), useMemo: f => f(), useEffect: f => effects.push(f),
      get: async () => { throw new Error(`${name}: offline`); }, post: async () => ({ ok: false, error: 'offline' }),
      setTimeout: () => 1, clearTimeout() {}, setInterval: () => 1, clearInterval() {}, toast() {},
      refreshLibrary: async () => {}, useVisibleRefresh() {}, localStorage: { getItem: () => null },
      location: { protocol: 'http:', host: 'fixture' }, navigator: {}, innerWidth: 390,
    });
    vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.view = ' + name + ';', env);
    env.view({ id: 'fixture/model', onBack() {}, onClose() {} });
    effects.forEach(f => f());
  }
}
await new Promise(r => setImmediate(r));
if (failures.length) { console.error(failures.join('\n')); process.exitCode = 1; }
