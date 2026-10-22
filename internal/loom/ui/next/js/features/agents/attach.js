// Pièces jointes d'un prompt : images collées, fichiers déposés ou choisis.
// Codeman les range dans le dossier de la session (.codeman-uploads) et rend
// leur chemin, que le prompt mentionne : le CLI lit le fichier lui-même.
import { t } from '../../core/i18n.js';
import { api } from './client.js';

// Fichiers d'un collage ou d'un dépôt. Une capture d'écran collée arrive sans
// nom utile : elle reçoit un nom daté pour rester lisible dans le prompt.
export function filesOf(data) {
  if (!data) return [];
  const out = [];
  for (const item of data.items || []) {
    if (item.kind !== 'file') continue;
    const f = item.getAsFile();
    if (f) out.push(f);
  }
  if (!out.length) for (const f of data.files || []) out.push(f);
  return out.map(f => {
    if (f.name && !/^image\.(png|jpe?g|gif|webp)$/i.test(f.name)) return f;
    const ext = (f.type.split('/')[1] || 'png').replace('jpeg', 'jpg').replace(/[^a-z0-9]/gi, '');
    const stamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '');
    return new File([f], `capture-${stamp}.${ext}`, { type: f.type || 'image/png' });
  });
}
export const hasFiles = e => [...(e.dataTransfer?.types || [])].includes('Files');

// Les chemins contenant un espace sont mis entre guillemets pour le CLI.
export const quotePath = p => (/\s/.test(p) ? `"${p}"` : p);

export async function uploadFiles(sessionId, files) {
  const paths = [];
  for (const f of files) {
    const r = await api.upload(sessionId, f);
    if (!r?.path) throw new Error(t('agents.attach.failed', { name: f.name }));
    paths.push(r.path);
  }
  return paths;
}
