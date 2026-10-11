package loom

import (
	"encoding/json"
	"os"

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
	return resources.Library{Root: loomSkillsDir(), OpenRoot: openOwnedSkillsRoot, ManagedLinks: links, NewID: newSessionID}
}

type resourceStateStore struct{}

func (resourceStateStore) GetJSON(key string, value any) bool {
	return getStoreJSON(bkState, key, value)
}
func (resourceStateStore) PutJSON(key string, value any) error {
	return putStoreJSON(bkState, key, value)
}

func copySkillDir(src, dest string) error { return resources.CopySkillDir(src, dest) }
