import { t, getLang } from '../../core/i18n.js';
// Hub : catalogue GGUF de Hugging Face. Recherche, fiche modèle, quantisations
// avec compatibilité VRAM, téléchargements suivis, README.
import { html, useState, useEffect, useRef, useMemo, useStore, cls, fmtBytes, debounce } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg, Empty } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, refreshLibrary } from '../../core/state.js';
import { md } from '../chat/md.js';

const fmtN = n => n >= 1e6 ? (n / 1e6).toFixed(1).replace(/\.0$/, '') + ' M' : n >= 1e3 ? (n / 1e3).toFixed(1).replace(/\.0$/, '') + ' k' : String(n || 0);
const PIPES = () => ([['', t("local.hub.tous")], ['text-generation', 'LLM'], ['image-text-to-text', 'Vision'], ['feature-extraction', 'Embeddings']]);
const kind = p => p === 'image-text-to-text' || p === 'image-to-text' ? 'Vision' : p === 'feature-extraction' || p === 'sentence-similarity' ? 'Embeddings' : 'LLM';

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

function Card({ m, onOpen }) {
  const [a, n] = m.id.split('/');
  return html`<button class="hub-card" onClick=${() => onOpen(m.id)}>
    <div class="hc-top"><img class="avatar" src=${'/api/hub/avatar?a=' + encodeURIComponent(m.author || a)} alt="" loading="lazy" onError=${e => { e.target.style.visibility = 'hidden'; }} />
      <div class="hc-name"><b>${n}</b><small>${m.author || a}</small></div></div>
    <div class="hc-tags"><span class="tag">${kind(m.pipeline)}</span>${m.params_b ? html`<span class="tag">${m.params_b}${t("local.hub.b")}</span>` : ''}</div>
    <div class="hc-foot"><span><${Icon} n="download" />${fmtN(m.downloads)}</span><span><${Icon} n="heart" />${fmtN(m.likes)}</span><${Icon} n="right" class="hc-go" /></div>
  </button>`;
}

function Detail({ id, onBack }) {
  const [d, setD] = useState(null);
  const [showAll, setShowAll] = useState(false);
  const models = useStore(app, s => s.models);
  const downloads = useDownloads();
  useEffect(() => { get('/api/hub/model?id=' + encodeURIComponent(id)).then(r => r.ok ? setD(r) : (toast(r.error || t("local.hub.fiche_indisponible"), 'err'), onBack())); }, [id]);
  const local = new Set((models || []).map(m => m.name.toLowerCase()));
  const readme = useMemo(() => d && d.readme ? md(d.readme.replace(/^---[\s\S]*?---\n/, '')) : '', [d, getLang()]);
  if (!d) return html`<div class="hub-detail"><div class="skeleton" style="height:90px"></div><div class="skeleton" style="height:260px;margin-top:16px"></div></div>`;
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
  return html`<div class="hub-detail anim-rise">
    <button class="btn ghost sm back" onClick=${onBack}><${Icon} n="left" />${t("local.hub.catalogue")}</button>
    <div class="hd-head"><img class="avatar lg" src=${'/api/hub/avatar?a=' + encodeURIComponent(d.author)} alt="" onError=${e => { e.target.style.visibility = 'hidden'; }} />
      <div><h2>${id.split('/')[1]}</h2><p class="muted">${d.author} · ${fmtN(d.downloads)} ${t("local.hub.telechargements")} ${fmtN(d.likes)} ${t("local.hub.j_aime")}${d.license ? ' · ' + d.license : ''}</p></div>
      <a class="btn sm ghost" href=${'https://huggingface.co/' + id} target="_blank" rel="noopener noreferrer">${t("local.hub.hugging_face")}<${Icon} n="right" /></a></div>
    <div class="card">
      <div class="quant-h"><h3>${t("local.hub.quantisations")}</h3><span class="muted">${vram ? t("local.hub.ta_vram") + (vram / 1024).toFixed(1) + t("environment.page.go") : ''}</span></div>
      <div class="quants">${shown.map(f => {
        const dl = downloads[f.name], have = local.has(f.name.toLowerCase()), ft = fit(f);
        const pct = dl && dl.total ? Math.round(dl.done * 100 / dl.total) : 0;
        return html`<div class=${cls('quant', f === best && 'best')}>
          <div class="q-main"><b class="mono">${f.quant || f.name}</b>${f === best && html`<span class="tag blue">${t("local.hub.recommande")}</span>`}${f.parts > 1 && html`<span class="tag">${f.parts} ${t("local.hub.parts")}</span>`}</div>
          <span class="mono muted">${fmtBytes(f.size)}</span>
          <span class=${'fit ' + ft}>${ft === 'fits' ? t("local.hub.tient_en_vram") : ft === 'tight' ? t("local.hub.juste") : ft === 'over' ? t("local.hub.deborde_en_ram") : ''}</span>
          <span class="q-act">${have ? html`<span class="tag green"><${Icon} n="check" />${t("local.hub.installe")}</span>`
            : dl && !dl.finished && !dl.canceled && !dl.error ? html`<div class="dl-prog"><div class="meter"><i style=${`width:${pct}%;background:var(--blue)`}></i></div><span class="mono">${pct} %</span><button class="icon-btn" aria-label="${t("local.hub.annuler")}" onClick=${() => cancel(f.name)}><${Icon} n="close" /></button></div>`
              : html`<button class="btn sm" onClick=${() => start(f)}><${Icon} n="download" />${t("local.hub.telecharger")}</button>`}</span>
        </div>`;
      })}</div>
      ${files.length > shown.length && html`<button class="btn ghost sm more-q" onClick=${() => setShowAll(true)}>${t("local.hub.voir_les")} ${files.length} ${t("local.hub.quantisations_2")}</button>`}
    </div>
    ${readme && html`<div class="card pad readme md" dangerouslySetInnerHTML=${{ __html: readme }}></div>`}
  </div>`;
}

