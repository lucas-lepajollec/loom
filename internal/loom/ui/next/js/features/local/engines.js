import { html, useEffect, useRef, useState, cls } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { refreshEngineNode, refreshLibrary, refreshStatus, refreshWorkspace } from '../../core/state.js';
import { visibleRefresh } from '../../core/poll.js';
import { Empty } from '../../ui/controls.js';
import { Drawer } from '../../ui/drawer.js';
import { confirm, toast } from '../../ui/dialog.js';
import { Icon } from '../../ui/icons.js';
import { copyText } from '../../ui/clipboard.js';
import { DirectEngineForm, Engine, Job, KIND_LABEL } from '../settings/page.js';
import { useVllm, VllmEngine, VLLMEngine } from '../settings/vllm.js';

const catalogPath = '/api/engines/catalog';
const connectionsPath = '/api/engines/connections';
const filters = ['all', 'installed', 'installable', 'linkable'];
const managedPython = ['vllm', 'sglang', 'mlx'];
const labelFor = (id, fallback) => KIND_LABEL[id] || fallback || id;
const engineCopy = (group, key, vars) => t(['engines', group, key].join('.'), vars);
const failure = error => ({ ok: false, error: error.message });

export async function refreshEngineSelection() {
  await refreshEngineNode();
  await Promise.allSettled([refreshStatus(), refreshLibrary(), refreshWorkspace()]);
}

export function selectLibrary(engine) {
  try { localStorage.setItem('loom.local.engine', engine); } catch (_) {}
}

function PythonManager({ kind }) {
  const v = useVllm(kind);
  const [model, setModel] = useState('');
  const x = v.x;
  if (!x) return html`<div class="skeleton models-loading"></div>`;
  if (kind === 'vllm') return html`<${VLLMEngine} /><${VllmEngine} v=${v} />`;
  const act = action => v.act({ action });
  const installing = x.job === 'install' || x.job === 'update';
  return html`<div class="models-group">
    ${x.requirement_note && html`<p class="note">${engineCopy('requirement', x.requirement_note)}</p>`}
    ${installing && html`<p class="state"><span class="spinner"></span>${t('settings.page.installation_en_cours')}</p>`}
    <${VllmEngine} v=${v} />
    <div class="models-actions engine-manager-actions">
      ${x.installed && !x.running && !x.job && !x.requirement_note && html`<button class="btn" onClick=${() => act('update')}>${t('settings.page.mettre_a_jour')}</button>`}
      ${!x.installed && !x.job && !x.requirement_note && html`<button class="btn primary" onClick=${() => act('install')}>${t('settings.page.installer')}</button>`}
      ${kind === 'sglang' && x.installed && html`<a class="btn" href="#/local" onClick=${() => selectLibrary(kind)}>${t('local.page.bibliotheque')}</a>`}
    </div>
    ${kind === 'mlx' && x.installed && !x.requirement_note && html`<form class="engine-model-form" onSubmit=${e => { e.preventDefault(); if (!x.job && model.trim()) v.start(model.trim()); }}>
      <label class="field"><span>${t('local.page.modele')}</span><input class="input" placeholder="mlx-community/Qwen3-4B-4bit" value=${model}
        onInput=${e => setModel(e.target.value)} /></label>
      <button type="submit" class="btn primary" disabled=${!!x.job || !model.trim()}>${t('local.page.charger')}</button>
    </form>`}
  </div>`;
}

function CatalogManager({ item, onChanged, local }) {
  if (item.id === 'llama.cpp') return html`<${Engine} />`;
  if (item.id === 'ik_llama.cpp') return html`
    <p class="note">${t('engines.ik.preset')}</p>
    ${item.binary && html`<code class="mono engine-wrap">${item.binary}</code>`}
    <a class="btn" href="#/local" onClick=${() => selectLibrary('llama')}>${t('local.page.bibliotheque')}</a>
    <${Job} onDone=${onChanged} />`;
  if (managedPython.includes(item.id) && item.category === 'managed' && local) return html`<${PythonManager} kind=${item.id} />`;
  return html`<div class="models-group">
    ${item.requirement_note && html`<p class="note">${engineCopy('requirement', item.requirement_note)}</p>`}
    ${item.install_hint && html`<p class="note">${engineCopy('install', item.install_hint)}</p>`}
    ${item.install_guide && html`<a class="btn" href=${item.install_guide} target="_blank" rel="noopener noreferrer">${t('engines.install_guide')}</a>`}
    ${item.install_command && html`<pre class="engine-snippet mono">${item.install_command}</pre>
      <button class="btn" onClick=${async () => toast(await copyText(item.install_command) ? t('api.copied', { what: t('engines.install_command') }) : t('settings.page.copie_refusee'))}>${t('local.page.copier')}</button>`}
    ${item.status === 'active' && html`<a class="btn" href="#/local">${t('local.page.bibliotheque')}</a>`}
  </div>`;
}

