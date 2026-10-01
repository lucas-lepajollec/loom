package loom

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/engine/llamacpp"
)

// Historical engine names during leaf-first migration. Configuration readers,
// sessions and web/proxy orchestration remain in Loom.
// Router execution uses explicit state and accessors supplied by these wrappers.
// The single compatibility owner replaces the scattered process/cache globals.
var llamaOwner = llamacpp.NewSupervisor()

type LlamaFlag = llamacpp.LlamaFlag
type ggufMeta = llamacpp.GGUFMeta
type chatTemplateCaps = llamacpp.ChatTemplateCaps
type estimateOpt = llamacpp.EstimateOptions
type vramEst = llamacpp.VRAMEstimate
type ParamSpec = llamacpp.ParamSpec
type curatedParams = llamacpp.CuratedParams
type routerEntry = llamacpp.RouterEntry

const (
	ggufMagic               = llamacpp.GGUFMagic
	ggufTypeUint8           = llamacpp.GGUFTypeUint8
	ggufTypeInt8            = llamacpp.GGUFTypeInt8
	ggufTypeUint16          = llamacpp.GGUFTypeUint16
	ggufTypeInt16           = llamacpp.GGUFTypeInt16
	ggufTypeUint32          = llamacpp.GGUFTypeUint32
	ggufTypeInt32           = llamacpp.GGUFTypeInt32
	ggufTypeFloat32         = llamacpp.GGUFTypeFloat32
	ggufTypeBool            = llamacpp.GGUFTypeBool
	ggufTypeString          = llamacpp.GGUFTypeString
	ggufTypeArray           = llamacpp.GGUFTypeArray
	ggufTypeUint64          = llamacpp.GGUFTypeUint64
	ggufTypeInt64           = llamacpp.GGUFTypeInt64
	ggufTypeFloat64         = llamacpp.GGUFTypeFloat64
	ggufMaxKV               = llamacpp.GGUFMaxKV
	ggufMaxString           = llamacpp.GGUFMaxString
	ggufMaxArrayLen         = llamacpp.GGUFMaxArrayLen
	estCUDAReserveB         = llamacpp.EstCUDAReserveB
	estComputeSafety        = llamacpp.EstComputeSafety
	estDefaultUBatch        = llamacpp.EstDefaultUBatch
	estCtxComputePerEmbd    = llamacpp.EstCtxComputePerEmbd
	estCtxComputePerEmbdMLA = llamacpp.EstCtxComputePerEmbdMLA
	estCtxComputeF16Mask    = llamacpp.EstCtxComputeF16Mask
	estVRAMFitFrac          = llamacpp.EstVRAMFitFrac
	estDefaultVocab         = llamacpp.EstDefaultVocab
)

