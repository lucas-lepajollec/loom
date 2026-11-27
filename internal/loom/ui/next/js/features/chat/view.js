import { VoiceMode } from '../voice/mode.js';
import { t } from '../../core/i18n.js';
// Vue discussion : en-tête (exécution, projet, panneau), fil, composeur.
import { html, useRef, useEffect, useLayoutEffect, useStore, useState, cls } from '../../core/lib.js';
import { Icon, LoomMark } from '../../ui/icons.js';
import { AmbientField } from '../../ui/ambient.js';
import { app } from '../../core/state.js';
import { chat, open } from './engine.js';
import { Messages, Starting } from './messages.js';
import { Composer } from './composer.js';
import { Picker, currentExec } from './picker.js';
import { Inspector } from '../inspector/inspector.js';
import { BackButton } from '../../app/phone-nav.js';
import { usePhone } from '../../core/viewport.js';
import { Minimap } from '../conversation/minimap.js';
import { JumpBottom } from '../conversation/kit.js';

function Thread({ phone }) {
  const { items, gen, loading, compacting } = useStore(chat, s => s.frozen ? { items: s.frozen, gen: null, loading: false, compacting: false } : { items: s.items, gen: s.gen, loading: s.loading, compacting: s.compacting });
  const starting = useStore(chat, s => s.starting);
  useStore(chat, s => s.localTarget);
  useStore(app, s => s.status && s.status.model + '|' + s.status.health);
  const { root, sessionId } = useStore(chat, s => ({ root: s.session?.workdir || '', sessionId: s.sessionId }));
  const box = useRef(), stick = useRef(true);
  const onScroll = () => {
    const el = box.current; if (!el) return; // scroll events can land after an unmount
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    stick.current = atEnd;
  };
  useLayoutEffect(() => { const el = box.current; if (el && stick.current) el.scrollTop = el.scrollHeight; });
  useEffect(() => { if (!loading) { stick.current = true; const el = box.current; if (el) el.scrollTop = el.scrollHeight; } }, [loading]);
  const empty = !loading && !items.length;
  const exec = currentExec();
  return html`<div class="cv-wrap">${!phone && !loading && !empty && html`<${Minimap} scroller=${box} />`}<div class="thread" ref=${box} onScroll=${onScroll}>
    ${loading ? html`<div class="thread-in"><div class="skeleton" style="height:44px;width:46%;margin-left:auto"></div><div class="skeleton" style="height:16px;width:80%;margin-top:26px"></div><div class="skeleton" style="height:16px;width:64%;margin-top:10px"></div></div>`
      : empty ? html`<div class="welcome anim-rise"><div class="welcome-in"><div class="welcome-mark"><${LoomMark} /></div><h1>${exec.name ? t("chat.view.que_veux_tu_faire") : t("chat.view.bienvenue_dans_loom")}</h1>
          <p>${exec.name ? html`${t("chat.view.la_discussion_part_avec")} <b>${exec.name}</b>${t("chat.view.tu_pourras_changer_de_modele_en_cours_de_route")}` : t("chat.view.choisis_un_modele_local_cloud_ou_un_harness_en_haut_pour_commence")}</p></div></div>`
      : html`<div class="thread-in"><${Messages} items=${items} gen=${gen} compacting=${compacting} root=${root} sessionId=${sessionId} />${starting && html`<${Starting} starting=${starting} />`}</div>`}
  </div><${JumpBottom} scroller=${box} watch=${items.length} /></div>`;
}

// Titre de la discussion dans l'en-tête.
function useChatTitle() {
  const title = useStore(chat, c => (c.session && c.session.title) || '');
  const navTitle = useStore(app, a => { const id = chat.get().sessionId || (a.nav && a.nav.active); const c = id && (a.nav.conversations || []).find(x => x.id === id); return c ? c.title : ''; });
  return title || navTitle;
}

function ChatTitle({ phone }) {
  const text = useChatTitle();
  return text ? html`<span class="chat-title">${phone && html`<i aria-hidden="true">/</i>`}<span>${text}</span></span>` : null;
}

// Le mode vocal remplace la discussion le temps de l'échange avec Jarvis.
export function ChatView({ route }) {
  const phone = usePhone();
  const phoneRevision = useStore(app, s => phone && s.phoneChatRevision);
  const insp = useStore(app, s => s.inspector);
  const toggle = () => { const v = !app.get().inspector; if (innerWidth > 1100) { try { localStorage.setItem('loom.next.insp', v ? '1' : '0'); } catch (_) {} } app.set({ inspector: v }); };
  const voice = useStore(app, s => s.voiceMode);
  const empty = useStore(chat, s => !s.loading && !(s.frozen || s.items || []).length);
  const hasTitle = !!useChatTitle();
  if (voice) return html`<div class="view chat-view"><${VoiceMode} discussionId=${voice.discussion} internet=${!!voice.internet} onClose=${async injected => { app.set({ voiceMode: null }); if (injected && voice.discussion) await open(voice.discussion, true); }} /></div>`;
  return html`<div class="view chat-view">
    <div class=${cls('chat-col', empty && 'is-empty')}>
      ${empty && !phone && html`<${AmbientField} variant="calm" />`}
      <header class=${cls('topbar', phone && 'phone-session-head')}>
        ${phone ? html`<${BackButton} /><div class=${cls('phone-chat-model', hasTitle && 'has-title')}><${Picker} phone /><${ChatTitle} phone /></div>` : html`<${ChatTitle} /><span class="grow"></span>`}
        <button class=${cls('icon-btn', insp && 'on')} aria-label="${t("chat.view.panneau")}" title="${t("chat.view.parametres_et_contexte")}" onClick=${toggle}><${Icon} n=${phone ? 'more' : 'panel'} /></button>
      </header>
      <${Thread} phone=${phone} />
      <${Composer} phone=${phone} dock=${!phone && html`<${Picker} place="above" />`} key=${phone ? route?.sub + '/' + phoneRevision : undefined} />
    </div>
    <${Inspector} open=${insp} onClose=${toggle} phone=${phone} />
  </div>`;
}
