// Loom — cœur du binaire (package loom, appelé par cmd/loom). Go n'autorise
// pas de sous-dossiers dans un même package : les fichiers sont donc préfixés
// par domaine.
//
//	run.go        point d'entrée : dispatch des sous-commandes, LoomHome() et
//	              l'arborescence de données
//	store/        base bbolt (loom.db) : configuration, préférences, conversation,
//	              clés, jetons, interrupteurs — tout l'état non éditable à la main
//	cli_*         expérience application (double-clic : UI + tray + splash)
//	web_*         serveur HTTP :8091 (UI embarquée via go:embed ui/, auth, prefs)
//	chat_*        chat CLI + conversation partagée, compaction, outils
//	llm_*         client llama-server (complétions, endpoint OpenAI /v1, bench)
//	backend_*     gestion llama.cpp : build, GPU, modèles et téléchargements,
//	              catalogue, presets, configuration
//	sys_*         intégration OS : install, services, plateforme, process, tray,
//	              splash, tty, auto-update
//
// Les suffixes _windows/_linux/_darwin/_unix/_other portent les contraintes de
// compilation par OS.
//
// Deux services à l'exécution, un seul binaire :
//   - loom-engine (« loom serve ») exec llama-server ;
//   - loom-ui (« loom web ») sert l'UI locale et l'endpoint OpenAI /v1.
//
// L'interface est ui/next/ (modules ES natifs, sans étape de build),
// embarquée telle quelle dans le binaire.
package loom
