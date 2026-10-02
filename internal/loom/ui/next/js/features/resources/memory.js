import { t } from '../../core/i18n.js';
// Mémoire : pages Markdown que l'agent local relit entre les sessions.
// Liste filtrable à gauche, éditeur à droite.
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Tip } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';

const MODES = () => ({ off: t("resources.memory.desactivee"), ondemand: t("resources.memory.a_la_demande") });

export function Memory() {
  const [pages, setPages] = useState(null);
  const [mode, setMode] = useState('');
  const [q, setQ] = useState('');
  const [cur, setCur] = useState(null); // { name, old, content, dirty }
  const load = async () => {
    const a = await get('/api/agent').catch(() => ({}));
    setPages((a.pages || a.skills || []).slice().sort((x, y) => x.name.localeCompare(y.name)));
    setMode(a.mem_mode || '');
  };
  useEffect(() => { load(); }, []);
  const pick = async name => {
    if (cur && cur.dirty && !await confirm(t("resources.memory.quitter_sans_enregistrer"), t("resources.memory.les_modifications_de") + (cur.name || t("resources.memory.nouvelle_page")) + t("resources.memory.seront_perdues"), { ok: t("resources.memory.quitter") })) return;
    const r = await get('/api/mem' + (name ? '?name=' + encodeURIComponent(name) : '')).catch(e => ({ error: e.message }));
    if (r.error) return toast(r.error, 'err');
    setCur({ name: r.name || '', old: r.name || '', content: r.content || '', dirty: false });
  };
  const save = async () => {
    const name = cur.name.trim();
    if (!name) return toast(t("resources.memory.donne_un_nom_a_la_page"), 'err');
    const r = await post('/api/mem/save', { name, old: cur.old, content: cur.content });
    if (!r.ok) return toast(r.error || t("resources.memory.enregistrement_impossible"), 'err');
    toast(t("resources.memory.page_enregistree")); setCur({ ...cur, name, old: name, dirty: false }); load();
  };
  const del = async () => {
    if (!await confirm(t("resources.memory.supprimer_la_page"), '« ' + cur.old + t("resources.memory.sera_supprimee_de_la_memoire"), { ok: t("resources.memory.supprimer"), danger: true })) return;
    const r = await post('/api/mem/delete', { name: cur.old });
    if (!r.ok) return toast(r.error || t("resources.memory.suppression_impossible"), 'err');
    setCur(null); load();
  };
  const shown = (pages || []).filter(p => !q || (p.name + ' ' + (p.desc || '')).toLowerCase().includes(q.toLowerCase()));
  return html`<div class="mem">
    <div class="toolbar">
      <label class="search"><${Icon} n="search" /><input placeholder="${t("resources.memory.filtrer_les_pages")}" aria-label="${t("resources.memory.filtrer_les_pages")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <span class="state">${t("resources.memory.memoire")} ${MODES()[mode] || mode || '…'}<${Tip} text="${t("resources.memory.le_mode_se_regle_dans_reglages_general_les_pages_restent_sur_cett")}" /></span>
      <span class="grow"></span>
      <button class="btn primary" onClick=${() => pick('')}><${Icon} n="plus" />${t("resources.memory.nouvelle_page_2")}</button>
    </div>
    ${pages === null ? html`<div class="skeleton" style="height:220px;margin-top:14px"></div>`
      : !pages.length && !cur ? html`<div class="card" style="margin-top:14px"><${Empty} icon="brain" title="${t("resources.memory.aucune_page")}" text="${t("resources.memory.ecris_ce_que_l_agent_doit_retenir_d_une_session_a_l_autre_prefere")}" >
          <button class="btn primary" onClick=${() => pick('')}>${t("resources.memory.creer_une_page")}</button></${Empty}></div>`
      : html`<div class="mem-grid">
        <div class="card mem-list">${shown.length ? shown.map(p => html`<button type="button" class=${cls('mem-item', cur && cur.old === p.name && 'on')} onClick=${() => pick(p.name)}>
            <b>${p.name}</b>${p.desc && html`<small>${p.desc}</small>`}</button>`) : html`<p class="note" style="padding:12px">${t("resources.memory.aucune_page_ne_correspond")}</p>`}</div>
        ${cur ? html`<div class="card mem-edit">
          <div class="mem-edit-h"><input class="input" aria-label="${t("resources.memory.nom_de_la_page")}" placeholder="${t("resources.memory.nom_de_la_page_2")}" value=${cur.name} onInput=${e => setCur({ ...cur, name: e.target.value, dirty: true })} />
            ${cur.old && html`<button class="icon-btn" aria-label="${t("resources.memory.supprimer_la_page")}" title="${t("resources.memory.supprimer")}" onClick=${del}><${Icon} n="trash" /></button>`}</div>
          <textarea class="textarea mono mem-text" aria-label="${t("resources.memory.contenu")}" spellcheck="false" value=${cur.content} onInput=${e => setCur({ ...cur, content: e.target.value, dirty: true })}></textarea>
          <div class="mem-edit-f"><span class="muted">${cur.dirty ? t("resources.memory.modifications_non_enregistrees") : cur.old ? t("resources.memory.enregistree") : t("resources.memory.nouvelle_page_2")}</span><span class="grow"></span>
            <button class="btn ghost" onClick=${() => setCur(null)}>${t("resources.memory.fermer")}</button><button class="btn primary" disabled=${!cur.dirty} onClick=${save}>${t("resources.memory.enregistrer")}</button></div>
        </div>` : html`<div class="card mem-edit mem-none"><p class="muted">${t("resources.memory.choisis_une_page_a_gauche_ou_cree_en_une")}</p></div>`}
      </div>`}
  </div>`;
}
