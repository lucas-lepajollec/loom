// Codeman's settings are server-wide. Scalar fields save independently;
// Codeman replaces nested settings objects, so edits to them are serialized.
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useLayoutEffect, useRef, useStore } from '../../core/lib.js';
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
const CLAUDE_EFFORTS = ['', 'low', 'medium', 'high', 'xhigh', 'max', 'ultracode'];
const CODEX_EFFORTS = ['', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'];
const HISTORY_BOUNDS = { min: 1000, max: 1000000 };
const BUFFER_BOUNDS = { min: 1048576, max: 134217728 };
let writes = Promise.resolve();

function TextField({ value, placeholder, label, mono, onSave, type = 'text', min, max, step, maxlength, required, disabled }) {
  const [draft, setDraft] = useState(value ?? '');
  useLayoutEffect(() => { setDraft(value ?? ''); }, [value]);
  const commit = e => {
    const field = e.currentTarget;
    const next = type === 'number' ? field.valueAsNumber : field.value.trim();
    if (!field.validity.valid || (type === 'number' && !Number.isFinite(next)) || (required && next === '')) {
      setDraft(value ?? '');
      return;
    }
    setDraft(next);
    if (next !== (value ?? '')) onSave(next);
  };
  return html`<input class=${'input sm' + (mono ? ' mono' : '')} type=${type} aria-label=${label} min=${min} max=${max} step=${step} maxlength=${maxlength}
    disabled=${disabled} required=${required} value=${draft} placeholder=${placeholder} onInput=${e => setDraft(e.target.value)}
    onBlur=${commit} onKeyDown=${e => { if (e.key === 'Enter') e.currentTarget.blur(); }} />`;
}

function Select({ value, options, onChange, label, disabled }) {
  const list = options.some(([v]) => v === value) ? options : [[value, value], ...options];
  return html`<select class="select sm" value=${value} aria-label=${label} disabled=${disabled} onChange=${e => onChange(e.target.value)}>
    ${list.map(([v, l]) => html`<option key=${v} value=${v}>${l}</option>`)}
  </select>`;
}

function Line({ label, tip, cli, children }) {
  return html`<div role="group" aria-label=${label} class="cm-setting-row">
    <span>${cli ? html`<span class="cm-cfg-cli"><${CliMark} mode=${cli} size="sm" />${label}</span>` : label}
      ${tip && html`<small>${tip}</small>`}</span><span class="cm-setting-control">${children}</span>
  </div>`;
}

function JSONField({ label, tip, value, array, onSave, disabled }) {
  const initial = JSON.stringify(value ?? (array ? [] : {}), null, 2);
  const [draft, setDraft] = useState(initial);
  const [error, setError] = useState('');
  useLayoutEffect(() => { setDraft(initial); setError(''); }, [initial]);
  const save = () => {
    try {
      const decoded = JSON.parse(draft);
      if (!decoded || typeof decoded !== 'object' || Array.isArray(decoded) !== !!array) throw new Error();
      onSave(decoded);
      setError('');
    } catch (_) { setError(t('agents.cfg.invalid_json')); }
  };
  return html`<div class="cm-setting-json">
    <label class="field"><span>${label}</span><small>${tip}</small>
      <textarea class="textarea mono" rows="4" value=${draft} disabled=${disabled} onInput=${e => setDraft(e.target.value)}></textarea>
    </label>
    ${error && html`<small class="cm-run-err" role="alert">${error}</small>`}
    <button class="btn sm" type="button" disabled=${disabled || draft === initial} onClick=${save}>${t('agents.save')}</button>
  </div>`;
}

function VoiceKey({ configured, onSave, disabled }) {
  const [draft, setDraft] = useState('');
  const remove = async () => {
    if (await confirm(t('agents.cfg.voice_key_clear'), t('agents.cfg.voice_key_clear_tip'), { ok: t('agents.cfg.voice_key_clear'), danger: true })) {
      onSave('');
    }
  };
  return html`<div class="cm-setting-json">
    <label class="field"><span>${t('agents.cfg.voice_key')}</span>
      <input class="input sm" type="password" autocomplete="new-password" maxlength="200" value=${draft} disabled=${disabled}
        placeholder=${configured ? t('agents.cfg.voice_key_set') : t('agents.cfg.voice_key_empty')} onInput=${e => setDraft(e.target.value)} />
    </label>
    <div class="cm-key-actions">
      ${configured && html`<button class="btn sm ghost danger" type="button" disabled=${disabled} onClick=${remove}>${t('agents.cfg.voice_key_clear')}</button>`}
      <button class="btn sm" type="button" disabled=${disabled || !draft.trim()} onClick=${() => { onSave(draft.trim()); setDraft(''); }}>${t('agents.save')}</button>
    </div>
  </div>`;
}

async function refreshClis() {
  const conn = agents.get().conn;
  const list = await api.clis();
  const current = agents.get().conn;
  if (current.url === conn.url && current.username === conn.username && Array.isArray(list)) agents.set({ clis: list });
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

function CliList({ manage, readOnly }) {
  const clis = useStore(agents, s => s.clis);
  const [busy, setBusy] = useState('');
  const toggle = async (c, on) => {
    setBusy(c.id);
    try { await api.setCli(c.id, on); await refreshClis(); }
    catch (e) { toast(e.message, 'err'); }
    finally { setBusy(''); }
  };
  return html`${clis.filter(c => c.kind !== 'shell').map(c => html`<${Line} key=${c.id}
    label=${c.label} cli=${c.id}
    tip=${c.installed === false ? t('agents.new.not_installed') : c.enabled === false ? t('agents.cfg.cli.off') : ''}>
    ${canInstall(c) ? html`<${InstallCli} cli=${c} manage=${manage && !readOnly} />`
      : html`<${Switch} checked=${c.enabled !== false} disabled=${!manage || readOnly || busy === c.id} label=${c.label} onChange=${v => toggle(c, v)} />`}
  </${Line}>`)}`;
}

function fields(values, change, disabled) {
  const sw = (key, label, fallback = false, tip) => html`<${Line} label=${label} tip=${tip}>
    <${Switch} checked=${values[key] ?? fallback} disabled=${disabled} label=${label} onChange=${v => change(key, v)} />
  </${Line}>`;
  const input = (key, label, opts = {}) => html`<${TextField} ...${opts} label=${label} value=${values[key] ?? opts.default} disabled=${disabled} onSave=${v => change(key, v)} />`;
  const text = (key, label, opts) => html`<${Line} label=${label} tip=${opts?.tip}>${input(key, label, opts)}</${Line}>`;
  const number = (key, label, opts) => text(key, label, { ...opts, type: 'number' });
  const selectInput = (key, label, options, fallback = '') => html`<${Select} label=${label} value=${values[key] ?? fallback} options=${options}
    disabled=${disabled} onChange=${v => change(key, v)} />`;
  const select = (key, label, options, fallback, tip) => html`<${Line} label=${label} tip=${tip}>${selectInput(key, label, options, fallback)}</${Line}>`;
  return { sw, text, number, select, input, selectInput };
}

function CaseDefaults({ s, cases, save, disabled }) {
  const names = [...new Set([...cases.map(c => c.name), ...Object.keys(s.caseSettings || {})])];
  const [selected, setSelected] = useState('');
  const current = names.includes(selected) ? selected : names[0];
  const options = names.map(name => [name, name]);
  const perCase = s.caseSettings?.[current] || {};
  const update = (key, value) => save(settings => ({
    caseSettings: { [current]: {
      agentTeams: settings.caseSettings?.[current]?.agentTeams ?? !!settings.agentTeamsEnabled,
      opusContext1m: settings.caseSettings?.[current]?.opusContext1m ?? !!settings.opusContext1mEnabled,
      [key]: value,
    } },
  }), api.saveSettings, (settings, patch) => ({
    ...settings, caseSettings: { ...settings.caseSettings, ...patch.caseSettings },
  }));
  const { sw } = fields(perCase, update, disabled || !s.syncCaseState);
  const shared = fields(s, (key, value) => save({ [key]: value }), disabled);
  return html`<${Group} title=${t('agents.cfg.case_defaults')}>
    ${shared.sw('syncCaseState', t('agents.cfg.case_sync'), false, t('agents.cfg.case_sync_tip'))}
    ${shared.select('lastUsedCase', t('agents.cfg.case_last_used'), [['', t('settings.page.none')], ...options])}
    ${shared.select('lastPickedCase', t('agents.cfg.case_last_picked'), [['', t('settings.page.none')], ...options])}
    ${s.syncCaseState && current && html`<${Line} label=${t('agents.cfg.case_pick')}><${Select} label=${t('agents.cfg.case_pick')} value=${current}
      options=${options} onChange=${setSelected} /></${Line}>
      ${sw('agentTeams', t('agents.cfg.teams'), !!s.agentTeamsEnabled)}
      ${sw('opusContext1m', t('agents.cfg.context_1m'), !!s.opusContext1mEnabled)}`}
    <${JSONField} label=${t('agents.cfg.case_order')} tip=${t('agents.cfg.case_order_tip')} array value=${s.caseOrder || cases.map(c => c.name)}
      disabled=${disabled} onSave=${order => save({ caseOrder: order }, patch => api.saveCaseOrder(patch.caseOrder))} />
  </${Group}>`;
}

function Notifications({ s, save, disabled }) {
  const prefs = s.notificationPreferences || {};
  const editPref = (key, value) => save(state => ({
    notificationPreferences: { ...state.notificationPreferences, _version: Math.max(5, state.notificationPreferences?._version || 0), [key]: value },
  }));
  const editEvent = (event, defaults, channel, value) => save(state => {
    const current = state.notificationPreferences || {};
    const eventTypes = current.eventTypes || {};
    return { notificationPreferences: {
      ...current, _version: Math.max(5, current._version || 0),
      eventTypes: { ...eventTypes, [event]: { ...defaults, ...eventTypes[event], [channel]: value } },
    } };
  });
  const { sw, number } = fields(prefs, editPref, disabled);
  const events = [
    ['permission_prompt', t('agents.cfg.event_permission'), true, true, true],
    ['elicitation_dialog', t('agents.cfg.event_question'), true, true, true],
    ['idle_prompt', t('agents.cfg.event_idle'), true, true],
    ['stop', t('agents.cfg.event_stop')],
    ['session_error', t('agents.cfg.event_error'), true, true],
    ['respawn_cycle', t('agents.cfg.event_respawn'), true],
    ['token_milestone', t('agents.cfg.event_tokens'), true],
    ['ralph_complete', t('agents.cfg.event_ralph'), true, true, true],
    ['subagent_spawn', t('agents.cfg.event_subagent_start')],
    ['subagent_complete', t('agents.cfg.event_subagent_end')],
  ];
  return html`<${Group} title=${t('agents.cfg.g.notifications')}>
    ${sw('enabled', t('agents.cfg.notifications_on'), true)}
    ${sw('browserNotifications', t('agents.cfg.notify_browser'), true)}
    ${sw('audioAlerts', t('agents.cfg.notify_audio'))}
    ${number('stuckThresholdMs', t('agents.cfg.notify_stuck'), { min: 60000, max: 7200000, default: 600000 })}
    ${number('toastDurationMs', t('agents.cfg.notify_toast'), { min: 1000, max: 300000, default: 3000 })}
    ${number('browserAutoCloseMs', t('agents.cfg.notify_close'), { min: 1000, max: 300000, default: 8000 })}
    ${sw('muteCritical', t('agents.cfg.notify_mute_critical'))}
    ${sw('muteWarning', t('agents.cfg.notify_mute_warning'))}
    ${sw('muteInfo', t('agents.cfg.notify_mute_info'))}
  </${Group}>
  <${Group} title=${t('agents.cfg.notify_events')}>
    ${events.map(([event, label, enabled = false, browser = false, audio = false]) => {
      const defaults = { enabled, browser, audio, push: false };
      const controls = fields(prefs.eventTypes?.[event] || {}, (channel, value) => editEvent(event, defaults, channel, value), disabled);
      return html`<details class="cm-settings-extra" key=${event}><summary>${label}</summary>
        ${controls.sw('enabled', t('agents.cron.enabled'), enabled)}
        ${controls.sw('browser', t('agents.cfg.notify_browser'), browser)}
        ${controls.sw('audio', t('agents.cfg.notify_audio'), audio)}
        ${controls.sw('push', t('agents.cfg.notify_push'))}
      </details>`;
    })}
  </${Group}>`;
}

function VoiceSettings({ s, save, disabled }) {
  const voice = s.voiceSettings || {};
  const edit = (key, value) => save(state => {
    const current = state.voiceSettings || {};
    return { voiceSettings: { ...current, [key === 'apiKey' ? 'apiKeySet' : key]: key === 'apiKey' ? !!value : value } };
  }, () => api.saveSettings({ voiceSettings: { [key]: value } }));
  const { sw } = fields(s, (key, value) => save({ [key]: value }), disabled);
  const v = fields({ ...voice, silenceTimeout: voice.silenceTimeout == null ? '' : String(voice.silenceTimeout) },
    (key, value) => edit(key, key === 'silenceTimeout' ? (value === '' ? null : Number(value)) : value), disabled);
  const displayNames = new Intl.DisplayNames(locale(), { type: 'language' });
  const languages = ['en-US', 'en-GB', 'es', 'fr', 'de', 'ja'].map(lang => [lang, displayNames.of(lang)]);
  return html`<${Group} title=${t('agents.cfg.voice')}>
    ${sw('claudeVoiceEnabled', t('agents.cfg.voice_claude'), false, t('agents.cfg.voice_claude_tip'))}
    ${v.select('provider', t('agents.cfg.voice_provider'), [
      ['auto', t('agents.cfg.auto')], ['claude', t('agents.admin.claude')], ['deepgram', t('agents.cfg.deepgram')], ['webspeech', t('agents.cfg.webspeech')],
    ], 'auto')}
    ${v.select('language', t('agents.cfg.voice_language'), [...languages, ['multi', t('agents.cfg.auto')]], 'en-US')}
    ${v.text('keyterms', t('agents.cfg.voice_keywords'), { maxlength: 500 })}
    ${v.select('insertMode', t('agents.cfg.voice_insert'), [['direct', t('agents.cfg.voice_direct')], ['compose', t('agents.cfg.voice_compose')]], 'direct')}
    ${v.select('silenceTimeout', t('agents.cfg.voice_silence'), [
      ['', t('agents.cfg.provider_default')], ['0', t('agents.cfg.voice_manual')],
      ...[3, 6, 15, 30].map(n => [String(n), t('agents.cfg.seconds', { n })]),
    ])}
    <${VoiceKey} configured=${voice.apiKeySet === true} disabled=${disabled} onSave=${value => edit('apiKey', value)} />
  </${Group}>`;
}

function SettingsPanel({ group, s, save, disabled }) {
  const clis = useStore(agents, state => state.clis);
  const cases = useStore(agents, state => state.cases);
  const [resume, setResume] = useState(autoResumeDefault);
  const change = (key, value) => save({ [key]: value });
  const nested = (parent, key, value) => save(state => ({ [parent]: { ...state[parent], [key]: value } }));
  const { sw, text, number, select, input, selectInput } = fields(s, change, disabled);
  const nice = fields(s.nice || {}, (key, value) => nested('nice', key, value), disabled);
  const efforts = {
    '': t('agents.cfg.cli_default'), none: t('settings.page.none'), minimal: t('agents.admin.minimal'),
    low: t('agents.admin.low'), medium: t('agents.admin.medium'), high: t('agents.admin.high'),
    xhigh: t('agents.admin.xhigh'), max: t('agents.admin.max'), ultracode: t('agents.cfg.ultracode'), ultra: t('agents.cfg.ultra'),
  };
  const tunnel = async enabled => {
    if (!enabled || await confirm(t('agents.cfg.tunnel'), t('agents.cfg.tunnel_tip'), { ok: t('agents.cfg.tunnel_enable') })) {
      change('tunnelEnabled', enabled);
    }
  };
  const panels = {
    sessions: () => html`
      <${Group} title=${t('agents.cfg.g.sessions')}>
        ${select('runMode', t('agents.cfg.run_mode'), clis.filter(c => c.enabled !== false && c.installed !== false).map(c => [c.id, c.label]), 'claude', t('agents.cfg.run_mode_tip'))}
        ${text('defaultWorkingDir', t('agents.cfg.default_dir'), { mono: true, maxlength: 500, tip: t('agents.cfg.default_dir_tip') })}
        ${text('defaultClaudeMdPath', t('agents.cfg.claude_md'), { mono: true, maxlength: 500 })}
        ${sw('autoNameSessions', t('agents.cfg.auto_name'), false, t('agents.cfg.auto_name_tip'))}
        ${sw('pinCliSessionName', t('agents.cfg.pin_name'), false, t('agents.cfg.pin_name_tip'))}
        <${Line} label=${t('agents.cfg.auto_resume')} tip=${t('agents.cfg.auto_resume_tip')}>
          <${Switch} label=${t('agents.cfg.auto_resume')} checked=${resume} onChange=${v => { setAutoResumeDefault(v); setResume(v); toast(t('agents.cfg.saved')); }} />
        </${Line}>
        ${sw('promptCacheKeepWarm', t('agents.cfg.cache_warm'))}
        ${number('promptCacheKeepWarmMaxHours', t('agents.cfg.cache_hours'), { min: 0.5, max: 24, step: 'any', default: 2 })}
      </${Group}>
      <${Group} title=${t('agents.admin.claude')}>
        ${select('claudeMode', t('agents.cfg.claude_mode'), CLAUDE_MODES(), 'dangerously-skip-permissions', t('agents.cfg.claude_mode_tip'))}
        ${s.claudeMode === 'allowedTools' && text('allowedTools', t('agents.cfg.allowed_tools'), { mono: true, maxlength: 2000, tip: t('agents.cfg.allowed_tools_tip') })}
        <${Line} label=${t('agents.admin.model_effort')} tip=${t('agents.cfg.claude_model_tip')}>
          ${input('claudeModel', t('agents.cfg.claude_model'), { mono: true, maxlength: 50, placeholder: t('agents.cfg.cli_default') })}
          ${selectInput('thinkingEffort', t('agents.cfg.effort'), CLAUDE_EFFORTS.map(e => [e, efforts[e]]))}
        </${Line}>
        ${sw('opusContext1mEnabled', t('agents.cfg.context_1m'))}
        ${select('claudeAdvisorModel', t('agents.cfg.advisor'), [['', t('agents.cfg.cli_default')], ['sonnet', t('agents.cfg.sonnet')], ['opus', t('agents.cfg.opus')], ['fable', t('agents.cfg.fable')]], '', t('agents.cfg.advisor_tip'))}
        ${sw('workspaceHooksEnabled', t('agents.cfg.hooks'), true, t('agents.cfg.hooks_tip'))}
        ${sw('agentTeamsEnabled', t('agents.cfg.teams'))}
        ${sw('agentSkillEnabled', t('agents.cfg.skill'), true, t('agents.cfg.skill_tip'))}
      </${Group}>
      <${Group} title=${t('agents.admin.codex')}>
        <${Line} label=${t('agents.admin.model_reasoning')}>
          ${input('codexModel', t('agents.cfg.codex_model'), { mono: true, maxlength: 100, placeholder: t('agents.cfg.cli_default') })}
          ${selectInput('codexReasoningEffort', t('agents.cfg.codex_effort'), CODEX_EFFORTS.map(e => [e, efforts[e]]))}
        </${Line}>
        ${sw('codexDangerouslyBypassApprovals', t('agents.cfg.codex_bypass'), false, t('agents.cfg.codex_bypass_tip'))}
        ${sw('codexNetworkAccess', t('agents.cfg.codex_network'))}
        ${sw('codexAnimationsEnabled', t('agents.cfg.codex_animations'), true)}
      </${Group}>
      <${CaseDefaults} s=${s} cases=${cases} save=${save} disabled=${disabled} />
    `,
    general: () => html`
      <${Group} title=${t('agents.cfg.g.general')}>
        ${text('displayName', t('agents.cfg.display_name'), { maxlength: 40, required: true, default: 'Codeman' })}
        <${Line} label=${t('agents.cfg.tunnel')} tip=${t('agents.cfg.tunnel_tip')}>
          <${Switch} label=${t('agents.cfg.tunnel')} checked=${s.tunnelEnabled} disabled=${disabled} onChange=${tunnel} />
        </${Line}>
      </${Group}>
      <${Group} title=${t('agents.cfg.tab_group')}>
        ${select('sessionListLayout', t('agents.cfg.tab_list'), [['header', t('agents.cfg.tab_header')], ['sidebar', t('agents.cfg.tab_sidebar')], ['sidebar-rich', t('agents.cfg.tab_sidebar_rich')]], 'header')}
        ${select('tabOrientation', t('agents.cfg.tab_direction'), [['horizontal', t('agents.cfg.horizontal')], ['vertical', t('agents.cfg.vertical')]], 'horizontal')}
        ${select('tabArrangement', t('agents.cfg.tab_arrangement'), [['classic', t('agents.cfg.classic')], ['state', t('agents.cfg.by_state')], ['case', t('agents.cfg.by_case')], ['ledger', t('agents.cfg.by_grid')]], 'classic')}
        ${select('tabStateOrder', t('agents.cfg.tab_state_order'), [['urgent-first', t('agents.cfg.urgent_first')], ['urgent-last', t('agents.cfg.urgent_last')]], 'urgent-first')}
        ${select('tabRailSort', t('agents.cfg.tab_sort'), [['activity', t('agents.cfg.sort_activity')], ['manual', t('agents.cfg.sort_manual')]], 'activity')}
        ${select('tabRailDetail', t('agents.cfg.tab_detail'), [['simple', t('agents.cfg.simple')], ['rich', t('agents.cfg.rich')]], 'rich')}
        ${number('tabRailWidth', t('agents.cfg.tab_width'), { min: 208, max: 360, default: 256 })}
        ${number('sessionSidebarFontSize', t('agents.cfg.tab_font'), { min: 11, max: 18, default: 12 })}
        ${sw('tabTwoRows', t('agents.cfg.tab_two'))}
        ${sw('showTabCliLogos', t('agents.cfg.tab_logos'), true)}
        ${sw('spawnedTabsFollowParent', t('agents.cfg.tab_follow'))}
      </${Group}>
      <${Group} title=${t('agents.cfg.panel_group')}>
        ${select('headerStatsStyle', t('agents.cfg.stats_style'), [['classic', t('agents.cfg.classic')], ['compact', t('agents.cfg.compact')], ['tiles', t('agents.cfg.by_grid')]], 'compact')}
        ${sw('showFontControls', t('agents.cfg.show_font'))}
        ${sw('showSystemStats', t('agents.cfg.show_stats'))}
        ${sw('showTokenCount', t('agents.cfg.show_tokens'))}
        ${sw('showCost', t('agents.cfg.show_cost'))}
        ${sw('showLifecycleLog', t('agents.cfg.show_lifecycle'))}
        ${sw('showResponseViewer', t('agents.cfg.show_response'))}
        ${sw('showMonitor', t('agents.cfg.show_monitor'))}
        ${sw('showProjectInsights', t('agents.cfg.show_insights'))}
        ${sw('showFileBrowser', t('agents.cfg.show_files'))}
        ${sw('showSubagents', t('agents.cfg.show_subagents'))}
        ${sw('showMultiMonitorButton', t('agents.cfg.show_multi_monitor'))}
        ${sw('showRedrawButton', t('agents.cfg.show_redraw'))}
        ${sw('gestureControlEnabled', t('agents.cfg.gestures'))}
      </${Group}>`,
    tracking: () => html`
      <${Group} title=${t('agents.cfg.g.tracking')}>
        ${sw('showPlanUsageLimits', t('agents.cfg.plan_usage'), true, t('agents.cfg.plan_usage_tip'))}
        ${sw('subagentTrackingEnabled', t('agents.cfg.subagents'), true)}
        ${sw('subagentActiveTabOnly', t('agents.cfg.subagents_active'), true)}
        ${sw('approvalsInboxEnabled', t('agents.cfg.approvals'), false, t('agents.cfg.approvals_tip'))}
        ${sw('ralphTrackerEnabled', t('agents.cfg.ralph'))}
        ${sw('showUltracodeAgents', t('agents.cfg.ultracode_panel'))}
        ${sw('ultracodeFloatingWindows', t('agents.cfg.ultracode_windows'))}
        ${sw('imageWatcherEnabled', t('agents.cfg.images'), false, t('agents.cfg.images_tip'))}
        ${sw('readMyMindEnabled', t('agents.cfg.readmymind'), false, t('agents.cfg.readmymind_tip'))}
        ${text('readMyMindModel', t('agents.cfg.mind_model'), { maxlength: 100, placeholder: t('agents.cfg.cli_default'), mono: true })}
      </${Group}>`,
    notifications: () => html`
      <${Notifications} s=${s} save=${save} disabled=${disabled} />
      <${VoiceSettings} s=${s} save=${save} disabled=${disabled} />`,
    system: () => html`
      <${Group} title=${t('agents.cfg.g.system')}>
        ${number('tmuxHistoryLimit', t('agents.cfg.tmux_history'), { ...HISTORY_BOUNDS, default: 100000, tip: t('agents.cfg.tmux_history_tip') })}
        ${number('terminalScrollbackLines', t('agents.cfg.scrollback'), { ...HISTORY_BOUNDS, default: 50000 })}
        ${number('terminalBufferMaxBytes', t('agents.cfg.buffer_max'), { ...BUFFER_BOUNDS, min: s.terminalBufferTrimBytes ?? BUFFER_BOUNDS.min, default: 32 * 1024 * 1024 })}
        ${number('terminalBufferTrimBytes', t('agents.cfg.buffer_trim'), { ...BUFFER_BOUNDS, max: s.terminalBufferMaxBytes ?? BUFFER_BOUNDS.max, default: 24 * 1024 * 1024 })}
        ${nice.sw('enabled', t('agents.cfg.nice'), false, t('agents.cfg.nice_tip'))}
        ${nice.number('niceValue', t('agents.cfg.nice_value'), { min: -20, max: 19, default: 10 })}
        ${sw('remoteAutoReconnect', t('agents.cfg.remote_reconnect'), true)}
      </${Group}>
      <${Group} title=${t('agents.cfg.backups')}>
        ${sw('configBackupEnabled', t('agents.cfg.backups_on'), true)}
        ${text('configBackupDir', t('agents.cfg.backups_folder'), { mono: true, maxlength: 1024, placeholder: t('agents.cfg.provider_default') })}
        ${number('configBackupKeepCount', t('agents.cfg.backups_count'), { min: 2, max: 500, default: 20 })}
        ${number('configBackupKeepDays', t('agents.cfg.backups_days'), { min: 0, max: 3650, default: 30, tip: t('agents.cfg.backups_days_tip') })}
      </${Group}>
      <${Group} title=${t('agents.insp.git')}>
        ${sw('gitFetchEnabled', t('agents.cfg.git_fetch'))}
        ${number('gitFetchIntervalSeconds', t('agents.cfg.git_interval'), { min: 30, max: 3600, default: 300 })}
      </${Group}>`,
    clis: () => html`
      <${Group} title=${t('agents.cfg.g.clis')}>
        ${sw('cliManagementEnabled', t('agents.cfg.cli_manage'), false, t('agents.cfg.cli_manage_tip'))}
        <${CliList} manage=${s.cliManagementEnabled === true} readOnly=${disabled} />
      </${Group}>
      <${Group} title=${t('agents.admin.mcp')}>
        ${sw('mcpSyncEnabled', t('agents.cfg.mcp_sync'), false, t('agents.cfg.mcp_sync_tip'))}
      </${Group}>`,
    advanced: () => html`
      <${Group} title=${t('agents.cfg.g.advanced')}>
        ${sw('customModelEndpointsEnabled', t('agents.cfg.endpoints'), false, t('agents.cfg.endpoints_tip'))}
        ${sw('agentInboxPersistEnabled', t('agents.cfg.persist_inbox'))}
        ${sw('agentCoordinationEnabled', t('agents.cfg.coordination'))}
      </${Group}>
      <${Group} title=${t('agents.cfg.execution')}>
        <${JSONField} label=${t('agents.cfg.execution_models')} tip=${t('agents.cfg.execution_models_tip')} value=${s.modelConfig} disabled=${disabled}
          onSave=${config => save({ modelConfig: config }, patch => api.saveModelConfig(patch.modelConfig))} />
        <${JSONField} label=${t('agents.cfg.respawn_presets')} tip=${t('agents.cfg.respawn_presets_tip')} array value=${s.respawnPresets} disabled=${disabled}
          onSave=${presets => change('respawnPresets', presets)} />
      </${Group}>`,
  };
  return panels[group]();
}

const merge = (current, patch) => ({ ...current, ...patch });

export function CodemanSettings() {
  const conn = useStore(agents, s => s.conn);
  const [group, setGroup] = useState('sessions');
  const [settings, setSettings] = useState(null);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  const record = useRef(null);
  const connected = conn.state === 'ready';
  const readOnly = conn.multi_user && conn.role === 'user';
  useEffect(() => {
    const source = { url: conn.url, username: conn.username, saved: {}, pending: [] };
    record.current = source;
    setError('');
    setSettings(null);
    if (connected) writes.then(() => record.current === source && api.settings()).then(value => {
      if (record.current === source) {
        source.saved = value || {};
        setSettings(source.saved);
        agents.set({ settings: source.saved });
      }
    }).catch(e => { if (record.current === source) setError(e.message); });
    return () => { record.current = null; };
  }, [conn.url, conn.username, connected, retry]);

  const save = (patch, send = api.saveSettings, apply = merge) => {
    const source = record.current;
    if (!source || !connected || readOnly) return;
    const getPatch = typeof patch === 'function' ? patch : () => patch;
    const edit = current => apply(current, getPatch(current));
    const syncDraft = () => {
      if (record.current === source) setSettings(source.pending.reduce((current, pending) => pending(current), source.saved));
    };
    const currentConnection = () => {
      const current = agents.get().conn;
      return current.state === 'ready' && current.url === source.url && current.username === source.username;
    };
    source.pending.push(edit);
    syncDraft();
    writes = writes.then(async () => {
      try {
        if (!currentConnection()) return;
        await send(getPatch(source.saved));
        if (!currentConnection()) return;
        source.saved = edit(source.saved);
        agents.set({ settings: source.saved });
        toast(t('agents.cfg.saved'));
      } catch (e) {
        if (currentConnection()) toast(e.code === 'FORBIDDEN' && conn.multi_user ? t('agents.cfg.forbidden') : e.message, 'err');
      } finally {
        source.pending.shift();
        syncDraft();
      }
    });
  };
  const groups = [
    ['sessions', t('agents.cfg.g.sessions')], ['general', t('agents.cfg.g.general')], ['tracking', t('agents.cfg.g.tracking')],
    ['notifications', t('agents.cfg.g.notifications')], ['system', t('agents.cfg.g.system')], ['clis', t('agents.admin.cli_mcp')],
    ['advanced', t('agents.cfg.g.advanced')], ['connection', t('agents.cfg.g.connection')],
  ];
  const selected = connected ? group : 'connection';
  const link = officialLink('');
  return html`<div class="cm-cfg">
    <nav class="cm-cfg-nav" aria-label=${t('agents.admin.groups')}>
      <div class="cm-cfg-links">${groups.map(([id, label]) => html`<button key=${id}
        disabled=${!connected && id !== 'connection'} aria-current=${selected === id ? 'true' : undefined} onClick=${() => setGroup(id)}>${label}</button>`)}</div>
      <label class="cm-cfg-picker"><span>${t('agents.admin.groups')}</span>
        <select class="select" value=${selected} onChange=${e => setGroup(e.target.value)}>
          ${groups.map(([id, label]) => html`<option key=${id} value=${id} disabled=${!connected && id !== 'connection'}>${label}</option>`)}
        </select></label>
      <p>${t('agents.cfg.lead')}</p>
      ${link && html`<a href=${link} target="codeman" rel="noopener">${t('agents.official')} <${Icon} n="link" /></a>`}
    </nav>
    <div class="cm-cfg-body" key=${selected}>
      ${selected === 'connection' ? html`<${ConnectionPanel} embedded />`
        : error ? html`<div class="cm-settings-error"><p class="msg-err" role="alert">${error}</p>
            <button class="btn sm" onClick=${() => setRetry(n => n + 1)}>${t('agents.cfg.retry')}</button></div>`
        : settings ? html`${readOnly && html`<p class="note">${t('agents.cfg.multi')}</p>`}
            <${SettingsPanel} group=${selected} s=${settings} save=${save} disabled=${readOnly} />`
        : html`<div class="skeleton cm-admin-loading"></div>`}
    </div>
  </div>`;
}