export function EngineCatalog({ source }) {
  const [catalog, setCatalog] = useState(null);
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState('all');
  const [drawer, setDrawer] = useState(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const request = useRef(0);
  const reload = async () => {
    const seq = ++request.current;
    setLoading(true);
    try {
      const next = await get(catalogPath, { retryAuth: false });
      if (seq === request.current) { setCatalog(next); setError(''); }
    } catch (e) {
      if (seq === request.current) setError(e.message);
    } finally {
      if (seq === request.current) setLoading(false);
    }
  };
  const jobs = catalog?.engines.some(e => e.job);
  useEffect(() => {
    if (jobs) {
      const stop = visibleRefresh(reload, 1800);
      return () => { stop(); request.current++; };
    }
    reload();
    return () => { request.current++; };
  }, [source, jobs]);

  const select = async (action, id) => {
    setBusy(true);
    try {
      const r = await post(connectionsPath, { action, id }).catch(failure);
      if (!r.ok) return toast(r.error || t('local.page.action_impossible'), 'err');
      await Promise.allSettled([refreshEngineSelection(), reload()]);
    } finally { setBusy(false); }
  };
  const install = async item => {
    if (item.install_options.includes('guide')) { setDrawer({ item }); return; }
    const [endpoint, body] = item.id === 'llama.cpp' ? ['/api/llamacpp/prebuilt', {}]
      : item.id === 'ik_llama.cpp' ? ['/api/llamacpp/install-custom', { name: item.custom_name, url: item.source_url }]
      : ['/api/engines/' + item.id, { action: 'install' }];
    setBusy(true);
    try {
      const r = await post(endpoint, body).catch(failure);
      if (!r.ok) return toast(r.error || t('local.page.action_impossible'), 'err');
      setDrawer({ item });
      await reload();
    } finally { setBusy(false); }
  };
  const useBackend = async item => {
    setBusy(true);
    try {
      const r = await post('/api/llamacpp/use', { mode: 'exist', bin: item.binary }).catch(failure);
      if (!r.ok) return toast(r.error || t('local.page.action_impossible'), 'err');
      if (catalog.active_engine?.url) await select('unlink');
      else await Promise.allSettled([refreshEngineSelection(), reload()]);
    } finally { setBusy(false); }
  };
  const installable = item => item.compatible && item.install_options.length > 0;
  const query = search.trim().toLowerCase();
  const list = (catalog?.engines || []).filter(item => {
    if (filter === 'installed' && !item.installed && item.status !== 'active') return false;
    if (filter === 'installable' && (!installable(item) || item.installed)) return false;
    if (filter === 'linkable' && !item.link_options.includes('url')) return false;
    return [item.name, labelFor(item.id), engineCopy('description', item.id), ...item.formats].some(x => x.toLowerCase().includes(query));
  });
  const openLink = item => setDrawer({ item, link: true });
  const current = drawer && (catalog?.engines.find(e => e.id === drawer.item.id) || drawer.item);
  const active = catalog?.active_engine;
  return html`<section class="models-group engine-catalog">
    <div class="models-section-head"><h2>${t('engines.title')}</h2></div>
    ${catalog?.connections.length > 0 && html`<section class="models-card engine-connections" aria-label=${t('engines.connections')}>
      <div class="models-row"><h3>${t('engines.connections')}</h3></div>
      ${catalog.connections.map(p => html`<div class="models-row" key=${p.id}>
        <div class="models-row-main"><b>${p.name || labelFor(p.kind)}</b><small>${[labelFor(p.kind), p.url, p.model].filter(Boolean).join(' · ')}</small></div>
        <div class="models-actions"><button class="btn sm" disabled=${busy} onClick=${() => select(p.active ? 'unlink' : 'activate', p.id)}>
          ${p.active ? t('engines.unlink') : t('engines.activate')}</button>
          <button class="btn sm" disabled=${busy} onClick=${async () => {
            if (await confirm(t('engines.connection.delete'), p.name, { ok: t('settings.page.retirer'), danger: true })) select('delete', p.id);
          }}>${t('settings.page.retirer')}</button></div>
      </div>`)}
    </section>`}
    <div class="models-toolbar engine-catalog-search">
      <label class="search"><${Icon} n="search" /><input aria-label=${t('engines.search')} placeholder=${t('engines.search')} value=${search} onInput=${e => setSearch(e.target.value)} /></label>
      <button class="btn" disabled=${loading} onClick=${reload}><${Icon} n="refresh" />${t('engines.scan')}</button>
    </div>
    <div role="group" aria-label=${t('engines.filters')} class="engine-filter-pills">
      ${filters.map(id => html`<button type="button" class="btn pill" key=${id} aria-pressed=${filter === id}
        onClick=${() => setFilter(id)}>${engineCopy('filter', id)}</button>`)}
    </div>
    ${error && html`<p class="note models-warning" role="status">${t('engines.unavailable')}</p>`}
    ${!catalog && !error ? html`<div class="skeleton models-loading"></div>` : catalog && html`<div class="models-card engine-catalog-card">
      ${!list.length && html`<${Empty} icon="search" title=${t('local.page.aucun_resultat')} />`}
      ${list.map(item => {
        const linked = catalog.connections.find(p => p.kind === item.id && !p.active);
        const tone = { active: 'green', installed: 'green', detected: 'amber', unsupported: 'red' }[item.status];
        const state = item.status === 'detected' ? t('engines.status.detected', { port: item.default_port }) : engineCopy('status', item.status);
        const guided = item.install_options.includes('guide');
        const manage = item.installed || item.status === 'active' || item.link_options.includes('binary') || item.job;
        return html`<article class="models-row engine-catalog-row" key=${item.id}>
          <div class="models-row-main">
            <b>${labelFor(item.id, item.name)}</b><small>${engineCopy('description', item.id)}</small>
            <div class="engine-catalog-tags">
              ${item.hardware_badge && html`<span class="tag green">${engineCopy('hardware', item.hardware_badge)}</span>`}
              ${item.formats.map(format => html`<span class="tag" key=${format}>${format}</span>`)}
              ${item.capabilities.map(cap => html`<span class="tag" key=${cap}>${engineCopy('cap', cap)}</span>`)}</div>
            <small class="engine-catalog-status"><i class=${cls('dot', tone)} aria-hidden="true"></i>${state}${item.version && ' · ' + item.version}
              ${item.job && html`<span class="spinner" aria-label=${t('settings.page.installation_en_cours')}></span>`}</small>
            ${item.requirement_note && html`<small>${engineCopy('requirement', item.requirement_note)}</small>`}
          </div>
          <div class="models-actions">
            ${(guided || installable(item)) && !item.installed && !item.job && html`<button class="btn sm primary" disabled=${busy} onClick=${() => install(item)}>
              ${t(guided ? 'engines.install_guide' : 'settings.page.installer')}</button>`}
            ${item.link_options.includes('url') && html`<button class="btn sm" disabled=${busy} onClick=${() => openLink(item)}>${t('settings.page.lier')}</button>`}
            ${linked && html`<button class="btn sm" disabled=${busy} onClick=${() => select('activate', linked.id)}>${t('engines.activate')}</button>`}
            ${item.binary && !item.job && active?.kind !== item.id && html`<button class="btn sm" disabled=${busy} onClick=${() => useBackend(item)}>${t('engines.activate')}</button>`}
            ${manage && html`<button class="btn sm" onClick=${() => setDrawer({ item })}>${t('engines.configure')}</button>`}
            ${active?.kind === item.id && active.url && html`<button class="btn sm" disabled=${busy} onClick=${() => select('unlink')}>${t('engines.unlink')}</button>`}
          </div>
        </article>`;
      })}
    </div>`}
    ${current && html`<${Drawer} title=${labelFor(current.id, current.name)} onClose=${() => setDrawer(null)}>
      ${drawer.link ? html`<${DirectEngineForm} registry start=${current.detected_url || (current.default_port ? 'http://127.0.0.1:' + current.default_port : '')}
        onDone=${saved => { setDrawer(null); if (saved) reload(); }} />`
        : html`<${CatalogManager} item=${current} onChanged=${reload} local=${!active?.url || !!active.managed || current.installed || !!current.job} />`}
    </${Drawer}>`}
  </section>`;
}