func parseLlamaHelp(text string) []LlamaFlag       { return llamacpp.ParseLlamaHelp(text) }
func parseLlamaHelpFlagLine(line string) LlamaFlag { return llamacpp.ParseLlamaHelpFlagLine(line) }
func splitHelpFlagAndDesc(line string) (flags, desc string) {
	return llamacpp.SplitHelpFlagAndDesc(line)
}
func splitHelpComma(s string) []string              { return llamacpp.SplitHelpComma(s) }
func helpTokenArg(p string) (token, arg string)     { return llamacpp.HelpTokenArg(p) }
func canonicalHelpFlag(longs []string) string       { return llamacpp.CanonicalHelpFlag(longs) }
func parseHelpChoices(s string) []string            { return llamacpp.ParseHelpChoices(s) }
func finishLlamaFlag(f *LlamaFlag)                  { llamacpp.FinishLlamaFlag(f) }
func lookupFlagConfigKey(f LlamaFlag) string        { return llamacpp.LookupFlagConfigKey(f) }
func flagTierFor(f LlamaFlag) string                { return llamacpp.FlagTierFor(f) }
func parseHelpAllowed(help string) []string         { return llamacpp.ParseHelpAllowed(help) }
func parseHelpChoicesFromHelp(help string) []string { return llamacpp.ParseHelpChoicesFromHelp(help) }
func inferHelpKind(f LlamaFlag) string              { return llamacpp.InferHelpKind(f) }
func looksNumeric(s string) bool                    { return llamacpp.LooksNumeric(s) }
func isHiddenLlamaFlag(f LlamaFlag) bool            { return llamacpp.IsHiddenLlamaFlag(f) }
func mergeUniq(a, b []string) []string              { return llamacpp.MergeUniq(a, b) }
func containsFold(list []string, s string) bool     { return llamacpp.ContainsFold(list, s) }
func readGGUFMeta(path string) (ggufMeta, error)    { return llamacpp.ReadGGUFMeta(path) }
func pickGGUFContext(arch string, ctxByKey map[string]int) int {
	return llamacpp.PickGGUFContext(arch, ctxByKey)
}
func ggufReadString(r io.Reader) (string, error)       { return llamacpp.GGUFReadString(r) }
func ggufReadInt(r io.Reader, typ uint32) (int, error) { return llamacpp.GGUFReadInt(r, typ) }
func ggufSkipValue(r io.Reader, typ uint32) error      { return llamacpp.GGUFSkipValue(r, typ) }
func ggufSkipArrayKeepLen(r io.Reader) (uint64, error) { return llamacpp.GGUFSkipArrayKeepLen(r) }
func ggufReadIntArray(r io.Reader) ([]int, error)      { return llamacpp.GGUFReadIntArray(r) }
func ggufScalarSize(typ uint32) int64                  { return llamacpp.GGUFScalarSize(typ) }
func chatTemplateThinks(s string) bool                 { return llamacpp.ChatTemplateThinks(s) }
func inspectChatTemplate(s string) chatTemplateCaps    { return llamacpp.InspectChatTemplate(s) }
func effortDefaultIn(chunk string) string              { return llamacpp.EffortDefaultIn(chunk) }
func kvBPE(t string) float64                           { return llamacpp.KVBPE(t) }
func layerNKVHeads(m ggufMeta, i int) int              { return llamacpp.LayerNKVHeads(m, i) }
func layerIsSWA(m ggufMeta, i int) bool                { return llamacpp.LayerIsSWA(m, i) }
func kvHeadDims(m ggufMeta, swa bool) (klen, vlen int) { return llamacpp.KVHeadDims(m, swa) }
func estimateKVBytesPerLayer(m ggufMeta, ctx int, kvK, kvV string) int64 {
	return llamacpp.EstimateKVBytesPerLayer(m, ctx, kvK, kvV)
}
func estimateKVBytes(m ggufMeta, ctx int, kvK, kvV string) int64 {
	return llamacpp.EstimateKVBytes(m, ctx, kvK, kvV)
}
func estimateComputeBytes(m ggufMeta, ctx, ubatch, np int, kvK, kvV string) int64 {
	return llamacpp.EstimateComputeBytes(m, ctx, ubatch, np, kvK, kvV)
}
func estimateMTPBytes(m ggufMeta, ctx int, kvK, kvV string) int64 {
	return llamacpp.EstimateMTPBytes(m, ctx, kvK, kvV)
}
func estimateRun(m ggufMeta, opt estimateOpt) vramEst { return llamacpp.EstimateRun(m, opt) }
func maxCtxThatFits(m ggufMeta, opt estimateOpt, budget int64) int {
	return llamacpp.MaxCtxThatFits(m, opt, budget)
}
func vramEstJSON(e vramEst) map[string]any           { return llamacpp.VRAMEstimateJSON(e) }
func readCuratedParams() curatedParams               { return llamacpp.ReadCuratedParams() }
func paramMatchesFlag(p ParamSpec, f LlamaFlag) bool { return llamacpp.ParamMatchesFlag(p, f) }
func mergeEngineParams(c curatedParams, flags []LlamaFlag) []ParamSpec {
	return llamacpp.MergeEngineParams(c, flags)
}
func argsToPresetOptions(bin string, args []string) ([][2]string, error) {
	return llamacpp.ArgsToPresetOptions(llamaFlagsForBin(bin), args)
}
func routerEntryName(options [][2]string) string   { return llamacpp.RouterEntryName(options) }
func renderRouterINI(entries []routerEntry) []byte { return llamacpp.RenderRouterINI(entries) }
func unquoteValue(v string) string                 { return llamacpp.UnquoteValue(v) }
func parseEnv(text string) map[string]string       { return llamacpp.ParseEnv(text) }
func quoteValue(v string) string                   { return llamacpp.QuoteValue(v) }
func formatEnv(m map[string]string) string         { return llamacpp.FormatEnv(m) }
func splitArgs(s string) []string                  { return llamacpp.SplitArgs(s) }
func presetDisplayName(content, fallback string) string {
	return llamacpp.PresetDisplayName(content, fallback)
}
func withDisplayName(content, name string) string { return llamacpp.WithDisplayName(content, name) }

func flagToConfigKey(id string) (string, bool) { return llamacpp.FlagConfigKey(id) }

