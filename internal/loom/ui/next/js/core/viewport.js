import { useEffect, useState } from './lib.js';

export const phoneQuery = '(max-width: 639px)';
export const isPhone = () => globalThis.matchMedia?.(phoneQuery).matches === true;

export const usePhone = () => useMedia(phoneQuery);

export function useMedia(query) {
  const [matches, setMatches] = useState(() => matchMedia(query).matches);
  useEffect(() => {
    const media = matchMedia(query);
    const update = () => setMatches(media.matches);
    update();
    media.addEventListener('change', update);
    return () => media.removeEventListener('change', update);
  }, [query]);
  return matches;
}
