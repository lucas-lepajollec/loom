/**
 * Loom Public Demo — Browser-Only Mock Engine
 *
 * Intercepts window.fetch, window.EventSource, and window.WebSocket for all
 * Loom /api/* endpoints so the real ui/next Preact/HTM interface runs
 * statically with 100% fictional, session-scoped sandbox data.
 *
 * Zero personal data, zero real IPs (RFC1918), zero network inference.
 */
(function () {
  'use strict';

  const STORAGE_KEY = 'loom_demo_state_v1';
  const DEMO_VERSION = '0.3.0-demo';
  const NOW = Date.now();
  const HOUR = 3600 * 1000;
  const DAY = 24 * HOUR;

  function randomId(len = 6) {
    const bytes = new Uint8Array(len);
    globalThis.crypto.getRandomValues(bytes);
    return Array.from(bytes, b => (b % 36).toString(36)).join('');
  }

  // ---------- 1. Language bootstrap (?lang=en|fr, default EN) ----------
  function bootstrapLanguage() {
    let lang = null;
    try {
      const search = typeof window !== 'undefined' && window.location && window.location.search;
      if (search) {
        const q = new URLSearchParams(search).get('lang');
        if (q === 'en' || q === 'fr') lang = q;
      }
    } catch (_) {}
    if (!lang) {
      try {
        const saved = localStorage.getItem('loom-lang');
        if (saved === 'en' || saved === 'fr') lang = saved;
      } catch (_) {}
    }
    if (!lang) lang = 'en';
    try {
      localStorage.setItem('loom-lang', lang);
    } catch (_) {}
    if (typeof document !== 'undefined' && document.documentElement) {
      document.documentElement.lang = lang;
    }
    return lang;
  }

  function bootstrapUiDefaults() {
    try {
      if (localStorage.getItem('loom.next.proj') === null) {
        localStorage.setItem('loom.next.proj', JSON.stringify(['proj-homelab', 'proj-research']));
      }
      if (localStorage.getItem('loom.files.open') === null) {
        localStorage.setItem('loom.files.open', JSON.stringify(['homelab', 'research', 'runbooks']));
      }
      if (localStorage.getItem('loom.files.last.brain') === null) {
        localStorage.setItem('loom.files.last.brain', JSON.stringify('homelab/cluster-topology.md'));
      }
      if (localStorage.getItem('loom-theme') === null) {
        localStorage.setItem('loom-theme', 'dark');
      }
    } catch (_) {}
  }

  // ---------- 2. Default 100% fictional sandbox state ----------
  function buildQuotaSamples(now, reset5h, reset7d, codex5h, codex7d) {
    const samples = [];
    // 14 days of synthetic quota readings every 6 hours for rich WindowChart + DailyChart
    const c7dStart = reset7d - 7 * DAY;
    const x7dStart = codex7d - 7 * DAY;
    for (let d = 13; d >= 0; d--) {
      for (let h = 0; h < 24; h += 6) {
        const at = now - d * DAY + (h - 18) * HOUR;
        if (at >= now - 10 * 60 * 1000) continue;
        // Current 7d window progress vs prior week
        const inCurC7 = at >= c7dStart;
        const c7Frac = inCurC7 ? Math.max(0, Math.min(1, (at - c7dStart) / (now - c7dStart))) : Math.max(0, Math.min(1, (at - (c7dStart - 7 * DAY)) / (7 * DAY)));
        const c7Used = Math.round((inCurC7 ? 6 + 50 * Math.pow(c7Frac, 0.92) : 8 + 74 * c7Frac) * 10) / 10;
        const c7Reset = inCurC7 ? reset7d : c7dStart;

        const inCurX7 = at >= x7dStart;
        const x7Frac = inCurX7 ? Math.max(0, Math.min(1, (at - x7dStart) / (now - x7dStart))) : Math.max(0, Math.min(1, (at - (x7dStart - 7 * DAY)) / (7 * DAY)));
        const x7Used = Math.round((inCurX7 ? 4 + 34 * Math.pow(x7Frac, 0.95) : 5 + 58 * x7Frac) * 10) / 10;
        const x7Reset = inCurX7 ? codex7d : x7dStart;

        const entry = {
          at,
          claude: { '7d': { used: c7Used, reset_at: c7Reset } },
          codex: { '7d': { used: x7Used, reset_at: x7Reset } },
        };
        if (at >= reset5h - 5 * HOUR) {
          const f5 = Math.max(0, Math.min(1, (at - (reset5h - 5 * HOUR)) / (now - (reset5h - 5 * HOUR))));
          entry.claude['5h'] = { used: Math.round((5 + 35 * f5) * 10) / 10, reset_at: reset5h };
        }
        if (at >= codex5h - 5 * HOUR) {
          const f5x = Math.max(0, Math.min(1, (at - (codex5h - 5 * HOUR)) / (now - (codex5h - 5 * HOUR))));
          entry.codex['5h'] = { used: Math.round((4 + 22 * f5x) * 10) / 10, reset_at: codex5h };
        }
        samples.push(entry);
      }
    }
    return samples;
  }

  function buildTokenDaily(now) {
    const out = [];
    const baseIn = [142000, 198000, 84000, 265000, 174000, 229000, 118000, 312000, 189000, 156000, 244000, 92000, 278000, 214000];
    const baseOut = [28400, 41200, 16900, 52300, 34100, 46800, 22100, 61900, 37800, 31200, 48700, 19400, 55100, 43900];
    const sess = [4, 6, 2, 7, 5, 6, 3, 8, 5, 4, 6, 3, 7, 5];
    for (let i = 0; i < 14; i++) {
      const d = new Date(now - i * DAY);
      const iso = d.toISOString().slice(0, 10);
      const inputTokens = baseIn[i];
      const outputTokens = baseOut[i];
      const estimatedCost = Math.round(((inputTokens * 3 + outputTokens * 15) / 1e6) * 100) / 100;
      out.push({ date: iso, inputTokens, outputTokens, sessions: sess[i], estimatedCost });
    }
    return out;
  }

  function createInitialState(lang) {
    const now = Date.now();
    const nowSec = Math.floor(now / 1000);
    const reset5h = now + 2.4 * HOUR;
    const reset7d = now + 2.6 * DAY;
    const codex5h = now + 3.1 * HOUR;
    const codex7d = now + 3.8 * DAY;

    return {
      lang,
      prefs: {
        lang,
        onboarded: '1',
        theme: 'dark',
        hide_reasoning: '0',
        hide_tools: '0',
        enter_newline: '0',
      },
      sysprompt: 'Be concise, technical, and explicit about assumptions. Prefer structured Markdown tables and runnable examples.',
      agentCompact: true,
      activeChatId: 'chat-vram-audit',
      engine: {
        active: true,
        health: true,
        model: '/srv/models/gguf/Qwen3-14B-Q4_K_M.gguf',
        model_name: 'Qwen3-14B-Q4_K_M.gguf',
        preset_id: 'preset-qwen3-code',
        preset_name: 'Qwen3 14B · Code & Reasoning',
        hostname: 'workstation.local',
        engine_kind: 'llama.cpp',
        port: 8081,
        ctx_effective: 32768,
        np: 2,
        idle_unload_minutes: 30,
        models_max: 2,
        interactive_grace_minutes: 15,
        exposed: false,
        key_required: false,
      },
      gpu: {
        index: 0,
        name: 'NVIDIA RTX 4090 (Simulated)',
        used: 11264,
        total: 24576,
        util: 26,
        temp: 47,
      },
      models: [
        {
          name: 'Qwen3-14B-Q4_K_M.gguf',
          path: '/srv/models/gguf/Qwen3-14B-Q4_K_M.gguf',
          value: '/srv/models/gguf/Qwen3-14B-Q4_K_M.gguf',
          dir: '/srv/models/gguf',
          size: 8998420480,
          shards: 1,
          vram_mb: 11264,
          native_ctx: 40960,
          n_layers: 40,
          thinks: true,
        },
        {
          name: 'Llama-3.3-8B-Instruct-Q6_K.gguf',
          path: '/srv/models/gguf/Llama-3.3-8B-Instruct-Q6_K.gguf',
          value: '/srv/models/gguf/Llama-3.3-8B-Instruct-Q6_K.gguf',
          dir: '/srv/models/gguf',
          size: 6592389120,
          shards: 1,
          vram_mb: 7920,
          native_ctx: 131072,
          n_layers: 32,
          thinks: false,
        },
        {
          name: 'Mistral-Small-24B-Instruct-Q4_K_M.gguf',
          path: '/srv/models/gguf/Mistral-Small-24B-Instruct-Q4_K_M.gguf',
          value: '/srv/models/gguf/Mistral-Small-24B-Instruct-Q4_K_M.gguf',
          dir: '/srv/models/gguf',
          size: 14328446976,
          shards: 1,
          vram_mb: 16840,
          native_ctx: 32768,
          n_layers: 40,
          thinks: false,
        },
        {
          name: 'Gemma-3-4B-IT-Q8_0.gguf',
          path: '/srv/models/gguf/Gemma-3-4B-IT-Q8_0.gguf',
          value: '/srv/models/gguf/Gemma-3-4B-IT-Q8_0.gguf',
          dir: '/srv/models/gguf',
          size: 4281729024,
          shards: 1,
          vram_mb: 5120,
          native_ctx: 131072,
          n_layers: 34,
          thinks: false,
        },
      ],
      presets: [
        {
          id: 'preset-qwen3-code',
          name: 'Qwen3 14B · Code & Reasoning',
          model: '/srv/models/gguf/Qwen3-14B-Q4_K_M.gguf',
          ctx: '32768',
          reasoning: true,
          vision: false,
          active: true,
          content: 'MODEL=/srv/models/gguf/Qwen3-14B-Q4_K_M.gguf\nCTX=32768\nNGL=999\nREASONING=on\nEXTRA_ARGS=--flash-attn on --cache-type-k q8_0 --cache-type-v q8_0 -np 2\n',
        },
        {
          id: 'preset-mistral-long',
          name: 'Mistral Small 24B · Long Context',
          model: '/srv/models/gguf/Mistral-Small-24B-Instruct-Q4_K_M.gguf',
          ctx: '32768',
          reasoning: false,
          vision: false,
          active: false,
          content: 'MODEL=/srv/models/gguf/Mistral-Small-24B-Instruct-Q4_K_M.gguf\nCTX=32768\nNGL=999\nEXTRA_ARGS=--flash-attn on --cache-type-k q8_0\n',
        },
        {
          id: 'preset-gemma-fast',
          name: 'Gemma 3 4B · Fast Draft',
          model: '/srv/models/gguf/Gemma-3-4B-IT-Q8_0.gguf',
          ctx: '16384',
          reasoning: false,
          vision: true,
          active: false,
          content: 'MODEL=/srv/models/gguf/Gemma-3-4B-IT-Q8_0.gguf\nCTX=16384\nNGL=999\n',
        },
      ],
      providers: [
        {
          id: 'prov-anthropic',
          name: 'Anthropic',
          endpoint: 'https://api.anthropic.com/v1',
          ready: true,
          remember: true,
          key_hint: 'sk-ant-••••demo',
          usage_mode: 'stream',
          models: ['claude-sonnet-4-5', 'claude-haiku-4-5'],
        },
        {
          id: 'prov-openai',
          name: 'OpenAI',
          endpoint: 'https://api.openai.com/v1',
          ready: true,
          remember: true,
          key_hint: 'sk-proj-••••demo',
          usage_mode: 'stream',
          models: ['gpt-4.1', 'o4-mini'],
        },
        {
          id: 'prov-openrouter',
          name: 'OpenRouter',
          endpoint: 'https://openrouter.ai/api/v1',
          ready: true,
          remember: false,
          key_hint: 'sk-or-••••demo',
          usage_mode: 'stream',
          models: ['deepseek/deepseek-r1', 'qwen/qwen3-235b-a22b'],
        },
      ],
      projects: [
        { id: 'proj-homelab', name: 'Homelab Ops', directory: '/workspace/homelab-ops', capability_ids: [], mcp_servers: [], brain_sources: [] },
        { id: 'proj-research', name: 'Research', directory: '/workspace/research-notes', capability_ids: [], mcp_servers: [], brain_sources: [] },
      ],
      agentFolders: {
        'cm-homelab-audit': 'proj-homelab',
        'cm-docs-sync': 'proj-research',
        'cm-gpu-bench': 'proj-homelab',
      },
      discussions: {
        'chat-vram-audit': {
          id: 'chat-vram-audit',
          title: 'KV cache tuning for 24 GB GPU',
          project_id: 'proj-homelab',
          runtime_id: 'llama.cpp',
          model: '/srv/models/gguf/Qwen3-14B-Q4_K_M.gguf',
          provider_id: '',
          provider_name: '',
          native_archive: 'chat-vram-audit',
          updated_at: now - 4 * 60 * 1000,
          status: 'idle',
          instructions: '',
          turns: [
            {
              user: 'How much VRAM does Qwen3-14B Q4_K_M leave for a 32K context across 2 parallel slots on a 24 GB card?',
              reasoning: 'Checking GGUF weight footprint (~8.98 GB) plus runtime overhead (~0.8 GB). With 40 layers, GQA KV heads, flash attention enabled, and Q8_0 KV cache quantization, 32K total context across 2 slots takes roughly 1.5 GB. Total stays near 11.3 GB, well below 24 GB.',
              assistant: 'With **`Qwen3-14B-Q4_K_M.gguf`** loaded on a 24 GB GPU with Flash Attention and **`Q8_0` KV cache**, you stay well within VRAM with ~12.7 GB of headroom:\n\n| Component | Estimated VRAM | Notes |\n| --- | --- | --- |\n| Model weights (`Q4_K_M`) | `8.98 GiB` | 40 / 40 layers offloaded (`-ngl 999`) |\n| KV cache (`32K` · 2 slots · `q8_0`) | `1.48 GiB` | 16K effective context per slot |\n| Compute buffers & CUDA runtime | `0.80 GiB` | Flash Attention enabled |\n| **Total** | **`11.26 GiB`** | **`12.74 GiB` free** |\n\n### Recommended llama.cpp flags\n\n```bash\nllama-server \\\n  --model /srv/models/gguf/Qwen3-14B-Q4_K_M.gguf \\\n  --ctx-size 32768 --parallel 2 \\\n  --flash-attn on --cache-type-k q8_0 --cache-type-v q8_0\n```\n\nYou can safely push context to **64K** or keep a lightweight 4B draft model resident in router mode without spilling into system RAM.',
              elapsed_ms: 2140,
              toks: 158,
              rate: 74.2,
              prompt_tokens: 412,
            },
            {
              user: 'Great — if I switch this thread to Claude Sonnet 4.5 for a second opinion, does Loom send my local reasoning trace?',
              reasoning: 'Summarizing Loom portable-context contract: only user and assistant turns are shared after confirmation; private reasoning blocks and tool states stay local.',
              assistant: 'No. When you switch a discussion from a **Local** model to a **Cloud** provider, Loom:\n\n1. Prompts for **explicit confirmation** showing the destination endpoint.\n2. Shares only the **portable conversation turns** (user prompts and final assistant replies).\n3. Keeps **private reasoning blocks**, tool approvals, and unrelated Second Brain files strictly local.\n\nTry clicking the model selector in the header above to preview switching between local GGUF presets and Cloud models in the same thread.',
              elapsed_ms: 1420,
              toks: 96,
              rate: 76.8,
              prompt_tokens: 684,
            },
          ],
        },
        'chat-mcp-review': {
          id: 'chat-mcp-review',
          title: 'MCP gateway confirmation policy',
          project_id: 'proj-homelab',
          runtime_id: 'openai-compatible',
          model: 'claude-sonnet-4-5',
          provider_id: 'prov-anthropic',
          provider_name: 'Anthropic',
          native_archive: 'chat-mcp-review',
          updated_at: now - 48 * 60 * 1000,
          status: 'idle',
          instructions: '',
          turns: [
            {
              user: 'Which /mcp/loom tools should require confirmation by default when giving an external agent access to my homelab nodes?',
              reasoning: '',
              assistant: 'Keep read-only inspection frictionless and gate every state-changing operation behind a **Confirm** policy rule:\n\n- **Allow**: listing machines, checking engine health, reading Second Brain notes, inspecting agent session status.\n- **Confirm**: `mcp.tool:loom/loom_machine_run` (shell execution on paired nodes) and `mcp.tool:agents/start_session` (spawning new Codeman sessions).\n- **Deny**: unapproved external provider spend or writes outside the designated workspace.\n\nYou can toggle **Ask before acting** in **Settings → Security & Data** and review individual rules under **Settings → Permissions**.',
              elapsed_ms: 1850,
              toks: 118,
              rate: 63.4,
              prompt_tokens: 520,
            },
          ],
        },
        'chat-rag-eval': {
          id: 'chat-rag-eval',
          title: 'BM25 vs hybrid vault retrieval',
          project_id: 'proj-research',
          runtime_id: 'llama.cpp',
          model: '/srv/models/gguf/Mistral-Small-24B-Instruct-Q4_K_M.gguf',
          provider_id: '',
          provider_name: '',
          native_archive: 'chat-rag-eval',
          updated_at: now - 3 * HOUR,
          status: 'idle',
          instructions: '',
          turns: [
            {
              user: 'Summarize the latency trade-off between pure BM25 and local embedding reranking on a 1,200-note Markdown vault.',
              reasoning: 'Comparing lexical BM25 indexing (<5 ms) against local embedding pass (~45 ms on GPU, ~190 ms on CPU) for top-20 candidate reranking.',
              assistant: 'For a 1,200-note personal Markdown vault (~8 MB plain text):\n\n- **BM25 lexical search**: ~`3 ms` median latency, zero GPU memory, exact match on hostnames, flags, and error codes.\n- **Hybrid (BM25 top-25 → local embedding rerank)**: ~`42 ms` on GPU, improves conceptual recall for loosely phrased questions.\n\nA two-stage pipeline (BM25 first, optional local rerank only when lexical confidence is low) gives the best balance of determinism and recall.',
              elapsed_ms: 1680,
              toks: 102,
              rate: 60.5,
              prompt_tokens: 390,
            },
          ],
        },
      },
      codeman: {
        config: {
          configured: true,
          url: 'https://codeman.demo.invalid',
          username: 'operator',
          password_set: true,
        },
        clis: [
          { id: 'claude', label: 'Claude Code', kind: 'agent', installed: true, version: '2.1.4', latestVersion: '2.1.4', updateAvailable: false },
          { id: 'codex', label: 'Codex', kind: 'agent', installed: true, version: '0.42.0', latestVersion: '0.42.0', updateAvailable: false },
          { id: 'gemini', label: 'Gemini CLI', kind: 'agent', installed: true, version: '0.18.2', latestVersion: '0.18.2', updateAvailable: false },
          { id: 'antigravity', label: 'Antigravity', kind: 'agent', installed: true, version: '1.4.0', latestVersion: '1.4.0', updateAvailable: false },
          { id: 'opencode', label: 'OpenCode', kind: 'agent', installed: false, stock: true, installCommand: 'npm i -g opencode-ai' },
        ],
        sessions: [
          {
            id: 'cm-homelab-audit',
            mode: 'claude',
            name: 'Homelab compose audit',
            workingDir: '/workspace/homelab-ops',
            pid: 4182,
            status: 'idle',
            isWorking: false,
            cliVersion: '2.1.4',
            displayModel: { model: 'claude-sonnet-4-5' },
            createdAt: now - 95 * 60 * 1000,
            lastActivityAt: now - 6 * 60 * 1000,
            tokens: { input: 28400, output: 6120, total: 34520 },
            totalCost: 0.18,
            promptCache: { warm: true, ttl: '4m', hitRatio: 0.84 },
            autoResumeEnabled: true,
            autoCompactEnabled: true,
            autoCompactThreshold: 120000,
            autoClearEnabled: false,
            autoClearThreshold: 0,
            messages: [
              { kind: 'prompt', role: 'user', text: 'Audit compose healthchecks and restart policies across the monitoring stack.' },
              { kind: 'tool', role: 'assistant', text: 'Read compose/monitoring.yml (142 lines)\nRun docker compose config --quiet' },
              {
                kind: 'response',
                role: 'assistant',
                turn: 1,
                text: 'Checked all 6 services in `compose/monitoring.yml`:\n\n- **4 services** already define `healthcheck` + `restart: unless-stopped`.\n- **2 gaps found**: `node-exporter` lacks a readiness probe and `loki` uses default memory limits.\n\nPrepared a clean diff in `compose/monitoring.yml` with bounded memory reservations and HTTP `/ready` probes.',
              },
            ],
            terminalBuffer:
              '\x1b[1;36mclaude\x1b[0m \x1b[2mv2.1.4 · claude-sonnet-4-5 · /workspace/homelab-ops\x1b[0m\r\n' +
              '\x1b[32m✓\x1b[0m Read compose/monitoring.yml (142 lines)\r\n' +
              '\x1b[32m✓\x1b[0m Validated YAML with docker compose config --quiet\r\n' +
              '\x1b[33m●\x1b[0m 2 healthcheck improvements staged in compose/monitoring.yml\r\n\r\n' +
              '\x1b[1m>\x1b[0m Ready for next instruction (simulated demo terminal)\r\n',
          },
          {
            id: 'cm-docs-sync',
            mode: 'codex',
            name: 'Benchmark parser refactor',
            workingDir: '/workspace/research-notes',
            pid: 4291,
            status: 'busy',
            isWorking: true,
            cliVersion: '0.42.0',
            displayModel: { model: 'gpt-5.3-codex' },
            createdAt: now - 35 * 60 * 1000,
            lastActivityAt: now - 45 * 1000,
            tokens: { input: 19200, output: 4850, total: 24050 },
            totalCost: 0.11,
            messages: [
              { kind: 'prompt', role: 'user', text: 'Extract the llama-bench CSV parser into a standalone module with unit tests.' },
              { kind: 'status', role: 'assistant', text: 'Running unit test suite (14/14 passing)...' },
              {
                kind: 'response',
                role: 'assistant',
                turn: 1,
                text: 'Moved CSV normalization into `scripts/bench-parser.mjs` and added deterministic fixtures for `pp512` and `tg128` runs. Verifying edge cases now.',
              },
            ],
            terminalBuffer:
              '\x1b[1;35mcodex\x1b[0m \x1b[2mv0.42.0 · /workspace/research-notes\x1b[0m\r\n' +
              '$ node --test tests/bench-parser.test.mjs\r\n' +
              '\x1b[32m✔ parses pp512 and tg128 rows (1.8ms)\x1b[0m\r\n' +
              '\x1b[32m✔ handles missing VRAM column cleanly (0.9ms)\x1b[0m\r\n' +
              'tests 14 | pass 14 | fail 0\r\n',
          },
          {
            id: 'cm-gpu-bench',
            mode: 'claude',
            name: 'GPU thermal sweep',
            workingDir: '/workspace/homelab-ops',
            pid: 4310,
            status: 'idle',
            isWorking: false,
            cliVersion: '2.1.4',
            displayModel: { model: 'claude-sonnet-4-5' },
            createdAt: now - 22 * 60 * 1000,
            lastActivityAt: now - 2 * 60 * 1000,
            tokens: { input: 9800, output: 2140, total: 11940 },
            totalCost: 0.06,
            messages: [
              { kind: 'prompt', role: 'user', text: 'Run a 3-point power-limit sweep on gpu-node.local and record tok/s per watt.' },
              { kind: 'response', role: 'assistant', turn: 1, text: 'Ready to run the read-only telemetry capture script on `gpu-node.local`. Waiting for your approval before executing the command.' },
            ],
            terminalBuffer:
              '\x1b[1;36mclaude\x1b[0m \x1b[2m· /workspace/homelab-ops\x1b[0m\r\n' +
              '\x1b[33m?\x1b[0m Approval requested: run scripts/gpu-telemetry-sweep.sh --dry-run\r\n',
          },
        ],
        approvals: [
          {
            id: 'appr-gpu-sweep',
            sessionId: 'cm-gpu-bench',
            sessionName: 'GPU thermal sweep',
            kind: 'permission',
            toolName: 'Bash',
            toolSummary: 'scripts/gpu-telemetry-sweep.sh --samples 3 --dry-run',
            message: 'Collect 3 simulated GPU power & throughput samples on gpu-node.local.',
            createdAt: now - 2 * 60 * 1000,
            options: [
              { n: 1, label: 'Allow once' },
              { n: 2, label: 'Allow for session' },
            ],
          },
        ],
        history: [
          {
            sessionId: 'hist-wireguard',
            mode: 'claude',
            firstPrompt: 'Generate WireGuard peer configs for workstation.local and nas-node.local',
            workingDir: '/workspace/homelab-ops',
            lastActivityAt: now - 18 * HOUR,
            sources: ['history'],
            resumeId: 'thread-wg-01',
          },
          {
            sessionId: 'hist-backup-verify',
            mode: 'codex',
            firstPrompt: 'Verify nightly restic snapshot retention policy and prune dry-run report',
            workingDir: '/workspace/homelab-ops',
            lastActivityAt: now - 42 * HOUR,
            sources: ['history'],
            resumeId: 'thread-restic-02',
          },
        ],
        cronJobs: [
          {
            id: 'cron-nightly-logs',
            name: 'Nightly backup & health digest',
            agentType: 'claude',
            workingDir: '/workspace/homelab-ops',
            enabled: true,
            promptMode: 'inline_text',
            promptText: 'Summarize nightly backup status and write a short report to Second Brain.',
            inputMode: 'paste',
            scheduleType: 'daily',
            dailyTime: '02:30',
            nextRunAt: now + 7 * HOUR,
            lastStatus: 'prompt_sent',
            concurrencyPolicy: 'skip_if_same_agent_running',
          },
          {
            id: 'cron-weekly-cve',
            name: 'Weekly container image & CVE check',
            agentType: 'codex',
            workingDir: '/workspace/homelab-ops',
            enabled: true,
            promptMode: 'inline_text',
            promptText: 'Check pinned compose image digests and list any upstream security updates.',
            inputMode: 'paste',
            scheduleType: 'weekly',
            weeklyDays: [1],
            weeklyTime: '08:00',
            nextRunAt: now + 2 * DAY,
            lastStatus: 'prompt_sent',
            concurrencyPolicy: 'skip_if_same_agent_running',
          },
        ],
        cronRuns: [
          {
            id: 'run-01',
            cronJobId: 'cron-nightly-logs',
            sessionId: 'cm-homelab-audit',
            startedAt: now - 17 * HOUR,
            status: 'prompt_sent',
            triggerType: 'scheduled',
          },
        ],
      },
      machines: {
        roles: {
          engine: 'local',
          voice: 'local',
          codeman: 'node-gpu',
          files: 'local',
        },
        remote: [
          {
            id: 'node-gpu',
            name: 'gpu-node.local',
            hostname: 'gpu-node.local',
            host: 'gpu-node.local',
            user: 'operator',
            home: '/home/operator',
            os: 'Linux',
            arch: 'x86_64',
            node_id: 'node-gpu-01',
            version: DEMO_VERSION,
            modules: ['engine', 'terminal', 'observe', 'voice'],
            capabilities: ['terminals', 'engine', 'voice'],
            harnesses: ['claude', 'codex'],
            gpus: [{ name: 'NVIDIA RTX 3090 (Simulated)', total: 24576 }],
            tools: [
              { id: 'claude', version: '2.1.4' },
              { id: 'codex', version: '0.42.0' },
            ],
          },
          {
            id: 'node-nas',
            name: 'nas-node.local',
            hostname: 'nas-node.local',
            host: 'nas-node.local',
            user: 'operator',
            home: '/home/operator',
            os: 'Linux',
            arch: 'x86_64',
            node_id: 'node-nas-02',
            version: DEMO_VERSION,
            modules: ['files', 'terminal', 'observe'],
            capabilities: ['terminals', 'files'],
            harnesses: [],
            gpus: [],
            tools: [],
          },
        ],
        terminals: [
          {
            id: 'term-workstation',
            title: 'homelab-shell',
            target: 'local',
            dir: '/workspace/homelab-ops',
            command: '',
            running: true,
          },
          {
            id: 'term-gpu',
            title: 'gpu-monitor',
            target: 'node-gpu',
            dir: '/home/operator',
            command: 'watch nvidia-smi',
            running: true,
          },
        ],
      },
      files: {
        root: '/home/demo/second-brain',
        notes: {
          'README.md': {
            modified: now - 2 * HOUR,
            content:
              '# Second Brain\n\nA shared Markdown workspace readable by your discussions and Codeman agents, editable directly inside **Loom**.\n\n- Notes auto-save locally to this browser session (`sessionStorage`).\n- Type `/` on an empty line to insert headings, task lists, tables, or code blocks.\n- Click **Reset** in the bottom-right Demo chip at any time to restore the initial notes.\n',
          },
          'homelab/cluster-topology.md': {
            modified: now - 18 * 60 * 1000,
            content:
              '# Homelab Cluster Topology\n\nAll nodes communicate over an encrypted private mesh with capability bricks assigned in **Machines**.\n\n| Node | Role Bricks | Hardware | Status |\n| --- | --- | --- | --- |\n| `workstation.local` | Local AI · Voice · Files | 16-core · 64 GB RAM · RTX 4090 (24 GB) | Online |\n| `gpu-node.local` | Codeman Agents · Remote Engine | 12-core · 64 GB RAM · RTX 3090 (24 GB) | Online |\n| `nas-node.local` | Files · Backups · Observe | 4-core · 16 GB RAM · 32 TB ZFS | Online |\n\n## Active Checklist\n\n- [x] Enable Flash Attention and `Q8_0` KV cache on `workstation.local`\n- [x] Pair `gpu-node.local` via Loom node token\n- [ ] Review nightly backup retention digest from Codeman cron\n',
          },
          'homelab/gpu-tuning-notes.md': {
            modified: now - 5 * HOUR,
            content:
              '# GPU & llama.cpp Tuning Notes\n\n## Router Mode Defaults\n\n- Keep `models_max = 2` so the 14B primary model and 4B fast draft can coexist.\n- Set idle VRAM unload to **30 minutes** to keep standby power low.\n- Protect interactive sessions with a **15-minute grace window** before background API keys can swap weights.\n\n```ini\nCTX=32768\nNGL=999\nEXTRA_ARGS=--flash-attn on --cache-type-k q8_0 --cache-type-v q8_0 -np 2\n```\n',
          },
          'research/local-llm-evals.md': {
            modified: now - 9 * HOUR,
            content:
              '# Local Model Evaluations (Synthetic)\n\nComparison of GGUF quantizations tested in Loom Bench on `workstation.local`:\n\n| Model | Quant | VRAM | Prompt (`pp512`) | Generation (`tg128`) |\n| --- | --- | --- | --- | --- |\n| Qwen3 14B | `Q4_K_M` | 11.3 GiB | 1,420 tok/s | 74.2 tok/s |\n| Llama 3.3 8B Instruct | `Q6_K` | 7.9 GiB | 2,180 tok/s | 112.5 tok/s |\n| Mistral Small 24B | `Q4_K_M` | 16.8 GiB | 910 tok/s | 46.8 tok/s |\n| Gemma 3 4B IT | `Q8_0` | 5.1 GiB | 3,050 tok/s | 164.0 tok/s |\n',
          },
          'runbooks/mcp-policy-guide.md': {
            modified: now - 26 * HOUR,
            content:
              '# MCP Gateway Policy Guide (`/mcp/loom`)\n\nExternal agent clients authenticate with a single scoped bearer token and inherit Loom central policy rules:\n\n1. **Read-only discovery** (machine metrics, file reads, session listing) is allowed by default.\n2. **Remote shell execution** (`mcp.tool:loom/loom_machine_run`) always prompts for human confirmation.\n3. **Agent session spawning** (`mcp.tool:agents/start_session`) requires confirmation and logs every decision to the bounded audit trail.\n',
          },
        },
      },
      voice: {
        machine: 'local',
        running: true,
        config: {
          stt: 'whisper-small-int8',
          tts: 'kokoro-en-v1',
          vad: 'silero-vad-v5',
          voice: 0,
          speed: 1.0,
          endpoint_silence: 0.7,
          idle_unload_minutes: 30,
          boot: false,
        },
        jarvis: {
          name: 'Jarvis',
          language: 'auto',
          formality: 'tu',
          length: 'short',
          personality: 'Concise, calm homelab assistant. Keep spoken answers under three sentences unless asked for detail.',
          model: 'discussion',
          fallback: '',
          context: 'discussion+memory',
          voice: { tts_model: '', voice_id: null, speed: null },
        },
      },
      quotas: {
        reset5h,
        reset7d,
        codex5h,
        codex7d,
        claudeResets: 1,
        codexCredits: [
          { id: 'codex-credit-1', title: 'Weekly bonus reset', expires_at: now + 11 * DAY },
        ],
        samples: buildQuotaSamples(now, reset5h, reset7d, codex5h, codex7d),
        tokenDaily: buildTokenDaily(now),
      },
      market: {
        catalog_at: now - 22 * 60 * 1000,
        bench_at: now - 2 * HOUR,
        watch: { notify: true, open_only: true },
        new: [
          { id: 'qwen/qwen3-14b', name: 'Qwen3 14B', creator: 'Qwen', created: nowSec - 6 * 86400, context: 40960, input_price: 0.08, output_price: 0.24, hf_id: 'Qwen/Qwen3-14B', free: false },
          { id: 'mistralai/mistral-small-24b', name: 'Mistral Small 24B', creator: 'Mistral', created: nowSec - 11 * 86400, context: 32768, input_price: 0.1, output_price: 0.3, hf_id: 'mistralai/Mistral-Small-24B-Instruct-2501', free: false },
          { id: 'google/gemma-3-12b-it', name: 'Gemma 3 12B IT', creator: 'Google', created: nowSec - 15 * 86400, context: 131072, input_price: 0, output_price: 0, hf_id: 'google/gemma-3-12b-it', free: true },
        ],
        bench: [
          { name: 'Claude Sonnet 4.5', creator: 'Anthropic', released: '2026-02-18', score: 146.4, low: 144.8, high: 148.0, open: false },
          { name: 'GPT-5.3 Codex', creator: 'OpenAI', released: '2026-03-04', score: 145.8, low: 144.1, high: 147.4, open: false },
          { name: 'Gemini 2.5 Pro', creator: 'Google', released: '2026-01-29', score: 143.9, low: 142.3, high: 145.5, open: false },
          { name: 'DeepSeek R1', creator: 'DeepSeek', released: '2025-12-12', score: 138.6, low: 136.9, high: 140.2, open: true },
          { name: 'Qwen3 235B A22B', creator: 'Qwen', released: '2026-02-05', score: 137.4, low: 135.7, high: 139.0, open: true },
          { name: 'Qwen3 14B', creator: 'Qwen', released: '2026-02-05', score: 126.2, low: 124.5, high: 127.8, open: true },
          { name: 'Mistral Small 24B', creator: 'Mistral', released: '2026-01-22', score: 123.5, low: 121.8, high: 125.1, open: true },
        ],
        catalog: [
          { id: 'anthropic/claude-sonnet-4-5', name: 'Claude Sonnet 4.5', creator: 'Anthropic', created: nowSec - 45 * 86400, context: 200000, input_price: 3.0, output_price: 15.0, hf_id: '', free: false },
          { id: 'openai/gpt-4.1', name: 'GPT-4.1', creator: 'OpenAI', created: nowSec - 62 * 86400, context: 1000000, input_price: 2.0, output_price: 8.0, hf_id: '', free: false },
          { id: 'deepseek/deepseek-r1', name: 'DeepSeek R1', creator: 'DeepSeek', created: nowSec - 80 * 86400, context: 128000, input_price: 0.55, output_price: 2.19, hf_id: 'deepseek-ai/DeepSeek-R1', free: false },
          { id: 'qwen/qwen3-14b', name: 'Qwen3 14B', creator: 'Qwen', created: nowSec - 6 * 86400, context: 40960, input_price: 0.08, output_price: 0.24, hf_id: 'Qwen/Qwen3-14B', free: false },
          { id: 'mistralai/mistral-small-24b', name: 'Mistral Small 24B', creator: 'Mistral', created: nowSec - 11 * 86400, context: 32768, input_price: 0.1, output_price: 0.3, hf_id: 'mistralai/Mistral-Small-24B-Instruct-2501', free: false },
          { id: 'google/gemma-3-12b-it', name: 'Gemma 3 12B IT', creator: 'Google', created: nowSec - 15 * 86400, context: 131072, input_price: 0, output_price: 0, hf_id: 'google/gemma-3-12b-it', free: true },
        ],
      },
      policy: {
        rules: [
          { id: 'agent-access-ask-run', scope: 'global', subject: 'mcp.tool:loom/loom_machine_run', decision: 'confirm', conditions: {} },
          { id: 'agent-access-ask-session', scope: 'global', subject: 'mcp.tool:agents/start_session', decision: 'confirm', conditions: {} },
          { id: 'ui-global-data-send-provider', scope: 'global', subject: 'data.send_provider', decision: 'confirm', conditions: {} },
          { id: 'rule-homelab-write', scope: 'project', scope_id: 'proj-homelab', subject: 'agent.file_write', decision: 'allow', conditions: { inside_workdir: true } },
        ],
        audit: [
          { id: 'aud-1', subject: 'mcp.tool:loom/loom_machine_run', decision: 'confirm', at: now - 14 * 60 * 1000, dry_run: false },
          { id: 'aud-2', subject: 'data.send_provider', decision: 'allow', at: now - 49 * 60 * 1000, dry_run: false },
          { id: 'aud-3', subject: 'agent.file_write', decision: 'allow', at: now - 2 * HOUR, dry_run: false },
        ],
      },
      mcpGateway: {
        token_set: true,
        agents: 'available',
        last_client: 'claude-code',
        last_used: nowSec - 380,
      },
      engineKeys: [
        {
          id: 'key-ide',
          name: 'Local IDE autocomplete',
          priority: 'interactive',
          max_concurrency: 2,
          requests_per_minute: 60,
          allowed_models: ['Qwen3-14B-Q4_K_M', 'Gemma-3-4B-IT-Q8_0'],
          last_used_at: now - 12 * 60 * 1000,
          usage: { requests: 184, prompt_tokens: 412000, completion_tokens: 38900 },
        },
        {
          id: 'key-batch',
          name: 'Nightly note summarizer',
          priority: 'background',
          max_concurrency: 1,
          requests_per_minute: 15,
          allowed_models: ['Gemma-3-4B-IT-Q8_0'],
          last_used_at: now - 9 * HOUR,
          usage: { requests: 42, prompt_tokens: 128000, completion_tokens: 14200 },
        },
      ],
      usagePrices: {
        'cloud:prov-anthropic:claude-sonnet-4-5': { input_per_million: 3, output_per_million: 15, currency: 'USD' },
        'cloud:prov-openai:gpt-4.1': { input_per_million: 2, output_per_million: 8, currency: 'USD' },
      },
      internet: { enabled: true, engine: 'go', url: '', key_set: false, key_hint: '' },
      search: { provider: 'duckduckgo', url: '', key_set: false },
      https: { enabled: false, running: false, port: 2543, mode: 'self-signed', urls: [] },
      networkWeb: { exposed: false, password_set: true, url: 'http://workstation.local:2510', port: 2510, restart: false },
      memoryMode: 'ondemand',
      vault: { encrypted: true, fully: true, locked: false, vault_copies: 1 },
      hubDownloads: {},
    };
  }

  const initialLang = bootstrapLanguage();
  bootstrapUiDefaults();

  function loadState() {
    try {
      const raw = sessionStorage.getItem(STORAGE_KEY);
      if (raw) {
        const parsed = JSON.parse(raw);
        if (parsed && typeof parsed === 'object' && parsed.discussions) {
          parsed.lang = initialLang;
          parsed.prefs = { ...(parsed.prefs || {}), lang: initialLang, onboarded: '1' };
          return parsed;
        }
      }
    } catch (_) {}
    return createInitialState(initialLang);
  }

  let state = loadState();
  function saveState() {
    try {
      sessionStorage.setItem(STORAGE_KEY, JSON.stringify(state));
    } catch (_) {}
  }

  // ---------- 3. Helpers ----------
  function jsonResponse(data, status = 200) {
    return new Response(JSON.stringify(data), {
      status,
      headers: { 'Content-Type': 'application/json; charset=utf-8' },
    });
  }

  function buildWorkspacePayload() {
    const models = [];
    for (const m of state.models) {
      models.push({
        id: 'local:' + m.path,
        name: m.name.replace(/\.gguf$/i, ''),
        model: m.path,
        engine_value: m.path,
        kind: 'local',
        runtime_id: 'llama.cpp',
        enabled: true,
        ready: true,
        reasoning_efforts: m.thinks ? ['low', 'medium', 'high'] : [],
      });
    }
    for (const p of state.providers) {
      for (const m of p.models) {
        models.push({
          id: `cloud:${p.id}:${m}`,
          name: m,
          model: m,
          kind: 'cloud',
          runtime_id: 'openai-compatible',
          provider_id: p.id,
          provider_name: p.name,
          endpoint: p.endpoint,
          enabled: true,
          ready: !!p.ready,
          reasoning_efforts: [],
        });
      }
    }
    return {
      ok: true,
      models,
      runtimes: [
        { id: 'llama.cpp', name: 'llama.cpp (Local)', kind: 'local', capabilities: ['chat', 'reasoning', 'tools'], degraded: [], filesystem_policies: [] },
        { id: 'openai-compatible', name: 'Cloud API', kind: 'cloud', capabilities: ['chat', 'tools'], degraded: [], filesystem_policies: [] },
      ],
      providers: state.providers,
      projects: state.projects,
      capabilities: [],
      machines: state.machines.remote,
      chat: { default_choice: 'local:' + state.models[0].path },
    };
  }

  function buildSessionObject(d) {
    return {
      id: d.id,
      title: d.title,
      project_id: d.project_id || '',
      runtime_id: d.runtime_id || 'llama.cpp',
      model: d.model,
      provider_id: d.provider_id || '',
      provider_name: d.provider_name || '',
      endpoint: (state.providers.find(p => p.id === d.provider_id) || {}).endpoint || '',
      native_archive: d.native_archive || '',
      status: d.status || 'idle',
      message_count: d.turns.length * 2,
      updated_at: d.updated_at,
      instructions: d.instructions || '',
      messages: [],
      turns: [],
      available_modes: [],
      available_config_options: [],
      changed_files: [],
      commands: [],
      context: { revision: 'rev-' + d.updated_at, project_id: d.project_id || '', items: [], skills: [], reference_ids: [], brain_citations: [] },
    };
  }

  function buildFileTree() {
    const root = [];
    const dirs = new Map();
    const ensureDir = (dirPath) => {
      if (dirs.has(dirPath)) return dirs.get(dirPath);
      const parts = dirPath.split('/');
      const name = parts[parts.length - 1];
      const node = { name, path: dirPath, dir: true, children: [], modified: Date.now() };
      dirs.set(dirPath, node);
      if (parts.length === 1) {
        root.push(node);
      } else {
        const parent = ensureDir(parts.slice(0, -1).join('/'));
        parent.children.push(node);
      }
      return node;
    };
    const keys = Object.keys(state.files.notes).sort();
    for (const rel of keys) {
      const meta = state.files.notes[rel];
      const parts = rel.split('/');
      const name = parts[parts.length - 1];
      const fileNode = { name, path: rel, dir: false, modified: meta.modified || Date.now() };
      if (parts.length === 1) root.push(fileNode);
      else ensureDir(parts.slice(0, -1).join('/')).children.push(fileNode);
    }
    const sortNodes = (list) => {
      list.sort((a, b) => (b.dir - a.dir) || a.name.localeCompare(b.name));
      for (const n of list) if (n.dir) sortNodes(n.children);
    };
    sortNodes(root);
    return root;
  }

  // ---------- 4. Live Discussion SSE Stream Hub ----------
  const activeDiscussionStreams = new Set();
  const activeCodemanStreams = new Set();
  let streamingAbort = false;

  function emitDiscussionEvent(sessionId, delta) {
    const line = `data: ${JSON.stringify({ choices: [{ delta }] })}\n\n`;
    const enc = new TextEncoder().encode(line);
    for (const sub of activeDiscussionStreams) {
      if (sub.sessionId === sessionId || (!sub.sessionId && sessionId === state.activeChatId)) {
        try { sub.controller.enqueue(enc); } catch (_) {}
      }
    }
  }

  function emitCodemanEvent(eventName, payload) {
    const frame = `event: ${eventName}\ndata: ${JSON.stringify(payload)}\n\n`;
    const enc = new TextEncoder().encode(frame);
    for (const sub of activeCodemanStreams) {
      try { sub.controller.enqueue(enc); } catch (_) {}
    }
  }

  function generateDemoReply(promptText, discussion) {
    const isFr = state.lang === 'fr';
    const q = String(promptText || '').toLowerCase();
    const modelShort = String(discussion.model || '').split('/').pop().replace(/\.gguf$/i, '');

    if (/vram|gpu|quant|llama|ctx|context/.test(q)) {
      return {
        reasoning: isFr
          ? 'Estimation rapide du budget VRAM avec Flash Attention et quantification du cache KV en Q8_0.'
          : 'Estimating VRAM budget with Flash Attention and Q8_0 KV cache quantization for the selected model.',
        answer: isFr
          ? `Simulation locale sur **\`${modelShort}\`** :\n\n- **Cache KV \`q8_0\`** : divise presque par deux l'empreinte mémoire du contexte long.\n- **Flash Attention** : garde le débit stable au-delà de \`16K\` tokens.\n- Ouvrez le panneau de droite (ou **Modèles → Bibliothèque**) pour tester l'estimateur VRAM interactif en temps réel.`
          : `Simulated local estimate for **\`${modelShort}\`**:\n\n- **\`q8_0\` KV cache**: cuts long-context VRAM footprint roughly in half with minimal perplexity impact.\n- **Flash Attention**: keeps prompt evaluation steady past \`16K\` tokens.\n- Open the right-hand inspector (or **Models → Library**) to adjust context and GPU layers interactively.`,
      };
    }
    if (/agent|codeman|claude|codex|cron/.test(q)) {
      return {
        reasoning: isFr
          ? 'Synthèse de la Passerelle Agents Codeman et des sessions actives dans le bac à sable.'
          : 'Summarizing the connected Codeman agent gateway and active demo sessions.',
        answer: isFr
          ? `**Codeman** est simulé comme connecté (\`codeman.demo.invalid\`) avec **${state.codeman.sessions.length} sessions actives** et **${state.codeman.cronJobs.length} tâches planifiées**.\n\nVous pouvez cliquer sur une session d'agent directement dans la barre latérale unifiée pour inspecter sa transcription, son terminal simulé et les demandes d'approbation.`
          : `**Codeman** is simulated as connected (\`codeman.demo.invalid\`) with **${state.codeman.sessions.length} active sessions** and **${state.codeman.cronJobs.length} scheduled cron jobs**.\n\nClick any agent session in the unified sidebar to inspect its transcript, simulated terminal output, and pending approvals.`,
      };
    }
    if (/mcp|policy|security|secu|permission|token/.test(q)) {
      return {
        reasoning: isFr
          ? 'Vérification des règles actives de la passerelle /mcp/loom.'
          : 'Checking active policy rules configured for the /mcp/loom gateway.',
        answer: isFr
          ? `Dans **Réglages → Sécurité et données**, la passerelle \`/mcp/loom\` est configurée avec la règle **Demander avant d'agir** active :\n\n- Lecture des machines & du second cerveau : **Autorisée**\n- Exécution distante (\`loom_machine_run\`) & lancement d'agents : **Confirmation requise**\n\nAucune vraie commande n'est jamais exécutée dans cette démonstration publique.`
          : `Under **Settings → Security & Data**, the \`/mcp/loom\` gateway has **Ask before acting** enabled:\n\n- Machine inspection & Second Brain reads: **Allowed**\n- Remote commands (\`loom_machine_run\`) & spawning agents: **Confirm required**\n\nNo real host commands are ever executed in this isolated public demo.`,
      };
    }
    return {
      reasoning: isFr
        ? `Préparation d'une réponse simulée en streaming pour « ${promptText.slice(0, 60)} » via ${modelShort}.`
        : `Drafting a simulated streaming response for "${promptText.slice(0, 60)}" using ${modelShort}.`,
      answer: isFr
        ? `Réponse simulée de **\`${modelShort}\`** dans le bac à sable Loom :\n\n> **Votre message :** ${promptText.replace(/\n/g, ' ')}\n\nTout dans cette interface fonctionne en local dans votre navigateur :\n- **Discussions** : changez de modèle local ou cloud sans perdre l'historique.\n- **Agents** : pilotez vos sessions Codeman et vos tâches cron.\n- **Fichiers** : éditez les notes Markdown du Second Cerveau.\n- **Activité** : suivez les quotas multi-harness, l'usage et la veille modèles.`
        : `Simulated reply from **\`${modelShort}\`** in the Loom browser sandbox:\n\n> **Your prompt:** ${promptText.replace(/\n/g, ' ')}\n\nEverything in this preview runs client-side with fictional fixtures:\n- **Discussions**: switch between Local GGUF presets and Cloud models in the same thread.\n- **Agents**: monitor simulated Codeman sessions, terminals, and cron schedules.\n- **Files**: edit Second Brain Markdown notes (persisted in \`sessionStorage\`).\n- **Activity**: explore multi-harness quotas, token usage, and Model Watch.`,
    };
  }

  function simulateStreamingTurn(discussion, promptText) {
    streamingAbort = false;
    discussion.status = 'running';
    const seqBase = (discussion.turns.length + 1) * 100;
    const startedAt = Date.now();
    const prov = {
      runtime_id: discussion.runtime_id,
      model: discussion.model,
      provider_name: discussion.provider_name || '',
      started_at: startedAt,
    };

    emitDiscussionEvent(discussion.id, {
      type: 'turn_start',
      text: promptText,
      seq: seqBase + 1,
      provenance: prov,
    });

    const { reasoning, answer } = generateDemoReply(promptText, discussion);
    const reasonChunks = reasoning && discussion.runtime_id === 'llama.cpp' ? reasoning.match(/.{1,24}/g) || [] : [];
    const answerChunks = answer.match(/[\s\S]{1,18}/g) || [answer];

    let step = 0;
    let seq = seqBase + 2;
    let genTokens = 0;

    const tick = () => {
      if (streamingAbort) {
        discussion.status = 'idle';
        emitDiscussionEvent(discussion.id, {
          type: 'turn_done',
          seq: ++seq,
          elapsed_ms: Date.now() - startedAt,
          metrics: { stats: { gen_per_second: 72.4 } },
          provenance: { ...prov, usage: { prompt_tokens: 420, completion_tokens: genTokens } },
        });
        saveState();
        return;
      }

      if (step < reasonChunks.length) {
        const chunk = reasonChunks[step++];
        genTokens += 4;
        emitDiscussionEvent(discussion.id, {
          type: 'reasoning_delta',
          text: chunk,
          toks: 4,
          seq: ++seq,
        });
        setTimeout(tick, 28);
        return;
      }

      const aIdx = step - reasonChunks.length;
      if (aIdx < answerChunks.length) {
        const chunk = answerChunks[aIdx];
        step++;
        genTokens += 3;
        emitDiscussionEvent(discussion.id, {
          type: 'text_delta',
          text: chunk,
          toks: 3,
          seq: ++seq,
        });
        setTimeout(tick, 24);
        return;
      }

      const elapsed = Math.max(450, Date.now() - startedAt);
      const rate = 74.6;
      discussion.status = 'idle';
      discussion.updated_at = Date.now();
      discussion.turns.push({
        user: promptText,
        reasoning: reasonChunks.length ? reasoning : '',
        assistant: answer,
        elapsed_ms: elapsed,
        toks: genTokens,
        rate,
        prompt_tokens: 460 + discussion.turns.length * 180,
      });
      if (discussion.turns.length === 1 && (!discussion.title || discussion.title === 'New discussion')) {
        discussion.title = promptText.trim().slice(0, 46);
      }
      saveState();

      emitDiscussionEvent(discussion.id, {
        type: 'usage',
        seq: ++seq,
        metrics: { prompt_tokens_total: 460 + discussion.turns.length * 180, gen_tokens: genTokens, gen_per_second: rate },
        provenance: { ...prov, usage: { prompt_tokens: 460 + discussion.turns.length * 180, completion_tokens: genTokens } },
      });
      emitDiscussionEvent(discussion.id, {
        type: 'turn_done',
        seq: ++seq,
        elapsed_ms: elapsed,
        metrics: { stats: { gen_per_second: rate } },
        provenance: { ...prov, usage: { prompt_tokens: 460 + discussion.turns.length * 180, completion_tokens: genTokens } },
      });
    };

    setTimeout(tick, 60);
  }

  function createDiscussionStreamResponse(requestedId) {
    const id = requestedId || state.activeChatId;
    const d = state.discussions[id] || null;
    let subRef = null;
    let heartbeat = null;

    const stream = new ReadableStream({
      start(controller) {
        subRef = { sessionId: requestedId || '', controller };
        activeDiscussionStreams.add(subRef);

        const pushDelta = (delta) => {
          const line = `data: ${JSON.stringify({ choices: [{ delta }] })}\n\n`;
          controller.enqueue(new TextEncoder().encode(line));
        };

        let seq = 0;
        pushDelta({ reset: true, replay: true, seq: ++seq });
        if (d) {
          const sess = buildSessionObject(d);
          pushDelta({ session: sess, context: sess.context, seq: ++seq });
          let ctxUsed = 0;
          for (const turn of d.turns) {
            const prov = {
              runtime_id: d.runtime_id,
              model: d.model,
              provider_name: d.provider_name || '',
              usage: { prompt_tokens: turn.prompt_tokens || 320, completion_tokens: turn.toks || 90 },
            };
            pushDelta({ type: 'turn_start', text: turn.user, seq: ++seq, provenance: prov });
            if (turn.reasoning) {
              pushDelta({ type: 'reasoning_delta', text: turn.reasoning, toks: 32, seq: ++seq });
            }
            pushDelta({ type: 'text_delta', text: turn.assistant, toks: turn.toks || 90, seq: ++seq });
            ctxUsed = (turn.prompt_tokens || 320) + (turn.toks || 90);
            pushDelta({
              type: 'usage',
              seq: ++seq,
              metrics: { prompt_tokens_total: turn.prompt_tokens || 320, gen_tokens: turn.toks || 90, gen_per_second: turn.rate || 72.5 },
              provenance: prov,
            });
            pushDelta({
              type: 'turn_done',
              seq: ++seq,
              elapsed_ms: turn.elapsed_ms || 1400,
              metrics: { stats: { gen_per_second: turn.rate || 72.5 } },
              provenance: prov,
            });
          }
          pushDelta({ ctx_used: ctxUsed, caught_up: true, session: sess, seq: ++seq });
        } else {
          pushDelta({ caught_up: true, seq: ++seq });
        }

        heartbeat = setInterval(() => {
          try {
            controller.enqueue(new TextEncoder().encode(': heartbeat\n\n'));
          } catch (_) {
            clearInterval(heartbeat);
          }
        }, 12000);
      },
      cancel() {
        clearInterval(heartbeat);
        if (subRef) activeDiscussionStreams.delete(subRef);
      },
    });

    return new Response(stream, {
      status: 200,
      headers: {
        'Content-Type': 'text/event-stream; charset=utf-8',
        'Cache-Control': 'no-cache',
      },
    });
  }

  function createCodemanEventsResponse() {
    let subRef = null;
    let heartbeat = null;
    const stream = new ReadableStream({
      start(controller) {
        subRef = { controller };
        activeCodemanStreams.add(subRef);
        controller.enqueue(new TextEncoder().encode('event: sse:heartbeat\ndata: {}\n\n'));
        heartbeat = setInterval(() => {
          try {
            controller.enqueue(new TextEncoder().encode('event: sse:heartbeat\ndata: {}\n\n'));
          } catch (_) {
            clearInterval(heartbeat);
          }
        }, 14000);
      },
      cancel() {
        clearInterval(heartbeat);
        if (subRef) activeCodemanStreams.delete(subRef);
      },
    });
    return new Response(stream, {
      status: 200,
      headers: {
        'Content-Type': 'text/event-stream; charset=utf-8',
        'Cache-Control': 'no-cache',
      },
    });
  }

  // ---------- 5. Engine parameter catalog for Inspector ----------
  const ENGINE_PARAMS = [
    // --- Essential tier (exact parity with internal/loom/engine/llamacpp/params/llamacpp.json) ---
    { id: 'ctx-size', key: 'CTX', flag: '--ctx-size', tier: 'essential', kind: 'slider', control: 'context', label: 'Context', tip: 'Token window. The slider goes up to the model’s native limit; type a value to go beyond it.', min: 1024, step: 1024, stack: true, affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'gpu-layers', key: 'NGL', flag: '--gpu-layers', tier: 'essential', kind: 'slider', control: 'layers', label: 'GPU layers', tip: 'Number of layers offloaded to the GPU. “All” offloads every layer.', min: 0, step: 1, stack: true, affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'reasoning', key: 'REASONING', flag: '--reasoning', tier: 'essential', kind: 'bool', control: 'reasoning', label: 'Reasoning', tip: 'The model reasons before answering if its template supports it. Turning this off sends --reasoning off.', when: 'thinks', affects_vram: false, requires_reload: true, available: true, supported: true },
    { id: 'reasoning-effort', key: 'REASONING_EFFORT', flag: '--reasoning-effort', tier: 'essential', kind: 'enum', control: 'effort', label: 'Effort', tip: 'Levels read from the model template. Blank uses the template default.', when: 'effort', affects_vram: false, requires_reload: true, available: true, supported: true },
    { id: 'vision', key: 'MMPROJ', flag: '--mmproj', tier: 'essential', kind: 'bool', control: 'vision', label: 'Vision', tip: 'Loads the mmproj projector found beside the model.', when: 'vision', affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'temp', key: 'TEMP', flag: '--temp', tier: 'essential', kind: 'number', label: 'Temperature', tip: 'Sampling randomness. Blank uses the model default.', step: 0.05, placeholder: '0.7', affects_vram: false, requires_reload: true, available: true, supported: true },
    { id: 'sysprompt', key: 'SYSPROMPT', tier: 'essential', kind: 'textarea', label: 'System prompt', tip: 'Specific to this model or preset. Blank uses the prompt from Settings.', stack: true, placeholder: 'Blank = prompt from Settings', affects_vram: false, requires_reload: true, available: true, supported: true },

    // --- Advanced tier (all 15 llama.cpp tuning controls) ---
    { id: 'kv', tier: 'advanced', kind: 'enum', control: 'kv', label: 'KV cache', tip: 'Context cache compression. f16 gives maximum quality; q8/q4 reduce VRAM use.', choices: [['', 'f16'], ['q8_0|q8_0', 'q8_0'], ['q5_1|q5_1', 'q5_1'], ['q4_0|q4_0', 'q4_0'], ['q8_0|q5_1', 'q8_0 / q5_1'], ['q8_0|q5_0', 'q8_0 / q5_0']], affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'parallel', key: 'NP', flag: '--parallel', tier: 'advanced', kind: 'number', control: 'parallel', label: 'Parallel slots', tip: 'Requests handled simultaneously by llama.cpp (continuous batching).', step: 1, placeholder: '4', affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'spec-type', flag: '--spec-type', tier: 'advanced', kind: 'enum', label: 'Speculative decoding', tip: 'Predicts tokens (MTP, EAGLE, n-grams) to speed up generation.', choices: [['', 'no'], ['draft-mtp', 'MTP'], ['draft-eagle3', 'EAGLE-3'], ['draft-simple', 'Draft model'], ['ngram-simple', 'n-grams']], affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'flash-attn', flag: '--flash-attn', tier: 'advanced', kind: 'enum', label: 'Flash attention', tip: 'Faster and more memory efficient. Auto lets llama.cpp decide.', requires_flag: true, choices: [['', 'auto'], ['on', 'on'], ['off', 'off']], affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'fit', key: 'FIT', flag: '--fit', tier: 'advanced', kind: 'bool', control: 'fit', label: 'Fit to VRAM', tip: 'Lets llama.cpp reduce unset parameters to fit in memory.', requires_flag: true, affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'batch-size', flag: '--batch-size', tier: 'advanced', kind: 'number', label: 'Batch', tip: 'Logical batch size for prompts. Larger is faster but uses more VRAM.', requires_flag: true, step: 1, placeholder: '2048', affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'ubatch-size', flag: '--ubatch-size', tier: 'advanced', kind: 'number', label: 'Micro-batch', tip: 'Physical micro-batch size. 512 is a good starting point.', requires_flag: true, step: 1, placeholder: '512', affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'threads', flag: '--threads', tier: 'advanced', kind: 'number', label: 'CPU threads', tip: '0 = automatic.', requires_flag: true, step: 1, placeholder: '0', affects_vram: false, requires_reload: true, available: true, supported: true },
    { id: 'threads-batch', flag: '--threads-batch', tier: 'advanced', kind: 'number', label: 'Threads (prompt)', tip: '0 = same as CPU threads.', requires_flag: true, step: 1, placeholder: '0', affects_vram: false, requires_reload: true, available: true, supported: true },
    { id: 'n-cpu-moe', flag: '--n-cpu-moe', tier: 'advanced', kind: 'number', label: 'MoE experts on CPU', tip: 'Keeps the first N expert layers on the CPU.', requires_flag: true, step: 1, placeholder: '0', affects_vram: true, requires_reload: true, available: true, supported: true },
    { id: 'top-p', flag: '--top-p', tier: 'advanced', kind: 'number', label: 'top_p', tip: '1 = disabled.', requires_flag: true, step: 0.05, placeholder: '0.95', affects_vram: false, requires_reload: true, available: true, supported: true },
    { id: 'top-k', flag: '--top-k', tier: 'advanced', kind: 'number', label: 'top_k', tip: '0 = disabled.', requires_flag: true, step: 1, placeholder: '40', affects_vram: false, requires_reload: true, available: true, supported: true },
    { id: 'min-p', flag: '--min-p', tier: 'advanced', kind: 'number', label: 'min_p', tip: '0 = disabled.', requires_flag: true, step: 0.01, placeholder: '0.05', affects_vram: false, requires_reload: true, available: true, supported: true },
    { id: 'repeat-penalty', flag: '--repeat-penalty', tier: 'advanced', kind: 'number', label: 'Repetition penalty', tip: '1 = neutral.', requires_flag: true, step: 0.05, placeholder: '1.05', affects_vram: false, requires_reload: true, available: true, supported: true },
    { id: 'presence-penalty', flag: '--presence-penalty', tier: 'advanced', kind: 'number', label: 'Presence penalty', tip: '0 = neutral.', requires_flag: true, step: 0.05, placeholder: '0.0', affects_vram: false, requires_reload: true, available: true, supported: true },

    // --- Expert tier (searchable llama.cpp native flags) ---
    { id: 'seed', flag: '--seed', tier: 'expert', kind: 'number', label: '--seed', tip: 'RNG seed (-1 for random).', default: '-1', available: true, supported: true },
    { id: 'split-mode', flag: '--split-mode', tier: 'expert', kind: 'enum', label: '--split-mode', tip: 'How to split the model across multiple GPUs.', choices: [['none', 'none'], ['layer', 'layer'], ['row', 'row']], available: true, supported: true },
    { id: 'tensor-split', flag: '--tensor-split', tier: 'expert', kind: 'string', label: '--tensor-split', tip: 'Fraction of the model to offload to each GPU (comma-separated).', default: '', available: true, supported: true },
    { id: 'main-gpu', flag: '--main-gpu', tier: 'expert', kind: 'number', label: '--main-gpu', tip: 'GPU index used for scratch and small tensors.', default: '0', available: true, supported: true },
    { id: 'no-kv-offload', flag: '--no-kv-offload', tier: 'expert', kind: 'bool', label: '--no-kv-offload', tip: 'Disable KV cache offload to GPU (keep KV in system RAM).', available: true, supported: true },
    { id: 'rope-scaling', flag: '--rope-scaling', tier: 'expert', kind: 'enum', label: '--rope-scaling', tip: 'RoPE frequency scaling method for extended context.', choices: [['none', 'none'], ['linear', 'linear'], ['yarn', 'yarn']], available: true, supported: true },
    { id: 'rope-freq-base', flag: '--rope-freq-base', tier: 'expert', kind: 'number', label: '--rope-freq-base', tip: 'RoPE base frequency (0 = loaded from model).', default: '0', available: true, supported: true },
    { id: 'rope-freq-scale', flag: '--rope-freq-scale', tier: 'expert', kind: 'number', label: '--rope-freq-scale', tip: 'RoPE frequency scaling factor.', default: '1.0', available: true, supported: true },
    { id: 'cache-reuse', flag: '--cache-reuse', tier: 'expert', kind: 'number', label: '--cache-reuse', tip: 'Minimum chunk size to attempt reusing from KV cache via shifting.', default: '0', available: true, supported: true },
    { id: 'slot-prompt-similarity', flag: '--slot-prompt-similarity', tier: 'expert', kind: 'number', label: '--slot-prompt-similarity', tip: 'Minimum prompt similarity ratio to reuse an existing slot.', default: '0.5', available: true, supported: true },
    { id: 'defrag-thold', flag: '--defrag-thold', tier: 'expert', kind: 'number', label: '--defrag-thold', tip: 'KV cache defragmentation fragmentation threshold.', default: '0.1', available: true, supported: true },
    { id: 'swa-full', flag: '--swa-full', tier: 'expert', kind: 'bool', label: '--swa-full', tip: 'Use full-size SWA cache for sliding-window attention models.', available: true, supported: true },
    { id: 'reasoning-format', flag: '--reasoning-format', tier: 'expert', kind: 'enum', label: '--reasoning-format', tip: 'How reasoning tags are parsed in completion responses.', choices: [['deepseek', 'deepseek'], ['auto', 'auto'], ['none', 'none']], available: true, supported: true },
    { id: 'reasoning-budget', flag: '--reasoning-budget', tier: 'expert', kind: 'number', label: '--reasoning-budget', tip: 'Maximum thinking tokens (-1 = unlimited, 0 = disabled).', default: '-1', available: true, supported: true },
    { id: 'chat-template', flag: '--chat-template', tier: 'expert', kind: 'string', label: '--chat-template', tip: 'Override built-in Jinja chat template name.', default: '', available: true, supported: true },
    { id: 'dynatemp-range', flag: '--dynatemp-range', tier: 'expert', kind: 'number', label: '--dynatemp-range', tip: 'Dynamic temperature range around base temperature.', default: '0.0', available: true, supported: true },
    { id: 'dynatemp-exp', flag: '--dynatemp-exp', tier: 'expert', kind: 'number', label: '--dynatemp-exp', tip: 'Dynamic temperature exponent.', default: '1.0', available: true, supported: true },
    { id: 'typical', flag: '--typical', tier: 'expert', kind: 'number', label: '--typical', tip: 'Locally typical sampling p (1.0 = disabled).', default: '1.0', available: true, supported: true },
    { id: 'xtc-probability', flag: '--xtc-probability', tier: 'expert', kind: 'number', label: '--xtc-probability', tip: 'XTC sampler probability (0.0 = disabled).', default: '0.0', available: true, supported: true },
    { id: 'xtc-threshold', flag: '--xtc-threshold', tier: 'expert', kind: 'number', label: '--xtc-threshold', tip: 'XTC sampler probability threshold.', default: '0.1', available: true, supported: true },
    { id: 'dry-multiplier', flag: '--dry-multiplier', tier: 'expert', kind: 'number', label: '--dry-multiplier', tip: 'DRY repetition penalty multiplier (0.0 = disabled).', default: '0.0', available: true, supported: true },
    { id: 'dry-base', flag: '--dry-base', tier: 'expert', kind: 'number', label: '--dry-base', tip: 'DRY repetition penalty base value.', default: '1.75', available: true, supported: true },
    { id: 'dry-allowed-length', flag: '--dry-allowed-length', tier: 'expert', kind: 'number', label: '--dry-allowed-length', tip: 'Minimum sequence length before DRY penalty applies.', default: '2', available: true, supported: true },
    { id: 'frequency-penalty', flag: '--frequency-penalty', tier: 'expert', kind: 'number', label: '--frequency-penalty', tip: 'Repeat alpha frequency penalty (0.0 = disabled).', default: '0.0', available: true, supported: true },
    { id: 'repeat-last-n', flag: '--repeat-last-n', tier: 'expert', kind: 'number', label: '--repeat-last-n', tip: 'Last N tokens considered for repetition penalty.', default: '64', available: true, supported: true },
    { id: 'mirostat', flag: '--mirostat', tier: 'expert', kind: 'enum', label: '--mirostat', tip: 'Mirostat sampling mode (0 = disabled, 1 = v1, 2 = v2).', choices: [['0', '0'], ['1', '1'], ['2', '2']], available: true, supported: true },
    { id: 'mirostat-lr', flag: '--mirostat-lr', tier: 'expert', kind: 'number', label: '--mirostat-lr', tip: 'Mirostat learning rate (eta).', default: '0.1', available: true, supported: true },
    { id: 'mirostat-ent', flag: '--mirostat-ent', tier: 'expert', kind: 'number', label: '--mirostat-ent', tip: 'Mirostat target entropy (tau).', default: '5.0', available: true, supported: true },
    { id: 'prio', flag: '--prio', tier: 'expert', kind: 'enum', label: '--prio', tip: 'Process scheduling priority.', choices: [['-1', 'low'], ['0', 'normal'], ['1', 'medium'], ['2', 'high']], available: true, supported: true },
    { id: 'numa', flag: '--numa', tier: 'expert', kind: 'enum', label: '--numa', tip: 'NUMA optimization strategy on multi-socket hosts.', choices: [['distribute', 'distribute'], ['isolate', 'isolate'], ['numactl', 'numactl']], available: true, supported: true },
    { id: 'cont-batching', flag: '--cont-batching', tier: 'expert', kind: 'bool', label: '--cont-batching', tip: 'Enable continuous batching (dynamic batching).', available: true, supported: true },
    { id: 'metrics', flag: '--metrics', tier: 'expert', kind: 'bool', label: '--metrics', tip: 'Expose Prometheus-compatible /metrics endpoint.', available: true, supported: true },
  ];

  const HUB_CATALOG = [
    { id: 'unsloth/Qwen3-14B-GGUF', author: 'unsloth', pipeline: 'text-generation', params_b: 14, downloads: 418200, likes: 1840, license: 'apache-2.0' },
    { id: 'unsloth/Qwen3-8B-GGUF', author: 'unsloth', pipeline: 'text-generation', params_b: 8, downloads: 512900, likes: 2110, license: 'apache-2.0' },
    { id: 'bartowski/Llama-3.3-8B-Instruct-GGUF', author: 'bartowski', pipeline: 'text-generation', params_b: 8, downloads: 329400, likes: 1290, license: 'llama3.3' },
    { id: 'unsloth/Mistral-Small-24B-Instruct-2501-GGUF', author: 'unsloth', pipeline: 'text-generation', params_b: 24, downloads: 284100, likes: 1520, license: 'apache-2.0' },
    { id: 'unsloth/gemma-3-12b-it-GGUF', author: 'unsloth', pipeline: 'image-text-to-text', params_b: 12, downloads: 391000, likes: 1690, license: 'gemma' },
    { id: 'nomic-ai/nomic-embed-text-v2-moe-GGUF', author: 'nomic-ai', pipeline: 'feature-extraction', params_b: 0.5, downloads: 215000, likes: 840, license: 'apache-2.0' },
  ];

  // ---------- 6. API Router ----------
  async function handleApi(urlObj, method, body) {
    const path = urlObj.pathname;
    const qs = urlObj.searchParams;
    const now = Date.now();
    const nowSec = Math.floor(now / 1000);

    // --- Auth & Health ---
    if (path === '/api/auth/status') {
      return jsonResponse({ ok: true, authenticated: true, password_set: true, local: true });
    }
    if (path.startsWith('/api/auth/')) {
      return jsonResponse({ ok: true, authenticated: true, password_set: true });
    }
    if (path === '/api/ping') {
      return jsonResponse({ ok: true, version: DEMO_VERSION, hostname: 'workstation.local' });
    }
    if (path === '/api/status') {
      return jsonResponse({
        ok: true,
        active: state.engine.active,
        health: state.engine.health,
        model: state.engine.model,
        model_name: state.engine.model_name,
        preset_id: state.engine.preset_id,
        preset_name: state.engine.preset_name,
        hostname: state.engine.hostname,
        engine_kind: state.engine.engine_kind,
        port: state.engine.port,
        ctx_effective: state.engine.ctx_effective,
      });
    }
    if (path === '/api/vram') {
      const used = state.engine.active && state.engine.model ? state.gpu.used : 420;
      return jsonResponse([{ ...state.gpu, used }]);
    }
    if (path === '/api/ram') {
      return jsonResponse({ used: 18432, total: 65536 });
    }
    if (path === '/api/engine/node') {
      return jsonResponse({ ok: true, remote: false, degraded: [], kind: 'llama.cpp', models: [] });
    }

    // --- Preferences ---
    if (path === '/api/prefs') {
      if (method === 'POST' && body && typeof body === 'object') {
        state.prefs = { ...state.prefs, ...body };
        if (body.lang === 'en' || body.lang === 'fr') state.lang = body.lang;
        saveState();
      }
      return jsonResponse({ ok: true, prefs: { ...state.prefs, lang: state.lang } });
    }

    // --- Workspace, Models, Presets ---
    if (path === '/api/workspace') {
      return jsonResponse(buildWorkspacePayload());
    }
    if (path === '/api/workspace/models/visibility') {
      return jsonResponse({ ok: true });
    }
    if (path === '/api/models') {
      return jsonResponse(state.models);
    }
    if (path === '/api/presets') {
      return jsonResponse(state.presets.map(p => ({ ...p, active: state.engine.preset_id === p.id })));
    }
    if (path === '/api/preset') {
      const id = qs.get('id');
      const p = state.presets.find(x => x.id === id) || state.presets[0];
      return jsonResponse({ ok: true, id: p.id, name: p.name, content: p.content });
    }
    if (path === '/api/preset/save' && method === 'POST') {
      const id = body.id || ('preset-' + randomId(6));
      const existing = state.presets.find(p => p.id === id);
      const modelMatch = /MODEL=(.+)/.exec(body.content || '');
      const model = modelMatch ? modelMatch[1].trim() : state.models[0].path;
      if (existing) {
        existing.name = body.name || existing.name;
        existing.content = body.content || existing.content;
        existing.model = model;
      } else {
        state.presets.push({ id, name: body.name || 'Custom Preset', model, ctx: '32768', reasoning: false, vision: false, active: false, content: body.content || '' });
      }
      saveState();
      return jsonResponse({ ok: true, id });
    }
    if (path === '/api/preset/delete' && method === 'POST') {
      state.presets = state.presets.filter(p => p.id !== body.id);
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/presets/order' && method === 'POST') {
      if (Array.isArray(body.ids)) {
        const byId = new Map(state.presets.map(p => [p.id, p]));
        state.presets = body.ids.map(id => byId.get(id)).filter(Boolean);
        saveState();
      }
      return jsonResponse({ ok: true });
    }
    if (path === '/api/models/delete' && method === 'POST') {
      state.models = state.models.filter(m => m.path !== body.name);
      saveState();
      return jsonResponse({ ok: true });
    }

    // --- Engine controls & inspection ---
    if (path === '/api/config') {
      const activePreset = state.presets.find(p => p.id === state.engine.preset_id);
      return jsonResponse({
        MODEL: state.engine.model,
        CTX: String(state.engine.ctx_effective),
        NGL: '999',
        REASONING: activePreset?.reasoning ? 'on' : '',
        EXTRA_ARGS: '--flash-attn on --cache-type-k q8_0 --cache-type-v q8_0 -np ' + state.engine.np,
      });
    }
    if (path === '/api/naked/defaults' || path === '/api/naked/remember') {
      const m = qs.get('model') || body?.model || state.engine.model;
      return jsonResponse({ ok: true, remembered: false, content: `MODEL=${m}\nCTX=32768\nNGL=999\n` });
    }
    if (path === '/api/engine/params') {
      return jsonResponse({ ok: true, params: ENGINE_PARAMS });
    }
    if (path === '/api/model-caps') {
      const mPath = qs.get('model') || state.engine.model;
      const m = state.models.find(x => x.path === mPath) || state.models[0];
      const hasVision = /gemma|vl|vision|llava/i.test(m.name || mPath);
      return jsonResponse({
        ok: true,
        native_ctx: m.native_ctx || 32768,
        n_layers: m.n_layers || 40,
        thinks: !!m.thinks,
        mmproj: hasVision ? ['mmproj-' + (m.name || 'model').replace(/\.gguf$/i, '') + '-f16.gguf'] : [],
        effort: m.thinks ? ['low', 'medium', 'high'] : [],
        effort_default: 'medium',
      });
    }
    if (path === '/api/estimate') {
      const mPath = body?.model || state.engine.model;
      const m = state.models.find(x => x.path === mPath) || state.models[0];
      const ctxMatch = /CTX=(\d+)/.exec(body?.content || '');
      const ctx = ctxMatch ? Number(ctxMatch[1]) : 32768;
      const kvMb = Math.round((ctx / 32768) * 1480);
      const weightsMb = Math.round((m.size / 1048576) * 1.05);
      const totalNeed = weightsMb + kvMb + 640;
      const vramTotal = state.gpu.total;
      const gpuMb = Math.min(totalNeed, vramTotal);
      const ramOffload = Math.max(0, totalNeed - vramTotal);
      return jsonResponse({
        ok: true,
        gpu_mb: gpuMb,
        kv_mb: kvMb,
        vram_total_mb: vramTotal,
        ram_offload_mb: ramOffload,
        ctx,
        native_ctx: m.native_ctx || 32768,
        max_ctx_fit: 65536,
      });
    }
    if (path === '/api/start') {
      state.engine.active = true;
      state.engine.health = true;
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/stop') {
      state.engine.active = false;
      state.engine.health = false;
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/restart') {
      state.engine.active = true;
      state.engine.health = true;
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/unload') {
      state.engine.model = '';
      state.engine.model_name = '';
      state.engine.preset_id = '';
      state.engine.preset_name = '';
      state.engine.health = false;
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/load-model' && method === 'POST') {
      const m = state.models.find(x => x.path === body.model || x.value === body.model || x.name === body.model) || state.models[0];
      state.engine.active = true;
      state.engine.health = true;
      state.engine.model = m.path;
      state.engine.model_name = m.name;
      state.engine.preset_id = '';
      state.engine.preset_name = '';
      state.gpu.used = m.vram_mb || 9600;
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/switch' && method === 'POST') {
      const idx = Math.max(0, (Number(body.n) || 1) - 1);
      const p = state.presets[idx] || state.presets[0];
      const m = state.models.find(x => x.path === p.model) || state.models[0];
      state.engine.active = true;
      state.engine.health = true;
      state.engine.model = p.model;
      state.engine.model_name = m.name;
      state.engine.preset_id = p.id;
      state.engine.preset_name = p.name;
      state.gpu.used = m.vram_mb || 11264;
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/apply' && method === 'POST') {
      const ctxMatch = /CTX=(\d+)/.exec(body?.content || '');
      if (ctxMatch) state.engine.ctx_effective = Number(ctxMatch[1]);
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/server') {
      if (method === 'POST' && body?.np) {
        state.engine.np = Number(body.np);
        saveState();
      }
      return jsonResponse({
        ok: true,
        engine_kind: 'llama.cpp',
        port: state.engine.port,
        np: state.engine.np,
        url: 'http://workstation.local:8081/v1',
        model_name: state.engine.model_name,
        slots: {
          ok: true,
          busy: 0,
          items: Array.from({ length: state.engine.np }, (_, i) => ({ id: i, busy: false, ctx: Math.round(state.engine.ctx_effective / state.engine.np), prompt: 0, tokens: 0, toks: 74.2 })),
        },
        stats: { live_toks: 0, avg_toks: 74.2 },
        recent: [
          { slot: 0, task: 41, prompt: 684, tokens: 96, ended: now - 4 * 60 * 1000 },
          { slot: 0, task: 40, prompt: 412, tokens: 158, ended: now - 6 * 60 * 1000 },
          { slot: 1, task: 39, prompt: 1240, tokens: 210, ended: now - 19 * 60 * 1000 },
        ],
      });
    }
    if (path === '/api/engine/service') {
      if (method === 'POST' && body) {
        if (body.idle_unload_minutes !== undefined) state.engine.idle_unload_minutes = Number(body.idle_unload_minutes);
        if (body.models_max !== undefined) state.engine.models_max = Number(body.models_max);
        if (body.interactive_grace_minutes !== undefined) state.engine.interactive_grace_minutes = Number(body.interactive_grace_minutes);
        saveState();
      }
      return jsonResponse({
        ok: true,
        idle_unload_minutes: state.engine.idle_unload_minutes,
        idle_unload_at: now + state.engine.idle_unload_minutes * 60 * 1000,
        models_max: state.engine.models_max,
        interactive_grace_minutes: state.engine.interactive_grace_minutes,
        resident: state.engine.model_name ? [{ model: state.engine.model_name.replace(/\.gguf$/i, ''), in_flight: 0 }] : [],
        queue: [],
      });
    }
    if (path === '/api/engine/openai') {
      return jsonResponse({
        ok: true,
        url_local: 'http://127.0.0.1:8081/v1',
        url_lan: 'http://workstation.local:8081/v1',
        exposed: state.engine.exposed,
        key_required: state.engine.key_required,
        port: 8081,
        models: [
          ...state.presets.map(p => ({ id: p.name, kind: 'preset' })),
          ...state.models.map(m => ({ id: m.name.replace(/\.gguf$/i, ''), kind: 'file' })),
        ],
      });
    }
    if (path === '/api/engine/keys') {
      if (method === 'POST' && body?.name) {
        const key = {
          id: 'key-' + randomId(5),
          name: body.name,
          priority: body.priority || 'interactive',
          max_concurrency: body.max_concurrency || 0,
          requests_per_minute: body.requests_per_minute || 0,
          allowed_models: body.allowed_models || [],
          last_used_at: 0,
          usage: { requests: 0, prompt_tokens: 0, completion_tokens: 0 },
        };
        state.engineKeys.push(key);
        saveState();
        return jsonResponse({ ok: true, key, secret: 'loom_demo_key_simulated_only_not_real' });
      }
      return jsonResponse({ ok: true, endpoint: 'http://workstation.local:8081/v1', keys: state.engineKeys });
    }
    if (path === '/api/engine/keys/delete' && method === 'POST') {
      state.engineKeys = state.engineKeys.filter(k => k.id !== body.id);
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/engine/keys/rotate' && method === 'POST') {
      return jsonResponse({ ok: true, secret: 'loom_demo_rotated_key_simulated_only' });
    }
    if (path === '/api/engine/keys/update' && method === 'POST') {
      state.engineKeys = state.engineKeys.map(k => (k.id === body.id ? { ...k, ...body } : k));
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/service/log') {
      return jsonResponse({
        ok: true,
        log:
          '[demo] llama-server router listening on 127.0.0.1:8081\n' +
          '[demo] backend: CUDA (NVIDIA RTX 4090 Simulated, 24576 MiB)\n' +
          '[demo] loaded /srv/models/gguf/Qwen3-14B-Q4_K_M.gguf (40/40 layers on GPU)\n' +
          '[demo] kv cache: q8_0 · flash-attn: enabled · slots: 2 x 16384 tokens\n' +
          '[demo] health: ready (avg 74.2 tok/s)\n',
      });
    }
    if (path === '/api/llamacpp') {
      return jsonResponse({
        ok: true,
        installed: true,
        os: 'linux',
        arch: 'amd64',
        bin: '/usr/local/bin/llama-server',
        config_bin: '/usr/local/bin/llama-server',
        commit: 'b5420',
        behind: 0,
        can_update: false,
        update_kind: 'prebuilt',
        plan: { backend: 'cuda' },
        health: { observed: true, healthy: true, devices: [{ id: 'CUDA0', name: 'NVIDIA RTX 4090 (Simulated)' }] },
      });
    }
    if (path === '/api/llamacpp/job') {
      return jsonResponse({ ok: true, exists: false, running: false, lines: [] });
    }
    if (path === '/api/engine/auto-update') {
      return jsonResponse({ ok: true, state: { auto: true, checked_at: now - 2 * HOUR } });
    }
    if (path === '/api/models/dirs') {
      return jsonResponse({ ok: true, dirs: [{ path: '/srv/models/gguf', count: state.models.length, download: true, home: true }] });
    }
    if (path === '/api/backends/devices') {
      return jsonResponse({ ok: true, devices: [{ id: 'CUDA0', name: 'NVIDIA RTX 4090 (Simulated)', total_mib: 24576 }] });
    }
    if (path === '/api/engines/vllm') {
      return jsonResponse({ ok: true, installed: false, running: false, job: '', download: null, dir: '/var/lib/loom/vllm' });
    }
    if (path === '/api/engines/vllm/download') {
      return jsonResponse({ ok: true, download: null });
    }
    if (path === '/api/engines/vllm/models') {
      return jsonResponse({ ok: true, models: [], cache: '/var/lib/loom/hf-cache' });
    }

    // --- Hugging Face Hub (Simulated) ---
    if (path === '/api/hub/search') {
      const q = (qs.get('q') || '').toLowerCase();
      const pipe = qs.get('pipeline') || '';
      const models = HUB_CATALOG.filter(m => (!pipe || m.pipeline === pipe) && (!q || m.id.toLowerCase().includes(q)));
      return jsonResponse({ ok: true, models });
    }
    if (path === '/api/hub/model') {
      const id = qs.get('id') || HUB_CATALOG[0].id;
      const item = HUB_CATALOG.find(m => m.id === id) || HUB_CATALOG[0];
      const base = id.split('/').pop().replace(/-GGUF$/i, '');
      return jsonResponse({
        ok: true,
        id,
        author: item.author,
        downloads: item.downloads,
        likes: item.likes,
        license: item.license,
        vram_total_mb: state.gpu.total,
        files: [
          { name: `${base}-Q4_K_M.gguf`, quant: 'Q4_K_M', size: Math.round(item.params_b * 620e6), url: `https://huggingface.co/${id}/resolve/main/${base}-Q4_K_M.gguf` },
          { name: `${base}-Q6_K.gguf`, quant: 'Q6_K', size: Math.round(item.params_b * 820e6), url: `https://huggingface.co/${id}/resolve/main/${base}-Q6_K.gguf` },
          { name: `${base}-Q8_0.gguf`, quant: 'Q8_0', size: Math.round(item.params_b * 1050e6), url: `https://huggingface.co/${id}/resolve/main/${base}-Q8_0.gguf` },
        ],
        readme: `# ${base}\n\nSimulated model card in the Loom public demo. Clicking **Download** simulates progress without transferring any real weights.\n`,
      });
    }
    if (path === '/api/models/download/probe') {
      return jsonResponse({ ok: true, enough: true, free_bytes: 420 * 1024 ** 3 });
    }
    if (path === '/api/models/download' && method === 'POST') {
      const filename = String(body?.url || 'demo-model.gguf').split('/').pop();
      state.hubDownloads[filename] = { filename, done: 0, total: 1000, finished: false, canceled: false, startedAt: Date.now() };
      return jsonResponse({ ok: true, filename });
    }
    if (path === '/api/models/download/status') {
      const list = [];
      for (const dl of Object.values(state.hubDownloads)) {
        if (!dl.finished && !dl.canceled) {
          dl.done = Math.min(dl.total, dl.done + 350);
          if (dl.done >= dl.total) {
            dl.finished = true;
            if (!state.models.some(m => m.name === dl.filename)) {
              state.models.push({ name: dl.filename, path: '/srv/models/gguf/' + dl.filename, value: '/srv/models/gguf/' + dl.filename, dir: '/srv/models/gguf', size: 4800000000, shards: 1, vram_mb: 5800, native_ctx: 32768, n_layers: 32, thinks: false });
              saveState();
            }
          }
        }
        list.push(dl);
      }
      return jsonResponse(list);
    }
    if (path === '/api/models/download/cancel' && method === 'POST') {
      if (state.hubDownloads[body?.filename]) state.hubDownloads[body.filename].canceled = true;
      return jsonResponse({ ok: true });
    }

    // --- Discussions, History, Folders, Streaming ---
    if (path === '/api/chat/history') {
      const conversations = Object.values(state.discussions).map(d => ({
        id: d.id,
        title: d.title,
        project_id: d.project_id || '',
        turns: d.turns.length,
        updated_at: d.updated_at,
        model: d.model,
        runtime_id: d.runtime_id,
        fav: !!d.fav,
      }));
      return jsonResponse({
        ok: true,
        active: state.activeChatId,
        project_id: (state.discussions[state.activeChatId] || {}).project_id || '',
        conversations,
        projects: state.projects,
      });
    }
    if (path === '/api/chat/settings') {
      return jsonResponse({ ok: true, default_choice: 'local:' + state.models[0].path });
    }
    if (path === '/api/chat/reset' && method === 'POST') {
      const id = 'chat-' + randomId(6);
      state.discussions[id] = {
        id,
        title: state.lang === 'fr' ? 'Nouvelle discussion' : 'New discussion',
        project_id: body?.project_id || '',
        runtime_id: 'llama.cpp',
        model: state.engine.model || state.models[0].path,
        provider_id: '',
        provider_name: '',
        native_archive: id,
        updated_at: Date.now(),
        status: 'idle',
        instructions: '',
        turns: [],
      };
      state.activeChatId = id;
      saveState();
      return jsonResponse({
        ok: true,
        initial_choice: { kind: 'local', model: state.models[0].path, engine_value: state.models[0].path, name: state.models[0].name },
      });
    }
    if (path === '/api/discussion/events') {
      return createDiscussionStreamResponse(body?.id || '');
    }
    if (path === '/api/chat/send' && method === 'POST') {
      const d = state.discussions[state.activeChatId];
      if (!d) return jsonResponse({ ok: false, error: 'No active discussion' }, 404);
      simulateStreamingTurn(d, body?.message || '');
      return jsonResponse({ ok: true });
    }
    if (path === '/api/chat/stop' || path === '/api/runtime/sessions/stop') {
      streamingAbort = true;
      return jsonResponse({ ok: true });
    }
    if (path === '/api/chat/compact' || path === '/api/runtime/sessions/compact') {
      return jsonResponse({ ok: true });
    }
    if (path === '/api/runtime/sessions') {
      const id = qs.get('id');
      if (id) {
        const d = state.discussions[id];
        if (!d) return jsonResponse({ ok: false, error: 'Not found' }, 404);
        const session = buildSessionObject(d);
        return jsonResponse({ ok: true, session, context: session.context });
      }
      const sessions = Object.values(state.discussions).map(buildSessionObject);
      return jsonResponse({ ok: true, sessions });
    }
    if (path === '/api/runtime/sessions/local' && method === 'POST') {
      const d = state.discussions[body?.id];
      if (!d) return jsonResponse({ ok: false, error: 'Not found' }, 404);
      state.activeChatId = d.id;
      saveState();
      const session = buildSessionObject(d);
      return jsonResponse({ ok: true, session, context: session.context });
    }
    if (path === '/api/runtime/sessions/create' && method === 'POST') {
      const id = 'chat-' + randomId(6);
      const d = {
        id,
        title: state.lang === 'fr' ? 'Nouvelle discussion' : 'New discussion',
        project_id: body?.project_id || '',
        runtime_id: 'llama.cpp',
        model: state.models[0].path,
        provider_id: '',
        provider_name: '',
        native_archive: id,
        updated_at: Date.now(),
        status: 'idle',
        instructions: '',
        turns: [],
      };
      state.discussions[id] = d;
      state.activeChatId = id;
      saveState();
      const session = buildSessionObject(d);
      return jsonResponse({ ok: true, session, context: session.context });
    }
    if (path === '/api/runtime/sessions/select' && method === 'POST') {
      const d = state.discussions[body?.id];
      if (!d) return jsonResponse({ ok: false, error: 'Not found' }, 404);
      const choice = buildWorkspacePayload().models.find(m => m.id === body.choice_id);
      if (choice) {
        d.runtime_id = choice.runtime_id;
        d.model = choice.model;
        d.provider_id = choice.provider_id || '';
        d.provider_name = choice.provider_name || '';
        d.updated_at = Date.now();
        saveState();
      }
      const session = buildSessionObject(d);
      return jsonResponse({ ok: true, session, context: session.context });
    }
    if (path === '/api/runtime/sessions/preview' && method === 'POST') {
      const d = state.discussions[body?.id];
      if (!d) return jsonResponse({ ok: false, error: 'Not found' }, 404);
      const session = buildSessionObject(d);
      return jsonResponse({
        ok: true,
        runtime_id: d.runtime_id,
        model: d.model,
        endpoint: session.endpoint,
        provider_id: d.provider_id || '',
        reasoning_effort: '',
        preview: { omitted: 0, messages: [], context: session.context },
      });
    }
    if (path === '/api/runtime/sessions/send' && method === 'POST') {
      const d = state.discussions[body?.id];
      if (!d) return jsonResponse({ ok: false, error: 'Not found' }, 404);
      simulateStreamingTurn(d, body?.text || '');
      return jsonResponse({ ok: true });
    }
    if (path === '/api/runtime/sessions/configure' && method === 'POST') {
      const d = state.discussions[body?.id];
      if (d) {
        if (body.title !== undefined) d.title = body.title;
        if (body.project_id !== undefined) d.project_id = body.project_id;
        if (body.instructions !== undefined) d.instructions = body.instructions;
        d.updated_at = Date.now();
        saveState();
        const session = buildSessionObject(d);
        return jsonResponse({ ok: true, session, context: session.context });
      }
      return jsonResponse({ ok: false, error: 'Not found' }, 404);
    }
    if (path === '/api/runtime/sessions/delete' || path === '/api/chat/history/delete') {
      delete state.discussions[body?.id];
      if (state.activeChatId === body?.id) state.activeChatId = Object.keys(state.discussions)[0] || '';
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/chat/history/rename' && method === 'POST') {
      if (state.discussions[body?.id]) {
        state.discussions[body.id].title = body.title;
        saveState();
      }
      return jsonResponse({ ok: true });
    }
    if (path === '/api/chat/history/move' && method === 'POST') {
      if (state.discussions[body?.id]) {
        state.discussions[body.id].project_id = body.project_id || '';
        saveState();
      }
      return jsonResponse({ ok: true });
    }
    if (path === '/api/chat/history/fav' && method === 'POST') {
      if (state.discussions[body?.id]) {
        state.discussions[body.id].fav = !!body.fav;
        saveState();
      }
      return jsonResponse({ ok: true });
    }
    if (path === '/api/chat/history/restore' && method === 'POST') {
      if (state.discussions[body?.id]) {
        state.activeChatId = body.id;
        saveState();
      }
      return jsonResponse({ ok: true });
    }

    // --- Folders / Projects ---
    if (path === '/api/projects' && method === 'POST') {
      const id = 'proj-' + randomId(5);
      state.projects.push({ id, name: body?.name || 'Folder', directory: '', capability_ids: [], mcp_servers: [], brain_sources: [] });
      saveState();
      return jsonResponse({ ok: true, id });
    }
    if (path === '/api/projects/rename' && method === 'POST') {
      const p = state.projects.find(x => x.id === body?.id);
      if (p && body?.name) { p.name = body.name; saveState(); }
      return jsonResponse({ ok: true });
    }
    if (path === '/api/projects/delete' && method === 'POST') {
      state.projects = state.projects.filter(x => x.id !== body?.id);
      for (const d of Object.values(state.discussions)) if (d.project_id === body?.id) d.project_id = '';
      for (const [sid, fid] of Object.entries(state.agentFolders)) if (fid === body?.id) delete state.agentFolders[sid];
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/agent-folders') {
      if (method === 'POST' && body?.id) {
        if (body.folder) state.agentFolders[body.id] = body.folder;
        else delete state.agentFolders[body.id];
        saveState();
      }
      return jsonResponse({ ok: true, folders: state.agentFolders });
    }

    // --- Cloud Providers ---
    if (path === '/api/providers') {
      return jsonResponse({ ok: true, providers: state.providers });
    }
    if (path === '/api/providers/models' && method === 'POST') {
      return jsonResponse({ ok: true, models: ['claude-sonnet-4-5', 'claude-haiku-4-5', 'gpt-4.1', 'o4-mini', 'mistral-large-latest'] });
    }
    if (path === '/api/providers/save' && method === 'POST') {
      const id = body?.id || ('prov-' + randomId(5));
      const existing = state.providers.find(p => p.id === id);
      const rec = { id, name: body.name || 'Custom Cloud', endpoint: body.endpoint, ready: true, remember: !!body.remember, models: body.models || [body.model], usage_mode: body.usage_mode || 'stream' };
      if (existing) Object.assign(existing, rec);
      else state.providers.push(rec);
      saveState();
      return jsonResponse({ ok: true, provider: rec });
    }
    if (path === '/api/providers/disconnect' && method === 'POST') {
      const p = state.providers.find(x => x.id === body?.id);
      if (p) { p.ready = false; saveState(); }
      return jsonResponse({ ok: true });
    }

    // --- Codeman Gateway & API v1 ---
    if (path === '/api/codeman/config') {
      return jsonResponse({ ok: true, ...state.codeman.config });
    }
    if (path === '/api/codeman/status') {
      return jsonResponse({ ok: true, reachable: true, auth: 'ok', role: 'admin', version: '1.52.0', ui_url: state.codeman.config.url });
    }
    if (path === '/api/codeman/v1/events') {
      return createCodemanEventsResponse();
    }
    if (path === '/api/codeman/v1/status') {
      return jsonResponse({
        success: true,
        data: {
          version: '1.52.0',
          planUsage: {
            modelDisplayName: 'Claude Sonnet 4.5',
            fiveHour: { usedPercentage: 42, resetAt: state.quotas.reset5h },
            sevenDay: { usedPercentage: 58, resetAt: state.quotas.reset7d },
            codex: {
              fiveHour: { usedPercentage: 28, resetAt: state.quotas.codex5h },
              sevenDay: { usedPercentage: 39, resetAt: state.quotas.codex7d },
            },
          },
        },
      });
    }
    if (path === '/api/codeman/v1/clis') {
      return jsonResponse({ success: true, data: state.codeman.clis });
    }
    if (path === '/api/codeman/v1/cases') {
      return jsonResponse({
        success: true,
        data: [
          { name: 'homelab-ops', path: '/workspace/homelab-ops' },
          { name: 'research-notes', path: '/workspace/research-notes' },
        ],
      });
    }
    if (path === '/api/codeman/v1/settings') {
      if (method === 'PUT' && body) return jsonResponse({ success: true, data: body });
      return jsonResponse({
        success: true,
        data: { runMode: 'claude', defaultWorkingDir: '/workspace/homelab-ops', cliManagementEnabled: true, terminalScrollbackLines: 20000 },
      });
    }
    if (path === '/api/codeman/v1/reboot-restore') {
      return jsonResponse({ success: true, data: { sessions: [] } });
    }
    if (path === '/api/codeman/v1/system/update/check') {
      return jsonResponse({
        success: true,
        data: { currentVersion: '1.52.0', updateAvailable: false, latestVersion: '1.52.0', selfUpdateEnabled: false, installKind: 'git' },
      });
    }
    if (path === '/api/codeman/v1/token-stats') {
      return jsonResponse({ success: true, data: { daily: state.quotas.tokenDaily } });
    }
    if (path === '/api/codeman/v1/approvals') {
      return jsonResponse({ success: true, data: { approvals: state.codeman.approvals } });
    }
    const apprMatch = /^\/api\/codeman\/v1\/approvals\/([^/]+)\/(answer|dismiss)$/.exec(path);
    if (apprMatch && method === 'POST') {
      const id = decodeURIComponent(apprMatch[1]);
      state.codeman.approvals = state.codeman.approvals.filter(a => a.id !== id);
      saveState();
      emitCodemanEvent('approval:resolved', { id });
      return jsonResponse({ success: true });
    }
    if (/^\/api\/codeman\/v1\/approvals\/session\/[^/]+\/viewed$/.test(path)) {
      return jsonResponse({ success: true });
    }
    if (path === '/api/codeman/v1/cron/jobs') {
      if (method === 'POST' && body) {
        const job = { id: 'cron-' + randomId(5), enabled: true, nextRunAt: now + 6 * HOUR, lastStatus: 'pending', ...body };
        state.codeman.cronJobs.push(job);
        saveState();
        return jsonResponse({ success: true, data: job });
      }
      return jsonResponse({ success: true, data: state.codeman.cronJobs });
    }
    if (path === '/api/codeman/v1/cron/runs' || /^\/api\/codeman\/v1\/cron\/jobs\/[^/]+\/runs$/.test(path)) {
      return jsonResponse({ success: true, data: state.codeman.cronRuns });
    }
    const cronEnMatch = /^\/api\/codeman\/v1\/cron\/jobs\/([^/]+)\/enabled$/.exec(path);
    if (cronEnMatch && method === 'PUT') {
      const job = state.codeman.cronJobs.find(j => j.id === decodeURIComponent(cronEnMatch[1]));
      if (job) { job.enabled = !!body?.enabled; saveState(); }
      return jsonResponse({ success: true, data: job });
    }
    if (path === '/api/codeman/v1/sessions/unified') {
      return jsonResponse({ success: true, data: { sessions: state.codeman.history, total: state.codeman.history.length } });
    }
    if (path === '/api/codeman/v1/sessions') {
      if (method === 'POST' && body) {
        const s = {
          id: 'cm-' + randomId(6),
          mode: body.mode || 'claude',
          name: body.name || (body.mode === 'shell' ? 'Shell' : 'Demo agent session'),
          workingDir: body.workingDir || '/workspace/homelab-ops',
          pid: 4500 + state.codeman.sessions.length,
          status: 'idle',
          isWorking: false,
          cliVersion: '2.1.4',
          displayModel: { model: body.mode === 'codex' ? 'gpt-5.3-codex' : 'claude-sonnet-4-5' },
          createdAt: now,
          lastActivityAt: now,
          tokens: { input: 0, output: 0, total: 0 },
          totalCost: 0,
          messages: [],
          terminalBuffer: '\x1b[1;36m' + (body.mode || 'claude') + '\x1b[0m session started (simulated demo)\r\n$ ',
        };
        state.codeman.sessions.unshift(s);
        saveState();
        emitCodemanEvent('session:created', { session: s });
        return jsonResponse({ success: true, data: s });
      }
      return jsonResponse({ success: true, data: state.codeman.sessions });
    }
    const cmSessMatch = /^\/api\/codeman\/v1\/sessions\/([^/]+)(?:\/(.+))?$/.exec(path);
    if (cmSessMatch) {
      const sid = decodeURIComponent(cmSessMatch[1]);
      const sub = cmSessMatch[2] || '';
      const sess = state.codeman.sessions.find(s => s.id === sid);
      if (!sess) return jsonResponse({ success: false, errorCode: 'NOT_FOUND' }, 404);
      if (!sub) {
        if (method === 'DELETE') {
          state.codeman.sessions = state.codeman.sessions.filter(s => s.id !== sid);
          saveState();
          emitCodemanEvent('session:deleted', { id: sid });
          return jsonResponse({ success: true });
        }
        return jsonResponse({ success: true, data: sess });
      }
      if (sub === 'name' && method === 'PUT') {
        sess.name = body?.name || sess.name;
        saveState();
        emitCodemanEvent('session:updated', { session: sess });
        return jsonResponse({ success: true, data: sess });
      }
      if (sub === 'last-response') {
        return jsonResponse({ success: true, data: { messages: sess.messages || [] } });
      }
      if (sub === 'terminal') {
        return jsonResponse({ success: true, data: { terminalBuffer: sess.terminalBuffer || '', captureCols: 100, paneHistoryLines: 32 } });
      }
      if (sub === 'input' && method === 'POST') {
        const line = String(body?.input || '').replace(/\r/g, '').trim();
        if (line && !line.startsWith('\x1b')) {
          sess.messages = sess.messages || [];
          sess.messages.push({ kind: 'prompt', role: 'user', text: line });
          sess.messages.push({
            kind: 'response',
            role: 'assistant',
            turn: sess.messages.length,
            text: `Simulated ${sess.mode} response to \`${line}\` inside \`${sess.workingDir}\`. No real shell commands or API requests were executed.`,
          });
          sess.terminalBuffer = (sess.terminalBuffer || '') + `\r\n> ${line}\r\n\x1b[32m✓\x1b[0m Simulated response recorded.\r\n`;
          sess.lastActivityAt = Date.now();
          saveState();
          emitCodemanEvent('session:idle', { id: sess.id });
        }
        return jsonResponse({ success: true, data: { delivered: true } });
      }
      if (sub === 'git-status') {
        if (sid === 'cm-docs-sync') return jsonResponse({ success: true, data: {
          state: 'ok', checkedAt: now, repos: [{ name: 'research-notes', path: sess.workingDir, status: {
            repoRoot: sess.workingDir, branch: 'refactor/bench-parser', counts: { uncommitted: 2 },
            files: [{ path: 'scripts/bench-parser.mjs', kind: 'modified', worktree: 'M' }, { path: 'tests/bench-parser.test.mjs', kind: 'untracked', worktree: '?' }],
          } }],
        } });

        return jsonResponse({
          success: true,
          data: {
            state: 'ok',
            checkedAt: now - 30000,
            repos: [
              {
                name: 'homelab-ops',
                path: '/workspace/homelab-ops',
                status: {
                  repoRoot: '/workspace/homelab-ops',
                  branch: 'main',
                  ahead: 1,
                  behind: 0,
                  counts: { uncommitted: 2 },
                  unpushedCount: 1,
                  files: [
                    { path: 'compose/monitoring.yml', kind: 'modified', worktree: 'M' },
                    { path: 'scripts/gpu-telemetry-sweep.sh', kind: 'untracked', worktree: '?' },
                  ],
                  unpushed: [{ hash: 'a94c2e1f', subject: 'chore: add healthchecks to monitoring compose' }],
                },
              },
            ],
          },
        });
      }
      if (sub === 'git-diff') {
        if (sid === 'cm-docs-sync') {
          const diff = qs.get('path') === 'tests/bench-parser.test.mjs'
            ? "@@ -0,0 +1,2 @@\n+const row = parseBenchmark('pp512,42');\n+assert.equal(row.tokensPerSecond, 42);"
            : "@@ -12,3 +12,6 @@\n export function parseBenchmark(line) {\n-  return line.split(',');\n+  const [test, rate] = line.trim().split(',');\n+  const tokensPerSecond = Number(rate);\n+  if (!Number.isFinite(tokensPerSecond)) return null;\n+  return { test, tokensPerSecond };\n }";
          return jsonResponse({ success: true, data: { diff } });
        }

        return jsonResponse({
          success: true,
          data: {
            diff: '@@ -18,6 +18,11 @@\n   loki:\n     image: grafana/loki:3.4\n     restart: unless-stopped\n+    healthcheck:\n+      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:3100/ready"]\n+      interval: 30s\n+      timeout: 5s\n',
          },
        });
      }
      if (sub === 'files') {
        return jsonResponse({
          success: true,
          data: {
            root: sess.workingDir,
            tree: [
              { name: 'compose', path: 'compose', type: 'directory', children: [{ name: 'monitoring.yml', path: 'compose/monitoring.yml', type: 'file' }] },
              { name: 'scripts', path: 'scripts', type: 'directory', children: [{ name: 'gpu-telemetry-sweep.sh', path: 'scripts/gpu-telemetry-sweep.sh', type: 'file' }] },
              { name: 'README.md', path: 'README.md', type: 'file' },
            ],
          },
        });
      }
      if (sub === 'file-content') {
        return jsonResponse({ success: true, data: { content: '# Simulated repository file\n# Path: ' + (qs.get('path') || 'README.md') + '\n' } });
      }
      if (sub === 'run-summary') {
        return jsonResponse({
          success: true,
          data: {
            summary: {
              events: [
                { id: 'ev-1', timestamp: now - 6 * 60 * 1000, title: 'Validated compose/monitoring.yml', details: '0 schema warnings', severity: 'info' },
                { id: 'ev-2', timestamp: now - 12 * 60 * 1000, title: 'Session started', details: sess.workingDir, severity: 'info' },
              ],
            },
          },
        });
      }
      if (sub === 'subagents') return jsonResponse({ success: true, data: [] });
      if (sub === 'active-tools') return jsonResponse({ success: true, data: { tools: [] } });
      return jsonResponse({ success: true, data: {} });
    }

    // --- Machines & Terminals ---
    if (path === '/api/machines') {
      if (method === 'POST' && body?.check_only) {
        return jsonResponse({ ok: true, machine: body.machine });
      }
      return jsonResponse({ ok: true, machines: state.machines.remote });
    }
    if (path === '/api/machines/local') {
      return jsonResponse({
        ok: true,
        hostname: 'workstation.local',
        os: 'Linux',
        arch: 'x86_64',
        version: DEMO_VERSION,
        gpus: [{ name: 'NVIDIA RTX 4090 (Simulated)', total: 24576 }],
        harnesses: ['claude', 'codex'],
        tools: [
          { id: 'claude', version: '2.1.4' },
          { id: 'codex', version: '0.42.0' },
        ],
      });
    }
    if (path === '/api/machines/roles') {
      if (method === 'POST' && body?.role && body?.machine) {
        state.machines.roles[body.role] = body.machine;
        saveState();
      }
      return jsonResponse({ ok: true, roles: state.machines.roles });
    }
    if (path === '/api/machines/metrics') {
      const GiB = 1024 ** 3;
      return jsonResponse({
        ok: true,
        metrics: {
          local: {
            os: 'Linux',
            cpu: 24,
            cores: 16,
            ram_used: 18.4 * GiB,
            ram_total: 64 * GiB,
            gpus: [{ name: 'NVIDIA RTX 4090 (Simulated)', vram_used: (state.gpu.used / 1024) * GiB, vram_total: 24 * GiB, util: 26 }],
            disk: { path: '/', used: 412 * GiB, total: 1800 * GiB },
          },
          'node-gpu': {
            os: 'Linux',
            cpu: 38,
            cores: 12,
            ram_used: 26.2 * GiB,
            ram_total: 64 * GiB,
            gpus: [{ name: 'NVIDIA RTX 3090 (Simulated)', vram_used: 14.6 * GiB, vram_total: 24 * GiB, util: 42 }],
            disk: { path: '/', used: 620 * GiB, total: 2000 * GiB },
          },
          'node-nas': {
            os: 'Linux',
            cpu: 11,
            cores: 4,
            ram_used: 5.8 * GiB,
            ram_total: 16 * GiB,
            gpus: [],
            disk: { path: '/mnt/tank', used: 11200 * GiB, total: 32000 * GiB },
          },
        },
      });
    }
    if (path === '/api/machines/discover') {
      return jsonResponse({ ok: true, nodes: [] });
    }
    if (path === '/api/machines/folders') {
      return jsonResponse({ ok: true, folders: ['/workspace/homelab-ops', '/workspace/research-notes', '/home/demo/second-brain'] });
    }
    if (/^\/api\/machines\/[^/]+\/node\/update$/.test(path) || path === '/api/update') {
      return jsonResponse({ ok: true, current: DEMO_VERSION, latest: DEMO_VERSION, available: false, can_apply: false, channel: 'stable' });
    }
    if (/^\/api\/machines\/[^/]+\/node$/.test(path)) {
      return jsonResponse({ ok: true, linked: true, version: DEMO_VERSION });
    }
    if (path === '/api/terminals') {
      if (method === 'POST') {
        const term = {
          id: 'term-' + randomId(5),
          title: body?.title || (body?.target === 'node-gpu' ? 'gpu-node.local' : 'shell'),
          target: body?.target || 'local',
          dir: body?.dir || '/workspace/homelab-ops',
          command: body?.command || '',
          running: true,
        };
        state.machines.terminals.push(term);
        saveState();
        return jsonResponse({ ok: true, terminal: term });
      }
      return jsonResponse({ ok: true, supported: true, terminals: state.machines.terminals });
    }
    if (path === '/api/terminals/ticket' && method === 'POST') {
      return jsonResponse({ ok: true, ticket: 'demo-ticket-' + (body?.id || 'local') });
    }
    if (path === '/api/terminals/close' && method === 'POST') {
      state.machines.terminals = state.machines.terminals.filter(t => t.id !== body?.id);
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/previews') {
      return jsonResponse({ ok: true, previews: [] });
    }

    // --- Files / Second Brain ---
    if (path === '/api/files/root') {
      if (method === 'POST' && body?.path) { state.files.root = body.path; saveState(); }
      return jsonResponse({ ok: true, root: state.files.root, exists: true });
    }
    if (path === '/api/files/tree') {
      return jsonResponse({ ok: true, root: state.files.root, exists: true, tree: buildFileTree() });
    }
    if (path === '/api/files/list') {
      const reqPath = qs.get('path') || '.';
      const entries = reqPath === '.'
        ? [
            { name: 'workspace', path: 'workspace', dir: true },
            { name: 'home', path: 'home', dir: true },
            { name: 'srv', path: 'srv', dir: true },
          ]
        : [{ name: 'README.md', path: reqPath + '/README.md', dir: false }];
      return jsonResponse({ ok: true, root: '/', entries });
    }
    if (path === '/api/files/read') {
      const p = qs.get('path') || '';
      const note = state.files.notes[p];
      if (!note) return jsonResponse({ ok: false, error: 'File not found' }, 404);
      return jsonResponse({ ok: true, path: p, content: note.content, modified: note.modified });
    }
    if (path === '/api/files/write' && method === 'POST') {
      const p = body?.path || '';
      if (!p) return jsonResponse({ ok: false, error: 'Missing path' }, 400);
      const modified = Date.now();
      state.files.notes[p] = { content: body.content ?? '', modified };
      saveState();
      return jsonResponse({ ok: true, path: p, modified });
    }
    if (path === '/api/files/op' && method === 'POST') {
      if (body?.op === 'delete' && body.path) {
        for (const key of Object.keys(state.files.notes)) {
          if (key === body.path || key.startsWith(body.path + '/')) delete state.files.notes[key];
        }
        saveState();
      } else if (body?.op === 'rename' && body.path && body.to) {
        const next = {};
        for (const [k, v] of Object.entries(state.files.notes)) {
          if (k === body.path) next[body.to] = v;
          else if (k.startsWith(body.path + '/')) next[body.to + k.slice(body.path.length)] = v;
          else next[k] = v;
        }
        state.files.notes = next;
        saveState();
      } else if (body?.op === 'mkdir' && body.path) {
        state.files.notes[body.path + '/index.md'] = { content: `# ${body.path.split('/').pop()}\n`, modified: Date.now() };
        saveState();
      }
      return jsonResponse({ ok: true });
    }

    // --- Voice & Jarvis ---
    if (path === '/api/voice/node') {
      if (method === 'POST' && body?.machine) { state.voice.machine = body.machine; saveState(); }
      return jsonResponse({
        ok: true,
        machine: state.voice.machine,
        machines: [
          { id: 'local', name: 'workstation.local' },
          { id: 'node-gpu', name: 'gpu-node.local' },
        ],
      });
    }
    if (path === '/api/voice') {
      if (method === 'POST' && body) { state.voice.config = { ...state.voice.config, ...body }; saveState(); }
      return jsonResponse({
        ok: true,
        engine: { installed: true, version: '1.13.8', provider: 'cpu', cpu_installable: true, nvidia: true },
        config: state.voice.config,
        service: { running: state.voice.running },
        download: null,
        disk_bytes: 584000000,
      });
    }
    if (path === '/api/voice/service' && method === 'POST') {
      state.voice.running = body?.action !== 'stop';
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/voice/packs') {
      return jsonResponse({
        ok: true,
        packs: [
          { pack: { id: 'en-cpu-small', language: 'EN', hardware: 'CPU', stt: 'whisper-small-int8', tts: 'kokoro-en-v1', vad: 'silero-vad-v5' }, size: 584000000, installable: true },
          { pack: { id: 'fr-cpu-small', language: 'FR', hardware: 'CPU', stt: 'whisper-small-int8', tts: 'piper-fr-siwis', vad: 'silero-vad-v5' }, size: 540000000, installable: true },
          { pack: { id: 'multi-cpu', language: 'EN/FR', hardware: 'CPU', stt: 'whisper-medium-int8', tts: 'kokoro-multi-v1', vad: 'silero-vad-v5' }, size: 1180000000, installable: true },
        ],
      });
    }
    if (path === '/api/voice/models') {
      return jsonResponse({
        ok: true,
        models: [
          { id: 'whisper-small-int8', kind: 'stt', languages: ['en', 'fr'], family: 'Whisper', installed: true, disk_bytes: 244000000, size: 244000000, installable: true },
          { id: 'kokoro-en-v1', kind: 'tts', languages: ['en'], family: 'Kokoro', installed: true, disk_bytes: 332000000, size: 332000000, installable: true },
          { id: 'piper-fr-siwis', kind: 'tts', languages: ['fr'], family: 'Piper', installed: true, disk_bytes: 68000000, size: 68000000, installable: true },
          { id: 'silero-vad-v5', kind: 'vad', languages: ['multi'], family: 'Silero', installed: true, disk_bytes: 8000000, size: 8000000, installable: true },
        ],
      });
    }
    if (path === '/api/voice/jarvis') {
      if (method === 'POST' && body) { state.voice.jarvis = { ...state.voice.jarvis, ...body }; saveState(); }
      return jsonResponse({
        ok: true,
        ...state.voice.jarvis,
        routes: buildWorkspacePayload().models.slice(0, 6),
      });
    }
    if (path === '/api/voice/download/status') {
      return jsonResponse({ ok: true, download: null });
    }
    if (path === '/api/voice/bench') {
      return jsonResponse({ ok: true, tts: { first_audio_ms: 240, real_time_factor: 0.14 }, stt: { latency_ms: 185, text: 'Loom voice check' }, sample_text: 'Loom voice check' });
    }

    // --- Activity: Quotas, Usage, Market ---
    if (path === '/api/quotas') {
      return jsonResponse({
        ok: true,
        accounts: [
          {
            id: 'claude',
            plan: 'pro',
            source: 'cli',
            limits: [
              { id: 'claude-5h', window: '5h', used: 42, reset_at: state.quotas.reset5h },
              { id: 'claude-7d', window: '7d', used: 58, reset_at: state.quotas.reset7d },
            ],
            resets: { available: state.quotas.claudeResets, checked_at: now - 12 * 60 * 1000, stale: false },
          },
          {
            id: 'codex',
            plan: 'plus',
            source: 'cli',
            limits: [
              { id: 'codex-5h', window: '5h', used: 28, reset_at: state.quotas.codex5h },
              { id: 'codex-7d', window: '7d', used: 39, reset_at: state.quotas.codex7d },
            ],
            resets: { available: state.quotas.codexCredits.length, credits: state.quotas.codexCredits },
          },
          {
            id: 'antigravity',
            plan: 'workspace',
            credits: 1840,
            source: 'cli',
            limits: [
              { id: 'ag-gemini', group: 'Gemini Pro', window: '5h', used: 24, reset_at: now + 3.6 * HOUR },
              { id: 'ag-claude', group: 'Claude', window: '5h', used: 36, reset_at: now + 3.6 * HOUR },
            ],
          },
        ],
        samples: state.quotas.samples,
      });
    }
    if (path === '/api/quotas/reset' && method === 'POST') {
      state.quotas.codexCredits = state.quotas.codexCredits.filter(c => c.id !== body?.credit_id);
      saveState();
      return jsonResponse({ ok: true, outcome: 'reset' });
    }
    if (path === '/api/usage') {
      const pAnthropic = state.usagePrices['cloud:prov-anthropic:claude-sonnet-4-5'];
      const pOpenAI = state.usagePrices['cloud:prov-openai:gpt-4.1'];
      return jsonResponse({
        ok: true,
        quotas: [],
        models: [
          {
            choice_id: 'local:/srv/models/gguf/Qwen3-14B-Q4_K_M.gguf',
            runtime_id: 'llama.cpp',
            kind: 'local',
            name: 'Qwen3-14B-Q4_K_M',
            provider: 'workstation.local',
            turns: 18,
            reported_turns: 18,
            usage: { prompt_tokens: 46200, completion_tokens: 12840 },
          },
          {
            choice_id: 'cloud:prov-anthropic:claude-sonnet-4-5',
            runtime_id: 'openai-compatible',
            kind: 'cloud',
            name: 'claude-sonnet-4-5',
            provider: 'Anthropic',
            turns: 9,
            reported_turns: 9,
            usage: { prompt_tokens: 34800, completion_tokens: 8920 },
            price: pAnthropic,
            estimated_cost: (34800 * pAnthropic.input_per_million + 8920 * pAnthropic.output_per_million) / 1e6,
          },
          {
            choice_id: 'cloud:prov-openai:gpt-4.1',
            runtime_id: 'openai-compatible',
            kind: 'cloud',
            name: 'gpt-4.1',
            provider: 'OpenAI',
            turns: 5,
            reported_turns: 5,
            usage: { prompt_tokens: 21400, completion_tokens: 5110 },
            price: pOpenAI,
            estimated_cost: (21400 * pOpenAI.input_per_million + 5110 * pOpenAI.output_per_million) / 1e6,
          },
        ],
      });
    }
    if (path === '/api/usage/price' && method === 'POST') {
      if (body?.choice_id) {
        state.usagePrices[body.choice_id] = {
          input_per_million: Number(body.input_per_million),
          output_per_million: Number(body.output_per_million),
          currency: body.currency || 'USD',
        };
        saveState();
      }
      return jsonResponse({ ok: true });
    }
    if (path === '/api/usage/providers') {
      return jsonResponse({
        ok: true,
        providers: [
          {
            provider_id: 'prov-openrouter',
            name: 'OpenRouter',
            supported: true,
            currency: 'USD',
            balance: 18.42,
            limit: 25.0,
            limit_remaining: 18.42,
            period_usage: { day: 0.34, week: 2.18, month: 6.58 },
            fetched_at: nowSec - 180,
          },
          {
            provider_id: 'prov-anthropic',
            name: 'Anthropic',
            supported: false,
          },
          {
            provider_id: 'prov-openai',
            name: 'OpenAI',
            supported: false,
          },
        ],
      });
    }
    if (path === '/api/usage/providers/refresh') {
      return jsonResponse({ ok: true });
    }
    if (path === '/api/market') {
      return jsonResponse({ ok: true, ...state.market, catalog_error: '', bench_error: '' });
    }
    if (path === '/api/market/settings' && method === 'POST') {
      state.market.watch = { ...state.market.watch, ...body };
      saveState();
      return jsonResponse({ ok: true, watch: state.market.watch });
    }

    // --- Settings: Security, Agent Access (/mcp/loom), Policy, Doctor, Startup, Notify, Internet ---
    if (path === '/api/mcp/gateway') {
      return jsonResponse({ ok: true, ...state.mcpGateway });
    }
    if (path === '/api/mcp/gateway/token' && method === 'POST') {
      if (body?.revoke) {
        state.mcpGateway.token_set = false;
        saveState();
        return jsonResponse({ ok: true });
      }
      state.mcpGateway.token_set = true;
      saveState();
      return jsonResponse({ ok: true, token: 'loom_demo_simulated_mcp_token_not_real' });
    }
    if (path === '/api/policy') {
      if (method === 'POST' && Array.isArray(body?.rules)) {
        state.policy.rules = body.rules;
        saveState();
      }
      return jsonResponse({ ok: true, rules: state.policy.rules });
    }
    if (path === '/api/policy/pending') {
      return jsonResponse({ ok: true, requests: [] });
    }
    if (path === '/api/policy/audit') {
      return jsonResponse({ ok: true, entries: state.policy.audit });
    }
    if (path === '/api/doctor/run') {
      return jsonResponse({
        ok: true,
        checks: [
          { id: 'core.version', area: 'core', status: 'ok', detail: `${DEMO_VERSION}; linux/amd64` },
          { id: 'core.disk', area: 'core', status: 'ok', detail: 'available_bytes:450000000000' },
          { id: 'engine.llama', area: 'engine', status: 'ok', detail: 'healthy' },
          { id: 'voice.service', area: 'voice', status: 'ok', detail: 'running' },
          { id: 'machine.node-gpu', area: 'machines', status: 'ok', detail: 'reachable' },
          { id: 'machine.node-nas', area: 'machines', status: 'ok', detail: 'reachable' },
          { id: 'brain.root', area: 'brain', status: 'ok', detail: 'ready' },
          { id: 'security.access', area: 'security', status: 'ok', detail: 'password_set' },
        ],
      });
    }
    if (path === '/api/sysprompt') {
      if (method === 'POST' && typeof body?.text === 'string') { state.sysprompt = body.text; saveState(); }
      return jsonResponse({ ok: true, text: state.sysprompt });
    }
    if (path === '/api/agent') {
      return jsonResponse({ ok: true, compact: state.agentCompact, pages: [], skills: [] });
    }
    if (path === '/api/agent/compact' && method === 'POST') {
      state.agentCompact = !!body?.on;
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/https') {
      if (method === 'POST' && body) { state.https = { ...state.https, ...body, running: !!body.enabled }; saveState(); }
      return jsonResponse({ ok: true, ...state.https });
    }
    if (path === '/api/network/web') {
      if (method === 'POST' && body?.exposed !== undefined) { state.networkWeb.exposed = !!body.exposed; saveState(); }
      return jsonResponse({ ok: true, status: state.networkWeb });
    }
    if (path === '/api/network' && method === 'POST') {
      state.engine.exposed = !!body?.exposed;
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/apikey' && method === 'POST') {
      state.engine.key_required = !!body?.on;
      saveState();
      return jsonResponse({ ok: true });
    }
    if (path === '/api/memory') {
      if (method === 'POST' && body?.mode) { state.memoryMode = body.mode; saveState(); }
      return jsonResponse({ ok: true, mode: state.memoryMode });
    }
    if (path === '/api/mem/health') {
      return jsonResponse({ ok: true, ...state.vault });
    }
    if (path === '/api/mem/snapshots') {
      return jsonResponse({
        ok: true,
        snapshots: [
          { id: 'snap-1', when: new Date(now - 6 * HOUR).toISOString(), size: 248000 },
          { id: 'snap-2', when: new Date(now - 30 * HOUR).toISOString(), size: 239000 },
        ],
      });
    }
    if (path === '/api/startup' || /^\/api\/machines\/[^/]+\/(node\/)?startup$/.test(path)) {
      return jsonResponse({
        ok: true,
        supported: true,
        linger: true,
        services: [
          { id: 'ui', unit: 'loom-ui.service', installed: true, enabled: true, user: false },
          { id: 'engine', unit: 'loom-engine.service', installed: true, enabled: true, user: false },
        ],
        policy: { engine: 'llama.cpp', model: '' },
        engines: { llama: true, vllm: false, vllm_models: [] },
        voice: { ready: true, boot: false },
      });
    }
    if (path === '/api/notify') {
      return jsonResponse({
        ok: true,
        secure: true,
        push_subscription_count: 0,
        config: {
          public_base_url: 'https://loom.demo.invalid',
          ntfy: { enabled: false, server_url: 'https://ntfy.sh', topic: '', token_set: false },
          webhook: { enabled: false, url: '', secret_set: false },
          push: { enabled: false },
          rules: {
            events: ['task.waiting', 'task.failed', 'node.offline', 'model.new'],
            completed_after_seconds: 120,
            include_summaries: false,
            quiet_hours: { enabled: false, start: '22:00', end: '07:00', timezone: 'UTC' },
          },
        },
      });
    }
    if (path === '/api/internet') {
      if (method === 'POST' && body) { state.internet = { ...state.internet, ...body }; saveState(); }
      return jsonResponse({ ok: true, ...state.internet });
    }
    if (path === '/api/internet/search') {
      if (method === 'POST' && body) { state.search = { ...state.search, ...body }; saveState(); }
      return jsonResponse({ ok: true, ...state.search });
    }
    if (path === '/api/internet/search/test') {
      return jsonResponse({ ok: true, results: [{ title: 'Loom Documentation (Simulated Result)', url: 'https://docs.loom.lucas-homelab.fr' }] });
    }
    if (path === '/api/workspaces' || /^\/api\/machines\/[^/]+\/workspaces$/.test(path)) {
      return jsonResponse({
        ok: true,
        workspaces: [
          { id: 'ws-homelab', name: 'Homelab Ops', path: '/workspace/homelab-ops' },
          { id: 'ws-research', name: 'Research', path: '/workspace/research-notes' },
        ],
      });
    }
    if (path === '/api/mcp') {
      return jsonResponse({ ok: true, servers: [] });
    }
    if (path === '/api/paths') {
      return jsonResponse({
        ok: true,
        home: '/var/lib/loom',
        database: '/var/lib/loom/loom.db',
        models: '/srv/models/gguf',
        presets: '/var/lib/loom/presets',
        backends: '/usr/local/bin',
      });
    }

    // Fallback for any other /api/* route
    return jsonResponse({ ok: true });
  }

  // ---------- 7. Intercept window.fetch ----------
  const nativeFetch = typeof window !== 'undefined' && typeof window.fetch === 'function' ? window.fetch.bind(window) : null;

  async function demoFetch(input, init = {}) {
    const rawUrl = typeof input === 'string' ? input : (input && input.url) || '';
    let urlObj;
    try {
      urlObj = new URL(rawUrl, typeof window !== 'undefined' ? window.location.origin : 'http://127.0.0.1');
    } catch (_) {
      return nativeFetch ? nativeFetch(input, init) : jsonResponse({ ok: false }, 400);
    }

    if (urlObj.pathname.startsWith('/api/')) {
      const method = String(init.method || (typeof input === 'object' && input.method) || 'GET').toUpperCase();
      let body = null;
      if (init.body && typeof init.body === 'string') {
        try { body = JSON.parse(init.body); } catch (_) { body = null; }
      }
      return handleApi(urlObj, method, body);
    }

    if (nativeFetch) return nativeFetch(input, init);
    return jsonResponse({ ok: false, error: 'Fetch unavailable' }, 404);
  }

  // ---------- 8. Intercept window.EventSource ----------
  const NativeEventSource = typeof window !== 'undefined' ? window.EventSource : undefined;

  class MockEventSource {
    constructor(url) {
      this.url = String(url);
      this.readyState = 0;
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      this._listeners = new Map();
      setTimeout(() => {
        if (this.readyState === 2) return;
        this.readyState = 1;
        const ev = { type: 'open' };
        if (typeof this.onopen === 'function') this.onopen(ev);
        (this._listeners.get('open') || []).forEach(fn => fn(ev));
      }, 20);
    }
    addEventListener(type, fn) {
      if (!this._listeners.has(type)) this._listeners.set(type, []);
      this._listeners.get(type).push(fn);
    }
    removeEventListener(type, fn) {
      const arr = this._listeners.get(type);
      if (arr) this._listeners.set(type, arr.filter(x => x !== fn));
    }
    close() {
      this.readyState = 2;
    }
  }
  MockEventSource.CONNECTING = 0;
  MockEventSource.OPEN = 1;
  MockEventSource.CLOSED = 2;

  function DemoEventSource(url, cfg) {
    const str = String(url || '');
    if (str.includes('/api/')) return new MockEventSource(str);
    return NativeEventSource ? new NativeEventSource(url, cfg) : new MockEventSource(str);
  }
  DemoEventSource.CONNECTING = 0;
  DemoEventSource.OPEN = 1;
  DemoEventSource.CLOSED = 2;

  // ---------- 9. Intercept window.WebSocket ----------
  const NativeWebSocket = typeof window !== 'undefined' ? window.WebSocket : undefined;

  class MockWebSocket {
    constructor(url) {
      this.url = String(url || '');
      this.readyState = 0;
      this.binaryType = 'blob';
      this.onopen = null;
      this.onmessage = null;
      this.onclose = null;
      this.onerror = null;
      this._buf = '';
      this._isCodeman = this.url.includes('/api/codeman/ws/');
      this._isTerminal = this.url.includes('/api/terminals/ws');

      setTimeout(() => {
        if (this.readyState === 3) return;
        this.readyState = 1;
        if (typeof this.onopen === 'function') this.onopen({ type: 'open' });
        if (this._isTerminal) {
          const isGpu = this.url.includes('term-gpu');
          const banner = isGpu
            ? '\x1b[1;32moperator@gpu-node.local\x1b[0m:\x1b[1;34m~\x1b[0m$ nvidia-smi --query-gpu=name,memory.used,memory.total,utilization.gpu --format=csv\r\n' +
              'name, memory.used [MiB], memory.total [MiB], utilization.gpu [%]\r\n' +
              'NVIDIA RTX 3090 (Simulated), 14950 MiB, 24576 MiB, 42 %\r\n\r\n' +
              '\x1b[1;32moperator@gpu-node.local\x1b[0m:\x1b[1;34m~\x1b[0m$ '
            : '\x1b[1;32moperator@workstation.local\x1b[0m:\x1b[1;34m/workspace/homelab-ops\x1b[0m$ git status -sb\r\n' +
              '## main...origin/main [ahead 1]\r\n' +
              ' M compose/monitoring.yml\r\n' +
              '?? scripts/gpu-telemetry-sweep.sh\r\n\r\n' +
              '\x1b[1;32moperator@workstation.local\x1b[0m:\x1b[1;34m/workspace/homelab-ops\x1b[0m$ ';
          this._emit(new TextEncoder().encode(banner).buffer);
        }
      }, 30);
    }
    _emit(data) {
      if (this.readyState === 1 && typeof this.onmessage === 'function') {
        this.onmessage({ data });
      }
    }
    send(payload) {
      if (this.readyState !== 1) return;
      if (this._isCodeman && typeof payload === 'string') {
        try {
          const msg = JSON.parse(payload);
          if (msg.t === 'z') {
            this._emit(JSON.stringify({ t: 'zc', c: msg.c || 100, r: msg.r || 30 }));
          } else if (msg.t === 'i' && typeof msg.d === 'string') {
            this._emit(JSON.stringify({ t: 'o', d: msg.d === '\r' ? '\r\n\x1b[2m[demo] command received in simulated terminal\x1b[0m\r\n> ' : msg.d }));
          }
        } catch (_) {}
        return;
      }
      if (this._isTerminal && typeof payload === 'string') {
        if (payload.startsWith('{') && payload.includes('"resize"')) return;
        const out = payload === '\r'
          ? '\r\n\x1b[2m[demo] simulated shell — no host command executed\x1b[0m\r\n\x1b[1;32moperator@workstation.local\x1b[0m:\x1b[1;34m/workspace/homelab-ops\x1b[0m$ '
          : payload;
        this._emit(new TextEncoder().encode(out).buffer);
      }
    }
    close() {
      this.readyState = 3;
      if (typeof this.onclose === 'function') this.onclose({ code: 1000, reason: 'closed' });
    }
  }
  MockWebSocket.CONNECTING = 0;
  MockWebSocket.OPEN = 1;
  MockWebSocket.CLOSING = 2;
  MockWebSocket.CLOSED = 3;

  function DemoWebSocket(url, protocols) {
    const str = String(url || '');
    if (str.includes('/api/')) return new MockWebSocket(str);
    return NativeWebSocket ? new NativeWebSocket(url, protocols) : new MockWebSocket(str);
  }
  DemoWebSocket.CONNECTING = 0;
  DemoWebSocket.OPEN = 1;
  DemoWebSocket.CLOSING = 2;
  DemoWebSocket.CLOSED = 3;

  if (typeof window !== 'undefined') {
    if (typeof navigator !== 'undefined' && navigator.onLine === false) {
      try {
        Object.defineProperty(navigator, 'onLine', { get: () => true, configurable: true });
      } catch (_) {}
    }
    window.fetch = demoFetch;
    window.EventSource = DemoEventSource;
    window.WebSocket = DemoWebSocket;
    window.__LOOM_DEMO__ = {
      version: DEMO_VERSION,
      handleApi,
      getState: () => state,
      resetState: () => {
        state = createInitialState(state.lang);
        saveState();
      },
    };
  }
})();
