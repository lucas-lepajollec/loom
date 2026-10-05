// Private observations survive route changes, never a browser restart. Clear
// them on authentication changes; do not persist account data in localStorage.
const snapshots = new Map();
export const recall = key => snapshots.get(key) ?? null;
export const remember = (key, value) => { snapshots.set(key, value); return value; };
export const clearObservations = () => snapshots.clear();
