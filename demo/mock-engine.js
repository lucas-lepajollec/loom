// Loom Client-Side In-Memory Mock Engine
// High-fidelity simulation of llama.cpp inference, VRAM/RAM metrics,
// Hugging Face Hub search & downloads, model loading, and parameter tuning.
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

  // --- Initial Seed Data ---
  const defaultModels = [
    {
      name: "qwen2.5-7b-instruct-q4_k_m.gguf",
      value: "/home/demo/.local/share/loom/models/qwen2.5-7b-instruct-q4_k_m.gguf",
      path: "/home/demo/.local/share/loom/models/qwen2.5-7b-instruct-q4_k_m.gguf",
      dir: "/home/demo/.local/share/loom/models",
      size: 4920780800,
      quant: "Q4_K_M",
      context_len: 32768,
      modified: "2026-09-18 14:20"
    },
    {
      name: "deepseek-r1-distill-qwen-14b-q4_k_m.gguf",
      value: "/home/demo/.local/share/loom/models/deepseek-r1-distill-qwen-14b-q4_k_m.gguf",
      path: "/home/demo/.local/share/loom/models/deepseek-r1-distill-qwen-14b-q4_k_m.gguf",
      dir: "/home/demo/.local/share/loom/models",
      size: 9437184000,
      quant: "Q4_K_M",
      context_len: 65536,
      modified: "2026-09-15 09:12"
    },
    {
      name: "llama-3.3-70b-instruct-iq3_xxs.gguf",
      value: "/home/demo/.local/share/loom/models/llama-3.3-70b-instruct-iq3_xxs.gguf",
      path: "/home/demo/.local/share/loom/models/llama-3.3-70b-instruct-iq3_xxs.gguf",
      dir: "/home/demo/.local/share/loom/models",
      size: 29527900160,
      quant: "IQ3_XXS",
      context_len: 131072,
      modified: "2026-09-10 18:45"
    }
  ];

  const defaultPresets = [
    {
      id: "general",
      name: "Généraliste (Instruct)",
      model: "/home/demo/.local/share/loom/models/qwen2.5-7b-instruct-q4_k_m.gguf",
      quant: "Q4_K_M",
      ctx: 32768,
      active: true,
      content: "MODEL=/home/demo/.local/share/loom/models/qwen2.5-7b-instruct-q4_k_m.gguf\nCTX=32768\nNGL=33\nTEMP=0.7\nTOP_P=0.9\nMIN_P=0.05\nBATCH=2048\nUBATCH=512\nTHREADS=8\nFLASH_ATTN=1"
    },
    {
      id: "coding",
      name: "Code & Architecture",
      model: "/home/demo/.local/share/loom/models/qwen2.5-7b-instruct-q4_k_m.gguf",
      quant: "Q4_K_M",
      ctx: 65536,
      active: false,
      content: "MODEL=/home/demo/.local/share/loom/models/qwen2.5-7b-instruct-q4_k_m.gguf\nCTX=65536\nNGL=33\nTEMP=0.2\nTOP_P=0.85\nMIN_P=0.08\nBATCH=2048\nUBATCH=512\nTHREADS=8\nFLASH_ATTN=1"
    },
    {
      id: "reasoning",
      name: "Raisonnement Approfondi",
      model: "/home/demo/.local/share/loom/models/deepseek-r1-distill-qwen-14b-q4_k_m.gguf",
      quant: "Q4_K_M",
      ctx: 65536,
      active: false,
      content: "MODEL=/home/demo/.local/share/loom/models/deepseek-r1-distill-qwen-14b-q4_k_m.gguf\nCTX=65536\nNGL=48\nTEMP=0.6\nTOP_P=0.95\nMIN_P=0.05\nBATCH=2048\nUBATCH=512\nTHREADS=8\nFLASH_ATTN=1"
    }
  ];

  const defaultChatTurn = [
    {
      user: "Quelle est la différence concrète entre les quantifications GGUF Q4_K_M et Q8_0 pour un usage local ?",
      reasoning: "1. Analyser les deux quantifications GGUF courantes : Q4_K_M (k-quants 4-bit) et Q8_0 (8-bit standard).\n2. Comparer les trois dimensions clés :\n   - Empreinte VRAM / RAM\n   - Vitesse d'inférence (bande passante mémoire GPU)\n   - Perte de perplexité / précision\n3. Donner une recommandation pratique pour llama.cpp sur station de travail.",
      assistant: "Dans **llama.cpp**, le format GGUF propose plusieurs niveaux de quantification avec des compromis distincts :\n\n### 1. Compromis Mémoire & VRAM\n- **Q4_K_M (4 bits, K-Quants Medium)** :\n  - Utilise un schéma hybride : les couches d'attention critiques et les tenseurs sensibles sont quantifiés en 5 ou 6 bits, tandis que les autres sont en 4 bits.\n  - **Empreinte VRAM** : ~4.7 Go pour un modèle 7B, ~9.2 Go pour un 14B.\n  - **Précision** : Perte de perplexité inférieure à 0.05 par rapport au modèle FP16 d'origine.\n- **Q8_0 (8 bits standard)** :\n  - Quantification uniforme sur 8 bits (quasi sans perte perceptible).\n  - **Empreinte VRAM** : Presque le double (~8.5 Go pour un 7B, ~16.5 Go pour un 14B).\n\n### 2. Vitesse d'exécution\nSur GPU (CUDA / Metal), la vitesse de génération en phase de décodage est directement limitée par la **bande passante mémoire** (*memory bandwidth bound*).\n- **Q4_K_M** nécessite de transférer 2× moins d'octets par token généré, ce qui le rend **35 à 50 % plus rapide** que le Q8_0 sur la plupart des cartes graphiques.\n\n### Recommandation pratique\nPour 95 % des cas d'usage locaux, le **Q4_K_M** offre le meilleur équilibre entre vitesse de génération maximale et fidélité du raisonnement.",
      stats: { gen_tokens: 184, speed: 52.4, prompt_tokens_total: 26 },
      elapsed_ms: 2200
    }
  ];

  // --- Reactive In-Memory State ---
  let localModels = getStore('models', defaultModels);
  let localPresets = getStore('presets', defaultPresets);
  let activeModelPath = getStore('active_model', "/home/demo/.local/share/loom/models/qwen2.5-7b-instruct-q4_k_m.gguf");
  let activePresetId = getStore('active_preset', "general");
  let conversationTurns = getStore('chat_turns', defaultChatTurn);
  let activeDownloads = {};
  let pendingGeneration = null;
  let activeStreamController = null;

  function baseName(path) {
    if (!path) return '';
    const idx = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    return idx >= 0 ? path.slice(idx + 1) : path;
  }

  function getActiveModelObj() {
    if (!activeModelPath) return null;
    return localModels.find(m => m.path === activeModelPath || m.value === activeModelPath || m.name === baseName(activeModelPath)) || {
      name: baseName(activeModelPath),
      path: activeModelPath,
      value: activeModelPath,
      size: 4920780800
    };
  }

  function computeVramMb() {
    const m = getActiveModelObj();
    if (!m) return 420; // baseline OS GPU idle
    const n = m.name.toLowerCase();
    if (n.includes('70b')) return 23200;
    if (n.includes('32b')) return 20400;
    if (n.includes('24b')) return 15800;
    if (n.includes('14b')) return 9800;
    if (n.includes('7b') || n.includes('8b')) return 5840;
    return 6200;
  }

  function computeRamMb() {
    return activeModelPath ? 10444 : 4120;
  }

  // --- Hugging Face Hub Catalog ---
  const hubCatalog = [
    {
      id: "deepseek-ai/DeepSeek-R1-Distill-Qwen-14B-GGUF",
      author: "deepseek-ai",
      params_b: 14,
      pipeline: "text-generation",
      downloads: 145000,
      likes: 3890,
      size: 9437184000,
      quant_recommend: "Q4_K_M",
      license: "MIT",
      readme: "### DeepSeek-R1-Distill-Qwen-14B\n\nModèle distillé issu de **DeepSeek-R1** combiné avec l'architecture performante de Qwen 2.5 14B.\n\n- Excellentes capacités de raisonnement pas-à-pas avec balises `<think>`\n- Format GGUF optimisé pour GPU 12-16 Go\n- Contexte natif : 65 536 jetons",
      files: [
        { name: "deepseek-r1-distill-qwen-14b-q4_k_m.gguf", quant: "Q4_K_M", size: 9437184000, url: "https://huggingface.co/deepseek-ai/DeepSeek-R1-Distill-Qwen-14B-GGUF/resolve/main/deepseek-r1-distill-qwen-14b-q4_k_m.gguf" },
        { name: "deepseek-r1-distill-qwen-14b-q5_k_m.gguf", quant: "Q5_K_M", size: 10850000000, url: "https://huggingface.co/deepseek-ai/DeepSeek-R1-Distill-Qwen-14B-GGUF/resolve/main/deepseek-r1-distill-qwen-14b-q5_k_m.gguf" },
        { name: "deepseek-r1-distill-qwen-14b-q8_0.gguf", quant: "Q8_0", size: 15400000000, url: "https://huggingface.co/deepseek-ai/DeepSeek-R1-Distill-Qwen-14B-GGUF/resolve/main/deepseek-r1-distill-qwen-14b-q8_0.gguf" }
      ]
    },
    {
      id: "Qwen/Qwen2.5-Coder-32B-Instruct-GGUF",
      author: "Qwen",
      params_b: 32,
      pipeline: "text-generation",
      downloads: 89200,
      likes: 2410,
      size: 19800000000,
      quant_recommend: "Q4_K_M",
      license: "Apache-2.0",
      readme: "### Qwen2.5-Coder-32B-Instruct\n\nLe modèle open-weight de référence pour la programmation, l'architecture logicielle et le débogage.\n\n- Support de plus de 90 langages de programmation\n- Équivalent Claude 3.5 Sonnet sur les benchmarks de code\n- Optimisé pour GPU 24 Go (RTX 4090 / 3090)",
      files: [
        { name: "qwen2.5-coder-32b-instruct-q4_k_m.gguf", quant: "Q4_K_M", size: 19800000000, url: "https://huggingface.co/Qwen/Qwen2.5-Coder-32B-Instruct-GGUF/resolve/main/qwen2.5-coder-32b-instruct-q4_k_m.gguf" },
        { name: "qwen2.5-coder-32b-instruct-iq3_m.gguf", quant: "IQ3_M", size: 15200000000, url: "https://huggingface.co/Qwen/Qwen2.5-Coder-32B-Instruct-GGUF/resolve/main/qwen2.5-coder-32b-instruct-iq3_m.gguf" },
        { name: "qwen2.5-coder-32b-instruct-q8_0.gguf", quant: "Q8_0", size: 34100000000, url: "https://huggingface.co/Qwen/Qwen2.5-Coder-32B-Instruct-GGUF/resolve/main/qwen2.5-coder-32b-instruct-q8_0.gguf" }
      ]
    },
    {
      id: "mistralai/Mistral-Small-24B-Instruct-2501-GGUF",
      author: "mistralai",
      params_b: 24,
      pipeline: "text-generation",
      downloads: 64300,
      likes: 1820,
      size: 14200000000,
      quant_recommend: "Q4_K_M",
      license: "Apache-2.0",
      readme: "### Mistral Small 24B Instruct 2501\n\nModèle compact de dernière génération par Mistral AI, taillé pour le raisonnement logique, la rédaction et l'appel d'outils (function calling).\n\n- Fenêtre de contexte de 32 768 jetons\n- Inférence ultra-rapide sur stations de travail modernes",
      files: [
        { name: "mistral-small-24b-instruct-2501-q4_k_m.gguf", quant: "Q4_K_M", size: 14200000000, url: "https://huggingface.co/mistralai/Mistral-Small-24B-Instruct-2501-GGUF/resolve/main/mistral-small-24b-instruct-2501-q4_k_m.gguf" },
        { name: "mistral-small-24b-instruct-2501-q5_k_m.gguf", quant: "Q5_K_M", size: 16800000000, url: "https://huggingface.co/mistralai/Mistral-Small-24B-Instruct-2501-GGUF/resolve/main/mistral-small-24b-instruct-2501-q5_k_m.gguf" }
      ]
    },
    {
      id: "meta-llama/Llama-3.3-70B-Instruct-GGUF",
      author: "meta-llama",
      params_b: 70,
      pipeline: "text-generation",
      downloads: 112000,
      likes: 4120,
      size: 29500000000,
      quant_recommend: "IQ3_XXS",
      license: "Llama-3.3",
      readme: "### Llama 3.3 70B Instruct\n\nLe modèle ouvert haut de gamme de Meta, offrant des performances comparables aux modèles propriétaires de premier plan.\n\n- Contexte étendu jusqu'à 131 072 jetons\n- Quantification IQ3_XXS permettant de tourner sur un GPU 24 Go unique",
      files: [
        { name: "llama-3.3-70b-instruct-iq3_xxs.gguf", quant: "IQ3_XXS", size: 29527900160, url: "https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct-GGUF/resolve/main/llama-3.3-70b-instruct-iq3_xxs.gguf" },
        { name: "llama-3.3-70b-instruct-q4_k_m.gguf", quant: "Q4_K_M", size: 42800000000, url: "https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct-GGUF/resolve/main/llama-3.3-70b-instruct-q4_k_m.gguf" }
      ]
    },
    {
      id: "Qwen/Qwen2.5-7B-Instruct-GGUF",
      author: "Qwen",
      params_b: 7,
      pipeline: "text-generation",
      downloads: 210000,
      likes: 5400,
      size: 4920780800,
      quant_recommend: "Q4_K_M",
      license: "Apache-2.0",
      readme: "### Qwen 2.5 7B Instruct\n\nModèle polyvalent et agile, idéal pour les stations bureautiques et ordinateurs portables avec 8 Go de VRAM.\n\n- 50+ tokens par seconde sur GPU NVIDIA RTX 40-series\n- Contexte de 32 768 jetons",
      files: [
        { name: "qwen2.5-7b-instruct-q4_k_m.gguf", quant: "Q4_K_M", size: 4920780800, url: "https://huggingface.co/Qwen/Qwen2.5-7B-Instruct-GGUF/resolve/main/qwen2.5-7b-instruct-q4_k_m.gguf" },
        { name: "qwen2.5-7b-instruct-q8_0.gguf", quant: "Q8_0", size: 8120000000, url: "https://huggingface.co/Qwen/Qwen2.5-7B-Instruct-GGUF/resolve/main/qwen2.5-7b-instruct-q8_0.gguf" }
      ]
    }
  ];

  // Intercept window.fetch
  const originalFetch = window.fetch;

  window.fetch = async function(resource, init = {}) {
    let url = typeof resource === 'string' ? resource : resource.url;
    const method = (init.method || 'GET').toUpperCase();

    let path = url;
    try {
      if (url.startsWith('http://') || url.startsWith('https://')) {
        const u = new URL(url);
        path = u.pathname + u.search;
      }
    } catch {}

    if (!path.includes('/api/') && !path.includes('/v1/')) {
      return originalFetch.apply(this, arguments);
    }
    const apiIndex = path.indexOf('/api/');
    if (apiIndex !== -1) {
      path = path.slice(apiIndex);
    }

    const jsonRes = (data, status = 200) => new Response(JSON.stringify(data), {
      status,
      headers: { 'Content-Type': 'application/json' }
    });

    // --- /api/ping ---
    if (path.startsWith('/api/ping')) {
      return jsonRes({ ok: true, pong: Date.now() });
    }

    // --- /api/status ---
    if (path.startsWith('/api/status')) {
      const activeObj = getActiveModelObj();
      const vramMb = computeVramMb();
      const ramMb = computeRamMb();
      return jsonRes({
        ok: true,
        status: "ok",
        active: !!activeObj,
        health: true,
        running: !!activeObj,
        engine: "llama-server",
        engine_status: activeObj ? "running" : "stopped",
        engine_installed: true,
        pid: activeObj ? 48921 : 0,
        port: 8081,
        host: "127.0.0.1",
        lan: false,
        model: activeObj ? activeObj.path : "",
        model_name: activeObj ? activeObj.name : "",
        preset_name: activePresetId ? (localPresets.find(p=>p.id===activePresetId)||{}).name : "",
        slots: 4,
        slots_total: 4,
        slots_idle: pendingGeneration ? 2 : 4,
        slots_active: pendingGeneration ? 2 : 0,
        vram_used: vramMb,
        vram_total: 24576,
        ram_used: ramMb,
        ram_total: 32768,
        uptime: "4h 12m",
        version: "0.9.4"
      });
    }

    // --- /api/vram (MUST return Array of GPU objects for loadVram) ---
    if (path.startsWith('/api/vram')) {
      const vramMb = computeVramMb();
      return jsonRes([
        {
          name: "NVIDIA GeForce RTX 4090",
          used: vramMb,
          total: 24576,
          util: activeModelPath ? 14 : 1,
          temp: activeModelPath ? 42 : 34
        }
      ]);
    }

    // --- /api/ram (MUST return { used, total } for loadRam) ---
    if (path.startsWith('/api/ram')) {
      const ramMb = computeRamMb();
      return jsonRes({
        used: ramMb,
        total: 32768
      });
    }

    // --- /api/memory ---
    if (path.startsWith('/api/memory')) {
      const ramMb = computeRamMb();
      return jsonRes({
        used: ramMb,
        total: 32768,
        free: 32768 - ramMb
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
      const activeObj = getActiveModelObj();
      return jsonRes({
        PORT: "8081",
        BIN: "/usr/local/bin/llama-server",
        MODEL: activeObj ? activeObj.path : "",
        CTX: "32768",
        BATCH: "2048",
        UBATCH: "512",
        NGL: "33",
        THREADS: "8",
        TEMP: "0.7",
        TOP_P: "0.9",
        MIN_P: "0.05",
        FLASH_ATTN: "1"
      });
    }

    // --- /api/llamacpp ---
    if (path.startsWith('/api/llamacpp')) {
      return jsonRes({
        kind: "server",
        path: "/usr/local/bin/llama-server",
        version: "b4820 (AVX2 CUDA 12)",
        status: "ready",
        in_use: true
      });
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

    // --- /api/models ---
    if (path.startsWith('/api/models')) {
      if (path.startsWith('/api/models/dirs')) {
        return jsonRes({
          download_dir: "/home/demo/.local/share/loom/models",
          dirs: ["/home/demo/.local/share/loom/models"]
        });
      }
      if (path.startsWith('/api/models/download/probe')) {
        return jsonRes({ ok: true, enough: true, free_bytes: 512000000000 });
      }
      if (path.startsWith('/api/models/download/cancel')) {
        let b = {}; try { b = JSON.parse(init.body); } catch {}
        if (b.filename) delete activeDownloads[b.filename];
        return jsonRes({ ok: true });
      }
      if (path.startsWith('/api/models/download/status')) {
        const statuses = Object.values(activeDownloads);
        // Simulate progressive download ticks
        statuses.forEach(st => {
          if (!st.finished) {
            st.done = Math.min(st.total, st.done + Math.max(Math.floor(st.total / 5), 2000000000));
            if (st.done >= st.total) {
              st.finished = true;
              // Add to local models library
              if (!localModels.some(m => m.name.toLowerCase() === st.filename.toLowerCase())) {
                const newM = {
                  name: st.filename,
                  value: "/home/demo/.local/share/loom/models/" + st.filename,
                  path: "/home/demo/.local/share/loom/models/" + st.filename,
                  dir: "/home/demo/.local/share/loom/models",
                  size: st.total,
                  quant: st.quant || "Q4_K_M",
                  context_len: 32768,
                  modified: "2026-09-22 13:50"
                };
                localModels.push(newM);
                setStore('models', localModels);
              }
            }
          }
        });
        return jsonRes(statuses);
      }
      if (path.startsWith('/api/models/download') && method === 'POST') {
        let b = {}; try { b = JSON.parse(init.body); } catch {}
        const urlStr = b.url || '';
        const fname = urlStr.split('/').pop() || 'nouveau-modele.gguf';
        activeDownloads[fname] = {
          filename: fname,
          quant: "Q4_K_M",
          done: 0,
          total: 9437184000,
          speed: 118000000,
          finished: false
        };
        return jsonRes({ ok: true, filename: fname });
      }
      return jsonRes(localModels);
    }

    // --- /api/presets ---
    if (path.startsWith('/api/presets')) {
      return jsonRes(localPresets);
    }

    if (path.startsWith('/api/preset')) {
      const u = new URL('http://127.0.0.1' + path);
      const id = u.searchParams.get('id') || 'general';
      const p = localPresets.find(x => x.id === id) || localPresets[0];
      return jsonRes(p);
    }

    // --- /api/prefs ---
    if (path.startsWith('/api/prefs')) {
      if (method === 'POST') return jsonRes({ ok: true });
      return jsonRes({
        ok: true,
        prefs: {
          theme: 'dark',
          hide_reasoning: '0',
          hide_tools: '0',
          fold_tools: '0',
          enter_newline: '0'
        }
      });
    }

    // --- /api/naked/defaults ---
    if (path.startsWith('/api/naked/defaults')) {
      const u = new URL('http://127.0.0.1' + path);
      const mPath = u.searchParams.get('model') || activeModelPath;
      return jsonRes({
        ok: true,
        native_ctx: 32768,
        content: `MODEL=${mPath}\nCTX=32768\nNGL=33\nTEMP=0.7\nTOP_P=0.9\nMIN_P=0.05\nBATCH=2048\nUBATCH=512\nTHREADS=8\nFLASH_ATTN=1`
      });
    }

    // --- /api/naked/remember ---
    if (path.startsWith('/api/naked/remember')) {
      return jsonRes({ ok: true, remembered: false });
    }

    // --- /api/estimate (Live VRAM calculation when moving sliders) ---
    if (path.startsWith('/api/estimate')) {
      let b = {}; try { b = JSON.parse(init.body); } catch {}
      const m = b.model || activeModelPath;
      const vramTotal = 24576;
      let gpuMb = 5840;
      if (m.toLowerCase().includes('70b')) gpuMb = 23200;
      else if (m.toLowerCase().includes('32b')) gpuMb = 20400;
      else if (m.toLowerCase().includes('24b')) gpuMb = 15800;
      else if (m.toLowerCase().includes('14b')) gpuMb = 9800;
      return jsonRes({
        ok: true,
        gpu_mb: gpuMb,
        total_mb: gpuMb,
        vram_total_mb: vramTotal,
        ram_offload_mb: 0,
        verdict: "fits",
        ctx: 32768,
        kv_mb: 1024,
        max_ctx_fit: 65536
      });
    }

    // --- /api/load-model ---
    if (path.startsWith('/api/load-model') && method === 'POST') {
      let b = {}; try { b = JSON.parse(init.body); } catch {}
      if (b.model) {
        activeModelPath = b.model;
        activePresetId = '';
        setStore('active_model', activeModelPath);
        setStore('active_preset', '');
        localPresets.forEach(p => p.active = false);
        setStore('presets', localPresets);
      }
      return jsonRes({ ok: true, model: baseName(activeModelPath) });
    }

    // --- /api/unload ---
    if (path.startsWith('/api/unload') && method === 'POST') {
      activeModelPath = '';
      activePresetId = '';
      setStore('active_model', '');
      setStore('active_preset', '');
      localPresets.forEach(p => p.active = false);
      setStore('presets', localPresets);
      return jsonRes({ ok: true });
    }

    // --- /api/apply ---
    if (path.startsWith('/api/apply') && method === 'POST') {
      let b = {}; try { b = JSON.parse(init.body); } catch {}
      if (b.content) {
        const match = b.content.match(/^[ \t]*MODEL[ \t]*=[ \t]*(.+)$/m);
        if (match && match[1]) {
          activeModelPath = match[1].trim().replace(/^["']|["']$/g, '');
          setStore('active_model', activeModelPath);
        }
      }
      if (b.preset_id) {
        activePresetId = b.preset_id;
        setStore('active_preset', activePresetId);
        localPresets.forEach(p => p.active = (p.id === activePresetId));
        setStore('presets', localPresets);
      } else {
        activePresetId = '';
        setStore('active_preset', '');
        localPresets.forEach(p => p.active = false);
        setStore('presets', localPresets);
      }
      return jsonRes({ ok: true });
    }

    // --- /api/switch ---
    if (path.startsWith('/api/switch') && method === 'POST') {
      let b = {}; try { b = JSON.parse(init.body); } catch {}
      let target = null;
      if (b.n && localPresets[b.n - 1]) {
        target = localPresets[b.n - 1];
      } else if (b.id) {
        target = localPresets.find(p => p.id === b.id);
      } else if (b.preset) {
        target = localPresets.find(p => p.id === b.preset || p.name === b.preset);
      } else if (b.name) {
        target = localPresets.find(p => p.name === b.name);
      }
      if (target) {
        activePresetId = target.id;
        activeModelPath = target.model;
        localPresets.forEach(p => p.active = (p.id === target.id));
        setStore('active_model', activeModelPath);
        setStore('active_preset', activePresetId);
        setStore('presets', localPresets);
      }
      return jsonRes({ ok: true, preset: target?.name });
    }

    // --- /api/hub/search ---
    if (path.startsWith('/api/hub/search')) {
      const u = new URL('http://127.0.0.1' + path);
      const q = (u.searchParams.get('q') || '').toLowerCase().trim();
      const pipe = u.searchParams.get('pipeline') || '';

      let results = hubCatalog.filter(m => {
        if (pipe && m.pipeline !== pipe) return false;
        if (!q) return true;
        return m.id.toLowerCase().includes(q) || m.author.toLowerCase().includes(q);
      });

      return jsonRes({
        ok: true,
        models: results.map(m => ({
          id: m.id,
          author: m.author,
          params_b: m.params_b,
          pipeline: m.pipeline,
          downloads: m.downloads,
          likes: m.likes,
          size: m.size,
          quant_recommend: m.quant_recommend,
          quant_count: m.files.length
        }))
      });
    }

    // --- /api/hub/model ---
    if (path.startsWith('/api/hub/model')) {
      const u = new URL('http://127.0.0.1' + path);
      const id = u.searchParams.get('id') || '';
      const m = hubCatalog.find(x => x.id === id) || hubCatalog[0];
      return jsonRes({
        ok: true,
        id: m.id,
        author: m.author,
        params_b: m.params_b,
        downloads: m.downloads,
        likes: m.likes,
        license: m.license,
        vram_total_mb: 24576,
        ram_total_mb: 32768,
        readme: m.readme,
        files: m.files
      });
    }

    // --- /api/chat/history ---
    if (path.startsWith('/api/chat/history')) {
      return jsonRes({
        conversations: [
          {
            id: "demo-conv-1",
            title: conversationTurns[0]?.user || "Différence entre quantifications GGUF",
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
        ctx_used: 240,
        compact_count: 0
      });
    }

    // --- /api/chat/send ---
    if (path.startsWith('/api/chat/send') && method === 'POST') {
      let body = {};
      try { body = JSON.parse(init.body); } catch {}
      const userMsg = body.message || "Bonjour !";

      const activeObj = getActiveModelObj();
      const modelLabel = activeObj ? activeObj.name : "llama.cpp local";

      let reasonText = `Analyse de la requête locale sous Loom.\nModèle sélectionné : ${modelLabel}.\n1. Vérifier la pertinence technique.\n2. Rédiger une explication claire et actionnable pour l'utilisateur.`;
      let replyText = `Voici une réponse générée localement par **${modelLabel}** via Loom.\n\nVous avez demandé : *« ${userMsg} »*\n\n- **Backend** : llama.cpp (AVX2 + CUDA 12)\n- **Format des tenseurs** : GGUF avec KV cache dynamique\n- **Vitesse moyenne** : ~51.2 tokens/seconde`;

      if (userMsg.toLowerCase().includes("vram") || userMsg.toLowerCase().includes("mémoire") || userMsg.toLowerCase().includes("kv")) {
        reasonText = "Calcul de l'allocation mémoire GPU / RAM selon le modèle actif et le contexte demandé.\n1. Évaluer la taille du modèle en VRAM.\n2. Calculer le budget du cache KV selon la précision (q8_0 vs q4_0).\n3. Proposer les optimisations clés.";
        replyText = `### Optimisation du KV Cache & VRAM dans Loom\n\nPour économiser la VRAM sur votre GPU sans réduire drastiquement la longueur de contexte :\n\n1. **Quantification du cache KV (`--cache-type-k q8_0 --cache-type-v q8_0`)** :\n   Divise par 2 la taille du cache d'attention sans perte mesurable de qualité.\n2. **Flash Attention activé (`-fa 1`)** :\n   Réduit l'empreinte mémoire d'activation quadratique à une valeur linéaire.\n3. **Gestion dynamique des slots** :\n   Chaque slot libère son contexte dès la fin de la génération.\n\nSur **${modelLabel}**, ces optimisations permettent d'économiser **jusqu'à 2.8 Go de VRAM** à 32k tokens.`;
      } else if (userMsg.toLowerCase().includes("code") || userMsg.toLowerCase().includes("python") || userMsg.toLowerCase().includes("api")) {
        reasonText = "L'utilisateur demande un exemple d'intégration avec l'API compatible OpenAI de Loom.";
        replyText = `Voici comment interroger votre instance locale Loom en Python via la bibliothèque standard OpenAI :\n\n\`\`\`python\nfrom openai import OpenAI\n\n# Loom expose un endpoint compatible OpenAI sur le port 8081\nclient = OpenAI(\n    base_url="http://127.0.0.1:8081/v1",\n    api_key="loom-local" # Clé arbitraire en local\n)\n\nresponse = client.chat.completions.create(\n    model="${modelLabel}",\n    messages=[\n        {"role": "system", "content": "Vous êtes un assistant IA concis et rigoureux."},\n        {"role": "user", "content": "Quelle est la vitesse de ce modèle ?"}\n    ],\n    stream=True\n)\n\nfor chunk in response:\n    content = chunk.choices[0].delta.content or ""\n    print(content, end="", flush=True)\n\`\`\``;
      }

      const newTurn = {
        user: userMsg,
        reasoning: reasonText,
        assistant: replyText,
        stats: { gen_tokens: 136, speed: 51.2, prompt_tokens_total: 24 },
        elapsed_ms: 1950
      };

      conversationTurns.push(newTurn);
      setStore('chat_turns', conversationTurns);
      pendingGeneration = newTurn;

      // Broadcast live to open SSE stream
      (async () => {
        if (!activeStreamController) return;
        const encoder = new TextEncoder();
        const sendChunk = (delta) => {
          try {
            activeStreamController.enqueue(encoder.encode(`data: ${JSON.stringify({ choices: [{ delta }] })}\n\n`));
          } catch {}
        };

        sendChunk({ user: userMsg });
        await new Promise(r => setTimeout(r, 60));

        if (reasonText) {
          sendChunk({ reasoning_content: reasonText });
          await new Promise(r => setTimeout(r, 80));
        }

        // Stream content in small chunks
        const words = replyText.split(' ');
        for (let i = 0; i < words.length; i += 3) {
          const slice = words.slice(i, i + 3).join(' ') + (i + 3 < words.length ? ' ' : '');
          sendChunk({ content: slice });
          await new Promise(r => setTimeout(r, 35));
        }

        sendChunk({
          stats: newTurn.stats,
          elapsed_ms: newTurn.elapsed_ms,
          turn_done: true
        });

        pendingGeneration = null;
      })();

      return jsonRes({ ok: true });
    }

    // --- /api/chat (SSE Stream) ---
    if (path.startsWith('/api/chat') && method === 'POST') {
      const encoder = new TextEncoder();

      const stream = new ReadableStream({
        async start(controller) {
          // Replay history
          for (let idx = 0; idx < conversationTurns.length; idx++) {
            const turn = conversationTurns[idx];

            controller.enqueue(encoder.encode(`data: ${JSON.stringify({ choices: [{ delta: { user: turn.user } }] })}\n\n`));
            await new Promise(r => setTimeout(r, 15));

            if (turn.reasoning) {
              controller.enqueue(encoder.encode(`data: ${JSON.stringify({ choices: [{ delta: { reasoning_content: turn.reasoning } }] })}\n\n`));
              await new Promise(r => setTimeout(r, 15));
            }

            controller.enqueue(encoder.encode(`data: ${JSON.stringify({ choices: [{ delta: { content: turn.assistant } }] })}\n\n`));
            await new Promise(r => setTimeout(r, 15));

            controller.enqueue(encoder.encode(`data: ${JSON.stringify({
              choices: [{
                delta: {
                  stats: turn.stats,
                  elapsed_ms: turn.elapsed_ms,
                  turn_done: true
                }
              }]
            })}\n\n`));
            await new Promise(r => setTimeout(r, 15));
          }

          // Caught up
          controller.enqueue(encoder.encode(`data: ${JSON.stringify({ choices: [{ delta: { caught_up: true } }] })}\n\n`));
          controller.close();
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
      const activeObj = getActiveModelObj();
      const vramMb = computeVramMb();
      return jsonRes({
        running: !!activeObj,
        slots: [
          { id: 0, state: pendingGeneration ? "processing" : "idle", tokens_second: pendingGeneration ? 51.2 : 0, n_decoded: 184, model: activeObj ? activeObj.name : "Pas de modèle", task: "Chat interactif" },
          { id: 1, state: "idle", tokens_second: 0, n_decoded: 0, model: activeObj ? activeObj.name : "Pas de modèle", task: "Standby (/v1)" },
          { id: 2, state: "idle", tokens_second: 0, n_decoded: 0, model: activeObj ? activeObj.name : "Pas de modèle", task: "Standby" },
          { id: 3, state: "idle", tokens_second: 0, n_decoded: 0, model: activeObj ? activeObj.name : "Pas de modèle", task: "Standby" }
        ],
        vram_used: vramMb,
        vram_total: 24576,
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
            decode_toks: 34,
            n_ctx: 4096,
            timestamp: 1726880000
          },
          {
            model: "Mistral Small 24B Instruct (Q4_K_M)",
            prefill_toks: 310,
            decode_toks: 26,
            n_ctx: 4096,
            timestamp: 1726880000
          }
        ]
      });
    }

    // Default fallback
    return jsonRes({ ok: true, simulated: true });
  };
})();
