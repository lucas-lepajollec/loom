import { ActivityLayout } from '../usage/page.js';
import { useVisibleRefresh } from '../usage/refresh.js';
import { get, post } from '../../core/api.js';
import { t, locale } from '../../core/i18n.js';
import { html, useMemo, useRef, useState } from '../../core/lib.js';
import { validShape } from '../../core/shape.js';
import { Empty, Switch } from '../../ui/controls.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';

const AA_URL = 'https://artificialanalysis.ai';
// OpenRouter and up to three Artificial Analysis pages may be fetched in sequence.
const REQUEST_TIMEOUT = 125000;
const EMPTY = [];
const DASH = '—';
const nonnegative = n => Number.isFinite(n) && n >= 0 ? n : null;
const number = (n, digits = 0) => n.toLocaleString(locale(), { maximumFractionDigits: digits });
const usd = n => nonnegative(n) == null ? DASH : n.toLocaleString(locale(), {
  style: 'currency', currency: 'USD', currencyDisplay: 'narrowSymbol', maximumFractionDigits: 6,
});
const timestamp = value => typeof value === 'string' ? Date.parse(value) : NaN;

const modelShape = {
  id: '', name: '', creator: '', created: 0, context: 0, input_price: 0, output_price: 0, hf_id: '', free: false,
};
const nullableNumber = n => n === null || Number.isFinite(n);
const scoreShape = {
  slug: '', name: '', creator: '', released: '', intelligence: nullableNumber, speed: nullableNumber,
  first_token: nullableNumber, input_price: nullableNumber, output_price: nullableNumber,
};
const nullableRows = shape => value => value === null || validShape(value, [shape]);
const responseShape = {
  catalog: nullableRows(modelShape), new: nullableRows(modelShape), bench: nullableRows(scoreShape), has_key: false,
  catalog_at: 0, catalog_error: '', bench_at: 0, bench_error: '', intelligence_index_version: 0,
  watch: { notify: false, open_only: false },
};

function sourceError(error) {
  if (error === 'the key was refused') return t('market.key_refused');
  if (error === 'the daily quota of this key is used up; try again tomorrow') return t('market.key_quota');
  return error;
}

function RefreshTime({ at, failed }) {
  const date = new Date(at);
  if (!(at > 0) || !Number.isFinite(date.getTime())) return html`<small class="muted">${t('market.never_refreshed')}</small>`;
  const label = date.toLocaleString(locale(), { dateStyle: 'medium', timeStyle: 'short' });
  return html`<time class="muted" datetime=${date.toISOString()}>
    ${failed ? t('market.last_attempt', { date: label }) : t('market.last_refresh', { date: label })}
  </time>`;
}

function ModelDate({ at }) {
  const date = new Date(at);
  if (!Number.isFinite(date.getTime())) return DASH;
  const label = date.toLocaleDateString(locale(), { dateStyle: 'medium', timeZone: 'UTC' });
  return html`<time datetime=${date.toISOString().slice(0, 10)}>${label}</time>`;
}

function usePage(items, size) {
  const [index, set] = useState(0);
  const last = Math.max(0, Math.ceil(items.length / size) - 1);
  const current = Math.min(index, last);
  const first = current * size;
  const end = Math.min(first + size, items.length);
  return { rows: items.slice(first, end), first, end, total: items.length, current, last, set };
}

function PageButtons({ page, label }) {
  if (page.last === 0) return null;
  const prev = t('market.previous_page');
  const next = t('market.next_page');
  return html`<div class="market-pages" role="group" aria-label=${label}>
    <small role="status">${t('market.range', { a: number(page.first + 1), b: number(page.end), n: number(page.total) })}</small>
    <button class="icon-btn" type="button" aria-label=${prev} title=${prev} disabled=${page.current === 0}
      onClick=${() => page.set(page.current - 1)}><${Icon} n="left" /></button>
    <button class="icon-btn" type="button" aria-label=${next} title=${next} disabled=${page.current === page.last}
      onClick=${() => page.set(page.current + 1)}><${Icon} n="right" /></button>
  </div>`;
}

function ModelTags({ model }) {
  return html`${model.hf_id && html`<span class="tag green">${t('market.open_weights')}</span>`}
    ${model.free && html`<span class="tag">${t('market.free')}</span>`}`;
}

function ContextSize({ model }) {
  return model.context > 0 ? t('market.context_tokens', { n: number(model.context) }) : DASH;
}

