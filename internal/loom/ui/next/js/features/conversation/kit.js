// Kit commun des conversations (sessions d'agent et discussions Loom) :
// pli de travail, lignes d'étape, carte des fichiers modifiés, pied de tour,
// bouton « revenir en bas » et carte d'accord posée dans le composeur.
// Styles : css/conversation.css (préfixe cv-).
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { copyText } from '../../ui/clipboard.js';
import { toast } from '../../ui/dialog.js';
import { duration } from './turns.js';

// « A travaillé 2 min 14 s › » : replié par défaut, il garde le fil lisible.
export function WorkFold({ label, children, open: start = false }) {
  const [open, setOpen] = useState(start);
  return html`<div class=${cls('cv-fold', open && 'open')}>
    <button type="button" class="cv-work" aria-expanded=${String(open)} onClick=${() => setOpen(!open)}>
      <span>${label}</span><${Icon} n="right" class="cv-chev" /></button>
    ${open && html`<div class="cv-fold-b">${children}</div>`}
  </div>`;
}

export const workLabel = msValue => msValue >= 1000 ? t('conv.worked', { time: duration(msValue) }) : t('conv.worked_short');

// Une ligne grise par étape. live : texte qui scintille, durée qui avance.
export function Step({ icon = 'tool', children, meta, live, since, title }) {
  return html`<div class=${cls('cv-step', live && 'live')} title=${title}>
    ${live ? html`<span class="cv-spin" aria-hidden="true"></span>` : html`<${Icon} n=${icon} />`}
    <span class="cv-step-t">${children}</span>
    ${live && since ? html`<${Elapsed} since=${since} />` : meta && html`<span class="cv-meta">${meta}</span>`}
  </div>`;
}

export function Elapsed({ since }) {
  const [, tick] = useState(0);
  useEffect(() => { const id = setInterval(() => tick(x => x + 1), 1000); return () => clearInterval(id); }, []);
  return html`<span class="cv-meta">${duration(Date.now() - since)}</span>`;
}

const diffLines = text => String(text || '').split('\n').filter(l => !/^(diff --git|index |--- |\+\+\+ )/.test(l));

function DiffView({ text, truncated }) {
  return html`<pre class="cv-diff">${diffLines(text).map((l, i) => html`<span key=${i} class=${l.startsWith('@@') ? 'h' : l[0] === '+' ? 'a' : l[0] === '-' ? 'd' : ''}>${l || ' '}</span>`)}${truncated && html`<span class="h">…</span>`}</pre>`;
}

function FileRow({ file, loadDiff }) {
  const [open, setOpen] = useState(false);
  const [diff, setDiff] = useState(null);
  const toggle = () => {
    setOpen(!open);
    if (!open && diff === null && loadDiff) loadDiff(file).then(setDiff).catch(e => setDiff({ error: e.message }));
  };
  const slash = file.path.lastIndexOf('/');
  return html`<div class=${cls('cv-file', open && 'open')}>
    <button type="button" class="cv-file-h" aria-expanded=${String(open)} onClick=${toggle}>
      <span class="cv-path"><span class="dir">${file.path.slice(0, slash + 1)}</span><span class="name">${file.path.slice(slash + 1)}</span></span>
      <em class="cv-add">+${file.added}</em><em class="cv-del">−${file.removed}</em>
      <${Icon} n="chevron" class="cv-caret" /></button>
    ${open && (diff === null ? html`<div class="cv-diff-wait"><span class="spinner"></span></div>`
      : diff.error ? html`<p class="cv-diff-err">${diff.error}</p>` : html`<${DiffView} text=${diff.text} truncated=${diff.truncated} />`)}
  </div>`;
}

// Carte « N fichiers modifiés +x −y » ; chaque fichier se déplie sur son diff.
export function Changes({ changes, loadDiff, onReview }) {
  if (!changes?.files.length) return null;
  return html`<div class="cv-changes">
    <div class="cv-changes-h"><span>${t('conv.files_changed', { n: changes.files.length })}</span>
      <em class="cv-add">+${changes.added}</em><em class="cv-del">−${changes.removed}</em>
      <span class="grow"></span>
      ${onReview && html`<button type="button" class="cv-link" onClick=${onReview}>${t('conv.review')}<${Icon} n="right" /></button>`}</div>
    ${changes.files.map(f => html`<${FileRow} key=${f.path} file=${f} loadDiff=${loadDiff} />`)}
  </div>`;
}

export function TurnFoot({ parts, copy }) {
  const text = parts.filter(Boolean).join(' · ');
  if (!text && !copy) return null;
  return html`<div class="cv-foot"><span>${text}</span>
    ${copy && html`<button type="button" class="icon-btn" aria-label=${t('chat.messages.copy')} title=${t('chat.messages.copy')}
      onClick=${() => copyText(copy).then(() => toast(t('chat.messages.copied')))}><${Icon} n="copy" /></button>`}</div>`;
}

// Bouton « revenir en bas », visible quand on a remonté le fil.
export function JumpBottom({ scroller, watch }) {
  const [away, setAway] = useState(false);
  useEffect(() => {
    const el = scroller.current; if (!el) return;
    const check = () => setAway(el.scrollHeight - el.scrollTop - el.clientHeight > 240);
    check();
    el.addEventListener('scroll', check, { passive: true });
    return () => el.removeEventListener('scroll', check);
  }, [scroller.current, watch]);
  if (!away) return null;
  const go = () => scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
  return html`<button type="button" class="cv-jump" aria-label=${t('conv.jump')} title=${t('conv.jump')} onClick=${go}><${Icon} n="chevron" /></button>`;
}

// Carte d'accord ou de question, au-dessus du champ du composeur.
// choices : [{ label, hint?, sub?, primary?, deny?, run }]
export function Tray({ icon = 'lock', title, meta, detail, choices, busy, extra, aside, vertical }) {
  return html`<div class="cv-tray anim-rise" role="group" aria-label=${title}>
    <div class="cv-tray-h"><${Icon} n=${icon} /><b>${title}</b><span class="grow"></span>${meta && html`<span class="cv-tray-meta">${meta}</span>`}${aside}</div>
    ${detail && html`<pre class="cv-tray-cmd">${detail}</pre>`}
    <div class=${cls('cv-acts', vertical && 'vertical')}>
      ${choices.map((c, i) => html`<button type="button" key=${i}
          class=${cls('cv-choice', c.primary && 'primary', c.deny && 'deny')} disabled=${busy} onClick=${c.run}>
        <span class="cv-choice-t"><span>${c.label}</span>${c.sub && html`<small>${c.sub}</small>`}</span>
        ${c.hint && html`<kbd aria-hidden="true">${c.hint}</kbd>`}</button>`)}
      ${extra}
    </div>
  </div>`;
}
