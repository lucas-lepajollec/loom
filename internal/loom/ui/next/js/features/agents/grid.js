// Mosaïque : plusieurs sessions côte à côte, chacune dans son vrai terminal.
// Le choix des tuiles et la disposition restent sur cet appareil.
import { t } from '../../core/i18n.js';
import { html, useState, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty } from '../../ui/controls.js';
import { agents, stateOf, sessionList, projectOf } from './store.js';
import { AgentTerminal } from './terminal.js';
import { StateDot } from './rail.js';
import { CliMark } from './new-session.js';
import { openSession } from './nav.js';

const KEY = 'loom.codeman.grid';
const read = () => { try { return JSON.parse(localStorage.getItem(KEY) || 'null'); } catch (_) { return null; } };
const save = v => { try { localStorage.setItem(KEY, JSON.stringify(v)); } catch (_) {} };

function Tile({ s, onRemove, onMax, max }) {
  const state = stateOf(s);
  return html`<div class=${cls('cm-tile', 'st-' + state, max && 'max')}>
    <div class="cm-tile-h">
      <${CliMark} mode=${s.mode} size="sm" />
      <button class="cm-tile-name trunc" onClick=${() => openSession(s.id)} title=${t('agents.grid.open')}>${s.name || s.id.slice(0, 8)}</button>
      <span class="muted trunc cm-tile-dir">${projectOf(s.workingDir)}</span>
      <span class="grow"></span><${StateDot} state=${state} />
      <button class="icon-btn" aria-label=${max ? t('agents.grid.restore') : t('agents.grid.maximize')} onClick=${onMax}><${Icon} n=${max ? 'sidebar' : 'panel'} /></button>
      <button class="icon-btn" aria-label=${t('agents.grid.remove')} onClick=${onRemove}><${Icon} n="close" /></button>
    </div>
    <div class="cm-tile-b"><${AgentTerminal} sessionId=${s.id} fontSize=${12} /></div>
  </div>`;
}

export function Mosaic() {
  useStore(agents, s => [s.order, s.sessions, s.approvals]);
  const all = sessionList();
  const [picked, setPicked] = useState(() => read());
  const [max, setMax] = useState('');
  // Par défaut : les sessions vivantes, dans la limite de six tuiles.
  const ids = (picked || all.filter(s => stateOf(s) !== 'stopped').slice(0, 6).map(s => s.id)).filter(id => all.some(s => s.id === id));
  const set = next => { setPicked(next); save(next); };
  const shown = ids.map(id => all.find(s => s.id === id)).filter(Boolean);
  const visible = max && shown.some(s => s.id === max) ? shown.filter(s => s.id === max) : shown;
  const cols = visible.length <= 1 ? 1 : visible.length <= 4 ? 2 : 3;
  return html`<div class="cm-mosaic">
    <div class="cm-mosaic-bar">
      <span class="muted">${t('agents.grid.lead')}</span><span class="grow"></span>
      <div class="chips">${all.map(s => html`<button type="button" key=${s.id} class=${cls('chip-btn', ids.includes(s.id) && 'on')} aria-pressed=${String(ids.includes(s.id))}
          onClick=${() => set(ids.includes(s.id) ? ids.filter(x => x !== s.id) : [...ids, s.id].slice(-9))}><${StateDot} state=${stateOf(s)} />${s.name || s.id.slice(0, 8)}</button>`)}</div>
    </div>
    ${!visible.length ? html`<${Empty} icon="terminal" title=${t('agents.grid.empty')} text=${t('agents.grid.empty_text')} />`
      : html`<div class="cm-tiles" style=${`--cols:${cols}`}>${visible.map(s => html`<${Tile} key=${s.id} s=${s} max=${max === s.id}
          onMax=${() => setMax(max === s.id ? '' : s.id)} onRemove=${() => { set(ids.filter(x => x !== s.id)); if (max === s.id) setMax(''); }} />`)}</div>`}
  </div>`;
}
