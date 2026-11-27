import { useRef } from '../core/lib.js';
import { app, phoneBack, phoneTab } from '../core/state.js';
import { isPhone } from '../core/viewport.js';

// Écrans tactiles, surtout iOS (Safari, Brave, Chrome : tous WebKit sur iPhone).
// Loom est une application plein écran : le document lui-même ne doit jamais
// défiler. iOS le fait pourtant quand le clavier s'ouvre ou qu'une liste
// « rebondit », puis laisse la page décalée : l'affichage et les zones de
// toucher ne coïncident plus et des boutons semblent morts. On remet donc le
// document à zéro dès que le clavier se ferme ou qu'un champ perd le focus.
const coarse = () => window.matchMedia('(hover:none) and (pointer:coarse)').matches;

function keepDocumentAtTop() {
  const vv = window.visualViewport;
  const reset = () => {
    // Clavier encore ouvert : iOS a besoin de ce décalage pour montrer le champ.
    if (vv && vv.height < window.innerHeight - 80) return;
    const el = document.scrollingElement || document.documentElement;
    if (window.scrollY || el.scrollTop) window.scrollTo(0, 0);
  };
  let t = null;
  const soon = () => { clearTimeout(t); t = setTimeout(reset, 120); };
  vv?.addEventListener('resize', soon);
  document.addEventListener('focusout', soon, true);
  window.addEventListener('orientationchange', soon);
  window.addEventListener('pageshow', soon);
}

// Diagnostic à la demande : ?touchdebug=1 dans l'adresse. Une bande en bas de
// l'écran montre les derniers touchers (élément visé, élément réellement
// atteint, clic reçu ou non) et l'état du défilement/zoom. Rien n'est envoyé.
function touchDebug() {
  // Seulement quand l'adresse le demande, jamais mémorisé : rien ne reste
  // affiché après coup. On efface l'ancienne mémorisation des onglets ouverts.
  try { sessionStorage.removeItem('loom-touchdebug'); } catch (_) {}
  if (new URLSearchParams(location.search).get('touchdebug') !== '1') return;
  const box = document.createElement('div');
  box.setAttribute('style', 'position:fixed;left:4px;right:4px;bottom:calc(4px + env(safe-area-inset-bottom));z-index:9999;max-height:38vh;overflow:auto;font:11px/1.35 ui-monospace,monospace;background:rgba(0,0,0,.82);color:#e8e8e8;padding:6px 8px;border-radius:8px;pointer-events:none;white-space:pre-wrap');
  document.body.appendChild(box);
  const lines = [];
  const d = e => !e ? '∅' : e.tagName.toLowerCase() + (e.id ? '#' + e.id : '') + (typeof e.className === 'string' && e.className.trim() ? '.' + e.className.trim().split(/\s+/).slice(0, 2).join('.') : '') + ((e.textContent || '').trim() ? ' «' + e.textContent.trim().slice(0, 18) + '»' : '');
  const state = () => {
    const vv = window.visualViewport;
    const layers = document.querySelectorAll('.scrim,.drawer-scrim,.pop,.insp.open').length;
    return `scrollY=${Math.round(window.scrollY)} vv=${vv ? [Math.round(vv.offsetTop), Math.round(vv.height), vv.scale.toFixed(2)].join('/') : '-'} inner=${window.innerHeight} layers=${layers} focus=${d(document.activeElement)}`;
  };
  const log = s => { lines.push(s); while (lines.length > 14) lines.shift(); box.textContent = lines.join('\n') + '\n' + state(); };
  let pending = null;
  document.addEventListener('touchstart', e => {
    const p = e.touches[0];
    const hit = document.elementFromPoint(p.clientX, p.clientY);
    pending = { target: e.target, at: Date.now() };
    log(`touch ${Math.round(p.clientX)},${Math.round(p.clientY)} target=${d(e.target)}${hit !== e.target ? ' hit=' + d(hit) : ''}`);
  }, { capture: true, passive: true });
  document.addEventListener('click', e => {
    log(`  click ${d(e.target)}${e.defaultPrevented ? ' (prevented)' : ''}`);
    pending = null;
  }, true);
  document.addEventListener('touchend', () => {
    const was = pending;
    setTimeout(() => { if (was && pending === was) { log('  no click (swallowed)'); pending = null; } }, 450);
  }, { capture: true, passive: true });
  window.visualViewport?.addEventListener('resize', () => log('viewport resize'));
  window.addEventListener('scroll', () => log('document scrolled'), { passive: true });
  log('touchdebug on — ' + navigator.userAgent.replace(/Mozilla\/5.0 /, '').slice(0, 90));
}

