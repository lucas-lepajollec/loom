import { html, useState, useRef, useEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { request } from '../../core/api.js';
const initial = () => ({ yaw: -0.48, pitch: 0.32, zoom: 0.85, x: 0, y: 0 });
function projection(c, width, height) {
  const sx = Math.cos(c.yaw),
    sz = Math.sin(c.yaw),
    cy = Math.cos(c.pitch),
    cz = Math.sin(c.pitch);
  const scale = Math.min(width, height) * c.zoom;
  const x = scale / width,
    y = scale / height;
  return [
    sx * x,
    sz * cz * y,
    -sz * cy * 0.5,
    0,
    0,
    cy * y,
    cz * 0.5,
    0,
    sz * x,
    -sx * cz * y,
    sx * cy * 0.5,
    0,
    c.x,
    c.y,
    0,
    1,
  ];
}

function gpu(gl, mesh) {
  const shaders = [],
    buffers = [],
    program = gl.createProgram();
  function shader(type, source) {
    const sh = gl.createShader(type);
    shaders.push(sh);
    gl.shaderSource(sh, source);
    gl.compileShader(sh);
    gl.attachShader(program, sh);
  }
  function buffer(type, data) {
    const b = gl.createBuffer();
    buffers.push(b);
    gl.bindBuffer(type, b);
    gl.bufferData(type, data, gl.STATIC_DRAW);
    return b;
  }
  function dispose() {
    for (const b of buffers) gl.deleteBuffer(b);
    for (const sh of shaders) gl.deleteShader(sh);
    gl.deleteProgram(program);
  }
  shader(
    gl.VERTEX_SHADER,
    `#version 300 es
    in vec3 position; uniform mat4 projection; out vec3 point;
    void main(){ gl_Position=projection*vec4(position,1.); point=gl_Position.xyz; gl_PointSize=2.; }`
  );
  shader(
    gl.FRAGMENT_SHADER,
    `#version 300 es
    precision highp float; in vec3 point; out vec4 color; uniform bool lines;
    void main(){ float light=lines?1.:.35+.65*abs(dot(normalize(cross(dFdx(point),dFdy(point))),normalize(vec3(-.4,.7,.6))));
    color=vec4(vec3(.62,.79,.91)*light,1.); }`
  );
  gl.linkProgram(program);
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    dispose();
    throw new Error('WebGL');
  }
  gl.useProgram(program);
  buffer(gl.ARRAY_BUFFER, mesh.positions);
  const loc = gl.getAttribLocation(program, 'position');
  gl.enableVertexAttribArray(loc);
  gl.vertexAttribPointer(loc, 3, gl.FLOAT, false, 0, 0);
  const triangles = buffer(gl.ELEMENT_ARRAY_BUFFER, mesh.indices);
  const matrix = gl.getUniformLocation(program, 'projection'),
    lines = gl.getUniformLocation(program, 'lines');
  let edges;
  return {
    dispose,
    draw(m, wire) {
      gl.viewport(0, 0, gl.canvas.width, gl.canvas.height);
      gl.clearColor(0.085, 0.1, 0.12, 1);
      gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
      gl.enable(gl.DEPTH_TEST);
      gl.uniformMatrix4fv(matrix, false, m);
      gl.uniform1i(lines, wire || !mesh.indices.length ? 1 : 0);
      if (!mesh.indices.length) {
        gl.drawArrays(gl.POINTS, 0, mesh.positions.length / 3);
        return;
      }
      if (wire && !edges) {
        const data = new Uint32Array(mesh.indices.length * 2);
        for (let i = 0; i < mesh.indices.length; i += 3) {
          const [a, b, c] = mesh.indices.subarray(i, i + 3);
          data.set([a, b, b, c, c, a], i * 2);
        }
        edges = buffer(gl.ELEMENT_ARRAY_BUFFER, data);
      }
      gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, wire ? edges : triangles);
      gl.drawElements(wire ? gl.LINES : gl.TRIANGLES, mesh.indices.length * (wire ? 2 : 1), gl.UNSIGNED_INT, 0);
    },
  };
}

