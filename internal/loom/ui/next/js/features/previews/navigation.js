export const viewportPresets = {
  auto: null,
  desktop: [1280, 800],
  tablet: [768, 1024],
  mobile: [390, 844],
  compact: [320, 568],
};

export function viewportSize(preset, rotate) {
  const size = viewportPresets[preset];
  if (!size) return null;
  return rotate ? [size[1], size[0]] : [...size];
}

export function appPath(raw) {
  if (!raw.startsWith('/') || raw.startsWith('//') || /[\\\u0000-\u001f]/.test(raw)) return '';
  try {
    const u = new URL(raw, 'http://preview.invalid');
    if (
      u.origin !== 'http://preview.invalid' ||
      u.pathname.startsWith('//') ||
      u.pathname.startsWith('/__loom_preview/')
    )
      return '';
    return u.pathname + u.search + u.hash;
  } catch {
    return '';
  }
}

export function fileQuery(item, session, path = item.path) {
  const absolute = path.startsWith('/');
  const q = new URLSearchParams({ path: absolute ? path.slice(1) : path, machine: item.target || 'local' });
  if (absolute || session.workingDir) q.set('scope', 'machine');
  if (!absolute && session.workingDir) q.set('dir', session.workingDir);
  return q;
}
