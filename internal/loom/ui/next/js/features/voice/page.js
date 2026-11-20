// Voix & Jarvis : une seule page en deux colonnes (moteur vocal et réglages à
// gauche, profil Jarvis à droite). Contrat : docs/voice.md.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Seg, Tip } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { Modal, toast, confirm } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go } from '../../core/state.js';
import { SectionTabs } from '../../app/sections.js';
import { Line, Group } from '../settings/kit.js';

const fmtSize = n => !n ? '0' : n >= 1e9 ? t('voice.gb', { n: (n / 1e9).toFixed(1) }) : t('voice.mb', { n: (n / 1e6).toFixed(0) });
const KIND = () => ({ stt: t('voice.kind.stt'), tts: t('voice.kind.tts'), vad: t('voice.kind.vad'), kws: t('voice.kind.kws') });
const PACK = () => ({ 'fr-cpu-small': t('voice.pack.fr-cpu-small'), 'en-cpu-small': t('voice.pack.en-cpu-small'), 'multi-cpu': t('voice.pack.multi-cpu'), 'fr-gpu': t('voice.pack.fr-gpu') });
const PACK_WHY = () => ({ 'fr-cpu-small': t('voice.why.fr-cpu-small'), 'en-cpu-small': t('voice.why.en-cpu-small'), 'multi-cpu': t('voice.why.multi-cpu'), 'fr-gpu': t('voice.why.fr-gpu') });
const PHASE = () => ({ queued: t('voice.phase.queued'), downloading: t('voice.phase.downloading'), extracting: t('voice.phase.extracting'), complete: t('voice.phase.complete'), failed: t('voice.phase.failed'), cancelled: t('voice.phase.cancelled') });
const LANGS = () => ({ fr: t('jv.lang_fr'), en: t('jv.lang_en'), es: t('jv.lang_es'), de: t('jv.lang_de'), it: t('jv.lang_it'), pt: t('jv.lang_pt'), nl: t('jv.lang_nl') });
const PERSONA_MAX = 1000;

function PacksModal({ packs, installed, busy, dl, onInstall, onClose }) {
  return html`<${Modal} wide title=${t('voice.packs')} sub=${t('voice.packs_tip')} onClose=${onClose}
      foot=${html`<button class="btn primary" onClick=${onClose}>${t('ui.dialog.fermer')}</button>`}>
    <div class="vx-pack-list">
      ${packs.map(p => {
        const pk = p.pack || {};
        const have = [pk.stt, pk.tts, pk.vad].filter(Boolean).every(id => installed.some(m => m.id === id));
        return html`<div class="vx-pack" key=${pk.id}>
          <div class="vx-pack-h">
            <div><b>${PACK()[pk.id] || pk.id}</b> <span class="muted">${[pk.language, pk.hardware, fmtSize(p.size)].filter(Boolean).join(' · ')}</span></div>
            ${have ? html`<span class="state"><i class="dot green"></i>${t('voice.pack_installed')}</span>`
              : html`<button class="btn sm primary" disabled=${!!busy || dl.running || !p.installable} title=${p.requires_nvidia ? t('voice.needs_gpu') : ''} onClick=${() => onInstall(p)}>${t('voice.install')}</button>`}
          </div>
          <p>${PACK_WHY()[pk.id] || pk.reason}</p>
          <div class="vx-contents">${[pk.stt, pk.tts, pk.vad].filter(Boolean).map(id => html`<span class=${cls('tag', installed.some(m => m.id === id) && 'green')}>${id}</span>`)}</div>
        </div>`;
      })}
    </div>
  </${Modal}>`;
}

