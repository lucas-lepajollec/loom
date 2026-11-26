// Connexions : ce que tous les agents de cette machine utilisent, présenté
// comme une page de plugins. Les serveurs MCP (l'accès à Loom) et les skills
// du dossier des capacités sont des cartes ; une carte ouvre sa fiche dans le
// panneau latéral. Le dossier reste la source : Loom le lit et, sur demande,
// relie ses skills aux dossiers des CLI. Contrat serveur : capability_folder.go.
import { html, useState, useEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { usePhone } from '../../core/viewport.js';
import { Icon } from '../../ui/icons.js';
import { Modal, toast } from '../../ui/dialog.js';
import { Drawer } from '../../ui/drawer.js';
import { Seg, Empty } from '../../ui/controls.js';
import { FolderPicker } from '../../ui/folder.js';
import { md } from '../chat/md.js';
import { AgentAccess } from '../settings/agent-access.js';
import { skillCards, matches } from './cards.js';

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

// État commun à tous les CLI : relié partout, ou ce qu'il reste à faire.
function linkState(clients) {
  const sum = k => clients.reduce((n, c) => n + c[k].length, 0);
  return { missing: sum('missing'), blocked: sum('blocked'), stale: sum('stale'), names: clients.map(c => c.label).join(', ') };
}

const Tile = ({ icon }) => html`<span class="cx-tile" aria-hidden="true"><${Icon} n=${icon} /></span>`;

function Card({ icon, name, description, meta, status, onOpen }) {
  return html`<button type="button" class="cx-card" onClick=${onOpen}>
    <span class="cx-card-top"><${Tile} icon=${icon} /><span class="cx-name">${name}</span>${status && html`<span class="cx-status">${status}</span>`}</span>
    <span class="cx-desc">${description}</span>
    ${meta && html`<span class="cx-meta">${meta}</span>`}
  </button>`;
}

function SkillDetail({ name, onBack }) {
  const [d, setD] = useState(null);
  useEffect(() => { get('/api/capabilities/skill?name=' + encodeURIComponent(name)).then(setD).catch(e => setD({ ok: false, error: e.message })); }, [name]);
  if (!d) return html`<p class="cx-note">…</p>`;
  if (d.ok === false) return html`<p role="alert" class="cx-note msg-err">${d.error}</p>`;
  return html`<div class="cx-detail">
    ${onBack && html`<button type="button" class="btn sm ghost cx-back" onClick=${onBack}><${Icon} n="left" />${onBack.label}</button>`}
    <div class="cx-detail-head"><${Tile} icon="tool" /><div><h2>${d.skill.name}</h2><span class="tag">${t('connections.kind.skill')}</span></div></div>
    <p class="cx-lead">${d.skill.description}</p>
    <dl class="cx-facts">
      <dt>${t('connections.detail.path')}</dt><dd class="mono">${d.path}</dd>
      ${d.skill.title && html`<dt>${t('connections.detail.name')}</dt><dd class="mono">${d.skill.title}</dd>`}
      ${d.files.length > 0 && html`<dt>${t('connections.detail.files')}</dt><dd class="mono cx-files">${d.files.slice(0, 12).map(f => html`<span>${f}</span>`)}${d.files.length > 12 && html`<span>${t('connections.detail.more', { n: d.files.length - 12 })}</span>`}</dd>`}
    </dl>
    <h3 class="cx-section">${t('connections.detail.instructions')}</h3>
    <div class="md cx-md" dangerouslySetInnerHTML=${{ __html: md(d.body) }}></div>
  </div>`;
}

function FamilyDetail({ card, onOpen }) {
  return html`<div class="cx-detail">
    <div class="cx-detail-head"><${Tile} icon="grid" /><div><h2>${card.name}</h2><span class="tag">${t('agents.links.skills', { n: card.members.length })}</span></div></div>
    <p class="cx-lead">${t('connections.family_lead', { family: card.name })}</p>
    <div class="cx-rows">${card.members.map(m => html`<button type="button" key=${m.name} class="cx-row" onClick=${() => onOpen(m.name)}>
      <span class="cx-row-name">${m.name.slice(card.name.length + 1)}</span><span class="cx-row-desc">${m.description}</span><${Icon} n="right" />
    </button>`)}</div>
  </div>`;
}

function Detail({ open, onClose }) {
  const [inner, setInner] = useState('');
  useEffect(() => setInner(''), [open]);
  const title = open.kind === 'mcp' ? 'MCP' : t('connections.kind.skill');
  return html`<${Drawer} title=${title} onClose=${onClose}>
    ${open.kind === 'mcp' ? html`<div class="cx-detail">
        <div class="cx-detail-head"><${Tile} icon="plug" /><div><h2>Loom</h2><span class="tag">MCP</span></div></div>
        <p class="cx-lead">${t('connections.loom_lead')}</p>
        <div class="set-body"><${AgentAccess} /></div>
      </div>`
      : open.kind === 'family' && !inner ? html`<${FamilyDetail} card=${open} onOpen=${setInner} />`
      : html`<${SkillDetail} name=${inner || open.id} onBack=${inner ? Object.assign(() => setInner(''), { label: open.name }) : null} />`}
  </${Drawer}>`;
}

export function ConnectionsPage() {
  const phone = usePhone();
  const [state, setState] = useState(null);
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [filter, setFilter] = useState('all');
  const [q, setQ] = useState('');
  const [open, setOpen] = useState(null);
  useEffect(() => { loadCapabilities().then(setState); }, []);
  const link = async () => {
    setBusy(true);
    const r = await post('/api/capabilities/link', { client: '' }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false) return toast(r.error || t('agents.links.failed'), 'err');
    setState(r);
    toast(t('agents.links.linked_done'));
  };
  const dir = state?.dir || '';
  const cards = skillCards(state?.skills);
  const needle = q.trim().toLowerCase();
  const shownSkills = filter === 'mcp' ? [] : cards.filter(c => matches(c, needle));
  const mcp = { kind: 'mcp', id: 'loom', name: 'Loom', description: t('connections.loom_desc') };
  const shownMcp = filter === 'skills' || !matches(mcp, needle) ? [] : [mcp];
  const links = linkState(state?.clients || []);
  const todo = links.missing + links.stale;
  const skillCount = (state?.skills || []).length;

  return html`<div class="view page"><div class="page-in cx-page">
    <div class="page-head cx-head">
      <div><h1>${t('agents.links.title')}</h1><p>${t('connections.lead')}</p></div>
      <label class="search cx-search"><${Icon} n="search" /><input type="search" aria-label=${t('connections.search')} placeholder=${t('connections.search')} value=${q} onInput=${e => setQ(e.target.value)} /></label>
    </div>
    <${Seg} label=${t('connections.filter')} value=${filter} onChange=${setFilter} options=${[
      { value: 'all', label: t('connections.all') },
      { value: 'skills', label: t('connections.kind.skills'), count: skillCount || '' },
      { value: 'mcp', label: 'MCP', count: 1 },
    ]} />

    <div class="cx-source">
      <${Icon} n="folder" />
      <div class="cx-source-text">
        <span class="mono cx-path">${state === null ? '…' : dir ? homePath(dir) : t('agents.links.not_set')}</span>
        <span class="cx-source-state">${!dir ? t('connections.no_folder')
          : state?.error ? state.error
          : todo ? t('connections.to_link', { n: todo })
          : t('connections.linked_all')}${dir && !state?.error && links.blocked ? ' · ' + t('agents.links.blocked', { n: links.blocked }) : ''}</span>
      </div>
      ${dir && !state?.error && todo > 0 && html`<button class="btn sm primary" disabled=${busy} onClick=${link}>${t('agents.links.link')}</button>`}
      ${!dir && state?.suggested && html`<button class="btn sm primary" onClick=${() => post('/api/capabilities/folder', { path: state.suggested }).then(setState)}>${t('connections.use_suggested', { path: homePath(state.suggested) })}</button>`}
      <button class="btn sm" aria-haspopup="dialog" onClick=${() => setEditing(true)}>${dir ? t('connections.change') : t('settings.brain.choose')}</button>
    </div>

    ${shownMcp.length > 0 && html`<section class="cx-group">
      <h2 class="cx-group-h">MCP</h2>
      <div class="cx-grid">${shownMcp.map(c => html`<${Card} key=${c.id} icon="plug" name=${c.name} description=${c.description} meta=${t('connections.loom_meta')} onOpen=${() => setOpen(c)} />`)}</div>
    </section>`}

    ${filter !== 'mcp' && html`<section class="cx-group">
      <h2 class="cx-group-h">${t('connections.kind.skills')}</h2>
      ${!dir ? html`<${Empty} icon="folder" title=${t('connections.empty_title')} text=${t('agents.links.layout')} />`
        : shownSkills.length ? html`<div class="cx-grid">${shownSkills.map(c => html`<${Card} key=${c.id}
            icon=${c.kind === 'family' ? 'grid' : 'tool'} name=${c.name} description=${c.description}
            meta=${c.kind === 'family' ? t('agents.links.skills', { n: c.members.length }) : c.skill.scripts ? t('agents.links.scripts', { n: c.skill.scripts }) : ''}
            onOpen=${() => setOpen(c)} />`)}</div>`
        : html`<p class="cx-note">${needle ? t('connections.no_match') : t('agents.links.no_skills_text')}</p>`}
    </section>`}
    <p class="cx-note">${t('connections.footnote', { clients: links.names || 'Claude Code' })}</p>

    ${open && html`<${Detail} open=${open} onClose=${() => setOpen(null)} />`}
    ${editing && html`<${FolderDialog} current=${dir} suggested=${state?.suggested} onClose=${() => setEditing(false)} onSaved=${setState} />`}
  </div></div>`;
}
