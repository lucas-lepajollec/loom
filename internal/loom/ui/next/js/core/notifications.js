// Notifications applicatives légères : réponses de discussion terminées hors
// vue et alertes masquées (mises à jour, quotas) mémorisées par identifiant.
import { createStore } from './lib.js';

const DISMISS_KEY = 'loom.notifications.dismissed';
function loadDismissed() {
  try {
    const raw = JSON.parse(localStorage.getItem(DISMISS_KEY) || '[]');
    return Array.isArray(raw) ? raw.slice(-80) : [];
  } catch (_) { return []; }
}
function saveDismissed(list) {
  try { localStorage.setItem(DISMISS_KEY, JSON.stringify(list.slice(-80))); } catch (_) {}
}

export const notifications = createStore({
  chatReplies: [],
  dismissed: loadDismissed(),
});

export function notifyChatReply({ id = '', title = '' } = {}) {
  const key = id || 'native';
  const cur = notifications.get().chatReplies.filter(x => x.id !== key);
  notifications.set({
    chatReplies: [{ id: key, title: title || '', at: Date.now() }, ...cur].slice(0, 12),
  });
}

export function clearChatReply(id) {
  const key = id || 'native';
  const cur = notifications.get().chatReplies;
  if (!cur.some(x => x.id === key)) return;
  notifications.set({ chatReplies: cur.filter(x => x.id !== key) });
}

export function clearAllChatReplies() {
  if (!notifications.get().chatReplies.length) return;
  notifications.set({ chatReplies: [] });
}

export function dismissNotice(id) {
  if (!id) return;
  const cur = notifications.get().dismissed;
  if (cur.includes(id)) return;
  const next = [...cur, id].slice(-80);
  saveDismissed(next);
  notifications.set({ dismissed: next });
}
