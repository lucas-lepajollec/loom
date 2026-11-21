package brain

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxMemoryFile = 64 << 10

// MemoryStoreOptions confines all IO to Dir. Base is .loom for a user vault,
// or loom-memory for Loom's private fallback. Codecs apply only to the fallback.
type MemoryStoreOptions struct {
	Dir    string
	Base   string
	Decode func([]byte) ([]byte, error)
}

// legacyMemoryReader reads the previous class-based layout during Markdown migration.
type legacyMemoryReader struct {
	opts      MemoryStoreOptions
	items     map[string]MemoryItem
	malformed int
}

func (s *legacyMemoryReader) read(root *os.Root, path string) ([]byte, error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("memory file is not regular")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxMemoryFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxMemoryFile {
		return nil, errors.New("memory file exceeds 64 KiB")
	}
	if s.opts.Decode != nil {
		return s.opts.Decode(b)
	}
	return b, nil
}
func unmarshalMemory(data []byte) (MemoryItem, error) {
	var item MemoryItem
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return item, errors.New("missing memory frontmatter")
	}
	header, body, ok := bytes.Cut(data[4:], []byte("\n---\n"))
	if !ok {
		return item, errors.New("unterminated memory frontmatter")
	}
	var fields map[string]yaml.Node
	if err := yaml.Unmarshal(header, &fields); err != nil {
		return item, err
	}
	for _, key := range []string{"id", "class", "scope", "tags", "importance", "confidence", "created_at", "updated_at", "last_used_at", "provenance", "supersedes", "status"} {
		field, ok := fields[key]
		if !ok || field.Tag == "!!null" {
			return item, fmt.Errorf("missing memory field %s", key)
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(header))
	dec.KnownFields(true)
	if err := dec.Decode(&item); err != nil {
		return item, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return item, errors.New("multiple frontmatter documents")
	}
	item.Text = string(body)
	return item, validateMemory(item)
}
func (s *legacyMemoryReader) scan(root *os.Root) error {
	s.items = map[string]MemoryItem{}
	s.malformed = 0
	for _, class := range memoryClasses {
		dir := filepath.Join(s.opts.Base, "memory", class)
		f, err := root.Open(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			s.items = nil
			return err
		}
		entries, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			s.items = nil
			return err
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			if !entry.Type().IsRegular() {
				s.malformed++
				continue
			}
			data, err := s.read(root, filepath.Join(dir, entry.Name()))
			var item MemoryItem
			if err == nil {
				item, err = unmarshalMemory(data)
			}
			if err != nil || item.Class != class || entry.Name() != item.ID+".md" {
				s.malformed++
				continue
			}
			if _, exists := s.items[item.ID]; exists {
				s.malformed++
				continue
			}
			s.items[item.ID] = item
		}
	}
	return nil
}
