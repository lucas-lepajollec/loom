import { options } from 'preact';
import { app, enterPhoneChat } from '../core/state.js';
import { baseName } from '../core/lib.js';
import { chat, newDiscussion } from '../features/chat/engine.js';

export function defaultModel(state) {
  const { workspace, status } = state;
  const choice = workspace?.models?.find(m => m.id === workspace.chat?.default_choice && m.enabled && (m.kind === 'local' || m.ready));
  const target = chat.get().localTarget;
  return choice?.name || String(target?.name || status?.preset_name || status?.model_name || baseName(status?.model)).replace(/\.gguf$/i, '');
}

export async function startPhoneDiscussion(folder, text = '', autoSend = !!text, options = {}) {
  if (app.get().phoneDraft?.preparing) return;
  const draft = { folder, text, files: options.files || [], choice: options.choice, focus: !text, preparing: true };
  const preparation = newDiscussion(folder, { navigate: false, choice: options.choice });
  enterPhoneChat(draft);
  if (draft.focus) {
    // Mount and focus inside the initiating tap so mobile Safari can open its keyboard.
    const schedule = options.debounceRendering;
    let render;
    options.debounceRendering = update => { render = update; };
    try { app.flush(); } finally { options.debounceRendering = schedule; }
    render?.();
  }
  const ready = await preparation;
  if (app.get().phoneDraft === draft) app.set({ phoneDraft: { ...draft, preparing: false, failed: !ready, send: ready && autoSend } });
}
