import { useEffect, useState } from './lib.js';

const phoneQuery = '(max-width: 760px)';
export const isPhone = () => globalThis.matchMedia?.(phoneQuery).matches === true;

export function usePhone() {
  const [phone, setPhone] = useState(isPhone);
  useEffect(() => {
    const media = matchMedia(phoneQuery);
    const update = () => setPhone(media.matches);
    media.addEventListener('change', update);
    return () => media.removeEventListener('change', update);
  }, []);
  return phone;
}
