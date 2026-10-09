package loom

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed voice/catalog.json
var voiceCatalogFS embed.FS

type voiceArtifact struct {
	ID        string            `json:"id"`
	Archive   string            `json:"archive,omitempty"`
	URL       string            `json:"url,omitempty"`
	Size      int64             `json:"size,omitempty"`
	SHA256    string            `json:"sha256"`
	OS        string            `json:"os,omitempty"`
	Arch      string            `json:"arch,omitempty"`
	Provider  string            `json:"provider,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	Languages []string          `json:"languages,omitempty"`
	Family    string            `json:"family,omitempty"`
	Files     map[string]string `json:"files,omitempty"`
	Streaming bool              `json:"streaming,omitempty"`
}
type voicePack struct {
	ID       string `json:"id"`
	Language string `json:"language"`
	Hardware string `json:"hardware"`
	STT      string `json:"stt"`
	TTS      string `json:"tts"`
	VAD      string `json:"vad"`
	KWS      string `json:"kws"`
	Reason   string `json:"reason"`
}
type voiceCatalog struct {
	Version string          `json:"version"`
	Engines []voiceArtifact `json:"engines"`
	Models  []voiceArtifact `json:"models"`
	Packs   []voicePack     `json:"packs"`
}

var curatedVoice = func() voiceCatalog {
	var c voiceCatalog
	b, _ := voiceCatalogFS.ReadFile("voice/catalog.json")
	if err := json.Unmarshal(b, &c); err != nil {
		panic(err)
	}
	return c
}()

func voiceRoot() string              { return filepath.Join(LoomHome(), "voice") }
func voiceModelDir(id string) string { return filepath.Join(voiceRoot(), "models", id) }
func voiceModel(id string) (voiceArtifact, error) {
	for _, m := range curatedVoice.Models {
		if m.ID == id {
			return m, nil
		}
	}
	return voiceArtifact{}, errors.New("unknown voice model")
}

// Resolve curated patterns once at install. Ambiguous archives are refused,
// rather than silently selecting a different model or quantization.
func voiceResolveFiles(root string, m voiceArtifact) (map[string]string, error) {
	out := map[string]string{}
	for key, pattern := range m.Files {
		var matches []string
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			match, _ := filepath.Match(pattern, d.Name())
			if match {
				matches = append(matches, path)
				if d.IsDir() {
					return filepath.SkipDir
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("voice model %s: expected one %s (%s), found %d", m.ID, key, pattern, len(matches))
		}
		resolved, err := filepath.EvalSymlinks(matches[0])
		if err != nil || !archivePathWithin(root, resolved) {
			return nil, errors.New("voice model path escapes installation")
		}
		out[key] = matches[0]
	}
	return out, nil
}
func voiceDiskUsage(root string) int64 {
	var size int64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, e := d.Info(); e == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
}
func voiceModelInstalled(m voiceArtifact) bool {
	_, err := voiceResolveFiles(voiceModelDir(m.ID), m)
	_, receiptErr := os.Stat(filepath.Join(voiceModelDir(m.ID), ".verified.json"))
	return err == nil && receiptErr == nil
}
func voiceEngineArtifact(goos, arch, provider string) (voiceArtifact, error) {
	for _, a := range curatedVoice.Engines {
		if a.OS == goos && a.Arch == arch && a.Provider == provider {
			return a, nil
		}
	}
	return voiceArtifact{}, errors.New("no curated sherpa build for this OS/architecture/provider")
}
func voiceHashReady(a voiceArtifact) bool {
	if len(a.SHA256) != 64 {
		return false
	}
	for _, c := range strings.ToLower(a.SHA256) {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
