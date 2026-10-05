import { t } from '../core/i18n.js';
// Contrôles réutilisables : segmenté animé, onglets, interrupteur, curseur,
// info-bulle ⓘ, popover ancré.
import { html, render, useRef, useLayoutEffect, useEffect, useState, cls } from '../core/lib.js';
import { Icon } from './icons.js';
import { popoverPosition } from './popover-position.js';
import { Portal } from './dialog.js';

export function Seg({ value, options, onChange, size, label }) {
  const box = useRef(), ind = useRef();
  useLayoutEffect(() => {
    const on = box.current && box.current.querySelector('[aria-selected="true"]');
    if (on && ind.current) { ind.current.style.width = on.offsetWidth + 'px'; ind.current.style.transform = `translateX(${on.offsetLeft}px)`; }
  });
  return html`<div class=${cls('seg', size)} role="tablist" aria-label=${label} ref=${box}>
    <span class="ind" ref=${ind} aria-hidden="true"></span>
    ${options.map(o => html`<button type="button" role="tab" aria-selected=${String(o.value === value)} onClick=${() => onChange(o.value)} disabled=${o.disabled}>
      ${o.label}${o.count != null && o.count !== '' ? html` <span class="n">${o.count}</span>` : ''}</button>`)}
  </div>`;
}

export function Tabs({ value, options, onChange, label }) {
  return html`<div class="tabs" role="tablist" aria-label=${label}>
    ${options.map(o => html`<button type="button" role="tab" aria-selected=${String(o.value === value)} onClick=${() => onChange(o.value)}>${o.label}${o.count != null ? html`<span class="n">${o.count}</span>` : ''}</button>`)}
  </div>`;
}

export const Switch = ({ checked, onChange, disabled, label }) =>
  html`<button type="button" class="switch" role="switch" aria-checked=${String(!!checked)} aria-label=${label} disabled=${disabled} onClick=${() => onChange(!checked)}></button>`;

// Curseur : piste + remplissage + poignée, piloté par un input range invisible
// (clavier et lecteurs d'écran restent natifs).
export function Slider({ value, min, max, step, onInput, onChange, label }) {
  const [drag, setDrag] = useState(false);
  const pct = max > min ? Math.max(0, Math.min(100, (Number(value) - min) * 100 / (max - min))) : 0;
  return html`<div class=${cls('slider', drag && 'drag')}>
    <span class="rail"></span><span class="fill" style=${`width:${pct}%`}></span><span class="knob" style=${`left:${pct}%`}></span>
    <input type="range" min=${min} max=${max} step=${step || 1} value=${value} aria-label=${label}
      onInput=${e => onInput && onInput(Number(e.target.value))} onChange=${e => onChange && onChange(Number(e.target.value))}
      onPointerDown=${() => setDrag(true)} onPointerUp=${() => setDrag(false)} />
  </div>`;
}

// ⓘ : l'explication vit dans une bulle au survol/focus, jamais en paragraphe.
// La bulle est rendue à la racine du document : un parent animé (transform)
// décalerait sinon sa position « fixed » loin de l'icône.
let tipLayer = null;
export function Tip({ text }) {
  const ref = useRef();
  const show = () => {
    const r = ref.current.getBoundingClientRect();
    if (!tipLayer) { tipLayer = document.createElement('div'); document.body.appendChild(tipLayer); }
    const x = Math.max(8, Math.min(r.left - 6, innerWidth - 296));
    const below = r.bottom + 8 + 120 < innerHeight;
    render(html`<span class="tipbox" role="tooltip" style=${`left:${x}px;${below ? `top:${r.bottom + 8}px` : `bottom:${innerHeight - r.top + 8}px`}`}>${text}</span>`, tipLayer);
  };
  const hide = () => { if (tipLayer) render(null, tipLayer); };
  useEffect(() => hide, []);
  return html`<span class="tip" tabindex="0" ref=${ref} onMouseEnter=${show} onMouseLeave=${hide} onFocus=${show} onBlur=${hide} aria-label=${text}>${t("ui.controls.i")}</span>`;
}

