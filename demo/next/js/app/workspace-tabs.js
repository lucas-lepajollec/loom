// Keep recently opened surfaces in this browser window. Closing a tab only
// removes its navigation shortcut. Processes and sessions belong to the host.
import { html, useState, useEffect, useRef, useStore } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { agents } from '../features/agents/store.js';
import { Menu } from '../ui/controls.js';
import { Icon } from '../ui/icons.js';
import { groupOf } from './sections.js';

function describe(route) {
  const { section, sub, id } = route;
  const group = groupOf(section);
  const sessionId = section === 'agents' && sub === 's' && id;
  const path = [section, sub, id].filter(Boolean).map(encodeURIComponent).join('/');
  const special = {
    chat: ['chat', 'side.type_chat'], settings: ['gear', 'app.shell.reglages'],
    terminals: ['terminal', 'app.routes.terminaux'], workspaces: ['folder', 'app.routes.environnement'],
    inbox: ['inbox', 'mobile.inbox'], sessions: ['chat', 'mobile.sessions'], more: ['more', 'mobile.more'],
  };
  if (!group && !special[section]) return null;
  return {
    key: sessionId ? 'agent/' + sessionId : section === 'files' && id ? path : group?.id || section,
    href: '#/' + path,
    icon: sessionId ? 'agent' : group?.icon || special[section][0],
    group: group?.id, labelKey: special[section]?.[1], sessionId,
    file: section === 'files' && id ? id.split('/').pop() : '',
  };
}

export function WorkspaceTabs({ route }) {
  const active = describe(route);
  const [recent, setRecent] = useState([]);
  const current = useRef();
  const [menu, setMenu] = useState(null);
  const sessions = useStore(agents, state => state.sessions);
  useEffect(() => {
    if (!active) return;
    setRecent(old => {
      const i = old.findIndex(tab => tab.key === active.key);
      if (i < 0) return [...old.slice(-9), active];
      return old.map((tab, index) => index === i ? active : tab);
    });
  }, [active?.href]);
  useEffect(() => { current.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' }); }, [recent, active?.key]);
  const name = tab => tab.file || (tab.sessionId && (sessions[tab.sessionId]?.name || tab.sessionId))
    || (tab.group ? groupOf(tab.href.slice(2).split('/')[0])?.label : t(tab.labelKey));
  const closeTab = tab => {
    if (recent.length < 2) return;
    const index = recent.findIndex(other => other.key === tab.key);
    if (index < 0) return;
    if (tab.key === active?.key) location.hash = recent[index ? index - 1 : 1].href;
    setRecent(old => old.filter(item => item.key !== tab.key));
  };
  return html`<div class="workspace-tabbar"><nav class="workspace-tabs" aria-label=${t('workspace.open_pages')}>
    ${recent.map(tab => html`<div key=${tab.key} class="work-tab" ref=${tab.key === active?.key ? current : undefined}>
      <a href=${tab.href} title=${name(tab)} aria-current=${tab.key === active?.key ? 'page' : undefined}>
        <${Icon} n=${tab.icon} /><span>${name(tab)}</span></a>
      ${recent.length > 1 && html`<button type="button" aria-label=${t('workspace.close_tab', { name: name(tab) })}
        onClick=${() => closeTab(tab)}><${Icon} n="close" /></button>`}
    </div>`)}
  </nav><button class="icon-btn tab-picker" type="button" aria-label=${t('workspace.open_pages')} title=${t('workspace.open_pages')}
    aria-haspopup="menu" aria-expanded=${!!menu} onClick=${e => setMenu(e.currentTarget)}><${Icon} n="chevron" /></button>
    ${menu && html`<${Menu} anchor=${menu} label=${t('workspace.open_pages')} onClose=${() => setMenu(null)}
      items=${[...recent.map(tab => ({ label: name(tab), icon: tab.icon, current: tab.key === active?.key, href: tab.href })),
        ...(recent.length > 1 ? ['-', { label: t('workspace.close_others'), icon: 'close', run: () => setRecent(recent.filter(tab => tab.key === active?.key)) }] : [])]} />`}
  </div>`;
}
