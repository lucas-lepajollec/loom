import { t } from '../../core/i18n.js';
// Machines distantes : un autre ordinateur joignable en SSH où tournent des
// harnesses (ex. Hermes dans un conteneur). L'utilisateur colle un bloc sur la
// machine ; il autorise la clé de Loom et affiche une ligne LOOM-MACHINE que
// Loom lit pour remplir le formulaire. Loom vérifie ensuite la connexion et
// propose les harnesses trouvés là-bas.
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { Tip } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { go, refreshWorkspace } from '../../core/state.js';
import { copyText } from '../../ui/clipboard.js';
import { openTerminalWith } from '../terminals/page.js';
import { Lifecycle } from './lifecycle.js';

export { copyText };


// Ligne « LOOM-MACHINE {…} » collée depuis le terminal de la machine.
export function parseMachineLine(text) {
  const i = String(text || '').indexOf(t("harnesses.machines.loom_machine"));
  if (i < 0) return null;
  const rest = text.slice(i + 13);
  const end = rest.lastIndexOf('}');
  try { return JSON.parse(rest.slice(0, end + 1)); } catch (_) { return null; }
}

export function MachineDialog({ machine, onClose }) {
  const [setup, setSetup] = useState('');
  const [paste, setPaste] = useState('');
  const [v, setV] = useState(machine ? { id: machine.id, name: machine.name, host: machine.host, user: machine.user, port: String(machine.port || 22) } : { name: '', host: '', user: '', port: '22' });
  const [check, setCheck] = useState(null);
  const [chosen, setChosen] = useState(machine ? (machine.harnesses || []).map(id => id.slice(('custom-' + machine.id + '-').length)) : []);
  const [busy, setBusy] = useState('');
  useEffect(() => { get('/api/machines').then(r => r.ok && setSetup(r.setup)).catch(() => {}); }, []);
  const set = patch => { setV({ ...v, ...patch }); setCheck(null); };
  const onPaste = text => {
    setPaste(text);
    const info = parseMachineLine(text);
    if (!info) return;
    set({ host: info.host || v.host, user: info.user || v.user, name: v.name || info.hostname || info.host || '' });
  };
  const body = () => ({ machine: { id: v.id || '', name: v.name.trim(), host: v.host.trim(), user: v.user.trim(), port: Number(v.port) || 22 } });
  const test = async () => {
    setBusy('check');
    const r = await post('/api/machines', { ...body(), check_only: true }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) { setCheck({ error: r.error || t("harnesses.machines.connexion_impossible") }); return; }
    setCheck(r);
    if (!machine) setChosen(r.offers.filter(o => o.ready).map(o => o.id));
  };
  const save = async () => {
    setBusy('save');
    const r = await post('/api/machines', { ...body(), harnesses: chosen }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t("harnesses.machines.enregistrement_impossible"), 'err');
    toast(r.machine.name + t("harnesses.machines.connectee")); await refreshWorkspace(); onClose(r.machine);
  };
  const toggle = id => setChosen(chosen.includes(id) ? chosen.filter(x => x !== id) : [...chosen, id]);
  const ready = v.host.trim() && v.user.trim();
  const offers = check && check.offers ? check.offers.filter(o => o.installed) : [];
  return html`<${Modal} wide title=${machine ? t("environment.page.modifier_prefix") + machine.name : t("harnesses.machines.connecter_une_machine")} sub="${t("harnesses.machines.un_autre_ordinateur_ou_tournent_des_harnesses_joint_en_ssh")}" onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>${t("harnesses.machines.annuler")}</button>
        ${check && check.ok ? html`<button class="btn primary" disabled=${!!busy} onClick=${save}>${busy === 'save' ? t("environment.page.connexion") : t("harnesses.machines.enregistrer_machine")}</button>`
          : html`<button class="btn primary" disabled=${!!busy || !ready} onClick=${test}>${busy === 'check' ? t("harnesses.machines.test") : t("harnesses.machines.tester_la_connexion")}</button>`}`}>
    <div class="mx-steps">
      <div class="mx-step"><span class="mx-n">1</span><div class="mx-body">
        <b>${t("harnesses.machines.sur_la_machine_distante_colle_ce_bloc_dans_un_terminal")}</b>
        <p class="note">${t("harnesses.machines.avec_l_utilisateur_qui_fait_tourner_tes_agents_ssh_console_proxmo")} <code>${t("harnesses.machines.loom_machine_2")}</code>.</p>
        <div class="mx-code"><pre>${setup || '…'}</pre><button class="btn sm" disabled=${!setup} onClick=${async () => { const ok = await copyText(setup); toast(ok ? t("harnesses.machines.bloc_copie") : t("harnesses.machines.copie_refusee_selectionne_le_texte"), ok ? '' : 'err'); }}><${Icon} n="copy" />${t("harnesses.machines.copier")}</button></div>
      </div></div>
      <div class="mx-step"><span class="mx-n">2</span><div class="mx-body">
        <b>${t("harnesses.machines.colle_ici_la_ligne_affichee")}</b>
        <textarea class="input mono mx-paste" rows="2" placeholder="${t("harnesses.machines.loom_machine_3")}" value=${paste} onInput=${e => onPaste(e.target.value)}></textarea>
        ${paste && !parseMachineLine(paste) && html`<p class="note warn">${t("harnesses.machines.ligne_loom_machine_introuvable_copie_toute_la_derniere_ligne")}</p>`}
        <div class="mx-fields">
          <label class="field"><span>${t("harnesses.machines.nom")}</span><input class="input" value=${v.name} placeholder="${t("harnesses.machines.ex_hermes_agent")}" onInput=${e => set({ name: e.target.value })} /></label>
          <label class="field"><span>${t("harnesses.machines.adresse")}<${Tip} text="${t("harnesses.machines.ip_ou_nom_de_la_machine_sur_ton_reseau_la_ligne_collee_donne_son")}" /></span><input class="input mono" value=${v.host} placeholder="192.168.1.20" onInput=${e => set({ host: e.target.value })} /></label>
          <label class="field"><span>${t("harnesses.machines.utilisateur")}</span><input class="input mono" value=${v.user} placeholder="${t("harnesses.machines.moi")}" onInput=${e => set({ user: e.target.value })} /></label>
          <label class="field mx-port"><span>${t("harnesses.machines.port_ssh")}</span><input class="input mono" inputmode="numeric" value=${v.port} onInput=${e => set({ port: e.target.value.replace(/\D/g, '') })} /></label>
        </div>
      </div></div>
      ${check && html`<div class="mx-step"><span class="mx-n">3</span><div class="mx-body">
        ${check.error ? html`<b>${t("harnesses.machines.connexion_impossible")}</b><p class="note err">${check.error}</p>`
          : html`<b>${t("harnesses.machines.connectee_a")} ${check.machine.hostname || check.machine.host}<span class="muted"> · ${check.machine.os} · ${check.machine.home}</span></b>
            <p class="note">${t("harnesses.machines.harnesses_facultatifs")}</p>
            ${offers.length ? html`<div class="mx-offers">${offers.map(o => html`<label class=${cls('mx-offer', !o.ready && 'off')} key=${o.id}>
                <input type="checkbox" disabled=${!o.ready} checked=${chosen.includes(o.id)} onChange=${() => toggle(o.id)} />
                <${Logo} name=${o.logo} /><span class="grow"><b>${o.name}</b><small>${o.ready ? o.version || t("harnesses.machines.installe") : o.missing}</small></span></label>`)}</div>`
              : html`<p class="note">${t("harnesses.machines.aucun_harness_pris_en_charge_trouve_sur_cette_machine_hermes_clau")}</p>`}`}
      </div></div>`}
    </div>
  </${Modal}>`;
}

const NAMES = { hermes: 'Hermes', 'claude-code': 'Claude Code', codex: 'Codex', pi: 'Pi', opencode: 'OpenCode' };

export function MachinesSection({ onEdit }) {
  const [data, setData] = useState(null);
  const load = () => get('/api/machines').then(r => setData(r.ok ? r : { machines: [] }), () => setData({ machines: [] }));
  useEffect(() => { load(); }, []);
  MachinesSection.reload = load;
  const [manage, setManage] = useState(null);
  const remove = async m => {
    if (!await confirm(t("harnesses.machines.retirer") + m.name, t("harnesses.machines.ses_harnesses_disparaissent_de_loom_les_discussions_deja_faites_r"), { ok: t("harnesses.machines.retirer_2"), danger: true })) return;
    const r = await post('/api/machines/delete', { id: m.id });
    if (!r.ok) return toast(r.error || t("harnesses.machines.suppression_impossible"), 'err');
    await refreshWorkspace(); load();
  };
  if (!data || !data.machines.length) return null;
  return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.machines.machines_distantes")} <span class="count">${data.machines.length}</span><${Tip} text="${t("harnesses.machines.loom_s_y_connecte_en_ssh_avec_sa_propre_cle_et_lance_les_harnesse")}" /></h2></div>
    <div class="card">${data.machines.map(m => html`<div class="mx-row" key=${m.id}>
      <span class="mx-ico"><${Icon} n="server" /></span>
      <span class="grow"><b>${m.name}</b><small class="mono">${m.user}@${m.host}${m.port !== 22 ? ':' + m.port : ''}</small></span>
      <span class="mx-hs">${(m.harnesses || []).map(id => { const h = id.slice(('custom-' + m.id + '-').length); return html`<button class="chip-btn" key=${id} onClick=${() => go('harnesses', id)}><${Logo} name=${h} />${NAMES[h] || h}</button>`; })}</span>
      <button class="btn sm ghost" onClick=${() => setManage(m)}>${t("harnesses.machines.harnesses")}</button>
      <button class="btn sm ghost" onClick=${() => openTerminalWith({ target: m.id, dir: m.home || '', title: m.name })}><${Icon} n="prompt" />${t("harnesses.machines.terminal")}</button>
      <button class="btn sm ghost" onClick=${() => onEdit(m)}>${t("harnesses.machines.modifier")}</button>
      <button class="icon-btn" aria-label=${t("harnesses.machines.retirer") + m.name} onClick=${() => remove(m)}><${Icon} n="trash" /></button>
    </div>`)}</div>
    ${manage && html`<${MachineHarnesses} m=${manage} onClose=${() => { setManage(null); load(); }} />`}</section>`;
}

