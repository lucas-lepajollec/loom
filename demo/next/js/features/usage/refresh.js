import { useEffect, useRef } from '../../core/lib.js';
import { visibleRefresh } from '../../core/poll.js';
export { visibleRefresh } from '../../core/poll.js';

export function useVisibleRefresh(task, interval, key = '') {
  const latest = useRef(task); latest.current = task;
  useEffect(() => visibleRefresh(alive => latest.current(alive), interval), [interval, key]);
}
