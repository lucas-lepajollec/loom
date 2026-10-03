import { useEffect, useRef } from '../../core/lib.js';

// One read at a time. Pause while hidden and refresh when the page is visible
// again. Late responses cannot update an unmounted page.
export function visibleRefresh(task, interval, env = globalThis) {
  let stopped = false, pending = false, timer;
  const alive = () => !stopped;
  const run = async () => {
    env.clearTimeout(timer);
    if (stopped || pending || env.document.hidden) return;
    pending = true;
    try { await task(alive); } catch (_) {}
    finally {
      pending = false;
      if (!stopped && !env.document.hidden) timer = env.setTimeout(run, interval);
    }
  };
  const visibility = () => { env.clearTimeout(timer); if (!env.document.hidden) run(); };
  env.document.addEventListener('visibilitychange', visibility);
  run();
  return () => { stopped = true; env.clearTimeout(timer); env.document.removeEventListener('visibilitychange', visibility); };
}

export function useVisibleRefresh(task, interval, key = '') {
  const latest = useRef(task); latest.current = task;
  useEffect(() => visibleRefresh(alive => latest.current(alive), interval), [interval, key]);
}
