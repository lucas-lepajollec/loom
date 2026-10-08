// Skills : des méthodes réutilisables (dossiers SKILL.md) rangées dans le
// cerveau principal, liées aux agents sans copie et lisibles par MCP.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Empty, Menu, Tip } from '../../ui/controls.js';
import { Logo } from '../../ui/logo.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, refreshWorkspace } from '../../core/state.js';
import { FolderPicker } from '../../ui/folder.js';

const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');

// Une skill est un dossier (SKILL.md + fichiers). Celles du dossier de Loom se
// modifient ici ; celles d'un dossier lié se lisent, et peuvent être copiées.
function SkillEditor({ skill, onClose }) {
  const ro = !!(skill && skill.read_only);
  const [name, setName] = useState(skill ? skill.name : '');
  const [desc, setDesc] = useState(skill ? skill.description : '');
  const [instr, setInstr] = useState(skill ? skill.instructions : '');
  const save = async copy => {
    const r = await post('/api/capabilities/save', { id: skill && !copy ? skill.id : '', name, description: desc, instructions: instr });
    if (!r.ok) return toast(r.error, 'err'); toast(copy ? t("resources.page.copiee_dans_loom") : t("resources.page.skill_enregistree")); refreshWorkspace(); onClose();
  };
  return html`<${Modal} wide title=${!skill ? t("resources.page.nouvelle_skill") : ro ? skill.name : t("resources.page.modifier_la_skill")} sub=${skill && skill.dir ? html`<span class="mono">${home(skill.dir)}${t("resources.page.skill_md")}</span>${skill.files ? ' · ' + skill.files + t("resources.page.autre") + (skill.files > 1 ? 's' : '') + t("resources.page.fichier") + (skill.files > 1 ? 's' : '') : ''}` : t("resources.page.une_methode_reutilisable_enregistree_comme_dossier_dans_les_skill")} onClose=${onClose}
      foot=${ro ? html`<span class="muted grow">${t("resources.page.dossier_lie")}${skill.source_label}${t("resources.page.modifie_la_dans_ce_dossier")}</span><button class="btn ghost" onClick=${onClose}>${t("resources.page.fermer")}</button><button class="btn" onClick=${() => save(true)}>${t("resources.page.copier_dans_loom")}</button>`
        : html`<button class="btn ghost" onClick=${onClose}>${t("resources.page.annuler")}</button><button class="btn primary" disabled=${!name.trim() || !instr.trim()} onClick=${() => save(false)}>${t("resources.page.enregistrer")}</button>`}>
    <label class="field"><span>${t("resources.page.nom")}</span><input class="input" value=${name} readonly=${ro} onInput=${e => setName(e.target.value)} placeholder="${t("resources.page.ex_revue_de_code")}" maxlength="160" /></label>
    <label class="field"><span>${t("resources.page.quand_l_utiliser")}</span><input class="input" value=${desc} readonly=${ro} onInput=${e => setDesc(e.target.value)} placeholder="${t("resources.page.ex_relire_une_modification_avant_de_la_publier")}" maxlength="500" /></label>
    <label class="field"><span>${t("resources.page.instructions")}</span><textarea class="textarea" rows="12" value=${instr} readonly=${ro} onInput=${e => setInstr(e.target.value)} maxlength="8000"></textarea></label>
  </${Modal}>`;
}

