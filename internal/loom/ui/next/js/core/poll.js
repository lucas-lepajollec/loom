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
  env.addEventListener?.('pageshow', visibility);
  env.addEventListener?.('online', visibility);
  run();
  return () => { stopped = true; env.clearTimeout(timer); env.document.removeEventListener('visibilitychange', visibility); env.removeEventListener?.('pageshow', visibility); env.removeEventListener?.('online', visibility); };
}

// Manual refreshes and periodic observations share one in-flight read.
export function singleFlight(task) {
  let pending;
  return (...args) => {
    if (!pending) pending = Promise.resolve().then(() => task(...args)).finally(() => { pending = null; });
    return pending;
  };
}
