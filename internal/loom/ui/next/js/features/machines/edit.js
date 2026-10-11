import { t } from '../../core/i18n.js';
// Link an SSH machine using Loom’s public key and inventory metadata.
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { Tip } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { go, refreshWorkspace } from '../../core/state.js';
import { copyText } from '../../ui/clipboard.js';
import { openTerminalWith } from '../terminals/page.js';

// Ligne « LOOM-MACHINE {…} » collée depuis le terminal de la machine.
export function parseMachineLine(text) {
  const i = String(text || '').indexOf(t("machines.edit.loom_machine"));
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
    if (!r.ok) { setCheck({ error: r.error || t("machines.edit.connexion_impossible") }); return; }
    setCheck(r);
  };
  const save = async () => {
    setBusy('save');
    const r = await post('/api/machines', body()).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t("machines.edit.enregistrement_impossible"), 'err');
    toast(r.machine.name + t("machines.edit.connectee")); await refreshWorkspace(); onClose(r.machine);
  };
  const ready = v.host.trim() && v.user.trim();
  return html`<${Modal} wide title=${machine ? t("environment.page.modifier_prefix") + machine.name : t("machines.edit.connecter_une_machine")} sub="${t("machines.edit.un_autre_ordinateur_ou_tournent_des_harnesses_joint_en_ssh")}" onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>${t("machines.edit.annuler")}</button>
        ${check && check.ok ? html`<button class="btn primary" disabled=${!!busy} onClick=${save}>${busy === 'save' ? t("environment.page.connexion") : t("machines.edit.enregistrer_machine")}</button>`
          : html`<button class="btn primary" disabled=${!!busy || !ready} onClick=${test}>${busy === 'check' ? t("machines.edit.test") : t("machines.edit.tester_la_connexion")}</button>`}`}>
    <div class="mx-steps">
      <div class="mx-step"><span class="mx-n">1</span><div class="mx-body">
        <b>${t("machines.edit.sur_la_machine_distante_colle_ce_bloc_dans_un_terminal")}</b>
        <p class="note">${t("machines.edit.avec_l_utilisateur_qui_fait_tourner_tes_agents_ssh_console_proxmo")} <code>${t("machines.edit.loom_machine_2")}</code>.</p>
        <div class="mx-code"><pre>${setup || '…'}</pre><button class="btn sm" disabled=${!setup} onClick=${async () => { const ok = await copyText(setup); toast(ok ? t("machines.edit.bloc_copie") : t("machines.edit.copie_refusee_selectionne_le_texte"), ok ? '' : 'err'); }}><${Icon} n="copy" />${t("machines.edit.copier")}</button></div>
      </div></div>
      <div class="mx-step"><span class="mx-n">2</span><div class="mx-body">
        <b>${t("machines.edit.colle_ici_la_ligne_affichee")}</b>
        <textarea class="input mono mx-paste" rows="2" placeholder="${t("machines.edit.loom_machine_3")}" value=${paste} onInput=${e => onPaste(e.target.value)}></textarea>
        ${paste && !parseMachineLine(paste) && html`<p class="note warn">${t("machines.edit.ligne_loom_machine_introuvable_copie_toute_la_derniere_ligne")}</p>`}
        <div class="mx-fields">
          <label class="field"><span>${t("machines.edit.nom")}</span><input class="input" value=${v.name} placeholder="${t("machines.edit.ex_hermes_agent")}" onInput=${e => set({ name: e.target.value })} /></label>
          <label class="field"><span>${t("machines.edit.adresse")}<${Tip} text="${t("machines.edit.ip_ou_nom_de_la_machine_sur_ton_reseau_la_ligne_collee_donne_son")}" /></span><input class="input mono" value=${v.host} placeholder="192.168.1.20" onInput=${e => set({ host: e.target.value })} /></label>
          <label class="field"><span>${t("machines.edit.utilisateur")}</span><input class="input mono" value=${v.user} placeholder="${t("machines.edit.moi")}" onInput=${e => set({ user: e.target.value })} /></label>
          <label class="field mx-port"><span>${t("machines.edit.port_ssh")}</span><input class="input mono" inputmode="numeric" value=${v.port} onInput=${e => set({ port: e.target.value.replace(/\D/g, '') })} /></label>
        </div>
      </div></div>
      ${check && html`<div class="mx-step"><span class="mx-n">3</span><div class="mx-body">
        ${check.error ? html`<b>${t("machines.edit.connexion_impossible")}</b><p class="note err">${check.error}</p>`
          : html`<b>${t("machines.edit.connectee_a")} ${check.machine.hostname || check.machine.host}<span class="muted"> · ${check.machine.os} · ${check.machine.home}</span></b>
`}
      </div></div>`}
    </div>
  </${Modal}>`;
}
