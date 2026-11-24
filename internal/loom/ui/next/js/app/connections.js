import { html } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { Icon } from '../ui/icons.js';
import { AgentAccess } from '../features/settings/agent-access.js';

export function ConnectionsPage() {
  const destinations = [
    ['#/agents/settings', 'agent', t('agents.nav.settings') + ' · Codeman'],
    ['#/cloud', 'cloud', t('app.routes.cloud')],
    ['#/settings/policy', 'lock', t('policy.title')],
  ];
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><h1>${t('workspace.connections')}</h1></div>
    <div class="set-body"><${AgentAccess} title="MCP Loom" />
      <nav class="connection-links" aria-label=${t('workspace.connections')}>
        ${destinations.map(([href, icon, label]) => html`<a key=${href} href=${href}><${Icon} n=${icon} /><span>${label}</span><${Icon} n="right" /></a>`)}
      </nav>
    </div>
  </div></div>`;
}
