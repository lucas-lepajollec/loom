import { html, useEffect, useRef, useState } from '../../core/lib.js';
import { get, post } from '../../core/api.js';
import { locale, t } from '../../core/i18n.js';
import { Drawer } from '../../ui/drawer.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { Seg, Switch } from '../../ui/controls.js';

const msg = (key, values) => t(`connections.mcp.${key}`, values);
const onDemand = 'on_demand';
export const serverExposure = (server, mode) => server.exposure || (mode === 'smart' ? onDemand : 'direct');
export const exposureBadge = (server, mode) => msg(serverExposure(server, mode) === onDemand ? 'smart_badge' : 'direct');
export const transportBadge = server => server.transport === 'http' ? (server.type === 'sse' ? 'HTTP · SSE' : 'HTTP') : 'stdio';
export const serverStatus = server => !server.enabled ? msg('disabled')
  : server.connected ? msg('connected', { n: server.tools?.length || 0 }) : msg('error');

export function useMCPServers() {
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const request = useRef({ version: 0, controller: null });
  const publish = result => {
    request.current.version++;
    setError('');
    setData(result);
  };
  const reload = async () => {
    request.current.controller?.abort();
    const controller = new AbortController();
    const version = ++request.current.version;
    request.current.controller = controller;
    try {
      const result = await get('/api/mcp', { signal: controller.signal, cache: 'no-store' });
      if (version === request.current.version) publish(result);
    } catch (e) {
      if (version === request.current.version && !controller.signal.aborted) setError(e.message);
    }
  };
  useEffect(() => {
    reload();
    const timer = setInterval(() => { if (!document.hidden) reload(); }, 20000);
    return () => { clearInterval(timer); request.current.controller?.abort(); };
  }, []);
  return { data, error, publish, reload };
}

export function MCPBudget({ budget }) {
  if (!budget) return null;
  const fmt = new Intl.NumberFormat(locale());
  const values = { injected: fmt.format(budget.injected_tokens), direct: fmt.format(budget.direct_tokens), saved: fmt.format(budget.saved_percent) };
  return html`<div class="cx-mcp-budget">
    <strong>${msg(budget.saved_percent ? 'budget_saved' : 'budget', values)}</strong>
    ${budget.on_demand_tools > 0 && html`<p>${msg('discovery_hint')}</p>`}
  </div>`;
}

const exposureOptions = (busy, drawer = false) => [
  { value: onDemand, label: msg('on_demand'), disabled: busy },
  { value: 'direct', label: msg(drawer ? 'always' : 'direct'), disabled: busy },
];

