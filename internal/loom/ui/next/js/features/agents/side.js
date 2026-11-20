// Pièces de la barre latérale propres à Codeman : l'historique à reprendre et
// la fenêtre « nouvelle session ». La liste elle-même est dans app/sessions.js.
import { t } from '../../core/i18n.js';
import { html, useState, useStore, useEffect } from '../../core/lib.js';
import { confirm, toast } from '../../ui/dialog.js';
import { agents, projectOf } from './store.js';
import { placeSession } from './layout.js';
import { resumeSession, resumeSupport } from './actions.js';
import { CliMark, NewSession } from './new-session.js';
import { openSession } from './nav.js';

export function HistoryRow({ r }) {
  const [busy, setBusy] = useState(false);
  const resume = async () => {
    if (busy) return;
    const support = resumeSupport(r);
    const text = support === 'none' ? t('agents.history.no_resume', { cli: r.mode }) : support === 'latest' ? t('agents.history.resume_latest') : t('agents.history.resume_exact');
    if (!await confirm(t('agents.history.resume_title'), text, { ok: support === 'none' ? t('agents.history.new_here') : t('agents.history.resume') })) return;
    setBusy(true);
    try { openSession(await resumeSession(r)); } catch (e) { toast(e.message, 'err'); } finally { setBusy(false); }
  };
  return html`<button class="chat-link sub cm-side-hist" disabled=${busy} onClick=${resume} title=${r.workingDir}>
    <${CliMark} mode=${r.mode || 'claude'} size="xs" />
    <span>${r.name || (r.firstPrompt ? r.firstPrompt.slice(0, 60) : r.sessionId.slice(0, 8))}</span>
    ${busy ? html`<span class="spinner"></span>` : html`<small>${projectOf(r.workingDir)}</small>`}</button>`;
}

// Fenêtre « nouvelle session », ouverte depuis la barre latérale, la page ou
// un volet vide de la grille (qui reçoit alors la session).
export function NewSessionHost() {
  const creating = useStore(agents, s => s.creating);
  useEffect(() => {
    const key = e => { if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === 'n' && agents.get().conn.state === 'ready') { e.preventDefault(); agents.set({ creating: {} }); } };
    addEventListener('keydown', key); return () => removeEventListener('keydown', key);
  }, []);
  if (!creating) return null;
  const cell = creating.cell;
  return html`<${NewSession} preset=${creating.preset} onClose=${() => agents.set({ creating: null })}
    onCreated=${id => { if (cell != null) placeSession(id, cell); openSession(id); }} />`;
}
