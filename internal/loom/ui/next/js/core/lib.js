// Point d'entrée des dépendances : Preact + hooks + htm (vendored, sans build).
import { h as preactH, render, Fragment, createContext } from 'preact';
import { useState, useEffect, useLayoutEffect, useRef, useMemo, useCallback, useContext, useReducer } from 'preact/hooks';
import htm from 'htm';
import { language, t } from './i18n.js';
import { selectedEqual } from './equal.js';

export function useLang() {
  const [, update] = useReducer(n => n + 1, 0);
  const lang = language.get();
  useLayoutEffect(() => {
    const unsubscribe = language.subscribe(update);
    if (language.get() !== lang) update();
    return unsubscribe;
  }, []);
  return lang;
}

// Subscribe each functional component, including independently mounted drawers.
// Stable wrappers keep component identity, drafts, focus and hook state intact.
const translatedComponents = new WeakMap();
export function h(type, props, ...children) {
  if (typeof type === 'function' && type !== Fragment && !type.prototype?.render) {
    if (!translatedComponents.has(type)) {
      const Component = type;
      const Translated = props => { useLang(); return Component(props); };
      Translated.displayName = type.displayName || type.name;
      translatedComponents.set(type, Translated);
    }
    type = translatedComponents.get(type);
  }
  return preactH(type, props, ...children);
}
export const html = htm.bind(h);
export { render, Fragment, createContext, useState, useEffect, useLayoutEffect, useRef, useMemo, useCallback, useContext, useReducer };

// Store minimal : un objet d'état, des abonnés, des mises à jour groupées par
// frame. Les composants s'abonnent via useStore(sélecteur) et ne se redessinent
// que si la valeur sélectionnée change.
export function createStore(initial) {
  let state = initial;
  const subs = new Set();
  let queued = false, frame, timer;
  const flush = () => {
    if (!queued) return;
    queued = false; cancelAnimationFrame(frame); clearTimeout(timer);
    subs.forEach(fn => fn());
  };
  return {
    get: () => state,
    set(patch) {
      state = { ...state, ...(typeof patch === 'function' ? patch(state) : patch) };
      if (!queued) {
        queued = true; frame = requestAnimationFrame(flush);
        // A suspended/throttled animation frame must not hold all UI updates.
        timer = setTimeout(flush, 100);
      }
    },
    subscribe(fn) { subs.add(fn); return () => subs.delete(fn); },
  };
}

export function useStore(store, select = s => s) {
  const [, force] = useReducer(x => x + 1, 0);
  const ref = useRef(); ref.current = select;
  const val = useRef(select(store.get()));
  useLayoutEffect(() => {
    const sync = () => {
      const next = ref.current(store.get());
      if (!selectedEqual(next, val.current)) { val.current = next; force(); }
    };
    const unsubscribe = store.subscribe(sync);
    sync();
    return unsubscribe;
  }, [store]);
  val.current = select(store.get());
  return val.current;
}

export const cls = (...xs) => xs.filter(Boolean).join(' ');
export const fmtTok = n => { n = Math.max(0, Math.round(n || 0)); const u = n >= 1e9 ? [1e9, 'B'] : n >= 1e6 ? [1e6, 'M'] : n >= 1e3 ? [1e3, 'K'] : null; return u ? (n / u[0]).toFixed(1).replace(/\.0$/, '') + u[1] : String(n); };
export const fmtBytes = b => { if (!b) return '—'; const g = b / 1073741824; return g >= 1 ? g.toFixed(1) + t('common.units.gb') : (b / 1048576).toFixed(0) + t('common.units.mb'); };
export const fmtSecs = s => { s = Math.max(0, Math.round(s)); const h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), r = s % 60; return h ? h + 'h ' + String(m).padStart(2, '0') + 'm' : m ? m + 'm ' + String(r).padStart(2, '0') + 's' : r + 's'; };
export const baseName = p => String(p || '').split(/[\\/]/).pop();
export const debounce = (fn, ms) => { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; };
