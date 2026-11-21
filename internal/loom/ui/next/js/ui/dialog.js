import { t } from '../core/i18n.js';
// Couches globales : dialogues impératifs (ask/confirm), toasts, info-bulles.
import { html, render, createStore, useStore, useEffect, useLayoutEffect, useRef, useState, cls } from '../core/lib.js';
import { Icon } from './icons.js';
import { isPhone } from '../core/viewport.js';

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
export const topModal = () => [...document.querySelectorAll('.dialog[aria-modal="true"]')].at(-1);

export function Modal({ title, sub, onClose, children, foot, wide, class: c, focusField = true }) {
  const box = useRef(), close = useRef(onClose), previous = useRef(document.activeElement);
  const gesture = useRef(null);
  close.current = onClose;
  useEffect(() => {
    const prev = previous.current;
    const panel = box.current;
    const key = e => {
      if (panel !== topModal() || e.defaultPrevented) return;
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        close.current?.();
      } else if (e.key === 'Tab') {
        const targets = [...panel.querySelectorAll('button, input, select, textarea, a[href], summary, [tabindex]')]
          .filter(el => !el.disabled && el.tabIndex >= 0 && el.getClientRects().length);
        const first = targets[0], last = targets.at(-1);
        const active = document.activeElement;
        if (!panel.contains(active) || active === panel || active === (e.shiftKey ? first : last)) {
          e.preventDefault();
          (e.shiftKey ? last || panel : first || panel).focus();
        }
      }
    };
    document.addEventListener('keydown', key);
    const frame = requestAnimationFrame(() => {
      if (panel !== topModal() || panel.contains(document.activeElement)) return;
      const field = (!isPhone() || focusField) && panel.querySelector('.dialog-body :is(input, textarea, select):not([type="hidden"]):not(:disabled)');
      const action = panel.querySelector('.dialog-foot .primary:not(:disabled)');
      (field || action || panel).focus({ preventScroll: true });
    });
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener('keydown', key);
      const current = topModal();
      if (prev?.isConnected && (!current || current === panel || current.contains(prev))) prev.focus({ preventScroll: true });
    };
  }, []);
  const reset = () => { gesture.current = null; box.current?.style.removeProperty('--sheet-offset'); };
  const swipe = {
    onTouchStart: e => { if (isPhone() && onClose && e.touches.length === 1) gesture.current = e.touches[0].clientY; },
    onTouchMove: e => {
      if (gesture.current == null) return;
      const dy = e.touches[0].clientY - gesture.current;
      box.current.style.setProperty('--sheet-offset', Math.max(0, dy) + 'px');
    },
    onTouchCancel: reset,
    onTouchEnd: e => {
      const dismiss = gesture.current != null && e.changedTouches[0].clientY - gesture.current > 64;
      reset();
      if (dismiss) onClose();
    },
  };
  return html`<${Portal}><div class="scrim" onClick=${e => { if (e.target === e.currentTarget) onClose?.(); }}>
    <div class=${cls('dialog', wide && 'wide', c)} role="dialog" aria-modal="true" aria-label=${title} tabIndex="-1" ref=${box}>
      ${onClose && html`<div class="sheet-grab" aria-hidden="true" ...${swipe}><span></span></div>`}
      <header class="dialog-head" ...${swipe}><div><h2>${title}</h2>${sub && html`<p>${sub}</p>`}</div>
        ${onClose && html`<button type="button" class="icon-btn" aria-label=${t("ui.dialog.fermer")} onClick=${onClose}><${Icon} n="close" /></button>`}</header>
      <div class="dialog-body">${children}</div>
      ${foot && html`<footer class="dialog-foot">${foot}</footer>`}
    </div></div></${Portal}>`;
}

function AskDialog({ d }) {
  const [v, setV] = useState((d.input && d.input.value) || '');
  const ok = () => d.close(d.input ? v : true);
  return html`<${Modal} title=${d.title} onClose=${() => d.close(null)}
    foot=${html`<button type="button" class="btn" onClick=${() => d.close(null)}>${t("ui.dialog.annuler")}</button><button type="button" class=${cls('btn primary', d.danger && 'danger')} onClick=${ok}>${d.ok || t("ui.dialog.continuer")}</button>`}>
    ${d.message && html`<p style="margin:0;color:var(--text-2);line-height:1.55">${d.message}</p>`}
    ${d.input && html`<input class="input" type=${d.input.type || 'text'} autocomplete=${d.input.autocomplete} placeholder=${d.input.placeholder || ''} value=${v} onInput=${e => setV(e.target.value)} onKeyDown=${e => e.key === 'Enter' && ok()} />`}
  </${Modal}>`;
}

export function Layers() {
  const { dialogs, toasts } = useStore(layers);
  return html`${dialogs.map(d => html`<${AskDialog} key=${d.id} d=${d} />`)}
    <div class="toasts" aria-live="polite">${toasts.map(localT => html`<div key=${localT.id} class=${cls('toast', localT.kind === 'err' && 'err')}>${localT.text}</div>`)}</div>`;
}
