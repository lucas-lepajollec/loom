import { t } from '../../core/i18n.js';
import { html, useState, useEffect } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tip } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { Lifecycle } from './lifecycle.js';

// Ce que le harness possède déjà sur cette machine, lu dans son CLI et ses
// dossiers : version, compte, clés présentes, MCP, plugins, skills. Mise à jour
// par sa propre commande, sur demande.
const MCP_STATE = () => ({ connected: ['green', t("harnesses.page.connecte_2")], enabled: ['green', t("harnesses.page.active")], 'needs-auth': ['amber', t("harnesses.page.a_authentifier")], disabled: ['', t("harnesses.page.desactive")], error: ['red', 'erreur'] });
export function MachineState({ rt, commands, accountRevision = 0 }) {
  const [x, setX] = useState(null);
  const load = async refresh => {
    const r = await get('/api/runtimes/' + rt.id + '/inspect' + (refresh ? '?refresh=1' : '')).catch(() => null);
    setX(r && r.ok ? r.inspection : false);
  };
  useEffect(() => { load(accountRevision > 0); }, [rt.id, accountRevision]);
  const adopt = async m => {
    const r = await post('/api/runtimes/' + rt.id + '/mcp/adopt', { name: m.name }).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) return toast(r.error || t("harnesses.page.adoption_impossible"), 'err');
    toast(r.name + t("harnesses.page.ajoute_a_loom_desactive") + (r.env_to_fill && r.env_to_fill.length ? t("harnesses.page.a_completer") + r.env_to_fill.join(', ') : ''));
  };
  if (x === false) return null;
  if (!x) return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.sur_cette_machine")}</h2></div><div class="card pad"><div class="state"><span class="spinner"></span>${t("harnesses.page.lecture_de")} ${rt.name}…</div></div></section>`;
  if (!x.installed) return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.sur_cette_machine")}</h2></div><div class="card pad"><${Lifecycle} target="local" id=${rt.id} name=${rt.name} where="sur cette machine" onChange=${() => { setX(null); load(true); }} />${rt.docs ? html`<p class="note"><a href=${rt.docs} target="_blank" rel="noopener noreferrer">${t("harnesses.page.documentation_de")} ${rt.name}</a></p>` : ''}</div></section>`;
  const a = x.auth || {};
  return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.sur_cette_machine")}<${Tip} text=${t("harnesses.page.lu_directement_dans_le_cli_de") + rt.name + t("harnesses.page.et_ses_dossiers_loom_ne_lit_jamais_la_valeur_des_cles_il_indique")} /></h2>
      <button class="btn sm ghost" onClick=${() => { setX(null); load(true); }}><${Icon} n="refresh" />${t("harnesses.page.relire")}</button></div>
    <div class="grid2">
      <div class="card pad">
        <div class="kv"><span>${t("harnesses.page.emplacement")}</span><code class="mono trunc" style="max-width:62%">${x.path.replace(/^\/home\/[^/]+/, '~')}</code></div>
        <div class="kv"><span>${t("harnesses.page.compte")}</span><span class="state"><i class=${'dot ' + (a.connected ? 'green' : '')}></i>${a.connected ? [a.method, a.account].filter(Boolean).join(' · ') || a.status || t("harnesses.page.connecte_2") : a.providers ? t("harnesses.page.aucun_fournisseur") : a.status || t("harnesses.page.non_lu")}</span></div>
        ${a.providers && a.providers.length > 0 && html`<div class="kv"><span>${t("harnesses.page.fournisseurs")}</span><span>${a.providers.join(' · ')}</span></div>`}
        <div class="kv"><span>${t("harnesses.page.cles_d_api_detectees")}</span><span>${x.env.length ? x.env.map(e => html`<span class="tag">${e}</span> `) : html`<span class="muted">${t("harnesses.page.aucune_dans_l_environnement")}</span>`}</span></div>
        <${Lifecycle} target="local" id=${rt.id} name=${rt.name} where="sur cette machine" onChange=${() => load(true)} />
      </div>
      <div class="card pad">
        <div class="kv"><span>${t("harnesses.page.serveurs_mcp_du_harness")}</span><span class="num">${x.mcp_known ? x.mcp.length : t("harnesses.page.non_lus")}</span></div>
        ${x.mcp.map(m => { const st = MCP_STATE()[m.status] || ['', m.status || '']; return html`<div class="kv" key=${m.name}><span class="trunc" title=${m.target || ''}>${m.name}</span><span class="state">${st[1] && html`<i class=${'dot ' + st[0]}></i>`}${st[1]}
          ${(m.command || m.url) && html`<button class="btn sm ghost" title="${t("harnesses.page.copier_ce_serveur_dans_loom_pour_le_donner_aussi_aux_autres_harne")}" onClick=${() => adopt(m)}>${t("harnesses.page.adopter")}</button>`}</span></div>`; })}
        <div class="kv"><span>${t("harnesses.page.plugins")}</span><span class="num">${x.plugins.length}</span></div>
        ${x.plugins.map(p => html`<div class="kv" key=${p}><span>${p}</span><span></span></div>`)}
        <div class="kv"><span>${t("harnesses.page.skills_installees")}</span><span class="num">${x.skills.length}</span></div>
        ${x.skills.map(k => html`<div class="kv" key=${k.folder + k.name}><span class="trunc" title=${k.description || ''}>${k.name}</span>${k.from_loom ? html`<span class="tag">${t("harnesses.page.via_loom")}</span>` : html`<span class="muted mono" style="font-size:11.5px">${k.folder}</span>`}</div>`)}
        ${commands != null && html`<div class="kv"><span>${t("harnesses.page.commandes_disponibles")}<${Tip} text="${t("harnesses.page.commandes_et_skills_que_le_harness_annonce_dans_une_session_plugi")}" /></span><span class="num">${commands}</span></div>`}
      </div>
    </div>
  </section>`;
}
