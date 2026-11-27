// Orchestration multi-agents Loom : arbre de sous-sessions, fil d'équipe
// partagé (board) et réglages de garde-fous, synchronisés en temps réel via
// /api/orchestration/events.
import { createStore, useEffect } from '../../core/lib.js';
import { get, post, request } from '../../core/api.js';
import { visibleConnection } from '../../core/poll.js';
import { nodeOf as nodeOfPure, rootIdOf as rootIdOfPure, visibleParentOf as visibleParentOfPure, childrenOf as childrenOfPure, descendantsOf as descendantsOfPure } from './orchestration-tree.js';

const DEFAULT_SETTINGS = {
  auto_inject_mcp: true,
  auto_write_worktree_mcp: true,
  max_depth: 2,
  max_children_per_root: 6,
  subagent_permissions: 'inherit',
};

export const orchestration = createStore({
  nodes: {},
  boardCounts: {},
  boards: {},
  catalog: null,
  settings: DEFAULT_SETTINGS,
  loaded: false,
});

export async function refreshCatalog() {
  try {
    const r = await get('/api/orchestration/catalog');
    if (r && r.ok && r.catalog) {
      orchestration.set({ catalog: r.catalog });
      return r.catalog;
    }
  } catch (_) {}
  return orchestration.get().catalog;
}

export async function reparentSession(sessionId, parentId) {
  const r = await post('/api/orchestration/reparent', { session_id: sessionId, parent_id: parentId || '' });
  if (!r || r.ok === false) throw new Error(r?.error || 'reparent failed');
  return r.node;
}

export async function pruneStopped(rootId, cleanupWorktrees = false) {
  const r = await post('/api/orchestration/prune-stopped', { root_id: rootId || '', cleanup_worktrees: cleanupWorktrees });
  if (!r || r.ok === false) throw new Error(r?.error || 'prune failed');
  return r.removed || 0;
}

export async function refreshOrchestration() {
  try {
    const r = await get('/api/orchestration/state');
    if (r && r.ok) {
      orchestration.set({
        nodes: r.nodes || {},
        boardCounts: r.board_counts || {},
        settings: { ...DEFAULT_SETTINGS, ...(r.settings || {}) },
        loaded: true,
      });
    }
  } catch (_) {}
}

export async function loadBoard(rootId) {
  if (!rootId) return [];
  const r = await get('/api/orchestration/board?root=' + encodeURIComponent(rootId));
  if (r && r.ok) {
    const rid = r.root_id || rootId;
    const msgs = Array.isArray(r.messages) ? r.messages : [];
    const st = orchestration.get();
    orchestration.set({
      boards: { ...st.boards, [rid]: msgs },
      boardCounts: { ...st.boardCounts, [rid]: msgs.length },
    });
    return msgs;
  }
  return [];
}

export async function postBoard(rootId, text, kind = 'human', toSession = '') {
  const r = await post('/api/orchestration/board', { root_id: rootId, text, kind, to_session: toSession });
  if (!r || r.ok === false) throw new Error(r?.error || 'board post failed');
  return r.message;
}

export async function spawnSubagent(body) {
  const r = await post('/api/orchestration/spawn', body);
  if (!r || r.ok === false) throw new Error(r?.error || 'spawn failed');
  return r.subagent;
}

export async function stopTree(sessionId, { mode = 'close', includeRoot = false, cleanupWorktrees = false } = {}) {
  const r = await post('/api/orchestration/stop-tree', {
    session_id: sessionId,
    mode,
    include_root: includeRoot,
    cleanup_worktrees: cleanupWorktrees,
  });
  if (!r || r.ok === false) throw new Error(r?.error || 'stop tree failed');
  return r;
}

export async function cleanupWorktree(sessionId, force = false) {
  const r = await post('/api/orchestration/cleanup-worktree', { session_id: sessionId, force });
  if (!r || r.ok === false) throw new Error(r?.error || 'cleanup failed');
  return r;
}