export function Hub() {
  const [q, setQ] = useState('');
  const [sort, setSort] = useState('downloads');
  const [pipe, setPipe] = useState('');
  const [list, setList] = useState(null);
  const [err, setErr] = useState('');
  const [open, setOpen] = useState('');
  const gen = useRef(0);
  const search = useMemo(() => debounce(async (q, sort, pipe) => {
    const g = ++gen.current; setErr('');
    const p = new URLSearchParams({ q, sort, offset: '0', limit: '36' }); if (pipe) p.set('pipeline', pipe);
    try { const r = await get('/api/hub/search?' + p); if (g !== gen.current) return; if (!r.ok) throw new Error(r.error); setList(r.models || []); }
    catch (e) { if (g === gen.current) { setErr(e.message || t("local.hub.hugging_face_injoignable")); setList([]); } }
  }, 260), []);
  useEffect(() => { setList(null); search(q, sort, pipe); }, [q, sort, pipe]);
  if (open) return html`<${Detail} id=${open} onBack=${() => setOpen('')} />`;
  return html`<div class="hub">
    <div class="toolbar"><label class="search"><${Icon} n="search" /><input placeholder="${t("local.hub.rechercher_un_modele_gguf_sur_hugging_face")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <${Seg} size="sm" value=${pipe} onChange=${setPipe} label="${t("local.hub.type")}" options=${PIPES().map(([v, l]) => ({ value: v, label: l }))} />
      <select class="select sm" value=${sort} onChange=${e => setSort(e.target.value)} aria-label="${t("local.hub.tri")}"><option value="downloads">${t("local.hub.plus_telecharges")}</option><option value="likes">${t("local.hub.plus_aimes")}</option><option value="lastModified">${t("local.hub.recents")}</option></select></div>
    ${err ? html`<div class="card"><${Empty} icon="globe" title="${t("local.hub.hugging_face_injoignable")}" text=${err} /></div>`
      : list === null ? html`<div class="hub-grid">${[...Array(9)].map(() => html`<div class="skeleton" style="height:128px;border-radius:14px"></div>`)}</div>`
        : list.length ? html`<div class="hub-grid stagger">${list.map(m => html`<${Card} key=${m.id} m=${m} onOpen=${setOpen} />`)}</div>`
          : html`<div class="card"><${Empty} icon="search" title="${t("local.hub.aucun_modele_gguf")}" text="${t("local.hub.essaie_un_autre_nom")}" /></div>`}
  </div>`;
}
