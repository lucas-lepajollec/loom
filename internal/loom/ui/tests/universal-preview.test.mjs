import test from 'node:test';
import assert from 'node:assert/strict';
import { matchRenderer, registerPreviewRenderer } from '../next/js/features/previews/renderers.js';
import { appPath, viewportSize, fileQuery } from '../next/js/features/previews/navigation.js';
import {
  extractPreviewPortsFromText,
  extractUrlsFromText,
  extractMentionedFiles,
} from '../next/js/features/agents/panel-helpers.js';
import { parseOBJ, parseSTL, parsePLY } from '../next/js/features/previews/mesh.js';
import { parseGLTF } from '../next/js/features/previews/gltf.js';

const bytes = (s) => new TextEncoder().encode(s).buffer;
test('registry chooses renderers for uppercase, multi-dot and unknown extensions and supports new renderers', () => {
  assert.equal(matchRenderer({ kind: 'port', port: 5173 }).id, 'web');
  for (const [id, extensions] of Object.entries({
    model3d: 'OBJ stl ply gltf glb',
    image: 'png jpg jpeg webp gif svg avif bmp ico',
    media: 'mp4 webm mov m4v mp3 wav ogg flac m4a',
    pdf: 'pdf',
    html: 'html htm',
    markdown: 'md mdx markdown',
    code: 'js go xyz',
  }))
    for (const ext of extensions.split(' '))
      assert.equal(matchRenderer({ kind: 'file', path: 'dir/a.v2.' + ext }).id, id);
  const next = { id: 'custom', matches: (item) => item.path === 'custom.xyz', load: async () => () => null };
  const remove = registerPreviewRenderer(next);
  assert.equal(matchRenderer({ kind: 'file', path: 'custom.xyz' }), next);
  assert.throws(() => registerPreviewRenderer(next));
  remove();
  assert.equal(matchRenderer({ kind: 'file', path: 'custom.xyz' }).id, 'code');
});

test('only loopback HTTP authorities produce port hints; ANSI and IPv6 preserve paths', () => {
  const text =
    '\x1b[32mhttp://localhost:5173/docs?q=a#heading\x1b[0m http://[::1]:8080/ http://127.0.0.1:5173. http://localhost:80 http://localhost:65536 http://localhost:4444@evil.test http://other:8080/';
  assert.deepEqual(extractPreviewPortsFromText(text), [5173, 8080]);
  assert.deepEqual(extractUrlsFromText(text), [
    'http://localhost:5173/docs?q=a#heading',
    'http://[::1]:8080/',
    'http://127.0.0.1:5173/',
  ]);
  assert.deepEqual(extractMentionedFiles([{ answer: 'Open scene.glb and assets/render.png' }]), [
    'scene.glb',
    'assets/render.png',
  ]);
});

test('viewport rotation and navigation stay inside the selected app or session directory', () => {
  assert.equal(viewportSize('auto', true), null);
  for (const [preset, w, h] of [
    ['desktop', 1280, 800],
    ['tablet', 768, 1024],
    ['mobile', 390, 844],
    ['compact', 320, 568],
  ]) {
    assert.deepEqual(viewportSize(preset, false), [w, h]);
    assert.deepEqual(viewportSize(preset, true), [h, w]);
  }
  assert.equal(appPath('/a/../b?q=1#x'), '/b?q=1#x');
  for (const path of [
    '//another.test',
    '/\\another.test',
    'https://another.test',
    '/a/..//other',
    '/__loom_preview/open',
  ])
    assert.equal(appPath(path), '');
  const q = fileQuery({ path: 'images/a #.png', target: 'studio' }, { workingDir: '/repo' });
  assert.deepEqual(Object.fromEntries(q), {
    path: 'images/a #.png',
    machine: 'studio',
    scope: 'machine',
    dir: '/repo',
  });
});