export function MCPServerDrawer({ server, mode, onClose, onEdit, onChange, onDelete }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [test, setTest] = useState(null);
  const action = async (operation, fields = {}, done) => {
    setBusy(true);
    setError('');
    const result = await post('/api/mcp/' + operation, { name: server.name, ...fields });
    setBusy(false);
    if (!result.ok) { setError(result.error || msg('error')); return; }
    if (result.servers) onChange(result);
    done?.(result);
  };
  const remove = async () => {
    if (await confirm(msg('delete_title'), msg('delete_text', { name: server.name }), { ok: msg('delete'), danger: true })) {
      await action('delete', {}, () => onDelete(server.name));
    }
  };
  const check = () => action('test', {}, result => {
    const checked = result.servers?.find(item => item.name === server.name);
    setTest({ latency: result.latency_ms, count: checked?.tools?.length || 0, connected: checked?.connected });
  });
  const toolNames = [...new Set([...(server.tools || []), ...(server.disabled || [])])].sort((a, b) => a.localeCompare(b));
  const exposure = value => action('save', { exposure: value, exposure_only: true });
  return html`<${Drawer} title=${server.name} class="cx-drawer cx-mcp-drawer" onClose=${onClose}>
    <div class="cx-detail cx-mcp-detail">
      <div class="cx-detail-hero">
        <div class="cx-mcp-tags"><span class="tag">${transportBadge(server)}</span><span class="tag">${exposureBadge(server, mode)}</span><span>${serverStatus(server)}</span></div>
        ${server.description && html`<p class="cx-lead">${server.description}</p>`}
        ${server.enabled && server.error && html`<p class="cx-note msg-err" role="status">${server.error}</p>`}
      </div>
      <div class="cx-mcp-control"><span>${msg('enabled')}</span>
        <${Switch} checked=${server.enabled} disabled=${busy} label=${msg('enabled')} onChange=${on => {
          setTest(null); action('toggle', { on });
        }} />
      </div>
      <div class="cx-mcp-mode">
        <${Seg} class="cx-mcp-seg" label=${msg('exposure')} value=${serverExposure(server, mode)} options=${exposureOptions(busy, true)} onChange=${exposure} />
        ${server.exposure && html`<button type="button" class="btn sm" disabled=${busy} onClick=${() => exposure('')}>${msg('inherit')}</button>`}
      </div>
      <div class="cx-mcp-actions">
        <button type="button" class="btn" disabled=${busy || !server.enabled} onClick=${check}>${msg('test')}</button>
        <button type="button" class="btn" disabled=${busy} onClick=${onEdit}>${msg('edit')}</button>
        <button type="button" class="btn danger" disabled=${busy} onClick=${remove}>${msg('delete')}</button>
      </div>
      ${error && html`<p role="alert" class="cx-note msg-err">${error}</p>`}
      ${test && html`<p role="status" class="cx-note">${msg(test.connected ? 'test_ok' : 'test_failed', { ms: test.latency ?? 0, n: test.count })}</p>`}
      <h3 class="cx-section">${msg('tools')}</h3>
      ${toolNames.length > 0 ? html`<ul class="cx-rows cx-mcp-tool-list">${toolNames.map(tool => html`
        <li key=${tool} class="cx-mcp-control"><span class="mono">${tool}</span>
          <${Switch} label=${tool} disabled=${busy} checked=${!server.disabled?.includes(tool)} onChange=${on => action('tool', { tool, on })} />
        </li>`)}</ul>` : html`<p class="cx-note">${msg('no_tools')}</p>`}
    </div>
  </${Drawer}>`;
}

const record = value => value && typeof value === 'object' && !Array.isArray(value);
const stringMap = value => record(value) && Object.values(value).every(item => typeof item === 'string');
const formatPairs = (map, separator) => Object.entries(map || {}).map(([key, value]) => {
  const quoted = value !== value.trim() || /[\r\n]/.test(value) || value.startsWith('"');
  return key + separator + (quoted ? JSON.stringify(value) : value);
}).join('\n');
function readPairs(text, separator, errorKey) {
  const pairs = [];
  for (const line of text.split('\n')) {
    if (!line.trim()) continue;
    const split = line.indexOf(separator);
    const name = line.slice(0, split).trim();
    if (split <= 0 || !name || pairs.some(([key]) => key === name)) throw new Error(msg(errorKey));
    let value = line.slice(split + 1).trim();
    if (value.startsWith('"') && value.endsWith('"')) {
      try { value = JSON.parse(value); } catch (_) { throw new Error(msg(errorKey)); }
    }
    pairs.push([name, value]);
  }
  return Object.fromEntries(pairs);
}

const inputsMap = items => Object.fromEntries((items || []).map(item => [item.name, item.value || '']));
const formFrom = (server = {}, fallback = onDemand) => ({
  name: server.name || '', transport: server.transport || (server.command ? 'stdio' : 'http'),
  type: server.type || (server.command ? 'stdio' : 'http'), command: server.command || '', url: server.url || '',
  args: server.args?.length ? JSON.stringify(server.args) : '', env: formatPairs(server.env, '='),
  headers: formatPairs(server.headers, ': '), description: server.description || '',
  exposure: server.exposure || fallback, enabled: server.enabled ?? true,
});