// Où vivent les skills : un dossier du cerveau principal (détecté ou créé),
// ou le dossier de Loom quand il n'y a pas de cerveau.
function HomeDialog({ h, onClose }) {
  const detected = h.detected || [];
  const initial = h.mode === 'brain' ? h.relative : (detected[0] ? detected[0].relative : 'skills');
  const [choice, setChoice] = useState(h.mode === 'loom' && !h.brain_source ? 'loom' : initial);
  const [custom, setCustom] = useState(detected.some(d => d.relative === initial) || initial === 'skills' ? '' : initial);
  const [busy, setBusy] = useState(false);
  const save = async () => {
    setBusy(true);
    const rel = choice === 'custom' ? custom.trim() : choice;
    const r = await post('/api/skills/home', choice === 'loom' ? { mode: 'loom' } : { mode: 'brain', relative: rel }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false || r.error) return toast(r.error || t('resources.page.impossible'), 'err');
    toast((r.copied || []).length ? t('skills.home.copied', { n: r.copied.length }) : t('skills.home.saved'));
    if ((r.conflicts || []).length) toast(t('skills.home.conflicts', { names: r.conflicts.join(', ') }), 'err');
    refreshWorkspace(); onClose(true);
  };
  const opts = [...detected.map(d => ({ value: d.relative, label: d.relative + '/', note: t('skills.home.found', { n: d.count }) })),
    ...(detected.some(d => d.relative === 'skills') ? [] : [{ value: 'skills', label: 'skills/', note: t('skills.home.create') }]),
    { value: 'custom', label: t('skills.home.other') }, { value: 'loom', label: t('skills.home.loom'), note: t('skills.home.loom_note') }];
  return html`<${Modal} title=${t('skills.home.title')} sub=${h.brain_source ? t('skills.home.sub') : t('skills.home.no_brain')} onClose=${busy ? undefined : () => onClose()}
      foot=${html`<button class="btn ghost" disabled=${busy} onClick=${() => onClose()}>${t('ui.dialog.annuler')}</button><button class="btn primary" disabled=${busy || (choice === 'custom' && !custom.trim())} onClick=${save}>${t('ui.dialog.enregistrer')}</button>`}>
    <div class="sk-opts" role="radiogroup">${opts.filter(o => o.value === 'loom' || h.brain_source).map(o => html`<button type="button" role="radio" aria-checked=${String(choice === o.value)} class=${cls('sk-opt', choice === o.value && 'on')} onClick=${() => setChoice(o.value)}>
      <${Icon} n=${o.value === 'loom' ? 'box' : 'folder'} /><span class="grow"><b class=${o.value !== 'loom' && o.value !== 'custom' ? 'mono' : ''}>${o.label}</b>${o.note && html`<small>${o.note}</small>`}</span>${choice === o.value && html`<${Icon} n="check" />`}</button>`)}</div>
    ${choice === 'custom' && html`<label class="field"><span>${t('skills.home.relative')}</span><input class="input mono" value=${custom} placeholder="notes/skills" onInput=${e => setCustom(e.target.value)} /></label>`}
    ${choice !== 'loom' && h.mode === 'loom' && html`<p class="note">${t('skills.home.copy_note')}</p>`}
  </${Modal}>`;
}

function SkillsHome({ h, onChanged }) {
  const [dlg, setDlg] = useState(false);
  const inBrain = h.mode === 'brain' && !h.fallback;
  return html`<div class="card loc-strip">
      <span class="mono-tile"><${Icon} n=${inBrain ? 'brain' : 'box'} /></span>
      <div class="grow"><div class="bs-name">${inBrain ? t('skills.home.in_brain', { name: h.brain_label || h.brain_source }) : t('skills.home.in_loom')}</div>
        <div class="bs-sub"><span class="mono trunc" title=${h.dir}>${inBrain ? h.relative + '/' : home(h.dir)}</span><span>${t('skills.count', { n: h.count || 0 })}</span>${h.fallback && h.reason && html`<span class="bs-state err">${h.reason}</span>`}</div></div>
      <button class=${cls('btn sm', !inBrain && h.brain_source ? 'primary' : 'ghost')} onClick=${() => setDlg(true)}>${!inBrain && h.brain_source ? t('skills.home.move') : t('skills.home.change')}</button>
    ${dlg && html`<${HomeDialog} h=${h} onClose=${ok => { setDlg(false); if (ok) onChanged(); }} />`}
  </div>`;
}

