// Package harness owns embedded agent catalogue and compatibility data.
package harness

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed tested_versions.json
var testedVersionsJSON []byte

var testedVersions = func() map[string][]string {
	var versions map[string][]string
	if err := json.Unmarshal(testedVersionsJSON, &versions); err != nil {
		panic("invalid tested agent versions: " + err.Error())
	}
	return versions
}()

// TestedVersions returns a copy; observations never amend accepted versions.
func TestedVersions(id string) []string {
	return append([]string{}, testedVersions[id]...)
}

func LatestTestedVersion(id string) string {
	versions := testedVersions[id]
	if len(versions) == 0 {
		return ""
	}
	return versions[len(versions)-1]
}

func VersionTested(id, version string) bool {
	// Codex prints its executable name; Pi releases sometimes prefix v.
	normalize := func(v string) string {
		return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "codex-cli "), "v")
	}
	for _, tested := range testedVersions[id] {
		if normalize(version) == normalize(tested) {
			return true
		}
	}
	return false
}
