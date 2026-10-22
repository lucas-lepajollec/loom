// xterm.js (MIT, vendor/xterm) n'est chargé qu'à la première page qui l'emploie
// (terminaux, agents), puis partagé.
let xtermReady = null;
export function loadXterm() {
  if (!xtermReady) xtermReady = new Promise((res, rej) => {
    const css = document.createElement('link');
    css.rel = 'stylesheet'; css.href = '/next/vendor/xterm/xterm.css';
    document.head.appendChild(css);
    const load = src => new Promise((ok, ko) => { const s = document.createElement('script'); s.src = src; s.onload = ok; s.onerror = ko; document.head.appendChild(s); });
    load('/next/vendor/xterm/xterm.js').then(() => load('/next/vendor/xterm/addon-fit.js')).then(() => res(window), err => { xtermReady = null; rej(err); });
  });
  return xtermReady;
}
