import { t, getLang } from '../core/i18n.js';
// Palette de recherche (Ctrl K) : pages, discussions, modèles et
// actions au même endroit. Flèches pour naviguer, Entrée pour ouvrir.
import { html, useState, useEffect, useRef, useStore, useMemo } from '../core/lib.js';
import { Icon } from '../ui/icons.js';
import { Logo } from '../ui/logo.js';
import { get } from '../core/api.js';
import { Modal } from '../ui/dialog.js';
import { app, go, setTheme } from '../core/state.js';
import { open, chooseLocal, chooseRemote } from '../features/chat/engine.js';
import { vendorOf } from '../features/chat/picker.js';
import { NAV_ITEMS } from './routes.js';
import { usePhone } from '../core/viewport.js';
import { agents, cliOf } from '../features/agents/store.js';
import { openSession } from '../features/agents/nav.js';

import { scoreText, matchRanges } from '../ui/search-rank.js';
import { settingsSections } from '../features/settings/catalog.js';

function Highlight({ text, query }) {
  const parts = [];
  let offset = 0;
  for (const [start, end] of matchRanges(text, query)) {
    parts.push(text.slice(offset, start), html`<mark>${text.slice(start, end)}</mark>`);
    offset = end;
  }
  return [...parts, text.slice(offset)];
}
export const openPalette = () => app.set({ palette: true });

function entries(s, phone, sessions, machines) {
  const out = [];
  const add = (group, e) => out.push({ group, ...e });
  add(t('app.shell.actions'), { icon: 'plus', title: t('side.new_session'), hint: phone ? '' : t("app.palette.ctrl_maj_o"), run: () => agents.set({ creating: { type: 'chat' } }) });
  add(t('app.shell.actions'), { icon: s.theme === 'light' ? 'moon' : 'sun', title: s.theme === 'light' ? t("app.palette.passer_en_theme_sombre") : t("app.palette.passer_en_theme_clair"), run: () => setTheme(s.theme === 'light' ? 'dark' : 'light') });
  for (const r of NAV_ITEMS) add(t("app.palette.pages"), { icon: r.nav.icon, title: r.nav.label, run: () => { location.hash = r.href; } });
  add(t("app.palette.pages"), { icon: 'gear', title: t("app.palette.reglages"), run: () => go('settings') });
  for (const item of settingsSections()) add(t('settings.page.reglages'), { icon: item.icon, title: item.title, keywords: item.keywords, run: () => go('settings', item.id) });
  add(t('app.palette.pages'), { icon: 'inbox', title: t('mobile.inbox'), run: () => go('inbox') });
  const headings = document.querySelectorAll('main .view.page :is(h2,h3), main .fl-doc :is(h1,h2,h3), main .cm-admin-top h2, main .cm-cc h2');
  for (const heading of headings) {
    if (!heading.getClientRects().length || heading.closest('nav')) continue;
    const title = heading.textContent.trim();
    if (title) add(t('workspace.on_page'), { icon: 'list', title, run: () => requestAnimationFrame(() => {
      if (heading.isConnected) { heading.tabIndex = -1; heading.focus({ preventScroll: true }); heading.scrollIntoView({ block: 'center' }); }
    }) });
  }
  for (const c of s.nav.conversations) add(t("app.palette.discussions"), { icon: 'chat', title: c.title || t("app.palette.nouvelle_discussion"), run: () => { if (phone) go('chat', c.id); else { open(c.id); go('chat'); } } });
  for (const session of Object.values(sessions)) add(t('agents.title'), { icon: 'agent', title: session.name || session.id, sub: cliOf(session.mode).label, run: () => openSession(session.id) });
  for (const machine of machines) add(t('settings.page.machines'), {
    icon: 'server', title: machine.name || machine.hostname || machine.id, sub: machine.host || '',
    run: () => go('machines', machine.id),
  });
  for (const p of s.presets || []) add(t("app.palette.modeles"), { logo: vendorOf(p.model || p.name), title: p.name, sub: t("app.palette.preset_local"), run: () => { go('chat'); chooseLocal({ presetIndex: s.presets.indexOf(p) + 1, name: p.name, model: p.model }); } });
  for (const m of (s.models || []).filter(m => !/mmproj/i.test(m.name))) add(t("app.palette.modeles"), { logo: vendorOf(m.name), title: m.name.replace(/\.gguf$/i, ''), sub: 'local', run: () => { go('chat'); chooseLocal({ model: m.value || m.path, name: m.name }); } });
  for (const m of ((s.workspace && s.workspace.models) || []).filter(m => m.kind !== 'local' && m.enabled))
    add(t("app.palette.modeles"), { logo: m.kind === 'cloud' ? m.provider_name : m.runtime_id, title: m.name, sub: m.provider_name, run: () => { go('chat'); chooseRemote(m); } });
  return out;
}