func buildLlamaServerArgsForConfig(cfg map[string]string) ([]string, error) {
	bin := cfg["BIN"]
	if bin == "" {
		return nil, fmt.Errorf("BIN non défini — lance « loom edit »")
	}
	model := cfg["MODEL"]
	if model == "" {
		return nil, fmt.Errorf("MODEL non défini — lance « loom edit »")
	}
	// MODEL vaut soit un simple nom de fichier (le .gguf vit dans LOOM_HOME ou
	// dans un dossier déclaré — disque externe…), soit un chemin absolu. Sous
	// systemd/launchd le WorkingDirectory vaut LOOM_HOME, donc le relatif tombait
	// juste ; lancé depuis une app de bureau, le répertoire courant est « / » et
	// llama-server ne trouvait rien. On résout donc explicitement, quel que soit
	// le contexte de lancement.
	resolved, err := resolveServeModelPath(model)
	if err != nil {
		return nil, err
	}
	model = resolved
	if _, err := os.Stat(model); err != nil {
		return nil, fmt.Errorf("modèle introuvable : %s", model)
	}
	// Modèle découpé en tranches : llama-server ouvre les suivantes tout seul, mais
	// s'il en manque une il démarre puis meurt sur un tenseur introuvable — message
	// incompréhensible, et systemd relance en boucle. On le dit ici, en clair.
	if missing := shardFamilyMissing(filepath.Dir(model), filepath.Base(model)); len(missing) > 0 {
		return nil, fmt.Errorf("modèle incomplet : il manque %s dans %s — ce modèle tient en %d fichiers, télécharge-les tous",
			strings.Join(missing, ", "), filepath.Dir(model), len(shardFamily(filepath.Base(model))))
	}
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(LoomHome(), bin)
	}
	// Le moteur précompilé s'installe dans un dossier versionné : un preset écrit
	// avant une mise à jour pointe sur une release qui n'existe plus. On le fait
	// suivre au moteur courant plutôt que d'échouer en 127.
	bin = prebuiltResolveBin(bin)

	// Make sure llama-server can find its bundled shared libraries (the .so/.dll
	// neighbours of the binary). This is platform-specific: LD_LIBRARY_PATH on
	// Linux, PATH on Windows — handled inside execServer.
	setLibraryPath(filepath.Dir(bin))

	// Sélection GPU (loom gpu) : on filtre les devices visibles par llama-server.
	// CUDA_DEVICE_ORDER=PCI_BUS_ID garantit que les index correspondent à ceux
	// affichés par nvidia-smi (sinon CUDA réordonne par "device le plus rapide").
	if v := cfg["CUDA_VISIBLE_DEVICES"]; v != "" {
		_ = os.Setenv("CUDA_VISIBLE_DEVICES", v)
		_ = os.Setenv("CUDA_DEVICE_ORDER", "PCI_BUS_ID")
	}

	return llamacpp.BuildServerArgs(cfg, llamacpp.ArgumentInputs{
		BinaryPath: bin, ModelPath: model, BackendPort: llamaBackendPort(),
		RuntimeGet: oaiRuntimeGet, ContextArg: serveCtxArg,
		ResolveModelPath: resolveServeModelPath,
		HasFlag:          func(id string) bool { return llamaHasFlag(bin, id) },
		BooleanFlag:      func(id string) bool { return llamaBooleanFlag(bin, id) },
		APIKey: func() (string, error) {
			if err := ensureAPIKeyIfRequired(); err != nil {
				return "", err
			}
			return readAPIKey(), nil
		},
		AppendRuntimeArgs: appendOAIRuntimeArgs, Warnings: os.Stderr,
	})
}