function ReleaseCard({ model }) {
  const name = model.name || model.id;
  return html`<article class="activity-card market-release">
    <header class="market-card-head">
      <${Logo} name=${model.creator} size="sm" />
      <div class="market-name">
        <h3>${name}</h3>
        <small>${model.creator && model.creator + ' · '}<${ModelDate} at=${model.created > 0 ? model.created * 1000 : NaN} /></small>
      </div>
    </header>
    ${(model.hf_id || model.free) && html`<div class="market-tags"><${ModelTags} model=${model} /></div>`}
    <dl class="market-release-metrics">
      <div><dt>${t('market.context')}</dt><dd><${ContextSize} model=${model} /></dd></div>
      <div><dt>${t('market.input_price')}</dt><dd>${usd(model.input_price)}</dd></div>
      <div><dt>${t('market.output_price')}</dt><dd>${usd(model.output_price)}</dd></div>
    </dl>
    ${model.hf_id && html`<div class="market-actions">
      <a class="btn sm" href=${'https://huggingface.co/' + model.hf_id.split('/').map(encodeURIComponent).join('/')}
        target="_blank" rel="noopener noreferrer">${t('local.hub.hugging_face')}</a>
      <a class="btn sm" href=${'#/local/hub/' + encodeURIComponent(name)}><${Icon} n="search" />${t('market.search_hub')}</a>
    </div>`}
  </article>`;
}

function Releases({ data, busy, error, refresh }) {
  const recent = useMemo(() => [...(data?.new || EMPTY)].sort((a, b) => b.created - a.created), [data?.new]);
  const page = usePage(recent, 3);
  return html`<section class="activity-section" aria-labelledby="market-new">
    <div class="activity-heading">
      <h2 id="market-new">${t('market.new')}</h2>
      <div class="market-tools">
        ${data && html`<${RefreshTime} at=${data.catalog_at} failed=${data.catalog_error} />`}
        <button class="btn sm" type="button" disabled=${!!busy} onClick=${refresh}>
          ${busy === 'refresh' ? html`<span class="spinner"></span>` : html`<${Icon} n="refresh" />`}
          ${t('common.refresh')}
        </button>
      </div>
    </div>
    ${error && html`<p class="note err" role="alert">${t('market.unavailable', { error })}</p>`}
    ${data?.catalog_error && html`<p class="note err" role="alert">${t('market.catalog_error', { error: data.catalog_error })}</p>`}
    ${data ? html`
      <div class="market-list-top"><small>${t('market.last_30_days')}</small><${PageButtons} page=${page} label=${t('market.new')} /></div>
      ${recent.length ? html`<div class="activity-grid market-releases">${page.rows.map(model => html`<${ReleaseCard} key=${model.id} model=${model} />`)}</div>`
        : (!data.catalog_error || data.catalog) && html`<div class="activity-card market-empty"><${Empty} icon="sparkle" title=${t('market.no_recent')} /></div>`}
    ` : !error && html`<div class="skeleton market-loading"></div>`}
  </section>`;
}

const rankingColumns = () => [
  { key: 'name', label: t('usage.page.modele'), value: m => m.name || m.slug || null },
  { key: 'intelligence', label: t('market.intelligence'), value: m => m.intelligence, format: n => number(n, 2), descending: true },
  { key: 'speed', label: t('market.speed'), value: m => nonnegative(m.speed), format: n => t('market.tok_per_second', { n: number(n, 1) }), descending: true },
  { key: 'first_token', label: t('market.first_token'), value: m => nonnegative(m.first_token), format: n => t('market.seconds', { n: number(n, 2) }) },
  { key: 'input_price', label: t('market.input_price'), value: m => nonnegative(m.input_price), format: usd },
  { key: 'output_price', label: t('market.output_price'), value: m => nonnegative(m.output_price), format: usd },
  { key: 'released', label: t('market.release_date'), value: m => timestamp(m.released), format: at => html`<${ModelDate} at=${at} />`, descending: true },
];

function compare(a, b, descending) {
  const known = v => typeof v === 'string' || Number.isFinite(v);
  if (!known(a)) return known(b) ? 1 : 0;
  if (!known(b)) return -1;
  const order = typeof a === 'string' ? a.localeCompare(b, locale()) : a - b;
  return descending ? -order : order;
}

