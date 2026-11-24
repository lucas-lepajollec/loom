import { useEffect, useState } from './lib.js';

const phoneQuery = '(max-width: 760px)';
export const isPhone = () => globalThis.matchMedia?.(phoneQuery).matches === true;

export function usePhone() {
  return useMedia(phoneQuery);
}

export function useMedia(query) {
  const [matches, setMatches] = useState(() => matchMedia(query).matches);
  useEffect(() => {
    const media = matchMedia(query);
    const update = () => setMatches(media.matches);
    media.addEventListener('change', update);
    return () => media.removeEventListener('change', update);
  }, [query]);
  return matches;
}
