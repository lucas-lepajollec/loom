// A renderer owns matching, its pill symbol and its view. Load heavy viewers
// only when an artifact is selected. New registrations take precedence.
const registry = [];
export function registerPreviewRenderer(renderer) {
  if (!renderer.id || typeof renderer.matches !== 'function' || typeof renderer.load !== 'function')
    throw new TypeError('Invalid preview renderer');
  if (registry.some((r) => r.id === renderer.id)) throw new Error('Duplicate preview renderer');
  registry.unshift(renderer);
  return () => {
    const index = registry.indexOf(renderer);
    if (index >= 0) registry.splice(index, 1);
  };
}
export const matchRenderer = (item) => registry.find((r) => r.matches(item));
const files = (name) => () => import('./file-views.js').then((module) => module[name]);
function file(id, icon, extensions, load, readsText = false) {
  registerPreviewRenderer({
    id,
    icon,
    load,
    readsText,
    matches: (item) => item.kind === 'file' && (!extensions || extensions.test(item.path)),
  });
}
file('code', '📄', null, files('TextView'), true);
file('markdown', '📄', /\.(?:md|markdown|mdx)$/i, files('TextView'), true);
file('html', '🌐', /\.html?$/i, files('TextView'), true);
file('pdf', '📄', /\.pdf$/i, files('PDFView'));
file('media', '▶', /\.(?:mp4|m4v|webm|mov|mp3|wav|ogg|flac|m4a)$/i, files('MediaView'));
file('image', '🖼', /\.(?:png|jpe?g|webp|gif|svg|avif|bmp|ico)$/i, files('ImageView'));
file('model3d', '🧊', /\.(?:obj|stl|ply|gltf|glb)$/i, () => import('./viewer3d.js').then((m) => m.ModelViewer));
registerPreviewRenderer({
  id: 'web',
  icon: '🌐',
  matches: (item) => item.kind === 'port',
  load: () => import('./web-view.js').then((m) => m.WebView),
});
