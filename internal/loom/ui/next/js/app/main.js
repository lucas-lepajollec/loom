import { t } from '../core/i18n.js';
import { html, render, useStore, cls } from '../core/lib.js';
import { Component } from 'preact';
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


// Une page qui plante pendant son affichage ne doit pas laisser Preact au milieu
// d'une mise à jour (écran noir, doublons, éléments invisibles mais touchables).
// La page fautive affiche l'erreur exacte ; la navigation reste utilisable.
class PageGuard extends Component {
  constructor(props) { super(props); this.state = { error: null }; }
  static getDerivedStateFromError(error) { return { error }; }
  componentDidCatch(error) { reportClientError(error, 'page ' + this.props.name); }
  // Changer d'onglet ou de sous-page réessaie : l'erreur d'une sous-page ne
  // bloque pas ses voisines.
  componentDidUpdate(prev) { if (this.state.error && prev.sub !== this.props.sub) this.setState({ error: null }); }
  render() {
    const e = this.state.error;
    if (!e) return this.props.children;
    return html`<div class="view page"><div class="page-in"><div class="card pad page-crash">
      <h3>${t('app.crash.title')}</h3><p class="note">${t('app.crash.note')}</p>
      <pre class="mono">${String(e && e.message || e).slice(0, 400)}${e && e.stack ? '\n' + String(e.stack).split('\n').slice(1, 4).join('\n') : ''}</pre>
      <button class="btn" onClick=${() => this.setState({ error: null })}>${t('app.crash.retry')}</button></div></div></div>`;
  }
}

// Erreurs hors affichage : une bande discrète, pour une capture d'écran.
let errorBar = null;
export function reportClientError(error, where) {
  try {
    const msg = (where ? where + ': ' : '') + String(error && error.message || error).slice(0, 300);
    console.error('[loom]', msg, error);
    // Phones keep the screen clean: the console still has the error.
    if (matchMedia('(max-width: 760px)').matches) return;
    if (!errorBar) {
      errorBar = document.createElement('div');
      errorBar.className = 'client-error';
      errorBar.addEventListener('click', () => { errorBar.remove(); errorBar = null; });
      document.body.appendChild(errorBar);
    }
    errorBar.textContent = '⚠ ' + msg + ' — ' + t('app.crash.dismiss');
  } catch (_) {}
}
// Only Loom's own scripts: browser extensions and injected wallets (e.g.
// Brave's window.ethereum) are not Loom errors.
const ownScript = file => { try { const u = new URL(file, location.href); return u.origin === location.origin && u.pathname.startsWith('/next/'); } catch (_) { return false; } };
window.addEventListener('error', e => { if (e.error && ownScript(e.filename)) reportClientError(e.error); });
window.addEventListener('unhandledrejection', e => { const r = e.reason; if (r && r.name !== 'AbortError') reportClientError(r); });

function Main() {
  const route = useStore(app, s => s.route);
  const Page = pageFor(route.section) || Placeholder;
  return html`<main class="main">
    ${route.section !== 'chat' && html`<div class="mobile-bar only-mobile"><button class="icon-btn" aria-label="${t("app.main.menu")}" onClick=${() => app.set({ sideOpen: true })}><${Icon} n="menu" /></button><b>${t("app.main.loom")}</b></div>`}
    <${PageGuard} key=${route.section} name=${route.section} sub=${route.sub || ''}><${Page} route=${route} /></${PageGuard}></main>`;
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
