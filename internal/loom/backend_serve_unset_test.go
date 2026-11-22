package loom

import "testing"

// A machine without an engine (it runs on another machine) must not make
// systemd or launchd restart `loom serve` forever.
func TestServeWithoutEngineStopsCleanlyUnderAServiceManager(t *testing.T) {
	testHome(t)
	t.Setenv("XPC_SERVICE_NAME", "0") // what macOS terminals set
	t.Setenv("INVOCATION_ID", "")
	if err := cmdServe(nil); err == nil {
		t.Fatal("interactive serve without BIN must fail")
	}
	t.Setenv("INVOCATION_ID", "synthetic-systemd-invocation")
	if err := cmdServe(nil); err != nil {
		t.Fatal("service without BIN must exit cleanly", err)
	}
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("XPC_SERVICE_NAME", "com.example.loom")
	if !startedByServiceManager() {
		t.Fatal("launchd service not recognised")
	}
}