// Popover ancré sous (ou au-dessus de) son déclencheur, fermé par clic extérieur/Échap.
export function Popover({ anchor, onClose, children, width, place, heightLimit, heightRatio = 1, class: c }) {
  const box = useRef(), close = useRef(onClose); close.current = onClose;
  const [st, setSt] = useState({ visibility: 'hidden' });
  useLayoutEffect(() => {
    if (!anchor || !box.current) return;
    const el = box.current, viewport = window.visualViewport;
    let frame;
    const update = () => {
      frame = null;
      if (!anchor.isConnected) { close.current(); return; }
      const bounds = { left: viewport?.offsetLeft || 0, top: viewport?.offsetTop || 0,
        width: viewport?.width || innerWidth, height: viewport?.height || innerHeight };
      const maxWidth = Math.max(0, bounds.width - 16) + 'px';
      const maxHeight = Math.max(0, Math.min(bounds.height - 16, heightLimit ?? Infinity, bounds.height * heightRatio)) + 'px';
      if (el.style.maxWidth !== maxWidth) el.style.maxWidth = maxWidth;
      if (el.style.maxHeight !== maxHeight) el.style.maxHeight = maxHeight;
      const next = { ...popoverPosition(anchor.getBoundingClientRect(), el.offsetWidth, el.offsetHeight, bounds, place), maxWidth, maxHeight };
      setSt(old => Object.keys(next).every(key => old[key] === next[key]) && !old.visibility ? old : next);
    };
    const schedule = e => {
      // Scrolling the menu itself does not move its anchor.
      if (e?.type === 'scroll' && el.contains(e.target)) return;
      if (frame == null) frame = requestAnimationFrame(update);
    };
    update();
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(schedule) : null;
    observer?.observe(el); observer?.observe(anchor);
    window.addEventListener('resize', schedule);
    window.addEventListener('scroll', schedule, true);
    viewport?.addEventListener('resize', schedule); viewport?.addEventListener('scroll', schedule);
    return () => {
      if (frame != null) cancelAnimationFrame(frame);
      observer?.disconnect(); window.removeEventListener('resize', schedule); window.removeEventListener('scroll', schedule, true);
      viewport?.removeEventListener('resize', schedule); viewport?.removeEventListener('scroll', schedule);
    };
  }, [anchor, width, place, heightLimit, heightRatio]);
  useEffect(() => {
    const clicked = e => { if (box.current && !box.current.contains(e.target) && !anchor?.contains(e.target)) close.current(); };
    const key = e => { if (e.key === 'Escape' && !e.defaultPrevented) { e.preventDefault(); close.current(); if (anchor?.isConnected) anchor.focus(); } };
    // A completed click works for mouse, touch and keyboard activation. Do not
    // dismantle a layer on pointerdown before the intended action can run.
    document.addEventListener('click', clicked, true); document.addEventListener('keydown', key);
    return () => { document.removeEventListener('click', clicked, true); document.removeEventListener('keydown', key); };
  }, [anchor]);
  const style = Object.entries({ ...st, width: width ? width + 'px' : undefined }).filter(([, v]) => v).map(([k, v]) => k.replace(/[A-Z]/g, m => '-' + m.toLowerCase()) + ':' + v).join(';');
  return html`<${Portal}><div class=${cls('pop', c)} ref=${box} style=${style} role="dialog" onClick=${e => e.stopPropagation()}>${children}</div></${Portal}>`;
}

// Menu contextuel simple : liste d'actions dans un Popover.
export function Menu({ anchor, onClose, items }) {
  return html`<${Popover} anchor=${anchor} onClose=${onClose}>
    ${items.map(it => it === '-' ? html`<hr />` : html`<button class=${cls('item', it.danger && 'danger')} onClick=${() => { onClose(); it.run(); }}>${it.icon && html`<${Icon} n=${it.icon} />`}${it.label}</button>`)}
  </${Popover}>`;
}

export const Empty = ({ icon, title, text, children }) => html`<div class="empty anim-rise"><${Icon} n=${icon || 'info'} /><h3>${title}</h3>${text && html`<p>${text}</p>`}${children}</div>`;
