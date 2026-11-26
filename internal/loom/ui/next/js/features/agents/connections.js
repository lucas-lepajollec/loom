// Agents › Connexions : ce que les agents de cette machine peuvent utiliser.
// En haut l'accès MCP à Loom, puis le dossier des capacités (skills/ et
// tasks/, versionné par l'utilisateur), l'état de ses skills dans chaque CLI
// et la liste des skills. Le dossier reste la source : Loom le lit et, sur
// demande, relie ses skills par des liens symboliques. Contrat serveur :
// capability_folder.go.
import { html, useState, useEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { Icon } from '../../ui/icons.js';
import { Modal, toast } from '../../ui/dialog.js';
import { FolderPicker } from '../../ui/folder.js';
import { Line, Group } from '../settings/kit.js';
import { AgentAccess } from '../settings/agent-access.js';

const homePath = p => String(p || '').replace(/^\/home\/[^/]+/, '~').replace(/^\/Users\/[^/]+/, '~');

export const loadCapabilities = () => get('/api/capabilities/folder').catch(e => ({ ok: false, error: e.message }));

function FolderDialog({ current, suggested, onClose, onSaved }) {
  const [path, setPath] = useState(current || suggested || '');
  const [pick, setPick] = useState(false);
  const [busy, setBusy] = useState(false);
  const save = async value => {
    setBusy(true);
    const r = await post('/api/capabilities/folder', { path: value }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false) return toast(r.error || t('agents.links.failed'), 'err');
    onSaved(r);
    onClose();
  };
  return html`<${Modal} title=${t('agents.links.folder')} sub=${t('agents.links.folder_sub')} onClose=${busy ? undefined : onClose}
      foot=${html`${current && html`<button class="btn" disabled=${busy} onClick=${() => save('')}>${t('agents.links.forget')}</button>`}
        <button class="btn" disabled=${busy} onClick=${onClose}>${t('ui.dialog.annuler')}</button>
        <button class="btn primary" disabled=${busy || !path.trim()} onClick=${() => save(path)}>${t('agents.links.use')}</button>`}>
    <div class="sb-form">
      <div class="field-row">
        <input class="input mono" aria-label=${t('agents.links.folder')} value=${path} placeholder="~/notes" onInput=${e => setPath(e.target.value)} />
        <button type="button" class="btn sm" onClick=${() => setPick(true)}>${t('settings.brain.choose')}</button>
      </div>
      <p class="note">${t('agents.links.layout')}</p>
    </div>
    ${pick && html`<${FolderPicker} start=${path} onClose=${() => setPick(false)} onPick=${d => { setPick(false); setPath(d); }} />`}
  </${Modal}>`;
}

function clientState(c) {
  const parts = [];
  if (c.linked.length) parts.push(t('agents.links.linked', { n: c.linked.length }));
  if (c.missing.length) parts.push(t('agents.links.missing', { n: c.missing.length }));
  if (c.blocked.length) parts.push(t('agents.links.blocked', { n: c.blocked.length }));
  if (c.stale.length) parts.push(t('agents.links.stale', { n: c.stale.length }));
  return parts.join(' · ') || t('agents.links.no_skills');
}

export function ConnectionsPage({ phone }) {
  const [state, setState] = useState(null);
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState('');
  const load = () => loadCapabilities().then(setState);
  useEffect(() => { load(); }, []);
  const link = async client => {
    setBusy(client || 'all');
    const r = await post('/api/capabilities/link', { client }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (r.ok === false) return toast(r.error || t('agents.links.failed'), 'err');
    setState(r);
    toast(t('agents.links.linked_done'));
  };
  const dir = state?.dir || '';
  const skills = state?.skills || [];
  const clients = state?.clients || [];
  const Heading = phone ? 'h2' : 'h1';
  return html`<div class="cm-scroll"><div class="cm-cron cm-conn"><section class="cm-cron-list">
    <div class="cm-cron-h"><div><${Heading}>${t('agents.links.title')}</${Heading}><p>${t('agents.links.lead')}</p></div></div>
    <div class="set-body">
      <${AgentAccess} />
      <${Group} title=${t('agents.links.capabilities')}>
        <${Line} label=${t('agents.links.folder')} sub=${t('agents.links.folder_sub')}>
          <button type="button" class="btn mono" aria-haspopup="dialog" onClick=${() => setEditing(true)}>
            <span class="trunc">${state === null ? '…' : dir ? homePath(dir) : t('agents.links.not_set')}</span>
            <${Icon} n="edit" />
          </button>
        </${Line}>
        ${state?.error && html`<p role="alert" class="set-note msg-err">${state.error}</p>`}
        ${!dir && state?.suggested && html`<${Line} label=${t('agents.links.suggested')} sub=${homePath(state.suggested)}>
          <button class="btn sm" onClick=${() => post('/api/capabilities/folder', { path: state.suggested }).then(setState)}>${t('agents.links.use')}</button>
        </${Line}>`}
      </${Group}>
      ${dir && !state?.error && html`
        <${Group} title=${t('agents.links.clients')}>
          ${clients.map(c => html`<${Line} key=${c.id} label=${c.label} sub=${homePath(c.dir)}>
            <span class="state">${clientState(c)}</span>
            ${(c.missing.length > 0 || c.stale.length > 0) && html`<button class="btn sm" disabled=${!!busy} onClick=${() => link(c.id)}>${t('agents.links.link')}</button>`}
          </${Line}>`)}
        </${Group}>
        <p class="note">${t('agents.links.clients_note')}</p>
        <${Group} title=${t('agents.links.skills', { n: skills.length })}>
          ${skills.length ? skills.map(s => html`<${Line} key=${s.name} label=${s.name} sub=${s.description} stack=${phone}>
            ${s.scripts > 0 && html`<span class="state">${t('agents.links.scripts', { n: s.scripts })}</span>`}
          </${Line}>`) : html`<p class="set-note">${t('agents.links.no_skills_text')}</p>`}
        </${Group}>`}
    </div></section>
    ${editing && html`<${FolderDialog} current=${dir} suggested=${state?.suggested} onClose=${() => setEditing(false)} onSaved=${setState} />`}
  </div></div>`;
}
