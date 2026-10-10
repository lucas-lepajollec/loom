import { configOptions, protocolLabel } from './options.js';
import { t, tSource } from '../../core/i18n.js';
import { Logo } from '../../ui/logo.js';
import { html, useState, useEffect, useStore, fmtTok } from '../../core/lib.js';
import { inspectTrigger } from '../../ui/drawer.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Tip } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { get, post, onChanged as onStateChanged } from '../../core/api.js';
import { app, go, refreshWorkspace } from '../../core/state.js';
import { groupVariants } from '../chat/picker.js';
import { splitEffort } from '../inspector/inspector.js';
import { setVisible } from '../cloud/page.js';
import { newDiscussion, chooseRemote } from '../chat/engine.js';
import { HarnessAccount } from './account.js';
import { CAPS, CapabilityEvidence } from './capabilities.js';
import { AgentMachines } from './agent-machines.js';
import { AgentDiscussions } from './discussions.js';
import { AgentModels, optValues, byCat } from './models.js';
import { AgentSettings } from './settings.js';
import { MachineState } from './machine-state.js';

export function Detail({ rt, models, onInspect }) {
  const native = models.filter(m => m.runtime_id === rt.id);
  const groups = groupVariants(native);
  const connected = native.length > 0;
  const supported = rt.implemented && rt.capabilities && rt.capabilities.length > 0;
  const connectable = supported && rt.capabilities.includes('connect');
  const [busy, setBusy] = useState(false);
  const connect = async () => {
    if (!connectable || !rt.cli || !rt.consent) return;
    if (!await confirm(t("harnesses.page.connecter_prefix") + rt.name, tSource(rt.consent), { ok: t("harnesses.page.connecter") })) return;
    setBusy(true);
    let r;
    try { r = await post('/api/runtimes/' + encodeURIComponent(rt.id) + '/connect', { consent: true }); }
    catch (e) { toast(e.message, 'err'); return; }
    finally { setBusy(false); }
    if (!r.ok) return toast(r.error, 'err'); toast(t("harnesses.page.modeles_de") + rt.name + t("harnesses.page.disponibles")); refreshWorkspace();
  };
  return html`<div class="h-detail anim-fade" key=${rt.id}>
    <div class="h-head object-head"><${Logo} name=${rt.id} size="lg" /><div class="grow object-copy"><div class="object-title"><h2>${rt.name}</h2></div><p class="object-description">${(rt.description_key ? t(rt.description_key, { command: rt.cli }) : tSource(rt.description)) || ''}</p></div>
      <div class="acts object-actions">${connectable ? html`<button class="btn" disabled=${busy} onClick=${connect}>${connected ? html`<${Icon} n="refresh" />${t("harnesses.page.actualiser")}` : t("cloud.page.connecter")}</button>` : !supported && html`<span class="tag">${t("harnesses.page.bientot")}</span>`}</div></div>
    <${CapabilityEvidence} rt=${rt} />
    ${!supported ? html`<div class="card pad soon"><${Icon} n="sparkle" /><div><b>${t("harnesses.page.adaptateur_en_preparation")}</b><p>${t("harnesses.page.loom_ne_lance_pas_encore")} ${rt.name}${t("harnesses.page.il_apparaitra_dans_le_selecteur_quand_son_adaptateur_saura_gerer")}</p></div></div>` : html`
      <div class="grid2">
        <div class="card pad"><div class="sec-h"><h2>${t("harnesses.page.source_du_modele")}<${Tip} text="${t("harnesses.page.natif_le_compte_du_harness_choisit_parmi_ses_modeles_les_modeles")}" /></h2></div>
          <div class="source"><label class="choice on"><input type="radio" checked /><span class="grow"><b>${t("harnesses.page.natif")}</b><small>${t("harnesses.page.le_compte")} ${rt.name} ${t("harnesses.page.choisit_ses_modeles")}</small></span></label>
            <label class="choice off"><input type="radio" disabled /><span class="grow"><b>${t("harnesses.page.modele_loom")}</b><small>${t("harnesses.page.local_ou_cloud_bientot")}</small></span></label></div></div>
        <div class="card pad"><div class="sec-h"><h2>${t("harnesses.page.ce_que_loom_pilote")}</h2></div>
          ${CAPS().map(([id, label]) => html`<div class="kv"><span>${label}</span>${(rt.capabilities || []).includes(id) ? html`<span class="state"><${Icon} n="check" />${t("harnesses.page.oui")}</span>` : html`<span class="muted">${t("harnesses.page.non")}</span>`}</div>`)}</div>
      </div>
      <div class="card"><div class="sec-h pad-h"><h2>${t("harnesses.page.modeles")} <span class="count">${groups.length}</span></h2>${connected && html`<span class="muted">${t("harnesses.page.visibles_dans_le_selecteur_de_discussion")}</span>`}</div>
        ${connected ? html`<div class="rows">${groups.map(g => { const on = g.variants.some(v => v.enabled); return html`<div class="row"><span class="grow" ...${inspectTrigger(() => onInspect(g.variants[0]), t("cloud.page.inspecter") + g.name)}><div class="t">${g.name}</div><div class="s">${g.variants.length > 1 ? g.variants.length + t("harnesses.page.niveaux_de_reflexion") : g.variants[0].model}</div></span>
          <${Switch} checked=${on} label=${t("cloud.page.afficher") + g.name} onChange=${v => g.variants.forEach(x => setVisible(x.id, v))} /></div>`; })}</div>`
          : html`<p class="note pad-b">${t("harnesses.page.connecte")} ${rt.name} ${t("harnesses.page.pour_lire_les_modeles_de_ton_compte")}</p>`}</div>
      <p class="note">${t("harnesses.page.chaque_envoi_ouvre_une_session_native_avec_le_texte_commun_de_la")} ${rt.name} ${t("harnesses.page.restent_chez_lui")} <a href="#/usage">${t("harnesses.page.voir_les_quotas")}</a></p>`}
    <${MachineState} rt=${rt} />
  </div>`;
}
export function AgentDetail({ rt, models, onEdit }) {
  const nav = useStore(app, a => a.nav);
  const [installs, setInstalls] = useState([]);
  const [probe, setProbe] = useState(null);
  const [info, setInfo] = useState(null);
  const [busy, setBusy] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const [custom, setCustom] = useState(null);
  const [usage, setUsage] = useState(null);
  const family = rt.machine ? rt.logo : rt.id;
  const loadInstalls = () => get('/api/agents/installations').then(r => r.ok && setInstalls((r.installations || []).filter(i => i.harness === family || i.runtime_id === rt.id))).catch(() => {});
  const loadProbe = () => get('/api/runtimes/' + rt.id + '/probe').then(p => setProbe((p && p.probe) || {})).catch(() => setProbe({}));
  const loadInfo = refresh => get('/api/runtimes/' + rt.id + '/inspect' + (refresh ? '?refresh=1' : '')).then(r => setInfo(r && r.ok ? r.inspection : false)).catch(() => setInfo(false));
  useEffect(() => {
    loadInstalls(); loadProbe(); if (!rt.machine) loadInfo();
    const off = onStateChanged(() => { loadInstalls(); loadProbe(); });
    if (rt.custom) get('/api/harness/custom').then(r => setCustom((r.agents || []).find(a => a.id === rt.id) || null)).catch(() => {});
    get('/api/usage/native?days=7').then(r => setUsage((r.harnesses || []).find(x => x.runtime_id === rt.id) || null)).catch(() => {});
    return off;
  }, [rt.id]);
  const here = installs.find(i => i.runtime_id === rt.id) || installs.find(i => i.machine === 'local');
  const managed = !!(here && here.managed) || !!rt.custom, used = !!rt.connected;
  const missing = rt.available === false;
  const setScope = async patch => {
    if (patch.enabled && !await confirm(t('agents.enable_title', { name: rt.name }), t('agents.enable_note', { name: rt.name, machine: here ? (here.machine === 'local' ? t('agents.this_machine') : here.machine_name) : '' }), { ok: t('agents.enable_ok') })) return;
    setBusy(true);
    const r = await post('/api/agents/installations', { machine: here.machine, harness: here.harness, ...patch, consent: !!patch.enabled }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok) return toast(r.error || t('agents.change_failed'), 'err');
    await refreshWorkspace(); loadInstalls(); loadProbe();
  };
  const reread = async () => {
    setBusy(true);
    const r = await post('/api/runtimes/' + rt.id + '/connect', { consent: true }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok) toast(r.error || t("harnesses.page.lecture_impossible"), 'err');
    await refreshWorkspace(); loadProbe(); if (!rt.machine) loadInfo(true);
  };
  const del = async () => {
    if (!await confirm(t("harnesses.page.supprimer") + rt.name, t("harnesses.page.le_harness_disparait_de_loom_les_discussions_passees_restent_lisi"), { ok: t("harnesses.page.supprimer_2"), danger: true })) return;
    const r = await post('/api/harness/custom/delete', { id: rt.id });
    if (!r.ok) return toast(r.error, 'err');
    await refreshWorkspace(); go('harnesses');
  };
  const cfg = configOptions(rt.features, (probe && probe.config) || []);
  const modelOpt = byCat(cfg, 'model'), effortOpt = byCat(cfg, 'thought_level');
  const list = optValues(modelOpt);
  const groups = splitEffort(list);
  const choices = models.filter(m => m.runtime_id === rt.id);
  const choiceFor = v => choices.find(c => c.id === rt.id + ':' + v) || choices[0];
  const startWith = async choice => { if (!choice) return toast(t("harnesses.page.ce_harness_n_est_pas_encore_disponible"), 'err'); await newDiscussion(); await chooseRemote(choice); };
  const talks = ((nav && nav.conversations) || []).filter(c => c.runtime_id === rt.id).sort((x, y) => (y.updated_at || 0) - (x.updated_at || 0));
  const auth = (info && info.auth) || {};
  // An agent that lists its models has a working account even when Loom cannot read it.
  const accountKnown = !!info && info.installed !== false && managed && (auth.connected || !cfg.length);
  const account = info ? (auth.connected ? [auth.method, auth.account].filter(Boolean).join(' · ') || t("harnesses.page.connecte_2") : auth.status || t('agents.account.none')) : '';
  const ver = probe && probe.agent && probe.agent.version;
  const compat = rt.compatibility || (probe && probe.compatibility) || {};
  const canList = !!rt.features?.history_import;
  const [tone, state] = missing ? ['', t("harnesses.page.non_installe")] : used ? ['green', t('agents.state.used')] : managed ? ['amber', t('agents.state.managed')] : ['', t('agents.state.unmanaged')];
  const launch = custom ? [custom.command, ...(custom.args || [])].join(' ') : rt.install_hint || rt.cli;
  return html`<div class="agent anim-fade">
    <div class="agent-h object-head"><${Logo} name=${rt.logo || rt.id} size="lg" />
      ${!missing && managed && used && html`<button class="icon-btn object-refresh" title=${t("harnesses.page.actualiser")} aria-label=${t("harnesses.page.actualiser")} disabled=${busy} onClick=${reread}><${Icon} n="refresh" /></button>`}
      <div class="grow object-copy"><div class="agent-t object-title"><h2>${rt.name}</h2><span class=${'pill object-state ' + tone}><i class=${'dot ' + tone}></i>${state}</span></div>
        <p class="object-description">${(rt.description_key ? t(rt.description_key, { command: rt.cli }) : tSource(rt.description)) || ''}</p>
        <div class="agent-meta object-meta">${(compat.protocol || ver) && html`<span title=${compat.tested_version ? t('agents.compat.tested', { v: compat.tested_version }) : ''}>${protocolLabel(rt.features || compat) || (rt.id === 'antigravity' ? t("harnesses.page.pont_loom") : 'ACP')} ${compat.version || ver || ''}</span>`}${accountKnown && html`<span><i class=${'dot ' + (auth.connected ? 'green' : 'amber')}></i>${account}</span>`}${usage && usage.total_tokens ? html`<a href="#/usage">${fmtTok(usage.total_tokens)} tokens · 7 j</a>` : ''}</div></div>
      <div class="acts object-actions">
        ${rt.custom && html`<button class="btn ghost" onClick=${() => onEdit(custom)}>${t("harnesses.page.modifier")}</button><button class="icon-btn" aria-label=${t("harnesses.page.supprimer_2")} onClick=${del}><${Icon} n="trash" /></button>`}
        ${accountKnown && !auth.connected && !missing && html`<button class="btn" onClick=${() => setAccountOpen(true)}>${t('agents.account.login')}</button>`}
        ${missing ? '' : !managed ? html`<button class="btn primary" disabled=${busy || !here} onClick=${() => setScope({ managed: true })}>${t('agents.manage')}</button>`
          : !used ? html`<button class="btn primary" disabled=${busy || !here} onClick=${() => setScope({ enabled: true })}>${t('agents.use')}</button>`
          : html`
            <button class="btn primary" disabled=${!choices.length} onClick=${() => startWith(choiceFor(modelOpt && modelOpt.currentValue))}><${Icon} n="plus" />${t("harnesses.page.nouvelle_discussion")}</button>`}</div></div>
    ${!missing && probe && probe.error && html`<div class="alert amber"><${Icon} n="alert" /><span>${probe.error}</span></div>`}
    ${(() => { const d = (probe && probe.degraded) || rt.degraded || []; return d.length > 0 && html`<div class="alert amber"><${Icon} n="alert" /><span><b>${t('agents.degraded')}</b> : ${d.map(x => x.capability).join(', ')}. ${t('agents.degraded_note')}</span></div>`; })()}
    ${!missing && compat.warning && html`<div class="alert"><${Icon} n="info" /><span>${compat.version && compat.tested_version ? t('agents.compat.drift', { v: compat.version, tested: compat.tested_version }) : t('agents.compat.unverified')}</span></div>`}
    ${accountOpen && html`<${HarnessAccount} rt=${rt} onClose=${() => setAccountOpen(false)} onChanged=${() => loadInfo(true)} onConnect=${reread} />`}

    <${AgentMachines} installs=${installs} onChanged=${() => { loadInstalls(); loadProbe(); }} />
    <${CapabilityEvidence} rt=${rt} compat=${compat} />
    ${managed && !missing && html`<${AgentDiscussions} rt=${rt} talks=${talks} canList=${canList} installs=${installs} target=${used ? choiceFor(modelOpt && modelOpt.currentValue) : null} />`}

    <${AgentModels} groups=${groups} modelOpt=${modelOpt} used=${used} startWith=${startWith} choiceFor=${choiceFor} list=${list} />

    <${AgentSettings} rt=${rt} enabled=${managed && !missing} />

    <details class="sec more-info"><summary>${t('agents.details')}</summary>
      <div class="card pad details-kv">
        ${info && info.path && html`<div class="kv"><span>${t("harnesses.page.emplacement")}</span><code class="mono">${info.path.replace(/^\/home\/[^/]+/, '~')}</code></div>`}
        <div class="kv"><span>${t("harnesses.page.lancement")}</span><code class="mono">${launch || '—'}</code></div>
        ${info && info.installed !== false && info.env && html`<div class="kv"><span>${t("harnesses.page.cles_d_api_detectees")}</span><span>${info.env.length ? info.env.join(' · ') : t("harnesses.page.aucune_dans_l_environnement")}</span></div>
          <div class="kv"><span>${t("harnesses.page.serveurs_mcp_du_harness")}</span><span>${info.mcp_known ? info.mcp.map(m => m.name).join(' · ') || '0' : t("harnesses.page.non_lus")}</span></div>
          <div class="kv"><span>${t("harnesses.page.plugins")}</span><span>${info.plugins.join(' · ') || '0'}</span></div>
          <div class="kv"><span>${t("harnesses.page.skills_installees")}</span><span>${info.skills.map(k => k.name).join(' · ') || '0'}</span></div>`}
        ${probe && probe.commands && html`<div class="kv"><span>${t("harnesses.page.commandes_disponibles")}</span><span>${probe.commands.length}</span></div>`}
        ${probe && probe.modes && probe.modes.length > 0 && html`<div class="kv"><span>${t("harnesses.page.modes_de_l_agent")}</span><span>${probe.modes.map(m => rt.id === 'antigravity' ? tSource(m.name) || m.id : m.name || m.id).join(' · ')}</span></div>`}
        ${effortOpt && html`<div class="kv"><span>${t("harnesses.page.reflexion")}</span><span>${optValues(effortOpt).map(v => v.name || v.value).join(' · ')}</span></div>`}
        ${rt.docs && html`<div class="kv"><span>${t("harnesses.page.documentation")}</span><a href=${rt.docs} target="_blank" rel="noopener noreferrer">${rt.docs.replace(/^https?:\/\//, '')}</a></div>`}
      </div></details>
  </div>`;
}
