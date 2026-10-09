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
    const layers = document.querySelectorAll('.scrim,.side-scrim,.drawer-scrim,.pop,.insp.open').length;
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

export function initTouch() {
  touchDebug();
  if (!coarse()) return;
  document.documentElement.classList.add('touch');
  keepDocumentAtTop();
}
