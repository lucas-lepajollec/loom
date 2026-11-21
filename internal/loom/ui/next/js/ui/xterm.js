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
    // Rendu GPU (WebGL), largeurs Unicode 11 (emojis), instantanés (serialize) et
    // écho prédictif (zerolag-input), comme le terminal de Codeman. Un module manquant ne bloque pas le terminal.
    const addons = () => Promise.all(['addon-fit', 'addon-webgl', 'addon-unicode11', 'addon-serialize', 'zerolag-input'].map(n => load('/next/vendor/xterm/' + n + '.js').catch(() => null)));
    Promise.all([styled, load('/next/vendor/xterm/xterm.js').then(addons)])
      .then(() => new Promise(ok => requestAnimationFrame(ok)))
      .then(() => res(window), err => { xtermReady = null; rej(err); });
  });
  return xtermReady;
}
