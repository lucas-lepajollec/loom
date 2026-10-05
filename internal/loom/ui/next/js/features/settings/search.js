import { html, useState, useEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { Group, Line } from './kit.js';
import { Icon } from '../../ui/icons.js';

export function SearchSettings() {
  const [loading, setLoading] = useState(true);
  const [saved, setSaved] = useState(null), [v, setV] = useState({ provider: 'duckduckgo', url: '' });
  const [key, setKey] = useState(''), [remove, setRemove] = useState(false), [busy, setBusy] = useState(false);
  const [query, setQuery] = useState(''), [result, setResult] = useState(null), [error, setError] = useState('');
  useEffect(() => { let alive = true; get('/api/internet/search').then(r => {
    if (!alive) return; if (!r.ok) { setError(r.error); return; }
    setSaved(r); setV({ provider: r.provider, url: r.url || '' });
  }).catch(e => { if (alive) setError(e.message); }).finally(() => { if (alive) setLoading(false); }); return () => { alive = false; }; }, []);
  const dirty = !saved || v.provider !== saved.provider || v.url !== (saved.url || '') || !!key || remove;
  const save = async () => {
    setBusy(true); setError('');
    try {
      const body = { ...v }; if (key || remove) body.key = remove ? '' : key;
      const r = await post('/api/internet/search', body); if (!r.ok) { setError(r.error); return; }
      setSaved(r); setV({ provider: r.provider, url: r.url || '' }); setKey(''); setRemove(false); setResult(null);
    } finally { setBusy(false); }
  };
  const test = async () => {
    setBusy(true); setError('');
    try { const r = await post('/api/internet/search/test', { query }); if (!r.ok) { setError(r.error); return; } setResult(r.results || []); }
    finally { setBusy(false); }
  };
  return html`<${Group} title=${t('search.provider')}>
    <${Line} label=${t('search.provider')} tip=${t('search.provider_tip')}><select class="select" disabled=${busy || loading} value=${v.provider} onChange=${e => { setV({ provider: e.target.value, url: '' }); setKey(''); setRemove(false); setResult(null); }}>
      <option value="duckduckgo">DuckDuckGo</option><option value="searxng">SearXNG</option><option value="brave">Brave Search</option><option value="tavily">Tavily</option></select></${Line}>
    ${v.provider === 'searxng' && html`<${Line} label=${t('search.url')} tip=${t('search.searx_tip')}><input class="input sm" value=${v.url} placeholder="http://searxng:8080" onInput=${e => setV({ ...v, url: e.target.value })} /></${Line}>`}
    ${v.provider !== 'duckduckgo' && html`<${Line} label=${t('search.key')} tip=${t('search.key_tip')}>
      <input class="input sm" type="password" autocomplete="off" value=${key} placeholder=${v.provider === 'searxng' ? t('settings.page.facultative') : t('search.key')} onInput=${e => { setKey(e.target.value); setRemove(false); }} />
      ${saved?.provider === v.provider && saved.key_set && !remove && html`<span class="muted">${t('search.key_stored')}</span><button class="btn sm ghost" onClick=${() => { setRemove(true); setKey(''); }}>${t('settings.page.retirer')}</button>`}
    </${Line}>`}
    <${Line} label=${t('search.save')}><button class="btn sm" disabled=${busy || loading || !dirty} onClick=${save}>${t('search.save')}</button></${Line}>
    <${Line} label=${t('search.test')} tip=${t('search.test_tip')}><input class="input sm" aria-label=${t('search.query')} value=${query} placeholder=${t('search.query')} onInput=${e => setQuery(e.target.value)} /><button class="btn sm" disabled=${busy || dirty || !query.trim()} onClick=${test}>${busy ? html`<span class="spinner"></span>` : html`<${Icon} n="search" />`}${t('search.test')}</button></${Line}>
    ${error && html`<p class="note err" role="alert">${error}</p>`}
    ${result && html`<p class="note" role="status">${t('search.results', { n: result.length })}</p>${result.map(r => html`<p class="note"><a href=${r.URL || r.url} target="_blank" rel="noopener noreferrer">${r.Title || r.title || r.URL || r.url}</a></p>`)}`}
  </${Group}>`;
}
