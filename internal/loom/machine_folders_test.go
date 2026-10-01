package loom

import "testing"

func TestMachineFolders(t *testing.T) {
	testHome(t)
	dir := t.TempDir()
	if _, err := cleanMachineFolders("local", []string{"relatif"}); err == nil {
		t.Fatal("chemin relatif accepté")
	}
	if _, err := cleanMachineFolders("local", []string{dir + "/absent"}); err == nil {
		t.Fatal("dossier absent accepté")
	}
	got, err := cleanMachineFolders("local", []string{dir, dir + "/", " "})
	if err != nil || len(got) != 1 || got[0] != dir {
		t.Fatalf("%v %v", err, got)
	}
	m := RemoteMachine{ID: "box", Name: "box", Host: "10.0.0.2", User: "root", Port: 22, Folders: []string{"/srv/app"}}
	if err := saveRemoteMachine(m, nil); err != nil {
		t.Fatal(err)
	}
	m.Folders = nil // a later re-check comes without folders
	if err := saveRemoteMachine(m, nil); err != nil {
		t.Fatal(err)
	}
	if f := machineFolders("box"); len(f) != 1 || f[0] != "/srv/app" {
		t.Fatalf("dossiers perdus : %v", f)
	}
	if _, err := cleanMachineFolders("box", []string{"srv"}); err == nil {
		t.Fatal("dossier distant relatif accepté")
	}
}