function Leaderboard({ models, loading }) {
  const [sort, setSort] = useState({ key: 'intelligence', descending: true });
  const columns = rankingColumns();
  const chosen = columns.find(column => column.key === sort.key);
  const ranked = useMemo(() => [...models].sort((a, b) => compare(a.intelligence, b.intelligence, true))
    .map((model, index) => ({ ...model, rank: Number.isFinite(model.intelligence) ? index + 1 : null })), [models]);
  const sorted = [...ranked].sort((a, b) => compare(chosen.value(a), chosen.value(b), sort.descending));
  const page = usePage(sorted, 12);
  const sortBy = column => {
    setSort({ key: column.key, descending: column.key === sort.key ? !sort.descending : !!column.descending });
    page.set(0);
  };
  if (!models.length) return loading ? html`<div class="skeleton market-loading"></div>`
    : html`<div class="activity-card market-empty"><${Empty} icon="chart" title=${t('market.no_scores')} /></div>`;

  const directionLabel = sort.descending ? t('market.sort_ascending') : t('market.sort_descending');
  return html`<div class="activity-card market-list">
    <div class="market-list-top market-rank-tools">
      <div class="market-sort">
        <select class="select" aria-label=${t('market.sort_by')} value=${sort.key}
          onChange=${e => sortBy(columns.find(column => column.key === e.target.value))}>
          ${columns.map(column => html`<option key=${column.key} value=${column.key}>${column.label}</option>`)}
        </select>
        <button class="icon-btn" type="button" aria-label=${directionLabel} title=${directionLabel} onClick=${() => sortBy(chosen)}>
          <${Icon} n="arrowUp" class=${sort.descending ? 'market-down' : ''} />
        </button>
      </div>
      <${PageButtons} page=${page} label=${t('market.ranking')} />
    </div>
    <div class="market-table market-ranking" role="table" aria-labelledby="market-ranking">
      <div class="market-row market-thead" role="row">
        ${columns.map(column => html`<span role="columnheader" key=${column.key}
          aria-sort=${sort.key === column.key ? (sort.descending ? 'descending' : 'ascending') : undefined}>
          <button type="button" onClick=${() => sortBy(column)}>${column.label}
            ${sort.key === column.key && html`<${Icon} n="arrowUp" class=${sort.descending ? 'market-down' : ''} />`}
          </button>
        </span>`)}
      </div>
      ${page.rows.map(model => html`<div class=${model.rank && model.rank <= 3 ? 'market-row market-top-three' : 'market-row'} role="row" key=${model.slug}>
        <span class="market-card-head" role="rowheader">
          <span class="market-rank" aria-label=${model.rank ? t('market.rank', { n: number(model.rank) }) : undefined}>
            ${model.rank ? number(model.rank) : DASH}
          </span>
          <span class="market-name"><b>${model.name || model.slug}</b>${model.creator && html`<small>${model.creator}</small>`}</span>
        </span>
        ${columns.slice(1).map(column => {
          const value = column.value(model);
          return html`<span class="market-value num" role="cell" data-label=${column.label} key=${column.key}>
            ${Number.isFinite(value) ? column.format(value) : DASH}
          </span>`;
        })}
      </div>`)}
    </div>
  </div>`;
}

function KeySetup({ busy, error, save }) {
  const [key, setKey] = useState('');
  const submit = e => {
    e.preventDefault();
    if (key.trim()) save({ key: key.trim() }, 'key');
  };
  return html`<div class="activity-card market-key">
    <div class="market-key-head">
      <${Icon} n="key" />
      <div><h3>${t('market.key_setup')}</h3><p class="note" id="market-key-note">${t('market.key_hint')}</p></div>
      <a class="btn sm" href=${AA_URL} target="_blank" rel="noopener noreferrer">${t('market.get_key')}<${Icon} n="right" /></a>
    </div>
    <form class="market-setting market-key-form" onSubmit=${submit}>
      <label for="market-key">${t('market.api_key')}</label>
      <input class="input" id="market-key" type="password" autocomplete="off" spellcheck="false" required maxlength="2000"
        value=${key} disabled=${busy === 'key'} aria-invalid=${error ? 'true' : undefined}
        aria-describedby=${error ? 'market-key-note market-key-error' : 'market-key-note'} onInput=${e => setKey(e.target.value)} />
      <button class="btn sm primary" type="submit" disabled=${!!busy || !key.trim()}>
        ${busy === 'key' ? t('market.validating_key') : t('ui.dialog.enregistrer')}
      </button>
    </form>
    ${error && html`<p id="market-key-error" class="note err market-setting" role="alert">${error}</p>`}
  </div>`;
}

