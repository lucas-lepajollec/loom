package loom

import (
	"runtime"
	"strings"
	"testing"
)

func TestEngineInstallPermissionsFailBeforeToolsAndNetwork(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("read-only proc filesystem fixture")
	}
	t.Setenv("LOOM_HOME", "/proc/loom-readonly-fixture")
	assertDenied := func(err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "not writable") {
			t.Fatalf("permissions failure lost: %v", err)
		}
	}
	_, err := prebuiltInstall(func(string) { t.Fatal("prebuilt reached download log") }, func(string) { t.Fatal("prebuilt reached upstream query") })
	assertDenied(err)
	assertDenied(llamacppInstall([]string{"--dir=/proc/loom-readonly-source"}))
	_, err = installCustomBackend("https://example.invalid/engine.git", "fixture", "", func(string) { t.Fatal("custom install reached tools/network") })
	assertDenied(err)
	v := &vllmState{job: "install"}
	assertDenied(v.installOrUpdate(false))
	if v.job != "" || v.err == "" {
		t.Fatal("vLLM permission failure left an action running")
	}
}
