// Fichiers : le second cerveau, un dossier où travaillent les agents.
// Arborescence à gauche (comme Obsidian), la note au centre, éditée en rendu :
// le Markdown n'apparaît que sur demande. Enregistrement automatique ; si un
// agent a modifié la note entre-temps, Loom le signale au lieu d'écraser.
// Deux portées : le second cerveau (#/files/n/<chemin>) ou toute la machine
// qui fournit les fichiers (#/files/m/<chemin depuis la racine>). La machine
// se choisit ici ; c'est la même brique que dans Machines.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { DropdownButton, Menu, Seg, Empty } from '../../ui/controls.js';
import { prompt, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go } from '../../core/state.js';
import { NoteEditor, isNote } from './editor.js';
import { useMedia } from '../../core/viewport.js';
import { BackButton } from '../../app/phone-nav.js';
import { usePullRefresh } from '../../app/touch.js';
import { ago } from '../agents/rail.js';
const load = (k, d) => { try { return JSON.parse(localStorage.getItem(k)) ?? d; } catch (_) { return d; } };
const save = (k, v) => { try { localStorage.setItem(k, JSON.stringify(v)); } catch (_) {} };
const parentOf = p => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '');
const baseOf = p => p.slice(p.lastIndexOf('/') + 1);
const isImage = p => /\.(png|jpe?g|gif|webp|avif)$/i.test(p);
// Portée courante, partagée par tous les appels de la page.
let scope = 'brain';
export const scopeQuery = (sep = '?') => (scope === 'machine' ? sep + 'scope=machine' : '');
const openPath = p => go('files', scope === 'machine' ? 'm' : 'n', p);

async function op(body) {
  const r = await post('/api/files/op' + scopeQuery(), body);
  if (r.ok === false) toast(r.error || t('files.failed'), 'err');
  return r.ok !== false;
}

function TreeRow({ n, depth, current, open, toggle, refresh, filter }) {
  const [menu, setMenu] = useState(null);
  const isOpen = filter || open.has(n.path);
  const items = [
    ...(n.dir ? [{ label: t('files.new_note'), icon: 'plus', run: () => newNote(n.path, refresh) }, { label: t('files.new_folder'), icon: 'folder', run: () => newFolder(n.path, refresh) }, '-'] : []),
    { label: t('files.rename'), icon: 'edit', run: () => renameEntry(n, refresh, current) },
    { label: t('files.delete'), icon: 'trash', danger: true, run: () => deleteEntry(n, refresh, current) },
  ];
  const activate = () => (n.dir ? toggle(n.path) : openPath(n.path));
  return html`<div class="fl-node">
    <div class=${cls('fl-row', current === n.path && 'on', n.dir && 'dir')} role="treeitem" aria-expanded=${n.dir ? String(isOpen) : undefined} aria-selected=${current === n.path ? 'true' : undefined}
        tabindex="0" style=${`padding-left:${8 + depth * 14}px`} onClick=${activate} onKeyDown=${e => e.key === 'Enter' && activate()}>
      ${n.dir ? html`<${Icon} n=${isOpen ? 'chevron' : 'right'} class="fl-caret" />` : html`<i class="fl-caret"></i>`}
      <span class="fl-name">${n.dir || !/\.md$/i.test(n.name) ? n.name : n.name.replace(/\.md$/i, '')}</span>
      ${n.outside && !(n.dir && isOpen) && html`<i class="fl-dot" title=${t('files.changed_outside')}></i>`}
      <button class="icon-btn fl-more" aria-label=${t('files.actions')} onClick=${e => { e.stopPropagation(); setMenu(e.currentTarget); }}><${Icon} n="more" /></button>
    </div>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}
    ${n.dir && isOpen && (n.children || []).map(c => html`<${TreeRow} key=${c.path} n=${c} depth=${depth + 1} current=${current} open=${open} toggle=${toggle} refresh=${refresh} filter=${filter} />`)}
  </div>`;
}