function pastedServers(text) {
  const data = JSON.parse(text);
  const entries = data?.mcpServers ?? data?.servers;
  if (!record(entries)) throw new Error();
  const configs = Object.entries(entries).filter(([name, c]) =>
    name.trim() && !name.includes('__') && record(c) && (typeof c.command === 'string' || typeof c.url === 'string') &&
    (!c.args || Array.isArray(c.args) && c.args.every(x => typeof x === 'string')) &&
    (!c.env || stringMap(c.env)) && (!c.headers || stringMap(c.headers)));
  if (!configs.length) throw new Error();
  return configs;
}

function MCPManualForm({ form, edit, busy, change, onError }) {
  const [paste, setPaste] = useState(false);
  const [snippet, setSnippet] = useState('');
  const [choices, setChoices] = useState([]);
  const apply = ([name, config]) => {
    change(formFrom({ ...config, name, transport: config.command ? 'stdio' : 'http',
      enabled: typeof config.enabled === 'boolean' ? config.enabled : true,
      exposure: config.exposure === 'direct' ? 'direct' : onDemand,
    }));
    setSnippet(''); setChoices([]); setPaste(false);
  };
  const parse = () => {
    try {
      const entries = pastedServers(snippet);
      if (entries.length === 1) apply(entries[0]);
      else setChoices(entries);
      onError('');
    } catch (_) { onError(msg('bad_json')); }
  };
  const field = (key, label, props = {}) => html`<label class="cx-mcp-field"><span>${msg(label)}</span>
    <input class="input mono" name=${key} value=${form[key]} onInput=${e => change({ [key]: e.target.value })} ...${props} />
  </label>`;
  const area = (key, label, placeholder) => html`<label class="cx-mcp-field"><span>${msg(label)}</span>
    <textarea class="input mono" name=${key} rows="2" value=${form[key]} placeholder=${placeholder} spellcheck="false"
      onInput=${e => change({ [key]: e.target.value })}></textarea>
  </label>`;
  return html`<div class="cx-mcp-form">
    ${!edit && html`<div class="cx-mcp-actions">
      <button type="button" class="btn sm" aria-expanded=${String(paste)} onClick=${() => setPaste(!paste)}>${msg('paste')}</button>
    </div>`}
    ${paste && html`<div class="cx-mcp-inline">
      <label class="cx-mcp-field"><span>mcpServers</span><textarea class="input mono" rows="4" spellcheck="false" value=${snippet}
        onInput=${e => { setChoices([]); setSnippet(e.target.value); }}></textarea></label>
      ${choices.length > 0 ? html`<label class="cx-mcp-field"><span>${msg('choose_server')}</span><select class="input" value="" onChange=${e => {
        if (e.target.value !== '') apply(choices[Number(e.target.value)]);
      }}><option value="">${msg('choose_server')}</option>${choices.map(([name], i) => html`<option key=${name} value=${i}>${name}</option>`)}</select></label>`
        : html`<button type="button" class="btn" disabled=${busy || !snippet.trim()} onClick=${parse}>${msg('prefill')}</button>`}
    </div>`}
    ${field('name', 'name', { readOnly: edit, autocomplete: 'off', spellcheck: 'false' })}
    <label class="cx-mcp-field"><span>${msg('transport')}</span>
      <select class="input" value=${form.transport === 'stdio' ? 'stdio' : form.type === 'sse' ? 'sse' : 'http'} onChange=${e => {
        const type = e.target.value;
        change({ transport: type === 'stdio' ? 'stdio' : 'http', type });
      }}>
        <option value="http">${msg('http')}</option><option value="sse">HTTP · SSE</option><option value="stdio">${msg('stdio')}</option>
      </select>
    </label>
    ${form.transport === 'http' ? html`
      ${field('url', 'url', { type: 'url', placeholder: 'https://example.com/mcp', autocomplete: 'off', spellcheck: 'false' })}
      ${area('headers', 'headers', 'Authorization: Bearer …')}` : html`
      ${field('command', 'command', { placeholder: 'npx', autocomplete: 'off', spellcheck: 'false' })}
      ${area('args', 'args', '["-y", "package-name"]')}
      ${area('env', 'env', 'API_KEY=')}`}
    ${field('description', 'description')}
    <${Seg} class="cx-mcp-seg" label=${msg('exposure')} value=${form.exposure} options=${exposureOptions(busy)} onChange=${exposure => change({ exposure })} />
  </div>`;
}

