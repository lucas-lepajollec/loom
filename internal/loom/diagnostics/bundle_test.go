package diagnostics

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestRedactionInEverySourceAndManifest(t *testing.T) {
	secrets := []string{"SECRET_API_123", "SECRET_TOKEN_456", "SECRET_PASSWORD_789", "SECRET_HEADER_012"}
	private := map[string]any{"key": secrets[0], "token": secrets[1], "password": secrets[2], "nested": map[string]any{"authorization": secrets[3], "url": "https://user:SECRET_PASSWORD_789@example.test/path?token=SECRET_TOKEN_456", "email": "person@example.test", "file": "/home/person/secret-path", "system_prompt": "PRIVATE_PROMPT", "messages": []string{"PRIVATE_DISCUSSION"}, "content": "PRIVATE_BRAIN"}, "detail": strings.Join(secrets, " ")}
	sources := []Source{}
	for _, name := range []string{"versions.json", "doctor.json", "compatibility.json", "failed-probes.json", "config.json", "events.json"} {
		sources = append(sources, Source{Name: name, Value: private})
	}
	for _, name := range []string{"engine", "node", "agents"} {
		sources = append(sources, Source{Name: "logs/" + name + ".log", IsLog: true, Log: "error " + strings.Join(secrets, " ") + " PRIVATE_PROMPT PRIVATE_DISCUSSION PRIVATE_BRAIN /home/person/secret-path person@example.test"})
	}
	raw, manifest, err := Build(sources, Redactor{Secrets: secrets, Homes: []string{"/home/person"}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != len(reader.File) {
		t.Fatal("manifest incomplete")
	}
	for _, file := range reader.File {
		in, _ := file.Open()
		data, _ := io.ReadAll(in)
		in.Close()
		for _, secret := range append(secrets, "PRIVATE_PROMPT", "PRIVATE_DISCUSSION", "PRIVATE_BRAIN", "/home/person", "person@example.test") {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatalf("%s leaked %s: %s", file.Name, secret, data)
			}
		}
		if strings.HasSuffix(file.Name, ".json") && !json.Valid(data) {
			t.Fatal("invalid JSON", file.Name)
		}
	}
}
func TestUnlabelledLogContentAndBounds(t *testing.T) {
	r := Redactor{}
	data, _ := r.Log(strings.Repeat("unlabelled private prompt\n", 500), 100)
	if bytes.Contains(data, []byte("unlabelled")) || bytes.Count(data, []byte("\n")) != 100 {
		t.Fatal(string(data))
	}
	if _, _, err := Build([]Source{{Name: "../private", Value: "x"}}, r); err == nil {
		t.Fatal("path escape accepted")
	}
	if _, _, err := Build([]Source{{Name: "big.json", Value: strings.Repeat("x", MaxSourceBytes)}}, r); err == nil {
		t.Fatal("source limit missing")
	}
}
