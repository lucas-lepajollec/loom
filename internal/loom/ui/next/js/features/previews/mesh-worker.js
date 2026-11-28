// Parsing and normalization can be interrupted by terminating the worker when
// switching artifacts. Only the owning view resolves authenticated file paths.
import { parseOBJ, parsePLY, parseSTL } from './mesh.js';
import { parseGLTF } from './gltf.js';

let acceptSibling;
const parsers = { obj: parseOBJ, ply: parsePLY, stl: parseSTL };
self.onmessage = async ({ data }) => {
  if (acceptSibling) {
    acceptSibling(data.bytes);
    acceptSibling = null;
    return;
  }
  try {
    const readSibling = (path, size) =>
      new Promise((resolve) => {
        acceptSibling = resolve;
        self.postMessage({ path, size });
      });
    const mesh = Object.hasOwn(parsers, data.ext)
      ? parsers[data.ext](data.bytes)
      : await parseGLTF(data.bytes, readSibling);
    const center = mesh.min.map((v, i) => (v + mesh.max[i]) / 2);
    const radius = Math.hypot(...mesh.max.map((v, i) => v - center[i])) || 1;
    mesh.halfSize = mesh.max.map((v, i) => (v - center[i]) / radius);
    for (let i = 0; i < mesh.positions.length; i++)
      mesh.positions[i] = (mesh.positions[i] - center[i % 3]) / radius;
    self.postMessage({ mesh }, [mesh.positions.buffer, mesh.indices.buffer]);
  } catch {
    self.postMessage({ failed: true });
  }
};
