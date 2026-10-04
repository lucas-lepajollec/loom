import { t } from '../core/i18n.js';
// Couches globales : dialogues impératifs (ask/confirm), toasts, info-bulles.
import { html, render, createStore, useStore, useEffect, useLayoutEffect, useRef, useState, cls } from '../core/lib.js';
import { Icon } from './icons.js';

export const layers = createStore({ dialogs: [], toasts: [] });
let seq = 0;

// ask({title, message, input?, ok?, danger?}) → Promise<string|true|null>
export function ask(spec) {
  return new Promise(resolve => {
    const id = ++seq;
    const close = v => { layers.set(s => ({ dialogs: s.dialogs.filter(d => d.id !== id) })); resolve(v); };
    layers.set(s => ({ dialogs: [...s.dialogs, { ...spec, id, close }] }));
  });
}
export const confirm = (title, message, opts = {}) => ask({ title, message, ok: opts.ok || t("ui.dialog.continuer"), danger: opts.danger }).then(v => v === true);
export const prompt = (title, opts = {}) => ask({ title, message: opts.message, input: { placeholder: opts.placeholder, value: opts.value }, ok: opts.ok || t("ui.dialog.enregistrer") });

export function toast(text, kind) {
  const id = ++seq;
  layers.set(s => ({ toasts: [...s.toasts.slice(-2), { id, text, kind }] }));
  setTimeout(() => layers.set(s => ({ toasts: s.toasts.filter(localT => localT.id !== id) })), kind === 'err' ? 4200 : 2200);
}

// Rendu directement dans <body> : un dialogue ouvert depuis une section animée
// (transform) resterait sinon pris dans son contexte d'empilement.
export function Portal({ children }) {
  const host = useRef(null);
  useLayoutEffect(() => {
    host.current = document.createElement('div');
    document.body.appendChild(host.current);
    return () => { render(null, host.current); host.current.remove(); };
  }, []);
  useLayoutEffect(() => { render(children, host.current); });
  return null;
}

// Dialogue générique (composant) : scrim + panneau, Échap et clic extérieur ferment.
export function Modal({ title, sub, onClose, children, foot, wide }) {
  const box = useRef();
  useEffect(() => {
    const prev = document.activeElement;
    const key = e => { if (e.key === 'Escape') { e.stopPropagation(); onClose && onClose(); } };
    document.addEventListener('keydown', key, true);
    const f = box.current && box.current.querySelector('input,textarea,select,button.primary');
    if (f) setTimeout(() => f.focus(), 30);
    return () => { document.removeEventListener('keydown', key, true); prev && prev.focus && prev.focus(); };
  }, []);
  return html`<${Portal}><div class="scrim" onMouseDown=${e => { if (e.target === e.currentTarget) onClose && onClose(); }}>
    <div class=${cls('dialog', wide && 'wide')} role="dialog" aria-modal="true" aria-label=${title} ref=${box}>
      <div class="dialog-head"><div style="flex:1;min-width:0"><h2>${title}</h2>${sub && html`<p>${sub}</p>`}</div>
        ${onClose && html`<button class="icon-btn" aria-label="${t("ui.dialog.fermer")}" onClick=${onClose}><${Icon} n="close" /></button>`}</div>
      <div class="dialog-body">${children}</div>
      ${foot && html`<div class="dialog-foot">${foot}</div>`}
    </div></div></${Portal}>`;
}

function AskDialog({ d }) {
  const [v, setV] = useState((d.input && d.input.value) || '');
  const ok = () => d.close(d.input ? v : true);
  return html`<${Modal} title=${d.title} onClose=${() => d.close(null)}
    foot=${html`<button class="btn ghost" onClick=${() => d.close(null)}>${t("ui.dialog.annuler")}</button><button class=${cls('btn primary', d.danger && 'danger')} onClick=${ok}>${d.ok || t("ui.dialog.continuer")}</button>`}>
    ${d.message && html`<p style="margin:0;color:var(--text-2);line-height:1.55">${d.message}</p>`}
    ${d.input && html`<input class="input" type=${d.input.type || 'text'} autocomplete=${d.input.autocomplete} placeholder=${d.input.placeholder || ''} value=${v} onInput=${e => setV(e.target.value)} onKeyDown=${e => e.key === 'Enter' && ok()} />`}
  </${Modal}>`;
}

export function Layers() {
  const { dialogs, toasts } = useStore(layers);
  return html`${dialogs.map(d => html`<${AskDialog} key=${d.id} d=${d} />`)}
    <div class="toasts" aria-live="polite">${toasts.map(localT => html`<div key=${localT.id} class=${cls('toast', localT.kind === 'err' && 'err')}>${localT.text}</div>`)}</div>`;
}
