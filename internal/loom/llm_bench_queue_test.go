package loom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBenchTestsCRUD(t *testing.T) {
	testHome(t)
	list := listBenchTests()
	if len(list) < 1 || list[0].ID != benchTestPerf {
		t.Fatalf("attendu Perfs brutes en tête : %+v", list)
	}
	got, err := saveCustomBenchTest("Résumé", "Résume ce texte en trois phrases.", 128)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "prompt" || got.MaxTokens != 128 {
		t.Fatalf("test = %+v", got)
	}
	if _, ok := findBenchTest(got.ID); !ok {
		t.Fatal("test introuvable après save")
	}
	if err := deleteCustomBenchTest(benchTestPerf); err == nil {
		t.Fatal("le test brut ne doit pas se supprimer")
	}
	if err := deleteCustomBenchTest(got.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := findBenchTest(got.ID); ok {
		t.Fatal("test encore là après delete")
	}
}

func TestStartBenchQueueRejectsEmpty(t *testing.T) {
	testHome(t)
	if _, err := startBenchQueue(benchTestPerf, nil); err == nil {
		t.Fatal("file vide acceptée")
	}
	if _, err := startBenchQueue("inconnu", []benchPick{{Model: "a.gguf"}}); err == nil {
		t.Fatal("test inconnu accepté")
	}
}

func TestBenchRowsFromPicksPreset(t *testing.T) {
	testHome(t)
	if err := os.MkdirAll(presetsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(presetsDir(), "demo.env"), []byte("# NAME=Demo\nMODEL=/tmp/x.gguf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := benchRowsFromPicks([]benchPick{
		{Preset: "demo", Name: "Demo", Model: "/tmp/x.gguf"},
		{Model: "/tmp/y.gguf", Name: "Y"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Preset != "demo" || rows[1].Model != "/tmp/y.gguf" {
		t.Fatalf("rows = %+v", rows)
	}
}
