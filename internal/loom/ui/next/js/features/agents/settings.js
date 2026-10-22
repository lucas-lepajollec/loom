// Réglages du serveur Codeman, lus et écrits par son API (/settings, /clis).
// Chaque changement est enregistré aussitôt, réglage par réglage : Codeman
// fusionne la modification dans son fichier. Ce sont les réglages de Codeman
// (tous ses clients les voient), pas des préférences de Loom.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { Line, Group } from '../settings/kit.js';
import { api } from './client.js';
import { agents } from './store.js';
import { CliMark } from './new-session.js';
import { ConnectionPanel } from './connection.js';
import { officialLink } from './nav.js';

const CLAUDE_MODES = () => [
  ['dangerously-skip-permissions', t('agents.cfg.mode.skip')],
  ['auto', t('agents.cfg.mode.auto')],
  ['normal', t('agents.cfg.mode.normal')],
  ['allowedTools', t('agents.cfg.mode.allowed')],
];
const EFFORTS = ['', 'low', 'medium', 'high', 'xhigh', 'max', 'ultracode'];
const CODEX_EFFORTS = ['', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'];
const ADVISORS = ['', 'sonnet', 'opus', 'fable'];

// Champ texte enregistré à la sortie du champ (ou Entrée), pas à chaque frappe.
function TextField({ value, placeholder, mono, onSave, type = 'text', min, max }) {
  const [v, setV] = useState(value ?? '');
  useEffect(() => { setV(value ?? ''); }, [value]);
  const commit = () => { const next = type === 'number' ? (v === '' ? undefined : Number(v)) : String(v).trim(); if (next !== (value ?? (type === 'number' ? undefined : ''))) onSave(next); };
  return html`<input class=${'input sm' + (mono ? ' mono' : '')} type=${type} min=${min} max=${max} value=${v} placeholder=${placeholder}
    onInput=${e => setV(e.target.value)} onBlur=${commit} onKeyDown=${e => { if (e.key === 'Enter') e.currentTarget.blur(); }} />`;
}

function Select({ value, options, onChange, label }) {
  return html`<select class="select sm" value=${value} aria-label=${label} onChange=${e => onChange(e.target.value)}>${options.map(([v, l]) => html`<option value=${v}>${l}</option>`)}</select>`;
}

function CliList({ settings, save }) {
  const clis = useStore(agents, s => s.clis);
  const [busy, setBusy] = useState('');
  const manage = settings.cliManagementEnabled === true;
  const refresh = () => api.clis().then(list => Array.isArray(list) && agents.set({ clis: list })).catch(() => {});
  const toggle = async (c, on) => {
    setBusy(c.id);
    try { await api.setCli(c.id, on); await refresh(); } catch (e) { toast(e.message, 'err'); } finally { setBusy(''); }
  };
  const install = async c => {
    if (!await confirm(t('agents.cfg.cli.install_title', { cli: c.label }), t('agents.cfg.cli.install_text', { cmd: c.installCommand || '' }), { ok: t('agents.cfg.cli.install') })) return;
    setBusy(c.id);
    try { await api.installCli(c.id); toast(t('agents.cfg.cli.installed', { cli: c.label })); await refresh(); } catch (e) { toast(e.message, 'err'); } finally { setBusy(''); }
  };
  return html`<${Group} title=${t('agents.cfg.g.clis')}>
    <${Line} label=${t('agents.cfg.cli_manage')} tip=${t('agents.cfg.cli_manage_tip')}><${Switch} checked=${manage} onChange=${v => save({ cliManagementEnabled: v })} /></${Line}>
    ${clis.filter(c => c.kind !== 'shell').map(c => html`<div class="set-line cm-cfg-cli" key=${c.id}>
      <div class="set-l"><${CliMark} mode=${c.id} size="sm" /><span>${c.label}</span>
        ${c.installed === false ? html`<span class="tag">${t('agents.new.not_installed')}</span>` : c.enabled === false ? html`<span class="tag">${t('agents.cfg.cli.off')}</span>` : ''}</div>
      <div class="set-c">
        ${busy === c.id ? html`<span class="spinner"></span>`
          : c.installed === false && c.stock !== false && c.installCommand ? html`<button class="btn sm" disabled=${!manage} title=${manage ? '' : t('agents.cfg.cli_manage_tip')} onClick=${() => install(c)}><${Icon} n="download" />${t('agents.cfg.cli.install')}</button>`
          : html`<${Switch} checked=${c.enabled !== false} disabled=${!manage} label=${c.label} onChange=${v => toggle(c, v)} />`}
      </div></div>`)}
  </${Group}>`;
}

export function CodemanSettings() {
  const clis = useStore(agents, s => s.clis);
  const multi = useStore(agents, s => s.conn.multi_user);
  const [settings, setSettings] = useState(null);
  const [error, setError] = useState('');
  useEffect(() => { api.settings().then(s => setSettings(s || {})).catch(e => setError(e.message)); }, []);
  // Enregistre une modification ; en cas de refus, l'affichage revient à la valeur réelle.
  const save = async patch => {
    const before = settings;
    setSettings({ ...settings, ...patch });
    try { await api.saveSettings(patch); toast(t('agents.cfg.saved')); agents.set({ settings: { ...before, ...patch } }); }
    catch (e) { setSettings(before); toast(e.status === 403 ? t('agents.cfg.forbidden') : e.message, 'err'); }
  };
  const link = officialLink('');
  if (error) return html`<div class="cm-cfg"><p class="msg-err"><${Icon} n="alert" /><span>${error}</span></p><${ConnectionPanel} embedded /></div>`;
  if (!settings) return html`<div class="cm-cfg"><div class="skeleton" style="height:240px"></div></div>`;
  const s = settings;
  const sw = (key, def = false) => html`<${Switch} checked=${s[key] ?? def} onChange=${v => save({ [key]: v })} />`;
  const agentsList = clis.filter(c => c.kind !== 'shell' && c.enabled !== false && c.installed !== false);
  const nice = s.nice || {};
  return html`<div class="cm-cfg">
    <div class="cm-cfg-h"><div><h1>${t('agents.cfg.title')}</h1><p class="muted">${t('agents.cfg.lead')}</p></div>
      ${link && html`<a class="btn sm" href=${link} target="codeman" rel="noopener"><${Icon} n="link" />${t('agents.cfg.all')}</a>`}</div>
    ${multi && html`<p class="note">${t('agents.cfg.multi')}</p>`}

    <${Group} title=${t('agents.cfg.g.sessions')}>
      <${Line} label=${t('agents.cfg.run_mode')} tip=${t('agents.cfg.run_mode_tip')}><${Select} label=${t('agents.cfg.run_mode')} value=${s.runMode || 'claude'} onChange=${v => save({ runMode: v })}
        options=${(agentsList.length ? agentsList : [{ id: 'claude', label: 'Claude Code' }]).map(c => [c.id, c.label])} /></${Line}>
      <${Line} label=${t('agents.cfg.default_dir')} tip=${t('agents.cfg.default_dir_tip')}><${TextField} mono value=${s.defaultWorkingDir} placeholder="~/Codes" onSave=${v => save({ defaultWorkingDir: v })} /></${Line}>
      <${Line} label=${t('agents.cfg.auto_name')} tip=${t('agents.cfg.auto_name_tip')}>${sw('autoNameSessions')}</${Line}>
      <${Line} label=${t('agents.cfg.pin_name')} tip=${t('agents.cfg.pin_name_tip')}>${sw('pinCliSessionName')}</${Line}>
    </${Group}>

    <${Group} title="Claude Code">
      <${Line} label=${t('agents.cfg.claude_mode')} tip=${t('agents.cfg.claude_mode_tip')}><${Select} label=${t('agents.cfg.claude_mode')} value=${s.claudeMode || 'dangerously-skip-permissions'} options=${CLAUDE_MODES()} onChange=${v => save({ claudeMode: v })} /></${Line}>
      ${s.claudeMode === 'allowedTools' && html`<${Line} label=${t('agents.cfg.allowed_tools')} tip=${t('agents.cfg.allowed_tools_tip')}><${TextField} mono value=${s.allowedTools} placeholder="Read,Edit,Bash(git:*)" onSave=${v => save({ allowedTools: v })} /></${Line}>`}
      <${Line} label=${t('agents.cfg.claude_model')} tip=${t('agents.cfg.claude_model_tip')}><${TextField} mono value=${s.claudeModel} placeholder=${t('agents.cfg.cli_default')} onSave=${v => save({ claudeModel: v })} /></${Line}>
      <${Line} label=${t('agents.cfg.context_1m')}>${sw('opusContext1mEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.effort')}><${Select} label=${t('agents.cfg.effort')} value=${s.thinkingEffort || ''} onChange=${v => save({ thinkingEffort: v })} options=${EFFORTS.map(e => [e, e || t('agents.cfg.cli_default')])} /></${Line}>
      <${Line} label=${t('agents.cfg.advisor')} tip=${t('agents.cfg.advisor_tip')}><${Select} label=${t('agents.cfg.advisor')} value=${s.claudeAdvisorModel || ''} onChange=${v => save({ claudeAdvisorModel: v })} options=${ADVISORS.map(e => [e, e || t('agents.cfg.cli_default')])} /></${Line}>
      <${Line} label=${t('agents.cfg.hooks')} tip=${t('agents.cfg.hooks_tip')}>${sw('workspaceHooksEnabled', true)}</${Line}>
      <${Line} label=${t('agents.cfg.teams')}>${sw('agentTeamsEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.skill')} tip=${t('agents.cfg.skill_tip')}>${sw('agentSkillEnabled')}</${Line}>
    </${Group}>

    <${Group} title="Codex">
      <${Line} label=${t('agents.cfg.codex_model')}><${TextField} mono value=${s.codexModel} placeholder=${t('agents.cfg.cli_default')} onSave=${v => save({ codexModel: v })} /></${Line}>
      <${Line} label=${t('agents.cfg.codex_effort')}><${Select} label=${t('agents.cfg.codex_effort')} value=${s.codexReasoningEffort || ''} onChange=${v => save({ codexReasoningEffort: v })} options=${CODEX_EFFORTS.map(e => [e, e || t('agents.cfg.cli_default')])} /></${Line}>
      <${Line} label=${t('agents.cfg.codex_bypass')} tip=${t('agents.cfg.codex_bypass_tip')}>${sw('codexDangerouslyBypassApprovals')}</${Line}>
    </${Group}>

    <${Group} title=${t('agents.cfg.g.tracking')}>
      <${Line} label=${t('agents.cfg.plan_usage')} tip=${t('agents.cfg.plan_usage_tip')}>${sw('showPlanUsageLimits', true)}</${Line}>
      <${Line} label=${t('agents.cfg.subagents')}>${sw('subagentTrackingEnabled', true)}</${Line}>
      <${Line} label=${t('agents.cfg.approvals')} tip=${t('agents.cfg.approvals_tip')}>${sw('approvalsInboxEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.ralph')}>${sw('ralphTrackerEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.images')} tip=${t('agents.cfg.images_tip')}>${sw('imageWatcherEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.readmymind')} tip=${t('agents.cfg.readmymind_tip')}>${sw('readMyMindEnabled')}</${Line}>
    </${Group}>

    <${Group} title=${t('agents.cfg.g.system')}>
      <${Line} label=${t('agents.cfg.tmux_history')} tip=${t('agents.cfg.tmux_history_tip')}><${TextField} type="number" min="1000" max="1000000" value=${s.tmuxHistoryLimit ?? 100000} onSave=${v => v >= 1000 && v <= 1000000 && save({ tmuxHistoryLimit: Math.round(v) })} /></${Line}>
      <${Line} label=${t('agents.cfg.nice')} tip=${t('agents.cfg.nice_tip')}><${Switch} checked=${nice.enabled ?? false} onChange=${v => save({ nice: { ...nice, enabled: v, niceValue: nice.niceValue ?? 10 } })} /></${Line}>
      ${nice.enabled && html`<${Line} label=${t('agents.cfg.nice_value')}><${TextField} type="number" min="-20" max="19" value=${nice.niceValue ?? 10} onSave=${v => v >= -20 && v <= 19 && save({ nice: { ...nice, niceValue: Math.round(v) } })} /></${Line}>`}
    </${Group}>

    <${CliList} settings=${s} save=${save} />

    <${Group} title=${t('agents.cfg.g.advanced')}>
      <${Line} label=${t('agents.cfg.endpoints')} tip=${t('agents.cfg.endpoints_tip')}>${sw('customModelEndpointsEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.mcp_sync')} tip=${t('agents.cfg.mcp_sync_tip')}>${sw('mcpSyncEnabled')}</${Line}>
    </${Group}>

    <section class="set-group"><h3>${t('agents.cfg.g.connection')}</h3><${ConnectionPanel} embedded /></section>
  </div>`;
}
