import { t } from '../../core/i18n.js';
import { html, useState, useStore, fmtTok, fmtSecs, baseName } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tip } from '../../ui/controls.js';
import { SessionPanel, PanelTabs } from '../../ui/session-panel.js';
import { app } from '../../core/state.js';
import { chat, compact, compactSession } from '../chat/engine.js';
import { currentExec } from '../chat/picker.js';
import { splitPastedMessage, downloadPaste } from '../chat/pasted-text.js';
import { LocalParams } from './params.js';

const KV = ({ k, children }) => html`<div class="kv"><span>${k}</span><span>${children}</span></div>`;

function lastTurn(s) { return s && (s.turns || []).at(-1); }

function CloudPanel() {
  const s = useStore(chat, c => c.session);
  const ws = useStore(app, a => a.workspace);
  if (!s) return null;
  const p = ((ws && ws.providers) || []).find(x => x.id === s.provider_id);
  const localT = lastTurn(s), u = localT && localT.usage;
  return html`<div class="insp-body">
    <div class="insp-model"><b>${baseName(s.model)}</b><span>${s.provider_name}</span></div>
    <p class="note insp-hint"><${Icon} n="globe" />${t('search.cloud_where')}<${Tip} text=${t('search.cloud_tip')} /></p>
    <div class="card pad-sm">
      <${KV} k=${t('cloud.page.fournisseur')}>${s.provider_name}</${KV}>
      <${KV} k=${t('settings.page.cle')}>${p && p.ready ? html`<span class="tag green">${t("inspector.inspector.en_memoire")}</span>` : html`<span class="tag amber">${t("inspector.inspector.a_reconnecter")}</span>`}</${KV}>
      <${KV} k=${t('inspector.inspector.destination')}><span class="mono trunc">${s.endpoint}</span></${KV}>
    </div>
    <div class="card pad-sm">
      <div class="lbl" style="margin-bottom:4px">${t("inspector.inspector.dernier_tour")}</div>
      ${u ? html`<${KV} k=${t('usage.page.entree')}>${fmtTok(u.prompt_tokens)} ${t("inspector.inspector.tok")}</${KV}><${KV} k=${t('bench.page.sortie')}>${fmtTok(u.completion_tokens)} ${t("inspector.inspector.tok")}</${KV}>`
        : html`<p class="note">${t("inspector.inspector.le_fournisseur_n_a_pas_communique_de_decompte")}</p>`}
      ${localT && localT.duration_seconds > 0 && html`<${KV} k=${t('bench.page.duree')}>${fmtSecs(localT.duration_seconds)}</${KV}>`}
    </div>
    <p class="note">${t("inspector.inspector.texte_seulement_les_fichiers_outils_et_la_memoire_privee_ne_sont")}</p>
    <a class="btn" href="#/cloud">${t("inspector.inspector.gerer_les_fournisseurs")}</a>
  </div>`;
}

function ChatSessionPanel({ exec }) {
  const { session, ctxUsed, busy, mode } = useStore(chat, s => ({ session: s.session, ctxUsed: s.ctxUsed, busy: s.busy, mode: s.mode }));
  const size = useStore(app, s => s.status?.ctx);
  const machine = useStore(app, s => s.engineNode?.hostname || s.serverInfo?.hostname);
  const native = mode === 'native';
  const used = native ? ctxUsed : session?.context?.used;
  const capacity = native ? size : session?.context?.size;
  const turn = lastTurn(session);
  return html`<div class="insp-body session-info">
    <div class="card pad-sm">
      <${KV} k=${t('agents.info.model')}>${exec.name || '—'}</${KV}>
      ${exec.kind === 'local' && machine && html`<${KV} k=${t('workspaces.machine')}>${machine}</${KV}>`}
      ${session?.workdir && html`<${KV} k=${t('agents.info.dir')}><span class="mono trunc" title=${session.workdir}>${session.workdir}</span></${KV}>`}
      <${KV} k=${t('inspector.params.contexte_tokens')}>${capacity > 0 && used != null ? fmtTok(used) + ' / ' + fmtTok(capacity) : '—'}</${KV}>
      ${turn?.duration_seconds > 0 && html`<${KV} k=${t('bench.page.duree')}>${fmtSecs(turn.duration_seconds)}</${KV}>`}
    </div>
    ${exec.kind === 'local' && used > 0 && html`<button class="btn" disabled=${busy} onClick=${native ? compact : compactSession}>${t('agents.panel.compact')}</button>`}
  </div>`;
}

function ChatFiles() {
  const items = useStore(chat, s => s.items);
  const files = new Map();
  for (const item of items) {
    if (item.k !== 'user') continue;
    for (const f of item.files || []) files.set(f.path || String(f), { name: f.name || baseName(String(f)) });
    for (const f of splitPastedMessage(item.text).files) files.set(f.name, f);
  }
  return html`<div class="insp-body session-info">
    ${files.size ? html`<ul class="session-attachments">${[...files].map(([key, f]) => html`<li key=${key}>
      <${Icon} n="file" /><span class="trunc">${f.name}</span>
      ${typeof f.content === 'string' && html`<button class="icon-btn" aria-label=${t('chat.composer.download_text')} onClick=${() => downloadPaste(f)}><${Icon} n="download" /></button>`}
    </li>`)}</ul>` : html`<p class="note">${t('noir.no_attachments')}</p>`}
    <a class="btn" href="#/files"><${Icon} n="folder" />${t('files.title')}</a>
  </div>`;
}

export function Inspector({ onClose }) {
  useStore(app, a => [a.status?.model, a.workspace?.runtimes]);
  useStore(chat, c => [c.mode, c.session?.runtime_id, c.localTarget]);
  const [tab, setTab] = useState('model');
  const exec = currentExec();
  const tabs = [
    ['model', t('inspector.inspector.modele')],
    ['session', t('inspector.inspector.session')],
    ['files', t('files.title')],
  ];
  return html`<${SessionPanel} title=${t('inspector.inspector.panneau')} onClose=${onClose}>
    <${PanelTabs} label=${t('inspector.inspector.panneau')} value=${tab} tabs=${tabs} onChange=${setTab} />
    <div class="insp-scroll" key=${tab + exec.kind}>
      ${tab === 'session' ? html`<${ChatSessionPanel} exec=${exec} />`
        : tab === 'files' ? html`<${ChatFiles} />`
        : exec.kind === 'local' ? html`<${LocalParams} noir />`
        : exec.kind === 'cloud' ? html`<${CloudPanel} />`
        : html`<p class="note session-info">${t('runtime.retired')}</p>`}
    </div>
  </${SessionPanel}>`;
}
