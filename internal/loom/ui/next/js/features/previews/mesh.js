// Mesh data stays in file coordinates until the viewer fits its camera.
export const MAX_VERTICES = 1_500_000;
const MAX_INDICES = 4_500_000;
export function check(ok) {
  if (!ok) throw new RangeError('Invalid or unsupported mesh');
}
export function meshBuilder() {
  const positions = [],
    indices = [],
    min = [Infinity, Infinity, Infinity],
    max = [-Infinity, -Infinity, -Infinity];
  function vertex(x, y, z) {
    check(positions.length < MAX_VERTICES * 3);
    for (const [axis, v] of [x, y, z].entries()) {
      check(Number.isFinite(Math.fround(v)));
      min[axis] = Math.min(min[axis], v);
      max[axis] = Math.max(max[axis], v);
    }
    positions.push(x, y, z);
  }
  function triangle(a, b, c) {
    check(indices.length < MAX_INDICES);
    for (const i of [a, b, c]) check(Number.isSafeInteger(i) && i >= 0);
    indices.push(a, b, c);
  }
  function face(ids) {
    check(ids.length >= 3);
    for (let i = 2; i < ids.length; i++) triangle(ids[0], ids[i - 1], ids[i]);
  }
  function finish() {
    check(positions.length > 0 && indices.every((i) => i < positions.length / 3));
    return { positions: Float32Array.from(positions), indices: Uint32Array.from(indices), min, max };
  }
  return {
    vertex,
    triangle,
    face,
    finish,
    get count() {
      return positions.length / 3;
    },
  };
}

const decoder = new TextDecoder();
export function parseOBJ(bytes) {
  const mesh = meshBuilder();
  for (const raw of decoder.decode(bytes).split(/\r?\n/)) {
    const [op, ...fields] = raw.split('#', 1)[0].trim().split(/\s+/);
    if (op === 'v') mesh.vertex(...fields.slice(0, 3).map(Number));
    if (op === 'f')
      mesh.face(
        fields.map((f) => {
          const n = Number(f.split('/', 1)[0]);
          return n < 0 ? mesh.count + n : n - 1;
        })
      );
  }
  return mesh.finish();
}

export function parseSTL(bytes) {
  const mesh = meshBuilder(),
    view = new DataView(bytes);
  const faces = bytes.byteLength >= 84 ? view.getUint32(80, true) : 0;
  if (faces && 84 + faces * 50 === bytes.byteLength) {
    check(faces * 3 <= MAX_VERTICES);
    for (let i = 0; i < faces; i++) {
      const base = 96 + i * 50;
      for (let j = 0; j < 36; j += 12) {
        mesh.vertex(...[0, 4, 8].map((offset) => view.getFloat32(base + j + offset, true)));
      }
      mesh.triangle(i * 3, i * 3 + 1, i * 3 + 2);
    }
  } else {
    for (const v of decoder.decode(bytes).matchAll(/\bvertex\s+(\S+)\s+(\S+)\s+(\S+)/gi)) {
      mesh.vertex(Number(v[1]), Number(v[2]), Number(v[3]));
      if (mesh.count % 3 === 0) mesh.triangle(mesh.count - 3, mesh.count - 2, mesh.count - 1);
    }
    check(mesh.count % 3 === 0);
  }
  return mesh.finish();
}

const plyTypes = Object.fromEntries(
  [
    ['char int8', 'getInt8', 1],
    ['uchar uint8', 'getUint8', 1],
    ['short int16', 'getInt16', 2],
    ['ushort uint16', 'getUint16', 2],
    ['int int32', 'getInt32', 4],
    ['uint uint32', 'getUint32', 4],
    ['float float32', 'getFloat32', 4],
    ['double float64', 'getFloat64', 8],
  ].flatMap(([names, method, width]) => names.split(' ').map((name) => [name, [method, width]]))
);

export function parsePLY(bytes) {
  const header = decoder.decode(bytes.slice(0, 65536)).match(/^ply\r?\n([\s\S]*?^end_header\r?\n)/m);
  check(header && header.index === 0);
  let offset = new TextEncoder().encode(header[0]).length;
  const elements = [],
    mesh = meshBuilder();
  let format;
  for (const line of header[1].split(/\r?\n/)) {
    const [op, ...fields] = line.trim().split(/\s+/);
    if (op === 'format') format = fields[0];
    if (op === 'element') {
      const count = Number(fields[1]);
      check(Number.isSafeInteger(count) && count >= 0 && count <= MAX_VERTICES);
      elements.push({ name: fields[0], count, props: [] });
    }
    if (op === 'property') elements.at(-1).props.push(fields);
  }
  check(['ascii', 'binary_little_endian', 'binary_big_endian'].includes(format));
  const view = new DataView(bytes);
  const tokens = format === 'ascii' ? decoder.decode(bytes.slice(offset)).matchAll(/\S+/g) : null;
  function read(type) {
    check(Object.hasOwn(plyTypes, type));
    if (tokens) {
      const value = Number(tokens.next().value?.[0]);
      check(Number.isFinite(value));
      return value;
    }
    const [method, width] = plyTypes[type];
    const value = view[method](offset, format === 'binary_little_endian');
    offset += width;
    return value;
  }
  for (const { name, count, props } of elements) {
    check(props.length > 0);
    for (let row = 0; row < count; row++) {
      const point = [];
      for (const [type, field, itemType, listName] of props) {
        if (type === 'list') {
          const n = read(field);
          check(Number.isSafeInteger(n) && n >= 0 && n <= MAX_VERTICES);
          const ids = [];
          for (let i = 0; i < n; i++) ids.push(read(itemType));
          if (name === 'face' && ['vertex_indices', 'vertex_index'].includes(listName)) mesh.face(ids);
        } else {
          const v = read(type),
            axis = ['x', 'y', 'z'].indexOf(field);
          if (axis >= 0) point[axis] = v;
        }
      }
      if (name === 'vertex') mesh.vertex(...point);
    }
  }
  return mesh.finish();
}