// La hauteur de l'application suit la zone réellement visible : iOS ne
// réduit pas 100dvh quand le clavier s'ouvre (il fait défiler la page), et la
// barre d'outils du navigateur en bas grignote l'écran. --app-h et --app-top
// placent l'application juste au-dessus du clavier et de la barre.
function followVisualViewport() {
  const vv = window.visualViewport;
  if (!vv) return;
  const root = document.documentElement;
  // Safari sur iPhone (pas l'app installée) pose sa barre d'adresse au-dessus
  // du clavier, par-dessus le bas de la zone visible : on garde de la marge.
  const ios = /iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
  const standalone = navigator.standalone === true || matchMedia('(display-mode: standalone)').matches;
  root.classList.toggle('ios-browser', ios && !standalone);
  root.classList.toggle('ios-standalone', ios && standalone);
  let frame = 0, tallest = 0, width = 0;
  const apply = () => {
    frame = 0;
    if (width !== window.innerWidth) { width = window.innerWidth; tallest = 0; }
    tallest = Math.max(tallest, window.innerHeight);
    const editing = document.activeElement?.matches('input,textarea,[contenteditable="true"]');
    const kbOpen = !!editing && vv.height < tallest - 120;
    const h = standalone && !kbOpen ? Math.max(Math.round(vv.height), window.innerHeight) : Math.round(vv.height);
    root.style.setProperty('--app-h', h + 'px');
    root.style.setProperty('--app-top', Math.round(vv.offsetTop) + 'px');
    root.classList.toggle('kb-open', kbOpen);
  };
  const soon = () => { if (!frame) frame = requestAnimationFrame(apply); };
  vv.addEventListener('resize', soon);
  vv.addEventListener('scroll', soon);
  window.addEventListener('orientationchange', soon);
  document.addEventListener('focusin', soon);
  document.addEventListener('focusout', soon);
  apply();
}

function swipeBack() {
  let origin;
  const delta = p => [p.clientX - origin.clientX, p.clientY - origin.clientY];
  const blockedSwipe = target => {
    const rail = target.closest('.cm-acc,.terminal-tabs,.fl-toolbar,.cm-panel-tabs,.cm-term-box,.term-box,.xterm');
    if (!rail) return false;
    if (rail.matches('.cm-term-box,.term-box,.xterm')) return true;
    return rail.scrollWidth > rail.clientWidth + 2 && rail.scrollLeft > 0;
  };
  document.addEventListener('touchstart', e => {
    const p = e.touches[0];
    const inside = e.target.closest('.app,.scrim');
    const layer = document.querySelector('.scrim,.voice-mode');
    origin = isPhone() && inside && !blockedSwipe(e.target) && e.touches.length === 1 && p.clientX <= 24 &&
      (layer || !phoneTab(app.get().route)) ? p : null;
  }, { passive: true, capture: true });
  document.addEventListener('touchmove', e => {
    if (!origin) return;
    const [x, y] = delta(e.touches[0]);
    if (e.touches.length > 1 || Math.abs(y) > 24 && Math.abs(y) > x) { origin = null; return; }
    if (x > 18 && x > Math.abs(y) * 1.4) {
      if (e.cancelable) e.preventDefault();
      e.stopPropagation();
    }
  }, { passive: false, capture: true });
  document.addEventListener('touchcancel', () => { origin = null; }, { passive: true });
  document.addEventListener('touchend', e => {
    if (!origin) return;
    const [x, y] = delta(e.changedTouches[0]);
    origin = null;
    if (x < 70 || x < Math.abs(y) * 1.6) return;
    if (e.cancelable) e.preventDefault();
    if (document.querySelector('.scrim')) document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    else if (document.querySelector('.voice-mode')) document.querySelector('.vm-close')?.click();
    else phoneBack();
  }, { passive: false, capture: true });
}

export function usePullRefresh(refresh) {
  const origin = useRef(null), pending = useRef(false);
  return {
    onTouchStart: e => {
      const el = e.currentTarget;
      origin.current = refresh && isPhone() && e.touches.length === 1 && el.scrollTop <= 0 && !pending.current &&
        !e.target.closest('input,textarea,select,[contenteditable]') ? e.touches[0] : null;
    },
    onTouchCancel: () => { origin.current = null; },
    onTouchMove: e => {
      const start = origin.current, p = e.touches[0];
      if (start && (e.touches.length > 1 || p.clientY - start.clientY < -8 || Math.abs(p.clientX - start.clientX) > 40)) origin.current = null;
    },
    onTouchEnd: async e => {
      const start = origin.current, el = e.currentTarget;
      origin.current = null;
      if (!start || el.scrollTop > 0 || e.changedTouches[0].clientY - start.clientY < 64) return;
      pending.current = true;
      el.setAttribute('aria-busy', 'true');
      el.classList.add('is-refreshing');
      try { await refresh(); } catch (_) {}
      finally { pending.current = false; el.removeAttribute('aria-busy'); el.classList.remove('is-refreshing'); }
    },
  };
}

export function initTouch() {
  touchDebug();
  swipeBack();
  if (!coarse()) return;
  document.documentElement.classList.add('touch');
  keepDocumentAtTop();
  followVisualViewport();
}
