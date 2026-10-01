// Point d'entrée des dépendances : Preact + hooks + htm (vendored, sans build).
import { h, render, Fragment, createContext } from 'preact';
import { useState, useEffect, useLayoutEffect, useRef, useMemo, useCallback, useContext, useReducer } from 'preact/hooks';
import htm from 'htm';

export const html = htm.bind(h);
export { h, render, Fragment, createContext, useState, useEffect, useLayoutEffect, useRef, useMemo, useCallback, useContext, useReducer };

// Store minimal : un objet d'état, des abonnés, des mises à jour groupées par
// frame. Les composants s'abonnent via useStore(sélecteur) et ne se redessinent
// que si la valeur sélectionnée change.
export function createStore(initial) {
  let state = initial;
  const subs = new Set();
  let queued = false;
  const flush = () => { queued = false; subs.forEach(fn => fn()); };
  return {
    get: () => state,
    set(patch) {
      state = { ...state, ...(typeof patch === 'function' ? patch(state) : patch) };
      if (!queued) { queued = true; requestAnimationFrame(flush); }
    },
    subscribe(fn) { subs.add(fn); return () => subs.delete(fn); },
  };
}

export function useStore(store, select = s => s) {
  const [, force] = useReducer(x => x + 1, 0);
  const ref = useRef(); ref.current = select;
  const val = useRef(select(store.get()));
  useEffect(() => store.subscribe(() => {
    const next = ref.current(store.get());
    if (!Object.is(next, val.current)) { val.current = next; force(); }
  }), [store]);
  val.current = select(store.get());
  return val.current;
}

export const cls = (...xs) => xs.filter(Boolean).join(' ');
export const fmtTok = n => { n = Math.max(0, Math.round(n || 0)); return n < 1000 ? String(n) : (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K'; };
export const fmtBytes = b => { if (!b) return '—'; const g = b / 1073741824; return g >= 1 ? g.toFixed(1) + ' Go' : (b / 1048576).toFixed(0) + ' Mo'; };
export const fmtSecs = s => { s = Math.max(0, Math.round(s)); const h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), r = s % 60; return h ? h + 'h ' + String(m).padStart(2, '0') + 'm' : m ? m + 'm ' + String(r).padStart(2, '0') + 's' : r + 's'; };
export const baseName = p => String(p || '').split(/[\\/]/).pop();
export const debounce = (fn, ms) => { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; };
