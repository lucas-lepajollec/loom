// Actions du domaine Agents. Chacune traduit une intention en appels
// officiels Codeman ; aucune ne contourne ses garde-fous ni ne choisit de
// mode de permission à la place de l'utilisateur.
import { api, sendKeys } from './client.js';
import { agents, upsertSession, dropSession, refreshUnified } from './store.js';

// Configuration propre à chaque CLI, d'après les schémas de création Codeman.
const CONFIG_KEY = { opencode: 'openCodeConfig', codex: 'codexConfig', gemini: 'geminiConfig', antigravity: 'antigravityConfig', pi: 'piConfig', grok: 'grokConfig', copilot: 'copilotConfig', deepseek: 'deepSeekConfig', omp: 'ompConfig' };
export const supportsModel = mode => mode === 'claude' || ['opencode', 'codex', 'gemini', 'antigravity', 'pi', 'grok', 'copilot', 'omp'].includes(mode);

function nextName(dir) {
  const base = String(dir || '').replace(/\/+$/, '').split('/').pop() || 'session';
  let n = 1;
  for (const s of Object.values(agents.get().sessions)) {
    const m = /^w(\d+)-/.exec(s.name || '');
    if (m && +m[1] >= n) n = +m[1] + 1;
  }
  return `w${n}-${base}`;
}

// Crée la session puis démarre son CLI (ou le shell). Codeman répond à la
// boîte de dialogue de confiance de Claude dans sa fenêtre de démarrage.
// Reprise après la limite d'abonnement pour les nouvelles sessions Claude
// (préférence de ce navigateur, activée par défaut).
export const autoResumeDefault = () => { try { return localStorage.getItem('loom.codeman.autoresume') !== '0'; } catch (_) { return true; } };
export const setAutoResumeDefault = on => { try { localStorage.setItem('loom.codeman.autoresume', on ? '1' : '0'); } catch (_) {} };
export const agentDraftKey = id => 'loom.codeman.draft.' + id;

export async function createSession({ mode, workingDir, name, model, effort }) {
  const body = { workingDir, mode, name: (name || '').trim() || nextName(workingDir) };
  const m = String(model || '').trim();
  const ef = String(effort || '').trim();
  if (mode === 'claude') {
    if (m && supportsModel(mode)) body.model = m;
  } else if (CONFIG_KEY[mode] && (m || (mode === 'codex' && ef))) {
    const cfg = {};
    if (m && supportsModel(mode)) cfg.model = m;
    if (mode === 'codex' && ef) cfg.reasoningEffort = ef;
    if (Object.keys(cfg).length) body[CONFIG_KEY[mode]] = cfg;
  }
  const created = await api.create(body);
  const session = created?.session || created;
  if (!session?.id) throw new Error('no session');
  upsertSession(session);
  try { await api.start(session.id, mode === 'shell'); }
  catch (err) { err.sessionId = session.id; throw err; }
  if (mode === 'claude' && autoResumeDefault()) api.autoResume(session.id, true).catch(() => {});
  api.session(session.id).then(upsertSession).catch(() => {});
  return session.id;
}

// Reprendre une conversation passée, comme l'interface officielle : Claude
// par l'identifiant de conversation, Codex par son fil, OpenCode/Pi/Grok/OMP
// par « continuer la plus récente » dans le même dossier. Gemini et
// Antigravity n'exposent pas de reprise : une nouvelle session est ouverte.
export function resumeSupport(row) {
  const mode = row.mode || 'claude';
  if (mode === 'claude') return 'exact';
  if (mode === 'codex') return row.resumeId ? 'exact' : 'none';
  if (['opencode', 'pi', 'grok', 'copilot', 'omp', 'deepseek'].includes(mode)) return 'latest';
  return 'none';
}
export async function resumeSession(row) {
  const mode = row.mode || 'claude';
  const body = { workingDir: row.workingDir, mode, name: row.name || nextName(row.workingDir) };
  if (mode === 'claude') body.resumeSessionId = row.claudeSessionId || row.sessionId;
  else if (mode === 'codex' && row.resumeId) body.codexConfig = { resumeSessionId: row.resumeId };
  else if (mode === 'deepseek') body.deepSeekConfig = { resumeSession: true };
  else if (CONFIG_KEY[mode] && resumeSupport(row) === 'latest') body[CONFIG_KEY[mode]] = { continueSession: true };
  const created = await api.create(body);
  const session = created?.session || created;
  upsertSession(session);
  await api.start(session.id, mode === 'shell');
  // Une reprise non-Claude est une nouvelle session : retirer l'ancienne ligne
  // seulement quand la conversation est réellement continuée.
  if (mode !== 'claude' && resumeSupport(row) !== 'none' && row.sessionId && row.sessionId !== session.id && !agents.get().sessions[row.sessionId]) {
    api.remove(row.sessionId).catch(() => {});
  }
  refreshUnified();
  return session.id;
}

export const interrupt = id => sendKeys(id, '\u001b');

export async function closeSession(id, killMux = true) {
  await api.remove(id, killMux);
  dropSession(id);
  refreshUnified();
}

export async function renameSession(id, name) {
  await api.rename(id, name);
  upsertSession({ id, name });
}

export async function answerApproval(item, body) {
  await api.answer(item.id, body);
  agents.set({ approvals: agents.get().approvals.filter(a => a.id !== item.id) });
}
// Accord lu à l'écran (Codex et les CLI auxquels Codeman ne répond pas) :
// Loom envoie la touche du choix, seulement si la fenêtre est encore affichée.
const plainScreen = s => String(s || '').replace(/\u001b\[[0-9;?]*[ -/]*[@-~]/g, '').replace(/\u001b\][^\u0007]*\u0007/g, '');
export async function answerOnScreen(item, choice) {
  const r = await api.terminal(item.sessionId, 'tail=120');
  const screen = plainScreen(r?.terminalBuffer).replace(/\s+/g, ' ');
  if (!screen.includes(choice.label.replace(/\s+/g, ' ').slice(0, 28))) {
    const e = new Error('dialog gone'); e.code = 'CONFLICT'; throw e;
  }
  await sendKeys(item.sessionId, choice.key);
  await api.dismiss(item.id).catch(() => {});
  agents.set({ approvals: agents.get().approvals.filter(a => a.id !== item.id) });
}
export async function dismissApproval(item) {
  await api.dismiss(item.id);
  agents.set({ approvals: agents.get().approvals.filter(a => a.id !== item.id) });
}
