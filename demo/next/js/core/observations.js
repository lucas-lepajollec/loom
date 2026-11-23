import { validData } from './shape.js';
// Private observations survive route changes, never a browser restart. Clear
// them on authentication changes; do not persist account data in localStorage.
const snapshots = new Map();
export const recall = key => snapshots.get(key) ?? null;
export const remember = (key, value) => {
  if (key !== 'usage' && key !== 'balances') return recall(key);
  const url = key === 'usage' ? '/api/usage' : '/api/usage/providers';
  const data = key === 'usage' ? value : { providers: value };
  if (validData(data, url)) snapshots.set(key, value);
  return recall(key);
};
export const clearObservations = () => snapshots.clear();