function RegistryTab({ onPreset }) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState(null);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    const timer = setTimeout(async () => {
      try {
        const data = await get('/api/mcp/registry/search?q=' + encodeURIComponent(query.trim()), { signal: controller.signal });
        if (!controller.signal.aborted) setResults(data.servers || []);
      } catch (e) { if (!controller.signal.aborted) setError(e.message); }
    }, 280);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [query, retry]);
  return html`<div class="cx-mcp-list">
    <label class="cx-mcp-field"><span>${msg('registry_search')}</span>
      <input class="input" type="search" value=${query} maxlength="150" onInput=${e => {
        setResults(null); setError(''); setQuery(e.target.value);
      }} />
    </label>
    ${error ? html`<p class="cx-note msg-err" role="alert">${error}</p><button type="button" class="btn" onClick=${() => { setError(''); setRetry(retry + 1); }}>${msg('retry')}</button>`
      : !results ? html`<p class="cx-note" role="status">${msg('loading')}</p>`
        : !results.length ? html`<p class="cx-note">${t('connections.no_match')}</p>`
          : results.map(server => html`<article class="cx-mcp-item" key=${server.id}>
            <h3>${server.title}</h3><p>${server.description}</p>
            ${server.presets?.length ? html`<div class="cx-mcp-list">${server.presets.map((p, i) => html`<div key=${i} class="cx-mcp-control">
              <div><span class="tag">${p.command || transportBadge(p)}</span><p class="mono cx-mcp-target">${p.url || [p.command, ...(p.args || [])].join(' ')}</p></div>
              <button type="button" class="btn sm" onClick=${() => onPreset(server, p)}>${msg('configure')}</button>
            </div>`)}</div>` : html`<p class="cx-note">${msg('no_preset')}</p>`}
          </article>`)}
  </div>`;
}

function SourceTab({ onAdopt }) {
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [path, setPath] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    get('/api/mcp/sources', { signal: controller.signal })
      .then(result => { if (!controller.signal.aborted) setData(result); })
      .catch(e => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, []);
  const run = async (url, body, done) => {
    setBusy(true); setError('');
    const result = await post(url, body);
    setBusy(false);
    if (!result.ok) { setError(result.error || msg('error')); return; }
    done(result);
  };
  const link = (path, label) => run('/api/mcp/sources', { path, label, action: 'link' }, result => { setData(result); setPath(''); });
  return html`<div class="cx-mcp-list">
    <p class="cx-note">${msg('import_hint')}</p>
    ${error && html`<p class="cx-note msg-err" role="alert">${error}</p>`}
    ${!data && !error && html`<p class="cx-note" role="status">${msg('loading')}</p>`}
    ${data?.suggested?.map(s => html`<div class="cx-mcp-control cx-mcp-item" key=${s.path}>
      <div><strong>${s.label}</strong><p class="mono cx-mcp-target">${s.path}</p></div>
      <button type="button" class="btn sm" disabled=${busy} onClick=${() => link(s.path, s.label)}>${msg('link')}</button>
    </div>`)}
    ${data?.sources?.map(source => html`<article class="cx-mcp-item" key=${source.path}>
      <h3>${source.label}</h3><p class="mono">${source.path}</p>
      ${source.error ? html`<p class="msg-err">${source.error}</p>` : source.servers?.length ? source.servers.map(s => html`
        <div class="cx-mcp-control" key=${s.source + '/' + s.name}>
          <div><strong>${s.name}</strong><p class="cx-mcp-target">${[s.transport, s.project].filter(Boolean).join(' · ')}</p></div>
          <button type="button" class="btn sm" disabled=${busy} onClick=${() =>
            run('/api/mcp/sources/adopt', { source: s.source, name: s.name }, onAdopt)}>${msg('adopt')}</button>
        </div>`) : html`<p class="cx-note">${t('connections.no_match')}</p>`}
    </article>`)}
    <label class="cx-mcp-field"><span>${msg('source_path')}</span><input class="input mono" placeholder="~/.claude.json" value=${path} onInput=${e => setPath(e.target.value)} /></label>
    <div class="cx-mcp-actions"><button type="button" class="btn" disabled=${busy || !path.trim()} onClick=${() => link(path.trim())}>${msg('link')}</button></div>
  </div>`;
}