function software(canvas, mesh) {
  const ctx = canvas.getContext('2d');
  return {
    dispose() {},
    draw(m, wire) {
      const w = canvas.width,
        h = canvas.height;
      ctx.fillStyle = '#161a1f';
      ctx.fillRect(0, 0, w, h);
      function project(i) {
        const p = mesh.positions,
          j = i * 3;
        const at = (k) => m[k] * p[j] + m[k + 4] * p[j + 1] + m[k + 8] * p[j + 2] + m[k + 12];
        return [((1 + at(0)) * w) / 2, ((1 - at(1)) * h) / 2, at(2)];
      }
      const faces = [],
        ids = mesh.indices;
      // Keep dragging responsive on devices without a GPU.
      const step = Math.max(1, Math.ceil(ids.length / 18000)) * 3;
      for (let i = 0; i < ids.length; i += step) {
        const a = project(ids[i]),
          b = project(ids[i + 1]),
          c = project(ids[i + 2]);
        faces.push({ a, b, c, depth: a[2] + b[2] + c[2] });
      }
      faces.sort((a, b) => b.depth - a.depth);
      ctx.strokeStyle = '#a6c6de';
      for (const { a, b, c } of faces) {
        ctx.beginPath();
        ctx.moveTo(a[0], a[1]);
        ctx.lineTo(b[0], b[1]);
        ctx.lineTo(c[0], c[1]);
        ctx.closePath();
        if (wire) ctx.stroke();
        else {
          const area = (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]);
          ctx.fillStyle =
            'hsl(205 30% ' +
            (35 +
              36 *
                Math.min(
                  1,
                  Math.abs(area) / (1 + Math.hypot(b[0] - a[0], b[1] - a[1]) * Math.hypot(c[0] - a[0], c[1] - a[1]))
                )) +
            '%)';
          ctx.fill();
        }
      }
      if (!ids.length) {
        ctx.fillStyle = '#a6c6de';
        for (let i = 0; i < mesh.positions.length / 3; i += Math.max(1, Math.ceil(mesh.positions.length / 60000))) {
          const [x, y] = project(i);
          ctx.fillRect(x, y, 2, 2);
        }
      }
    },
  };
}

function drawBounds(overlay, half, matrix, show) {
  const ctx = overlay.getContext('2d'),
    w = overlay.width,
    h = overlay.height;
  ctx.clearRect(0, 0, w, h);
  if (!show) return;
  const corners = Array.from({ length: 8 }, (_, bits) => {
    const [x, y, z] = half.map((v, i) => (bits & (1 << i) ? v : -v));
    const at = (k) => matrix[k] * x + matrix[k + 4] * y + matrix[k + 8] * z + matrix[k + 12];
    return [((1 + at(0)) * w) / 2, ((1 - at(1)) * h) / 2];
  });
  ctx.strokeStyle = '#ebae64';
  ctx.beginPath();
  for (let i = 0; i < 8; i++) {
    for (const bit of [1, 2, 4]) {
      if (i & bit) continue;
      ctx.moveTo(...corners[i]);
      ctx.lineTo(...corners[i | bit]);
    }
  }
  ctx.stroke();
}

