// Réglages du serveur Codeman, lus et écrits par son API (/settings, /clis).
// Chaque changement est enregistré aussitôt, réglage par réglage : Codeman
// fusionne la modification dans son fichier. Ce sont les réglages de Codeman
// (tous ses clients les voient), pas des préférences de Loom.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { Group } from '../settings/kit.js';
import { api } from './client.js';
import { agents } from './store.js';
import { CliMark } from './new-session.js';
import { autoResumeDefault, setAutoResumeDefault } from './actions.js';
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
function TextField({ value, placeholder, label, mono, onSave, type = 'text', min, max }) {
  const [v, setV] = useState(value ?? '');
  useEffect(() => { setV(value ?? ''); }, [value]);
  const commit = () => { const next = type === 'number' ? (v === '' ? undefined : Number(v)) : String(v).trim(); if (next !== (value ?? (type === 'number' ? undefined : ''))) onSave(next); };
  return html`<input class=${'input sm' + (mono ? ' mono' : '')} type=${type} aria-label=${label} min=${min} max=${max} value=${v} placeholder=${placeholder}
    onInput=${e => setV(e.target.value)} onBlur=${commit} onKeyDown=${e => { if (e.key === 'Enter') e.currentTarget.blur(); }} />`;
}

function Select({ value, options, onChange, label }) {
  return html`<select class="select sm" value=${value} aria-label=${label} onChange=${e => onChange(e.target.value)}>${options.map(([v, l]) => html`<option value=${v}>${l}</option>`)}</select>`;
}

function Line({ label, tip, children, paired }) {
  const tag = paired ? 'div' : 'label';
  return html`<${tag} role=${paired ? 'group' : undefined} aria-label=${paired ? label : undefined} class="cm-setting-row"><span>${label}${tip && html`<small>${tip}</small>`}</span><span class="cm-setting-control">${children}</span></${tag}>`;
}

async function refreshClis() {
  const list = await api.clis();
  if (Array.isArray(list)) agents.set({ clis: list });
}

export const canInstall = c => c.installed === false && c.stock !== false && !!c.installCommand;

export function InstallCli({ cli, manage }) {
  const [busy, setBusy] = useState(false);
  const install = async () => {
    if (!await confirm(t('agents.cfg.cli.install_title', { cli: cli.label }), t('agents.cfg.cli.install_text', { cmd: cli.installCommand }), { ok: t('agents.cfg.cli.install') })) return;
    setBusy(true);
    try {
      await api.installCli(cli.id);
      toast(t('agents.cfg.cli.installed', { cli: cli.label }));
      await refreshClis();
    } catch (e) { toast(e.message, 'err'); } finally { setBusy(false); }
  };
  return html`<button class="btn sm" disabled=${!manage || busy} title=${manage ? undefined : t('agents.cfg.cli_manage_tip')} onClick=${install}>
    ${busy && html`<span class="spinner"></span>`}${t('agents.cfg.cli.install')}</button>`;
}

function CliList({ settings, save }) {
  const clis = useStore(agents, s => s.clis);
  const [busy, setBusy] = useState('');
  const manage = settings.cliManagementEnabled === true;
  const toggle = async (c, on) => {
    setBusy(c.id);
    try { await api.setCli(c.id, on); await refreshClis(); } catch (e) { toast(e.message, 'err'); } finally { setBusy(''); }
  };
  return html`<${Group} title=${t('agents.cfg.g.clis')}>
    <${Line} label=${t('agents.cfg.cli_manage')} tip=${t('agents.cfg.cli_manage_tip')}><${Switch} checked=${manage} onChange=${v => save({ cliManagementEnabled: v })} /></${Line}>
    ${clis.filter(c => c.kind !== 'shell').map(c => html`<div class="set-line cm-cfg-cli" key=${c.id}>
      <div class="set-l"><${CliMark} mode=${c.id} size="sm" /><span>${c.label}</span>
        ${c.installed === false ? html`<span class="tag">${t('agents.new.not_installed')}</span>` : c.enabled === false ? html`<span class="tag">${t('agents.cfg.cli.off')}</span>` : ''}</div>
      <div class="set-c">
        ${busy === c.id ? html`<span class="spinner"></span>`
          : canInstall(c) ? html`<${InstallCli} cli=${c} manage=${manage} />`
          : html`<${Switch} checked=${c.enabled !== false} disabled=${!manage} label=${c.label} onChange=${v => toggle(c, v)} />`}
      </div></div>`)}
  </${Group}>`;
}