async function newNote(dir, refresh) {
  const name = await prompt(t('files.new_note'), { placeholder: t('files.note_name'), ok: t('files.create') });
  if (!name) return;
  const file = (dir ? dir + '/' : '') + (/\.[a-z0-9]+$/i.test(name) ? name : name + '.md');
  const r = await post('/api/files/write' + scopeQuery(), { path: file, content: '', create: true });
  if (r.ok === false) return toast(r.error || t('files.failed'), 'err');
  await refresh(); openPath(r.path);
}
async function newFolder(dir, refresh) {
  const name = await prompt(t('files.new_folder'), { placeholder: t('files.folder_name'), ok: t('files.create') });
  if (!name) return;
  if (await op({ op: 'mkdir', path: (dir ? dir + '/' : '') + name })) refresh();
}
async function renameEntry(n, refresh, current) {
  const name = await prompt(t('files.rename'), { value: n.name });
  if (!name || name === n.name) return;
  const to = (parentOf(n.path) ? parentOf(n.path) + '/' : '') + name;
  if (!await op({ op: 'rename', path: n.path, to })) return;
  await refresh();
  if (current === n.path) openPath(to);
  else if (current && current.startsWith(n.path + '/')) openPath(to + current.slice(n.path.length));
}
async function deleteEntry(n, refresh, current) {
  if (!await confirm(t('files.delete'), t('files.delete_text', { name: n.name }), { ok: t('files.delete'), danger: true })) return;
  if (!await op({ op: 'delete', path: n.path })) return;
  await refresh();
  if (current === n.path || (current && current.startsWith(n.path + '/'))) go('files');
}

const filterTree = (nodes, q) => nodes.flatMap(n => {
  if (n.dir) { const kids = filterTree(n.children || [], q); return kids.length ? [{ ...n, children: kids }] : []; }
  return n.name.toLowerCase().includes(q) ? [n] : [];
});

// En-tête commun : la machine qui fournit les fichiers et la portée.
function Source({ scopeNow, onScope, onRoot, refresh, rootLabel, rootPath, actions }) {
  const [machines, setMachines] = useState([]);
  const [roles, setRoles] = useState(null);
  const [menu, setMenu] = useState(null);
  const loadSources = () => Promise.all([get('/api/machines').catch(() => null), get('/api/machines/roles').catch(() => null)])
    .then(([m, r]) => { setMachines(((m && m.machines) || []).filter(x => (x.modules || []).includes('files'))); setRoles(r && r.roles); });
  useEffect(() => { loadSources(); }, []);
  const host = app.get().serverInfo?.hostname || t('app.shell.cette_machine');
  const current = roles?.files || 'local';
  const name = current === 'local' ? host : (machines.find(m => m.id === current) || {}).name || current;
  const use = async id => { const r = await post('/api/machines/roles', { role: 'files', machine: id }); if (r.ok === false) return toast(r.error || t('files.failed'), 'err'); await loadSources(); refresh(); };
  const items = [
    { label: host, icon: current === 'local' ? 'check' : 'server', run: () => use('local') },
    ...machines.map(m => ({ label: m.name, icon: current === m.id ? 'check' : 'server', run: () => use(m.id) })),
    '-',
    ...(scopeNow === 'brain' ? [{ label: t('files.change_root'), icon: 'folder', run: onRoot }] : []),
    { label: t('common.refresh'), icon: 'refresh', run: refresh },
  ];
  return html`<div class="fl-source">
    <div class="fl-tree-h">
      <${DropdownButton} class="sm fit" open=${menu} title=${rootPath} onClick=${e => setMenu(e.currentTarget)}><${Icon} n="server" /><span class="trunc">${name}</span></${DropdownButton}>
      ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}
      <span class="grow"></span>${actions}
    </div>
    <${Seg} class="fl-scope" size="sm" label=${t('files.scope')} value=${scopeNow} onChange=${onScope}
      options=${[{ value: 'brain', label: rootLabel }, { value: 'machine', label: t('files.whole_machine') }]} />
  </div>`;
}