function JarvisColumn({ jarvisRef, tts }) {
  const [p, setP] = useState(null);
  const [name, setName] = useState('');
  const [persona, setPersona] = useState('');
  const fill = r => { setP(r); setName(r.name || ''); setPersona(r.personality || ''); };
  useEffect(() => {
    get('/api/voice/jarvis').then(r => r.ok === false ? setP({ error: r.error }) : fill(r)).catch(e => setP({ error: e.message }));
  }, []);
  const save = async patch => {
    const r = await post('/api/voice/jarvis', patch).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error || t('jv.failed'), 'err');
    fill(r); toast(t('jv.saved'));
  };
  const talk = () => { go('chat'); app.set({ voiceMode: { discussion: '' } }); };
  if (!p) return html`<div class="vx-col" ref=${jarvisRef}><div class="skeleton" style="height:420px"></div></div>`;
  if (p.error) return html`<div class="vx-col" ref=${jarvisRef}><section class="set-group"><div class="vx-sec-h"><h2>${t('jv.title')}</h2></div><div class="card pad"><p class="note err">${p.error}</p></div></section></div>`;
  const routes = (p.routes || []).filter(r => r.enabled !== false);
  const routeOpts = routes.map(r => ({ value: r.id, label: r.name, note: (r.kind === 'local' ? t('jv.local') : r.provider_name || t('jv.cloud')) + (r.ready ? '' : ' · ' + t('jv.not_ready')) }));
  const v = p.voice || {};
  const fbLabel = p.fallback ? (routes.find(r => r.id === p.fallback)?.name || p.fallback) : '';
  return html`<div class="vx-col" ref=${jarvisRef} tabindex="-1">
    <section class="set-group anim-rise" aria-label=${t('jv.title')}>
      <div class="vx-sec-h">
        <h2>${t('jv.title')}</h2>
        <button type="button" class="btn primary sm" onClick=${talk}><${Icon} n="mic" />${t('jv.talk', { name: p.name || t('jv.title') })}</button>
      </div>
      <div class="card">
        <${Line} label=${t('jv.name')} sub=${t('voice.name_sub')} tip=${t('jv.name_tip')}>
          <input class="input sm vx-name" maxlength="80" aria-label=${t('jv.name')} value=${name} onInput=${e => setName(e.target.value)} onBlur=${() => name.trim() && name.trim() !== p.name && save({ name: name.trim() })} onKeyDown=${e => e.key === 'Enter' && e.currentTarget.blur()} />
        </${Line}>
        <${Line} label=${t('jv.language')} tip=${t('jv.language_tip')}>
          <div class="set-pick"><${ListPick} label=${t('jv.language')} value=${p.language} onChange=${x => save({ language: x })} options=${[{ value: 'auto', label: t('jv.lang_auto') }, ...Object.entries(LANGS()).map(([value, label]) => ({ value, label }))]} /></div>
        </${Line}>
        <${Line} label=${t('jv.formality')} tip=${t('jv.formality_tip')}>
          <${Seg} size="sm" label=${t('jv.formality')} value=${p.formality} onChange=${x => save({ formality: x })} options=${[{ value: 'tu', label: t('jv.tu') }, { value: 'vous', label: t('jv.vous') }]} />
        </${Line}>
        <${Line} label=${t('jv.length')} tip=${t('jv.length_tip')}>
          <${Seg} size="sm" label=${t('jv.length')} value=${p.length} onChange=${x => save({ length: x })} options=${[{ value: 'short', label: t('jv.short') }, { value: 'normal', label: t('jv.normal') }, { value: 'detailed', label: t('jv.detailed') }]} />
        </${Line}>
        <${Line} label=${t('jv.personality')} tip=${t('jv.personality_note')} stack>
          <textarea class="textarea" rows="2" maxlength=${PERSONA_MAX} placeholder=${t('jv.personality_ph')} value=${persona} onInput=${e => setPersona(e.target.value)}></textarea>
          <div class="set-actions" style="padding:4px 0 0;border-top:0">
            <small class="muted">${persona.length} / ${PERSONA_MAX}</small>
            <span class="grow"></span>
            <button type="button" class="btn sm" disabled=${persona === (p.personality || '')} onClick=${() => save({ personality: persona })}>${t('jv.save')}</button>
          </div>
        </${Line}>
        <${Line} label=${t('jv.model')} sub=${fbLabel ? t('jv.fallback') + ' : ' + fbLabel : ''} tip=${t('jv.model_tip')}>
          <div class="set-pick"><${ListPick} label=${t('jv.model')} value=${p.model} onChange=${x => save({ model: x })} options=${[{ value: 'discussion', label: t('jv.model_discussion') }, ...routeOpts]} /></div>
        </${Line}>
        <${Line} label=${t('jv.fallback')} tip=${t('jv.fallback_tip')}>
          <div class="set-pick"><${ListPick} label=${t('jv.fallback')} value=${p.fallback} onChange=${x => save({ fallback: x })} options=${[{ value: '', label: t('jv.fallback_auto') }, ...routeOpts]} /></div>
        </${Line}>
        <${Line} label=${t('jv.context')} tip=${t('jv.context_tip')}>
          <div class="set-pick"><${ListPick} label=${t('jv.context')} value=${p.context} onChange=${x => save({ context: x })} options=${[{ value: 'discussion+memory', label: t('jv.ctx_all') }, { value: 'memory', label: t('jv.ctx_memory') }, { value: 'none', label: t('jv.ctx_none') }]} /></div>
        </${Line}>
        <${Line} label=${t('jv.voice')} tip=${t('jv.tts_tip')}>
          <div class="set-pick"><${ListPick} label=${t('jv.tts')} value=${v.tts_model || ''} onChange=${x => save({ voice: { ...v, tts_model: x } })} options=${[{ value: '', label: t('jv.inherit') }, ...tts.map(m => ({ value: m.id, label: m.id, note: (m.languages || []).join(', ') }))]} /></div>
        </${Line}>
        <${Line} label=${t('voice.jarvis_voice_override')} tip=${t('jv.voice_id_tip')}>
          <div class="vx-inline">
            <input class="input sm num vx-num" type="number" min="0" max="1024" step="1" aria-label=${t('jv.voice_id')} placeholder=${t('jv.inherit_short')} value=${v.voice_id ?? ''} onChange=${e => save({ voice: { ...v, voice_id: e.target.value === '' ? null : Number(e.target.value) } })} />
            <input class="input sm num vx-num" type="number" min="0.5" max="2" step="0.05" aria-label=${t('jv.speed')} placeholder=${t('jv.inherit_short')} value=${v.speed ?? ''} onChange=${e => save({ voice: { ...v, speed: e.target.value === '' ? null : Number(e.target.value) } })} />
          </div>
        </${Line}>
      </div>
    </section>
  </div>`;
}

