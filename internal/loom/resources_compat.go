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

func parseMCPFile(data []byte) (map[string]json.RawMessage, map[string]json.RawMessage, map[string]MCPServerConfig, error) {
	return resources.ParseMCPFile(data)
}

func sameMCPFile(a, b os.FileInfo) bool { return resources.SameMCPFile(a, b) }

type MCPFileStatus = resources.MCPFileStatus

type MCPSource = resources.MCPSource

type MCPSourceServer = resources.MCPSourceServer

type MCPSourceStatus = resources.MCPSourceStatus

type linkedMCPServer = resources.LinkedMCPServer

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

func readSkillDir(src SkillSource, dir string) (Capability, bool) {
	return resources.ReadSkillDir(src, dir)
}

func scanSkills() []Capability { return resourceLibrary().ScanSkills(skillSources()) }

func writeLoomSkill(c Capability, dir string) (string, error) {
	return resourceLibrary().WriteSkill(c, dir)
}

func resourceLibrary() resources.Library {
	links := map[string]string{}
	return resources.Library{Root: loomSkillsDir(), OpenRoot: openOwnedSkillsRoot, ManagedLinks: links, NewID: newSessionID}
}

type resourceStateStore struct{}
