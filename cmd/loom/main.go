// Point d'entrée du binaire loom. Tout le code vit dans internal/loom ; ce
// dossier ne porte que le main() et les ressources Windows (.syso, icône,
// versioninfo) qui doivent résider dans le dossier du package main.
//
// Les directives ci-dessous embarquent les métadonnées Windows (éditeur, version,
// description) dans le .exe pour réduire les faux positifs antivirus. Régénère
// les .syso après avoir bumpé la version : `go generate ./cmd/loom`
// Le générateur est épinglé et lancé par go run, sans installation globale.
//
//go:generate go run github.com/lucas-lepajollec/loom/tools/gen-icon icon.ico
//go:generate go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0 -64 -icon=icon.ico -o resource_windows_amd64.syso versioninfo.json
//go:generate go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0 -64 -arm -icon=icon.ico -o resource_windows_arm64.syso versioninfo.json
package main

import "github.com/lucas-lepajollec/loom/internal/loom"

func main() { loom.Main() }
