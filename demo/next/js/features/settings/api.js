// API compatible OpenAI : tout ce qu'une application tierce (Tradoc, Open
// WebUI, Continue…) doit savoir pour utiliser les modèles de Loom, au même
// endroit : l'adresse à copier, l'accès depuis le réseau, les clés et les noms
// de modèles. Avec un moteur lié (une tour), c'est elle qui sert l'API.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { get, post, onChanged } from '../../core/api.js';
import { app, engineState } from '../../core/state.js';
import { copyText } from '../../ui/clipboard.js';
import { Line, Group } from './kit.js';
import { EngineKeys } from '../local/service.js';

const copy = async (text, what) => toast(await copyText(text) ? t('api.copied', { what }) : t('settings.page.copie_refusee'));
const apiURL = base => {
  const url = base?.replace(/\/+$/, '') || '';
  return url && (url.endsWith('/v1') ? url : url + '/v1');
};
const shellQuote = value => "'" + value.replace(/'/g, "'\"'\"'") + "'";

function Copyable({ value, what, big }) {
  return html`<span class=${'api-copy' + (big ? ' big' : '')}><code class="mono">${value}</code>
    <button class="icon-btn" aria-label=${t('api.copy', { what })} title=${t('api.copy', { what })} onClick=${() => copy(value, what)}><${Icon} n="copy" /></button></span>`;
}