// Toute la machine : chaque dossier se charge quand on l'ouvre.
function LazyRow({ n, depth, current, refreshAll, onSetBrain }) {
  const [open, setOpen] = useState(false);
  const [kids, setKids] = useState(null);
  const [menu, setMenu] = useState(null);
  const expand = async () => {
    if (open) return setOpen(false);
    setOpen(true);
    try { const r = await get('/api/files/list?scope=machine&path=' + encodeURIComponent(n.path)); setKids(r.entries || []); }
    catch (e) { setKids([]); toast(e.message, 'err'); }
  };
  useEffect(() => { if (n.dir && current && current.startsWith(n.path + '/') && !open) expand(); }, [current]);
  const items = n.dir ? [{ label: t('files.use_as_brain'), icon: 'star', run: () => onSetBrain(n.path) }, '-',
    { label: t('files.new_note'), icon: 'plus', run: () => newNote(n.path, refreshAll) }, { label: t('files.new_folder'), icon: 'folder', run: () => newFolder(n.path, refreshAll) }, '-',
    { label: t('files.rename'), icon: 'edit', run: () => renameEntry(n, refreshAll, current) }]
    : [{ label: t('files.rename'), icon: 'edit', run: () => renameEntry(n, refreshAll, current) }, { label: t('files.delete'), icon: 'trash', danger: true, run: () => deleteEntry(n, refreshAll, current) }];
  return html`<div class="fl-node">
    <div class=${cls('fl-row', current === n.path && 'on', n.dir && 'dir')} role="treeitem" aria-expanded=${n.dir ? String(open) : undefined} tabindex="0"
        style=${`padding-left:${8 + depth * 14}px`} onClick=${() => (n.dir ? expand() : openPath(n.path))} onKeyDown=${e => e.key === 'Enter' && (n.dir ? expand() : openPath(n.path))}>
      ${n.dir ? html`<${Icon} n=${open ? 'chevron' : 'right'} class="fl-caret" />` : html`<i class="fl-caret"></i>`}
      <span class="fl-name">${n.name}</span>
      <button class="icon-btn fl-more" aria-label=${t('files.actions')} onClick=${e => { e.stopPropagation(); setMenu(e.currentTarget); }}><${Icon} n="more" /></button>
    </div>
    ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}
    ${open && (kids === null ? html`<div class="fl-row" style=${`padding-left:${22 + depth * 14}px`}><span class="spinner"></span></div>`
      : kids.map(c => html`<${LazyRow} key=${c.path} n=${c} depth=${depth + 1} current=${current} refreshAll=${refreshAll} onSetBrain=${onSetBrain} />`))}
  </div>`;
}

function MachineTree({ current, onSetBrain, header }) {
  const [top, setTop] = useState(null);
  const [rootDir, setRootDir] = useState('/');
  const [stamp, setStamp] = useState(0);
  const load = async () => {
    try { const r = await get('/api/files/list?scope=machine&path=.'); setTop(r.entries || []); setRootDir(r.root || '/'); }
    catch (e) { setTop([]); toast(e.message, 'err'); }
  };
  useEffect(() => { load(); }, [stamp]);
  const refreshAll = async () => setStamp(x => x + 1);
  const pull = usePullRefresh(refreshAll);
  return html`<nav class="fl-tree" aria-label=${t('files.tree')}>
    ${header(null, refreshAll)}
    <div class="fl-list" role="tree" key=${stamp} ...${pull}>
      ${top === null ? html`<span class="spinner"></span>` : top.map(n => html`<${LazyRow} key=${n.path} n=${n} depth=${0} current=${current} refreshAll=${refreshAll} onSetBrain=${p => onSetBrain(rootDir, p)} />`)}
    </div>
    <p class="fl-root-path mono">${rootDir}</p>
  </nav>`;
}

const recentlyModified = (nodes, since) => nodes.flatMap(n => n.dir ? recentlyModified(n.children || [], since) : n.modified > since ? [n] : []);

function Tree({ data, current, refresh, header, phone }) {
  const [open, setOpen] = useState(() => new Set(load('loom.files.open', [])));
  const [q, setQ] = useState('');
  // La note ouverte reste visible : ses dossiers parents s'ouvrent.
  useEffect(() => {
    if (!current) return;
    const n = new Set(open); let p = parentOf(current), changed = false;
    while (p) { if (!n.has(p)) { n.add(p); changed = true; } p = parentOf(p); }
    if (changed) { setOpen(n); save('loom.files.open', [...n]); }
  }, [current]);
  const toggle = p => { const n = new Set(open); n.has(p) ? n.delete(p) : n.add(p); setOpen(n); save('loom.files.open', [...n]); };
  const query = q.trim().toLowerCase();
  const nodes = query ? filterTree(data.tree || [], query) : data.tree || [];
  const recent = phone && !query ? recentlyModified(nodes, Date.now() - 86400000).sort((a, b) => b.modified - a.modified).slice(0, 4) : [];
  const pull = usePullRefresh(refresh);
  return html`<nav class="fl-tree" aria-label=${t('files.tree')}>
    ${header(html`
      <button class="icon-btn" aria-label=${t('files.new_note')} title=${t('files.new_note')} onClick=${() => newNote(current ? parentOf(current) : '', refresh)}><${Icon} n="edit" /></button>
      <button class="icon-btn" aria-label=${t('files.new_folder')} title=${t('files.new_folder')} onClick=${() => newFolder(current ? parentOf(current) : '', refresh)}><${Icon} n="folder" /></button>`)}
    <label class="search fl-search"><${Icon} n="search" /><input placeholder=${t('files.search')} value=${q} onInput=${e => setQ(e.target.value)} aria-label=${t('files.search')} /></label>
    <div class="fl-list" role="tree" ...${pull}>
      ${recent.length > 0 && html`<section class="fl-recent">
        <h2>${t('mobile.files_recent')}</h2>
        ${recent.map(n => html`<a class="phone-link" key=${n.path} href=${'#/files/n/' + encodeURIComponent(n.path)}>
          <${Icon} n="file" /><span><b class="trunc">${n.name.replace(/\.md$/i, '')}</b><small>${[parentOf(n.path), ago(n.modified)].filter(Boolean).join(' · ')}</small></span><${Icon} n="right" />
        </a>`)}
        <h2>${t('files.tree')}</h2>
      </section>`}
      ${nodes.map(n => html`<${TreeRow} key=${n.path} n=${n} depth=${0} current=${current} open=${open} toggle=${toggle} refresh=${refresh} filter=${!!query} />`)}
      ${!nodes.length && html`<p class="fl-empty">${query ? t('files.no_match') : t('files.empty_folder')}</p>`}
    </div>
    <p class="fl-root-path mono" title=${data.root}>${data.root}</p>
  </nav>`;
}