export function MCPServerDialog({ server, mode, onClose, onSaved, onAdopt }) {
  const [tab, setTab] = useState('manual');
  const [form, setForm] = useState(() => formFrom(server, server ? serverExposure(server, mode) : onDemand));
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const change = patch => setForm(current => ({ ...current, ...patch }));
  const selectPreset = (template, preset) => {
    setForm(formFrom({ ...preset, name: template.suggested_name, description: template.description,
      env: inputsMap(preset.env_vars), headers: inputsMap(preset.headers),
    }));
    setTab('manual');
  };
  const save = async () => {
    const name = form.name.trim();
    if (!name || name.includes('__')) { setError(msg('bad_name')); return; }
    const body = { name, exposure: form.exposure, description: form.description, enabled: form.enabled, create_only: !server };
    if (server && !server.exposure && form.exposure === serverExposure(server, mode)) body.exposure = '';
    try {
      if (form.transport === 'http') {
        body.url = form.url.trim();
        body.type = form.type;
        body.headers = readPairs(form.headers, ':', 'bad_headers');
      } else {
        body.command = form.command.trim();
        body.type = form.type;
        try {
          body.args = form.args.trim() ? JSON.parse(form.args) : [];
          if (!Array.isArray(body.args) || !body.args.every(value => typeof value === 'string')) throw new Error();
        } catch (_) { throw new Error(msg('bad_args')); }
        body.env = readPairs(form.env, '=', 'bad_env');
      }
    } catch (e) { setError(e.message); return; }
    setBusy(true); setError('');
    const result = await post('/api/mcp/save', body);
    setBusy(false);
    if (!result.ok) { setError(result.error || msg('error')); return; }
    onSaved(result);
  };
  const ready = form.name.trim() && (form.transport === 'http' ? form.url.trim() : form.command.trim());
  return html`<${Modal} title=${msg(server ? 'edit_title' : 'add')} class="cx-mcp-modal" onClose=${busy ? undefined : onClose} focusField=${false} wide
    foot=${html`<button type="button" class="btn" disabled=${busy} onClick=${onClose}>${t('ui.dialog.annuler')}</button>
      ${tab === 'manual' && html`<button type="button" class="btn primary" disabled=${busy || !ready} onClick=${save}>${msg('save')}</button>`}`}>
    ${!server && html`<${Seg} class="cx-mcp-seg" label=${msg('add')} value=${tab} onChange=${value => { setError(''); setTab(value); }}
      options=${['manual', 'registry', 'import'].map(value => ({ value, label: msg(value), disabled: busy }))} />`}
    ${tab === 'manual' && html`<${MCPManualForm} form=${form} edit=${!!server} busy=${busy} change=${change} onError=${setError} />`}
    ${tab === 'registry' && html`<${RegistryTab} onPreset=${selectPreset} />`}
    ${tab === 'import' && html`<${SourceTab} onAdopt=${result => { toast(msg('adopted', { name: result.name })); onAdopt(); }} />`}
    ${error && html`<p role="alert" class="cx-note msg-err">${error}</p>`}
  </${Modal}>`;
}
