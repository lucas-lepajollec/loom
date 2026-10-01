package loom

import (
	"io"

	"github.com/lucas-lepajollec/loom/internal/loom/engine/llamacpp"
)

// Historical engine names during leaf-first migration. Configuration readers,
// caches, owned processes, sessions and HTTP orchestration remain in Loom.
// Wrappers pass resolved inputs to llama.cpp helpers; they add no global state.
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
