// Loom Client-Side In-Memory Mock Engine
// Intercepts all window.fetch calls for /api/* and /v1/*
(function() {
  const STORAGE_PREFIX = 'loom_demo_';

  function getStore(key, defaultValue) {
    try {
      const val = sessionStorage.getItem(STORAGE_PREFIX + key);
      return val ? JSON.parse(val) : defaultValue;
    } catch {
      return defaultValue;
    }
  }

  function setStore(key, value) {
    try {
      sessionStorage.setItem(STORAGE_PREFIX + key, JSON.stringify(value));
    } catch {}
  }

  // Initial Seed Data
  const defaultModels = [
    {
      id: "qwen2.5-7b-instruct-q4_k_m.gguf",
      name: "Qwen 2.5 7B Instruct",
      filename: "qwen2.5-7b-instruct-q4_k_m.gguf",
      path: "/home/demo/.local/share/loom/models/qwen2.5-7b-instruct-q4_k_m.gguf",
      size: 4920780800,
      size_human: "4.7 Go",
      quant: "Q4_K_M",
      context_len: 32768,
      active: true,
      modified: "2026-09-18 14:20"
    },
    {
      id: "deepseek-r1-distill-qwen-14b-q4_k_m.gguf",
      name: "DeepSeek R1 Distill Qwen 14B",
      filename: "deepseek-r1-distill-qwen-14b-q4_k_m.gguf",
      path: "/home/demo/.local/share/loom/models/deepseek-r1-distill-qwen-14b-q4_k_m.gguf",
      size: 9437184000,
      size_human: "9.1 Go",
      quant: "Q4_K_M",
      context_len: 65536,
      active: false,
      modified: "2026-09-15 09:12"
    },
    {
      id: "llama-3.3-70b-instruct-iq3_xxs.gguf",
      name: "Llama 3.3 70B Instruct",
      filename: "llama-3.3-70b-instruct-iq3_xxs.gguf",
      path: "/home/demo/.local/share/loom/models/llama-3.3-70b-instruct-iq3_xxs.gguf",
      size: 29527900160,
      size_human: "28.2 Go",
      quant: "IQ3_XXS",
      context_len: 131072,
      active: false,
      modified: "2026-09-10 18:45"
    }
  ];

  const defaultPresets = [
    {
      id: "default",
      name: "Généraliste (Instruct)",
      model: "qwen2.5-7b-instruct-q4_k_m.gguf",
      temperature: 0.7,
      top_p: 0.9,
      min_p: 0.05,
      n_ctx: 32768,
      n_gpu_layers: 33,
      system_prompt: "Vous êtes un assistant IA précis, rigoureux et bienveillant tournant localement sur le poste de travail de l'utilisateur.",
      active: true
    },
    {
      id: "coding",
      name: "Code & Architecture",
      model: "qwen2.5-7b-instruct-q4_k_m.gguf",
      temperature: 0.2,
      top_p: 0.85,
      min_p: 0.08,
      n_ctx: 65536,
      n_gpu_layers: 33,
      system_prompt: "Vous êtes un ingénieur logiciel expert en Go, C++, Linux et systèmes d'inférence haute performance.",
      active: false
    }
  ];

  const defaultChatTurn = [
    {
      user: "Comment fonctionne le continuous batching dans Loom ?",
      reasoning: "L'utilisateur souhaite comprendre le mécanisme de multiplexage de requêtes dans llama-server sous Loom.\n1. Présenter le problème de sérialisation classique.\n2. Exposer le drapeau -np 4 et l'assignation de slots continus.\n3. Mentionner le partage du cache KV sans blocage de tête de file.",
      assistant: "Le **Continuous Batching** dans Loom permet de servir plusieurs clients en parallèle sans que les requêtes ne se bloquent mutuellement en tête de file.\n\n### Principes clés :\n1. **Slots parallèles (`-np 4`)** : Loom lance le binaire `llama-server` avec 4 slots indépendants préalloués.\n2. **Zéro blocage de tête de file** : Lorsqu'un utilisateur génère une longue réponse de chat, un IDE comme Cursor ou Continue peut immédiatement envoyer une complétion sur le Slot 1 sans attendre.\n3. **Gestion intelligente du cache KV** : La mémoire des tokens est partagée et réallouée dynamiquement sans dépasser le budget VRAM configuré.",
      stats: { gen_tokens: 92, speed: 48.6, prompt_tokens_total: 28 },
      elapsed_ms: 1890
    }
  ];

  let conversationTurns = getStore('chat_turns', defaultChatTurn);
  let pendingGeneration = null;

  // Real fetch backup
  const originalFetch = window.fetch;

  window.fetch = async function(resource, init = {}) {
    let url = typeof resource === 'string' ? resource : resource.url;
    const method = (init.method || 'GET').toUpperCase();

    // Extract path
    let path = url;
    try {
      if (url.startsWith('http://') || url.startsWith('https://')) {
        const u = new URL(url);
        path = u.pathname + u.search;
      }
    } catch {}

    // Only intercept endpoints containing /api/ or /v1/
    if (!path.includes('/api/') && !path.includes('/v1/')) {
      return originalFetch.apply(this, arguments);
    }
    // Normalize path to start from /api/ or /v1/
    const apiIndex = path.indexOf('/api/');
    if (apiIndex !== -1) {
      path = path.slice(apiIndex);
    }

    // Helper JSON response
    const jsonRes = (data, status = 200) => {
      return new Response(JSON.stringify(data), {
        status,
        headers: { 'Content-Type': 'application/json' }
      });
    };

    // Helper Text response
    const textRes = (text, status = 200) => {
      return new Response(text, {
        status,
        headers: { 'Content-Type': 'text/plain; charset=utf-8' }
      });
    };

    // --- /api/ping ---
    if (path.startsWith('/api/ping')) {
      return jsonRes({ ok: true, pong: Date.now() });
    }

    // --- /api/status ---
    if (path.startsWith('/api/status')) {
      const activeModel = defaultModels.find(m => m.active) || defaultModels[0];
      return jsonRes({
        ok: true,
        status: "ok",
        engine: "llama-server",
        engine_status: "running",
        engine_installed: true,
        running: true,
        pid: 48921,
        port: 8081,
        host: "127.0.0.1",
        lan: false,
        model: activeModel.name,
        model_path: activeModel.path,
        slots: 4,
        slots_total: 4,
        slots_idle: pendingGeneration ? 2 : 3,
        slots_active: pendingGeneration ? 2 : 1,
        vram_used: 5120,
        vram_total: 16384,
        ram_used: 8192,
        ram_total: 32768,
        uptime: "4h 12m"
      });
    }

    // --- /api/vram ---
    if (path.startsWith('/api/vram')) {
      return jsonRes({
        total_mb: 16384,
        used_mb: 5120,
        free_mb: 11264,
        gpus: [
          {
            id: 0,
            name: "NVIDIA GeForce RTX 4090 (Simulé)",
            total_mb: 24576,
            used_mb: 5120,
            free_mb: 19456,
            compute_cap: "8.9"
          }
        ]
      });
    }

    // --- /api/ram ---
    if (path.startsWith('/api/ram')) {
      return jsonRes({
        total_mb: 32768,
        used_mb: 8192,
        free_mb: 24576
      });
    }

    // --- /api/paths ---
    if (path.startsWith('/api/paths')) {
      return jsonRes({
        models: "/home/demo/.local/share/loom/models",
        bin: "/usr/local/bin/llama-server",
        home: "/home/demo/.local/share/loom"
      });
    }

    // --- /api/config ---
    if (path.startsWith('/api/config')) {
      return jsonRes({
        PORT: "8081",
        NP: "4",
        CTX: "32768",
        NGL: "33",
        VRAM: "auto",
        FLASH_ATTN: "1"
      });
    }

    // --- /api/models ---
    if (path.startsWith('/api/models')) {
      if (path.startsWith('/api/models/dirs')) {
        return jsonRes(["/home/demo/.local/share/loom/models"]);
      }
      return jsonRes(getStore('models', defaultModels));
    }

    // --- /api/presets ---
    if (path.startsWith('/api/presets')) {
      return jsonRes(getStore('presets', defaultPresets));
    }

    if (path.startsWith('/api/preset')) {
      const presets = getStore('presets', defaultPresets);
      return jsonRes(presets[0]);
    }

    // --- /api/backends ---
    if (path.startsWith('/api/backends')) {
      return jsonRes([
        {
          id: "llamacpp",
          name: "llama.cpp (Poste de travail)",
          current: true,
          path: "/usr/local/bin/llama-server",
          version: "b4820 (AVX2 CUDA 12)",
          status: "ready"
        }
      ]);
    }

    // --- /api/chat/history ---
    if (path.startsWith('/api/chat/history')) {
      return jsonRes({
        conversations: [
          {
            id: "demo-conv-1",
            title: conversationTurns[0]?.user || "Continuous batching dans llama.cpp",
            updated_at: Math.floor(Date.now() / 1000),
            messages_count: conversationTurns.length * 2,
            project_id: ""
          }
        ],
        active: "demo-conv-1"
      });
    }

    // --- /api/chat/state ---
    if (path.startsWith('/api/chat/state')) {
      return jsonRes({
        generating: !!pendingGeneration,
        busy: !!pendingGeneration,
        ctx_used: 128,
        compact_count: 0
      });
    }

    // --- /api/chat/send ---
    if (path.startsWith('/api/chat/send') && method === 'POST') {
      let body = {};
      try { body = JSON.parse(init.body); } catch {}
      const userMsg = body.message || "Bonjour !";

      // Simulate a smart assistant response
      let reasonText = "Analyse de la demande utilisateur dans le contexte de la station Loom.\n1. Déterminer les points techniques essentiels.\n2. Rédiger une réponse structurée avec métriques réelles.";
      let replyText = `Voici une réponse simulée en temps réel depuis le moteur Loom.\n\nVous avez demandé : *« ${userMsg} »*\n\n- **Modèle actif** : Qwen 2.5 7B Instruct (Q4_K_M)\n- **Slots disponibles** : 4/4 en continuous batching (-np 4)\n- **Latence de premier token (TTFT)** : ~14 ms\n- **Vitesse de décodage** : 48.4 tok/s`;

      if (userMsg.toLowerCase().includes("vram") || userMsg.toLowerCase().includes("mémoire")) {
        reasonText = "L'utilisateur interroge le budget VRAM.\nCalculer le poids des tenseurs Q4_K_M + la réservation contextuelle KV pour 32k tokens sur 4 slots.";
        replyText = `### Dimensionnement VRAM dans Loom\n\nPour un modèle 7B en quantification **Q4_K_M** avec 4 slots de 32 768 tokens :\n\n- **Poids du modèle** : ~4.7 Go\n- **Cache KV par slot** : 512 Mo (Flash Attention activé)\n- **Cache KV total (4 slots)** : 2.0 Go\n- **Mémoire d'activation & scratch** : ~400 Mo\n\n**Budget total requis** : **~7.1 Go de VRAM**, s'insérant parfaitement dans un GPU 8 Go ou 16 Go sans débordement sur la RAM système.`;
      } else if (userMsg.toLowerCase().includes("code") || userMsg.toLowerCase().includes("python") || userMsg.toLowerCase().includes("curl")) {
        reasonText = "L'utilisateur demande un exemple d'intégration avec l'endpoint /v1 de Loom.";
        replyText = `Voici comment interroger le proxy Loom depuis Python via le SDK standard OpenAI :\n\n\`\`\`python\nfrom openai import OpenAI\n\n# Loom écoute sur le port 8091 en local\nclient = OpenAI(\n    base_url="http://127.0.0.1:8091/v1",\n    api_key="loom-local" # Clé ignorée en local\n)\n\nresponse = client.chat.completions.create(\n    model="qwen2.5-7b-instruct",\n    messages=[{"role": "user", "content": "Explique les KV caches"}],\n    stream=True\n)\n\nfor chunk in response:\n    print(chunk.choices[0].delta.content or "", end="")\n\`\`\``;
      }

      const newTurn = {
        user: userMsg,
        reasoning: reasonText,
        assistant: replyText,
        stats: { gen_tokens: 114, speed: 49.2, prompt_tokens_total: 18 },
        elapsed_ms: 2150
      };

      conversationTurns.push(newTurn);
      setStore('chat_turns', conversationTurns);
      pendingGeneration = newTurn;

      return jsonRes({ ok: true });
    }

    // --- /api/chat (SSE Stream) ---
    if (path.startsWith('/api/chat') && method === 'POST') {
      const encoder = new TextEncoder();

      const stream = new ReadableStream({
        async start(controller) {
          // Replay all turns
          for (let idx = 0; idx < conversationTurns.length; idx++) {
            const turn = conversationTurns[idx];

            // User prompt
            const userChunk = `data: ${JSON.stringify({ choices: [{ delta: { user: turn.user } }] })}\n\n`;
            controller.enqueue(encoder.encode(userChunk));
            await new Promise(r => setTimeout(r, 20));

            // Reasoning block
            if (turn.reasoning) {
              const reasonChunk = `data: ${JSON.stringify({ choices: [{ delta: { reasoning_content: turn.reasoning } }] })}\n\n`;
              controller.enqueue(encoder.encode(reasonChunk));
              await new Promise(r => setTimeout(r, 20));
            }

            // Assistant content
            const contentChunk = `data: ${JSON.stringify({ choices: [{ delta: { content: turn.assistant } }] })}\n\n`;
            controller.enqueue(encoder.encode(contentChunk));
            await new Promise(r => setTimeout(r, 20));

            // Finalize turn
            const doneChunk = `data: ${JSON.stringify({
              choices: [{
                delta: {
                  stats: turn.stats,
                  elapsed_ms: turn.elapsed_ms,
                  turn_done: true
                }
              }]
            })}\n\n`;
            controller.enqueue(encoder.encode(doneChunk));
            await new Promise(r => setTimeout(r, 20));
          }

          // Caught up
          const caughtUpChunk = `data: ${JSON.stringify({ choices: [{ delta: { caught_up: true } }] })}\n\n`;
          controller.enqueue(encoder.encode(caughtUpChunk));

          pendingGeneration = null;
        }
      });

      return new Response(stream, {
        status: 200,
        headers: {
          'Content-Type': 'text/event-stream',
          'Cache-Control': 'no-cache',
          'Connection': 'keep-alive'
        }
      });
    }

    // --- /api/chat/stop ---
    if (path.startsWith('/api/chat/stop')) {
      pendingGeneration = null;
      return jsonRes({ ok: true });
    }

    // --- /api/chat/reset ---
    if (path.startsWith('/api/chat/reset')) {
      conversationTurns = [...defaultChatTurn];
      setStore('chat_turns', conversationTurns);
      return jsonRes({ ok: true });
    }

    // --- /api/server (Slots & Stats) ---
    if (path.startsWith('/api/server')) {
      return jsonRes({
        running: true,
        slots: [
          { id: 0, state: pendingGeneration ? "processing" : "idle", tokens_second: pendingGeneration ? 48.4 : 0, n_decoded: 92, model: "Qwen 2.5 7B Instruct", task: "Chat interactif" },
          { id: 1, state: "idle", tokens_second: 0, n_decoded: 0, model: "Qwen 2.5 7B Instruct", task: "Standby (Cursor /v1)" },
          { id: 2, state: "idle", tokens_second: 0, n_decoded: 0, model: "Qwen 2.5 7B Instruct", task: "Standby (Agent)" },
          { id: 3, state: "idle", tokens_second: 0, n_decoded: 0, model: "Qwen 2.5 7B Instruct", task: "Standby" }
        ],
        vram_used: 5120,
        vram_total: 16384,
        n_ctx: 32768,
        np: 4
      });
    }

    // --- /api/bench ---
    if (path.startsWith('/api/bench')) {
      return jsonRes({
        running: false,
        results: [
          {
            model: "Qwen 2.5 7B Instruct (Q4_K_M)",
            prefill_toks: 680,
            decode_toks: 54,
            n_ctx: 4096,
            timestamp: 1726880000
          },
          {
            model: "DeepSeek R1 Distill Qwen 14B (Q4_K_M)",
            prefill_toks: 410,
            decode_toks: 32,
            n_ctx: 4096,
            timestamp: 1726880000
          },
          {
            model: "Llama 3.3 70B Instruct (IQ3_XXS)",
            prefill_toks: 140,
            decode_toks: 11,
            n_ctx: 4096,
            timestamp: 1726880000
          }
        ]
      });
    }

    // --- /api/hub/search ---
    if (path.startsWith('/api/hub/search')) {
      return jsonRes({
        models: [
          {
            id: "Qwen/Qwen2.5-7B-Instruct-GGUF",
            likes: 1420,
            downloads: 85200,
            quant_recommend: "q4_k_m"
          },
          {
            id: "deepseek-ai/DeepSeek-R1-Distill-Qwen-14B-GGUF",
            likes: 3890,
            downloads: 145000,
            quant_recommend: "q4_k_m"
          },
          {
            id: "bartowski/Llama-3.3-70B-Instruct-GGUF",
            likes: 2100,
            downloads: 62000,
            quant_recommend: "iq3_xxs"
          }
        ]
      });
    }

    // Fallback for any other /api endpoint: return { ok: true }
    return jsonRes({ ok: true, simulated: true });
  };
})();
