// Autorisations : une seule politique (autoriser / confirmer / refuser) pour
// tout ce que Loom décide seul. Par défaut elle reproduit le comportement
// existant ; une règle globale par grande famille se règle ici, les règles
// plus fines (projet, agent, machine) restent listées. Contrat : docs/policy.md.
import { html, useState, useEffect, useStore } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { app } from '../../core/state.js';
import { ListPick } from '../../ui/listpick.js';
import { toast, confirm } from '../../ui/dialog.js';
import { Icon } from '../../ui/icons.js';
import { Line, Group } from './kit.js';

// Familles réglables d'un coup ; « défaut » = pas de règle globale de l'interface.
const FAMILIES = () => [
  ['agent.command', t('policy.f.command'), t('policy.f.command_tip')], ['agent.file_write', t('policy.f.file_write'), t('policy.f.file_write_tip')], ['agent.fetch', t('policy.f.fetch'), t('policy.f.fetch_tip')],
  ['data.send_provider', t('policy.f.send_provider'), t('policy.f.send_provider_tip')], ['mcp.tool:*', t('policy.f.mcp'), t('policy.f.mcp_tip')], ['node.terminal', t('policy.f.terminal'), t('policy.f.terminal_tip')],
  ['node.install', t('policy.f.node_install'), t('policy.f.node_install_tip')], ['node.update', t('policy.f.node_update'), t('policy.f.node_update_tip')],
];
const SUBJECT_LABEL = () => ({ 'agent.command': t('policy.f.command'), 'agent.file_write': t('policy.f.file_write'), 'agent.fetch': t('policy.f.fetch'), 'agent.file_read': t('policy.s.file_read'), 'agent.search': t('policy.s.search'), 'agent.think': t('policy.s.think'), 'agent.tool': t('policy.s.tool'), 'agent.permission_full': t('policy.s.permission_full'), 'agent.filesystem_full': t('policy.s.filesystem_full'), 'data.send_provider': t('policy.f.send_provider'), 'memory.consolidate': t('policy.s.memory'), 'notification.summary': t('policy.s.summary'), 'node.terminal': t('policy.f.terminal'), 'node.install': t('policy.f.node_install'), 'node.update': t('policy.f.node_update'), 'spend.provider': t('policy.s.spend') });
const subjectLabel = s => SUBJECT_LABEL()[s] || (s.startsWith('mcp.tool:') ? t('policy.f.mcp') + ' · ' + s.slice(9) : s);
const DECISION_LABEL = () => ({ allow: t('policy.d.allow'), confirm: t('policy.d.confirm'), deny: t('policy.d.deny') });
const uiId = subject => 'ui-global-' + subject.replace(/[^a-z0-9]+/gi, '-');
const DECISIONS = () => [{ value: '', label: t('policy.d.default'), note: t('policy.d.default_note') }, { value: 'allow', label: t('policy.d.allow') }, { value: 'confirm', label: t('policy.d.confirm') }, { value: 'deny', label: t('policy.d.deny') }];