export function ApiSettings({ compact = false, server, children }) {
  const node = useStore(app, a => a.engineNode);
  const status = useStore(app, a => a.status);
  const [d, setD] = useState(null);
  const [err, setErr] = useState('');
  const remote = !!node;
  const direct = !!node?.direct;
  const [saving, setSaving] = useState(false);
  const load = signal => get('/api/engine/openai', { signal }).then(r => {
    if (!signal?.aborted) { setD(r); setErr(''); }
  }).catch(e => { if (!signal?.aborted) setErr(e.message); });
  useEffect(() => {
    const controller = new AbortController();
    setD(null);
    setErr('');
    load(controller.signal);
    const unsubscribe = onChanged(event => {
      if (/^\/api\/(engine\/keys|network)\b/.test(event.detail?.url)) load(controller.signal);
    });
    return () => { controller.abort(); unsubscribe(); };
  }, [node?.url, node?.direct]);
  const managed = d?.worker || server?.node_managed || direct || (!compact && remote);
  const url = apiURL(direct ? node.url : node?.v1) || (d?.exposed ? d.url_lan : d?.url_local) || server?.url || '';
  const exposed = d?.exposed ?? server?.lan;
  const keyRequired = d?.key_required ?? server?.key_required;
  const st = engineState(status);
  const running = !!status?.active;
  const toggle = async on => {
    setSaving(true);
    try {
      if (on && !await confirm(t('api.open_title'), t('models.api.open_text'), { ok: t('api.open_ok') })) return;
      const r = await post('/api/network', { exposed: on });
      if (!r.ok) return toast(r.error, 'err');
      toast(t('api.restart_note'));
      await load();
    } finally { setSaving(false); }
  };
  const requireKey = async on => {
    setSaving(true);
    try {
      const r = await post('/api/apikey', { action: 'require', on });
      if (r.ok === false) return toast(r.error, 'err');
      await load();
    } finally { setSaving(false); }
  };
  const models = Array.isArray(d?.models) ? d.models : [];
  const sampleModel = (direct ? node.model : models[0]?.id) || 'local';
  const payload = JSON.stringify({ model: sampleModel, messages: [{ role: 'user', content: t('models.api.hello') }] });
  const example = url ? [
    `curl ${shellQuote(url + '/chat/completions')}`,
    (keyRequired || exposed || direct) && '-H "Authorization: Bearer $LOOM_API_KEY"',
    '-H "Content-Type: application/json"',
    `-d ${shellQuote(payload)}`,
  ].filter(Boolean).join(' \\\n  ') : '';
  const network = managed ? html`<span class="models-row-meta">${direct || (!compact && remote) ? t('api.network_remote') : t('node.network_managed')}</span>`
    : typeof exposed === 'boolean' ? html`<${Switch} checked=${exposed} label=${compact ? t('models.api.network') : t('api.network')} disabled=${saving} onChange=${toggle} />` : null;
  const modelList = models.length ? html`<div class="api-models">${models.map(m => html`<div key=${m.id} class="api-model"><${Copyable} value=${m.id} what=${t('api.model_name')} /><span class="tag">${m.kind === 'preset' ? t('api.kind_preset') : t('api.kind_file')}</span></div>`)}</div>
      <p class="note muted api-note">${t('api.models_note')}</p>`
    : html`<p class="note muted api-note">${err && !remote ? t('api.unavailable') : t('api.models_empty')}</p>`;

  if (compact) return html`
    <section class="models-group">
      <div class="models-section-head"><h2>${t('models.api.title')}</h2></div>
      <div class="models-card">
        <div class="models-row">
          <div class="models-row-main">
            ${url ? html`<code class="mono">${url}</code>` : html`<span>${err ? t('api.unavailable') : t('api.base_url')}</span>`}
            <small>${t('models.api.hint')}</small>
          </div>
          ${url && html`<button class="btn sm" aria-label=${t('local.page.copier_l_url')} onClick=${() => copy(url, t('api.base_url'))}>${t('local.page.copier')}</button>`}
        </div>
        <div class="models-row">
          <div class="models-row-main"><span>${t('models.api.network')}</span><small>${t('models.api.network_hint')}</small></div>
          ${network}
        </div>
        ${!managed && !exposed && typeof keyRequired === 'boolean' && html`<div class="models-row">
          <div class="models-row-main"><span>${t('models.api.key_local')}</span><small>${t('models.api.key_local_hint')}</small></div>
          <${Switch} checked=${keyRequired} label=${t('models.api.key_local')} disabled=${saving} onChange=${requireKey} />
        </div>`}
        ${!direct && exposed && d?.firewall === 'ferme' && html`<div class="models-row">
          <div class="models-row-main"><span>${t('settings.page.pare_feu')}</span><small>${d.hint || t('api.firewall', { port: d.port })}</small></div>
        </div>`}
        ${example && html`<div class="models-row models-api-example">
          <details class="models-row-main"><summary>${t('local.page.exemple_curl')}</summary><pre class="mono">${example}</pre></details>
          <button class="btn sm" aria-label=${t('api.copy', { what: t('local.page.exemple_curl') })} onClick=${() => copy(example, t('local.page.exemple_curl'))}>${t('local.page.copier')}</button>
        </div>`}
      </div>
    </section>
    <${EngineKeys} compact local=${direct} catalog=${models.map(m => m.id).filter(Boolean)} />
    ${children}
    <details class="models-extra models-card">
      <summary>${direct ? t('models.api.this_machine') : t('api.app_title')}</summary>
      <div class="models-row"><div class="models-row-main"><span>${t('api.f_key')}</span>
        <small>${typeof keyRequired === 'boolean' ? keyRequired || exposed ? t('api.f_key_yes') : t('api.f_key_no') : t('settings.page.inconnu')}</small></div></div>
      <div class="models-row"><div class="models-row-main"><span>${t('api.f_model')}</span><small>${t('api.f_model_text')}</small></div></div>
      ${modelList}
    </details>`;

  return html`
    <p class="api-lead muted">${t('api.lead')}</p>
    <${Group} title=${t('api.where')}>
      <${Line} label=${t('api.engine')} tip=${t('api.engine_tip')}>
        <span class="api-engine"><i class=${'dot ' + (st.tone || '')}></i><b>${remote ? (node.hostname || t('api.remote')) : t('api.this_machine')}</b><span class="muted">${st.label}</span></span></${Line}>
      ${err && !remote ? html`<${Line} label=${t('api.base_url')}><span class="state">${t('api.unavailable')}</span></${Line}>`
        : html`<${Line} label=${t('api.base_url')} tip=${t('api.base_url_tip')} stack>${url ? html`<${Copyable} value=${url} what=${t('api.base_url')} big />` : html`<span class="state">…</span>`}
          ${d && !remote && d.exposed && html`<small class="muted">${t('api.local_also', { url: d.url_local })}</small>`}
          ${d && !remote && !d.exposed && html`<small class="muted">${t('api.local_only')}</small>`}</${Line}>`}
      <${Line} label=${t('api.network')} tip=${t('api.network_tip')}>${network || html`<span class="state">…</span>`}</${Line}>
      ${d && !remote && d.exposed && d.firewall === 'ferme' && html`<${Line} label=${t('settings.page.pare_feu')}><span class="state">${d.hint || t('api.firewall', { port: d.port })}</span></${Line}>`}
      ${!running && !remote && html`<div class="api-warn"><${Icon} n="alert" /><span>${t('api.stopped')}</span><a class="btn sm" href="#/engine">${t('api.open_engine')}</a></div>`}
    </${Group}>

    <${Group} title=${t('api.app_title')}>
      <div class="api-recipe">
        <div><span>${t('api.f_url')}</span>${url ? html`<${Copyable} value=${url} what=${t('api.base_url')} />` : '—'}</div>
        <div><span>${t('api.f_key')}</span><span class="muted">${d?.key_required || d?.exposed ? t('api.f_key_yes') : t('api.f_key_no')}</span></div>
        <div><span>${t('api.f_model')}</span><span class="muted">${t('api.f_model_text')}</span></div>
      </div>
      ${example && html`<details class="api-example"><summary>${t('api.example')}</summary><pre class="mono">${example}</pre>
        <button class="btn sm" onClick=${() => copy(example, t('api.example'))}><${Icon} n="copy" />${t('local.page.copier')}</button></details>`}
    </${Group}>

    <${EngineKeys} />

    <${Group} title=${t('api.models_title')}>${modelList}</${Group}>`;
}
