import { t, locale, tSource } from '../../core/i18n.js';
// Panneau de droite : s'adapte à l'exécution choisie. Local = paramètres
// llama.cpp ; Cloud = fournisseur, usage, coût.
import { html, useState, useEffect, useRef, useMemo, useStore, cls, fmtTok, fmtSecs, baseName } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg, Switch, Tip } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { Logo } from '../../ui/logo.js';
import { WorkspacePicker } from '../workspaces/folders.js';
import { Modal, toast, confirm, prompt } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, refreshWorkspace } from '../../core/state.js';
import { chat, open } from '../chat/engine.js';
import { currentExec } from '../chat/picker.js';
import { openTerminalWith } from '../terminals/page.js';
import { LocalParams } from './params.js';
import { panelState, DEFAULT_PANEL_WIDTH, clampPanelWidth, setLivePanelWidth, savePanelWidth } from '../agents/panel-state.js';

import { PreviewTab } from '../previews/tab.js';
import { extractFilesFromText, extractUrlsFromText } from '../agents/panel-helpers.js';

function DiscussionPreview({ onClose }) {
  const { id, workingDir, items } = useStore(chat, c => ({ id: c.sessionId || '', workingDir: c.session?.workdir || '', items: c.items }));
  const [active, setActive] = useState('');
  const preview = useMemo(() => {
    const text = items.slice(-24).map(i => i.text || '').join('\n');
    return { active, recent: [], detected: extractFilesFromText(text, workingDir), urls: extractUrlsFromText(text) };
  }, [active, items, workingDir]);
  return html`<${PreviewTab} key=${id} preview=${preview} session=${{ id, workingDir }} onFile=${setActive} onClose=${onClose} />`;
}

const KV = ({ k, children }) => html`<div class="kv"><span>${k}</span><span>${children}</span></div>`;

function lastTurn(s) { return s && (s.turns || []).at(-1); }

function CloudPanel() {
  const s = useStore(chat, c => c.session);
  const ws = useStore(app, a => a.workspace);
  if (!s) return null;
  const p = ((ws && ws.providers) || []).find(x => x.id === s.provider_id);
  const localT = lastTurn(s), u = localT && localT.usage;
  return html`<div class="insp-body">
    <div class="insp-model"><b>${baseName(s.model)}</b><span>${s.provider_name}</span></div>
    <p class="note insp-hint"><${Icon} n="globe" />${t('search.cloud_where')}<${Tip} text=${t('search.cloud_tip')} /></p>
    <div class="card pad-sm">
      <${KV} k=${t('cloud.page.fournisseur')}>${s.provider_name}</${KV}>
      <${KV} k=${t('settings.page.cle')}>${p && p.ready ? html`<span class="tag green">${t("inspector.inspector.en_memoire")}</span>` : html`<span class="tag amber">${t("inspector.inspector.a_reconnecter")}</span>`}</${KV}>
      <${KV} k=${t('inspector.inspector.destination')}><span class="mono trunc">${s.endpoint}</span></${KV}>
    </div>
    <div class="card pad-sm">
      <div class="lbl" style="margin-bottom:4px">${t("inspector.inspector.dernier_tour")}</div>
      ${u ? html`<${KV} k=${t('usage.page.entree')}>${fmtTok(u.prompt_tokens)} ${t("inspector.inspector.tok")}</${KV}><${KV} k=${t('bench.page.sortie')}>${fmtTok(u.completion_tokens)} ${t("inspector.inspector.tok")}</${KV}>`
        : html`<p class="note">${t("inspector.inspector.le_fournisseur_n_a_pas_communique_de_decompte")}</p>`}
      ${localT && localT.duration_seconds > 0 && html`<${KV} k=${t('bench.page.duree')}>${fmtSecs(localT.duration_seconds)}</${KV}>`}
    </div>
    <p class="note">${t("inspector.inspector.texte_seulement_les_fichiers_outils_et_la_memoire_privee_ne_sont")}</p>
    <a class="btn" href="#/cloud">${t("inspector.inspector.gerer_les_fournisseurs")}</a>
  </div>`;
}

