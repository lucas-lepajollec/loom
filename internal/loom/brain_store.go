package loom

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

// Definitions are settings; only the derived file index carries source text.
// Memory/conversation text is deliberately never persisted in this cache.
type brainStorage struct{ dir string }

func (s brainStorage) read(name string, limit int64) ([]byte, error) {
	f, err := os.Open(filepath.Join(s.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("Brain storage file exceeds limit")
	}
	return b, err
}
func (s brainStorage) write(name string, b []byte) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return memWriteFileAtomic(filepath.Join(s.dir, name), b, 0o600)
}
func (s brainStorage) LoadSources() ([]brain.Source, error) {
	b, err := s.read("sources.json", 4<<20)
	if err != nil || len(b) == 0 {
		return nil, err
	}
	var sources []brain.Source
	err = json.Unmarshal(b, &sources)
	return sources, err
}
func (s brainStorage) SaveSources(sources []brain.Source) error {
	b, err := json.Marshal(sources)
	if err != nil {
		return err
	}
	return s.write("sources.json", b)
}
func (s brainStorage) LoadIndex() (brain.Snapshot, error) {
	var snapshot brain.Snapshot
	b, err := s.read("index.json", 256<<20)
	if err != nil || len(b) == 0 {
		return snapshot, err
	}
	b, err = decodeMemContent(b)
	if err != nil {
		return snapshot, err
	}
	// The index is a disposable cache. Invalid JSON is rebuilt from sources;
	// encrypted data that cannot be unlocked is never treated as an empty cache.
	if json.Unmarshal(b, &snapshot) != nil {
		return brain.Snapshot{}, nil
	}
	return snapshot, nil
}
func (s brainStorage) SaveIndex(snapshot brain.Snapshot) error {
	b, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	b, err = encodeMemContent(b)
	if err != nil {
		return err
	}
	return s.write("index.json", b)
}
