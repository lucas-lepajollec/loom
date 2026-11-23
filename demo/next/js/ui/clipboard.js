// Copie dans le presse-papiers, y compris sur une adresse http du réseau local
// (où l'API presse-papiers du navigateur est refusée).
export async function copyText(text) {
  try { await navigator.clipboard.writeText(text); return true; } catch (_) {}
  // Adresse http sur le réseau local : l'API presse-papiers est refusée.
  const ta = document.createElement('textarea');
  ta.value = text; ta.style.position = 'fixed'; ta.style.opacity = '0';
  document.body.appendChild(ta); ta.select();
  let ok = false;
  try { ok = document.execCommand('copy'); } catch (_) {}
  ta.remove();
  return ok;
}