// Choisir ou créer le dossier du second cerveau.
function RootSetup({ data, done }) {
  const [path, setPath] = useState(data.root || '');
  const [busy, setBusy] = useState(false);
  const apply = async create => {
    setBusy(true);
    const r = await post('/api/files/root', { path, create });
    setBusy(false);
    if (r.ok === false) return toast(r.error || t('files.failed'), 'err');
    done();
  };
  return html`<div class="fl-setup"><${Empty} icon="folder" title=${t('files.setup_title')} text=${t('files.setup_text')}>
    <label class="fl-setup-in"><span class="sr">${t('files.root')}</span><input class="input mono" value=${path} onInput=${e => setPath(e.target.value)} /></label>
    <div class="fl-setup-acts"><button class="btn" disabled=${busy} onClick=${() => apply(false)}>${t('files.use_folder')}</button><button class="btn primary" disabled=${busy} onClick=${() => apply(true)}>${t('files.create_folder')}</button></div>
  </${Empty}></div>`;
}

function Viewer({ path, refresh, narrow }) {
  const [file, setFile] = useState(null);
  const [raw, setRaw] = useState(false);
  const [state, setState] = useState('saved');
  const [menu, setMenu] = useState(null);
  const reload = async () => {
    try {
      const r = await get('/api/files/read?path=' + encodeURIComponent(path) + scopeQuery('&'));
      setFile(r.ok === false ? { error: r.error || t('files.failed') } : r);
    } catch (e) { setFile({ error: e.message || t('files.failed') }); }
    setState('saved');
  };
  useEffect(() => { setFile(null); setRaw(false); reload(); }, [path]);
  const crumbs = path.split('/');
  const items = [
    ...(file && typeof file.content === 'string' && isNote(path) ? [{ label: raw ? t('files.view_rendered') : t('files.view_markdown'), icon: 'file', run: () => setRaw(!raw) }] : []),
    { label: t('files.rename'), icon: 'edit', run: () => renameEntry({ path, name: baseOf(path) }, refresh, path) },
    '-',
    { label: t('files.delete'), icon: 'trash', danger: true, run: () => deleteEntry({ path, name: baseOf(path) }, refresh, path) },
  ];
  return html`<section class="fl-view" aria-label=${path}>
    <header class="fl-head">
      ${narrow && html`<${BackButton} />`}
      <span class="fl-crumbs">${crumbs.map((c, i) => html`${i > 0 && html`<i>/</i>`}<span class=${i === crumbs.length - 1 ? 'here' : ''}>${i === crumbs.length - 1 ? c.replace(/\.md$/i, '') : c}</span>`)}</span>
      <span class="grow"></span>
      <span class=${cls('fl-state', state)}>${state === 'saving' ? t('files.saving') : state === 'dirty' ? t('files.unsaved') : state === 'conflict' ? t('files.conflict_short') : file && file.modified ? t('files.saved') : ''}</span>
      <button class="icon-btn" aria-label=${t('files.actions')} onClick=${e => setMenu(e.currentTarget)}><${Icon} n="more" /></button>
      ${menu && html`<${Menu} anchor=${menu} onClose=${() => setMenu(null)} items=${items} />`}
    </header>
    ${!file ? html`<div class="fl-body"><div class="skeleton" style="height:28px;width:50%"></div><div class="skeleton" style="height:16px;margin-top:18px"></div></div>`
      : file.error ? html`<${Empty} icon="alert" title=${t('files.failed')} text=${file.error} />`
      : isImage(path) ? html`<div class="fl-body fl-image"><img src=${'/api/files/raw?path=' + encodeURIComponent(path) + scopeQuery('&')} alt=${baseOf(path)} /></div>`
      : file.binary || file.too_large ? html`<${Empty} icon="file" title=${file.too_large ? t('files.too_large') : t('files.binary')} />`
      : html`<${NoteEditor} key=${path + (raw ? ':raw' : '')} path=${path} file=${file} raw=${raw || !isNote(path)} onState=${setState} onSaved=${m => { setFile(f => ({ ...f, modified: m })); refresh(); }} onReload=${reload} narrow=${narrow} />`}
  </section>`;
}

