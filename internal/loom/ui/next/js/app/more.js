import { html, useState, useStore, cls, baseName } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { Icon } from '../ui/icons.js';
import { toast } from '../ui/dialog.js';
import { app, engineState, phoneBack, refreshEngineNode, refreshStatus } from '../core/state.js';
import { controlEngine, engineKind } from '../features/local/page.js';
import { ownsVllm, useVllm } from '../features/settings/vllm.js';
import { VoiceMode } from '../features/voice/mode.js';
import { usePullRefresh } from './touch.js';

function EngineStatus({ node, vllm }) {
  const { status, unavailable } = useStore(app, s => ({ status: s.status, unavailable: s.unavailable.status }));
  const [busy, setBusy] = useState(false);
  const managed = vllm && ownsVllm(node, vllm.x);
  const direct = node?.direct && !managed;
  const running = managed || status?.active;
  const kind = engineKind(node?.kind || status?.engine_kind);
  const st = engineState(status);
  const tone = direct ? (node.reachable ? 'green' : 'red') : unavailable ? 'amber' : st.tone;
  const label = direct ? (node.reachable ? t('common.reachable') : t('common.unreachable'))
    : unavailable ? t('models.engine.unavailable') : running && status?.health ? t('local.page.pret')
      : running && !status?.load_error ? t('models.engine.starting') : st.label;
  const loaded = direct ? node.reachable : managed || (status?.health && !unavailable);
  const model = loaded && (vllm?.x?.model || status?.model_name || node?.model || baseName(status?.model));
  const toggle = async () => {
    setBusy(true);
    try { if (managed) await vllm.act({ action: 'stop' }); else await controlEngine(running ? 'stop' : 'start'); }
    catch (e) { toast(e.message, 'err'); }
    finally { setBusy(false); }
  };
  return html`<section class="phone-engine" aria-label=${t('local.page.moteur')}>
    <a class="phone-row-open" href="#/engine"><i class=${cls('dot', tone)}></i><span>
      <b>${label}</b>
      <small>${[kind, node?.hostname || app.get().serverInfo?.hostname, model].filter(Boolean).join(' · ')}</small>
    </span></a>
    ${!direct && html`<button type="button" class=${cls('btn', !running && 'primary')} onClick=${toggle} disabled=${busy || !status || unavailable}>
      ${running ? t('local.page.arreter') : t('local.page.demarrer')}
    </button>`}
  </section>`;
}
function VllmStatus({ node }) {
  return html`<${EngineStatus} node=${node} vllm=${useVllm()} />`;
}

function MoreList() {
  const node = useStore(app, s => s.engineNode);
  const pull = usePullRefresh(() => Promise.allSettled([refreshStatus(), refreshEngineNode()]));
  const links = [
    ['local', 'chip', t('app.groups.models')],
    ['agents', 'agent', t('agents.title')],
    ['machines', 'server', t('settings.page.machines')],
    ['files', 'folder', t('files.title'), t('files.second_brain')],
    ['settings', 'gear', t('app.palette.reglages')],
    ['more/jarvis', 'waveform', t('mobile.talk_jarvis')],
  ];
  return html`<div class="view phone-page">
    <header class="phone-title"><h1>${t('mobile.more')}</h1></header>
    <div class="phone-scroll more-list" ...${pull}>
      ${node?.direct && node.kind === 'vllm' ? html`<${VllmStatus} node=${node} />` : html`<${EngineStatus} node=${node} />`}
      <nav class="more-links" aria-label=${t('mobile.more')}>
        ${links.map(([id, icon, label, sub]) => html`<a class="phone-link" href=${'#/' + id} key=${id}><${Icon} n=${icon} /><span><b>${label}</b>${sub && html`<small>${sub}</small>`}</span><${Icon} n="right" /></a>`)}
      </nav>
    </div>
  </div>`;
}

export function MorePage({ route }) {
  return route.sub === 'jarvis'
    ? html`<div class="view chat-view"><${VoiceMode} onClose=${(_inject, destination) => { if (!destination) phoneBack(); }} /></div>`
    : html`<${MoreList} />`;
}