function Stage({ mesh, wire, bounds, reset }) {
  const host = useRef(),
    state = useRef(initial()),
    redraw = useRef(),
    wireRef = useRef(),
    boundsRef = useRef();
  boundsRef.current = bounds;
  wireRef.current = wire;
  useEffect(() => {
    const box = host.current;
    let canvas = document.createElement('canvas'),
      renderer;
    try {
      const gl = canvas.getContext('webgl2', { antialias: true });
      renderer = gl ? gpu(gl, mesh) : software(canvas, mesh);
    } catch {
      canvas = document.createElement('canvas');
      renderer = software(canvas, mesh);
    }
    const overlay = document.createElement('canvas');
    overlay.className = 'up-model-bounds';
    overlay.setAttribute('aria-hidden', 'true');
    box.append(canvas, overlay);
    canvas.setAttribute('aria-label', t('preview.controls3d'));
    canvas.tabIndex = 0;
    const pointers = new Map();
    let frame = 0;
    function draw() {
      frame = 0;
      const ratio = Math.min(devicePixelRatio || 1, 2);
      const w = Math.max(1, Math.round(box.clientWidth * ratio)),
        h = Math.max(1, Math.round(box.clientHeight * ratio));
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
      }
      if (overlay.width !== w || overlay.height !== h) {
        overlay.width = w;
        overlay.height = h;
      }
      const matrix = projection(state.current, w, h);
      renderer.draw(matrix, wireRef.current);
      drawBounds(overlay, mesh.halfSize, matrix, boundsRef.current);
    }
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(draw);
    };
    redraw.current = schedule;
    const resize = new ResizeObserver(schedule);
    resize.observe(box);
    const zoom = (factor) => {
      state.current.zoom = Math.min(16, Math.max(0.08, state.current.zoom * factor));
    };
    canvas.onwheel = (e) => {
      e.preventDefault();
      zoom(Math.exp(-e.deltaY * 0.002));
      schedule();
    };
    const distance = () => {
      const [a, b] = pointers.values();
      return a && b ? Math.hypot(a.x - b.x, a.y - b.y) : 0;
    };
    canvas.onpointerdown = (e) => {
      canvas.setPointerCapture(e.pointerId);
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    };
    canvas.onpointermove = (e) => {
      const prev = pointers.get(e.pointerId);
      if (!prev) return;
      const d = distance(),
        dx = e.clientX - prev.x,
        dy = e.clientY - prev.y,
        cam = state.current;
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (d) zoom(distance() / d);
      else if (e.shiftKey || e.buttons === 4) {
        cam.x += (2 * dx) / box.clientWidth;
        cam.y -= (2 * dy) / box.clientHeight;
      } else {
        cam.yaw += dx * 0.01;
        cam.pitch += dy * 0.01;
      }
      schedule();
    };
    canvas.onlostpointercapture = (e) => pointers.delete(e.pointerId);
    canvas.onkeydown = (e) => {
      const c = state.current;
      switch (e.key) {
        case 'ArrowLeft':
          c.yaw -= 0.1;
          break;
        case 'ArrowRight':
          c.yaw += 0.1;
          break;
        case 'ArrowUp':
          c.pitch -= 0.1;
          break;
        case 'ArrowDown':
          c.pitch += 0.1;
          break;
        case '+':
          zoom(1.2);
          break;
        case '-':
          zoom(1 / 1.2);
          break;
        default:
          return;
      }
      e.preventDefault();
      schedule();
    };
    schedule();
    return () => {
      resize.disconnect();
      cancelAnimationFrame(frame);
      redraw.current = null;
      renderer.dispose();
      canvas.remove();
      overlay.remove();
    };
  }, [mesh]);
  useEffect(() => {
    state.current = initial();
    redraw.current?.();
  }, [reset]);
  useEffect(() => redraw.current?.(), [wire, bounds]);
  return html`<div ref=${host} class="up-model-stage"></div>`;
}

export function ModelViewer({ item, fileURL }) {
  const [mesh, setMesh] = useState(null),
    [failed, setFailed] = useState(false);
  const [wire, setWire] = useState(false),
    [bounds, setBounds] = useState(false),
    [reset, setReset] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    async function read(path, expected) {
      const r = await request(fileURL(path), { signal: controller.signal });
      if (!r.ok || (expected && Number(r.headers.get('content-length')) !== expected)) throw new Error('mesh');
      return r.arrayBuffer();
    }
    const worker = new Worker(new URL('./mesh-worker.js', import.meta.url), { type: 'module' });
    const fail = () => {
      worker.terminate();
      if (!controller.signal.aborted) setFailed(true);
    };
    worker.onerror = fail;
    worker.onmessage = async ({ data }) => {
      if (data.failed) {
        fail();
        return;
      }
      if (data.mesh) {
        worker.terminate();
        setMesh(data.mesh);
        return;
      }
      try {
        const dir = item.path.slice(0, item.path.lastIndexOf('/') + 1);
        const bytes = await read(dir + data.path, data.size);
        worker.postMessage({ bytes }, [bytes]);
      } catch {
        fail();
      }
    };
    read(item.path)
      .then((bytes) => {
        worker.postMessage({ ext: item.path.split('.').pop().toLowerCase(), bytes }, [bytes]);
      })
      .catch(fail);
    return () => {
      worker.terminate();
      controller.abort();
    };
  }, [item.path]);
  if (!mesh) return html`<p role="status">${t(failed ? 'preview.bad_model' : 'connections.mcp.loading')}</p>`;
  const size = mesh.max.map((v, i) => Number((v - mesh.min[i]).toPrecision(4))).join(' × ');
  return html`<div class="up-toolbar">
      <button class="btn sm" aria-pressed=${wire} onClick=${() => setWire(!wire)}>${t('preview.wire')}</button>
      <button class="btn sm" aria-pressed=${bounds} onClick=${() => setBounds(!bounds)}>
        ${t('preview.bounds')}
      </button>
      <button class="btn sm" onClick=${() => setReset(reset + 1)}>${t('preview.reset')}</button>
    </div>
    <${Stage} mesh=${mesh} wire=${wire} bounds=${bounds} reset=${reset} />
    <small
      >${t('preview.mesh_stats', {
        vertices: mesh.positions.length / 3,
        triangles: mesh.indices.length / 3,
        size,
      })}</small
    >`;
}
