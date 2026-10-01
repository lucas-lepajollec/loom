// Briques communes des pages de réglages : une ligne (libellé, ⓘ, contrôle)
// et un groupe titré.
import { html, cls } from '../../core/lib.js';
import { Tip } from '../../ui/controls.js';

export const Line = ({ label, tip, children, stack }) => html`<div class=${cls('set-line', stack && 'stack')}><div class="set-l"><span>${label}</span>${tip && html`<${Tip} text=${tip} />`}</div><div class="set-c">${children}</div></div>`;
export const Group = ({ title, children }) => html`<section class="set-group anim-rise">${title && html`<h3>${title}</h3>`}<div class="card">${children}</div></section>`;
