import { t, getLang } from '../../core/i18n.js';
// Hub : catalogue GGUF de Hugging Face. Recherche, fiche modèle, quantisations
// avec compatibilité VRAM, téléchargements suivis, README.
import { ListPick } from '../../ui/listpick.js';
import { html, useState, useEffect, useMemo, useStore, cls, fmtBytes } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg, Empty } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, refreshLibrary } from '../../core/state.js';
import { md } from '../chat/md.js';

const fmtN = n => n >= 1e6 ? (n / 1e6).toFixed(1).replace(/\.0$/, '') + ' M' : n >= 1e3 ? (n / 1e3).toFixed(1).replace(/\.0$/, '') + ' k' : String(n || 0);
const PIPES = () => [
  { value: '', label: t('local.hub.tous') },
  { value: 'text-generation', label: t('models.hub.llm') },
  { value: 'image-text-to-text', label: t('inspector.catalog.vision.label') },
  { value: 'feature-extraction', label: t('models.hub.embeddings') },
];
const kind = p => p === 'image-text-to-text' || p === 'image-to-text' ? t('inspector.catalog.vision.label')
  : p === 'feature-extraction' || p === 'sentence-similarity' ? t('models.hub.embeddings') : t('models.hub.llm');

// Surveillance partagée des téléchargements en cours.
const dls = { map: {}, subs: new Set(), timer: null };
function watch() {
  if (dls.timer) return;
  dls.timer = setInterval(async () => {
    let list = [];
    try { list = (await get('/api/models/download/status')) || []; } catch (_) { return; }
    const next = {};
    for (const d of list) {
      if (d.error && !(dls.map[d.filename] || {}).error) toast(d.filename + ' : ' + d.error, 'err');
      if (d.finished && !(dls.map[d.filename] || {}).finished) { toast(d.filename + t("local.hub.telecharge")); refreshLibrary(); }
      next[d.filename] = d;
    }
    dls.map = next; dls.subs.forEach(f => f());
    if (!list.some(d => !d.finished && !d.canceled && !d.error)) { clearInterval(dls.timer); dls.timer = null; }
  }, 900);
}
function useDownloads() { const [, localT] = useState(0); useEffect(() => { const f = () => localT(x => x + 1); dls.subs.add(f); watch(); return () => dls.subs.delete(f); }, []); return dls.map; }

function Result({ m, onOpen }) {
  const [a, n] = m.id.split('/');
  const metadata = [m.author || (n ? a : ''), kind(m.pipeline), m.params_b && m.params_b + t('local.hub.b')].filter(Boolean);
  return html`<button class="models-row models-hub-result" onClick=${() => onOpen(m.id)}>
    <img class="avatar" src=${'/api/hub/avatar?a=' + encodeURIComponent(m.author || a)} alt="" loading="lazy" onError=${e => { e.target.style.visibility = 'hidden'; }} />
    <span class="models-row-main"><b>${n || m.id}</b><small>${metadata.join(' · ')}</small></span>
    <span class="models-hub-stats models-row-meta">
      ${Number.isFinite(m.downloads) && html`<span aria-label=${t('models.hub.downloads', { n: m.downloads })}><${Icon} n="download" />${fmtN(m.downloads)}</span>`}
      ${Number.isFinite(m.likes) && html`<span aria-label=${t('models.hub.likes', { n: m.likes })}><${Icon} n="heart" />${fmtN(m.likes)}</span>`}
    </span><${Icon} n="right" class="models-row-meta" />
  </button>`;
}

