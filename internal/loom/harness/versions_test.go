package harness

import "testing"

func TestTestedVersionsLoading(t *testing.T) {
	for _, id := range []string{"codex", "pi", "opencode", "claude-acp"} {
		versions := TestedVersions(id)
		if len(versions) == 0 || LatestTestedVersion(id) != versions[len(versions)-1] || !VersionTested(id, versions[0]) {
			t.Fatal(id, versions)
		}
		versions[0] = "changed"
		if VersionTested(id, "changed") {
			t.Fatal("mutable accepted data")
		}
	}
	for _, id := range []string{"hermes", "openclaw", "deepseek-harness", "missing"} {
		if len(TestedVersions(id)) != 0 || LatestTestedVersion(id) != "" {
			t.Fatal(id)
		}
	}
	if !VersionTested("codex", "0.159.2") || !VersionTested("pi", "v0.84.3") || VersionTested("pi", "999") {
		t.Fatal("version normalization")
	}
	for id, newest := range map[string]string{"codex": "codex-cli 0.162.0", "pi": "1.1.0", "opencode": "1.18.35"} {
		if LatestTestedVersion(id) != newest || !VersionTested(id, newest) {
			t.Fatal(id, TestedVersions(id))
		}
	}
}