export function Palette() {
  const phone = usePhone();
  const s = useStore(app, a => ({ on: a.palette, nav: a.nav, presets: a.presets, models: a.models, workspace: a.workspace, theme: a.theme }));
  const sessions = useStore(agents, a => s.on ? a.sessions : null);
  const [machines, setMachines] = useState([]);
  useEffect(() => {
    if (!s.on) return;
    const controller = new AbortController();
    setMachines([]);
    get('/api/machines', { signal: controller.signal, retryAuth: false }).then(result => {
      if (!controller.signal.aborted && Array.isArray(result?.machines)) setMachines(result.machines);
    }).catch(() => {});
    return () => controller.abort();
  }, [s.on]);
  const [q, setQ] = useState('');
  const [i, setI] = useState(0);
  const list = useRef();
  useEffect(() => { if (s.on) { setQ(''); setI(0); } }, [s.on]);
  const all = useMemo(() => (s.on ? entries(s, phone, sessions || {}, machines) : []), [s.on, s.nav, s.presets, s.models, s.workspace, s.theme, phone, sessions, machines, getLang()]);
  const shown = useMemo(() => {
    if (!q.trim()) return all.filter(e => ![t('workspace.on_page'), t('app.palette.modeles'), t('app.palette.projets'), t('settings.page.reglages'), t('agents.title')].includes(e.group)).filter((e, n, a) => e.group !== t('app.palette.discussions') || a.slice(0, n).filter(x => x.group === t('app.palette.discussions')).length < 6);
    const groups = new Map();
    for (const entry of all) {
      const score = scoreText({ title: entry.title, keywords: [entry.group, entry.sub || '', ...(entry.keywords || [])] }, q);
      if (!score) continue;
      if (!groups.has(entry.group)) groups.set(entry.group, []);
      groups.get(entry.group).push({ entry, score });
    }
    const ranked = [...groups.values()].map(rows => rows.sort((a, b) => b.score - a.score));
    return ranked.sort((a, b) => b[0].score - a[0].score).flat().slice(0, 40).map(row => row.entry);
  }, [all, q]);
  useEffect(() => { const el = list.current && list.current.querySelector('[aria-selected="true"]'); el && el.scrollIntoView({ block: 'nearest' }); }, [i, q]);
  if (!s.on) return null;
  const close = () => app.set({ palette: false });
  const run = e => { close(); e.run(); };
  const key = ev => {
    if (ev.key === 'ArrowDown') { ev.preventDefault(); setI(Math.min(shown.length - 1, i + 1)); }
    else if (ev.key === 'ArrowUp') { ev.preventDefault(); setI(Math.max(0, i - 1)); }
    else if (ev.key === 'Enter' && shown[i]) { ev.preventDefault(); run(shown[i]); }
  };
  let last = '';
  return html`<${Modal} class="palette" title=${t("app.palette.rechercher")} onClose=${close}
    foot=${html`<div class="palette-hints"><span><span class="kbd">↑</span><span class="kbd">↓</span> ${t("app.palette.naviguer")}</span><span><span class="kbd">${t("app.palette.entree")}</span> ${t("app.palette.ouvrir")}</span><span><span class="kbd">${t("app.palette.echap")}</span> ${t("app.palette.fermer")}</span></div>`}>
      <label class="search"><${Icon} n="search" /><input aria-label=${t("app.palette.rechercher")} placeholder=${t("app.palette.rechercher_une_discussion_un_modele_une_page")} value=${q}
        onInput=${e => { setQ(e.target.value); setI(0); }} onKeyDown=${key} role="combobox" aria-expanded="true" aria-controls="palette-list"
        aria-activedescendant=${shown[i] ? 'palette-option-' + i : undefined} /></label>
      <div class="palette-list" id="palette-list" role="listbox" ref=${list}>
        ${shown.length ? shown.map((e, n) => {
          const head = e.group !== last ? (last = e.group, html`<div class="item-group">${e.group}</div>`) : '';
          return html`${head}<button type="button" id=${'palette-option-' + n} role="option" tabIndex="-1"
            aria-selected=${String(n === i)} class="item palette-row" onMouseMove=${() => n !== i && setI(n)} onClick=${() => run(e)}>
            ${e.logo ? html`<${Logo} name=${e.logo} size="xs" />` : html`<${Icon} n=${e.icon} />`}
            <span class="t"><${Highlight} text=${e.title} query=${q} /></span>${e.sub && html`<span class="s">${e.sub}</span>`}${e.hint && html`<span class="kbd">${e.hint}</span>`}</button>`;
        }) : html`<div class="palette-empty">${t("app.palette.aucun_resultat_pour")} ${q} ».</div>`}
      </div>
    </${Modal}>`;
}
