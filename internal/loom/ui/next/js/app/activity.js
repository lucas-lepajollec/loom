import { t, tSource } from '../core/i18n.js';
// Centre d'activité : téléchargements, travaux du moteur et file de bench en
// cours. Un bouton discret dans la barre latérale, une liste avec progression.
import { html, useState, useEffect, useRef, useStore, cls } from '../core/lib.js';
import { loomApprovals, LoomApproval } from './approvals.js';
import { Icon } from '../ui/icons.js';
import { Popover } from '../ui/controls.js';
import { get, post, request } from '../core/api.js';
import { go } from '../core/state.js';
import { visibleRefresh } from '../core/poll.js';

const pct = (a, b) => (b > 0 ? Math.min(100, Math.round(a * 100 / b)) : null);

async function readActivity() {
  const [dl, job, bench] = await Promise.all([
    get('/api/models/download/status', { timeout: 6000, retryAuth: false }).catch(() => null),
    get('/api/llamacpp/job', { timeout: 6000, retryAuth: false }).catch(() => null),
    get('/api/bench/queue?compact=1', { timeout: 6000, retryAuth: false }).catch(() => null),
  ]);
  const items = [];
  for (const d of Array.isArray(dl) ? dl : []) {
    if (d.finished || d.canceled) continue;
    const go1 = n => (n / 1073741824).toFixed(1);
    items.push({ id: 'dl:' + d.filename, icon: 'download', title: d.filename, err: d.error,
      sub: d.error || (d.total ? `${go1(d.done)} / ${go1(d.total)} Go` + (d.speed ? ` · ${(d.speed / 1048576).toFixed(0)} Mo/s` : '') : t("app.activity.demarrage")),
      p: pct(d.done, d.total), cancel: d.error ? null : () => post('/api/models/download/cancel', { filename: d.filename }), to: () => go('local', 'hub') });
  }
  if (job && job.exists) {
    const label = { install: t("app.activity.installation_de_llama_cpp"), update: t("app.activity.mise_a_jour_de_llama_cpp"), prebuilt: t("app.activity.telechargement_de_llama_cpp") }[job.action] || 'llama.cpp';
    items.push({ id: 'job', icon: 'server', title: label, err: !job.running && job.error ? job.error : '',
      sub: job.running ? (tSource(job.phase) || t("app.activity.en_cours")) : job.error ? job.error : t("app.activity.termine"),
      p: null, cancel: job.running ? null : () => post('/api/llamacpp/job/dismiss', {}), cancelLabel: t("app.activity.masquer"), to: () => go('engine') });
  }
  const b = bench && bench.job;
  if (b && b.status === 'running') {
    const done = (b.rows || []).filter(r => ['ok', 'err', 'skip'].includes(r.status)).length;
    const title = [t('app.activity.bench_queue'), b.test_name].filter(Boolean).join(' · ');
    const cancel = b.id ? () => request('/api/bench/queue/cancel', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Loom-Bench-Job': b.id }, body: '{}',
    }).catch(() => null) : null;
    items.push({ id: 'bench', icon: 'gauge', title, sub: t("app.activity.v0_v1_modeles", { v0: done, v1: (b.rows || []).length }),
      p: pct(done, (b.rows || []).length), cancel, cancelLabel: t("app.activity.arreter") });
  }
  return items;
}

export function Activity() {
  const [items, setItems] = useState([]);
  const [anchor, setAnchor] = useState(null);
  const btn = useRef();
  useEffect(() => visibleRefresh(async alive => {
    const r = await readActivity(); if (alive()) setItems(r);
  }, 3000), []);
  const approvals = useStore(loomApprovals, s => s.items);
  const running = items.filter(i => !i.err).length;
  const count = items.length + approvals.length;
  return html`<button class=${cls('icon-btn act-btn', running && 'busy', approvals.length && 'waiting')} ref=${btn} aria-label=${t("app.activity.activite") + (count ? ' · ' + count : '')} title="${t("app.activity.activite")}"
      onClick=${() => setAnchor(anchor ? null : btn.current)}><${Icon} n="bell" />${count > 0 && html`<i class="act-dot"></i>`}</button>
    ${anchor && html`<${Popover} anchor=${anchor} onClose=${() => setAnchor(null)} width=${320} class="act-pop">
      <div class="act-h">${t("app.activity.activite")}</div>
      ${approvals.map(a => html`<${LoomApproval} key=${a.request_id} item=${a} class="act-approval" />`)}
      ${items.length || approvals.length ? items.map(i => {
        const row = i.to ? 'button' : 'div';
        const open = i.to && (() => { setAnchor(null); i.to(); });
        return html`<div class="act-row" key=${i.id}>
          <${Icon} n=${i.icon} />
          <${row} type=${i.to ? 'button' : undefined} class="act-main" onClick=${open}><b>${i.title}</b><small class=${i.err ? 'err' : ''}>${i.sub}</small>
            ${i.p != null && !i.err && html`<span class="gauge"><i style=${`width:${i.p}%`}></i></span>`}</${row}>
          ${i.cancel && html`<button class="btn sm" onClick=${async () => { await i.cancel(); setItems(await readActivity()); }}>${i.cancelLabel || t("app.activity.annuler")}</button>`}
        </div>`;
      }) : html`<div class="act-empty">${t("app.activity.rien_en_cours")}</div>`}
    </${Popover}>`}`;
}