export function PolicySettings() {
  const ws = useStore(app, a => a.workspace);
  const [doc, setDoc] = useState(null), [busy, setBusy] = useState(false), [audit, setAudit] = useState(null);
  const load = () => get('/api/policy').then(r => setDoc(r.ok === false ? { error: r.error } : r)).catch(e => setDoc({ error: e.message }));
  useEffect(() => { load(); }, []);
  if (!doc) return html`<${Group} title=${t('policy.title')}><div class="pad"><p class="note">${t('startup.loading')}</p></div></${Group}>`;
  if (doc.error) return html`<${Group} title=${t('policy.title')}><div class="pad"><p class="note err">${t('policy.unavailable')}</p></div></${Group}>`;
  const rules = doc.rules || [];
  const save = async next => {
    setBusy(true);
    const r = await post('/api/policy', { ...doc, rules: next }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false || r.error) return toast(r.error || t('policy.save_failed'), 'err');
    setDoc(r); toast(t('policy.saved'));
  };
  const familyValue = subject => (rules.find(r => r.id === uiId(subject)) || {}).decision || '';
  const setFamily = (subject, decision) => {
    const rest = rules.filter(r => r.id !== uiId(subject));
    save(decision ? [...rest, { id: uiId(subject), scope: 'global', subject, decision, conditions: {} }] : rest);
  };
  const projects = Object.fromEntries(((ws && ws.projects) || []).map(p => [p.id, p.name]));
  const agents = Object.fromEntries(((ws && ws.runtimes) || []).map(r => [r.id, r.name]));
  const scopeLabel = r => r.scope === 'global' ? t('policy.scope.global') : r.scope === 'project' ? t('policy.scope.project', { name: projects[r.scope_id] || r.scope_id }) : r.scope === 'agent' ? t('policy.scope.agent', { name: agents[r.scope_id] || r.scope_id }) : t('policy.scope.machine', { name: r.scope_id });
  const other = rules.filter(r => !r.id.startsWith('ui-global-') && !r.migrated);
  const imported = rules.filter(r => r.migrated);
  const remove = async r => { if (await confirm(t('policy.remove_title'), t('policy.remove_text'), { ok: t('policy.remove'), danger: true })) save(rules.filter(x => x.id !== r.id)); };
  const cond = c => [c.inside_workdir && t('policy.c.inside_workdir'), c.command_prefixes && c.command_prefixes.length && t('policy.c.commands', { list: c.command_prefixes.join(', ') }), c.max_cost != null && t('policy.c.max_cost', { n: c.max_cost }), c.legacy_permission && t('policy.c.legacy', { p: c.legacy_permission }), c.provider_id && c.provider_id, c.model && c.model].filter(Boolean).join(' · ');
  return html`
    <${Group} title=${t('policy.title')}>
      <div class="pad"><p class="note">${t('policy.lead')}</p></div>
      ${FAMILIES().map(([subject, label, tip]) => html`<${Line} key=${subject} label=${label} tip=${tip}>
        <div class="set-pick"><${ListPick} label=${label} disabled=${busy} value=${familyValue(subject)} onChange=${v => setFamily(subject, v)} options=${DECISIONS()} /></div></${Line}>`)}
    </${Group}>
    <${Group} title=${t('policy.rules')}>
      ${other.length ? other.map(r => html`<div class="set-line" key=${r.id}>
          <div class="set-l pol-rule"><span><b>${DECISION_LABEL()[r.decision] || r.decision}</b> · ${subjectLabel(r.subject)}</span><small class="muted">${[scopeLabel(r), cond(r.conditions || {}), r.migrated && t('policy.migrated')].filter(Boolean).join(' · ')}</small></div>
          <div class="set-c"><button class="icon-btn" aria-label=${t('policy.remove')} disabled=${busy} onClick=${() => remove(r)}><${Icon} n="trash" /></button></div></div>`)
        : html`<div class="pad"><p class="note">${t('policy.no_rules')}</p></div>`}
    </${Group}>
    ${imported.length > 0 && html`<details class="set-group pol-imported"><summary><h3>${t('policy.imported', { n: imported.length })}</h3></summary>
      <div class="card"><div class="pad"><p class="note">${t('policy.imported_note')}</p></div>
      ${imported.map(r => html`<div class="set-line" key=${r.id}><div class="set-l pol-rule"><span><b>${DECISION_LABEL()[r.decision] || r.decision}</b> · ${subjectLabel(r.subject)}</span><small class="muted">${[scopeLabel(r), cond(r.conditions || {})].filter(Boolean).join(' · ')}</small></div></div>`)}</div></details>`}
    <${Group} title=${t('policy.audit')}>
      ${audit === null ? html`<div class="set-actions"><button class="btn sm ghost" onClick=${() => get('/api/policy/audit').then(r => setAudit((r.entries || []).slice(-40).reverse())).catch(() => setAudit([]))}>${t('policy.audit_show')}</button></div>`
        : audit.length ? audit.map(e => html`<div class="set-line" key=${e.id}><div class="set-l"><span>${subjectLabel(e.subject)}</span><small class="muted">${new Date(e.at).toLocaleTimeString()}${e.dry_run ? ' · ' + t('policy.dry_run') : ''}</small></div><div class="set-c"><span class="note">${DECISION_LABEL()[e.decision] || e.decision}</span></div></div>`)
        : html`<div class="pad"><p class="note">${t('policy.audit_empty')}</p></div>`}
    </${Group}>`;
}
