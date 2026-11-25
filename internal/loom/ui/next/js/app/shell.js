import { t } from '../core/i18n.js';
import { html, useEffect } from '../core/lib.js';
import { Icon } from '../ui/icons.js';
import { ContextLinks, groupOf } from './sections.js';
import { openPalette } from './palette.js';
import { Sessions } from './sessions.js';
import { agents } from '../features/agents/store.js';

export const isSessionRoute = ({ section, sub }) => section === 'chat' || section === 'sessions'
  || section === 'agents' && ['s', 'grid'].includes(sub);

export function startNewSession(folder = '') {
  agents.set({ creating: { type: 'chat', folder } });
}

export function useShortcuts() {
  useEffect(() => {
    const key = e => {
      const mod = e.metaKey || e.ctrlKey, k = e.key.toLowerCase();
      if (mod && !e.shiftKey && k === 'k') { e.preventDefault(); openPalette(); }
      else if (mod && e.shiftKey && k === 'o') { e.preventDefault(); startNewSession(); }
    };
    addEventListener('keydown', key); return () => removeEventListener('keydown', key);
  }, []);
}

export function Rail({ count, route }) {
  const current = isSessionRoute(route) ? 'sessions' : groupOf(route.section)?.id || route.section;
  const link = (id, href, icon, label, n = 0) => html`<a key=${id} href=${href} aria-label=${label} title=${label}
    aria-current=${current === id ? 'page' : undefined}><${Icon} n=${icon} />${n > 0 && html`<small>${n}</small>`}</a>`;
  return html`<nav class="loom-rail" aria-label=${t('app.shell.navigation')}>
    ${link('sessions', '#/sessions', 'chat', t('mobile.sessions'))}
    ${link('files', '#/files', 'folder', t('files.title'))}
    ${link('machines', '#/machines', 'server', t('settings.page.machines'))}
    ${link('models', '#/local', 'chip', t('app.groups.models'))}
    ${link('agents', '#/agents', 'agent', t('agents.title'))}
    ${link('activity', '#/quotas', 'chart', t('app.groups.activity'))}
    <span class="grow"></span>
    ${link('inbox', '#/inbox', 'inbox', t('mobile.inbox'), count)}
    ${link('settings', '#/settings', 'gear', t('settings.page.reglages'))}
  </nav>`;
}

export function ContextColumn({ route }) {
  if (isSessionRoute(route)) return html`<aside class="loom-context sessions-context" aria-label=${t('mobile.sessions')}><${Sessions} /></aside>`;
  return html`<${ContextLinks} route=${route} />`;
}