test('OBJ triangulates negative-index polygons and rejects missing vertices', () => {
  const mesh = parseOBJ(bytes('v 0 0 0\nv 3 0 0\nv 3 2 0\nv 0 2 0\nf -4/1 -3/2 -2/3 -1/4'));
  assert.deepEqual([...mesh.indices], [0, 1, 2, 0, 2, 3]);
  assert.deepEqual(mesh.max, [3, 2, 0]);
  assert.throws(() => parseOBJ(bytes('v 0 0 0\nf 1 2 3')));
});
test('STL accepts both binary with a solid header and ASCII', () => {
  const binary = new ArrayBuffer(134),
    v = new DataView(binary);
  new Uint8Array(binary).set(new TextEncoder().encode('solid'));
  v.setUint32(80, 1, true);
  v.setFloat32(108, 2, true);
  v.setFloat32(124, 3, true);
  for (const b of [
    binary,
    bytes(
      'solid test\nfacet normal 0 0 1\nouter loop\nvertex 0 0 0\nvertex 2 0 0\nvertex 0 3 0\nendloop\nendfacet\nendsolid'
    ),
  ]) {
    const mesh = parseSTL(b);
    assert.deepEqual(mesh.max, [2, 3, 0]);
    assert.equal(mesh.indices.length, 3);
  }
});
test('PLY handles property order, both endiannesses and polygon lists', () => {
  for (const format of ['ascii', 'binary_little_endian', 'binary_big_endian']) {
    const header = bytes(
      'ply\nformat ' +
        format +
        ' 1.0\nelement vertex 3\nproperty float y\nproperty float x\nproperty float z\nelement face 1\nproperty list uchar int vertex_indices\nend_header\n'
    );
    const points = [0, 0, 0, 0, 2, 0, 3, 0, 0];
    const body = format === 'ascii' ? bytes(points.join(' ') + ' 3 0 1 2') : new ArrayBuffer(49);
    if (format !== 'ascii') {
      const v = new DataView(body),
        le = format === 'binary_little_endian';
      points.forEach((n, i) => v.setFloat32(i * 4, n, le));
      v.setUint8(36, 3);
      [0, 1, 2].forEach((n, i) => v.setInt32(37 + i * 4, n, le));
    }
    const data = new Uint8Array(header.byteLength + body.byteLength);
    data.set(new Uint8Array(header));
    data.set(new Uint8Array(body), header.byteLength);
    const mesh = parsePLY(data.buffer);
    assert.deepEqual(mesh.max, [2, 3, 0]);
    assert.deepEqual([...mesh.indices], [0, 1, 2]);
  }
});
test('glTF loads confined sibling and embedded buffers, honors node transforms and rejects external URLs', async () => {
  const bin = new Float32Array([0, 0, 0, 2, 0, 0, 0, 3, 0]).buffer;
  const doc = {
    asset: { version: '2.0' },
    buffers: [{ uri: 'parts/data.bin', byteLength: 36 }],
    bufferViews: [{ buffer: 0, byteLength: 36 }],
    accessors: [{ bufferView: 0, type: 'VEC3', componentType: 5126, count: 3 }],
    meshes: [{ primitives: [{ attributes: { POSITION: 0 } }] }],
    scenes: [{ nodes: [0] }],
    nodes: [{ translation: [1, 2, 3], children: [1] }, { mesh: 0 }],
  };
  const mesh = await parseGLTF(bytes(JSON.stringify(doc)), async (path, n) => {
    assert.equal(path, 'parts/data.bin');
    assert.equal(n, 36);
    return bin;
  });
  assert.deepEqual(mesh.min, [1, 2, 3]);
  assert.deepEqual(mesh.max, [3, 5, 3]);
  doc.buffers[0].uri = 'data:application/octet-stream;base64,' + Buffer.from(bin).toString('base64');
  assert.deepEqual((await parseGLTF(bytes(JSON.stringify(doc)))).indices, mesh.indices);
  for (const uri of ['../data.bin', '%2e%2e/a.bin', 'https://other.test/buffer']) {
    doc.buffers[0].uri = uri;
    await assert.rejects(parseGLTF(bytes(JSON.stringify(doc)), () => assert.fail('unconfined read')));
  }
  delete doc.buffers[0].uri;
  let json = JSON.stringify(doc);
  json = json.padEnd(Math.ceil(json.length / 4) * 4, ' ');
  const glb = new ArrayBuffer(28 + json.length + bin.byteLength),
    v = new DataView(glb);
  for (const [offset, value] of [
    [0, 0x46546c67],
    [4, 2],
    [8, glb.byteLength],
    [12, json.length],
    [16, 0x4e4f534a],
    [20 + json.length, 36],
    [24 + json.length, 0x004e4942],
  ])
    v.setUint32(offset, value, true);
  new Uint8Array(glb, 20, json.length).set(new TextEncoder().encode(json));
  new Uint8Array(glb, 28 + json.length).set(new Uint8Array(bin));
  assert.deepEqual((await parseGLTF(glb)).positions, mesh.positions);
});
