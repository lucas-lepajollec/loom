// xterm.js (MIT, vendor/xterm) n'est chargé qu'à la première page qui l'emploie
// (terminaux, agents), puis partagé.
let xtermReady = null;
export function loadXterm() {
  if (!xtermReady) xtermReady = new Promise((res, rej) => {
    // La feuille de style aussi : mesuré sans elle, le terminal prend une
    // mauvaise géométrie selon l'état du cache.
    const css = document.createElement('link');
    const styled = new Promise(ok => { css.onload = ok; css.onerror = ok; });
    css.rel = 'stylesheet'; css.href = '/next/vendor/xterm/xterm.css';
    document.head.appendChild(css);
    const load = src => new Promise((ok, ko) => { const s = document.createElement('script'); s.src = src; s.onload = ok; s.onerror = ko; document.head.appendChild(s); });
    Promise.all([styled, load('/next/vendor/xterm/xterm.js').then(() => load('/next/vendor/xterm/addon-fit.js'))])
      .then(() => new Promise(ok => requestAnimationFrame(ok)))
      .then(() => res(window), err => { xtermReady = null; rej(err); });
  });
  return xtermReady;
}