// Harnesses d'une machine connectée : installer, mettre à jour, puis les
// ajouter à Loom (ils apparaissent alors dans le sélecteur).
function MachineHarnesses({ m, onClose }) {
  const [offers, setOffers] = useState(null);
  const [busy, setBusy] = useState('');
  const load = () => get('/api/machines').then(r => setOffers((r.offers || {})[m.id] || [])).catch(() => setOffers([]));
  useEffect(() => { load(); }, [m.id]);
  const added = id => (m.harnesses || []).includes('custom-' + m.id + '-' + id);
  const add = async o => {
    setBusy(o.id);
    const ids = (m.harnesses || []).map(h => h.slice(('custom-' + m.id + '-').length));
    const r = await post('/api/machines', { machine: { id: m.id, name: m.name, host: m.host, user: m.user, port: m.port }, harnesses: [...new Set([...ids, o.id])] }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t("harnesses.machines.ajout_impossible"), 'err');
    m.harnesses = r.machine.harnesses; toast(o.name + t("harnesses.machines.ajoute_a_loom")); await refreshWorkspace(); load();
  };
  return html`<${Modal} wide title=${t("harnesses.machines.harnesses_sur") + m.name} sub="${t("harnesses.machines.installe_ou_mets_a_jour_les_harnesses_de_cette_machine_puis_ajout")}" onClose=${onClose}
      foot=${html`<button class="btn" onClick=${onClose}>${t("harnesses.machines.fermer")}</button>`}>
    ${!offers ? html`<div class="state"><span class="spinner"></span>${t("harnesses.machines.lecture_de_la_machine")}</div>`
      : html`<div class="mh-list">${offers.map(o => html`<div class="mh-item card pad" key=${o.id}>
          <div class="mh-head"><${Logo} name=${o.logo} /><b>${o.name}</b><span class="grow"></span>
            ${added(o.id) ? html`<span class="state"><i class="dot green"></i>${t("harnesses.machines.dans_loom")}</span>`
              : o.ready ? html`<button class="btn sm" disabled=${busy === o.id} onClick=${() => add(o)}>${t("harnesses.machines.ajouter_a_loom")}</button>`
              : o.installed && o.missing ? html`<span class="state err">${o.missing}</span>` : ''}</div>
          <${Lifecycle} target=${m.id} id=${o.id} name=${o.name} where=${t("harnesses.machines.sur") + m.name} onChange=${load} />
        </div>`)}</div>`}
  </${Modal}>`;
}
