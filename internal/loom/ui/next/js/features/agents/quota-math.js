// Calculs des quotas à partir des relevés de Loom (un relevé toutes les cinq
// minutes : { at, <compte>: { <limite>: { used, reset_at } } }). Sans DOM :
// testés seuls.

export const H = 3600e3, DAY = 24 * H;
export const SPANS = { '5h': 5 * H, '7d': 7 * DAY, month: 30 * DAY };
// Deux relevés d'une même fenêtre peuvent annoncer une remise à zéro décalée
// de quelques secondes : au-delà de dix minutes, c'est une autre fenêtre.
const SAME = 10 * 60e3;
const sameWindow = (a, b) => Math.abs((a || 0) - (b || 0)) <= SAME;

// Fenêtre en cours d'une limite : ses relevés depuis le début, le rythme
// récent (dernière heure pour 5 h, dernier jour sinon) et ce qu'il annonce :
// épuisement avant la remise à zéro (eta), ou part atteinte à la remise à
// zéro (atReset). null quand aucune fenêtre n'est ouverte.
export function currentWindow(samples, acc, key, limit, now = Date.now()) {
  const span = SPANS[limit.window];
  if (!span || !limit.reset_at || limit.reset_at <= now) return null;
  const end = limit.reset_at, start = end - span;
  const pts = [];
  for (const s of samples) {
    const w = s[acc] && s[acc][key];
    if (w && s.at >= start && s.at < now && sameWindow(w.reset_at, end)) pts.push([s.at, w.used]);
  }
  pts.sort((a, b) => a[0] - b[0]);
  pts.push([now, limit.used]);
  const look = span <= 5 * H ? H : DAY;
  const ref = pts.find(p => p[0] >= now - look);
  let rate = ref && now - ref[0] >= 15 * 60e3 ? (limit.used - ref[1]) / (now - ref[0]) : limit.used / Math.max(now - start, 60e3);
  rate = Math.max(0, rate);
  let eta = null, atReset = limit.used;
  if (limit.used >= 100) eta = now;
  else if (rate > 0) {
    const full = now + (100 - limit.used) / rate;
    if (full < end) eta = full;
    else atReset = Math.min(100, limit.used + rate * (end - now));
  }
  return { start, end, pts, used: limit.used, rate, eta, atReset, expected: Math.min(100, ((now - start) / span) * 100) };
}

const midnight = (y, m, d) => new Date(y, m, d).getTime();

// Part de la limite consommée chaque jour (en points de pourcentage), sur
// les `days` derniers jours, du plus ancien au plus récent. Une remise à zéro
// entre deux relevés compte la nouvelle consommation depuis zéro.
export function dailyUse(samples, acc, key, days = 14, now = Date.now()) {
  const today = new Date(now);
  const list = [];
  for (let i = days - 1; i >= 0; i--) {
    const from = midnight(today.getFullYear(), today.getMonth(), today.getDate() - i);
    list.push({ day: from, to: midnight(today.getFullYear(), today.getMonth(), today.getDate() - i + 1), used: 0 });
  }
  let prev = null;
  for (const s of [...samples].sort((a, b) => a.at - b.at)) {
    const w = s[acc] && s[acc][key];
    if (!w) continue;
    if (prev) {
      let d = 0;
      if (sameWindow(w.reset_at, prev.w.reset_at)) d = w.used - prev.w.used;
      else if (prev.w.reset_at && prev.w.reset_at <= s.at) d = w.used;
      const slot = list.find(x => s.at >= x.day && s.at < x.to);
      if (d > 0 && slot) slot.used += Math.min(100, d);
    }
    prev = { at: s.at, w };
  }
  return list.map(({ day, used }) => ({ day, used: Math.round(used * 10) / 10 }));
}
