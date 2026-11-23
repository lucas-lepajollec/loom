import { t, getLang } from '../core/i18n.js';
// Palette de recherche (Ctrl K) : pages, discussions, modèles et
// actions au même endroit. Flèches pour naviguer, Entrée pour ouvrir.
import { html, useState, useEffect, useRef, useStore, useMemo } from '../core/lib.js';
import { Icon } from '../ui/icons.js';
import { Logo } from '../ui/logo.js';
import { Modal } from '../ui/dialog.js';
import { app, go, setTheme } from '../core/state.js';
import { open, newDiscussion, chooseLocal, chooseRemote } from '../features/chat/engine.js';
import { vendorOf } from '../features/chat/picker.js';
import { NAV_ITEMS } from './routes.js';
import { usePhone } from '../core/viewport.js';
import { agents, cliOf } from '../features/agents/store.js';
import { openSession } from '../features/agents/nav.js';

const norm = s => String(s || '').toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');
export const openPalette = () => app.set({ palette: true });

function entries(s, phone, sessions) {
  const out = [];
  const add = (group, e) => out.push({ group, ...e });
  add(t('app.shell.actions'), { icon: 'plus', title: phone ? t('side.new_session') : t("app.palette.nouvelle_discussion"), hint: phone ? '' : t("app.palette.ctrl_maj_o"), run: () => phone ? agents.set({ creating: { type: 'chat' } }) : newDiscussion() });
  add(t('app.shell.actions'), { icon: s.theme === 'light' ? 'moon' : 'sun', title: s.theme === 'light' ? t("app.palette.passer_en_theme_sombre") : t("app.palette.passer_en_theme_clair"), run: () => setTheme(s.theme === 'light' ? 'dark' : 'light') });
  for (const r of NAV_ITEMS) add(t("app.palette.pages"), { icon: r.nav.icon, title: r.nav.label, run: () => { location.hash = r.href; } });
  add(t("app.palette.pages"), { icon: 'gear', title: t("app.palette.reglages"), run: () => go('settings') });
  for (const c of s.nav.conversations) add(t("app.palette.discussions"), { icon: 'chat', title: c.title || t("app.palette.nouvelle_discussion"), run: () => { if (phone) go('chat', c.id); else { open(c.id); go('chat'); } } });
  if (phone) for (const session of Object.values(sessions)) add(t('agents.title'), { icon: 'agent', title: session.name || session.id, sub: cliOf(session.mode).label, run: () => openSession(session.id) });
  for (const p of s.presets || []) add(t("app.palette.modeles"), { logo: vendorOf(p.model || p.name), title: p.name, sub: t("app.palette.preset_local"), run: () => { go('chat'); chooseLocal({ presetIndex: s.presets.indexOf(p) + 1, name: p.name, model: p.model }); } });
  for (const m of (s.models || []).filter(m => !/mmproj/i.test(m.name))) add(t("app.palette.modeles"), { logo: vendorOf(m.name), title: m.name.replace(/\.gguf$/i, ''), sub: 'local', run: () => { go('chat'); chooseLocal({ model: m.value || m.path, name: m.name }); } });
  for (const m of ((s.workspace && s.workspace.models) || []).filter(m => m.kind !== 'local' && m.enabled))
    add(t("app.palette.modeles"), { logo: m.kind === 'cloud' ? m.provider_name : m.runtime_id, title: m.name, sub: m.provider_name, run: () => { go('chat'); chooseRemote(m); } });
  return out;
}

export function Palette() {
  const phone = usePhone();
  const s = useStore(app, a => ({ on: a.palette, nav: a.nav, presets: a.presets, models: a.models, workspace: a.workspace, theme: a.theme }));
  const sessions = useStore(agents, a => phone && s.on ? a.sessions : null);
  const [q, setQ] = useState('');
  const [i, setI] = useState(0);
  const list = useRef();
  useEffect(() => { if (s.on) { setQ(''); setI(0); } }, [s.on]);
  const all = useMemo(() => (s.on ? entries(s, phone, sessions || {}) : []), [s.on, s.nav, s.presets, s.models, s.workspace, s.theme, phone, sessions, getLang()]);
  const shown = useMemo(() => {
    const words = norm(q).split(/\s+/).filter(Boolean);
    const hit = e => words.every(w => norm(e.title + ' ' + (e.sub || '') + ' ' + e.group).includes(w));
    if (!words.length) return all.filter(e => e.group !== t("app.palette.modeles") && e.group !== t("app.palette.projets") && e.group !== t('agents.title')).filter((e, n, a) => e.group !== t("app.palette.discussions") || a.slice(0, n).filter(x => x.group === t("app.palette.discussions")).length < 6);
    return all.filter(hit).slice(0, 40);
  }, [all, q]);
  useEffect(() => { const el = list.current && list.current.querySelector('[aria-selected="true"]'); el && el.scrollIntoView({ block: 'nearest' }); }, [i]);
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
            <span class="t">${e.title}</span>${e.sub && html`<span class="s">${e.sub}</span>`}${e.hint && html`<span class="kbd">${e.hint}</span>`}</button>`;
        }) : html`<div class="palette-empty">${t("app.palette.aucun_resultat_pour")} ${q} ».</div>`}
      </div>
    </${Modal}>`;
}
