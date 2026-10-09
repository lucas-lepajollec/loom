import { lifecycleVersion, lifecycleCurrent, lifecycleResult, lifecycleChannel, lifecycleInstallAction } from './lifecycle-state.js';
import { t, locale } from '../../core/i18n.js';
// Version et mises à jour d'un harness, sur cette machine ou une machine
// connectée : version installée et dernière publiée, installer, mettre à
// jour, mise à jour automatique (jamais pendant une discussion de ce harness).
import { html, useState, useEffect } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Tip } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { refreshWorkspace } from '../../core/state.js';

const when = localT => localT ? new Date(localT).toLocaleString(locale(), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '';
// « codex-cli 0.159.2 », « 2.1.287 (Claude Code) », « Hermes Agent v0.21.3 (…) » → le numéro.
const clean = v => { const m = String(v || '').match(/v?\d+(?:\.\d+)+[\w.+-]*/); return m ? m[0] : String(v || '').trim(); };

export function Lifecycle({ target, id, name, where, onChange, compact }) {
  const [x, setX] = useState(null);
  const [busy, setBusy] = useState('');
  const [log, setLog] = useState(null);
  const load = () => get('/api/harness/lifecycle?target=' + encodeURIComponent(target) + '&id=' + encodeURIComponent(id))
    .then(r => setX(r.state || false)).catch(() => setX(false));
  useEffect(() => { setX(null); load(); }, [target, id]);
  const run = async action => {
    const verb = action === 'repair' ? t('harnesses.lifecycle.repair') : action === 'install' ? t("harnesses.lifecycle.installer") : t("harnesses.lifecycle.mettre_a_jour");
    if (!await confirm(verb + ' ' + name, (action === 'repair' ? t('harnesses.lifecycle.repair_note') : action === 'install' ? t("harnesses.lifecycle.loom_lance_l_installation_officielle_de") : t("harnesses.lifecycle.loom_lance_la_mise_a_jour_de")) + name + ' ' + where + t("harnesses.lifecycle.cela_peut_prendre_quelques_minutes") + (action === 'update' ? t("harnesses.lifecycle.les_discussions_en_cours_avec_ce_harness_peuvent_etre_interrompue") : ''), { ok: verb })) return;
    setBusy(action);
    const r = await post('/api/harness/lifecycle', { target, id, action }, { timeout: 16 * 60 * 1000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    setLog({ ok: r.ok, text: r.log || r.error || '' });
    if (r.state) setX(r.state); else load();
    if (r.ok) { toast(name + (action === 'install' ? t("harnesses.lifecycle.installe") : lifecycleResult(r.state, t))); refreshWorkspace(); onChange && onChange(); }
    else toast(r.error || t("harnesses.lifecycle.echec"), 'err');
  };
  const auto = async on => {
    const r = await post('/api/harness/lifecycle/auto', { target, id, auto: on });
    if (!r.ok) return toast(r.error || t("harnesses.lifecycle.reglage_impossible"), 'err');
    setX({ ...x, auto: r.auto, last_auto: r.last_auto });
  };
  if (x === false) return null;
  if (!x) return compact ? html`<span class="spinner"></span>` : html`<div class="kv"><span>${t("harnesses.lifecycle.version")}</span><span class="state"><span class="spinner"></span>${t("harnesses.lifecycle.verification")}</span></div>`;
  const missing = !x.can_repair && (x.requires_missing || []).length > 0;
  const installAction = lifecycleInstallAction(x);
  const installLabel = t(x.can_repair ? 'harnesses.lifecycle.repair' : 'harnesses.lifecycle.installer');
  const actionLog = log && html`<details class="lc-log" open=${!log.ok}><summary>${t(log.ok ? "harnesses.lifecycle.journal_de_l_action" : "harnesses.lifecycle.journal_de_l_echec")}</summary><pre>${log.text || '(vide)'}</pre></details>`;
  // Ligne compacte d'un harness absent : juste de quoi l'installer.
  if (compact && !x.installed) return html`${x.can_repair && html`<span class="tag" title=${x.repair_path}>${lifecycleChannel(x, t)}</span>`}${missing ? html`<span class="state err">${t("harnesses.lifecycle.manque")} ${x.requires_missing.join(', ')}</span>`
    : html`<span class="muted">${x.latest ? 'v' + String(x.latest).replace(/^v/, '') : ''}</span><button class="btn sm" disabled=${!!busy} onClick=${() => run(installAction)}>${busy ? html`<span class="spinner"></span>${t("harnesses.lifecycle.installation")}` : html`<${Icon} n="download" />${installLabel}`}</button>`}${actionLog}`;
  const la = x.last_auto;
  return html`<div class="lc">
    ${x.channel && html`<span class="tag" title=${x.repair_path || x.path}>${lifecycleChannel(x, t)}</span>`}
    <div class="kv"><span>${t("harnesses.lifecycle.version")}</span><span class="num">${x.installed ? lifecycleVersion(x.version) || t('harnesses.lifecycle.version_unknown') : t("harnesses.lifecycle.non_installe")}${x.latest && html`<span class="muted"> ${t("harnesses.lifecycle.derniere")} ${x.latest}</span>`}</span></div>
    ${x.installed ? html`<div class="kv"><span>${t("harnesses.lifecycle.mise_a_jour")}</span><span>${x.update_available
        ? html`<button class="btn sm" disabled=${!!busy || !x.can_update} onClick=${() => run('update')}>${busy ? html`<span class="spinner"></span>${t("harnesses.lifecycle.mise_a_jour_2")}` : html`<${Icon} n="download" />${t("harnesses.lifecycle.mettre_a_jour_vers")} ${x.latest}`}</button>`
        : html`${lifecycleCurrent(x) && html`<span class="state"><i class="dot green"></i>${t("harnesses.lifecycle.a_jour")}</span>`}<button class="btn sm ghost" disabled=${!!busy || !x.can_update} onClick=${() => run('update')}>${busy ? t("harnesses.lifecycle.mise_a_jour_2") : (x.check_update ? t("harnesses.lifecycle.check_update") : t("harnesses.lifecycle.mettre_a_jour"))}</button>`}</span></div>
      ${!x.check_update && html`<div class="kv"><span>${t("harnesses.lifecycle.mise_a_jour_automatique")}<${Tip} text=${t("harnesses.lifecycle.verifiee_toutes_les_6_h") + where + t("harnesses.lifecycle.jamais_pendant_une_discussion_avec") + name + '.'} /></span><${Switch} checked=${x.auto} disabled=${!x.can_update} label=${t("harnesses.lifecycle.mise_a_jour_automatique_de") + name} onChange=${auto} /></div>`}
      ${la && html`<div class="kv"><span>${t("harnesses.lifecycle.derniere_auto")}</span><span class=${'state' + (la.ok ? '' : ' err')}>${when(la.at)} · ${la.ok ? (la.from ? clean(la.from) + ' → ' : '') + (la.to || t("harnesses.lifecycle.version_unknown")) : t("harnesses.lifecycle.echec_2")}</span></div>`}`
    : html`<div class="kv"><span>${t("harnesses.lifecycle.installation_2")}</span><span>${missing
        ? html`<span class="state err">${t("harnesses.lifecycle.manque")} ${x.requires_missing.join(', ')} ${where}</span>`
        : html`<button class="btn sm" disabled=${!!busy} onClick=${() => run(installAction)}>${busy ? html`<span class="spinner"></span>${t("harnesses.lifecycle.installation")}` : html`<${Icon} n="download" />${installLabel} ${name}`}</button>`}</span></div>`}
    ${x.unverified && html`<p class="note">${t("harnesses.lifecycle.commande_d_installation_non_verifiee_pour_ce_harness_relis_le_jou")}</p>`}
    ${x.installed && !x.can_update && html`<p class="note">${(x.errors || []).join(' · ')}</p>`}
    ${actionLog}
  </div>`;
}

// Machine connectée sur laquelle tourne un harness distant (custom-<machine>-<harness>).
export async function remoteHarnessTarget(runtimeId) {
  const r = await get('/api/machines').catch(() => null);
  const m = r && r.ok && r.machines.find(x => (x.harnesses || []).includes(runtimeId));
  return m ? { target: m.id, id: runtimeId.slice(('custom-' + m.id + '-').length), machine: m } : null;
}