export function Inspector({ open, onClose, phone }) {
  useStore(app, a => a.status && a.status.model);
  // currentExec() dépend de la discussion et du registre des runtimes (chargé après).
  useStore(chat, c => (c.mode || '') + ':' + ((c.session && c.session.runtime_id) || ''));
  useStore(app, a => a.workspace && a.workspace.runtimes);
  const width = useStore(panelState, p => p.width);
  const dragRef = useRef(null);
  useEffect(() => () => { document.body.classList.remove('cm-dragging'); }, []);
  const exec = currentExec();
  const title = exec.kind === 'local' ? t("inspector.inspector.parametres") : exec.kind === 'cloud' ? t("inspector.inspector.modele") : t("inspector.inspector.session");
  const [preview, showPreview] = useState(false);
  const sid = useStore(chat, c => c.sessionId);
  const settings = exec.kind === 'local' ? html`<${LocalParams} />` : exec.kind === 'cloud' ? html`<${CloudPanel} />` : html`<p class="note">${t('runtime.retired')}</p>`;
  const content = html`<div class="up-discussion-tabs"><${Seg} value=${preview ? 'preview' : 'settings'} label=${t('agents.insp.tabs')}
    options=${[{ value: 'settings', label: title }, { value: 'preview', label: t('agents.insp.preview') }]} onChange=${v => showPreview(v === 'preview')} /></div>
    ${preview && open ? html`<${DiscussionPreview} key=${sid} onClose=${() => showPreview(false)} />` : settings}`;
  if (phone) return open ? html`<${Modal} title=${title} onClose=${onClose} class="chat-inspector-sheet" focusField=${false}>${content}</${Modal}>` : null;
  const onResizeDown = e => {
    if (e.button !== 0 && e.pointerType === 'mouse') return;
    e.preventDefault();
    dragRef.current = { startX: e.clientX, startW: width, lastW: width };
    e.currentTarget.setPointerCapture(e.pointerId);
    document.body.classList.add('cm-dragging');
  };
  const onResizeMove = e => {
    if (!dragRef.current) return;
    dragRef.current.lastW = setLivePanelWidth(dragRef.current.startW + (dragRef.current.startX - e.clientX));
  };
  const onResizeUp = e => {
    if (!dragRef.current) return;
    const finalW = dragRef.current.lastW;
    dragRef.current = null;
    try { e.currentTarget.releasePointerCapture(e.pointerId); } catch (_) {}
    document.body.classList.remove('cm-dragging');
    savePanelWidth(finalW);
  };
  const onResizeKey = e => {
    if (e.key === 'ArrowLeft') { e.preventDefault(); savePanelWidth(width + 24); }
    else if (e.key === 'ArrowRight') { e.preventDefault(); savePanelWidth(width - 24); }
    else if (e.key === 'Enter' || e.key === '0') { e.preventDefault(); savePanelWidth(DEFAULT_PANEL_WIDTH); }
  };
  const wPx = clampPanelWidth(width);
  return html`<aside class=${cls('insp', open && 'open')} style=${open ? `width:${wPx}px` : undefined} aria-label="${t("inspector.inspector.panneau")}" aria-hidden=${String(!open)}>
    ${open && html`<div class="cm-insp-resize" role="separator" aria-orientation="vertical" aria-label=${t('agents.insp.resize')} title=${t('agents.insp.resize')} tabIndex="0"
      onPointerDown=${onResizeDown} onPointerMove=${onResizeMove} onPointerUp=${onResizeUp} onPointerCancel=${onResizeUp}
      onDblClick=${() => savePanelWidth(DEFAULT_PANEL_WIDTH)} onKeyDown=${onResizeKey}><i></i></div>`}
    <div class="insp-in" style=${open ? `width:${wPx}px` : undefined}>
      <header class="insp-head">
        <b class="insp-title">${title}</b>
        <button class="icon-btn" aria-label="${t("inspector.inspector.fermer_le_panneau")}" onClick=${onClose}><${Icon} n="close" /></button>
      </header>
      <div class="insp-scroll" key=${exec.kind}>${content}</div>
    </div>
  </aside>`;
}