// Agents : chaque famille lit ses skills dans un dossier à elle ; Loom y place
// un lien vers chaque skill (pas de copie). Les autres passent par la passerelle MCP.
function Distribution({ count }) {
  const [targets, setTargets] = useState(null);
  useEffect(() => { get('/api/skills/targets').then(r => setTargets(r.targets || [])).catch(() => setTargets([])); }, [count]);
  const toggle = async (localT, on) => {
    const r = await post('/api/skills/targets', { id: localT.id, enabled: on });
    if (!r.ok) return toast(r.error || t("resources.page.impossible"), 'err');
    setTargets(r.targets || []);
  };
  if (!targets || !targets.length) return null;
  return html`<section class="sec"><div class="sec-h"><h2>${t('skills.dist.title')}<${Tip} text=${t('skills.dist.tip')} /></h2></div>
    <div class="card bs-list">${targets.map(localT => html`<div class="bs-row" key=${localT.id}>
      <span class="dist-logos">${localT.harnesses.map(h => html`<${Logo} name=${h} size="sm" />`)}</span>
      <div class="grow"><div class="bs-name">${localT.name}</div><div class="bs-sub"><span class="mono trunc">${home(localT.dir)}</span>${localT.enabled && html`<span class="bs-state"><i class="dot green"></i>${t('skills.dist.linked', { n: localT.written.length })}</span>`}${localT.error && html`<span class="bs-state err" title=${localT.error}>${t("resources.page.attention")}</span>`}</div></div>
      <${Switch} checked=${localT.enabled} label=${t("resources.page.distribuer_a") + localT.name} onChange=${on => toggle(localT, on)} /></div>`)}</div></section>`;
}

