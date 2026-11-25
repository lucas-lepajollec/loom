import { t, getLang } from '../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, useMemo } from '../core/lib.js';
import { get } from '../core/api.js';
import { Icon } from '../ui/icons.js';
import { Logo } from '../ui/logo.js';
import { Modal } from '../ui/dialog.js';
import { app, go, setTheme } from '../core/state.js';
import { chooseLocal, chooseRemote } from '../features/chat/engine.js';
import { vendorOf } from '../features/chat/picker.js';
import { NAV_ITEMS } from './routes.js';
import { usePhone } from '../core/viewport.js';
import { agents, cliOf } from '../features/agents/store.js';

const norm = s => String(s || '').toLowerCase().normalize('NFD').replace(/\p{M}/gu, '');
const route = (section, ...parts) => '#/' + [section, ...parts].map(encodeURIComponent).join('/');
export const openPalette = () => app.set({ palette: true });

function filesFrom(nodes) {
  const files = [];
  // Only names and paths from the existing browser tree are used for search.
  for (const n of Array.isArray(nodes) ? nodes : []) {
    if (!n || typeof n.path !== 'string' || typeof n.name !== 'string') continue;
    if (n.dir) files.push(...filesFrom(n.children));
    else files.push({ icon: 'file', title: n.name, sub: n.path.split('/').slice(0, -1).join('/'), href: route('files', 'n', n.path) });
  }
  return files;
}

function visibleMessages() {
  return [...document.querySelectorAll('.main .thread [data-palette-message]')]
    .filter(node => node.getClientRects().length && node.textContent.trim())
    .map(node => ({
      icon: 'chat', title: node.textContent.trim().replace(/\s+/g, ' ').slice(0, 350),
      run: () => requestAnimationFrame(() => {
        if (!node.isConnected) return;
        node.scrollIntoView({ block: 'center' });
        node.focus({ preventScroll: true });
      }),
    })).reverse();
}

function entries(s, sessions, files, messages) {
  const byGroup = new Map();
  const add = (group, rows) => {
    if (rows.length) byGroup.set(group, rows);
  };
  add(t('mobile.sessions'), [
    ...s.nav.conversations.map(c => ({ icon: 'chat', title: c.title || t('app.palette.nouvelle_discussion'), href: route('chat', c.id) })),
    ...Object.values(sessions).map(session => ({
      icon: 'agent', title: session.name || session.id, sub: cliOf(session.mode).label, href: route('agents', 's', session.id),
    })),
  ]);
  add(t('files.title'), files);
  add(t('noir.palette_messages'), messages);
  add(t('app.shell.actions'), [
    { icon: 'plus', title: t('side.new_session'), hint: t('app.palette.ctrl_maj_o'), run: () => agents.set({ creating: { type: 'chat' } }) },
    { icon: 'terminal', title: t('noir.palette_terminal'), href: '#/terminals' },
    { icon: s.theme === 'light' ? 'moon' : 'sun', title: s.theme === 'light' ? t('app.palette.passer_en_theme_sombre') : t('app.palette.passer_en_theme_clair'), run: () => setTheme(s.theme === 'light' ? 'dark' : 'light') },
  ]);
  add(t('app.palette.pages'), [
    ...NAV_ITEMS.map(r => ({ icon: r.nav.icon, title: r.nav.label, href: r.href })),
    { icon: 'inbox', title: t('mobile.inbox'), href: '#/inbox' },
    { icon: 'gear', title: t('app.palette.reglages'), href: '#/settings' },
  ]);
  add(t('app.palette.modeles'), [
    ...(s.presets || []).map((p, i) => ({ logo: vendorOf(p.model || p.name), title: p.name, sub: t('app.palette.preset_local'), run: () => { go('chat'); chooseLocal({ presetIndex: i + 1, name: p.name, model: p.model }); } })),
    ...(s.models || []).filter(m => !/mmproj/i.test(m.name)).map(m => ({
      logo: vendorOf(m.name), title: m.name.replace(/\.gguf$/i, ''), sub: t('chat.picker.local'),
      run: () => { go('chat'); chooseLocal({ model: m.value || m.path, name: m.name }); },
    })),
    ...(s.workspace?.models || []).filter(m => m.kind !== 'local' && m.enabled).map(m => ({
      logo: m.kind === 'cloud' ? m.provider_name : m.runtime_id, title: m.name, sub: m.provider_name,
      run: () => { go('chat'); chooseRemote(m); },
    })),
  ]);
  return [...byGroup].flatMap(([group, rows]) => rows.map(row => ({ group, ...row })));
}

function highlight(value, words) {
  return value.split(/([\p{L}\p{N}]+)/u).map((part, i) =>
    /^\p{L}|^\p{N}/u.test(part) && words.some(word => norm(part).includes(word)) ? html`<mark key=${i}>${part}</mark>` : part);
}

