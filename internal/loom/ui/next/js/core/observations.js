import { validData } from './shape.js';
// Private observations survive route changes, never a browser restart. Clear
// them on authentication changes; do not persist account data in localStorage.
const snapshots = new Map();
export const recall = key => snapshots.get(key) ?? null;
export const remember = (key, value) => {
  const url = key === 'usage' ? '/api/usage' : key === 'balances' ? '/api/usage/providers' : '/api/usage/native';
  const data = key === 'usage' ? value : { [key === 'balances' ? 'providers' : 'harnesses']: value };
  if (validData(data, url)) snapshots.set(key, value);
  return recall(key);
};
export const clearObservations = () => snapshots.clear();
