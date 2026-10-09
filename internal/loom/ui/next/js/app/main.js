import { t } from '../core/i18n.js';
import { html, render, useStore, cls } from '../core/lib.js';
import { Icon } from '../ui/icons.js';
import { app, startPolling, setTheme } from '../core/state.js';
import { Layers } from '../ui/dialog.js';
import { Sidebar } from './shell.js';
import { init as initChat } from '../features/chat/engine.js';
import { Placeholder } from './placeholder.js';
import { pageFor } from './routes.js';
import { Palette } from './palette.js';
import { initLang } from '../core/i18n.js';
import { Welcome, initWelcome } from '../features/onboarding/welcome.js';
import { authStatus } from '../core/api.js';
import { AccessScreen } from './access.js';
import { initTouch } from './touch.js';


function Main() {
  const route = useStore(app, s => s.route);
  const Page = pageFor(route.section) || Placeholder;
  return html`<main class="main">
    ${route.section !== 'chat' && html`<div class="mobile-bar only-mobile"><button class="icon-btn" aria-label="${t("app.main.menu")}" onClick=${() => app.set({ sideOpen: true })}><${Icon} n="menu" /></button><b>${t("app.main.loom")}</b></div>`}
    <${Page} key=${route.section} route=${route} /></main>`;
}

function App() {
  const sideOpen = useStore(app, s => s.sideOpen);
  return html`<div class=${cls('app', sideOpen && 'side-open')}>
    <${Sidebar} />
    ${sideOpen && html`<div class="side-scrim only-mobile" onClick=${() => app.set({ sideOpen: false })}></div>`}
    <${Main} />
    <${Palette} />
    <${Layers} />
    <${Welcome} />
  </div>`;
}

setTheme(app.get().theme);
async function boot() {
  const root = document.getElementById('app');
  try {
    const status = await authStatus();
    if (!status.authenticated) { render(html`<${AccessScreen} status=${status} onDone=${boot} />`, root); return; }
    render(html`<${App} />`, root);
    initLang(); startPolling(); initChat(); initWelcome();
  } catch (_) {
    render(html`<div class="welcome" style="min-height:100dvh"><h1>Loom</h1><p role="alert">${t('access.unavailable')}</p><button class="btn" onClick=${boot}>${t('access.retry')}</button></div>`, root);
  }
}
initTouch();
boot();
