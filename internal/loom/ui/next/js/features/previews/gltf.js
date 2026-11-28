import { check, meshBuilder, MAX_VERTICES } from './mesh.js';

const identity = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];
function transform(m, x, y, z, w = 1) {
  return [0, 1, 2, 3].map((i) => m[i] * x + m[i + 4] * y + m[i + 8] * z + m[i + 12] * w);
}
function nodeMatrix(node) {
  if (node.matrix) {
    check(node.matrix.length === 16);
    return node.matrix;
  }
  const [x, y, z, w] = node.rotation || [0, 0, 0, 1];
  const [a, b, c] = node.scale || [1, 1, 1];
  return [
    a * (1 - 2 * y * y - 2 * z * z),
    a * (2 * x * y + 2 * z * w),
    a * (2 * x * z - 2 * y * w),
    0,
    b * (2 * x * y - 2 * z * w),
    b * (1 - 2 * x * x - 2 * z * z),
    b * (2 * y * z + 2 * x * w),
    0,
    c * (2 * x * z + 2 * y * w),
    c * (2 * y * z - 2 * x * w),
    c * (1 - 2 * x * x - 2 * y * y),
    0,
    ...(node.translation || [0, 0, 0]),
    1,
  ];
}

export async function parseGLTF(bytes, readSibling) {
  const decoder = new TextDecoder(),
    dv = new DataView(bytes);
  let json, bin;
  if (bytes.byteLength >= 12 && dv.getUint32(0, true) === 0x46546c67) {
    check(dv.getUint32(4, true) === 2 && dv.getUint32(8, true) === bytes.byteLength);
    for (let i = 12; i < bytes.byteLength; ) {
      const size = dv.getUint32(i, true),
        type = dv.getUint32(i + 4, true);
      i += 8;
      check(i + size <= bytes.byteLength);
      if (type === 0x4e4f534a) json = JSON.parse(decoder.decode(bytes.slice(i, i + size)));
      if (type === 0x004e4942) bin = bytes.slice(i, i + size);
      i += size;
    }
  } else json = JSON.parse(decoder.decode(bytes));
  check(json?.asset?.version === '2.0');
  let budget = 32 * 1024 * 1024;
  const buffers = [];
  for (const { uri, byteLength } of json.buffers) {
    check(buffers.length < 64 && Number.isSafeInteger(byteLength) && byteLength > 0 && byteLength <= budget);
    let buffer = bin;
    if (uri != null) {
      if (/^data:application\/(?:octet-stream|gltf-buffer);base64,/i.test(uri)) {
        buffer = Uint8Array.from(atob(uri.slice(uri.indexOf(',') + 1)), (c) => c.charCodeAt(0)).buffer;
      } else {
        const path = decodeURIComponent(uri);
        check(
          readSibling && !/[:?#%\\\x00-\x1f]/.test(path) && path.split('/').every((s) => s && !s.startsWith('.'))
        );
        buffer = await readSibling(path, byteLength);
      }
    }
    check(buffer && buffer.byteLength >= byteLength && buffer.byteLength <= byteLength + 3);
    budget -= byteLength;
    buffers.push(buffer);
  }
  function accessor(index, type) {
    const a = json.accessors[index],
      v = json.bufferViews[a.bufferView];
    check(
      a.type === type && !a.sparse && Number.isSafeInteger(a.count) && a.count > 0 && a.count <= MAX_VERTICES * 3
    );
    const methods = {
      5121: ['getUint8', 1],
      5123: ['getUint16', 2],
      5125: ['getUint32', 4],
      5126: ['getFloat32', 4],
    };
    check(Object.hasOwn(methods, a.componentType) && (type === 'VEC3') === (a.componentType === 5126));
    const [method, size] = methods[a.componentType],
      width = type === 'VEC3' ? 3 : 1;
    const view = new DataView(buffers[v.buffer], v.byteOffset ?? 0, v.byteLength);
    const start = a.byteOffset ?? 0,
      stride = v.byteStride ?? size * width;
    check(
      Number.isSafeInteger(start) &&
        start >= 0 &&
        Number.isSafeInteger(stride) &&
        stride >= size * width &&
        start + (a.count - 1) * stride + size * width <= view.byteLength
    );
    return { count: a.count, get: (i, k = 0) => view[method](start + i * stride + k * size, true) };
  }
  const mesh = meshBuilder();
  function addMesh(index, matrix) {
    for (const p of json.meshes[index].primitives) {
      const pos = accessor(p.attributes.POSITION, 'VEC3'),
        base = mesh.count;
      for (let i = 0; i < pos.count; i++)
        mesh.vertex(...transform(matrix, pos.get(i), pos.get(i, 1), pos.get(i, 2)));
      const ids = p.indices == null ? { count: pos.count, get: (i) => i } : accessor(p.indices, 'SCALAR');
      const id = (i) => {
        const n = ids.get(i);
        check(n < pos.count);
        return base + n;
      };
      const mode = p.mode ?? 4;
      if (mode === 0) continue;
      check([4, 5, 6].includes(mode) && (mode !== 4 || ids.count % 3 === 0));
      for (let i = 2; i < ids.count; i += mode === 4 ? 3 : 1) {
        const flip = mode === 5 && i % 2;
        mesh.triangle(id(mode === 6 ? 0 : i - (flip ? 1 : 2)), id(i - (flip ? 2 : 1)), id(i));
      }
    }
  }
  const seen = new Set();
  function visit(i, parent, depth) {
    check(depth < 256 && !seen.has(i));
    seen.add(i);
    const node = json.nodes[i],
      local = nodeMatrix(node);
    const world = [0, 4, 8, 12].flatMap((k) => transform(parent, ...local.slice(k, k + 4)));
    if (node.mesh != null) addMesh(node.mesh, world);
    for (const child of node.children || []) visit(child, world, depth + 1);
  }
  if (json.scenes?.length) {
    for (const root of json.scenes[json.scene ?? 0].nodes || []) visit(root, identity, 0);
  } else json.meshes.forEach((_, i) => addMesh(i, identity));
  return mesh.finish();
}
