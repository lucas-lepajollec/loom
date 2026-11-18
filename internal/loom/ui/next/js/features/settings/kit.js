// Briques communes des pages de réglages : une ligne (libellé, ⓘ, contrôle)
// et un groupe titré.
import { html, cls, useState } from '../../core/lib.js';
import { Tip } from '../../ui/controls.js';
import { Icon } from '../../ui/icons.js';

export const Line = ({ label, tip, children, stack }) => html`<div class=${cls('set-line', stack && 'stack')}><div class="set-l"><span>${label}</span>${tip && html`<${Tip} text=${tip} />`}</div><div class="set-c">${children}</div></div>`;
// Un groupe titré se replie : ouvert par défaut sur ordinateur, replié sur
// téléphone pour que la page reste une liste courte ; le choix est retenu.
const GROUPS_KEY = 'loom.groups';
const groupState = () => { try { return JSON.parse(localStorage.getItem(GROUPS_KEY) || '{}'); } catch (_) { return {}; } };
export function Group({ title, children }) {
  const key = typeof title === 'string' ? title : '';
  const [open, setOpen] = useState(() => { const v = key ? groupState()[key] : undefined; return v === undefined ? !(typeof innerWidth === 'number' && innerWidth <= 720) : v; });
  if (!title) return html`<section class="set-group anim-rise"><div class="card">${children}</div></section>`;
  const toggle = e => {
    const v = e.currentTarget.open; if (v === open) return; setOpen(v);
    if (key) try { const s = groupState(); s[key] = v; localStorage.setItem(GROUPS_KEY, JSON.stringify(s)); } catch (_) {}
  };
  return html`<details class="set-group set-fold anim-rise" open=${open} onToggle=${toggle}><summary><h3>${title}</h3><${Icon} n="chevron" class="set-caret" /></summary><div class="card">${children}</div></details>`;
}
