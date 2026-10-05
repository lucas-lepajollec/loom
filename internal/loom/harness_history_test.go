package loom

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestHarnessHistoryTransferFreshDestinationDedupAndOriginal(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_IMPORT_FIXTURE", "1")
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = old })
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source := acpAgent{ID: "native-source", Machine: "source-machine", Name: "Fixture source", Command: binary, Args: []string{"-test.run=TestNativeImportHelperProcess"}}
	for _, id := range []string{"destination-one", "destination-two"} {
		a := acpAgent{ID: id, Name: id, Custom: true, Command: binary}
		registeredRuntimes.upsert(&acpAdapter{agent: a})
		t.Cleanup(func() { registeredRuntimes.remove(id) })
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info := acpSessionInfo{SessionID: "native-fixture", Cwd: t.TempDir(), Title: "Source transcript"}
	a, err := transferHarnessHistory(ctx, source, info, "destination-one:default", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.RuntimeID != "destination-one" || a.ImportSource.TargetChoice != "destination-one:default" || len(a.Messages) != 2 || a.NativeSessionID != "" || a.Permission != "ask" || a.Workdir == info.Cwd {
		t.Fatalf("unsafe destination session: %+v", a)
	}
	original, err := importACPSession(ctx, source, info, "", true)
	if err != nil || original.ID == a.ID || original.ImportSource.TargetChoice != "" {
		t.Fatal("original transcript was replaced")
	}
	again, err := transferHarnessHistory(ctx, source, info, "destination-one:default", "")
	if err != nil || again.ID != a.ID {
		t.Fatal("duplicate destination transcript")
	}
	b, err := transferHarnessHistory(ctx, source, info, "destination-two:default", "")
	if err != nil || b.ID == a.ID || b.RuntimeID != "destination-two" {
		t.Fatal("second destination collided")
	}
	if len(workspaceSessions.list()) != 3 {
		t.Fatal("unexpected copies")
	}
	if len(workspaceSessions.runs) != 0 || len(workspaceSessions.acp) != 0 {
		t.Fatal("import ran an executor or generated a turn")
	}
	if nativeImportMatches(a, source, info.SessionID) {
		t.Fatal("transferred copy mistaken for the source")
	}
}
