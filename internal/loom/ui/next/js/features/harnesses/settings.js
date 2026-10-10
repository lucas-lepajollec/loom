import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore } from '../../core/lib.js';
import { Switch, Tip } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, refreshWorkspace } from '../../core/state.js';

// Source du modèle : Natif (le harness choisit parmi ses modèles) et, quand
// le harness le permet, Loom (tes modèles locaux ajoutés à sa propre liste).
export function ModelSource({ rt, line }) {
  const [x, setX] = useState(null);
  const [busy, setBusy] = useState(false);
  const load = () => get('/api/harness/model-source?id=' + encodeURIComponent(rt.id)).then(setX).catch(() => setX(null));
  useEffect(() => { load(); }, [rt.id]);
  if (!x) return null;
  const toggle = async on => {
    const msg = x.format === 'opencode'
      ? t("harnesses.page.tes_modeles_locaux_et_tes_fournisseurs_cloud_apparaissent_dans_la") + rt.name + t("harnesses.page.quand_loom_le_lance_rien_n_est_ecrit_dans_ses_fichiers_et_les_cle")
      : x.format === 'pi'
      ? t("harnesses.page.loom_ajoute_a") + x.file + t("harnesses.page.un_fournisseur_loom_tes_modeles_locaux_et_un_par_fournisseur_clou")
      : x.format === 'dsh'
      ? t('harnesses.models.dsh_note', { name: rt.name })
      : x.format === 'hermes'
      ? t('harnesses.models.hermes_note', { name: rt.name, file: x.file })
      : x.format === 'env'
      ? t("harnesses.page.tes_modeles_locaux_apparaissent_sous") + rt.name + t("harnesses.page.dans_le_selecteur_quand_tu_en_choisis_un_loom_lance") + rt.name + t("harnesses.page.avec_son_api_locale_comme_fournisseur_aucun_fichier_de") + rt.name + t("harnesses.page.n_est_modifie_et_ses_modeles_natifs_restent_disponibles")
      : t("harnesses.page.loom_ajoute_un_fournisseur_loom_dans") + x.file + t("harnesses.page.avec_tes_modeles_locaux_tes_autres_fournisseurs_ne_sont_pas_modif");
    if (on && !await confirm(t("harnesses.page.modeles_de_loom_dans") + rt.name, msg, { ok: t("harnesses.page.activer") })) return;
    setBusy(true);
    const r = await post('/api/harness/model-source', { id: rt.id, enabled: on });
    setBusy(false);
    if (!r.ok) return toast(r.error, 'err');
    toast(on ? t("harnesses.page.modeles_de_loom_proposes_dans") + rt.name : t("harnesses.page.modeles_de_loom_retires_de") + rt.name);
    load(); refreshWorkspace(); setTimeout(refreshWorkspace, 8000);
  };
  if (line) return html`<div class="set-line"><div class="set-l"><span>${t("harnesses.page.modeles_de_loom")}</span><${Tip} text="${t("harnesses.page.natif_le_harness_utilise_ses_propres_modeles_et_son_compte_loom_t")}" />${x.supported && html`<span class="muted" style="font-size:12px;margin-left:8px">${t('harnesses.models.local', { n: x.models })}</span>`}</div>
    <div class="set-c">${x.supported ? html`<${Switch} checked=${x.enabled} disabled=${busy} label=${t("harnesses.page.proposer_les_modeles_de_loom_dans") + rt.name} onChange=${toggle} />` : html`<span class="muted">${t("harnesses.page.pas_encore_pris_en_charge_par_ce_harness")}</span>`}</div></div>`;
  return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.source_du_modele")}<${Tip} text="${t("harnesses.page.natif_le_harness_utilise_ses_propres_modeles_et_son_compte_loom_t")}" /></h2></div>
    <div class="card">
      <div class="set-line"><div class="set-l"><span>${t("harnesses.page.natif")}</span></div><div class="set-c"><span class="state"><i class="dot green"></i>${t("harnesses.page.toujours_disponible")}</span></div></div>
      <div class="set-line"><div class="set-l"><span>${t("harnesses.page.modeles_de_loom")}</span>${x.supported && html`<span class="muted" style="font-size:12px;margin-left:8px">${t('harnesses.models.local', { n: x.models })}</span>`}</div>
        <div class="set-c">${x.supported ? html`<${Switch} checked=${x.enabled} disabled=${busy} label=${t("harnesses.page.proposer_les_modeles_de_loom_dans") + rt.name} onChange=${toggle} />`
          : html`<span class="muted">${t("harnesses.page.pas_encore_pris_en_charge_par_ce_harness")}</span>`}</div></div>
    </div></section>`;
}

// Ressources Loom reçues par ce harness : serveurs MCP (transmis à chaque
// session) et skills (copiées dans son dossier de skills), choisies une à une.
export function LoomResources({ rt }) {
  const ws = useStore(app, a => a.workspace);
  const skills = (ws && ws.capabilities) || [];
  const [mcp, setMcp] = useState(null);
  const [sk, setSk] = useState(null);
  const remote = (rt.capabilities || []).includes('remote');
  const load = async () => {
    const [m, localT] = await Promise.all([get('/api/harness/bindings?id=' + encodeURIComponent(rt.id)).catch(() => null), get('/api/skills/targets').catch(() => null)]);
    setMcp(m && m.ok ? m : { mcp: [], custom: false });
    const target = ((localT && localT.targets) || []).find(x => x.harnesses.includes(rt.id)) || null;
    setSk({ target, bindings: (localT && localT.bindings) || {} });
  };
  useEffect(() => { load(); }, [rt.id]);
  const setMcpBound = async (name, on) => {
    const names = mcp.mcp.filter(r => (r.name === name ? on : r.bound)).map(r => r.name);
    const r = await post('/api/harness/bindings', { id: rt.id, mcp: names });
    if (!r.ok) return toast(r.error, 'err'); load();
  };
  const resetMcp = async () => { await post('/api/harness/bindings', { id: rt.id, mcp: null }); load(); };
  const enableTarget = async () => {
    const r = await post('/api/skills/targets', { id: sk.target.id, enabled: true });
    if (!r.ok) return toast(r.error, 'err'); load();
  };
  const setSkill = async (id, on) => {
    const r = await post('/api/skills/binding', { skill_id: id, target: sk.target.id, enabled: on });
    if (!r.ok) return toast(r.error, 'err'); load();
  };
  if (!mcp || !sk) return null;
  const bound = id => !(sk.bindings[id] && sk.bindings[id][sk.target.id] === false);
  return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.ressources_loom_transmises")}<${Tip} text=${t("harnesses.page.loom_garde_la_definition_de_tes_skills_et_serveurs_mcp_tu_choisis") + rt.name + t("harnesses.page.recoit_le_harness_reste_maitre_de_leur_execution")} /></h2>
      <a class="btn sm ghost" href="#/brain">${t("harnesses.page.gerer_les_ressources")}</a></div>
    <div class="grid2">
      ${rt.features?.mcp_selection && html`<div class="card pad"><div class="sec-h"><h2>${t("harnesses.page.serveurs_mcp")}</h2>${mcp.custom && html`<button class="btn sm ghost" onClick=${resetMcp}>${t("harnesses.page.tous")}</button>`}</div>
        ${remote ? html`<p class="note">${t("harnesses.page.non_transmis_ce_harness_tourne_sur_une_autre_machine")}</p>`
          : mcp.mcp.length ? mcp.mcp.map(r => html`<div class="kv" key=${r.name}><span>${r.name}${!r.enabled && html` <span class="muted">${t("harnesses.page.desactive_dans_loom")}</span>`}</span>
              <${Switch} checked=${r.bound && r.enabled} disabled=${!r.enabled} label=${t("harnesses.page.transmettre") + r.name + t("harnesses.page.a") + rt.name} onChange=${on => setMcpBound(r.name, on)} /></div>`)
          : html`<p class="note">${t("harnesses.page.aucun_serveur_mcp_dans_loom")}</p>`}</div>`}
      ${rt.features?.skills && html`<div class="card pad"><div class="sec-h"><h2>${t("harnesses.page.skills")}</h2></div>
        ${!sk.target ? html`<p class="note">${t("harnesses.page.loom_ne_connait_pas_encore_le_dossier_de_skills_de_ce_harness")}</p>`
          : !sk.target.enabled ? html`<div class="kv"><span>${t("harnesses.page.distribution_vers")} ${sk.target.dir.replace(/^\/home\/[^/]+/, '~')}</span><button class="btn sm" onClick=${enableTarget}>${t("harnesses.page.activer")}</button></div>`
          : skills.length ? skills.map(c => html`<div class="kv" key=${c.id}><span class="trunc" title=${c.description || ''}>${c.name}</span>
              <${Switch} checked=${bound(c.id)} label=${t("harnesses.page.envoyer") + c.name + t("harnesses.page.a") + rt.name} onChange=${on => setSkill(c.id, on)} /></div>`)
          : html`<p class="note">${t("harnesses.page.aucune_skill_dans_loom")}</p>`}
        ${sk.target && sk.target.harnesses.length > 1 && sk.target.enabled && html`<p class="note" style="margin-top:8px">${t("harnesses.page.dossier_partage_avec")} ${sk.target.harnesses.filter(h => h !== rt.id).join(', ')}.</p>`}</div>`}
    </div></section>`;
}