func cloneEngineConfig(cfg map[string]string) map[string]string {
	out := make(map[string]string, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	return out
}

// buildLlamaServerArgs construit argv de llama-server à partir de la config
// courante. --host/--port forcés en dernier : le GGUF n'écoute que en local,
// le front Loom prend HOST:PORT.
func buildLlamaServerArgs() ([]string, error) {
	return (llamaCppEngine{}).BuildArgs(ModelConfig{})
}

// binSupportsReasoningFlag dit si ce llama-server accepte « --reasoning ».
//
// Le drapeau est récent : les moteurs plus anciens, et certains forks, ne le
// connaissent pas et REFUSENT de démarrer sur un argument inconnu. Comme on ne
// l'ajoute que pour interdire le raisonnement, mieux vaut demander au binaire
// que parier : on lit son aide, une fois, au lancement du moteur.
//
// L'aide se lit avec le même chemin de bibliothèques que le vrai lancement
// (setLibraryPath a déjà été appelé) : sans ça un moteur parfaitement valide
// échoue à s'exécuter (« libllama-common.so introuvable ») et on conclurait à
// tort qu'il ne gère pas le drapeau.
func binSupportsReasoningFlag(bin string) bool {
	return llamaHasFlag(bin, "reasoning")
}

// loomRouterStore adapts the existing active store without capturing its path;
// tests and vault changes retain the historical dynamic store resolution.
type loomRouterStore struct{}

func (loomRouterStore) GetString(key string) string         { return getStr(bkState, key) }
func (loomRouterStore) PutString(key, value string) error   { return putStr(bkState, key, value) }
func (loomRouterStore) GetJSON(key string, dst any) bool    { return getJSON(bkState, key, dst) }
func (loomRouterStore) PutJSON(key string, value any) error { return putJSON(bkState, key, value) }

func llamaRouter() *llamacpp.Router {
	return &llamacpp.Router{
		BackendPort: llamaBackendPort(), INIPath: routerINIPath(),
		State: loomRouterStore{}, Lock: llamaOwner.RouterLock(), Authorize: localAuthHeader,
		APIKey: func() (string, error) {
			if err := ensureAPIKeyIfRequired(); err != nil {
				return "", err
			}
			return readAPIKey(), nil
		},
		SetLastError: setLlamaLastError,
	}
}

type routerModel = llamacpp.RouterModel

const (
	routerMaxEntries   = llamacpp.RouterMaxEntries
	routerLoadBudget   = llamacpp.RouterLoadBudget
	routerStateEntries = llamacpp.RouterStateEntries
	routerStateActive  = llamacpp.RouterStateActive
	routerStateCurrent = llamacpp.RouterStateCurrent
)

func routerServerArgs(bin string) []string {
	r := llamaRouter()
	r.BinaryPath, r.ModelsMax = bin, routerModelsMax()
	// Keep fallback config reads after successful credential preparation.
	return r.ServerArgs(func() string { return ReadConfig()["API_KEY"] })
}
func loadRouterEntries() []routerEntry                         { return llamaRouter().Entries() }
func rememberRouterEntry(e routerEntry) ([]routerEntry, error) { return llamaRouter().RememberEntry(e) }
func writeRouterINI(entries []routerEntry) error               { return llamaRouter().WriteINI(entries) }
func ensureRouterINI() error                                   { return llamaRouter().EnsureINI() }
func routerDo(method, path string, body any, timeout time.Duration) ([]byte, int, error) {
	return llamaRouter().Do(method, path, body, timeout)
}
func routerReachable() bool                           { return llamaRouter().Reachable() }
func routerModels(reload bool) ([]routerModel, error) { return llamaRouter().Models(reload) }
func routerModelsWithTimeout(reload bool, timeout time.Duration) ([]routerModel, error) {
	return llamaRouter().ModelsWithTimeout(reload, timeout)
}
func routerModelStatus(name string) (routerModel, bool) { return llamaRouter().ModelStatus(name) }
func routerEnsureLoaded(e routerEntry) error            { return llamaRouter().EnsureLoaded(e) }
func routerActivate() error {
	return llamaRouter().Activate(
		func() bool { return strings.TrimSpace(ReadConfig()["MODEL"]) != "" },
		func() (routerEntry, error) { return buildRouterEntry(activeEntryLabel()) },
	)
}
func routerActivateVariant() (string, error) {
	return llamaRouter().ActivateVariant(func() (routerEntry, error) {
		return buildRouterEntry(activeEntryLabel() + " · variante API")
	})
}
func routerUnloadAll() error    { return llamaRouter().UnloadAll() }
func routerCurrentName() string { return llamaRouter().CurrentName() }
func observedEngineCtx() *int   { return llamaRouter().ObservedContext() }

// Historical lifecycle/cache entry points delegate to the same explicit owner.
type llamaSlot = llamacpp.Slot
type srvRecent = llamacpp.Recent

func setLlamaLastError(msg string)  { llamaOwner.SetLastError(msg) }
func getLlamaLastError() string     { return llamaOwner.LastError() }
func initOwnedLlamaSupervisor()     { llamaOwner.Init() }
func shutdownOwnedLlamaSupervisor() { llamaOwner.Shutdown() }
func ownedLlamaManaged() bool       { return llamaOwner.Managed() }
func ownedLlamaRunning() bool       { return llamaOwner.Running() }
func startOwnedLlama(bin string, args []string) error {
	return llamaOwner.Start(func() llamacpp.Launch { return ownedLlamaLaunch(bin, args) })
}
func stopOwnedLlama()                        { llamaOwner.Stop() }
func llamaBackendPortFor(public int) int     { return llamacpp.BackendPortFor(public) }
func llamaBackendPort() int                  { return llamaBackendPortFor(LLMPort()) }
func routerModeCached() bool                 { return llamaOwner.RouterModeCached(routerReachable) }
func loadGGUFMeta(path string) ggufMeta      { return llamaOwner.LoadGGUFMeta(path, ggufWeightBytes) }
func ggufContextLength(path string) int      { return loadGGUFMeta(path).ContextLength }
func parseLlamaSlots(raw []byte) []llamaSlot { return llamacpp.ParseSlots(raw) }
func resetServerWatch()                      { llamaOwner.ResetServerWatch() }
func noteServerSlots(slots []llamaSlot) ([]srvRecent, []map[string]any) {
	return llamaOwner.NoteServerSlots(slots)
}
func serverStats(busy int, liveTokS float64) map[string]any {
	return llamaOwner.ServerStats(busy, liveTokS)
}
