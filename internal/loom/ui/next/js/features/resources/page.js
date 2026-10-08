import { t } from '../../core/i18n.js';
// Brain : sources, mémoire, skills et outils MCP, définis une fois. Une seule
// barre de recherche, sur la ligne des onglets, cherche dans l'onglet ouvert.
import { html, useState, useEffect } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Brain, brainTabs } from './brain.js';
import { MemoryItems } from './memory-items.js';
import { Skills } from './skills.js';
import { Mcp } from './mcp.js';

const PLACEHOLDER = { sources: 'brain.search.sources', memory: 'brain.search.memory', skills: 'brain.search.skills', mcp: 'brain.search.mcp' };

export function ResourcesPage({ route }) {
  const requested = route.sub;
  const tab = ['memory', 'skills', 'mcp'].includes(requested) ? requested : 'sources';
  const [q, setQ] = useState('');
  useEffect(() => { setQ(''); }, [tab]);
  return html`<div class="view page"><div class="page-in">
    <div class="brain-bar"><nav class="sec-tabs" aria-label=${t("resources.page.brain")}>${brainTabs().map(o => html`<a href=${'#/brain/' + o.value} aria-current=${o.value === tab ? 'page' : undefined}>${o.label}</a>`)}</nav>
      <label class="search brain-q"><${Icon} n="search" /><input type="text" placeholder=${t(PLACEHOLDER[tab])} value=${q} onInput=${e => setQ(e.target.value)} /></label></div>
    <div class="page-head brain-page-head"><div><h1>${t('resources.page.brain')}</h1><p>${t('second_brain.page_subtitle')}</p></div></div>
    <div class="tab-body" key=${tab}>${tab === 'sources' ? html`<${Brain} q=${q} />` : tab === 'memory' ? html`<${MemoryItems} q=${q} />` : tab === 'skills' ? html`<${Skills} q=${q} />` : html`<${Mcp} q=${q} />`}</div>
  </div></div>`;
}
