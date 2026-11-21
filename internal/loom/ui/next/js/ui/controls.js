import { t } from '../core/i18n.js';
// Contrôles réutilisables : segmenté animé, onglets, interrupteur, curseur,
// info-bulle ⓘ, popover ancré.
import { html, render, useRef, useLayoutEffect, useEffect, useState, cls } from '../core/lib.js';
import { Icon } from './icons.js';
import { popoverPosition } from './popover-position.js';
import { Modal, Portal, topModal } from './dialog.js';
import { usePhone } from '../core/viewport.js';

export function Seg({ value, options, onChange, size, label, class: c }) {
  const box = useRef(), ind = useRef();
  useLayoutEffect(() => {
    const buttons = [...box.current.querySelectorAll('button')];
    const selected = buttons.find(button => button.getAttribute('aria-selected') === 'true');
    const pill = ind.current;
    pill.hidden = !selected;
    if (!selected) return;
    const measure = () => {
      pill.style.width = selected.offsetWidth + 'px';
      pill.style.height = selected.offsetHeight + 'px';
      pill.style.transform = `translate(${selected.offsetLeft}px,${selected.offsetTop}px)`;
    };
    measure();
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(measure) : null;
    observer?.observe(box.current);
    buttons.forEach(button => observer?.observe(button));
    return () => observer?.disconnect();
  });
  return html`<div class=${cls('seg', size, c)} role="tablist" aria-label=${label} ref=${box}>
    <span class="ind" ref=${ind} aria-hidden="true"></span>
    ${options.map(o => html`<button type="button" key=${o.value} role="tab" aria-selected=${String(o.value === value)} onClick=${() => onChange(o.value)} disabled=${o.disabled}>
      ${o.label}${o.count != null && o.count !== '' ? html` <span class="n">${o.count}</span>` : ''}</button>`)}
  </div>`;
}

export const DropdownButton = ({ open, popup = 'menu', class: c, children, ...props }) =>
  html`<button type="button" class=${cls('btn select', c)} aria-haspopup=${popup} aria-expanded=${String(!!open)} ...${props}>
    ${children}<${Icon} n="chevron" class="select-caret" />
  </button>`;

export const Switch = ({ checked, onChange, disabled, label }) =>
  html`<button type="button" class="switch" role="switch" aria-checked=${String(!!checked)} aria-label=${label} disabled=${disabled} onClick=${() => onChange(!checked)}></button>`;