function Detail({ id, onBack }) {
  const [d, setD] = useState(null);
  const [showAll, setShowAll] = useState(false);
  const models = useStore(app, s => s.models);
  const downloads = useDownloads();
  useEffect(() => {
    let active = true;
    get('/api/hub/model?id=' + encodeURIComponent(id)).then(result => { if (active) setD(result); }).catch(() => {
      if (!active) return;
      toast(t('local.hub.fiche_indisponible'), 'err');
      onBack();
    });
    return () => { active = false; };
  }, [id]);
  const local = new Set((models || []).map(m => m.name.toLowerCase()));
  const readme = useMemo(() => d && d.readme ? md(d.readme.replace(/^---[\s\S]*?---\n/, '')) : '', [d, getLang()]);
  if (!d) return html`<div class="models-group"><div class="skeleton models-loading"></div></div>`;
  const vram = d.vram_total_mb || 0;
  const files = (d.files || []).filter(f => !/^mmproj/i.test(f.name) && !/^mtp-/i.test(f.name)).sort((x, y) => x.size - y.size);
  const fit = f => { const need = f.size / 1048576 * 1.12; return !vram ? '' : need <= vram * 0.9 ? 'fits' : need <= vram * 1.05 ? 'tight' : 'over'; };
  const best = files.filter(f => fit(f) === 'fits').at(-1);
  const shown = showAll ? files : files.filter(f => fit(f) !== 'over' || f === files[0]).slice(-8);
  const start = async f => {
    const p = await post('/api/models/download/probe', { url: f.url, dir: '' });
    if (!p.ok || !p.enough) { toast(p.error || t("local.hub.espace_disque_insuffisant"), 'err'); return; }
    const r = await post('/api/models/download', { url: f.url, dir: '' });
    if (!r.ok) { toast(r.error || t("local.hub.telechargement_impossible"), 'err'); return; }
    dls.map[r.filename || f.name] = { filename: r.filename || f.name, done: 0, total: f.size }; dls.subs.forEach(x => x()); watch();
  };
  const cancel = async name => { await post('/api/models/download/cancel', { filename: name }); };
  return html`
    <div class="models-card models-row">
      <img class="avatar lg" src=${'/api/hub/avatar?a=' + encodeURIComponent(d.author)} alt="" onError=${e => { e.target.style.visibility = 'hidden'; }} />
      <div class="models-row-main"><b>${id.split('/').at(-1)}</b><small>${[
        d.author, Number.isFinite(d.downloads) && t('models.hub.downloads', { n: fmtN(d.downloads) }),
        Number.isFinite(d.likes) && t('models.hub.likes', { n: fmtN(d.likes) }), d.license,
      ].filter(Boolean).join(' · ')}</small></div>
      <a class="btn sm" href=${'https://huggingface.co/' + id} target="_blank" rel="noopener noreferrer">${t('local.hub.hugging_face')}<${Icon} n="right" /></a>
    </div>
    <section class="models-group">
      <div class="models-section-head"><h2>${t('local.hub.quantisations')}</h2>${vram > 0 && html`<small>${t('local.hub.ta_vram') + (vram / 1024).toFixed(1) + t('environment.page.go')}</small>`}</div>
      <div class="models-card">${shown.map(f => {
        const dl = downloads[f.name], have = local.has(f.name.toLowerCase()), ft = fit(f);
        const pct = dl && dl.total ? Math.min(100, Math.round(dl.done * 100 / dl.total)) : 0;
        const details = [
          fmtBytes(f.size), f.parts > 1 && f.parts + ' ' + t('local.hub.parts'),
          ft === 'fits' ? t('local.hub.tient_en_vram') : ft === 'tight' ? t('local.hub.juste') : ft === 'over' ? t('local.hub.deborde_en_ram') : '',
        ].filter(Boolean);
        return html`<div class="models-row" key=${f.name}>
          <div class="models-row-main">
            <b class="mono">${f.quant || f.name} ${f === best && html`<span class="tag">${t('local.hub.recommande')}</span>`}</b>
            <small class=${cls(ft === 'over' && 'models-warning')}>${details.join(' · ')}</small>
          </div>
          <div class="models-actions">${have ? html`<span class="models-good">${t('local.hub.installe')}</span>`
            : dl && !dl.finished && !dl.canceled && !dl.error ? html`<div class="models-download">
                <div class="gauge" role="progressbar" aria-label=${f.name} aria-valuemin="0" aria-valuemax="100" aria-valuenow=${pct}><i style=${{ width: pct + '%' }}></i></div>
                <span class="num">${pct} %</span>
                <button class="icon-btn" aria-label=${t('local.hub.annuler')} onClick=${() => cancel(f.name)}><${Icon} n="close" /></button>
              </div>`
              : html`<button class="btn sm" onClick=${() => start(f)}>${t('local.hub.telecharger')}</button>`}</div>
        </div>`;
      })}
      ${!files.length && html`<div class="models-row models-empty">${t('local.hub.aucun_modele_gguf')}</div>`}
      ${files.length > shown.length && html`<div class="models-row"><button class="btn sm" onClick=${() => setShowAll(true)}>${t('models.hub.all_quants', { n: files.length })}</button></div>`}
      </div>
    </section>
    ${readme && html`<div class="card models-card pad readme md" dangerouslySetInnerHTML=${{ __html: readme }}></div>`}`;
}

export function Hub({ enginePicker, initialQuery = '' }) {
  const [q, setQ] = useState(initialQuery);
  const [sort, setSort] = useState('downloads');
  const [pipe, setPipe] = useState('');
  const [list, setList] = useState(null);
  const [err, setErr] = useState('');
  const [open, setOpen] = useState('');
  useEffect(() => {
    setList(null);
    setErr('');
    const controller = new AbortController();
    const timer = setTimeout(async () => {
      const p = new URLSearchParams({ q, sort, offset: '0', limit: '36' });
      if (pipe) p.set('pipeline', pipe);
      try {
        const result = await get('/api/hub/search?' + p, { signal: controller.signal });
        if (!controller.signal.aborted) setList(result.models || []);
      } catch (e) { if (!controller.signal.aborted) setErr(e.message); }
    }, 260);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [q, sort, pipe]);
  if (open) return html`
    <div class="models-toolbar"><button class="btn sm" onClick=${() => setOpen('')}><${Icon} n="left" />${t('local.hub.catalogue')}</button>${enginePicker}</div>
    <${Detail} key=${open} id=${open} onBack=${() => setOpen('')} />`;
  const label = t('local.hub.rechercher_un_modele_gguf_sur_hugging_face');
  return html`
    <div class="models-toolbar"><label class="search"><${Icon} n="search" /><input aria-label=${label} placeholder=${label} value=${q} onInput=${e => setQ(e.target.value)} /></label>
      ${enginePicker}
      <${Seg} size="sm" value=${pipe} onChange=${setPipe} label=${t('local.hub.type')} options=${PIPES()} />
      <${ListPick} value=${sort} onChange=${setSort} label=${t('local.hub.tri')} options=${[
        { value: 'downloads', label: t('local.hub.plus_telecharges') }, { value: 'likes', label: t('local.hub.plus_aimes') },
        { value: 'lastModified', label: t('local.hub.recents') }]} />
    </div>
    ${err ? html`<div class="models-card"><${Empty} icon="globe" title=${t('local.hub.hugging_face_injoignable')} text=${err} /></div>`
      : list === null ? html`<div class="skeleton models-loading"></div>`
        : list.length ? html`<div class="models-card">${list.map(m => html`<${Result} key=${m.id} m=${m} onOpen=${setOpen} />`)}</div>`
          : html`<div class="models-card"><${Empty} icon="search" title=${t('local.hub.aucun_modele_gguf')} text=${t('local.hub.essaie_un_autre_nom')} /></div>`}`;
}
