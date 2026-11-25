import { VoiceMode } from '../voice/mode.js';
import { t } from '../../core/i18n.js';
// Vue discussion : en-tête (exécution, projet, panneau), fil, composeur.
import { html, useRef, useEffect, useLayoutEffect, useStore, useState, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { app } from '../../core/state.js';
import { chat, open } from './engine.js';
import { Messages, Starting } from './messages.js';
import { Composer } from './composer.js';
import { Picker } from './picker.js';
import { Inspector } from '../inspector/inspector.js';
import { Seg } from '../../ui/controls.js';
import { agents } from '../agents/store.js';
import { go } from '../../core/state.js';
import { BackButton } from '../../app/phone-nav.js';
import { usePhone } from '../../core/viewport.js';

function Thread({ phone }) {
  const { items, gen, loading, compacting } = useStore(chat, s => s.frozen ? { items: s.frozen, gen: null, loading: false, compacting: false } : { items: s.items, gen: s.gen, loading: s.loading, compacting: s.compacting });
  const starting = useStore(chat, s => s.starting);
  useStore(chat, s => s.localTarget);
  useStore(app, s => s.status && s.status.model + '|' + s.status.health);
  const { root, sessionId } = useStore(chat, s => ({ root: s.session?.workdir || '', sessionId: s.sessionId }));
  const box = useRef(), stick = useRef(true);
  const [showDown, setShowDown] = useState(false);
  const onScroll = () => {
    const el = box.current; if (!el) return; // scroll events can land after an unmount
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    stick.current = atEnd; setShowDown(!atEnd);
  };
  useLayoutEffect(() => { const el = box.current; if (el && stick.current) el.scrollTop = el.scrollHeight; });
  useEffect(() => { if (!loading) { stick.current = true; const el = box.current; if (el) el.scrollTop = el.scrollHeight; } }, [loading]);
  const empty = !loading && !items.length;
  return html`<div class="thread" ref=${box} onScroll=${onScroll}>
    ${loading ? html`<div class="thread-in"><div class="skeleton" style="height:44px;width:46%;margin-left:auto"></div><div class="skeleton" style="height:16px;width:80%;margin-top:26px"></div><div class="skeleton" style="height:16px;width:64%;margin-top:10px"></div></div>`
      : empty ? html`<div class="welcome anim-rise"><h1>${t('noir.new_title')}</h1>${!phone && html`<${SessionType} />`}</div>`
      : html`<div class="thread-in"><${Messages} items=${items} gen=${gen} compacting=${compacting} root=${root} sessionId=${sessionId} />${starting && html`<${Starting} starting=${starting} />`}</div>`}
    ${showDown && html`<button class="to-end anim-fade" aria-label="${t("chat.view.aller_en_bas")}" onClick=${() => { stick.current = true; const el = box.current; if (el) el.scrollTop = el.scrollHeight; }}><${Icon} n="chevron" /></button>`}
  </div>`;
}

// Une nouvelle session est une discussion (un modèle) ou une session d'agent
// (un harness lancé par Codeman). Choisir Agent ouvre la fenêtre de Codeman.
function SessionType() {
  const conn = useStore(agents, s => s.conn);
  const pick = v => { if (v !== 'agent') return; if (conn.state === 'ready') agents.set({ creating: { type: 'agent' } }); else go('agents', 'settings'); };
  return html`<div class="welcome-type"><${Seg} value="chat" size="lg" onChange=${pick} label=${t('side.session_type')}
    options=${[{ value: 'chat', label: html`<${Icon} n="chat" />${t('side.type_chat')}` }, { value: 'agent', label: html`<${Icon} n="agent" />${t('side.type_agent')}` }]} /></div>`;
}

// Titre de la discussion à côté du modèle, comme le nom d'une session d'agent.
function ChatTitle() {
  const title = useStore(chat, c => (c.session && c.session.title) || '');
  const navTitle = useStore(app, a => { const id = chat.get().sessionId || (a.nav && a.nav.active); const c = id && (a.nav.conversations || []).find(x => x.id === id); return c ? c.title : ''; });
  const text = title || navTitle;
  return text ? html`<span class="chat-title"><i aria-hidden="true">/</i><span>${text}</span></span>` : null;
}

// Le mode vocal remplace la discussion le temps de l'échange avec Jarvis.
export function ChatView({ route }) {
  const phone = usePhone();
  const phoneRevision = useStore(app, s => s.phoneChatRevision);
  const [override, setOverride] = useState(false);
  const insp = useStore(app, s => s.inspector);
  const toggle = () => { const v = empty && !phone ? !override : !app.get().inspector; setOverride(v); if (innerWidth >= 1180) { try { localStorage.setItem('loom.next.insp', v ? '1' : '0'); } catch (_) {} } app.set({ inspector: v }); };
  const voice = useStore(app, s => s.voiceMode);
  const empty = useStore(chat, s => !s.loading && !(s.frozen || s.items || []).length);
  if (voice) return html`<div class="view chat-view"><${VoiceMode} discussionId=${voice.discussion} internet=${!!voice.internet} onClose=${async injected => { app.set({ voiceMode: null }); if (injected && voice.discussion) await open(voice.discussion, true); }} /></div>`;
  return html`<div class="view chat-view">
    <div class=${cls('chat-col', empty && 'is-empty')}>
      <header class=${cls('topbar', phone && 'phone-session-head')}>
        ${phone ? html`<${BackButton} /><div class="phone-chat-model"><${Picker} phone /><${ChatTitle} /></div>` : html`<${Picker} /><${ChatTitle} /><span class="grow"></span>`}
        <button class=${cls('icon-btn', insp && 'on')} aria-label="${t("chat.view.panneau")}" title="${t("chat.view.parametres_et_contexte")}" onClick=${toggle}><${Icon} n=${phone ? 'more' : 'panel'} /></button>
      </header>
      <${Thread} phone=${phone} />
      <${Composer} phone=${phone} key=${route?.sub + '/' + phoneRevision} />
    </div>
    ${insp && (!empty || phone || override) && html`<${Inspector} key=${route.sub} onClose=${toggle} />`}
  </div>`;
}
