// Jarvis : le profil du compagnon vocal. Qui il est (nom, langue, tutoiement,
// longueur des réponses, personnalité), avec quel modèle il réfléchit, ce qu'il
// lit, et sa voix propre. Contrat : docs/voice.md « Jarvis profile ».
import { t } from '../../core/i18n.js';
import { html, useState, useEffect } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go } from '../../core/state.js';
import { GroupPage } from '../../app/sections.js';
import { Line, Group } from '../settings/kit.js';

const LANGS = () => ({ fr: t('jv.lang_fr'), en: t('jv.lang_en'), es: t('jv.lang_es'), de: t('jv.lang_de'), it: t('jv.lang_it'), pt: t('jv.lang_pt'), nl: t('jv.lang_nl') });
const PERSONA_MAX = 1000;

export function JarvisPage() {
  const [p, setP] = useState(null), [tts, setTts] = useState([]);
  const [name, setName] = useState(''), [persona, setPersona] = useState('');
  const fill = r => { setP(r); setName(r.name || ''); setPersona(r.personality || ''); };
  useEffect(() => {
    get('/api/voice/jarvis').then(r => r.ok === false ? setP({ error: r.error }) : fill(r)).catch(e => setP({ error: e.message }));
    get('/api/voice/models').then(r => setTts((r.models || []).filter(m => m.kind === 'tts' && m.installed))).catch(() => {});
  }, []);
  const save = async patch => {
    const r = await post('/api/voice/jarvis', patch).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error || t('jv.failed'), 'err');
    fill(r); toast(t('jv.saved'));
  };
  const talk = () => { go('chat'); app.set({ voiceMode: { discussion: '' } }); };
  if (!p) return html`<${GroupPage} title=${t('jv.title')} lead=${t('jv.lead')}><div class="skeleton" style="height:220px"></div></${GroupPage}>`;
  if (p.error) return html`<${GroupPage} title=${t('jv.title')} lead=${t('jv.lead')}><p class="note err">${p.error}</p></${GroupPage}>`;
  const routes = (p.routes || []).filter(r => r.enabled !== false);
  const routeOpts = routes.map(r => ({ value: r.id, label: r.name, note: (r.kind === 'local' ? t('jv.local') : r.provider_name || t('jv.cloud')) + (r.ready ? '' : ' · ' + t('jv.not_ready')) }));
  const v = p.voice || {};
  return html`<${GroupPage} title=${t('jv.title')} lead=${t('jv.lead')}>
    <div class="jv-talk"><button class="btn primary" onClick=${talk}><${Icon} n="mic" />${t('jv.talk', { name: p.name })}</button><span class="note">${t('jv.talk_note')}</span></div>

    <${Group} title=${t('jv.identity')}>
      <${Line} label=${t('jv.name')} tip=${t('jv.name_tip')}><input class="input sm" maxlength="80" value=${name} onInput=${e => setName(e.target.value)} onBlur=${() => name.trim() !== p.name && save({ name: name.trim() })} /></${Line}>
      <${Line} label=${t('jv.language')} tip=${t('jv.language_tip')}><div class="set-pick"><${ListPick} label=${t('jv.language')} value=${p.language} onChange=${x => save({ language: x })} options=${[{ value: 'auto', label: t('jv.lang_auto') }, ...Object.entries(LANGS()).map(([value, label]) => ({ value, label }))]} /></div></${Line}>
      <${Line} label=${t('jv.formality')} tip=${t('jv.formality_tip')}><${Seg} size="sm" label=${t('jv.formality')} value=${p.formality} onChange=${x => save({ formality: x })} options=${[{ value: 'tu', label: t('jv.tu') }, { value: 'vous', label: t('jv.vous') }]} /></${Line}>
      <${Line} label=${t('jv.length')} tip=${t('jv.length_tip')}><${Seg} size="sm" label=${t('jv.length')} value=${p.length} onChange=${x => save({ length: x })} options=${[{ value: 'short', label: t('jv.short') }, { value: 'normal', label: t('jv.normal') }, { value: 'detailed', label: t('jv.detailed') }]} /></${Line}>
    </${Group}>

    <${Group} title=${t('jv.personality')}>
      <div class="pad jv-persona">
        <textarea class="textarea" rows="4" maxlength=${PERSONA_MAX} placeholder=${t('jv.personality_ph')} value=${persona} onInput=${e => setPersona(e.target.value)}></textarea>
        <div class="set-actions"><small class="muted">${persona.length} / ${PERSONA_MAX}</small><span class="grow"></span>
          <button class="btn sm" disabled=${persona === (p.personality || '')} onClick=${() => save({ personality: persona })}>${t('jv.save')}</button></div>
        <p class="note">${t('jv.personality_note')}</p>
      </div>
    </${Group}>

    <${Group} title=${t('jv.brain')}>
      <${Line} label=${t('jv.model')} tip=${t('jv.model_tip')}><div class="set-pick"><${ListPick} label=${t('jv.model')} value=${p.model} onChange=${x => save({ model: x })} options=${[{ value: 'discussion', label: t('jv.model_discussion') }, ...routeOpts]} /></div></${Line}>
      <${Line} label=${t('jv.fallback')} tip=${t('jv.fallback_tip')}><div class="set-pick"><${ListPick} label=${t('jv.fallback')} value=${p.fallback} onChange=${x => save({ fallback: x })} options=${[{ value: '', label: t('jv.fallback_auto') }, ...routeOpts]} /></div></${Line}>
      <${Line} label=${t('jv.context')} tip=${t('jv.context_tip')}><div class="set-pick"><${ListPick} label=${t('jv.context')} value=${p.context} onChange=${x => save({ context: x })} options=${[{ value: 'discussion+memory', label: t('jv.ctx_all') }, { value: 'memory', label: t('jv.ctx_memory') }, { value: 'none', label: t('jv.ctx_none') }]} /></div></${Line}>
    </${Group}>

    <${Group} title=${t('jv.voice')}>
      <${Line} label=${t('jv.tts')} tip=${t('jv.tts_tip')}><div class="set-pick"><${ListPick} label=${t('jv.tts')} value=${v.tts_model || ''} onChange=${x => save({ voice: { ...v, tts_model: x } })} options=${[{ value: '', label: t('jv.inherit') }, ...tts.map(m => ({ value: m.id, label: m.id, note: (m.languages || []).join(', ') }))]} /></div></${Line}>
      <${Line} label=${t('jv.voice_id')} tip=${t('jv.voice_id_tip')}><input class="input sm num" type="number" min="0" max="1024" step="1" placeholder=${t('jv.inherit_short')} value=${v.voice_id ?? ''} onChange=${e => save({ voice: { ...v, voice_id: e.target.value === '' ? null : Number(e.target.value) } })} /></${Line}>
      <${Line} label=${t('jv.speed')} tip=${t('jv.speed_tip')}><input class="input sm num" type="number" min="0.5" max="2" step="0.05" placeholder=${t('jv.inherit_short')} value=${v.speed ?? ''} onChange=${e => save({ voice: { ...v, speed: e.target.value === '' ? null : Number(e.target.value) } })} /></${Line}>
      <div class="pad"><p class="note">${t('jv.voice_note')} <a href="#/voice">${t('jv.voice_link')}</a></p></div>
    </${Group}>
  </${GroupPage}>`;
}