function SkillCard({ s: sk, used, onOpen, onDelete }) {
  const [anchor, setAnchor] = useState(null);
  return html`<div class="mcard sk-card">
    <div class="mcard-h"><span class="mx-ico"><${Icon} n=${sk.read_only ? 'link' : 'sparkle'} /></span>
      <button class="grow sk-main" onClick=${onOpen}><b>${sk.name}</b><small>${sk.read_only ? sk.source_label : t('skills.card.library')}</small></button>
      <button class="icon-btn" aria-label=${t('brain.src.actions')} onClick=${e => setAnchor(anchor ? null : e.currentTarget)}><${Icon} n="more" /></button></div>
    <p class="mcard-d">${sk.description || t("resources.page.instructions_reutilisables")}</p>
    <div class="ws-card-f"><span class="muted">${used ? t('skills.used', { n: used }) : t('resources.page.aucun_projet')}</span></div>
    ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} items=${[{ icon: sk.read_only ? 'eye' : 'edit', label: sk.read_only ? t("resources.page.voir") : t("harnesses.machines.modifier"), run: onOpen }, ...(sk.read_only ? [] : ['-', { icon: 'trash', label: t("resources.page.supprimer"), danger: true, run: onDelete }])]} />`}
  </div>`;
}

// Dossiers liés en plus (lecture seule) et dossiers trouvés sur la machine.
function LinkedFolders({ onChange }) {
  const [data, setData] = useState(null);
  const [pick, setPick] = useState(false);
  const load = () => get('/api/skills/sources').then(setData).catch(() => setData(null));
  useEffect(() => { load(); }, []);
  const link = async (path, label) => {
    const r = await post('/api/skills/sources', { path, label: label || '' });
    if (!r.ok) return toast(r.error || t("resources.page.impossible"), 'err');
    toast(t("resources.page.dossier_lie_2")); load(); refreshWorkspace(); onChange();
  };
  const unlink = async s => {
    if (!await confirm(t("resources.page.ne_plus_lier_ce_dossier"), t("resources.page.ses_skills_disparaissent_de_loom_et_des_harnesses_ou_loom_les_ava"), { ok: t("resources.page.delier") })) return;
    await post('/api/skills/sources', { unlink: s.id }); load(); refreshWorkspace(); onChange();
  };
  if (!data) return null;
  const linked = (data.sources || []).filter(s => !s.builtin), sugg = data.suggested || [];
  return html`<section class="sec"><div class="sec-h"><h2>${t('skills.linked.title')}<${Tip} text=${t('skills.linked.tip')} /></h2><span class="grow"></span>
      <button class="btn sm" onClick=${() => setPick(true)}><${Icon} n="link" />${t("resources.page.lier_un_dossier")}</button></div>
    ${linked.length || sugg.length ? html`<div class="card bs-list">${linked.map(s => html`<div class="bs-row" key=${s.id}>
        <span class="mono-tile"><${Icon} n="link" /></span>
        <div class="grow"><div class="bs-name">${s.label}</div><div class="bs-sub"><span class="mono trunc">${home(s.path)}</span>${s.error ? html`<span class="bs-state err">${s.error}</span>` : html`<span>${t('skills.count', { n: s.count })} · ${t('second_brain.read')}</span>`}</div></div>
        <button class="btn sm ghost" onClick=${() => unlink(s)}>${t("resources.page.delier")}</button></div>`)}
      ${sugg.map(g => html`<div class="bs-row" key=${g.path}>
        <span class="mono-tile"><${Icon} n="folder" /></span>
        <div class="grow"><div class="bs-name">${g.label}<span class="tag">${t('skills.linked.found')}</span></div><div class="bs-sub"><span class="mono trunc">${home(g.path)}</span></div></div>
        <button class="btn sm" onClick=${() => link(g.path, g.label)}>${t("resources.page.lier")}</button></div>`)}</div>`
      : html`<div class="card bs-none"><div class="grow"><p>${t('skills.linked.empty')}</p></div></div>`}
    ${pick && html`<${FolderPicker} start="" onClose=${() => setPick(false)} onPick=${d => { setPick(false); link(d); }} />`}
  </section>`;
}

export function Skills() {
  const ws = useStore(app, s => s.workspace);
  const [dlg, setDlg] = useState(null);
  const [h, setH] = useState(null);
  const [q, setQ] = useState('');
  const loadHome = () => Promise.all([get('/api/skills/home'), get('/api/brain/sources').catch(() => ({}))]).then(([r, b]) => {
    const src = ((b && b.sources) || []).find(s => s.id === r.brain_source);
    setH(r.ok === false ? null : { ...r, brain_label: src ? src.label : r.brain_source });
  }).catch(() => setH(null));
  useEffect(() => { loadHome(); }, []);
  const skills = (ws && ws.capabilities) || [], projects = (ws && ws.projects) || [];
  const del = async s => { if (!await confirm(t("resources.page.supprimer_la_skill"), t("resources.page.son_dossier") + home(s.dir) + t("resources.page.est_supprime") + s.name + t("resources.page.ne_sera_plus_ajoutee_aux_projets_ni_aux_harnesses"), { ok: t("resources.page.supprimer"), danger: true })) return; const r = await post('/api/capabilities/delete', { id: s.id }); if (!r.ok) toast(r.error, 'err'); refreshWorkspace(); loadHome(); };
  const needle = q.trim().toLowerCase();
  const shown = needle ? skills.filter(s => (s.name + ' ' + (s.description || '')).toLowerCase().includes(needle)) : skills;
  return html`<div class="bs">
    ${h ? html`<${SkillsHome} h=${h} onChanged=${() => { loadHome(); refreshWorkspace(); }} />` : html`<div class="skeleton" style="height:56px"></div>`}
    <section class="sec"><div class="sec-h"><h2>${t('skills.list')}${skills.length > 0 && html` <span class="count">${skills.length}</span>`}<${Tip} text=${t('skills.home.tip')} /></h2><span class="grow"></span>
        ${skills.length > 6 && html`<label class="search sk-q"><${Icon} n="search" /><input placeholder=${t('skills.search')} value=${q} onInput=${e => setQ(e.target.value)} /></label>`}</div>
      <div class="mcards">${shown.map(s => html`<${SkillCard} key=${s.id} s=${s} used=${projects.filter(p => (p.capability_ids || []).includes(s.id)).length} onOpen=${() => setDlg({ skill: s })} onDelete=${() => del(s)} />`)}
        <button type="button" class="mcard add" onClick=${() => setDlg({})}><${Icon} n="plus" /><span>${t("resources.page.nouvelle_skill")}</span><small>${t('skills.card.add_note')}</small></button></div>
    </section>
    <${Distribution} count=${skills.length} />
    <${LinkedFolders} onChange=${loadHome} />
    ${dlg && html`<${SkillEditor} skill=${dlg.skill} onClose=${() => { setDlg(null); loadHome(); }} />`}
  </div>`;
}
