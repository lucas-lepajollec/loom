// One read at a time. Pause while hidden and refresh when the page is visible
// again. Late responses cannot update an unmounted page.
export function visibleRefresh(task, interval, env = globalThis) {
  let stopped = false, pending = false, resume = false, timer;
  const alive = () => !stopped;
  const run = async () => {
    env.clearTimeout(timer);
    if (stopped || pending || env.document.hidden) return;
    pending = true;
    try { await task(alive); } catch (_) {}
    finally {
      pending = false;
      if (!stopped && !env.document.hidden) {
        if (resume) { resume = false; run(); }
        else timer = env.setTimeout(run, interval);
      }
    }
  };
  const visibility = () => {
    env.clearTimeout(timer);
    if (env.document.hidden) { resume = false; return; }
    if (pending) resume = true; else { resume = false; run(); }
  };
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

// Reattach observation transports only. Callers never resend user input or
// generation on reconnect. One owner gates callbacks and cancels stale setup.
export function visibleConnection(connect, delay = 1000, env = globalThis) {
  let stopped = false, paused = false, owner = null, timer;
  const available = () => !stopped && !paused && !env.document.hidden && env.navigator?.onLine !== false;
  const close = () => {
    env.clearTimeout(timer);
    const old = owner; owner = null;
    if (old) { old.controller.abort(); old.cleanup?.(); }
  };
  const run = () => {
    env.clearTimeout(timer);
    if (!available() || owner) return;
    const current = { controller: new env.AbortController(), cleanup: null };
    owner = current;
    const alive = () => owner === current && available();
    const retry = () => {
      if (!alive()) return;
      close();
      if (available()) timer = env.setTimeout(run, delay);
    };
    try {
      Promise.resolve(connect({ signal: current.controller.signal, alive, retry })).then(cleanup => {
        if (owner === current) current.cleanup = cleanup;
        else cleanup?.();
      }, retry);
    } catch (_) { retry(); }
  };
  const suspend = () => { paused = true; close(); };
  const resume = () => { paused = false; close(); run(); };
  const visibility = () => { if (env.document.hidden) suspend(); else resume(); };
  env.document.addEventListener('visibilitychange', visibility);
  env.addEventListener?.('pagehide', suspend);
  env.addEventListener?.('offline', suspend);
  env.addEventListener?.('pageshow', resume);
  env.addEventListener?.('online', resume);
  run();
  return () => {
    stopped = true; close();
    env.document.removeEventListener('visibilitychange', visibility);
    env.removeEventListener?.('pagehide', suspend); env.removeEventListener?.('offline', suspend);
    env.removeEventListener?.('pageshow', resume); env.removeEventListener?.('online', resume);
  };
}