// Curseur : piste + remplissage + poignée, piloté par un input range invisible
// (clavier et lecteurs d'écran restent natifs).
export function Slider({ value, min, max, step, onInput, onChange, label }) {
  const pct = max > min ? Math.max(0, Math.min(100, (Number(value) - min) * 100 / (max - min))) : 0;
  return html`<div class="slider">
    <span class="rail"></span><span class="fill" style=${`width:${pct}%`}></span><span class="knob" style=${`left:${pct}%`}></span>
    <input type="range" min=${min} max=${max} step=${step || 1} value=${value} aria-label=${label}
      onInput=${e => onInput && onInput(Number(e.target.value))} onChange=${e => onChange && onChange(Number(e.target.value))} />
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
  // Au toucher, iOS simule un survol qui laisserait la bulle ouverte : seule la
  // souris ouvre au survol ; un toucher ouvre, le toucher suivant referme.
  const enter = e => { if (e.pointerType === 'mouse') show(); };
  const leave = e => { if (e.pointerType === 'mouse') hide(); };
  const tap = e => {
    e.preventDefault(); e.stopPropagation();
    show();
    const away = () => { hide(); document.removeEventListener('touchstart', away, true); window.removeEventListener('scroll', away, true); };
    setTimeout(() => { document.addEventListener('touchstart', away, true); window.addEventListener('scroll', away, true); }, 0);
  };
  return html`<span class="tip" tabindex="0" ref=${ref} onPointerEnter=${enter} onPointerLeave=${leave} onClick=${tap} onBlur=${hide} aria-label=${text}>${t("ui.controls.i")}</span>`;
}

// Popover ancré sous (ou au-dessus de) son déclencheur, fermé par clic extérieur/Échap.
export function Popover({ sheet, label, children, ...props }) {
  const phone = usePhone();
  if (sheet && phone) return html`<${Modal} title=${label} onClose=${props.onClose} class=${props.class} focusField=${false}>${children}</${Modal}>`;
  return html`<${AnchoredPopover} ...${props} label=${label}>${children}</${AnchoredPopover}>`;
}

function AnchoredPopover({ anchor, onClose, children, width, place, heightLimit, heightRatio = 1, class: c, role = 'dialog', label }) {
  const box = useRef(), close = useRef(onClose); close.current = onClose;
  const [st, setSt] = useState({ visibility: 'hidden' });
  useLayoutEffect(() => {
    if (!anchor || !box.current) return;
    const el = box.current, viewport = window.visualViewport;
    let frame;
    const update = () => {
      frame = null;
      if (!anchor.isConnected || !anchor.getClientRects().length) { close.current(); return; }
      const bounds = { left: viewport?.offsetLeft || 0, top: viewport?.offsetTop || 0,
        width: viewport?.width || innerWidth, height: viewport?.height || innerHeight };
      const maxWidth = Math.max(0, Math.min(width ?? 360, bounds.width - 16)) + 'px';
      const height = Math.max(0, Math.min(bounds.height - 16, heightLimit ?? Infinity, bounds.height * heightRatio));
      if (el.style.maxWidth !== maxWidth) el.style.maxWidth = maxWidth;
      el.style.maxHeight = height + 'px';
      const next = { ...popoverPosition(anchor.getBoundingClientRect(), el.offsetWidth, el.offsetHeight, bounds, place), maxWidth };
      next.maxHeight = Math.min(height, parseFloat(next.maxHeight)) + 'px';
      el.style.maxHeight = next.maxHeight;
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
  useLayoutEffect(() => {
    const clicked = e => {
      if (!box.current || box.current.contains(e.target)) return;
      if (anchor?.contains(e.target) && anchor.matches('button, a, [role="button"]')) {
        // The open layer owns the trigger's next activation. Suppress its
        // open handler, even when the trigger has no toggle of its own.
        e.preventDefault();
        e.stopPropagation();
        anchor.focus({ preventScroll: true });
      }
      close.current();
    };
    const key = e => {
      if (e.key !== 'Escape' || e.defaultPrevented) return;
      const modal = topModal();
      if (modal && !modal.contains(anchor)) return;
      e.preventDefault();
      e.stopPropagation();
      close.current();
      if (anchor?.isConnected) anchor.focus({ preventScroll: true });
    };
    // A completed click covers mouse, touch and keyboard activation.
    document.addEventListener('click', clicked, true); document.addEventListener('keydown', key, true);
    return () => { document.removeEventListener('click', clicked, true); document.removeEventListener('keydown', key, true); };
  }, [anchor]);
  const style = Object.entries({ ...st, width: width ? width + 'px' : undefined }).filter(([, v]) => v).map(([k, v]) => k.replace(/[A-Z]/g, m => '-' + m.toLowerCase()) + ':' + v).join(';');
  return html`<${Portal}><div class=${cls('pop', c)} data-in-dialog=${anchor?.closest('.dialog') ? '' : undefined}
    ref=${box} style=${style} role=${role} aria-label=${label} onClick=${e => e.stopPropagation()}>${children}</div></${Portal}>`;
}

export function Menu({ anchor, onClose, items, label, role = 'menu', ...position }) {
  const list = useRef();
  const itemRole = role === 'listbox' ? 'option' : 'menuitem';
  const enabled = () => [...list.current.querySelectorAll(`[role="${itemRole}"]:not(:disabled)`)];
  const restore = () => { onClose(); if (anchor.isConnected) anchor.focus({ preventScroll: true }); };
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      const options = enabled();
      const first = options.find(el => el.matches('[aria-selected="true"],[aria-current="page"]')) || options[0];
      first?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, [anchor]);
  const key = e => {
    if (e.key === 'Tab') { restore(); return; }
    const items = enabled(), index = items.indexOf(e.target);
    let next;
    if (e.key === 'ArrowDown') next = items[(index + 1) % items.length];
    else if (e.key === 'ArrowUp') next = items[(index - 1 + items.length) % items.length];
    else if (e.key === 'Home') next = items[0];
    else if (e.key === 'End') next = items.at(-1);
    else if (e.key.length === 1 && e.key.trim() && !e.ctrlKey && !e.metaKey && !e.altKey) {
      const letter = e.key.toLocaleLowerCase();
      next = [...items.slice(index + 1), ...items.slice(0, index + 1)]
        .find(item => item.querySelector('.item-title').textContent.toLocaleLowerCase().startsWith(letter));
    }
    if (next) { e.preventDefault(); next.focus(); }
  };
  const row = (it, i) => {
    if (it === '-') return html`<hr key=${i} role="separator" />`;
    const props = { class: cls('item', it.danger && 'danger'), role: itemRole, tabIndex: -1 };
    const content = html`${it.icon && html`<${Icon} n=${it.icon} />`}
      <span class="item-label"><span class="item-title">${it.label}</span>${it.note && html`<small>${it.note}</small>`}</span>
      ${(it.selected || it.current) && html`<${Icon} n="check" class="item-check" />`}`;
    return it.href
      ? html`<a key=${it.href} ...${props} href=${it.href} target=${it.target} rel=${it.rel} aria-current=${it.current ? 'page' : undefined} onClick=${restore}>${content}</a>`
      : html`<button key=${it.key ?? i} type="button" ...${props} disabled=${it.disabled}
          aria-selected=${role === 'listbox' ? String(!!it.selected) : undefined}
          onClick=${() => { restore(); it.run(); }}>${content}</button>`;
  };
  const groups = new Map();
  for (const it of items) {
    const group = it.group || '';
    if (!groups.has(group)) groups.set(group, []);
    groups.get(group).push(it);
  }
  const name = label || anchor.getAttribute('aria-label') || anchor.labels?.[0]?.textContent.trim() || anchor.textContent.trim() || undefined;
  return html`<${Popover} anchor=${anchor} onClose=${onClose} role="presentation" class="menu" sheet label=${name} ...${position}>
    <div role=${role} aria-label=${name} ref=${list} onKeyDown=${key}>
      ${[...groups].map(([group, rows]) => group
        ? html`<div key=${group} role="group" aria-label=${group}><div class="item-group" aria-hidden="true">${group}</div>${rows.map(row)}</div>`
        : rows.map(row))}
    </div>
  </${Popover}>`;
}

export function OverflowMenu({ label, items, disabled }) {
  const [anchor, setAnchor] = useState(null);
  return html`<button type="button" class="icon-btn" aria-label=${label} disabled=${disabled}
    aria-haspopup="menu" aria-expanded=${!!anchor} onClick=${e => setAnchor(e.currentTarget)}><${Icon} n="more" /></button>
    ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${items} />`}`;
}

export const Empty = ({ icon, title, text, children }) => html`<div class="empty anim-rise"><${Icon} n=${icon || 'info'} /><h3>${title}</h3>${text && html`<p>${text}</p>`}${children}</div>`;