export function VoicePage({ route, focus }) {
  const [machine, setMachine] = useState('local'), [machines, setMachines] = useState([]);
  const [v, setV] = useState(null), [packs, setPacks] = useState([]), [models, setModels] = useState([]);
  const [busy, setBusy] = useState(''), [kind, setKind] = useState('');
  const [showPacks, setShowPacks] = useState(false);
  const [sample, setSample] = useState(t('voice.sample'));
  const [testBusy, setTestBusy] = useState(false), [bench, setBench] = useState(null);
  const audio = useRef(null), jarvisRef = useRef(null);

  const q = (path, extra = '') => path + (path.includes('?') ? '&' : '?') + 'machine=' + encodeURIComponent(machine) + extra;
  const load = () => {
    get(q('/api/voice')).then(r => setV(r.ok === false ? { error: r.error } : r)).catch(e => setV({ error: e.message }));
    get(q('/api/voice/packs')).then(r => setPacks(r.packs || [])).catch(() => setPacks([]));
    get(q('/api/voice/models')).then(r => setModels(r.models || [])).catch(() => setModels([]));
  };
  useEffect(() => { get('/api/voice/node').then(r => { if (r.ok !== false) { setMachines(r.machines || []); setMachine(r.machine || 'local'); } }).catch(() => {}); }, []);
  useEffect(() => { setV(null); load(); }, [machine]);
  useEffect(() => {
    if ((focus === 'jarvis' || route?.section === 'jarvis') && jarvisRef.current) {
      jarvisRef.current.scrollIntoView?.({ behavior: 'smooth', block: 'start' });
    }
  }, [focus, route?.section, !!v]);

  // Suivi d'un téléchargement en cours.
  useEffect(() => {
    if (!v || !v.download || !v.download.running) return;
    const id = setInterval(() => get(q('/api/voice/download/status')).then(r => {
      if (!r.download) return;
      setV(old => ({ ...old, download: r.download }));
      if (!r.download.running) { load(); if (r.download.error) toast(r.download.error, 'err'); }
    }).catch(() => {}), 1500);
    return () => clearInterval(id);
  }, [v && v.download && v.download.running, machine]);

  const act = async (key, path, body, ok) => {
    setBusy(key);
    const r = await post(q(path), body || {}, { timeout: 60000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (r.ok === false) { toast(r.error || t('voice.failed'), 'err'); return null; }
    if (ok) toast(ok);
    load(); return r;
  };
  const chooseMachine = async id => { setMachine(id); await post('/api/voice/node', { machine: id }).catch(() => {}); };

  const speak = async () => {
    setTestBusy(true);
    try {
      const res = await fetch(q('/api/voice/test/tts'), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: sample }), credentials: 'same-origin' });
      if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || t('voice.failed'));
      const url = URL.createObjectURL(await res.blob());
      if (audio.current) { audio.current.pause(); URL.revokeObjectURL(audio.current.src); }
      audio.current = new Audio(url); await audio.current.play();
    } catch (e) { toast(e.message, 'err'); }
    setTestBusy(false);
  };
  const runBench = async () => {
    setTestBusy(true);
    const r = await post(q('/api/voice/bench'), {}, { timeout: 120000 }).catch(e => ({ ok: false, error: e.message }));
    setTestBusy(false);
    if (r.ok === false) return toast(r.error || t('voice.failed'), 'err');
    setBench(r);
  };

  if (!v) return html`<div class="view page"><div class="page-in vx-page"><${SectionTabs} /><div class="skeleton" style="height:280px"></div></div></div>`;
  const e = v.engine || {}, cfg = v.config || {}, svc = v.service || {}, dl = v.download || {};
  const installed = models.filter(m => m.installed);
  const ttsInstalled = installed.filter(m => m.kind === 'tts');
  const pick = k => installed.filter(m => m.kind === k).map(m => ({ value: m.id, label: m.id, note: (m.languages || []).join(', ') + ' · ' + fmtSize(m.disk_bytes || m.size) }));
  const saveCfg = patch => act('cfg', '/api/voice', { ...cfg, ...patch });
  const ready = e.installed && cfg.stt && cfg.tts && cfg.vad;
  const machineLabel = (machines.find(m => m.id === machine)?.name) || (machine === 'local' ? t('history.local') : machine);
  const installedPack = packs.find(p => {
    const pk = p.pack || {};
    return [pk.stt, pk.tts, pk.vad].filter(Boolean).every(id => installed.some(m => m.id === id));
  });
  const idleOpts = [5, 15, 30, 60, 120, 0];
  if (cfg.idle_unload_minutes != null && !idleOpts.includes(Number(cfg.idle_unload_minutes))) idleOpts.unshift(Number(cfg.idle_unload_minutes));
  const benchSub = bench
    ? t('voice.bench_summary', { s: (bench.tts.first_audio_ms / 1000).toFixed(1), x: (1 / bench.tts.real_time_factor).toFixed(1) })
      + (bench.stt ? ` · ${t('voice.kind.stt')} ${Math.round(bench.stt.latency_ms)} ms` : '')
    : '';

  return html`<div class="view page"><div class="page-in vx-page">
    <${SectionTabs} />
    <div class="vx-cols">
      <div class="vx-col">
        <${Group} title=${t('voice.engine')}>
          <div class="set-line">
            <div class="set-l">
              <i class=${'mc-dot ' + (svc.running ? 'green' : e.installed ? '' : '')}></i>
              <span class="set-label">
                <span>${svc.running ? t('voice.running_on', { machine: machineLabel }) : e.installed ? t('voice.stopped') : t('settings.page.non_installe')}</span>
                <small>${e.installed ? `sherpa-onnx ${e.version || ''} · ${(e.selection?.provider || e.provider || 'cpu').toUpperCase()}` : t('voice.engine_sub')}</small>
              </span>
            </div>
            <div class="set-c">
              ${!e.installed ? html`<button type="button" class="btn sm primary" disabled=${!!busy || dl.running || !e.cpu_installable} onClick=${() => act('install', '/api/voice/install', { cuda: false })}>${t('voice.install')}</button>
                  ${e.nvidia && html`<button type="button" class="btn sm" disabled=${!!busy || dl.running} onClick=${() => act('install', '/api/voice/install', { cuda: true })}>${t('voice.install_gpu')}</button>`}`
                : svc.running ? html`<button type="button" class="btn sm" disabled=${!!busy} onClick=${() => act('svc', '/api/voice/service', { action: 'restart' })}><${Icon} n="refresh" />${t('local.page.redemarrer')}</button>
                  <button type="button" class="btn sm" disabled=${!!busy} onClick=${() => act('svc', '/api/voice/service', { action: 'stop' })}>${t('local.page.arreter')}</button>`
                : html`<button type="button" class="btn sm primary" disabled=${!!busy || !ready} title=${ready ? '' : t('voice.choose_models')} onClick=${() => act('svc', '/api/voice/service', { action: 'start' })}><${Icon} n="play" />${t('local.page.demarrer')}</button>`}
            </div>
          </div>
          ${v.error && html`<div class="pad"><p class="note err">${v.error}</p></div>`}
          ${svc.error && html`<div class="pad"><p class="note err">${svc.error}</p></div>`}
          ${v.runtime_requires && html`<div class="pad"><p class="note">${t('voice.requires', { what: v.runtime_requires })}</p></div>`}
          ${dl.running && html`<${Line} label=${PHASE()[dl.phase] || dl.phase}><span class="mono muted">${dl.artifact} ${dl.total ? Math.round(dl.received * 100 / dl.total) + ' %' : ''}</span><button type="button" class="btn sm ghost" onClick=${() => act('cancel', '/api/voice/download/cancel', {})}>${t('ui.dialog.annuler')}</button></${Line}>`}
          <${Line} label=${t('voice.where')} tip=${t('voice.machine_tip')}>
            <div class="set-pick"><${ListPick} label=${t('voice.machine')} value=${machine} onChange=${chooseMachine} options=${(machines.length ? machines : [{ id: 'local', name: 'Local' }]).map(m => ({ value: m.id, label: m.id === 'local' ? t('settings.machines.cette_machine_2') : m.name }))} /></div>
          </${Line}>
          ${e.installed && packs.length > 0 && html`<${Line} label=${t('voice.installed_pack')} sub=${installedPack ? (PACK()[installedPack.pack?.id] || installedPack.pack?.id) : t('voice.pack_none')}>
            <button type="button" class="btn sm ghost" onClick=${() => setShowPacks(true)}>${t('voice.pack_change')}</button>
          </${Line}>`}
        </${Group}>

        ${e.installed && html`<${Group} title=${t('voice.settings_title')}>
          <${Line} label=${t('voice.kind.stt')}><div class="set-pick"><${ListPick} label=${t('voice.kind.stt')} disabled=${svc.running} value=${cfg.stt} onChange=${x => saveCfg({ stt: x })} options=${pick('stt')} /></div></${Line}>
          <${Line} label=${t('voice.kind.tts')}>
            <div class="vx-inline">
              <div class="set-pick"><${ListPick} label=${t('voice.kind.tts')} disabled=${svc.running} value=${cfg.tts} onChange=${x => saveCfg({ tts: x })} options=${pick('tts')} /></div>
              <input class="input sm num vx-num" type="number" min="0" step="1" aria-label=${t('voice.voice_id')} title=${t('voice.voice_id_tip')} disabled=${svc.running} value=${cfg.voice} onChange=${ev => saveCfg({ voice: Number(ev.target.value) })} />
              ${ready && html`<button type="button" class="btn sm" disabled=${testBusy || !sample.trim()} onClick=${speak}>${t('voice.speak')}</button>`}
            </div>
          </${Line}>
          <${Line} label=${t('voice.kind.vad')}><div class="set-pick"><${ListPick} label=${t('voice.kind.vad')} disabled=${svc.running} value=${cfg.vad} onChange=${x => saveCfg({ vad: x })} options=${pick('vad')} /></div></${Line}>
          <${Line} label=${t('voice.speed_silence')} tip=${t('voice.speed_tip')}>
            <div class="vx-inline">
              <input class="input sm num vx-num" type="number" min="0.5" max="2" step="0.05" aria-label=${t('voice.speed')} disabled=${svc.running} value=${cfg.speed} onChange=${ev => saveCfg({ speed: Number(ev.target.value) })} />
              <input class="input sm num vx-num" type="number" min="0.2" max="3" step="0.1" aria-label=${t('voice.silence')} title=${t('voice.silence_tip')} disabled=${svc.running} value=${cfg.endpoint_silence} onChange=${ev => saveCfg({ endpoint_silence: Number(ev.target.value) })} />
            </div>
          </${Line}>
          <${Line} label=${t('voice.idle')} tip=${t('voice.idle_tip')}>
            <div class="set-pick"><${ListPick} label=${t('voice.idle')} disabled=${svc.running} value=${String(cfg.idle_unload_minutes)} onChange=${x => saveCfg({ idle_unload_minutes: Number(x) })} options=${idleOpts.map(m => ({ value: String(m), label: m === 0 ? t('voice.never') : t('voice.minutes', { n: m }) }))} /></div>
          </${Line}>
          <${Line} label=${t('voice.boot')} tip=${t('voice.boot_tip')}><${Switch} label=${t('voice.boot')} disabled=${svc.running || !ready} checked=${cfg.boot} onChange=${on => saveCfg({ boot: on })} /></${Line}>
          ${ready && html`<${Line} label=${t('voice.measure_title')} sub=${benchSub}>
            <button type="button" class="btn sm" disabled=${testBusy} onClick=${runBench}>${t('voice.bench')}</button>
          </${Line}>`}
          ${ready && bench?.stt?.text != null && html`<${Line} label=${t('voice.heard')} sub=${`« ${bench.stt.text} » — ${t('voice.expected')} « ${bench.sample_text} »`} />`}
          ${ready && html`<${Line} label=${t('voice.sample_label')} stack>
            <input class="input sm" value=${sample} aria-label=${t('voice.sample_label')} onInput=${ev => setSample(ev.target.value)} />
          </${Line}>`}
          ${svc.running && html`<div class="pad"><p class="note">${t('voice.stop_to_change')}</p></div>`}
        </${Group}>`}

        ${e.installed && html`<details class="set-group vx-lib" open=${!packs.length}>
          <summary><h3>${t('voice.library')} <span class="count">${fmtSize(v.disk_bytes)}</span></h3></summary>
          <p class="note vx-lib-note">${t('voice.library_note')}</p>
          <div class="vx-filter"><${ListPick} label=${t('voice.kind_filter')} value=${kind} onChange=${setKind} options=${[{ value: '', label: t('voice.all') }, ...Object.entries(KIND()).map(([k, l]) => ({ value: k, label: l }))]} /></div>
          <div class="card">${models.filter(m => !kind || m.kind === kind).map(m => html`<div class="set-line" key=${m.id}>
            <div class="set-l vx-model"><span><b>${m.id}</b> <span class="tag">${KIND()[m.kind] || m.kind}</span>${[cfg.stt, cfg.tts, cfg.vad].includes(m.id) && html` <span class="tag green">${t('voice.in_use')}</span>`}${m.streaming && html` <span class="tag">${t('voice.streaming')}</span>`}</span><small class="muted">${[(m.languages || []).join(', '), m.family, fmtSize(m.installed ? m.disk_bytes : m.size)].filter(Boolean).join(' · ')}</small></div>
            <div class="set-c">${m.installed ? html`<button class="icon-btn" aria-label=${t('settings.page.supprimer')} disabled=${!!busy || svc.running} onClick=${async () => { if (await confirm(t('voice.delete_title', { id: m.id }), t('voice.delete_text'), { ok: t('settings.page.supprimer'), danger: true })) act('del', '/api/voice/models/delete', { id: m.id }); }}><${Icon} n="trash" /></button>`
              : html`<button class="btn sm ghost" disabled=${!!busy || dl.running || !m.installable} onClick=${() => act('dl', '/api/voice/models/download', { id: m.id })}><${Icon} n="download" />${t('voice.download')}</button>`}</div></div>`)}</div>
        </details>`}
      </div>

      <${JarvisColumn} jarvisRef=${jarvisRef} tts=${ttsInstalled} />
    </div>
    ${showPacks && html`<${PacksModal} packs=${packs} installed=${installed} busy=${busy} dl=${dl}
      onInstall=${p => { setShowPacks(false); act('pack', '/api/voice/packs', { id: p.pack.id, cuda: !!p.requires_nvidia }); }}
      onClose=${() => setShowPacks(false)} />`}
  </div></div>`;
}
