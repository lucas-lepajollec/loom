package llamacpp

import (
	"os"
	"strings"
	"time"
)

type ggufMetaCacheEnt struct {
	mod  time.Time
	size int64
	meta GGUFMeta
}

func (s *Supervisor) LoadGGUFMeta(path string, weightBytes func(string) int64) GGUFMeta {
	path = strings.TrimSpace(path)
	if path == "" {
		return GGUFMeta{}
	}
	st, err := os.Stat(path)
	if err != nil {
		return GGUFMeta{}
	}
	s.modelMu.Lock()
	if ent, ok := s.models[path]; ok && ent.mod.Equal(st.ModTime()) && ent.size == st.Size() {
		m := ent.meta
		s.modelMu.Unlock()
		return m
	}
	s.modelMu.Unlock()
	m, _ := ReadGGUFMeta(path)
	m.Path = path
	m.FileBytes = weightBytes(path)
	s.modelMu.Lock()
	s.models[path] = ggufMetaCacheEnt{mod: st.ModTime(), size: st.Size(), meta: m}
	s.modelMu.Unlock()
	return m
}
