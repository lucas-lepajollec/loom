// Synthetic observations only. No CLI, sign-in, installation or generation.
export function phoneFixtures(ws, remote) {
  const families = ws.runtimes.filter(r => r.kind === 'harness');
  const runtimes = ws.runtimes.map(r => r.kind === 'harness' && r.implemented ? { ...r, available: true, connected: true, features: { ...r.features, config_options: [...new Set([...(r.features?.config_options || []), 'model'])], history_import: true } } : r);
  const agent = runtimes.find(r => r.id === 'codex');
  const model = 'native-model-with-a-long-discovered-name';
  const models = runtimes.filter(r => r.kind === 'harness' && r.implemented).map(r => ({ id: r.id + ':' + model, name: model, model, kind: 'harness', runtime_id: r.id, enabled: true, ready: true }));
  const projects = [{ id: 'fixture-project', name: 'A project with a long title for phone layout', directory: '/workspace/a-long-project-folder', machine: remote.id, default_choice: agent.id + ':' + model }];
  const installations = families.filter(r => r.implemented).flatMap(r => ['local', remote.id].map(machine => ({ machine, machine_name: machine === 'local' ? 'This machine' : remote.name, harness: r.id, runtime_id: machine === 'local' ? r.id : '', name: r.name, installed: true, managed: true, enabled: machine === 'local', ready: true, version: '1.2.3' })));
  const config = [{ id: 'model', category: 'model', currentValue: model, options: [model, ...Array.from({ length: 9 }, (_, i) => 'native-model-' + i)].map(value => ({ value, name: value, description: 'Synthetic native catalog observation' })) }];
  return async route => {
    const u = new URL(route.request().url()), path = u.pathname;
    if (path === '/api/estimate') return route.fulfill({ json: { ok: true, gpu_mb: 1024, vram_total_mb: 8192, ram_offload_mb: 0 } });
    if (/^\/api\/runtimes\/[^/]+\/quota$/.test(path)) return route.fulfill({ json: { ok: true, quota: { windows: [] } } });
    // All mutations are denied. Opening a dialog or native history is read-only.
    if (route.request().method() !== 'GET' && path !== '/api/discussion/events') return route.fulfill({ status: 409, json: { ok: false, error: 'Read-only phone fixture' } });
    if (path === '/api/workspace') return route.fulfill({ json: { ...ws, runtimes, models, projects } });
    if (path === '/api/agents/installations') return route.fulfill({ json: { ok: true, installations } });
    if (path === '/api/machines') return route.fulfill({ json: { ok: true, machines: [remote], offers: { [remote.id]: [] } } });
    if (/^\/api\/runtimes\/[^/]+\/probe$/.test(path)) return route.fulfill({ json: { ok: true, probe: { config, agent: { version: '1.2.3' }, commands: [], modes: [] } } });
    if (/^\/api\/runtimes\/[^/]+\/inspect$/.test(path)) return route.fulfill({ json: { ok: true, inspection: { installed: true, path: '/opt/agents/a-long-installation-directory/bin/agent', auth: { connected: false, status: 'Synthetic account' }, env: [], mcp: [], plugins: [], skills: [] } } });
    if (/^\/api\/runtimes\/[^/]+\/sessions$/.test(path) || path === '/api/harness-history' && u.searchParams.has('source')) return route.fulfill({ json: { ok: true, sessions: [{ sessionId: 'synthetic', title: 'Native discussion with a long title for phone checks', cwd: '/workspace/a-long-project-folder', updatedAt: '2026-01-01T00:00:00Z' }] } });
    if (path === '/api/harness/lifecycle') return route.fulfill({ json: { ok: true, state: { installed: true, version: '1.2.3', latest: '1.2.3', can_update: true, check_update: true, channel: 'native' } } });
    if (path === '/api/harness/model-source') return route.fulfill({ json: { ok: true, supported: true, enabled: false, format: 'env', models: 2 } });
    if (path === '/api/agents/catalog') return route.fulfill({ json: [{ id: 'synthetic', name: 'Synthetic ACP agent', version: '1.2.3', description: 'A catalog entry for deterministic phone layout checks', installed: true }] });
    if (path === '/api/harness/bindings') return route.fulfill({ json: { ok: true, custom: false, mcp: [] } });
    if (path === '/api/skills/targets') return route.fulfill({ json: { ok: true, targets: [], bindings: {} } });
    if (path === '/api/env/services') return route.fulfill({ json: { ok: true, services: [{ id: 'synthetic', name: 'A service with a long name', url: 'http://192.0.2.10:3000/a-long-service-path-for-phone-layout', machine: remote.id }] } });
    if (path === '/api/env/docker') return route.fulfill({ json: { ok: true, enabled: true, containers: [{ name: 'synthetic-container', image: 'registry.example.test/a-long-image-name:version', state: 'running', status: 'Up two days', ports: '192.0.2.10:3000->3000/tcp' }] } });
    if (path === '/api/env/proxmox') return route.fulfill({ json: { ok: true, config: { enabled: true } } });
    if (path === '/api/env/proxmox/resources') return route.fulfill({ json: { ok: true, resources: [{ vmid: 100, name: 'synthetic-machine-with-a-long-name', type: 'qemu', node: 'synthetic-node', status: 'running', mem: 1073741824, maxmem: 4294967296 }] } });
    if (path === '/api/models') return route.fulfill({ json: [{ name: 'synthetic-model-with-a-long-name-Q4_K_M.gguf', path: '/synthetic-model-library/model.gguf', dir: '/synthetic-model-library', size: 1073741824 }] });
    if (path === '/api/chat/history') return route.fulfill({ json: { ok: true, projects, conversations: [] } });
    if (path === '/api/workspaces' || path.endsWith('/workspaces')) return route.fulfill({ json: { ok: true, workspaces: [{ id: 'synthetic', name: 'A workspace with a long name', path: '/workspace/a-long-project-folder' }] } });
    if (path === '/api/tasks') return route.fulfill({ json: { ok: true, tasks: [{ id: 'synthetic', discussion_id: 'synthetic', title: 'A long retained task title for phone layout', status: 'waiting_input', model, executor: agent.id, requests: [{ id: 'request', kind: 'user_input', summary: 'Synthetic read-only request' }] }] } });
    if (path === '/api/tasks/stream') return route.fulfill({ contentType: 'text/event-stream', body: ': synthetic read-only stream\n\n' });
    if (path === '/api/usage/native') return route.fulfill({ json: { ok: true, harnesses: [{ runtime_id: agent.id, name: agent.name, sessions: 2, input_tokens: 1000, output_tokens: 200, total_tokens: 1200, by_model: [{ model, tokens: 1200 }] }] } });
    if (path === '/api/usage') return route.fulfill({ json: { ok: true, quotas: [], models: [{ runtime_id: agent.id, name: model, provider: agent.name, turns: 2, reported_turns: 2, usage: { prompt_tokens: 1000, completion_tokens: 200 }, price: { input: 1, output: 2 } }] } });
    if (path === '/api/usage/providers') return route.fulfill({ json: { ok: true, providers: [] } });
    if (path === '/api/runtime/sessions') return route.fulfill({ json: { ok: true, sessions: [{ message_count: 1, id: 'synthetic', title: 'Discussion with a long title for phone checks', runtime_id: agent.id, model, updated_at: 1767225600000 }] } });
    return route.continue();
  };
}
