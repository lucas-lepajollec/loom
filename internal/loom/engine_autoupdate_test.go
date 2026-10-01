package loom

import (
	"errors"
	"testing"
	"time"
)

func TestEngineAutoUpdateWaitsForAFreeEngine(t *testing.T) {
	testHome(t)
	now := time.Unix(1_800_000_000, 0)
	check := func() (string, string, string, error) { return "prebuilt", "b100", "b200", nil }
	applied := ""
	apply := func(kind string) error { applied = kind; return nil }
	if a := engineAutoTick(now, check, func() bool { return true }, apply); applied != "" || a.CheckedAt != 0 {
		t.Fatal("désactivé : rien ne doit se passer")
	}
	saveEngineAuto(engineAuto{Auto: true})
	if a := engineAutoTick(now, check, func() bool { return false }, apply); applied != "" || a.Pending != "b200" {
		t.Fatalf("moteur occupé : attendre, %+v", a)
	}
	if a := engineAutoTick(now, check, func() bool { return true }, apply); applied != "prebuilt" || a.Pending != "" || a.LastTo != "b200" || a.LastFrom != "b100" {
		t.Fatalf("moteur libre : appliquer, %+v", a)
	}
	applied = ""
	upToDate := func() (string, string, string, error) { return "prebuilt", "b200", "", nil }
	if engineAutoTick(now, upToDate, func() bool { return true }, apply); applied != "" {
		t.Fatal("à jour : rien à faire")
	}
	failing := func() (string, string, string, error) { return "", "", "", errors.New("réseau") }
	if a := engineAutoTick(now, failing, func() bool { return true }, apply); a.LastError != "réseau" || applied != "" {
		t.Fatalf("%+v", a)
	}
}