export async function saveOrchSettings(settings) {
  const r = await post('/api/orchestration/settings', settings);
  if (!r || r.ok === false) throw new Error(r?.error || 'save failed');
  if (r.settings) orchestration.set({ settings: { ...DEFAULT_SETTINGS, ...r.settings } });
  return r.settings;
}

// ---------- arbre ----------
export const nodeOf = (id, nodes = orchestration.get().nodes) => nodeOfPure(id, nodes);
export const rootIdOf = (id, nodes = orchestration.get().nodes) => rootIdOfPure(id, nodes);
export const visibleParentOf = (id, visibleIds, nodes = orchestration.get().nodes) => visibleParentOfPure(id, visibleIds, nodes);
export const childrenOf = (parentId, nodes = orchestration.get().nodes) => childrenOfPure(parentId, nodes);
export const descendantsOf = (parentId, nodes = orchestration.get().nodes) => descendantsOfPure(parentId, nodes);

// ---------- SSE temps réel ----------
function handleOrchEvent(name, payload) {
  const st = orchestration.get();
  if (name === 'init' && payload) {
    orchestration.set({
      nodes: payload.nodes || {},
      boardCounts: payload.board_counts || {},
      settings: { ...DEFAULT_SETTINGS, ...(payload.settings || {}) },
      loaded: true,
    });
    return;
  }
  if (name === 'settings' && payload) {
    orchestration.set({ settings: { ...DEFAULT_SETTINGS, ...payload } });
    return;
  }
  if (name === 'tree' && payload) {
    if (payload.node && payload.node.session_id) {
      orchestration.set({ nodes: { ...st.nodes, [payload.node.session_id]: payload.node } });
    } else {
      refreshOrchestration();
    }
    return;
  }
  if (name === 'board' && payload && payload.root_id) {
    const rid = payload.root_id;
    const prev = st.boards[rid];
    const nextBoards = prev
      ? { ...st.boards, [rid]: prev.some(m => m.id === payload.id) ? prev : [...prev, payload].slice(-200) }
      : st.boards;
    const nextCount = prev ? nextBoards[rid].length : (st.boardCounts[rid] || 0) + 1;
    orchestration.set({
      boards: nextBoards,
      boardCounts: { ...st.boardCounts, [rid]: nextCount },
    });
  }
}

let refs = 0;
let stopStream = null;

function openOrchStream() {
  return visibleConnection(async owner => {
    let reader;
    try {
      const r = await request('/api/orchestration/events', {
        retryAuth: false,
        timeout: 15000,
        signal: owner.signal,
        headers: { Accept: 'text/event-stream' },
      });
      if (!owner.alive()) return;
      if (!r.ok || !(r.headers.get('Content-Type') || '').includes('text/event-stream')) {
        owner.retry();
        return;
      }
      reader = r.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      (async () => {
        try {
          for (;;) {
            const { done, value } = await reader.read();
            if (done || !owner.alive()) break;
            buf += dec.decode(value, { stream: true });
            let i;
            while ((i = buf.indexOf('\n\n')) >= 0) {
              const frame = buf.slice(0, i);
              buf = buf.slice(i + 2);
              let name = 'message';
              let data = '';
              for (const line of frame.split('\n')) {
                if (line.startsWith('event:')) name = line.slice(6).trim();
                else if (line.startsWith('data:')) data += line.slice(5).trim();
              }
              if (!data) continue;
              try { handleOrchEvent(name, JSON.parse(data)); } catch (_) {}
            }
            if (buf.length > 2 * 1024 * 1024) buf = '';
          }
        } catch (_) {}
        if (owner.alive()) owner.retry();
      })();
    } catch (_) {
      if (owner.alive()) owner.retry();
      return;
    }
    return () => { reader?.cancel().catch(() => {}); };
  }, 3000);
}

export function useOrchestrationLive() {
  useEffect(() => {
    refs++;
    if (!stopStream) stopStream = openOrchStream();
    return () => {
      if (--refs <= 0) {
        refs = 0;
        stopStream?.();
        stopStream = null;
      }
    };
  }, []);
}
