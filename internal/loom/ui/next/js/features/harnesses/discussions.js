import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tip, Seg } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshNav } from '../../core/state.js';
import { open as openChat } from '../chat/engine.js';

const ago = ms => { const m = Math.round((Date.now() - ms) / 60000); return m < 1 ? t("harnesses.page.a_l_instant") : m < 60 ? t('common.relative.minutes', { n: m }) : m < 1440 ? t('common.relative.hours', { n: Math.round(m / 60) }) : t('common.relative.days', { n: Math.round(m / 1440) }); };

// Sessions déjà ouvertes dans le harness (hors Loom) : les importer pour les
// continuer ici, avec la mémoire native du harness.
// Sessions natives d'un agent, sur cette machine ou sur une autre machine où
// il est géré. Une session d'une autre machine est reprise ici par l'agent
// utilisé (target), avec sa transcription.
function NativeSessions({ rt, embedded, onCount, source = 'local', target = null }) {
  const [list, setList] = useState(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState('');
  const [q, setQ] = useState('');
  const [project, setProject] = useState('');
  const [fresh, setFresh] = useState(true);
  const projects = useStore(app, s => s.workspace?.projects || []);
  const load = async () => {
    setErr(''); setList(null);
    const r = await get(source === 'local' ? '/api/runtimes/' + rt.id + '/sessions' : '/api/harness-history?source=' + encodeURIComponent(source), { timeout: 95000 }).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) { setErr(r.error || t("harnesses.page.lecture_impossible")); setList([]); return; }
    // Sessions Loom itself opened in the agent carry Loom's handoff text: they
    // are already discussions here.
    const own = x => /^Loom portable discussion/.test(x.title || '');
    const sessions = (r.sessions || []).filter(x => !own(x)).sort((a, b) => String(b.updatedAt || '').localeCompare(String(a.updatedAt || '')));
    setList(sessions); onCount && onCount(sessions.filter(x => !x.imported).length);
  };
  useEffect(() => { if (source !== 'local' || rt.available !== false) load(); }, [rt.id, source]);
  const importOne = async x => {
    if (x.imported) { openChat(x.imported); go('chat'); return; }
    if (source !== 'local') {
      if (!target) return toast(t('agents.import.no_target'), 'err');
      if (!await confirm(t('agents.import.remote_title'), t('agents.import.remote_note', { name: rt.name, target: target.name }), { ok: t("harnesses.page.importer") })) return;
      setBusy(x.sessionId);
      const r = await post('/api/harness-history/transfer', { source, choice_id: target.id, project_id: project, consent: true, ...x }, { timeout: 190000 }).catch(e => ({ ok: false, error: e.message }));
      setBusy('');
      if (!r.ok) return toast(r.error || t("harnesses.page.import_impossible"), 'err');
      await refreshNav(); openChat(r.session.id); go('chat'); return;
    }
    setBusy(x.sessionId);
    const r = await post('/api/runtimes/' + rt.id + '/sessions/import', { sessionId: x.sessionId, cwd: x.cwd, title: x.title || '', project_id: project, fresh }, { timeout: 190000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t("harnesses.page.import_impossible"), 'err');
    await refreshNav(); openChat(r.session.id); go('chat');
  };
  const shown = (list || []).filter(x => !q || ((x.title || '') + ' ' + x.cwd).toLowerCase().includes(q.toLowerCase()));
  const rows = list === null ? html`<div class="card pad"><div class="state"><span class="spinner"></span>${t("harnesses.page.lecture_des_sessions_de")} ${rt.name}…</div></div>`
      : err ? html`<div class="card pad"><p class="note err">${err}</p></div>`
      : !shown.length ? html`<div class="card pad"><p class="note">${q ? t("harnesses.page.aucune_session_ne_correspond") : t('agents.native_empty', { name: rt.name })}</p></div>`
      : html`<div class="card rows">${shown.slice(0, 30).map(x => html`<div class="row" key=${x.sessionId}>
          <div class="grow"><div class="t">${x.title || t("harnesses.page.session_sans_titre")}</div>
            <div class="s">${[x.cwd.replace(/^\/home\/[^/]+/, '~'), x.updatedAt ? ago(Date.parse(x.updatedAt)) : ''].filter(Boolean).join(' · ')}</div></div>
          <button class=${cls('btn sm', x.imported && 'ghost')} disabled=${!!busy} onClick=${() => importOne(x)}>${busy === x.sessionId ? html`<span class="spinner"></span>${t("harnesses.page.import")}` : x.imported ? t("harnesses.page.ouvrir") : t("harnesses.page.importer")}</button></div>`)}</div>`;
  if (embedded) return html`<div class="import-bar">
      <label class="field inline"><span>${t('harnesses.import.project')}</span><select class="select sm" value=${project} onChange=${e => setProject(e.target.value)}><option value="">—</option>${projects.map(p => html`<option value=${p.id}>${p.name}</option>`)}</select></label>
      ${source === 'local' && html`<label class="check"><input type="checkbox" checked=${fresh} onChange=${e => setFresh(e.target.checked)} /><span>${t('agents.import_fresh')}</span><${Tip} text=${t('harnesses.import.fresh') + ' ' + t('harnesses.import.note')} /></label>`}
      <span class="grow"></span>${list && list.length > 6 && html`<label class="search" style="width:200px"><${Icon} n="search" /><input placeholder="${t("harnesses.page.filtrer")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>`}
      <button class="btn sm ghost" onClick=${load}><${Icon} n="refresh" />${t("harnesses.page.actualiser")}</button></div>${rows}`;
  return html`<section class="sec"><div class="sec-h"><h2>${t("harnesses.page.sessions_de")} ${rt.name} <span class="count">${list ? list.length : ''}</span><${Tip} text=${t("harnesses.page.les_discussions_que_tu_as_eues_directement_dans") + rt.name + t("harnesses.page.les_importer_les_ajoute_a_loom_tu_les_continues_ici_avec_la_memoi")} /></h2>
      <span style="display:flex;gap:8px">${list && list.length > 6 && html`<label class="search" style="width:220px"><${Icon} n="search" /><input placeholder="${t("harnesses.page.filtrer")}" aria-label="${t("harnesses.page.filtrer_les_sessions")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>`}
      <button class="btn sm ghost" onClick=${load}><${Icon} n="refresh" />${t("harnesses.page.actualiser")}</button></span></div>
    <label class="field"><span>${t('harnesses.import.project')}</span><select class="select" value=${project} onChange=${e => setProject(e.target.value)}><option value="">—</option>${projects.map(p => html`<option value=${p.id}>${p.name}</option>`)}</select></label>
    <label class="check"><input type="checkbox" checked=${fresh} onChange=${e => setFresh(e.target.checked)} /><span>${t('harnesses.import.fresh')}</span></label><p class="note">${t('harnesses.import.note')}</p>
    ${list === null ? html`<div class="card pad"><div class="state"><span class="spinner"></span>${t("harnesses.page.lecture_des_sessions_de")} ${rt.name}…</div></div>`
      : err ? html`<div class="card pad"><p class="note err">${err}</p></div>`
      : !shown.length ? html`<div class="card pad"><p class="note">${q ? t("harnesses.page.aucune_session_ne_correspond") : t("harnesses.page.aucune_session_trouvee")}</p></div>`
      : html`<div class="card rows">${shown.slice(0, 30).map(x => html`<div class="row" key=${x.sessionId}>
          <div class="grow"><div class="t">${x.title || t("harnesses.page.session_sans_titre")}</div>
            <div class="s">${[x.cwd.replace(/^\/home\/[^/]+/, '~'), x.updatedAt ? ago(Date.parse(x.updatedAt)) : ''].filter(Boolean).join(' · ')}</div></div>
          ${x.imported && html`<span class="tag">${t("harnesses.page.dans_loom")}</span>`}
          <button class="btn sm" disabled=${!!busy} onClick=${() => importOne(x)}>${busy === x.sessionId ? html`<span class="spinner"></span>${t("harnesses.page.import")}` : x.imported ? t("harnesses.page.ouvrir") : t("harnesses.page.importer")}</button></div>`)}</div>`}
  </section>`;
}
// Discussions : celles de Loom, et celles faites directement dans l'agent
// (hors sessions que Loom a lui-même ouvertes), importables ici.
export function AgentDiscussions({ rt, talks, canList, installs, target }) {
  const [tab, setTab] = useState('loom');
  const [native, setNative] = useState(null);
  // Où lire les sessions : chaque machine où l'agent est géré et prêt.
  const sources = installs.filter(i => i.managed && i.installed && (i.machine !== 'local' ? i.ready && canList : canList))
    .map(i => ({ value: i.machine === 'local' ? 'local' : 'remote:' + i.machine + ':' + i.harness, label: i.machine === 'local' ? t('agents.this_machine') : i.machine_name }));
  const [source, setSource] = useState(null);
  const from = source || (sources[0] && sources[0].value) || 'local';
  return html`<section class="sec"><div class="sec-h"><h2>${t('agents.discussions')}</h2>
      ${sources.length > 0 && html`<${Seg} size="sm" value=${tab} onChange=${setTab} label=${t('agents.discussions')} options=${[{ value: 'loom', label: t('agents.tab.loom'), count: talks.length }, { value: 'native', label: t('agents.tab.native', { name: rt.name }), count: native == null ? '' : native }]} />`}</div>
    ${tab === 'native' && sources.length ? html`${sources.length > 1 && html`<div class="src-pick"><span>${t('agents.import.from')}</span><${Seg} size="sm" value=${from} onChange=${v => { setSource(v); setNative(null); }} label=${t('agents.import.from')} options=${sources} /></div>`}
        <${NativeSessions} key=${from} rt=${rt} embedded source=${from} target=${target} onCount=${setNative} />`
      : talks.length ? html`<div class="card rows">${talks.slice(0, 8).map(c => html`<button type="button" class="row link-row" key=${c.id} onClick=${() => { openChat(c.id); go('chat'); }}>
          <div class="grow"><div class="t">${c.title || t("harnesses.page.discussion")}</div><div class="s">${[c.model && c.model !== 'default' ? c.model : '', c.workdir ? c.workdir.split('/').pop() : '', c.updated_at ? ago(c.updated_at) : ''].filter(Boolean).join(' · ')}</div></div>
          <${Icon} n="right" /></button>`)}</div>`
      : html`<div class="card pad"><p class="note">${t('agents.no_talks', { name: rt.name })}</p></div>`}
  </section>`;
}