export function CodemanSettings() {
  const clis = useStore(agents, s => s.clis);
  const conn = useStore(agents, s => s.conn);
  const [group, setGroup] = useState('sessions');
  const [resume, setResume] = useState(autoResumeDefault);
  const [settings, setSettings] = useState(null);
  const [error, setError] = useState('');
  const writes = useRef(Promise.resolve());
  useEffect(() => {
    let active = true;
    setError('');
    setSettings(null);
    if (conn.state === 'ready') api.settings().then(value => {
      if (active) {
        setSettings(value || {});
        agents.set({ settings: value || {} });
      }
    }).catch(e => { if (active) setError(e.message); });
    return () => { active = false; };
  }, [conn.url, conn.state]);
  // Codeman merges one setting per write. Keep rapid edits in click order.
  const save = patch => {
    const key = Object.keys(patch)[0];
    setSettings(current => ({ ...current, ...patch }));
    writes.current = writes.current.then(async () => {
      if (agents.get().conn.url !== conn.url) return;
      try {
        await api.saveSettings(patch);
        if (agents.get().conn.url !== conn.url) return;
        agents.set(state => ({ settings: { ...state.settings, ...patch } }));
        toast(t('agents.cfg.saved'));
      } catch (e) {
        if (agents.get().conn.url !== conn.url) return;
        const previous = agents.get().settings[key];
        setSettings(current => current?.[key] === patch[key] ? { ...current, [key]: previous } : current);
        toast(e.status === 403 ? t('agents.cfg.forbidden') : e.message, 'err');
      }
    });
  };
  const link = officialLink('');
  const groups = [
    ['sessions', t('agents.cfg.g.sessions')], ['tracking', t('agents.cfg.g.tracking')],
    ['system', t('agents.cfg.g.system')], ['clis', t('agents.admin.cli_mcp')],
    ['advanced', t('agents.cfg.g.advanced')], ['connection', t('agents.cfg.g.connection')],
  ];
  const connected = conn.state === 'ready' && !error;
  const selected = connected ? group : 'connection';
  const s = settings || {};
  const sw = (key, def = false) => html`<${Switch} checked=${s[key] ?? def} onChange=${v => save({ [key]: v })} />`;
  const agentsList = clis.filter(c => c.kind !== 'shell' && c.enabled !== false && c.installed !== false);
  const nice = s.nice || {};
  const efforts = {
    '': t('agents.cfg.cli_default'), none: t('settings.page.none'), minimal: t('agents.admin.minimal'),
    low: t('agents.admin.low'), medium: t('agents.admin.medium'), high: t('agents.admin.high'),
    xhigh: t('agents.admin.xhigh'), max: t('agents.admin.max'), ultracode: 'Ultracode', ultra: 'Ultra',
  };
  const panels = {
    sessions: () => html`
    <${Group} title=${t('agents.cfg.g.sessions')}>
      <${Line} label=${t('agents.cfg.run_mode')} tip=${t('agents.cfg.run_mode_tip')}><${Select} label=${t('agents.cfg.run_mode')} value=${s.runMode || 'claude'} onChange=${v => save({ runMode: v })}
        options=${(agentsList.length ? agentsList : [{ id: 'claude', label: t('agents.admin.claude') }]).map(c => [c.id, c.label])} /></${Line}>
      <${Line} label=${t('agents.cfg.default_dir')} tip=${t('agents.cfg.default_dir_tip')}><${TextField} mono value=${s.defaultWorkingDir} placeholder="~/Codes" onSave=${v => save({ defaultWorkingDir: v })} /></${Line}>
      <${Line} label=${t('agents.cfg.auto_name')} tip=${t('agents.cfg.auto_name_tip')}>${sw('autoNameSessions')}</${Line}>
      <${Line} label=${t('agents.cfg.pin_name')} tip=${t('agents.cfg.pin_name_tip')}>${sw('pinCliSessionName')}</${Line}>
      <${Line} label=${t('agents.cfg.auto_resume')} tip=${t('agents.cfg.auto_resume_tip')}><${Switch} checked=${resume} onChange=${v => { setAutoResumeDefault(v); setResume(v); toast(t('agents.cfg.saved')); }} /></${Line}>
    </${Group}>

    <${Group} title=${t('agents.admin.claude')}>
      <${Line} label=${t('agents.cfg.claude_mode')} tip=${t('agents.cfg.claude_mode_tip')}><${Select} label=${t('agents.cfg.claude_mode')} value=${s.claudeMode || 'dangerously-skip-permissions'} options=${CLAUDE_MODES()} onChange=${v => save({ claudeMode: v })} /></${Line}>
      ${s.claudeMode === 'allowedTools' && html`<${Line} label=${t('agents.cfg.allowed_tools')} tip=${t('agents.cfg.allowed_tools_tip')}><${TextField} mono value=${s.allowedTools} placeholder="Read,Edit,Bash(git:*)" onSave=${v => save({ allowedTools: v })} /></${Line}>`}
      <${Line} paired label=${t('agents.admin.model_effort')} tip=${t('agents.cfg.claude_model_tip')}><${TextField} label=${t('agents.cfg.claude_model')} mono value=${s.claudeModel} placeholder=${t('agents.cfg.cli_default')} onSave=${v => save({ claudeModel: v })} />
        <${Select} label=${t('agents.cfg.effort')} value=${s.thinkingEffort || ''} onChange=${v => save({ thinkingEffort: v })} options=${EFFORTS.map(e => [e, efforts[e]])} /></${Line}>
      <${Line} label=${t('agents.cfg.context_1m')}>${sw('opusContext1mEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.advisor')} tip=${t('agents.cfg.advisor_tip')}><${Select} label=${t('agents.cfg.advisor')} value=${s.claudeAdvisorModel || ''} onChange=${v => save({ claudeAdvisorModel: v })} options=${ADVISORS.map(e => [e, e || t('agents.cfg.cli_default')])} /></${Line}>
      <details class="cm-settings-extra"><summary>${t('agents.setup.advanced')}</summary>
      <${Line} label=${t('agents.cfg.hooks')} tip=${t('agents.cfg.hooks_tip')}>${sw('workspaceHooksEnabled', true)}</${Line}>
      <${Line} label=${t('agents.cfg.teams')}>${sw('agentTeamsEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.skill')} tip=${t('agents.cfg.skill_tip')}>${sw('agentSkillEnabled')}</${Line}>
      </details>
    </${Group}>

    <${Group} title=${t('agents.admin.codex')}>
      <${Line} paired label=${t('agents.admin.model_reasoning')}><${TextField} label=${t('agents.cfg.codex_model')} mono value=${s.codexModel} placeholder=${t('agents.cfg.cli_default')} onSave=${v => save({ codexModel: v })} />
        <${Select} label=${t('agents.cfg.codex_effort')} value=${s.codexReasoningEffort || ''} onChange=${v => save({ codexReasoningEffort: v })} options=${CODEX_EFFORTS.map(e => [e, efforts[e]])} /></${Line}>
      <${Line} label=${t('agents.cfg.codex_bypass')} tip=${t('agents.cfg.codex_bypass_tip')}>${sw('codexDangerouslyBypassApprovals')}</${Line}>
    </${Group}>
    `,
    tracking: () => html`
    <${Group} title=${t('agents.cfg.g.tracking')}>
      <${Line} label=${t('agents.cfg.plan_usage')} tip=${t('agents.cfg.plan_usage_tip')}>${sw('showPlanUsageLimits', true)}</${Line}>
      <${Line} label=${t('agents.cfg.subagents')}>${sw('subagentTrackingEnabled', true)}</${Line}>
      <${Line} label=${t('agents.cfg.approvals')} tip=${t('agents.cfg.approvals_tip')}>${sw('approvalsInboxEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.ralph')}>${sw('ralphTrackerEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.images')} tip=${t('agents.cfg.images_tip')}>${sw('imageWatcherEnabled')}</${Line}>
      <${Line} label=${t('agents.cfg.readmymind')} tip=${t('agents.cfg.readmymind_tip')}>${sw('readMyMindEnabled')}</${Line}>
    </${Group}>
    `,
    system: () => html`
    <${Group} title=${t('agents.cfg.g.system')}>
      <${Line} label=${t('agents.cfg.tmux_history')} tip=${t('agents.cfg.tmux_history_tip')}><${TextField} type="number" min="1000" max="1000000" value=${s.tmuxHistoryLimit ?? 100000} onSave=${v => v >= 1000 && v <= 1000000 && save({ tmuxHistoryLimit: Math.round(v) })} /></${Line}>
      <${Line} label=${t('agents.cfg.nice')} tip=${t('agents.cfg.nice_tip')}><${Switch} checked=${nice.enabled ?? false} onChange=${v => save({ nice: { ...nice, enabled: v, niceValue: nice.niceValue ?? 10 } })} /></${Line}>
      ${nice.enabled && html`<${Line} label=${t('agents.cfg.nice_value')}><${TextField} type="number" min="-20" max="19" value=${nice.niceValue ?? 10} onSave=${v => v >= -20 && v <= 19 && save({ nice: { ...nice, niceValue: Math.round(v) } })} /></${Line}>`}
    </${Group}>
    `,
    clis: () => html`
    <${CliList} settings=${s} save=${save} />

    <${Group} title=${t('agents.admin.mcp')}>
      <${Line} label=${t('agents.cfg.mcp_sync')} tip=${t('agents.cfg.mcp_sync_tip')}>${sw('mcpSyncEnabled')}</${Line}>
    </${Group}>
    `,
    advanced: () => html`
    <${Group} title=${t('agents.cfg.g.advanced')}>
      <${Line} label=${t('agents.cfg.endpoints')} tip=${t('agents.cfg.endpoints_tip')}>${sw('customModelEndpointsEnabled')}</${Line}>
    </${Group}>
    `,
  };
  return html`<div class="cm-cfg">
    <nav class="cm-cfg-nav" aria-label=${t('agents.admin.groups')}>
      <div class="cm-cfg-links">${groups.map(([id, label]) => html`<button key=${id}
        disabled=${!connected && id !== 'connection'} aria-current=${selected === id ? 'true' : undefined} onClick=${() => setGroup(id)}>${label}</button>`)}</div>
      <label class="cm-cfg-picker"><span>${t('agents.admin.groups')}</span>
        <select class="select" value=${selected} onChange=${e => setGroup(e.target.value)}>
          ${groups.map(([id, label]) => html`<option value=${id} disabled=${!connected && id !== 'connection'}>${label}</option>`)}
        </select></label>
      <p>${t('agents.cfg.lead')}</p>
      ${link && html`<a href=${link} target="codeman" rel="noopener">${t('agents.cfg.all')} <${Icon} n="link" /></a>`}
    </nav>
    <div class="cm-cfg-body" key=${selected}>
    ${conn.multi_user && html`<p class="note">${t('agents.cfg.multi')}</p>`}
    ${error && html`<p class="msg-err">${error}</p>`}
    ${selected === 'connection' ? html`<${ConnectionPanel} embedded />`
      : settings ? panels[selected]() : html`<div class="skeleton cm-admin-loading"></div>`}
    </div>
  </div>`;
}