export function AgentSettings({ rt, enabled = true }) {
  const [resources, setResources] = useState(false);
  return html`${enabled && html`<section class="sec"><div class="sec-h"><h2>${t('agents.settings')}</h2></div>
      <div class="card">
        ${rt.features?.model_sources?.includes('loom') && html`<${ModelSource} rt=${rt} line />`}
        ${(rt.features?.mcp_selection || rt.features?.skills) && html`<div class="set-line"><div class="set-l"><span>${t('agents.resources')}</span><${Tip} text=${t("harnesses.page.loom_garde_la_definition_de_tes_skills_et_serveurs_mcp_tu_choisis") + rt.name + t("harnesses.page.recoit_le_harness_reste_maitre_de_leur_execution")} /></div>
          <div class="set-c"><button class="btn sm ghost" onClick=${() => setResources(true)}>${t('agents.choose')}</button></div></div>`}
        <div class="set-line"><div class="set-l"><span>${t("harnesses.page.dossier_et_autorisations")}</span><${Tip} text=${t("harnesses.page.choisis_les_par_discussion_dans_le_panneau_de_droite_demander_mod")} /></div><div class="set-c"><span class="muted">${t('agents.per_discussion')}</span></div></div>
      </div></section>`}
    ${resources && html`<${Modal} wide title=${t('agents.resources')} onClose=${() => setResources(false)}><${LoomResources} rt=${rt} /></${Modal}>`}`;
}
