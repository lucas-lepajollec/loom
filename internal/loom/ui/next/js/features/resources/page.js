import { t } from '../../core/i18n.js';
// Brain : sources, mémoire, recherche, skills et outils MCP, définis une fois.
import { html } from '../../core/lib.js';
import { Tabs } from '../../ui/controls.js';
import { go } from '../../core/state.js';
import { Brain, BrainSearch, brainTabs } from './brain.js';
import { MemoryItems } from './memory-items.js';
import { Skills } from './skills.js';
import { Mcp } from './mcp.js';

export function ResourcesPage({ route }) {
  const requested = route.sub;
  const tab = ['memory', 'search', 'skills', 'mcp'].includes(requested) ? requested : 'sources';
  return html`<div class="view page"><div class="page-in">
    <div class="page-head brain-page-head"><div><h1>${t('resources.page.brain')}</h1><p>${t('second_brain.page_subtitle')}</p></div></div>
    <${Tabs} value=${tab} onChange=${localT => go('brain', localT)} label="${t("resources.page.brain")}" options=${brainTabs()} />
    <div class="tab-body" key=${tab}>${tab === 'sources' ? html`<${Brain} />` : tab === 'memory' ? html`<${MemoryItems} />` : tab === 'search' ? html`<${BrainSearch} />` : tab === 'skills' ? html`<${Skills} />` : html`<${Mcp} />`}</div>
  </div></div>`;
}
