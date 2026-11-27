import { options } from 'preact';
import { app, enterPhoneChat } from '../core/state.js';
import { baseName } from '../core/lib.js';
import { chat, newDiscussion, chooseLocal, chooseRemote } from '../features/chat/engine.js';

export function defaultModel(state) {
  const { workspace, status } = state;
  const choice = workspace?.models?.find(m => m.id === workspace.chat?.default_choice && m.enabled && (m.kind === 'local' || m.ready));
  const target = chat.get().localTarget;
  return choice?.name || String(target?.name || status?.preset_name || status?.model_name || baseName(status?.model)).replace(/\.gguf$/i, '');
}

export async function startPhoneDiscussion(folder, text = '', autoSend = !!text, selection = null) {
  if (app.get().phoneDraft?.preparing) return;
  const draft = { folder, text, focus: !text, preparing: true };
  const preparation = (async () => {
    const ok = await newDiscussion(folder, { navigate: false });
    if (!ok) return false;
    if (selection?.kind === 'local') await chooseLocal(selection.target);
    else if (selection?.kind === 'cloud') await chooseRemote(selection.model);
    return true;
  })();
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
