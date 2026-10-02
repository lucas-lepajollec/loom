package loom

import "net/http"

// Shared engine control handlers. No UI, discussion, Brain or harness startup.
func registerEngineControlRoutes(api func(string, http.HandlerFunc)) {
	api("/api/status", handleStatus)
	api("/api/service/log", handleServiceLog) // journal du service pour diagnostiquer un modèle qui ne charge pas
	api("/api/vram", handleVram)
	api("/api/ram", handleRam)
	api("/api/config", handleConfigEnv)
	api("/api/reasoning", handleReasoning) // change l'effort de réflexion à chaud (raccourci composeur)
	api("/api/catalog", handleCatalog)
	api("/api/paths", handlePaths)
	api("/api/models", handleModels)
	api("/api/models/delete", handleModelDelete)
	api("/api/models/dirs", handleModelDirs) // dossiers de modèles (disque externe…)
	api("/api/models/download", handleModelDownload)
	api("/api/models/download/probe", handleModelDownloadProbe) // taille + espace libre avant de lancer
	api("/api/models/download/status", handleModelDownloadStatus)
	api("/api/models/download/cancel", handleModelDownloadCancel)
	api("/api/hub/search", handleHubSearch) // GGUF Hugging Face (recherche)
	api("/api/hub/model", handleHubModel)   // fiche dépôt + fichiers + VRAM
	api("/api/hub/avatar", handleHubAvatar) // logo org/user Hugging Face
	api("/api/backends", handleBackends)
	api("/api/backends/custom", handleBackendsCustom)                    // backends custom uniquement (hors ⚡/🔧)
	api("/api/backends/devices", handleBackendDevices)                   // GPU vus par CE moteur (noms/ordre propres au backend)
	api("/api/llamacpp", handleLlamacpp)                                 // statut du backend llama.cpp
	api("/api/llamacpp/check", handleLlamacppCheck)                      // git fetch + retard sur origin
	api("/api/llamacpp/install", handleLlamacppInstall)                  // job : clone + build + BIN
	api("/api/llamacpp/install-custom", handleLlamacppInstallCustom)     // job : clone d'un fork depuis une URL Git (par preset, sans BIN global)
	api("/api/llamacpp/uninstall-custom", handleLlamacppUninstallCustom) // supprime un backend custom (backends/<name>)
	api("/api/llamacpp/update", handleLlamacppUpdate)                    // job : pull + rebuild + restart
	api("/api/llamacpp/job", handleLlamacppJob)                          // progression + logs du job
	api("/api/llamacpp/job/dismiss", handleLlamacppJobDismiss)           // masque un job terminé (l'erreur ne revient plus au démarrage)
	api("/api/llamacpp/prebuilt", handleLlamacppPrebuilt)                // job : binaires officiels précompilés
	api("/api/llamacpp/prebuilt/check", handleLlamacppPrebuiltCheck)     // dernière release officielle vs installée
	api("/api/llamacpp/use", handleLlamacppUse)                          // bascule BIN entre versions déjà installées
	api("/api/presets", handlePresets)
	api("/api/presets/order", handlePresetsOrder)
	api("/api/preset", handlePreset)
	api("/api/preset/save", handlePresetSave)
	api("/api/preset/delete", handlePresetDelete)
	api("/api/engine/auto-update", handleEngineAuto)
	api("/api/engines/vllm", handleVLLM)
	api("/api/engines/vllm/params", handleVLLMParams)
	api("/api/engines/vllm/auto-update", handleVLLMAuto)
	api("/api/engines/vllm/models", handleVLLMModels)
	api("/api/engines/vllm/models/delete", handleVLLMDelete)
	api("/api/engines/vllm/hub/search", handleVLLMSearch)
	api("/api/engines/vllm/download", handleVLLMDownload)
	api("/api/engines/vllm/download/cancel", handleVLLMDownloadCancel)
	api("/api/server", handleServer) // Serveur API : slots llama-server, NP, requêtes
	api("/api/switch", handleSwitch)
	api("/api/load-model", handleLoadModel)         // charge un .gguf sans preset
	api("/api/unload", handleUnload)                // décharge modèle/preset et arrête le moteur Loom
	api("/api/apply", handleApplyLive)              // applique la config du panneau (modèle nu ou preset)
	api("/api/naked/remember", handleNakedRemember) // souvenir par .gguf pour le prochain chargement nu
	api("/api/naked/defaults", handleNakedDefaults) // défauts GGUF d'un modèle nu
	api("/api/llama-flags", handleLlamaFlags)       // catalogue llama-server --help
	api("/api/engine/params", handleEngineParams)   // curated controls merged with installed help
	api("/api/model-caps", handleModelCaps)         // vision / raisonnement natifs d'un GGUF
	api("/api/estimate", handleEstimate)            // estimation VRAM (panneau / presets / alerte)
	api("/api/start", svcHandler("start"))
	api("/api/stop", svcHandler("stop"))
	api("/api/restart", svcHandler("restart"))
}