function Ranking({ data, busy, error, save }) {
  return html`<section class="activity-section" aria-labelledby="market-ranking">
    <div class="activity-heading">
      <h2 id="market-ranking">${t('market.ranking')}</h2>
      ${data.has_key && html`<a class="market-credit" href=${AA_URL} target="_blank" rel="noopener noreferrer">${t('market.aa_credit')}</a>`}
    </div>
    ${data.has_key ? html`
      <div class="market-ranking-meta">
        <div><${RefreshTime} at=${data.bench_at} failed=${data.bench_error} />
          ${data.intelligence_index_version > 0 && html`<small>${t('market.index_version', { n: data.intelligence_index_version })}</small>`}</div>
        <button class="btn sm ghost" type="button" disabled=${!!busy} onClick=${() => save({ forget_key: true }, 'key')}>${t('market.remove_key')}</button>
      </div>
      ${data.bench_error && html`<p class="note err" role="alert">${t('market.aa_error', { error: sourceError(data.bench_error) })}</p>`}
      ${error && html`<p class="note err" role="alert">${error}</p>`}
      <${Leaderboard} models=${data.bench || EMPTY} loading=${!!busy && !(data.bench_at > 0) && !data.bench_error} />
    ` : html`<${KeySetup} busy=${busy} error=${error} save=${save} />`}
  </section>`;
}

function Catalogue({ models }) {
  const [query, setQuery] = useState('');
  const [creator, setCreator] = useState('');
  const [openOnly, setOpenOnly] = useState(false);
  const [freeOnly, setFreeOnly] = useState(false);
  const lang = locale();
  const creators = useMemo(() => [...new Set(models.map(model => model.creator).filter(Boolean))]
    .sort((a, b) => a.localeCompare(b, lang)), [models, lang]);
  const selectedCreator = creators.includes(creator) ? creator : '';
  const term = query.trim().toLocaleLowerCase(lang);
  const filtered = useMemo(() => models.filter(model =>
    (!selectedCreator || model.creator === selectedCreator) && (!openOnly || model.hf_id) && (!freeOnly || model.free)
      && (!term || [model.name, model.id, model.creator, model.hf_id].some(value => value?.toLocaleLowerCase(lang).includes(term)))),
  [models, selectedCreator, openOnly, freeOnly, term, lang]);
  const page = usePage(filtered, 18);
  const filter = setter => e => {
    setter(e.target.type === 'checkbox' ? e.target.checked : e.target.value);
    page.set(0);
  };
  const searchLabel = t('market.search');
  const columns = [t('usage.page.modele'), t('market.context'), t('market.input_price'), t('market.output_price')];
  return html`<section class="activity-section" aria-labelledby="market-catalogue">
    <div class="activity-heading"><h2 id="market-catalogue">${t('market.catalogue')}</h2></div>
    <div class="activity-card market-list">
      <div class="market-filters">
        <label class="search">
          <${Icon} n="search" /><input type="search" aria-label=${searchLabel} placeholder=${searchLabel} value=${query} onInput=${filter(setQuery)} />
        </label>
        <select class="select" aria-label=${t('market.creator')} value=${selectedCreator} onChange=${filter(setCreator)}>
          <option value="">${t('market.all_creators')}</option>
          ${creators.map(name => html`<option key=${name} value=${name}>${name}</option>`)}
        </select>
        <label class="market-filter"><input type="checkbox" checked=${openOnly} onChange=${filter(setOpenOnly)} />${t('market.open_only')}</label>
        <label class="market-filter"><input type="checkbox" checked=${freeOnly} onChange=${filter(setFreeOnly)} />${t('market.free_only')}</label>
      </div>
      ${page.last > 0 && html`<div class="market-list-top"><${PageButtons} page=${page} label=${t('market.catalogue')} /></div>`}
      ${filtered.length ? html`<div class="market-table market-catalogue" role="table" aria-labelledby="market-catalogue">
        <div class="market-row market-thead" role="row">${columns.map(label => html`<span key=${label} role="columnheader">${label}</span>`)}</div>
        ${page.rows.map(model => html`<div class="market-row" role="row" key=${model.id}>
          <span class="market-card-head" role="rowheader"><${Logo} name=${model.creator} size="sm" />
            <span class="market-name"><b>${model.name || model.id}</b><span class="market-tags">
              <small>${model.creator}</small><${ModelTags} model=${model} />
            </span></span>
          </span>
          <span class="market-value num" role="cell" data-label=${columns[1]}><${ContextSize} model=${model} /></span>
          <span class="market-value num" role="cell" data-label=${columns[2]}>${usd(model.input_price)}</span>
          <span class="market-value num" role="cell" data-label=${columns[3]}>${usd(model.output_price)}</span>
        </div>`)}
      </div>` : html`<div class="market-empty"><${Empty} icon="search" title=${models.length ? t('market.no_matches') : t('market.no_catalogue')} /></div>`}
    </div>
  </section>`;
}

