// Package platform fournit les primitives OS autonomes : chemins par défaut,
// console, navigateur, disque, RAM, processus détachés, pare-feu, icônes/splash,
// service d'UI et helpers de téléchargement/remplacement des mises à jour.
// Il ne dépend pas du package loom. Les valeurs détenues par Loom (dossier bin,
// coupe-circuit du pare-feu) sont fournies explicitement par l'appelant.
//
// Les noms appelés par le reste de loom restent dans platform_compat.go. Les
// variables historiques du rendu, des DLL et du compteur de renommage sont
// déplacées, sans nouvel état global ni copie de cet état dans la compatibilité.
// Les variantes OS conservent leurs tags et leurs différences de comportement.
//
// Restent dans loom pendant cette tranche :
//   - sys_datadir.go : chemins applicatifs, ReadConfig et WriteConfig.
//   - sys_paths.go : currentPaths, agentWorkspace et présentation CLI/HTTP.
//   - sys_install_{unix,linux,darwin,windows}.go : migration, provisionnement,
//     serviceName, chemins et sortie CLI ; helpers privés de ces installateurs.
//   - sys_firstrun_{windows,other}.go et ses tests Windows : installation,
//     Version, provisionnement et appPort ; helpers privés du premier lancement.
//   - sys_platform_{unix,windows}.go : bibliothèques CUDA, findNvcc, isDir,
//     hasTool, hasNvidiaGPU et sortie CLI ; helpers privés du build MSVC.
//   - sys_service.go : prévol moteur, résolution des modèles, édition de config
//     et présentation VRAM. Son test autonome de propriété du cmdline est déplacé.
//   - sys_service_{linux,darwin,windows}.go : supervision du moteur, chemins,
//     serviceName, ensureLoomEngineBind et sortie CLI ; helpers privés du service.
//   - sys_network.go et ses tests : configuration moteur, clé API, LLMPort,
//     localIP, état firewallInert et présentation CLI/HTTP.
//   - sys_tray_{windows,darwin,other}.go : workspaceSessions.shutdownACP et,
//     sous Windows, svcStop ; la variante sans tray reste avec ses homologues.
//   - sys_restart_{windows,other}.go : sessions ACP et relance de l'application ;
//     la variante sans relance reste avec son homologue Windows.
//   - sys_update.go : Version, orchestration de la mise à jour et du redémarrage,
//     serviceName et handlers HTTP. Les trois tests autonomes suivent les helpers.
//
// Ces dépendances ne sont ni inversées par callback ni remplacées par des globals.
package platform
