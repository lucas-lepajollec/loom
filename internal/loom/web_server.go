package loom

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// cmdWeb starts the HTTP server on the given port (default 8091).
// 8090 belongs to a live Loom. Loom listens on loopback unless WEB_HOST opens
// it to the network, which requires a control key (web_network.go).
func cmdWeb(args []string) error {
	if err := provisionDataDir(); err != nil {
		fmt.Printf("%s Loom data: %v\n", yellow("[!]"), err)
	}
	port := 8091
	if len(args) > 0 && args[0] != "" {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid port: %s", args[0])
		}
		port = n
	}
	host := webHost()
	if err := webListenCheck(host); err != nil {
		return err
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("could not start Loom on %s: %w (stop the service using this port or choose another port with `loom web <port>`)", addr, err)
	}
	defer ln.Close()
	webBound.host, webBound.port = host, port
	// Only once the port is ours: these write to Loom's data.
	registerCustomACPAgents()
	go syncModelSinks()
	loadRememberedProviderKeys()
	go probeMissingACPAgents()
	go engineAutoLoop()
	go vllmAutoLoop()
	lifecycleCtx, stopLifecycle := context.WithCancel(context.Background())
	defer stopLifecycle()
	go harnessLifecycle.autoLoop(lifecycleCtx)
	mux := newWebMux(lifecycleCtx)
	fmt.Printf("[loom web] http://%s  (Ctrl-C to stop)\n", addr)
	if !webKeyConfigured() {
		fmt.Printf("%s control API is NOT protected (no key). Before exposing it to the internet:\n", yellow("[!]"))
		fmt.Printf("       %s\n", bold("loom set-web-key"))
	} else {
		fmt.Printf("%s API protected by a key (Authorization: Bearer …)\n", green("[ok]"))
	}

	// ReadHeaderTimeout : sans lui, une connexion qui n'envoie jamais sa requête
	// immobilise une goroutine pour toujours, même sur l'interface locale.
	// Surtout PAS de WriteTimeout ici : il couperait les flux SSE du chat, qui
	// restent ouverts aussi longtemps que l'utilisateur regarde la page.
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return srv.Serve(ln)
}

// newWebMux construit le routeur HTTP de l'UI web. Extrait de cmdWeb pour être
// réutilisé par `loom link`, qui sert ce même mux à travers le tunnel sans
// repasser par un écouteur TCP local.
var convLoadOnce sync.Once

