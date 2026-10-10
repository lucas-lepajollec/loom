import { t } from '../../core/i18n.js';
import { Logo } from '../../ui/logo.js';
import { html, useState } from '../../core/lib.js';
import { Switch, Tip } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { post } from '../../core/api.js';
import { go, refreshWorkspace } from '../../core/state.js';
import { Lifecycle } from './lifecycle.js';
import { Catalogue } from './catalog.js';

// Ajout ou modification d'un harness ACP personnalisé (n'importe quel agent qui
// parle ACP sur stdio, y compris sur une autre machine via ssh).
// Les harnesses d'une autre machine passent par « Connecter une machine ».
const PRESETS = () => ([{ label: t("harnesses.page.agent_sur_cette_machine"), name: '', command: '', args: '', remote: false }]);
const splitArgs = localT => (String(localT).match(/"[^"]*"|'[^']*'|\S+/g) || []).map(a => a.replace(/^(["'])(.*)\1$/, '$2'));
const joinArgs = a => (a || []).map(x => /\s/.test(x) ? '"' + x + '"' : x).join(' ');
export function AddAgentDialog({ installs, onClose, onChanged, onCustom }) {
  const [busy, setBusy] = useState('');
  const detected = installs.filter(i => i.installed && !i.managed);
  const missing = installs.filter(i => i.machine === 'local' && !i.installed);
  const apply = async (i, use) => {
    if (use && !await confirm(t('agents.enable_title', { name: i.name }), t('agents.enable_note', { name: i.name, machine: i.machine_name }), { ok: t('agents.enable_ok') })) return;
    setBusy(i.machine + i.harness);
    const r = await post('/api/agents/installations', { machine: i.machine, harness: i.harness, managed: true, ...(use ? { enabled: true, consent: true } : {}) }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t('agents.change_failed'), 'err');
    await refreshWorkspace(); onChanged(r.installations || []);
  };
  return html`<${Modal} title=${t('agents.add.title')} sub=${t('agents.add.sub')} onClose=${onClose}>
    <div class="add-agents">
      <h4>${t('agents.add.detected')}</h4>
      ${detected.length ? html`<div class="card">${detected.map(i => html`<div class="set-line" key=${i.machine + i.harness}>
          <div class="set-l"><${Logo} name=${i.logo || i.harness} size="sm" /><span>${i.name}</span><span class="muted">· ${i.machine === 'local' ? t('agents.this_machine') : i.machine_name}${i.version ? ' · ' + i.version.split(' ')[0] : ''}</span></div>
          <div class="set-c"><button class="btn sm ghost" disabled=${!!busy} onClick=${() => apply(i, false)}>${t('agents.manage')}</button>
            <button class="btn sm" disabled=${!!busy || !i.ready} onClick=${() => apply(i, true)}>${t('agents.add.manage_use')}</button></div></div>`)}</div>`
        : html`<p class="note">${t('agents.add.none_detected')}</p>`}
      ${missing.length > 0 && html`<h4>${t('agents.add.install')}</h4><div class="card">${missing.map(i => html`<div class="set-line" key=${i.harness}>
          <div class="set-l"><${Logo} name=${i.logo || i.harness} size="sm" /><span>${i.name}</span></div>
          <div class="set-c"><${Lifecycle} compact target="local" id=${i.harness} name=${i.name} where=${t('agents.this_machine')} onChange=${async action => { if (action === 'install') await post('/api/agents/installations', { machine: 'local', harness: i.harness, managed: true, enabled: true, consent: true }).catch(() => {}); await refreshWorkspace(); onChanged(null); }} /></div></div>`)}</div>
        <p class="note">${t('agents.add.install_note')}</p>`}
      <h4>${t('agents.catalog.title')}</h4>
      <${Catalogue} onAdded=${id => { onClose(); if (id) go('harnesses', id); }} />
      <h4>${t('agents.add.custom')}</h4>
      <div class="card"><div class="set-line"><div class="set-l"><span>${t('agents.add.custom_text')}</span></div><div class="set-c"><button class="btn sm ghost" onClick=${onCustom}>${t('agents.add.custom_btn')}</button></div></div></div>
    </div></${Modal}>`;
}

export function CustomDialog({ agent, onClose }) {
  const [v, setV] = useState(agent ? { name: agent.name, command: agent.command, args: joinArgs(agent.args), remote: !!agent.remote } : { ...PRESETS()[0] });
  const [busy, setBusy] = useState(false);
  const set = patch => setV({ ...v, ...patch });
  const save = async () => {
    if (/<hôte>/.test(v.args)) return toast(t("harnesses.page.remplace_hote_par_ta_machine_ex_hermes_lxc"), 'err');
    setBusy(true);
    const r = await post('/api/harness/custom', { id: agent ? agent.id : '', name: v.name, command: v.command, args: splitArgs(v.args), remote: v.remote });
    setBusy(false);
    if (!r.ok) return toast(r.error || t("harnesses.page.enregistrement_impossible"), 'err');
    toast(v.name + t("harnesses.page.ajoute")); await refreshWorkspace(); onClose(r.agent);
  };
  return html`<${Modal} title=${agent ? t("environment.page.modifier_prefix") + agent.name : t("harnesses.page.ajouter_un_harness")} sub="${t("harnesses.page.tout_agent_qui_parle_acp_agent_client_protocol_sur_stdio")}" onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>${t("harnesses.page.annuler")}</button><button class="btn primary" disabled=${busy || !v.name || !v.command} onClick=${save}>${t("harnesses.page.enregistrer")}</button>`}>
    <label class="field"><span>${t("harnesses.page.nom")}</span><input class="input" value=${v.name} placeholder="${t("harnesses.page.ex_hermes")}" onInput=${e => set({ name: e.target.value })} /></label>
    <label class="field"><span>${t("harnesses.page.commande")}</span><input class="input mono" value=${v.command} placeholder="${t("harnesses.page.ex_ssh_npx_opencode")}" onInput=${e => set({ command: e.target.value })} /></label>
    <label class="field"><span>${t("harnesses.page.arguments")}</span><input class="input mono" value=${v.args} placeholder="${t("harnesses.page.ex_t_hermes_lxc_hermes_acp")}" onInput=${e => set({ args: e.target.value })} /></label>
    <div class="set-line" style="padding:4px 0;border:0"><div class="set-l"><span>${t("harnesses.page.sur_une_autre_machine")}</span><${Tip} text="${t("harnesses.page.le_dossier_de_travail_est_alors_un_chemin_sur_cette_machine_la_lo")}" /></div>
      <div class="set-c"><${Switch} checked=${v.remote} label="${t("harnesses.page.sur_une_autre_machine")}" onChange=${on => set({ remote: on })} /></div></div>
  </${Modal}>`;
}
