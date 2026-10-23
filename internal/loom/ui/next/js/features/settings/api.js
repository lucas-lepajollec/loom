// API compatible OpenAI : tout ce qu'une application tierce (Tradoc, Open
// WebUI, Continue…) doit savoir pour utiliser les modèles de Loom, au même
// endroit : l'adresse à copier, l'accès depuis le réseau, les clés et les noms
// de modèles. Avec un moteur lié (une tour), c'est elle qui sert l'API.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, engineState } from '../../core/state.js';
import { copyText } from '../../ui/clipboard.js';
import { Line, Group } from './kit.js';
import { EngineKeys } from '../local/service.js';

const copy = async (text, what) => toast(await copyText(text) ? t('api.copied', { what }) : t('settings.page.copie_refusee'));

function Copyable({ value, what, big }) {
  return html`<span class=${'api-copy' + (big ? ' big' : '')}><code class="mono">${value}</code>
    <button class="icon-btn" aria-label=${t('api.copy', { what })} title=${t('api.copy', { what })} onClick=${() => copy(value, what)}><${Icon} n="copy" /></button></span>`;
}

export function ApiSettings() {
  const node = useStore(app, a => a.engineNode);
  const status = useStore(app, a => a.status);
  const [d, setD] = useState(null);
  const [err, setErr] = useState('');
  const load = () => get('/api/engine/openai').then(r => { if (r.ok === false) { setErr(r.error || 'error'); setD(null); } else { setD(r); setErr(''); } }).catch(e => setErr(e.message));
  useEffect(() => { load(); }, [node?.url]);
  const remote = !!node;
  const managed = remote || d?.worker;
  // L'adresse à donner : celle du moteur lié, sinon celle du réseau quand l'API
  // y est ouverte, sinon celle de cette machine seulement.
  const url = remote && node.v1 ? node.v1.replace(/\/+$/, '') + '/v1' : d ? (d.exposed ? d.url_lan : d.url_local) : '';
  const st = engineState(status);
  const running = !!status?.active;
  const toggle = async on => {
    if (on && !await confirm(t('api.open_title'), t('api.open_text'), { ok: t('api.open_ok') })) return;
    const r = await post('/api/network', { exposed: on }).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error, 'err');
    toast(t('api.restart_note'));
    load();
  };
  const models = d?.models || [];
  const example = url ? `curl ${url}/chat/completions \\\n  -H "Authorization: Bearer $LOOM_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model": "${models[0]?.id || 'local'}", "messages": [{"role": "user", "content": "Hello"}]}'` : '';
  return html`
    <p class="api-lead muted">${t('api.lead')}</p>
    <${Group} title=${t('api.where')}>
      <${Line} label=${t('api.engine')} tip=${t('api.engine_tip')}>
        <span class="api-engine"><i class=${'dot ' + (st.tone || '')}></i><b>${remote ? (node.hostname || t('api.remote')) : t('api.this_machine')}</b><span class="muted">${st.label}</span></span></${Line}>
      ${err && !remote ? html`<${Line} label=${t('api.base_url')}><span class="state">${t('api.unavailable')}</span></${Line}>`
        : html`<${Line} label=${t('api.base_url')} tip=${t('api.base_url_tip')} stack>${url ? html`<${Copyable} value=${url} what=${t('api.base_url')} big />` : html`<span class="state">…</span>`}
          ${d && !remote && d.exposed && html`<small class="muted">${t('api.local_also', { url: d.url_local })}</small>`}
          ${d && !remote && !d.exposed && html`<small class="muted">${t('api.local_only')}</small>`}</${Line}>`}
      <${Line} label=${t('api.network')} tip=${t('api.network_tip')}>
        ${managed ? html`<span class="state">${remote ? t('api.network_remote') : t('node.network_managed')}</span>` : d ? html`<${Switch} checked=${d.exposed} label=${t('api.network')} onChange=${toggle} />` : html`<span class="state">…</span>`}</${Line}>
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

    <${Group} title=${t('api.models_title')}>
      ${models.length ? html`<div class="api-models">${models.map(m => html`<div key=${m.id} class="api-model"><${Copyable} value=${m.id} what=${t('api.model_name')} /><span class="tag">${m.kind === 'preset' ? t('api.kind_preset') : t('api.kind_file')}</span></div>`)}</div>
        <p class="note muted api-note">${t('api.models_note')}</p>`
        : html`<p class="note muted api-note">${err && !remote ? t('api.unavailable') : t('api.models_empty')}</p>`}
    </${Group}>`;
}