func newWebMux(lifecycle ...context.Context) *http.ServeMux {
	// Charge l'état de conversation persisté (une fois par process : loom web ET
	// loom link serve appellent newWebMux).
	convLoadOnce.Do(LoadConversation)
	annotateLoadedNativeConversation(conv)
	// Pré-chauffe les serveurs MCP en tâche de fond : sinon le handshake (plusieurs
	// secondes pour un serveur lancé via npx) est payé par le premier message.
	MCPPrewarm()
	// Un téléchargement de modèle coupé net (crash, restart du service) laisse un
	// .part orphelin non reprenable : on nettoie au démarrage.
	cleanStalePartFiles()
	// Idem pour un envoi de fichier coupé en plein transfert : les sessions vivent
	// en mémoire, aucun .part ne survit utilement à l'arrêt du process.
	cleanStaleUploadParts()
	// Idem pour une installation de moteur : elle meurt avec le process. On
	// recharge son état pour l'annoncer « interrompue » au lieu de n'afficher
	// plus rien du tout.
	lcRestoreOnce.Do(lcRestore)
	// Reprend une migration de chiffrement mémoire interrompue (crash/coupure).
	// Sans DEK en RAM (démarrage à froid), laisse le journal en place : un
	// déverrouillage ultérieur la reprendra. Ne perd jamais de données.
	resumeMemMigration()
	// La clé de pilotage n'est plus stockée en clair (juste son empreinte) : on
	// convertit une ancienne valeur en clair une fois pour toutes.
	migrateWebKeyToHash()
	mux := http.NewServeMux()
	var brainCtx context.Context
	if len(lifecycle) > 0 {
		brainCtx = lifecycle[0]
	}
	registerBrainRoutes(mux, brainCtx)
	registerWebAssets(mux)
	api := webAPI(mux)
	newEnvironment().register(api)
	api("/api/ping", handlePing)
	api("/api/workspace", handleWorkspace)
	api("/api/usage", handleUsage)
	api("/api/usage/native", handleNativeUsage)
	api("/api/usage/native/refresh", handleNativeUsageRefresh)
	api("/api/usage/providers", handleProviderBalances)
	api("/api/usage/providers/refresh", handleProviderBalanceRefresh)
	api("/api/usage/refresh", handleUsageRefresh)
	api("/api/usage/price", handleUsagePrice)
	api("/api/workspace/antigravity/connect", handleAgyConnect)
	api("/api/workspace/codex/connect", handleCodexConnect)
	api("/api/runtimes", handleACPRuntimes)
	api("/api/runtime/sessions/approval", handleACPApproval)
	api("/api/runtime/sessions/files", handleACPFiles)
	api("/api/runtime/sessions/diff", handleACPDiff)
	api("/api/fs/dirs", handleACPDirs)
	api("/api/runtimes/{id}/connect", handleRuntimeConnect)
	api("/api/runtimes/{id}/probe", handleACPProbe)
	api("/api/runtimes/{id}/inspect", handleHarnessInspect) // ce que le harness possède déjà (MCP, skills, compte…)
	api("/api/runtimes/{id}/update", handleHarnessUpdate)
	api("/api/runtimes/{id}/mcp/adopt", handleHarnessMCPAdopt) // copier un MCP du harness dans Loom
	api("/api/harness/bindings", handleHarnessBindings)
	api("/api/harness/lifecycle", handleHarnessLifecycle)
	api("/api/harness/lifecycle/auto", handleHarnessLifecycleAuto)
	api("/api/harness/model-source", handleModelSink)          // modèles Loom proposés dans un harness ouvert (Pi)        // MCP Loom transmis à chaque harness
	api("/api/skills/binding", handleSkillBinding)             // une skill vers un dossier de skills      // mise à jour du CLI du harness
	api("/api/runtimes/{id}/sessions", handleACPSessions)      // sessions natives d’un harness ACP
	api("/api/runtimes/{id}/sessions/import", handleACPImport) // importer une session native dans Loom // modèles et réglages annoncés par un harness ACP
	api("/api/runtimes/{id}/quota", handleRuntimeQuota)
	api("/api/providers", handleProviders)
	api("/api/providers/save", handleProviderSave)
	api("/api/providers/models", handleProviderModels)
	api("/api/providers/disconnect", handleProviderDisconnect)
	api("/api/discussion/events", handleDiscussionEvents)
	api("/api/runtime/sessions", handleRuntimeSessions)
	api("/api/runtime/sessions/terminal", handleRuntimeSessionTerminal)
	api("/api/runtime/sessions/select", handleRuntimeSessionSelect)
	api("/api/runtime/sessions/import", handleRuntimeSessionImport)
	api("/api/runtime/sessions/local", handleRuntimeSessionLocal)
	api("/api/runtime/sessions/configure", handleRuntimeSessionConfigure)
	api("/api/runtime/sessions/preview", handleRuntimeSessionPreview)
	api("/api/workspace/models/visibility", handleModelChoice)
	api("/api/workspace/harnesses/save", handleHarnessProfileSave)
	api("/api/runtime/sessions/create", handleRuntimeSessionCreate)
	api("/api/runtime/sessions/send", handleRuntimeSessionSend)
	api("/api/runtime/sessions/stop", handleRuntimeSessionStop)
	api("/api/runtime/sessions/delete", handleRuntimeSessionDelete)
	api("/api/projects/context", handleProjectContext)
	api("/api/projects/info", handleProjectInfo)
	api("/api/capabilities/save", handleCapabilitySave)
	api("/api/capabilities/delete", handleCapabilityDelete)
	api("/api/skills/targets", handleSkillSinks)
	api("/api/skills/sources", handleSkillSources) // distribution des skills aux harnesses
	api("/api/harness/custom", handleCustomACP)
	api("/api/harness/custom/delete", handleCustomACPDelete)
	api("/api/machines", handleRemoteMachines)
	api("/api/machines/delete", handleRemoteMachineDelete)
	api("/api/machines/folders", handleMachineFolders)
	api("/api/machines/local", handleLocalMachine)
	api("/api/status", handleStatus)
	api("/api/service/log", handleServiceLog) // journal du service pour diagnostiquer un modèle qui ne charge pas
	api("/api/vram", handleVram)
	api("/api/ram", handleRam)
	api("/api/config", handleConfigEnv)
	api("/api/reasoning", handleReasoning) // change l'effort de réflexion à chaud (raccourci composeur)
	api("/api/catalog", handleCatalog)
	api("/api/paths", handlePaths)
	api("/api/update", handleUpdateCheck)
	api("/api/update/apply", handleUpdateApply)
	api("/api/models", handleModels)
	api("/api/models/delete", resyncModelSinks(handleModelDelete))
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
	api("/api/preset/save", resyncModelSinks(handlePresetSave))
	api("/api/preset/delete", resyncModelSinks(handlePresetDelete))
	api("/api/agent", handleAgent)
	api("/api/agent/toggle", handleAgentToggle)
	api("/api/agent/compact", handleCompactToggle)
	api("/api/apikey", handleAPIKey)
	api("/api/internet", handleInternet)
	api("/api/mcp/file", handleMCPFile)
	api("/api/mcp/sources", handleMCPSources)
	api("/api/mcp/sources/adopt", handleMCPSourceAdopt)
	api("/api/mcp", handleMCP)
	api("/api/mcp/save", handleMCPSave)
	api("/api/mcp/delete", handleMCPDelete)
	api("/api/mcp/toggle", handleMCPToggle)
	api("/api/mcp/tool", handleMCPTool)
	api("/api/mcp/test", handleMCPTest)
	api("/api/memory", handleMemoryMode)
	api("/api/network/web", handleWebNetwork)
	api("/api/engine/node", handleEngineNode)
	api("/api/engine/auto-update", handleEngineAuto)
	api("/api/engines/vllm", handleVLLM)
	api("/api/engines/vllm/params", handleVLLMParams)
	api("/api/engines/vllm/auto-update", handleVLLMAuto)
	api("/api/engines/vllm/models", handleVLLMModels)
	api("/api/engines/vllm/models/delete", handleVLLMDelete)
	api("/api/engines/vllm/hub/search", handleVLLMSearch)
	api("/api/engines/vllm/download", handleVLLMDownload)
	api("/api/engines/vllm/download/cancel", handleVLLMDownloadCancel)
	api("/api/terminals", handleTerminals)
	api("/api/terminals/close", handleTerminalClose)
	api("/api/terminals/ticket", handleTerminalTicket)
	// One-time ticket from /api/terminals/ticket instead of the key header.
	mux.HandleFunc("/api/terminals/ws", handleTerminalWS)
	api("/api/node/info", handleNodeInfo)
	api("/api/network", handleNetwork) // écoute LAN du moteur + pare-feu (Windows)
	api("/api/server", handleServer)   // Serveur API : slots llama-server, NP, requêtes
	api("/api/prefs", handleWebPrefs)
	api("/api/sysprompt", handleSysPrompt)
	api("/api/sysprompt/model", handleModelSysPrompt) // prompt du modèle/preset (live, sans restart)
	// Alias rétro-compat : l'ancien portail loom.local (dépôt loom-relay) pilote
	// encore l'agent via /api/tools* et /api/skills/toggle à travers le tunnel E2E.
	// On les mappe sur le mode agent unifié le temps que le portail soit mis à jour.
	api("/api/tools", handleAgent)
	api("/api/tools/toggle", handleAgentToggle)
	api("/api/skills", handleAgent)
	api("/api/skills/toggle", handleAgentToggle)
	api("/api/mem", handleMem)
	api("/api/mem/save", handleMemSave)
	api("/api/mem/delete", handleMemDelete)
	api("/api/mem/health", handleMemHealth)       // état chiffrement/verrou/pages/snapshots
	api("/api/mem/encrypt", handleMemEncrypt)     // active le chiffrement (renvoie la clé de récupération)
	api("/api/mem/decrypt", handleMemDecrypt)     // remet la mémoire en clair
	api("/api/mem/unlock", handleMemUnlock)       // déverrouille (mot de passe ou clé de récupération)
	api("/api/mem/addkey", handleMemAddKey)       // ajoute un wrap (ex. clé d'API) au coffre déjà ouvert
	api("/api/mem/lock", handleMemLock)           // reverrouille (purge la DEK de la RAM)
	api("/api/mem/snapshots", handleMemSnapshots) // liste + restauration des snapshots locaux
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
	api("/api/bench", handleBench)
	api("/api/bench/last", handleBenchLast)
	api("/api/bench/tests", handleBenchTests)
	api("/api/bench/tests/delete", handleBenchTestsDelete)
	api("/api/bench/queue", handleBenchQueue)
	api("/api/bench/queue/cancel", handleBenchQueueCancel)
	api("/api/bench/runs", handleBenchRuns)
	api("/api/chat", handleChat)                               // flux d'ABONNEMENT (SSE) : rejoue + suit le fil
	api("/api/chat/send", handleChatSend)                      // envoie un message (lance la génération détachée)
	api("/api/chat/upload", handleChatUpload)                  // dépose un fichier dans le workspace agent (joint au message suivant)
	api("/api/chat/file", handleChatFile)                      // télécharge un fichier produit par l'agent (dossier de travail only)
	api("/api/chat/stop", handleChatStop)                      // interrompt la génération en cours
	api("/api/chat/reset", handleChatReset)                    // nouvelle conversation (archive la courante dans l'historique)
	api("/api/chat/history", handleChatHistory)                // liste des conversations archivées
	api("/api/chat/history/restore", handleChatHistoryRestore) // recharge une conversation archivée
	api("/api/chat/history/delete", handleChatHistoryDelete)   // supprime définitivement une archive
	api("/api/chat/history/rename", handleChatHistoryRename)   // renomme une conversation archivée
	api("/api/chat/history/fav", handleChatHistoryFav)         // épingle/dépingle en favori
	api("/api/chat/history/clear", handleChatHistoryClear)     // supprime tout sauf les favoris
	api("/api/chat/history/move", handleChatHistoryMove)       // déplace une conversation vers un projet
	api("/api/projects", handleProjects)                       // GET liste / POST crée
	api("/api/projects/rename", handleProjectsRename)
	api("/api/projects/delete", handleProjectsDelete)
	api("/api/chat/compact", handleChatCompact) // compaction manuelle du contexte
	api("/api/chat/state", handleChatState)     // instantané léger {seq, generating, ctx_used}
	api("/api/chat/export", handleChatExport)   // téléchargement du fil (?format=md|json)
	// Notifications Web Push : le serveur pousse une notif à la fin d'un tour
	// utilisateur (push.go), même app fermée / iPhone verrouillé.
	api("/api/push/key", handlePushKey)                 // clé publique VAPID (pour s'abonner)
	api("/api/push/subscribe", handlePushSubscribe)     // enregistre un abonnement
	api("/api/push/unsubscribe", handlePushUnsubscribe) // retire un abonnement
	return mux
}

// resyncModelSinks refreshes the "loom" provider of harnesses (Pi) after the
// local library changes, so they list the same models as Loom.
func resyncModelSinks(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h(w, r)
		go syncModelSinks()
	}
}