function WatchSettings({ watch, busy, error, save }) {
  const notify = t('market.notify');
  const open = t('market.open_only');
  return html`<section class="activity-section" aria-labelledby="market-watch">
    <div class="activity-heading"><h2 id="market-watch">${t('market.settings')}</h2></div>
    <div class="activity-card market-watch">
      <div class="market-setting"><span>${notify}</span>
        <${Switch} label=${notify} checked=${watch.notify} disabled=${!!busy} onChange=${value => save({ notify: value }, 'watch')} />
      </div>
      <div class="market-setting"><span>${open}</span>
        <${Switch} label=${open} checked=${watch.open_only} disabled=${!!busy} onChange=${value => save({ open_only: value }, 'watch')} />
      </div>
      <div class="market-setting market-delivery">
        <p class="note">${t('market.delivery', { event: t('notify.ev.model_new') })}</p>
        <a class="market-credit" href="#/settings/notifications">${t('market.notification_settings')}</a>
      </div>
      ${error && html`<p class="note err market-setting" role="alert">${error}</p>`}
    </div>
  </section>`;
}

export function MarketPage() {
  const [data, setData] = useState(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [keyError, setKeyError] = useState('');
  const [watchError, setWatchError] = useState('');
  const pending = useRef(false);

  const run = async (action, task, alive = () => true) => {
    if (pending.current) return;
    pending.current = true;
    setBusy(action);
    try { await task(); }
    finally {
      pending.current = false;
      if (alive()) setBusy('');
    }
  };

  const read = async (force, alive = () => true) => {
    const options = { timeout: REQUEST_TIMEOUT };
    try {
      const result = force ? await post('/api/market', {}, options) : await get('/api/market', options);
      if (result?.ok === false) throw new Error(result.error || t('common.error'));
      if (!validShape(result, responseShape) || !['catalog', 'new', 'bench'].every(field => field in result)
          || typeof result.has_key !== 'boolean' || typeof result.watch?.notify !== 'boolean' || typeof result.watch?.open_only !== 'boolean') {
        throw new Error(t('market.invalid_data'));
      }
      if (!alive()) return;
      setData(result);
      setError('');
    } catch (err) {
      if (alive()) setError(err.message || t('common.error'));
    }
  };

  useVisibleRefresh(alive => run('read', () => read(false, alive), alive), 60000);

  const save = (body, action) => run(action, async () => {
    const setFailure = action === 'key' ? setKeyError : setWatchError;
    setFailure('');
    const result = await post('/api/market/settings', body, { timeout: REQUEST_TIMEOUT });
    if (!result.ok) {
      const error = sourceError(result.error || t('common.error'));
      setFailure(t('market.save_failed', { error }));
      return;
    }
    if (action === 'watch') {
      setData(previous => ({ ...previous, watch: { ...previous.watch, ...body } }));
      return;
    }
    setData(previous => ({
      ...previous, has_key: !!body.key, bench: null, bench_at: 0, bench_error: '', intelligence_index_version: 0,
    }));
    if (body.key) await read(false);
  });

  return html`<${ActivityLayout} title=${t('market.title')}>
    <${Releases} data=${data} busy=${busy} error=${error} refresh=${() => run('refresh', () => read(true))} />
    ${data && html`
      <${Ranking} data=${data} busy=${busy} error=${keyError} save=${save} />
      <${Catalogue} models=${data.catalog || EMPTY} />
      <${WatchSettings} watch=${data.watch} busy=${busy} error=${watchError} save=${save} />
    `}
  </${ActivityLayout}>`;
}
