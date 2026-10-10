import { protocolLabel } from './options.js';
import { t, tSource } from '../../core/i18n.js';
import { Logo } from '../../ui/logo.js';
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { go, refreshWorkspace } from '../../core/state.js';
import { groupVariants } from '../chat/picker.js';
import { CAPS, isACP } from './capabilities.js';

export function Catalogue({ onAdded }) {
  const [list, setList] = useState(null);
  const [q, setQ] = useState('');
  const [busy, setBusy] = useState('');
  useEffect(() => { get('/api/agents/catalog').then(r => setList(Array.isArray(r) ? r : [])).catch(() => setList([])); }, []);
  const add = async a => {
    setBusy(a.id);
    const r = await post('/api/agents/catalog/add', { id: a.id }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.install_hint ? t('agents.catalog.install_first', { name: a.name }) : r.error || t('agents.change_failed'), 'err');
    await refreshWorkspace(); onAdded(r.agent && r.agent.id);
  };
  if (list === null) return html`<div class="skeleton" style="height:120px"></div>`;
  const needle = q.trim().toLowerCase();
  const rows = list.filter(a => !a.builtin && (!needle || (a.name + ' ' + (a.description || '')).toLowerCase().includes(needle)));
  return html`<div class="cat-search"><${Icon} n="search" /><input class="input" placeholder=${t('agents.catalog.search')} value=${q} onInput=${e => setQ(e.target.value)} /></div>
    <div class="card cat-list">${rows.map(a => html`<div class="set-line" key=${a.id}>
      <div class="set-l cat-l">${a.icon ? html`<img class="cat-ico" src=${a.icon} alt="" loading="lazy" />` : html`<${Logo} name=${a.id} size="sm" />`}
        <div class="grow"><div class="cat-name">${a.name} <span class="muted mono">${a.version}</span></div>${a.description && html`<div class="cat-d">${a.description}</div>`}</div></div>
      <div class="set-c">${a.added ? html`<span class="muted">${t('agents.catalog.added')}</span>`
        : a.installed ? html`<button class="btn sm" disabled=${!!busy} onClick=${() => add(a)}>${t('agents.catalog.add')}</button>`
        : html`<a class="btn sm ghost" href=${a.repository || '#'} target="_blank" rel="noopener noreferrer" title=${t('agents.catalog.not_installed')}>${t('agents.catalog.install')}</a>`}</div></div>`)}
      ${!rows.length && html`<p class="note pad">${t('agents.catalog.empty')}</p>`}</div>
    <p class="note">${t('agents.catalog.note')}</p>`;
}
const SHORT = () => ({ chat: t("harnesses.page.discussion"), 'native-events': t("harnesses.page.outils_natifs"), usage: t("harnesses.page.tokens_et_cout"), 'reasoning-summary': t("harnesses.page.resume_de_reflexion"), approvals: t("harnesses.page.autorisations"), skills: 'Skills', mcp: 'MCP Loom', tools: t("harnesses.page.outils_et_diffs"), plan: 'Plan', workdir: t("harnesses.page.dossier_de_travail"), remote: t("harnesses.page.machine_distante") });
// Où une famille d'agents est gérée : une puce par machine, verte si l'agent
// y mène des discussions Loom.
export const installationsOf = (rt, all) => all.filter(i => i.managed && (i.installed || i.machine === 'local') && (i.harness === rt.id || i.runtime_id === rt.id));

export function Card({ rt, models, installs = [] }) {
  const n = groupVariants(models.filter(m => m.runtime_id === rt.id)).length;
  const supported = rt.implemented && rt.capabilities && rt.capabilities.length > 0;
  const caps = CAPS().filter(([id]) => (rt.capabilities || []).includes(id)).map(([id]) => [id, SHORT()[id]]);
  const acp = isACP(rt), missing = rt.available === false;
  const state = !supported ? null : missing ? ['', t("harnesses.page.non_installe")] : acp ? (installs.some(i => i.enabled) || rt.connected ? ['green', t('agents.state.used')] : ['', t('agents.state.managed')]) : n ? ['green', t("harnesses.page.connecte_3")] : ['', t("harnesses.page.non_connecte")];
  return html`<button type="button" class=${cls('hx', (!supported || missing) && 'is-soon')} onClick=${() => go('harnesses', rt.id)}>
    <div class="hx-top"><${Logo} name=${rt.id} />
      <span class="grow"><b>${rt.name}</b>${rt.machine ? html`<code>${t("harnesses.page.sur_3")} ${rt.machine}</code>` : rt.cli && html`<code>${acp ? (protocolLabel(rt.features) || 'ACP') : rt.cli}</code>`}</span>
      ${!supported ? html`<span class="soon-pill">${t("harnesses.page.bientot_2")}</span>` : html`<span class="state"><i class=${'dot ' + state[0]}></i>${state[1]}</span>`}</div>
    ${rt.description && html`<p>${rt.description_key ? t(rt.description_key, { command: rt.cli }) : tSource(rt.description)}</p>`}
    ${supported && installs.length > 0 && html`<div class="hx-inst"><span>${t('agents.installations')}</span>${installs.map(i => html`<span class="hx-chip" key=${i.machine} title=${i.enabled ? t('agents.state.used') : t('agents.managed_only')}><i class=${'dot ' + (i.enabled ? 'green' : '')}></i>${i.machine === 'local' ? t('agents.this_machine') : i.machine_name}${!i.installed && html`<em>${t('agents.to_install')}</em>`}${i.version && html`<em>${i.version.replace(/^v/, '').split(' ')[0]}</em>`}</span>`)}</div>`}
    <div class="hx-foot">${!supported ? (rt.id === 'hermes' ? t("harnesses.page.connecte_sa_machine_avec_connecter_une_machine") : t("harnesses.page.adaptateur_en_preparation")) : missing ? t("harnesses.page.installe") + (rt.cli === 'npx' ? t("harnesses.page.node_js_et_le_cli") : rt.cli) + t("harnesses.page.pour_l_utiliser")
      : acp ? (rt.custom ? t("harnesses.page.personnalise") : '') + t("harnesses.page.dossier_outils_et_autorisations_dans_loom") : n ? n + t("harnesses.page.modele_2") + (n > 1 ? 's' : '') + t("harnesses.page.dans_le_selecteur") : t("harnesses.page.ouvre_pour_connecter_ton_compte")}</div>
  </button>`;
}
