// Synthetic Local/Cloud observations; never generate, install or sign in.
export function phoneFixtures(ws, remote) {
  const model = 'cloud-model-with-a-long-discovered-name';
  const projects = [{ id: 'fixture-project', name: 'A project with a long title for phone layout', directory: '/workspace/a-long-project-folder', machine: remote.id, default_choice: 'fixture-cloud' }];
  const providers = [{ id: 'fixture', name: 'Fixture cloud', ready: true, endpoint: 'https://api.example.test/v1', models: [model] }];
  const observations = {
    '/api/workspace': { ...ws, runtimes: ws.runtimes.filter(r => ['local', 'cloud'].includes(r.kind)), models: [{ id: 'fixture-cloud', name: model, model, kind: 'cloud', runtime_id: 'openai-compatible', provider_id: 'fixture', provider_name: 'Fixture cloud', enabled: true, ready: true }], projects, providers },
    '/api/machines': { ok: true, machines: [remote] },
    '/api/env/services': { ok: true, services: [{ id: 'synthetic', name: 'A service with a long name', url: 'http://192.0.2.10:3000/a-long-service-path-for-phone-layout', machine: remote.id }] },
    '/api/env/docker': { ok: true, enabled: true, containers: [{ name: 'synthetic-container', image: 'registry.example.test/a-long-image-name:version', state: 'running', status: 'Up two days', ports: '192.0.2.10:3000->3000/tcp' }] },
    '/api/env/proxmox': { ok: true, config: { enabled: true } },
    '/api/env/proxmox/resources': { ok: true, resources: [{ vmid: 100, name: 'synthetic-machine-with-a-long-name', type: 'qemu', node: 'synthetic-node', status: 'running', mem: 1073741824, maxmem: 4294967296 }] },
    '/api/models': [{ name: 'synthetic-model-with-a-long-name-Q4_K_M.gguf', path: '/synthetic-model-library/model.gguf', dir: '/synthetic-model-library', size: 1073741824 }],
    '/api/chat/history': { ok: true, projects, conversations: [] },
    '/api/tasks': { ok: true, tasks: [{ id: 'synthetic', discussion_id: 'synthetic', title: 'A long retained task title for phone layout', status: 'waiting_input', model, executor: 'openai-compatible', requests: [{ id: 'request', kind: 'user_input', summary: 'Synthetic read-only request' }] }] },
    '/api/usage': { ok: true, quotas: [], models: [{ runtime_id: 'openai-compatible', name: model, provider: 'Fixture cloud', turns: 2, reported_turns: 2, usage: { prompt_tokens: 1000, completion_tokens: 200 }, price: { input: 1, output: 2 } }] },
    '/api/usage/providers': { ok: true, providers: [] },
    '/api/runtime/sessions': { ok: true, sessions: [{ message_count: 1, id: 'synthetic', title: 'Discussion with a long title for phone checks', runtime_id: 'openai-compatible', model, updated_at: 1767225600000 }] },
  };
  return async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/estimate') return route.fulfill({ json: { ok: true, gpu_mb: 1024, vram_total_mb: 8192, ram_offload_mb: 0 } });
    if (route.request().method() !== 'GET' && path !== '/api/discussion/events') return route.fulfill({ status: 409, json: { ok: false, error: 'Read-only phone fixture' } });
    if (observations[path]) return route.fulfill({ json: observations[path] });
    if (path === '/api/workspaces' || path.endsWith('/workspaces')) return route.fulfill({ json: { ok: true, workspaces: [{ id: 'synthetic', name: 'A workspace with a long name', path: '/workspace/a-long-project-folder' }] } });
    if (path === '/api/tasks/stream') return route.fulfill({ contentType: 'text/event-stream', body: ': synthetic read-only stream\n\n' });
    return route.continue();
  };
}
