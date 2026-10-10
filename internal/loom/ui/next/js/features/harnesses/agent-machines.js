import { t } from '../../core/i18n.js';
import { html, useState, useEffect } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Tip } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { refreshWorkspace } from '../../core/state.js';
import { lifecycleVersion, lifecycleCurrent, lifecycleResult, lifecycleChannel } from './lifecycle-state.js';
import { Lifecycle } from './lifecycle.js';

// Une ligne par machine : version (et mise à jour), gérer, utiliser.
function MachineRow({ i, onChanged }) {
  const [x, setX] = useState(null);
  const [busy, setBusy] = useState('');
  const [log, setLog] = useState(null);
  const load = () => i.managed ? get('/api/harness/lifecycle?target=' + encodeURIComponent(i.machine) + '&id=' + encodeURIComponent(i.harness)).then(r => setX(r.state || false)).catch(() => setX(false)) : setX(false);
  useEffect(() => { setX(null); load(); }, [i.machine, i.harness, i.managed]);
  const where = i.machine === 'local' ? t('agents.this_machine') : i.machine_name;
  const change = async patch => {
    if (patch.enabled && !await confirm(t('agents.enable_title', { name: i.name }), t('agents.enable_note', { name: i.name, machine: where }), { ok: t('agents.enable_ok') })) return;
    setBusy('switch');
    const r = await post('/api/agents/installations', { machine: i.machine, harness: i.harness, ...patch, consent: !!patch.enabled }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t('agents.change_failed'), 'err');
    await refreshWorkspace(); onChanged();
  };
  const update = async () => {
    if (!await confirm(t('harnesses.lifecycle.mettre_a_jour') + ' ' + i.name, t('agents.update_note', { name: i.name, machine: where }))) return;
    setBusy('update');
    const r = await post('/api/harness/lifecycle', { target: i.machine, id: i.harness, action: 'update' }, { timeout: 16 * 60 * 1000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (r.state) setX(r.state); else load();
    setLog({ ok: r.ok, text: r.log || r.error || '' });
    r.ok ? toast(i.name + lifecycleResult(r.state, t)) : toast(r.error || t('harnesses.lifecycle.echec'), 'err');
    await refreshWorkspace(); onChanged();
  };
  const version = lifecycleVersion(x ? x.version : i.version);
  return html`<div class="am-row">
    <span class="am-m"><${Icon} n=${i.machine === 'local' ? 'chip' : 'server'} /><b>${where}</b></span>
    <span class="am-v" data-label=${t("harnesses.lifecycle.version")}>${!i.installed ? html`<${Lifecycle} compact target=${i.machine} id=${i.harness} name=${i.name} where=${where} onChange=${async action => { if (action === 'install') await post('/api/agents/installations', { machine: i.machine, harness: i.harness, managed: true, enabled: true, consent: true }).catch(() => {}); onChanged(); }} />` : !i.managed ? html`<span class="muted">${lifecycleVersion(i.version) || t('harnesses.lifecycle.version_unknown')}</span>` : x === null ? html`<span class="spinner"></span>` : html`<span class="mono">${version || t('harnesses.lifecycle.version_unknown')}</span>${x?.channel && html`<span class="tag" title=${x.path}>${lifecycleChannel(x, t)}</span>`}
      ${x && !x.can_update && html`<span class="muted" title=${(x.errors || []).join(' · ')}>${t('harnesses.lifecycle.manual_update')}</span>`}${x && x.can_update ? html`<button class="btn sm" disabled=${!!busy} onClick=${update}>${busy === 'update' ? html`<span class="spinner"></span>` : html`<${Icon} n="download" />`}${x.check_update ? t("harnesses.lifecycle.check_update") : t("harnesses.lifecycle.mettre_a_jour") + (x.update_available ? " " + x.latest : "")}</button>` : ''}${lifecycleCurrent(x) && html`<span class="muted">${t('harnesses.lifecycle.a_jour')}</span>`}`}</span>
    <div class="scope-control"><span>${t('agents.manage')}</span><${Switch} label=${t('agents.manage')} checked=${i.managed} disabled=${!!busy} onChange=${v => change({ managed: v })} /></div>
    <div class="scope-control"><span>${t('agents.use')}</span><${Switch} label=${t('agents.use')} checked=${i.enabled} disabled=${!!busy || !i.ready} onChange=${v => change({ enabled: v })} /></div>
  </div>${log && html`<details class="lc-log" open=${!log.ok}><summary>${t(log.ok ? 'harnesses.lifecycle.journal_de_l_action' : 'harnesses.lifecycle.journal_de_l_echec')}</summary><pre>${log.text}</pre></details>`}`;
}

export function AgentMachines({ installs, onChanged }) {
  // Every known machine can install a missing agent through its lifecycle.
  const list = installs;
  if (!list.length) return null;
  return html`<section class="sec"><div class="sec-h"><h2>${t('agents.machines')}<${Tip} text=${t('agents.machine_tip')} /></h2></div>
    <div class="card am">
      <div class="am-row am-head"><span>${t('agents.col.machine')}</span><span>${t('harnesses.lifecycle.version')}</span><span>${t('agents.manage')}</span><span>${t('agents.use')}</span></div>
      ${list.map(i => html`<${MachineRow} key=${i.machine} i=${i} onChanged=${onChanged} />`)}
    </div></section>`;
}