export function Palette() {
  const phone = usePhone();
  const s = useStore(app, a => ({ on: a.palette, nav: a.nav, presets: a.presets, models: a.models, workspace: a.workspace, theme: a.theme }));
  const sessions = useStore(agents, a => s.on ? a.sessions : null);
  const [q, setQ] = useState('');
  const [i, setI] = useState(0);
  const [files, setFiles] = useState([]);
  const [messages, setMessages] = useState([]);
  const list = useRef();
  const searchFiles = s.on && q.trim().length >= 2;
  useEffect(() => {
    if (s.on) setMessages(visibleMessages());
    else { setQ(''); setI(0); setFiles([]); setMessages([]); }
  }, [s.on]);
  useEffect(() => {
    if (!searchFiles) return;
    const controller = new AbortController();
    get('/api/files/tree', { signal: controller.signal, retryAuth: false, timeout: 6000 }).then(data => {
      if (!controller.signal.aborted) setFiles(filesFrom(data?.tree));
    }).catch(() => {});
    return () => controller.abort();
  }, [searchFiles]);
  const all = useMemo(() => s.on ? entries(s, sessions || {}, files, messages) : [],
    [s.on, s.nav, s.presets, s.models, s.workspace, s.theme, sessions, files, messages, getLang()]);
  const words = norm(q).split(/\s+/).filter(Boolean);
  const shown = words.length
    ? all.filter(e => words.every(w => norm([e.title, e.sub, e.group].join(' ')).includes(w))).slice(0, 40)
    : all.filter(e => e.group === t('app.shell.actions') || e.group === t('app.palette.pages')).concat(all.filter(e => e.group === t('mobile.sessions')).slice(0, 6));
  const index = Math.min(i, Math.max(0, shown.length - 1));
  useEffect(() => { list.current?.querySelector('[aria-selected=true]')?.scrollIntoView({ block: 'nearest' }); }, [index]);
  if (!s.on) return null;
  const close = () => app.set({ palette: false });
  const run = (e, newTab) => {
    close();
    if (e.href) {
      if (newTab) window.open(e.href, '_blank', 'noopener');
      else location.hash = e.href;
    } else e.run();
  };
  const key = ev => {
    if (ev.key === 'ArrowDown') { ev.preventDefault(); setI(Math.max(0, Math.min(shown.length - 1, index + 1))); }
    else if (ev.key === 'ArrowUp') { ev.preventDefault(); setI(Math.max(0, index - 1)); }
    else if (ev.key === 'Enter' && shown[index]) { ev.preventDefault(); run(shown[index], ev.metaKey || ev.ctrlKey); }
  };
  let last = '';
  return html`<${Modal} class="palette noir-palette" title=${t('app.palette.rechercher')} onClose=${close}
    foot=${html`<div class="palette-hints"><span><kbd>↑</kbd><kbd>↓</kbd> ${t('app.palette.naviguer')}</span>
      <span><kbd>↵</kbd> ${t('app.palette.ouvrir')}</span>
      ${shown[index]?.href && html`<span><kbd>${/Mac/.test(navigator.platform) ? '⌘' : 'Ctrl'} ↵</kbd> ${t('noir.palette_new_tab')}</span>`}</div>`}>
    <label class="search"><${Icon} n="search" /><input aria-label=${t('app.palette.rechercher')} placeholder=${t('noir.palette_placeholder')} value=${q}
      onInput=${e => { setQ(e.target.value); setI(0); }} onKeyDown=${key} role="combobox" aria-expanded="true" aria-controls="palette-list"
      aria-activedescendant=${shown[index] ? 'palette-option-' + index : undefined} /></label>
    <div class="palette-list" id="palette-list" role="listbox" ref=${list}>
      ${shown.length ? shown.map((e, n) => {
        const head = e.group !== last ? (last = e.group, html`<div class="item-group">${e.group}</div>`) : '';
        return html`${head}<button type="button" id=${'palette-option-' + n} role="option" tabIndex="-1"
          aria-selected=${String(n === index)} class="item palette-row" onMouseMove=${() => n !== index && setI(n)} onClick=${ev => run(e, ev.metaKey || ev.ctrlKey)}>
          ${e.logo ? html`<${Logo} name=${e.logo} size="xs" />` : html`<${Icon} n=${e.icon} />`}
          <span class="palette-copy"><span class="t">${highlight(e.title, words)}</span>${e.sub && html`<small>${e.sub}</small>`}</span>
          ${!phone && (e.hint || n === index) && html`<kbd>${e.hint || '↵'}</kbd>`}</button>`;
      }) : html`<div class="palette-empty">${t('app.palette.aucun_resultat_pour')} ${q} ».</div>`}
    </div>
  </${Modal}>`;
}
