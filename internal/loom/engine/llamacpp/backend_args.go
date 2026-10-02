package llamacpp

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ArgumentInputs supplies resolved paths and Loom-owned accessors. Accessors are
// called at the same stage as launch construction (not eagerly), preserving
// credential preparation, help/cache discovery and runtime overlay precedence.
// All accessors are required; this package owns no config, credentials or caches.
type ArgumentInputs struct {
	BinaryPath        string
	ModelPath         string
	BackendPort       int
	RuntimeGet        func(string) string
	ContextArg        func(map[string]string, string) string
	ResolveModelPath  func(string) (string, error)
	HasFlag           func(string) bool
	BooleanFlag       func(string) bool
	APIKey            func() (string, error)
	AppendRuntimeArgs func([]string) []string
	Warnings          io.Writer
}

// BuildServerArgs is the single config-to-argv implementation. Inputs have
// already passed Loom's model/shard validation and process environment setup.
// EXTRA_ARGS and runtime flags retain their order; loopback binding is last.
func BuildServerArgs(cfg map[string]string, in ArgumentInputs) ([]string, error) {
	bin, model := in.BinaryPath, in.ModelPath
	get := func(key, fallback string) string {
		if v := in.RuntimeGet(key); v != "" {
			return v
		}
		if v, ok := cfg[key]; ok && v != "" {
			return v
		}
		return fallback
	}
	kv := get("KV_TYPE", "")
	ktv := get("KV_TYPE_K", kv)
	vtv := get("KV_TYPE_V", kv)

	// Only opt into fitting on binaries that actually advertise it. Explicit
	// EXTRA_ARGS wins, as for every existing native flag.
	fit := get("FIT", "")
	args := SplitArgs(cfg["EXTRA_ARGS"])
	for i, arg := range args {
		if (arg == "--fit" || arg == "-fit") && i+1 < len(args) {
			fit = args[i+1]
		}
		if strings.HasPrefix(arg, "--fit=") {
			fit = strings.TrimPrefix(arg, "--fit=")
		}
	}
	fitSupported := fit != "" && in.HasFlag("fit")
	autoFit := fitSupported && strings.EqualFold(fit, "on")
	llmArgs := []string{bin, "-m", model}
	if fitSupported && get("FIT", "") != "" {
		llmArgs = append(llmArgs, "--fit", get("FIT", ""))
	}
	nglFallback := "999"
	if autoFit {
		nglFallback = ""
	}
	if ngl := get("NGL", nglFallback); ngl != "" {
		llmArgs = append(llmArgs, "-ngl", ngl)
	}
	ctxCfg := cfg
	if autoFit {
		ctxCfg = cloneConfig(cfg)
		ctxCfg["FIT"] = "on"
	} else {
		ctxCfg = cloneConfig(cfg)
		delete(ctxCfg, "FIT")
	}
	if ctx := in.ContextArg(ctxCfg, model); ctx != "" {
		llmArgs = append(llmArgs, "-c", ctx)
	}

	llmArgs = append(llmArgs,
		"-t", get("THREADS", "0"),
		"-tb", get("THREADS_BATCH", "0"),
		"-b", get("BATCH", "2048"),
		"-ub", get("UBATCH", "512"),
		// Par défaut 4 slots parallèles : permet de servir les requêtes concurrentes
		// (ex: TraDoc, clients externes, agents) en simultané sans sérialisation.
		// llama-server gère le continuous batching nativement. Un preset ou réglage
		// peut ajuster NP= ou -np dans EXTRA_ARGS.
		"-np", get("NP", "4"),
	)
	if ktv != "" {
		llmArgs = append(llmArgs, "-ctk", ktv)
	}
	if vtv != "" {
		llmArgs = append(llmArgs, "-ctv", vtv)
	}
	// Vision : le projecteur multimodal (mmproj-*.gguf) donne des yeux au modèle.
	// C'est un fichier .gguf À PART du modèle, chargé via --mmproj. On le résout
	// comme le modèle (nom simple cherché dans les dossiers déclarés, ou chemin
	// absolu), pour qu'un preset écrit sous Windows reste lançable ailleurs et que
	// le champ « Vision » de l'interface n'ait qu'à écrire le nom du fichier.
	// Introuvable = on préfère le dire clairement plutôt que laisser llama-server
	// mourir en boucle sur un « failed to load mmproj » cryptique.
	if mm := strings.TrimSpace(cfg["MMPROJ"]); mm != "" {
		mmPath, err := in.ResolveModelPath(mm)
		if err != nil {
			return nil, fmt.Errorf("vision projector not found: %s (%v)", mm, err)
		}
		if _, err := os.Stat(mmPath); err != nil {
			return nil, fmt.Errorf("vision projector not found: %s", mmPath)
		}
		llmArgs = append(llmArgs, "--mmproj", mmPath)
	}
	// Décodage spéculatif à modèle de draft (EAGLE-3, dFlash, dSpark, modèle
	// brouillon séparé, et parfois MTP quand la tête est fournie à part) : le .gguf
	// qui anticipe les jetons est choisi dans l'éditeur de preset et rangé dans la
	// clé MODEL_DRAFT, traduite ici en --model-draft et résolue comme le modèle
	// principal (nom simple cherché dans les dossiers déclarés, ou chemin absolu).
	// MTP est le plus souvent intégré au modèle → clé vide, pas de --model-draft.
	if md := strings.TrimSpace(cfg["MODEL_DRAFT"]); md != "" {
		mdPath, err := in.ResolveModelPath(md)
		if err != nil {
			return nil, fmt.Errorf("draft model not found: %s (%v)", md, err)
		}
		if _, err := os.Stat(mdPath); err != nil {
			return nil, fmt.Errorf("draft model not found: %s", mdPath)
		}
		llmArgs = append(llmArgs, "--model-draft", mdPath)
	}
	// Raisonnement. Trois cas, et la nuance compte :
	//
	//   REASONING=on|auto|deepseek → --reasoning <valeur>
	//   REASONING=off              → --reasoning off  (interdiction EXPLICITE)
	//   clé absente                → aucun drapeau, le moteur fait son défaut
	//
	// L'interface écrivait « off » en EFFAÇANT la ligne, ce qui n'est pas du tout
	// la même chose : sans drapeau, llama-server suit le gabarit du modèle, et un
	// modèle à raisonnement raisonne. L'interrupteur affichait donc « désactivé »
	// pendant que le modèle réfléchissait quand même. Il faut le dire au moteur.
	if r := strings.TrimSpace(cfg["REASONING"]); r != "" {
		if reasoningActive(r) {
			// budget illimité par défaut (-1) : on laisse le modèle réfléchir jusqu'au
			// bout au lieu de le couper à 2048, ce qui tronquait la vraie réponse (la
			// réflexion atteignait le plafond et il ne restait plus de marge pour le
			// contenu). L'anti-boucle côté llm_client.go reste le garde-fou. NE PAS forcer 0 :
			// sur llama.cpp vanilla, 0 = "immediate end" → coupe tout le raisonnement
			// (le fork ik_llama.cpp l'ignore). Configurable via REASONING_BUDGET.
			llmArgs = append(llmArgs, "--reasoning", r, "--reasoning-budget", get("REASONING_BUDGET", "-1"))
		} else if in.HasFlag("reasoning") {
			// Pas de budget ici : « off » suffit, et un budget sur un moteur qui
			// n'attend rien d'autre ne ferait qu'ajouter une occasion d'échouer.
			llmArgs = append(llmArgs, "--reasoning", "off")
		} else {
			// Vieux moteur (ou fork) qui ne connaît pas le drapeau : le lui passer
			// le ferait sortir en erreur au démarrage, donc boucler. On le dit et on
			// continue sans — mieux vaut un modèle qui réfléchit qu'un moteur mort.
			fmt.Fprintf(in.Warnings, "[loom serve] this engine does not support --reasoning: cannot disable reasoning\n")
		}
	}
	if eff := strings.TrimSpace(cfg["REASONING_EFFORT"]); eff != "" && in.HasFlag("reasoning-effort") {
		switch strings.ToLower(strings.TrimSpace(cfg["REASONING"])) {
		case "off", "none", "false", "0", "no", "disable", "disabled":
			// L'effort sans raisonnement contredit l'interrupteur.
		default:
			llmArgs = append(llmArgs, "--reasoning-effort", eff)
		}
	}
	// API_KEY protège le serveur quand il est exposé sur internet : llama-server
	// exige alors l'en-tête "Authorization: Bearer <clé>". La clé est lue depuis
	// $LOOM_HOME/.api_key en priorité (elle survit ainsi aux changements de preset
	// qui réécrivent config.env), avec config.env comme repli rétro-compatible.
	key, err := in.APIKey()
	if err != nil {
		return nil, err
	}
	if key != "" {
		llmArgs = append(llmArgs, "--api-key", key)
	} else if k := cfg["API_KEY"]; k != "" {
		llmArgs = append(llmArgs, "--api-key", k)
	}
	// /slots (suivi live des jetons) est parfois éteint par défaut. On ne
	// passe --slots que si ce binaire l'accepte comme booléen — jamais un
	// drapeau inventé, jamais un argument-chemin.
	if in.BooleanFlag("slots") {
		llmArgs = append(llmArgs, "--slots")
	}
	// EXTRA_ARGS is appended verbatim, split like the shell would — quotes kept
	// together so a path with spaces stays one argument.
	llmArgs = append(llmArgs, SplitArgs(cfg["EXTRA_ARGS"])...)
	llmArgs = in.AppendRuntimeArgs(llmArgs)
	// Après EXTRA_ARGS pour gagner sur un --host/--port collé dans le preset :
	// llama-server reste local, le front Loom est l'écoute publique.
	llmArgs = append(llmArgs,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(in.BackendPort),
	)
	return llmArgs, nil
}

func cloneConfig(cfg map[string]string) map[string]string {
	out := make(map[string]string, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	return out
}

func reasoningActive(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "off", "none", "false", "0", "no", "disable", "disabled":
		return false
	}
	return true
}
