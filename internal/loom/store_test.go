package loom

import "testing"

// testHome donne au test un $LOOM_HOME à lui, et neutralise le pare-feu : un
// test ne doit jamais poser (ni retirer) une règle entrante sur la machine qui
// le fait tourner.
func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("LOOM_HOME", home)
	firewallInert = true
	t.Cleanup(func() {
		skillSinkJobs.Wait()
		firewallInert = false
	})
	return home
}

// Une base absente se comporte comme une base vide : les lecteurs ont tous un
// défaut, aucun ne doit paniquer.
func TestLecturesSurBaseVide(t *testing.T) {
	testHome(t)
	if len(ReadConfig()) != 0 {
		t.Error("configuration non vide sur une base neuve")
	}
	if agentEnabled() || internetEnabled() {
		t.Error("interrupteurs actifs sur une base neuve")
	}
	if readAPIKey() != "" || readWebKey() != "" {
		t.Error("clés non vides sur une base neuve")
	}
}
