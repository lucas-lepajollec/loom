import { VoiceMode } from '../voice/mode.js';
import { t } from '../../core/i18n.js';
// Vue discussion : en-tête (exécution, projet, panneau), fil, composeur.
import { html, useRef, useEffect, useLayoutEffect, useStore, useState, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { app } from '../../core/state.js';
import { chat, open } from './engine.js';
import { Messages } from './messages.js';
import { Composer } from './composer.js';
import { Picker, currentExec } from './picker.js';
import { Inspector } from '../inspector/inspector.js';

function Thread() {
  const { items, gen, loading, compacting } = useStore(chat, s => s.frozen ? { items: s.frozen, gen: null, loading: false, compacting: false } : { items: s.items, gen: s.gen, loading: s.loading, compacting: s.compacting });
  useStore(app, s => s.status && s.status.model + '|' + s.status.health);
  const { root, sessionId } = useStore(chat, s => ({ root: (s.harness && s.harness.workdir) || '', sessionId: s.sessionId }));
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
  const exec = currentExec();
  return html`<div class="thread" ref=${box} onScroll=${onScroll}>
    ${loading ? html`<div class="thread-in"><div class="skeleton" style="height:44px;width:46%;margin-left:auto"></div><div class="skeleton" style="height:16px;width:80%;margin-top:26px"></div><div class="skeleton" style="height:16px;width:64%;margin-top:10px"></div></div>`
      : empty ? html`<div class="welcome anim-rise"><div class="welcome-mark"><${Icon} n="loom" /></div><h1>${exec.name ? t("chat.view.que_veux_tu_faire") : t("chat.view.bienvenue_dans_loom")}</h1>
          <p>${exec.name ? html`${t("chat.view.la_discussion_part_avec")} <b>${exec.name}</b>${t("chat.view.tu_pourras_changer_de_modele_en_cours_de_route")}` : t("chat.view.choisis_un_modele_local_cloud_ou_un_harness_en_haut_pour_commence")}</p></div>`
      : html`<div class="thread-in"><${Messages} items=${items} gen=${gen} compacting=${compacting} root=${root} sessionId=${sessionId} /></div>`}
    ${showDown && html`<button class="to-end anim-fade" aria-label="${t("chat.view.aller_en_bas")}" onClick=${() => { stick.current = true; const el = box.current; if (el) el.scrollTop = el.scrollHeight; }}><${Icon} n="chevron" /></button>`}
  </div>`;
}

// Le mode vocal remplace la discussion le temps de l'échange avec Jarvis.
function voiceDiscussion() { return chat.get().sessionId || (app.get().nav && app.get().nav.active) || ''; }
export function ChatView() {
  const insp = useStore(app, s => s.inspector);
  const toggle = () => { const v = !app.get().inspector; if (innerWidth > 1100) { try { localStorage.setItem('loom.next.insp', v ? '1' : '0'); } catch (_) {} } app.set({ inspector: v }); };
  const voice = useStore(app, s => s.voiceMode);
  if (voice) return html`<div class="view chat-view"><${VoiceMode} discussionId=${voice.discussion} internet=${!!voice.internet} onClose=${async injected => { app.set({ voiceMode: null }); if (injected && voice.discussion) await open(voice.discussion, true); }} /></div>`;
  return html`<div class="view chat-view">
    <div class="chat-col">
      <header class="topbar">
        <button class="icon-btn only-mobile" aria-label="${t("chat.view.menu")}" onClick=${() => app.set({ sideOpen: true })}><${Icon} n="menu" /></button>
        <${Picker} />
        <span class="grow"></span>
        <button class=${cls('icon-btn', insp && 'on')} aria-label="${t("chat.view.panneau")}" title="${t("chat.view.parametres_et_contexte")}" onClick=${toggle}><${Icon} n="panel" /></button>
      </header>
      <${Thread} />
      <${Composer} />
    </div>
    <${Inspector} open=${insp} onClose=${toggle} />
  </div>`;
}
