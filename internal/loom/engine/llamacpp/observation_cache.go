package llamacpp

import (
	"os"
	"time"
)

type llamaHelpCache struct {
	bin   string
	mod   int64
	text  string
	flags []LlamaFlag
}

// HelpText uses the same binary/mtime key and nonempty-result policy as Loom.
// Path resolution and native command/environment preparation belong to callers.
func (s *Supervisor) HelpText(bin string, read func() string) string {
	mod := int64(0)
	if st, err := os.Stat(bin); err == nil {
		mod = st.ModTime().UnixNano()
	}
	s.helpMu.Lock()
	if s.help.bin == bin && s.help.mod == mod && s.help.text != "" {
		text := s.help.text
		s.helpMu.Unlock()
		return text
	}
	s.helpMu.Unlock()
	text := read()
	s.helpMu.Lock()
	s.help = llamaHelpCache{bin: bin, mod: mod, text: text, flags: ParseLlamaHelp(text)}
	s.helpMu.Unlock()
	return text
}

func (s *Supervisor) HelpFlags() []LlamaFlag {
	s.helpMu.Lock()
	defer s.helpMu.Unlock()
	if s.help.bin == "" {
		return nil
	}
	out := make([]LlamaFlag, len(s.help.flags))
	copy(out, s.help.flags)
	return out
}

// RouterModeCached retains the two-second /props observation cache. It is not
// reset on launch or model change, matching the historical invalidation policy.
func (s *Supervisor) RouterModeCached(probe func() bool) bool {
	s.propsMu.Lock()
	defer s.propsMu.Unlock()
	if time.Since(s.propsSeen) < 2*time.Second {
		return s.propsRouter
	}
	s.propsRouter = probe()
	s.propsSeen = time.Now()
	return s.propsRouter
}

func (s *Supervisor) InvalidateRouterMode() {
	s.propsMu.Lock()
	defer s.propsMu.Unlock()
	s.propsSeen = time.Time{}
}
