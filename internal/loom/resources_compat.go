package loom

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
)

// Historical names delegate to the leaf; Loom retains application state.

type MCPServerConfig = resources.MCPServerConfig

func sortedServerNames(servers map[string]MCPServerConfig) []string {
	return resources.SortedServerNames(servers)
}

func mcpJSONError(err error) error { return resources.MCPJSONError(err) }

func parseMCPFile(data []byte) (map[string]json.RawMessage, map[string]json.RawMessage, map[string]MCPServerConfig, error) {
	return resources.ParseMCPFile(data)
}

func sameMCPFile(a, b os.FileInfo) bool { return resources.SameMCPFile(a, b) }

type MCPFileStatus = resources.MCPFileStatus

type MCPSource = resources.MCPSource

type MCPSourceServer = resources.MCPSourceServer

type MCPSourceStatus = resources.MCPSourceStatus

type linkedMCPServer = resources.LinkedMCPServer

func mcpStringKeys(m map[string]string) []string { return resources.MCPStringKeys(m) }

func mcpSourceSelector(path, project string) string {
	return resources.MCPSourceSelector(path, project)
}

func adoptedMCPDefinition(cfg MCPServerConfig, withEnv bool) (MCPServerConfig, error) {
	return resources.AdoptedMCPDefinition(cfg, withEnv)
}

func readMCPSource(source MCPSource) ([]linkedMCPServer, error) {
	return resources.ReadMCPSource(source)
}

type Capability = resources.Capability

type SkillSource = resources.SkillSource

var errReadOnlySkill = resources.ErrReadOnlySkill

func parseSkillMarkdown(raw string) (map[string]string, string) {
	return resources.ParseSkillMarkdown(raw)
}

func yamlQuote(s string) string { return resources.YAMLQuote(s) }

func skillFileContent(slug string, c Capability) string { return resources.SkillFileContent(slug, c) }

func readSkillDir(src SkillSource, dir string) (Capability, bool) {
	return resources.ReadSkillDir(src, dir)
}

func skillDirSlug(name string) string { return resources.SkillDirSlug(name) }

func asciiFold(s string) string { return resources.ASCIIFold(s) }

func isLoomSkillLink(dir string) bool { return resourceLibrary().IsLoomSkillLink(dir) }

func scanSkills() []Capability { return resourceLibrary().ScanSkills(skillSources()) }

func writeLoomSkill(c Capability, dir string) (string, error) {
	return resourceLibrary().WriteSkill(c, dir)
}

func resourceLibrary() resources.Library {
	links := map[string]string{}
	for _, target := range loadSkillSinks() {
		for name, source := range target.Links {
			links[filepath.Join(target.Dir, name)] = source
		}
	}
	return resources.Library{Root: loomSkillsDir(), OpenRoot: openOwnedSkillsRoot, ManagedLinks: links, NewID: newSessionID}
}

func skillBound(b map[string]map[string]bool, skillID, target string) bool {
	return resources.SkillBound(b, skillID, target)
}

func harnessMCPBinding(id string) *[]string { return resourceBindings().HarnessMCPBinding(id) }

func skillBindings() map[string]map[string]bool { return resourceBindings().SkillBindings() }

func setHarnessMCPBinding(id string, names *[]string) error {
	return resourceBindings().SetHarnessMCPBinding(id, names, validateACPMCPSelection)
}

type resourceStateStore struct{}

func (resourceStateStore) GetJSON(key string, value any) bool {
	return getStoreJSON(bkState, key, value)
}
func (resourceStateStore) PutJSON(key string, value any) error {
	return putStoreJSON(bkState, key, value)
}
func resourceBindings() resources.Bindings { return resources.Bindings{Store: resourceStateStore{}} }

type skillSinkTarget = resources.SkillSinkTarget

func hasName(list []string, s string) bool { return resources.HasName(list, s) }

func isLinkInto(p string, skills []Capability) bool { return resources.IsLinkInto(p, skills) }

func skillSinkCurrent(dest, src string) bool { return resources.SkillSinkCurrent(dest, src) }

func placeSkill(src, dest string) error { return resources.PlaceSkill(src, dest) }

func copySkillDir(src, dest string) error { return resources.CopySkillDir(src, dest) }

func adoptedMCP(m HarnessMCP) (MCPServerConfig, error) {
	return resources.AdoptHarnessMCP(m.Command, m.URL, m.Args, m.EnvNames)
}