export function FilesPage({ route }) {
  const narrow = useMedia('(max-width: 859px)');
  const [data, setData] = useState(null);
  const [setup, setSetup] = useState(false);
  const scopeNow = route.sub === 'm' ? 'machine' : route.sub === 'n' ? 'brain' : load('loom.files.scope', 'brain');
  scope = scopeNow;
  const current = route.sub === 'n' || route.sub === 'm' ? route.id || '' : '';
  const [failed, setFailed] = useState('');
  // Une lecture refusée (serveur injoignable, session expirée) laisse
  // l'arborescence affichée et réessaie au prochain passage.
  const refresh = async () => {
    try { const r = await get('/api/files/tree'); if (r && r.ok !== false) { setData(r); setFailed(''); } else setFailed((r && r.error) || t('files.failed')); return r; }
    catch (e) { setFailed(e.message || t('files.failed')); return null; }
  };
  useEffect(() => { refresh(); const timer = setInterval(() => { if (!document.hidden) refresh(); }, 15000); return () => clearInterval(timer); }, []);
  useEffect(() => {
    save('loom.files.scope', scopeNow);
    const key = 'loom.files.last.' + scopeNow;
    if (current) save(key, current); else if (!narrow) { const last = load(key, ''); if (last) openPath(last); }
  }, [current, scopeNow]);
  const setScope = v => { save('loom.files.scope', v); scope = v; const last = narrow ? '' : load('loom.files.last.' + v, ''); go('files', v === 'machine' ? 'm' : 'n', last || undefined); };
  const setBrain = async (rootDir, rel) => {
    const path = (rootDir.replace(/\/+$/, '') + '/' + rel).replace(/^\/\//, '/');
    const r = await post('/api/files/root', { path });
    if (r.ok === false) return toast(r.error || t('files.failed'), 'err');
    toast(t('files.brain_set', { path })); await refresh(); setScope('brain');
  };
  const phoneHeader = narrow && html`<header class="fl-head phone-subhead"><h1>${t('files.title')}</h1></header>`;
  if (!data) return html`<div class=${cls('view files', narrow && 'narrow')}>${phoneHeader}<div class="fl-body">${failed ? html`<${Empty} icon="alert" title=${t('files.failed')} text=${failed}><button class="btn" onClick=${refresh}>${t('common.refresh')}</button></${Empty}>` : html`<span class="spinner"></span>`}</div></div>`;
  const header = (actions, reload = refresh) => html`<${Source} scopeNow=${scopeNow} onScope=${setScope} onRoot=${() => setSetup(true)} refresh=${narrow ? reload : refresh} rootLabel=${t('files.second_brain')} rootPath=${data.root} actions=${actions} />`;
  if (scopeNow === 'brain' && (!data.exists || setup)) return html`<div class=${cls('view files', narrow && 'narrow')}>${phoneHeader}<${RootSetup} data=${data} done=${async () => { setSetup(false); await refresh(); }} /></div>`;
  const showTree = !narrow || !current;
  return html`<div class=${cls('view files', narrow && 'narrow')}>
    ${!current && phoneHeader}
    ${showTree && (scopeNow === 'machine' ? html`<${MachineTree} key=${'m'} current=${current} onSetBrain=${setBrain} header=${header} />`
      : html`<${Tree} data=${data} current=${current} refresh=${refresh} header=${header} phone=${narrow} />`)}
    ${current ? html`<${Viewer} key=${scopeNow + current} path=${current} refresh=${refresh} narrow=${narrow} />`
      : !narrow && html`<section class="fl-view"><${Empty} icon="file" title=${t('files.pick')} text=${t('files.pick_text')} /></section>`}
  </div>`;
}
