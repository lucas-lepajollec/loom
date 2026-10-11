package loom

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
 "github.com/lucas-lepajollec/loom/internal/loom/resources"
)

var machineIDRe = regexp.MustCompile(`[^a-z0-9]+`)
var mcpNameRe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimLeft(strings.TrimPrefix(p, "~"), "/\\"))
	}
	return p
}
func hasNodeModule(modules []string, module string) bool { return slices.Contains(modules, module) }
func cloneEventMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func validateMCPSelection(names []string) error {
	if len(names) > 128 {
		return errors.New("maximum 128 MCP servers")
	}
	defs, err := LoadMCPConfig()
	if err != nil {
		return errors.New("MCP definitions unavailable")
	}
	for _, n := range names {
		if _, ok := defs[n]; !ok {
			return errors.New("selected MCP server not found")
		}
	}
	return nil
}

func hasName(list []string,name string)bool{return slices.Contains(list,name)}
// Used only to label preserved historical agent turns, never to execute them.
func legacyRuntime(id string)bool{return id!=""&&id!="llama.cpp"&&id!="openai-compatible"&&id!="policy"}
func runtimeVaultError()error{if memEncActive()&&!memUnlocked(){return errors.New("unlock the Loom vault")};return nil}
func defaultWorkspaceForTarget(target string)(resources.Workspace,error){ws:=workspaceList(target);for _,w:=range ws{if w.Default{return w,nil}};return resources.Workspace{},errors.New("default workspace unavailable")}
