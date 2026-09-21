package ajean

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// La ressource de version Windows (cmd/loom/versioninfo.json) doit suivre la
// constante Version.
func TestVersionInfoWindowsSuitLaVersion(t *testing.T) {
	b, err := os.ReadFile("../../cmd/loom/versioninfo.json")
	if err != nil {
		t.Fatal(err)
	}
	var vi struct {
		FixedFileInfo struct {
			FileVersion    struct{ Major, Minor, Patch int }
			ProductVersion struct{ Major, Minor, Patch int }
		}
		StringFileInfo struct{ ProductVersion string }
	}
	if err := json.Unmarshal(b, &vi); err != nil {
		t.Fatal(err)
	}
	f := vi.FixedFileInfo.FileVersion
	for name, got := range map[string]string{
		"FixedFileInfo.FileVersion":     fmt.Sprintf("%d.%d.%d", f.Major, f.Minor, f.Patch),
		"FixedFileInfo.ProductVersion":  fmt.Sprintf("%d.%d.%d", vi.FixedFileInfo.ProductVersion.Major, vi.FixedFileInfo.ProductVersion.Minor, vi.FixedFileInfo.ProductVersion.Patch),
		"StringFileInfo.ProductVersion": vi.StringFileInfo.ProductVersion,
	} {
		if got != Version {
			t.Errorf("%s = %s, attendu %s — pense à mettre à jour cmd/loom/versioninfo.json après un bump", name, got, Version)
		}
	}
}
